package jev

import (
	"strings"
	"testing"

	"github.com/toritori0318/mekiki/internal/config"
	"github.com/toritori0318/mekiki/internal/lint"
	"github.com/toritori0318/mekiki/internal/skill"
)

// A plan is what --dry-run prints and what Judge sends. It is pure, so every question a
// skill would be asked can be pinned here without a socket.

const contract = "## Contract\n" +
	"- **Trigger**: the user asks for a weekly plan draft.\n" +
	"- **Inputs**: account_id.\n" +
	"- **Preconditions**: the account state directory exists.\n" +
	"- **Outputs**: 01_plan.md\n" +
	"- **Postconditions**: 01_plan.md exists.\n" +
	"- **Non-goals**: does not inspect schedule deltas (reviewing-schedule-delta owns that).\n"

func sk(name, desc, body string, fm skill.Meta) *skill.Skill {
	if fm == nil {
		fm = skill.Meta{}
	}
	fm["name"] = name
	fm["description"] = desc
	return &skill.Skill{Plugin: "acme", Name: name, Dir: "/repo/plugins/acme/skills/" + name,
		Meta: fm, Body: body, BodyStart: 5}
}

func defaults() *config.Config {
	c, _ := config.Load("")
	return c
}

func questionsOf(r Request) map[string]bool {
	out := map[string]bool{}
	for _, s := range r.Subjects {
		out[s.Rule] = true
	}
	return out
}

func TestPlanAsksOneRequestPerSkillWithTheRulesItsInputsAllow(t *testing.T) {
	full := sk("drafting-weekly-plan", "Drafts the weekly plan.", contract+"\nSteps.", nil)
	bare := sk("bare", "Does a thing.", "No contract here.", nil)

	plan := Plan([]*skill.Skill{full, bare}, nil, defaults())

	if len(plan) != 2 {
		t.Fatalf("plan has %d requests, want one per skill", len(plan))
	}
	if q := questionsOf(plan[0]); !q["J2"] || !q["J3"] || !q["J5"] {
		t.Errorf("full skill asked %v, want J2, J3 and J5", q)
	}
	if q := questionsOf(plan[1]); q["J2"] || q["J5"] || !q["J3"] {
		t.Errorf("skill without a Contract asked %v, want only J3", q)
	}
	if plan[0].State["description"] != "Drafts the weekly plan." {
		t.Errorf("state = %+v, want the description in it", plan[0].State)
	}
	if c, _ := plan[0].State["contract"].(map[string]string); c["Trigger"] == "" {
		t.Errorf("state = %+v, want the parsed Contract in it", plan[0].State)
	}
}

func TestPlanCarriesTheL6SentenceInsideTheJ1Question(t *testing.T) {
	body := "## Contract\n- **Non-goals**: none.\n\nDivide revenue by sessions and compare against the threshold.\n"
	s := sk("flash-sale", "Runs a flash sale.", body, nil)
	l6 := lint.Finding{Rule: "L6", Severity: lint.Warn, Skill: s.Key(), File: s.SkillMD(),
		Line: s.BodyLine(4), Message: `deterministic logic ("Divide") is described in prose but there is no scripts/`}

	plan := Plan([]*skill.Skill{s}, []lint.Finding{l6}, defaults())

	var j1 *Subject
	for id, sub := range plan[0].Subjects {
		if sub.Rule == "J1" {
			sub := sub
			j1 = &sub
			if !strings.Contains(plan[0].Questions[id].Instructions, "Divide revenue by sessions") {
				t.Errorf("J1 question = %q, want the matched sentence inside it", plan[0].Questions[id].Instructions)
			}
		}
	}
	if j1 == nil {
		t.Fatal("no J1 question for a skill with an L6 finding")
	}
	if j1.Annotates != 0 {
		t.Errorf("J1 annotates finding %d, want index 0 (the L6 finding)", j1.Annotates)
	}
}

func TestPlanAsksJ4OnlyWhereL20WouldNotAlreadyBeExempt(t *testing.T) {
	cfg := defaults()
	pub := "## Contract\n- **Non-goals**: none.\n\nThen publish the campaign to the storefront.\n"
	performs := sk("publishing-campaign", "Publishes campaigns.", pub, nil)
	flagged := sk("publishing-flagged", "Publishes campaigns.", pub, skill.Meta{"disable-model-invocation": true})
	quiet := sk("drafting-copy", "Drafts copy.", "## Contract\n- **Non-goals**: none.\n\nWrite the draft.\n", nil)

	plan := Plan([]*skill.Skill{performs, flagged, quiet}, nil, cfg)

	if q := questionsOf(plan[0]); !q["J4"] {
		t.Errorf("publication wording without the flag asked %v, want J4", q)
	}
	if q := questionsOf(plan[1]); q["J4"] {
		t.Errorf("a skill with disable-model-invocation asked %v, want no J4", q)
	}
	if q := questionsOf(plan[2]); q["J4"] {
		t.Errorf("a skill with no risk wording asked %v, want no J4", q)
	}
	for id, sub := range plan[0].Subjects {
		if sub.Rule == "J4" {
			if !strings.Contains(plan[0].Questions[id].Instructions, "publish the campaign") || sub.Line != performs.BodyLine(4) {
				t.Errorf("J4 = %q at %d, want the matched line at body line 4", plan[0].Questions[id].Instructions, sub.Line)
			}
		}
	}
}

func TestEstimatePricesTheWholePlanAsABound(t *testing.T) {
	s := sk("drafting-weekly-plan", "Drafts the weekly plan.", contract, nil)
	plan := Plan([]*skill.Skill{s}, nil, defaults())

	est := Estimate(plan)

	if est.Requests != 1 || est.Questions != 3 {
		t.Errorf("Estimate = %+v, want 1 request with 3 questions", est)
	}
	// The payload alone: the description, the Contract and three questions run to hundreds
	// of tokens, and a zero here would mean the body was never counted.
	if est.Tokens < 200 {
		t.Errorf("Tokens = %d, want the payload counted", est.Tokens)
	}
	if want := float64(est.Tokens) / 1e6 * 0.042; est.USD != want {
		t.Errorf("USD = %v, want %v", est.USD, want)
	}
}

func TestPlanGivesJ4EveryMatchingLineNotJustTheFirst(t *testing.T) {
	// A skill that disclaims publishing in its Non-goals and then publishes in a step would,
	// on the first match alone, be judged on the disclaimer and read as clean.
	body := "## Contract\n- **Non-goals**: never publishes without confirmation.\n\n## Steps\n1. Draft.\n2. Publish the page to the storefront.\n"
	s := sk("publishing-campaign", "Publishes campaigns.", body, nil)

	plan := Plan([]*skill.Skill{s}, nil, defaults())

	for id, sub := range plan[0].Subjects {
		if sub.Rule != "J4" {
			continue
		}
		q := plan[0].Questions[id].Instructions
		if !strings.Contains(q, "never publishes without confirmation") || !strings.Contains(q, "Publish the page to the storefront") {
			t.Errorf("J4 question = %q, want both matching lines in it", q)
		}
		if sub.Line != s.BodyLine(2) {
			t.Errorf("J4 at line %d, want the first match (body line 2) as the anchor", sub.Line)
		}
	}
}
