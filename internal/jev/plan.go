package jev

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/toritori0318/mekiki/internal/config"
	"github.com/toritori0318/mekiki/internal/lint"
	"github.com/toritori0318/mekiki/internal/skill"
)

// Request is everything sent about one skill: one state, several questions.
type Request struct {
	Skill     *skill.Skill
	State     map[string]any
	Questions map[string]Question
	// Subjects says what each question's answer means, keyed like Questions.
	Subjects map[string]Subject
}

// Subject is one question's meaning for the report.
type Subject struct {
	Rule string
	Line int // SKILL.md line the finding lands on
	// Annotates is the index of the lint finding a high or low answer is appended to, or -1
	// when the answer becomes a finding of its own. J1 annotates the L6 finding it judged.
	Annotates int
	Detail    string // for the message: the tier for J4
}

// Plan builds the requests for the skills in scope. It is pure: nothing is sent.
//
// The state is the least context that holds the answers — description, when_to_use and the
// parsed Contract. A sentence a rule is about travels inside its question, so an unrelated
// edit elsewhere in the body cannot move that verdict.
func Plan(skills []*skill.Skill, findings []lint.Finding, cfg *config.Config) []Request {
	targets := lint.DelegationTargets(skills)
	guards := cfg.GuardNames()
	var out []Request
	for _, s := range skills {
		if s.Broken {
			continue
		}
		r := Request{Skill: s, Questions: map[string]Question{}, Subjects: map[string]Subject{}}
		contract := skill.ParseContract(s.Body)
		r.State = map[string]any{
			"skill":       s.Name,
			"description": s.Meta.Str("description"),
		}
		if w := s.Meta.Str("when_to_use"); w != "" {
			r.State["when_to_use"] = w
		}
		if contract.Present {
			r.State["contract"] = contract.Items
		}
		contractLine := s.BodyLine(max(contract.StartLine, 1))

		ask := func(id string, q Question, sub Subject) {
			r.Questions[id] = q
			r.Subjects[id] = sub
		}
		plain := func(rl rule) Question { return Noul(rl.Statement, rl.WhenTrue, rl.WhenFalse) }

		// J1: one per L6 finding on this skill, the matched line inside the question.
		for i, f := range findings {
			if f.Rule != "L6" || f.Skill != s.Key() {
				continue
			}
			rl := rules["J1"]
			q := Noul(rl.Statement+"\n\nsentence: "+bodyLine(s, f.Line), rl.WhenTrue, rl.WhenFalse)
			ask(fmt.Sprintf("J1:%d", f.Line), q, Subject{Rule: "J1", Line: f.Line, Annotates: i})
		}
		if contract.Items["Trigger"] != "" {
			ask("J2", plain(rules["J2"]), Subject{Rule: "J2", Line: contractLine, Annotates: -1})
		}
		if s.Meta.Str("description") != "" {
			ask("J3", plain(rules["J3"]), Subject{Rule: "J3", Line: 1, Annotates: -1})
		}
		// J4: the L20 tiers the wording made unactionable, with L20's own exemptions.
		if v, ok := s.Meta.Bool("disable-model-invocation"); !(ok && v) && !targets[s.Name] && !guards[s.Name] {
			for _, tier := range []struct{ key, name string }{{"risk_publish", "external publication"}, {"risk_write", "destructive write"}} {
				// Every matching line travels, not the first: a skill that disclaims the operation
				// in its Non-goals and performs it in a step must be judged on the step.
				re := cfg.Pattern(tier.key)
				var lines []string
				first := 0
				for i, line := range strings.Split(s.Body, "\n") {
					if re.MatchString(line) {
						if first == 0 {
							first = i + 1
						}
						lines = append(lines, strings.TrimSpace(line))
					}
				}
				if first == 0 {
					continue
				}
				rl := rules["J4"]
				q := Noul(rl.Statement+"\n\noperation: "+tier.name+"\nsentences:\n"+strings.Join(lines, "\n"), rl.WhenTrue, rl.WhenFalse)
				ask("J4:"+tier.key, q, Subject{Rule: "J4", Line: s.BodyLine(first), Annotates: -1, Detail: tier.name})
			}
		}
		if contract.Items["Non-goals"] != "" {
			ask("J5", plain(rules["J5"]), Subject{Rule: "J5", Line: contractLine, Annotates: -1})
		}
		out = append(out, r)
	}
	return out
}

func bodyLine(s *skill.Skill, skillMDLine int) string {
	lines := strings.Split(s.Body, "\n")
	i := skillMDLine - s.BodyStart
	if i < 0 || i >= len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[i])
}

// Cost is what --dry-run prints. It counts the payload only: the server bills some fixed
// overhead per request on top, which nobody here has measured, so read it as the floor of
// what a run costs rather than a quote. Comparing it with the billed input_tokens of a real
// run is how to learn the overhead.
type Cost struct {
	Skills, Requests, Questions, Tokens int
	USD                                 float64
}

// usdPerMTok is the published input price; output is not billed.
const usdPerMTok = 0.042

// Estimate prices a plan without sending it.
func Estimate(plan []Request) Cost {
	e := Cost{Skills: len(plan), Requests: len(plan)}
	for _, r := range plan {
		body, _ := json.Marshal(map[string]any{"model": DefaultModel, "state": r.State, "questions": r.Questions})
		e.Questions += len(r.Questions)
		e.Tokens += skill.EstimateTokens(string(body))
	}
	e.USD = float64(e.Tokens) / 1e6 * usdPerMTok
	return e
}

// String is the one-line summary --dry-run prints.
func (e Cost) String() string {
	return fmt.Sprintf("jev dry run: %d skill(s), %d request(s), %d question(s), ~%d payload tokens (server overhead not included), ~$%.5f — nothing was sent",
		e.Skills, e.Requests, e.Questions, e.Tokens, e.USD)
}

// QuestionIDs returns a request's question ids in a stable order, for --dry-run listings.
func (r Request) QuestionIDs() []string {
	ids := make([]string, 0, len(r.Questions))
	for id := range r.Questions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
