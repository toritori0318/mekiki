// Package lint performs the mechanical checks of the SkillProtocol conventions.
package lint

import (
	"sort"

	"github.com/toritori0318/mekiki/internal/config"
	"github.com/toritori0318/mekiki/internal/skill"
)

// Severity levels. Only Error affects the exit code, so heuristic rules that can produce
// false positives never break a build.
const (
	Error = "error"
	Warn  = "warn"
)

// Finding is a single reported problem. The JSON field names are part of the public
// interface: `mekiki diff` compares two snapshots, and CI pipelines parse them.
type Finding struct {
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	Skill    string `json:"skill"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Message  string `json:"message"`
}

// Scaled implements "error for new skills, warn for pre-existing ones". Together with the
// baseline file this is what lets a large existing corpus adopt the conventions gradually
// instead of being flooded with errors on day one.
func Scaled(isNew bool) string {
	if isNew {
		return Error
	}
	return Warn
}

// Context is the evaluation context handed to every rule.
type Context struct {
	Config *config.Config
	Skills []*skill.Skill // for cross-cutting rules
	Roots  []string
}

// SingleRule evaluates one skill.
type SingleRule func(s *skill.Skill, c *Context) []Finding

// CrossRule evaluates all skills together.
type CrossRule func(c *Context) []Finding

func sortFindings(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		a, b := fs[i], fs[j]
		if (a.Severity == Error) != (b.Severity == Error) {
			return a.Severity == Error
		}
		if a.Skill != b.Skill {
			return a.Skill < b.Skill
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		return a.Line < b.Line
	})
}
