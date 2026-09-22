package jev

import (
	"errors"
	"fmt"
	"sort"

	"github.com/toritori0318/mekiki/internal/lint"
)

// Outcome is what a judged run adds to the report.
type Outcome struct {
	// Findings are warnings, one per answer at or over its rule's cutoff. Never errors.
	Findings []lint.Finding
	// Annotations are suffixes for existing lint findings, keyed by their index: J1's
	// probability on the L6 finding it judged, whatever the answer was.
	Annotations map[int]string
	Notes       []string
	InputTokens int
}

// Judge sends every request and reads the answers into findings. A request that fails is
// skipped and counted; an answer the server left out is counted; neither reads as clean. A
// refused key stops the run at once, since every later request would be refused too.
//
// ponytail: sequential; a bounded worker pool when a full-corpus run is slow enough to
// matter.
func Judge(c *Client, plan []Request) (Outcome, error) {
	out := Outcome{Annotations: map[int]string{}}
	failed := map[string]int{}
	unanswered := 0
	for _, r := range plan {
		ans, err := c.Ask(r.State, r.Questions)
		if err != nil {
			var ae *AuthError
			if errors.As(err, &ae) {
				return out, err
			}
			failed[err.Error()]++
			continue
		}
		out.InputTokens += ans.InputTokens
		for _, id := range r.QuestionIDs() {
			sub := r.Subjects[id]
			p, ok := ans.Noul(id)
			if !ok {
				unanswered++
				continue
			}
			rl := rules[sub.Rule]
			if sub.Annotates >= 0 {
				out.Annotations[sub.Annotates] = fmt.Sprintf(" [jev %s: reads as computation %.2f]", sub.Rule, p)
				continue
			}
			if p < rl.Cutoff {
				continue
			}
			out.Findings = append(out.Findings, lint.Finding{Rule: sub.Rule, Severity: lint.Warn,
				Skill: r.Skill.Key(), File: r.Skill.SkillMD(), Line: sub.Line, Message: message(sub, p)})
		}
	}
	reasons := make([]string, 0, len(failed))
	for reason := range failed {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	for _, reason := range reasons {
		out.Notes = append(out.Notes, fmt.Sprintf("%d skill(s) were not judged (%s)", failed[reason], reason))
	}
	if unanswered > 0 {
		out.Notes = append(out.Notes, fmt.Sprintf("%d question(s) had no answer in the response and were not judged", unanswered))
	}
	return out, nil
}

func message(sub Subject, p float64) string {
	switch sub.Rule {
	case "J2":
		return fmt.Sprintf("the Contract's Trigger reads as contradicting or unrelated to the description (jev %.2f)", p)
	case "J3":
		return fmt.Sprintf("the description reads as missing what it does, when to use it, or what to use instead (jev %.2f)", p)
	case "J4":
		return fmt.Sprintf("%s reads as performed by this skill, not merely mentioned, and disable-model-invocation is not set (jev %.2f)", sub.Detail, p)
	case "J5":
		return fmt.Sprintf("Non-goals reads as excluding work without naming its owner (jev %.2f)", p)
	}
	return fmt.Sprintf("%s (jev %.2f)", sub.Rule, p)
}
