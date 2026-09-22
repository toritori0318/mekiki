package lint

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"

	"github.com/toritori0318/mekiki/internal/config"
	"github.com/toritori0318/mekiki/internal/skill"
)

// singleRules and crossRules are the rule registry. Rule IDs are a stable public interface:
// they appear in findings, in suppress comments and in the conventions document, so they
// are never renumbered.
var (
	singleRules = []SingleRule{L1, L2, L3, L4, L5, L6, L8, L9, L11, L12, L13, L15, L17, L18, L19, L22, L24, L25}
	crossRules  = []CrossRule{L7, L10, L14, L16, L20, L21, L23}
)

// suppressRe matches an in-file suppression. A suppression without a stated reason is
// invalid by design: the reason is what makes the exception reviewable.
var suppressRe = regexp.MustCompile(`<!--\s*mekiki:\s*disable\s+(L\d+)\s+--\s*\S[^>]*?-->`)

// legacySuppressRe keeps suppressions written for the previous tool name working.
var legacySuppressRe = regexp.MustCompile(`<!--\s*skill-lint:\s*disable\s+(L\d+)\s+--\s*\S[^>]*?-->`)

// Options configures a run.
type Options struct {
	Paths        []string
	BaselinePath string
	ConfigPath   string
}

// Result is the outcome of a run.
type Result struct {
	Findings []Finding `json:"findings"`
	Notes    []string  `json:"notes"`
	Skills   int       `json:"-"`
	// SkillKeys is every discovered `plugin:name`, sorted. It goes into the JSON snapshot so
	// that a later `atlas --base` can distinguish a new skill from one that had no findings.
	SkillKeys []string `json:"-"`
	// SkillDirs maps each key to the directory it was discovered in, which is how `--changed`
	// decides whether a changed file belongs to a skill.
	SkillDirs map[string]string `json:"-"`
	// Parsed and Config are the skills as read and the settings they were read with, for a
	// later pass (`--jev`) that asks about them without discovering the corpus twice.
	Parsed []*skill.Skill `json:"-"`
	Config *config.Config `json:"-"`
}

// Summary counts findings by severity.
type Summary struct {
	Skills   int `json:"skills"`
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
}

// Summary computes the counts for a result.
func (r *Result) Summary() Summary {
	s := Summary{Skills: r.Skills}
	for _, f := range r.Findings {
		if f.Severity == Error {
			s.Errors++
		} else {
			s.Warnings++
		}
	}
	return s
}

// Run discovers skills under the given paths and evaluates every rule.
func Run(opts Options) (*Result, error) {
	skills, err := skill.Discover(opts.Paths)
	if err != nil {
		return nil, err
	}
	cfg, notes := config.Load(opts.ConfigPath)
	baseline, bnotes := loadBaseline(opts.BaselinePath)
	notes = append(bnotes, notes...)

	for _, s := range skills {
		// With no baseline every skill counts as pre-existing, which keeps a first run from
		// drowning the user in errors.
		s.IsNew = baseline != nil && !baseline[s.Key()]
	}

	roots := make([]string, 0, len(opts.Paths))
	for _, p := range opts.Paths {
		roots = append(roots, skill.Expand(p))
	}
	ctx := &Context{Config: cfg, Skills: skills, Roots: roots}

	var findings []Finding
	for _, s := range skills {
		if s.Broken {
			findings = append(findings, Finding{"L0", Error, s.Key(), s.SkillMD(), 1,
				joinProblems(s.Problems)})
			continue // with unparseable frontmatter the other rules cannot say anything useful
		}
		for _, p := range s.Problems {
			findings = append(findings, Finding{"L0", Warn, s.Key(), s.SkillMD(), 1, p})
		}
		for _, rule := range singleRules {
			findings = append(findings, rule(s, ctx)...)
		}
	}
	for _, rule := range crossRules {
		findings = append(findings, rule(ctx)...)
	}

	findings = applySuppressions(findings, skills)
	findings = dedupe(findings)
	sortFindings(findings)

	keys := make([]string, 0, len(skills))
	dirs := make(map[string]string, len(skills))
	for _, s := range skills {
		keys = append(keys, s.Key())
		dirs[s.Key()] = s.Dir
	}
	sort.Strings(keys)
	return &Result{Findings: findings, Notes: notes, Skills: len(skills),
		SkillKeys: keys, SkillDirs: dirs, Parsed: skills, Config: cfg}, nil
}

func joinProblems(ps []string) string {
	out := ""
	for i, p := range ps {
		if i > 0 {
			out += "; "
		}
		out += p
	}
	return out
}

func applySuppressions(findings []Finding, skills []*skill.Skill) []Finding {
	suppressed := map[string]map[string]bool{}
	for _, s := range skills {
		set := map[string]bool{}
		for _, m := range suppressRe.FindAllStringSubmatch(s.Text, -1) {
			set[m[1]] = true
		}
		for _, m := range legacySuppressRe.FindAllStringSubmatch(s.Text, -1) {
			set[m[1]] = true
		}
		if len(set) > 0 {
			suppressed[s.Key()] = set
		}
	}
	out := findings[:0]
	for _, f := range findings {
		if suppressed[f.Skill][f.Rule] {
			continue
		}
		out = append(out, f)
	}
	return out
}

func dedupe(findings []Finding) []Finding {
	seen := map[Finding]bool{}
	out := make([]Finding, 0, len(findings))
	for _, f := range findings {
		if seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

// ---- baseline ----

type baselineFile struct {
	Skills []string `json:"skills"`
}

func loadBaseline(path string) (map[string]bool, []string) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, []string{fmt.Sprintf(
			"no baseline at %s; treating every skill as pre-existing. "+
				"Run `mekiki lint <path> --update-baseline` to create one.", path)}
	}
	var bf baselineFile
	if err := json.Unmarshal(raw, &bf); err != nil {
		return nil, []string{fmt.Sprintf("baseline is not valid JSON (%s): %v", path, err)}
	}
	set := map[string]bool{}
	for _, k := range bf.Skills {
		set[k] = true
	}
	return set, nil
}

// WriteBaseline freezes the currently discovered skills as pre-existing.
func WriteBaseline(paths []string, dest string) (int, error) {
	skills, err := skill.Discover(paths)
	if err != nil {
		return 0, err
	}
	keys := make([]string, 0, len(skills))
	for _, s := range skills {
		keys = append(keys, s.Key())
	}
	sort.Strings(keys)
	raw, err := json.MarshalIndent(baselineFile{Skills: keys}, "", "  ")
	if err != nil {
		return 0, err
	}
	return len(keys), os.WriteFile(dest, append(raw, '\n'), 0o644)
}
