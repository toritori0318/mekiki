package lint

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Snapshot is the JSON shape written by `mekiki lint --format json`.
//
// Skills lists every discovered `plugin:name`. Compare does not read it — the finding delta
// is what gates a build — but `mekiki atlas --base` needs it to tell a skill that is new
// from one that merely had no findings before. A snapshot written by an older mekiki has no
// such list, which is a case the Atlas reports rather than guesses at.
type Snapshot struct {
	Summary  Summary   `json:"summary"`
	Findings []Finding `json:"findings"`
	Skills   []string  `json:"skills"`
	Notes    []string  `json:"notes"`
}

// Delta is what changed between two snapshots.
type Delta struct {
	Added    []Finding    `json:"added"`
	Resolved []Finding    `json:"resolved"`
	Summary  DeltaSummary `json:"summary"`
}

// DeltaSummary counts the change.
type DeltaSummary struct {
	Added       int `json:"added"`
	AddedErrors int `json:"added_errors"`
	Resolved    int `json:"resolved"`
}

// LoadSnapshot reads a snapshot produced by `mekiki lint --format json`.
func LoadSnapshot(path string) (*Snapshot, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read snapshot %s: %w", path, err)
	}
	var s Snapshot
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("snapshot %s is not valid JSON: %w", path, err)
	}
	return &s, nil
}

// Compare reports what a change added and resolved.
//
// The diff key is the (rule, skill) multiset, deliberately excluding line and message:
// those drift between runs (a message that embeds a count reads "637 lines" then "641
// lines"), and keying on them would report a single unchanged problem as one added plus one
// resolved finding.
func Compare(base, head *Snapshot) Delta {
	type key struct{ rule, skill string }
	group := func(fs []Finding) map[key][]Finding {
		m := map[key][]Finding{}
		for _, f := range fs {
			if strings.HasPrefix(f.Rule, "J") {
				continue // a judged verdict can move between runs; the diff is for the mechanical rules
			}
			k := key{f.Rule, f.Skill}
			m[k] = append(m[k], f)
		}
		return m
	}
	b, h := group(base.Findings), group(head.Findings)

	d := Delta{Added: []Finding{}, Resolved: []Finding{}}
	for k, items := range h {
		if extra := len(items) - len(b[k]); extra > 0 {
			d.Added = append(d.Added, items[:extra]...)
		}
	}
	for k, items := range b {
		if gone := len(items) - len(h[k]); gone > 0 {
			d.Resolved = append(d.Resolved, items[:gone]...)
		}
	}
	order := func(fs []Finding) {
		sort.SliceStable(fs, func(i, j int) bool {
			a, b := fs[i], fs[j]
			if (a.Severity == Error) != (b.Severity == Error) {
				return a.Severity == Error
			}
			if a.Skill != b.Skill {
				return a.Skill < b.Skill
			}
			return a.Rule < b.Rule
		})
	}
	order(d.Added)
	order(d.Resolved)

	d.Summary = DeltaSummary{Added: len(d.Added), Resolved: len(d.Resolved)}
	for _, f := range d.Added {
		if f.Severity == Error {
			d.Summary.AddedErrors++
		}
	}
	return d
}
