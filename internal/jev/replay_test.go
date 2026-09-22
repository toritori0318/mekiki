package jev

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/toritori0318/mekiki/internal/lint"
)

// Each rule ships fixtures under testdata/<J>/<case>/SKILL.md with expect.json labelling
// every case bad or clean. A baseline.json beside them is the recorded answer of a live run;
// the ordinary test replays it against the shipped cutoff with no key and no socket. A rule
// with no baseline is uncalibrated, which the test says rather than passes.

type baseline struct {
	Model   string             `json:"model"`
	Answers map[string]float64 `json:"answers"` // "<case>/<question id>" -> probability
}

// fixtureAnswers asks about every fixture case of a rule and returns the answers keyed the
// way a baseline stores them.
func fixtureAnswers(t *testing.T, c *Client, rule string) map[string]float64 {
	t.Helper()
	dir := filepath.Join("testdata", rule)
	res, err := lint.Run(lint.Options{Paths: []string{dir}})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]float64{}
	for _, r := range Plan(res.Parsed, res.Findings, res.Config) {
		ans, err := c.Ask(r.State, r.Questions)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range r.QuestionIDs() {
			if r.Subjects[id].Rule != rule {
				continue
			}
			if p, ok := ans.Noul(id); ok {
				out[r.Skill.Name+"/"+id] = p
			}
		}
	}
	return out
}

func ruleDirs(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func TestEveryRuleHasFixturesLabelledBothWays(t *testing.T) {
	for _, rule := range ruleDirs(t) {
		if _, ok := rules[rule]; !ok {
			t.Errorf("testdata/%s has no rule", rule)
		}
		var expect map[string]string
		raw, err := os.ReadFile(filepath.Join("testdata", rule, "expect.json"))
		if err != nil {
			t.Errorf("%s: %v", rule, err)
			continue
		}
		json.Unmarshal(raw, &expect)
		labels := map[string]bool{}
		for c, l := range expect {
			labels[l] = true
			if _, err := os.Stat(filepath.Join("testdata", rule, c, "SKILL.md")); err != nil {
				t.Errorf("%s: case %q has no SKILL.md", rule, c)
			}
		}
		if !labels["bad"] || !labels["clean"] {
			t.Errorf("%s: a fit needs both a bad and a clean case, got %v", rule, expect)
		}
		// The mechanical half has to fire on every case, or the fixture measures nothing:
		// a J1 case the L6 regex misses is never asked about.
		res, err := lint.Run(lint.Options{Paths: []string{filepath.Join("testdata", rule)}})
		if err != nil {
			t.Fatal(err)
		}
		asked := map[string]bool{}
		for _, r := range Plan(res.Parsed, res.Findings, res.Config) {
			for _, sub := range r.Subjects {
				if sub.Rule == rule {
					asked[r.Skill.Name] = true
				}
			}
		}
		for c := range expect {
			if !asked[c] {
				t.Errorf("%s: case %q is never asked %s; its matcher did not fire", rule, c, rule)
			}
		}
	}
	for id := range rules {
		if _, err := os.Stat(filepath.Join("testdata", id)); err != nil {
			t.Errorf("rule %s ships no fixtures", id)
		}
	}
}

func TestReplayBaselinesAgainstTheShippedCutoffs(t *testing.T) {
	for _, rule := range ruleDirs(t) {
		t.Run(rule, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", rule, "baseline.json"))
			if err != nil {
				t.Skipf("%s is uncalibrated: no baseline.json; record one with MEKIKI_JEV_LIVE=1 go test ./internal/jev -run Live", rule)
			}
			var b baseline
			if err := json.Unmarshal(raw, &b); err != nil {
				t.Fatal(err)
			}
			var expect map[string]string
			exp, _ := os.ReadFile(filepath.Join("testdata", rule, "expect.json"))
			json.Unmarshal(exp, &expect)
			cutoff := rules[rule].Cutoff
			for key, p := range b.Answers {
				c := filepath.Dir(key)
				want := expect[c]
				got := "clean"
				if p >= cutoff {
					got = "bad"
				}
				if got != want {
					t.Errorf("%s answered %.2f against cutoff %.2f, reads %s, labelled %s", key, p, cutoff, got, want)
				}
			}
		})
	}
}

// TestLiveRecordBaselines is the fit. It runs only when asked, needs a key, and rewrites
// every baseline.json from the fixtures; commit the result.
func TestLiveRecordBaselines(t *testing.T) {
	if os.Getenv("MEKIKI_JEV_LIVE") == "" {
		t.Skip("set MEKIKI_JEV_LIVE=1 (and TYPESAFE_API_KEY) to record baselines")
	}
	c, err := NewClientFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range ruleDirs(t) {
		answers := fixtureAnswers(t, c, rule)
		raw, _ := json.MarshalIndent(baseline{Model: c.Model, Answers: answers}, "", "  ")
		if err := os.WriteFile(filepath.Join("testdata", rule, "baseline.json"), append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: recorded %d answers", rule, len(answers))
	}
}
