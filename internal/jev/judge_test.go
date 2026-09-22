package jev

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/toritori0318/mekiki/internal/lint"
	"github.com/toritori0318/mekiki/internal/skill"
)

// Judge turns answers into the report. Its contract: a high answer is a warning and never an
// error, J1 annotates rather than adds, and a request that failed or an answer that never
// came is said so — silence must not read as a clean corpus.

// fake answers every noul question with the probability keyed by question id; a skill named
// in fail gets a 500.
func fake(t *testing.T, byID map[string]float64, fail map[string]bool) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			State     map[string]any            `json:"state"`
			Questions map[string]map[string]any `json:"questions"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if fail[req.State["skill"].(string)] {
			w.WriteHeader(500)
			return
		}
		answers := map[string]any{}
		for id := range req.Questions {
			if p, ok := byID[id]; ok {
				answers[id] = map[string]any{"type": "noul", "noul": p}
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"answers": answers, "usage": map[string]int{"input_tokens": 100}})
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, APIKey: "k", Retries: 1, Sleep: func(d time.Duration) {}}
}

func TestJudgeReportsAHighAnswerAsAWarningAndALowOneNotAtAll(t *testing.T) {
	s := sk("drafting-weekly-plan", "Drafts the weekly plan.", contract, nil)
	plan := Plan([]*skill.Skill{s}, nil, defaults())
	c := fake(t, map[string]float64{"J2": 0.82, "J3": 0.12, "J5": 0.5}, nil)

	out, err := Judge(c, plan)
	if err != nil {
		t.Fatal(err)
	}

	rules := map[string]lint.Finding{}
	for _, f := range out.Findings {
		rules[f.Rule] = f
		if f.Severity != lint.Warn {
			t.Errorf("%s reported as %s, want warn: a judged rule never fails a build", f.Rule, f.Severity)
		}
	}
	if _, ok := rules["J2"]; !ok {
		t.Error("J2 at 0.82 produced no finding")
	}
	if _, ok := rules["J3"]; ok {
		t.Error("J3 at 0.12 produced a finding")
	}
	if _, ok := rules["J5"]; !ok {
		t.Error("J5 exactly at the cutoff produced no finding; the cutoff is inclusive")
	}
	if f := rules["J2"]; !strings.Contains(f.Message, "0.82") || f.Skill != s.Key() || f.Line != s.BodyLine(1) {
		t.Errorf("J2 = %+v, want the probability in the message, on the skill, at the Contract line", f)
	}
}

func TestJudgeAnnotatesTheL6FindingInsteadOfAddingOne(t *testing.T) {
	body := "## Contract\n- **Non-goals**: none.\n\nDivide revenue by sessions.\n"
	s := sk("flash-sale", "Runs a flash sale.", body, nil)
	l6 := lint.Finding{Rule: "L6", Severity: lint.Warn, Skill: s.Key(), File: s.SkillMD(), Line: s.BodyLine(4), Message: "deterministic logic"}
	plan := Plan([]*skill.Skill{s}, []lint.Finding{l6}, defaults())
	id := "J1:" + itoa(s.BodyLine(4))
	c := fake(t, map[string]float64{id: 0.91, "J3": 0.1, "J5": 0.1}, nil)

	out, err := Judge(c, plan)
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range out.Findings {
		if f.Rule == "J1" {
			t.Errorf("J1 became its own finding %+v; it should annotate the L6 one", f)
		}
	}
	if note := out.Annotations[0]; !strings.Contains(note, "0.91") || !strings.Contains(note, "J1") {
		t.Errorf("annotation on the L6 finding = %q, want J1's probability", note)
	}
}

func TestJudgeKeepsGoingPastAFailedSkillAndSaysSo(t *testing.T) {
	a := sk("alpha", "Does alpha.", contract, nil)
	b := sk("beta", "Does beta.", contract, nil)
	plan := Plan([]*skill.Skill{a, b}, nil, defaults())
	c := fake(t, map[string]float64{"J2": 0.9, "J3": 0.9, "J5": 0.9}, map[string]bool{"alpha": true})

	out, err := Judge(c, plan)
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range out.Findings {
		if f.Skill == a.Key() {
			t.Errorf("finding %+v on a skill whose request failed", f)
		}
	}
	if n := len(out.Findings); n != 3 {
		t.Errorf("%d findings, want beta's three", n)
	}
	if !hasNote(out.Notes, "1 skill(s) were not judged") {
		t.Errorf("notes = %q, want the failed skill counted", out.Notes)
	}
}

func TestJudgeCountsAMissingAnswerAsNotJudged(t *testing.T) {
	s := sk("alpha", "Does alpha.", contract, nil)
	plan := Plan([]*skill.Skill{s}, nil, defaults())
	c := fake(t, map[string]float64{"J2": 0.9}, nil) // J3 and J5 never answered

	out, err := Judge(c, plan)
	if err != nil {
		t.Fatal(err)
	}

	if len(out.Findings) != 1 || out.Findings[0].Rule != "J2" {
		t.Errorf("findings = %+v, want only the answered J2", out.Findings)
	}
	if !hasNote(out.Notes, "2 question(s) had no answer") {
		t.Errorf("notes = %q, want the unanswered questions counted, never read as clean", out.Notes)
	}
}

func TestJudgeStopsOnARefusedKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(402) }))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", Sleep: func(time.Duration) {}}
	plan := Plan([]*skill.Skill{sk("a", "A.", contract, nil), sk("b", "B.", contract, nil)}, nil, defaults())

	_, err := Judge(c, plan)

	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err = %v, want the auth error surfaced so the run exits 2", err)
	}
}

func hasNote(notes []string, want string) bool {
	for _, n := range notes {
		if strings.Contains(n, want) {
			return true
		}
	}
	return false
}

func itoa(i int) string { return strconv.Itoa(i) }
