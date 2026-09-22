package lint

import "testing"

// Delta mode exists because warnings pass the CI gate by design: surfacing "what this
// change added" is what makes warn-level findings reviewable in a pull request.

func snap(fs ...Finding) *Snapshot { return &Snapshot{Findings: fs} }

func f(rule, skillKey, severity, message string, line int) Finding {
	return Finding{Rule: rule, Severity: severity, Skill: skillKey,
		File: "/x/" + skillKey + "/SKILL.md", Line: line, Message: message}
}

func TestCompareAddedAndResolved(t *testing.T) {
	base := snap(f("L2", "p:a", Warn, "msg", 1), f("L2", "p:a", Warn, "msg", 1))
	head := snap(f("L2", "p:a", Warn, "msg", 1), f("L19", "p:b", Warn, "body is long", 1))
	d := Compare(base, head)

	if d.Summary.Added != 1 || d.Added[0].Rule != "L19" {
		t.Errorf("added = %+v", d.Added)
	}
	if d.Summary.Resolved != 1 || d.Resolved[0].Rule != "L2" {
		t.Errorf("resolved = %+v", d.Resolved)
	}
	if d.Summary.AddedErrors != 0 {
		t.Errorf("addedErrors = %d, want 0", d.Summary.AddedErrors)
	}
}

func TestCompareCountsAddedErrorsSeparately(t *testing.T) {
	d := Compare(snap(), snap(f("L3", "p:a", Error, "lifecycle wording", 2)))
	if d.Summary.AddedErrors != 1 {
		t.Errorf("addedErrors = %d, want 1", d.Summary.AddedErrors)
	}
}

func TestCompareIgnoresLineAndMessageDrift(t *testing.T) {
	// The same unchanged problem must not be reported as one added plus one resolved just
	// because the message embeds a count that moved.
	base := snap(f("L19", "p:a", Warn, "body is 637 lines", 10))
	head := snap(f("L19", "p:a", Warn, "body is 641 lines", 12))
	d := Compare(base, head)
	if d.Summary.Added != 0 || d.Summary.Resolved != 0 {
		t.Errorf("drift reported as a delta: %+v", d)
	}
}

func TestCompareEmptyIsNoDelta(t *testing.T) {
	d := Compare(snap(), snap())
	if d.Summary.Added != 0 || d.Summary.Resolved != 0 {
		t.Errorf("empty comparison = %+v", d)
	}
	if d.Added == nil || d.Resolved == nil {
		t.Error("slices must be non-nil so JSON renders [] rather than null")
	}
}

func TestCompareIgnoresJudgedRules(t *testing.T) {
	// A judged verdict near its cutoff can land on either side between runs, and keyed into
	// the diff it would read as one added plus one resolved finding. The diff is for the
	// mechanical rules; the judged ones are read on the lint report itself.
	base := &Snapshot{Findings: []Finding{{Rule: "J2", Skill: "acme:alpha", Severity: Warn}}}
	head := &Snapshot{Findings: []Finding{{Rule: "J3", Skill: "acme:alpha", Severity: Warn}, {Rule: "L6", Skill: "acme:alpha", Severity: Warn}}}

	d := Compare(base, head)

	if len(d.Added) != 1 || d.Added[0].Rule != "L6" || len(d.Resolved) != 0 {
		t.Errorf("Compare = added %+v resolved %+v, want only the L6 add", d.Added, d.Resolved)
	}
}
