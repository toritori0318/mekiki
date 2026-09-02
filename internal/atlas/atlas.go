// Package atlas builds the Skill Atlas: a single self-contained HTML page showing
// dependencies, contracts, maturity tiers and lint findings for a whole skill corpus.
package atlas

import (
	"embed"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/toritori0318/mekiki/internal/lint"
	"github.com/toritori0318/mekiki/internal/skill"
)

//go:embed template.html
var templateFS embed.FS

// Entry is one skill as the page consumes it. JSON names are the page's data contract.
type Entry struct {
	Key            string              `json:"key"`
	Plugin         string              `json:"plugin"`
	Name           string              `json:"name"`
	Desc           string              `json:"desc"`
	Status         string              `json:"status"`
	UserInvocable  any                 `json:"ui"`
	Canonical      string              `json:"canonical"`
	Tier           int                 `json:"tier"`
	Errors         int                 `json:"errors"`
	Warns          int                 `json:"warns"`
	Findings       []EntryFinding      `json:"findings"`
	Contract       EntryContract       `json:"contract"`
	Heuristic      map[string][]string `json:"heur"`
	Requires       []string            `json:"requires"`
	DependsOn      []string            `json:"depends_on"`
	Mentions       []string            `json:"mentions"`
	Flow           []EntryFlowStep     `json:"flow"`
	FlowSource     string              `json:"flow_source"`
	IsOrchestrator bool                `json:"is_orchestrator"`
	DependedBy     []string            `json:"depended_by"`
	RefFormats     map[string]int      `json:"ref_formats"`
	HasScripts     bool                `json:"has_scripts"`
	HasTests       bool                `json:"has_tests"`
	HasRefs        bool                `json:"has_refs"`
	HasEvals       bool                `json:"has_evals"`
	// EvalResult is "" when evals/results.json is absent, else "passed" or "failing".
	EvalResult string `json:"eval_result"`
	// Diff fields are populated only when a base snapshot was given, and stay zero otherwise.
	DiffNew         bool `json:"diff_new"`
	DiffAddedErrors int  `json:"diff_added_err"`
	DiffResolved    int  `json:"diff_resolved"`
}

// EntryFinding is a lint finding as shown on the page.
type EntryFinding struct {
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// EntryContract carries the parsed Contract block.
type EntryContract struct {
	Present  bool              `json:"present"`
	Complete bool              `json:"complete"`
	Items    map[string]string `json:"items"`
}

// EntryFlowStep is one orchestration phase.
type EntryFlowStep struct {
	Phase   *int     `json:"phase"`
	Skill   string   `json:"skill"`
	Inputs  []string `json:"inputs"`
	Outputs []string `json:"outputs"`
}

// Model is the page payload.
type Model struct {
	Skills []Entry  `json:"skills"`
	Notes  []string `json:"notes"`
	// Diff is nil unless a base snapshot was given; the page then draws no delta markers at
	// all, so the default output is exactly what it was before --base existed.
	Diff *Diff `json:"diff,omitempty"`
}

// Diff summarises this corpus against a base snapshot, for the review of a single change.
//
// Only findings and the skill list are compared. Tiers are deliberately excluded: a snapshot
// does not record them, and recomputing one for the base tree would mean linting a corpus
// the page was never given.
type Diff struct {
	Added         int      `json:"added"`
	AddedErrors   int      `json:"added_errors"`
	Resolved      int      `json:"resolved"`
	NewSkills     []string `json:"new_skills"`
	RemovedSkills []string `json:"removed_skills"`
}

// heuristicHeading matches the I/O-ish headings used to infer inputs and outputs for skills
// that predate the Contract convention. Without this the page would be empty for a corpus
// that has not adopted Contract yet; the page labels these values as inferred.
var heuristicHeading = regexp.MustCompile(
	`(?i)^#{2,3}\s*(Inputs?|Outputs?|Preconditions?|入出力|入力|出力先|出力|前提条件|前提|成果物)\b`)

var formatByExt = map[string]string{
	".md": "md", ".markdown": "md", ".yaml": "yaml", ".yml": "yaml",
	".json": "json", ".jsonl": "jsonl", ".sql": "sql",
}

// Build assembles the model for the given paths. basePath, when non-empty, is a snapshot
// from `mekiki lint --format json` to compare this corpus against.
func Build(paths []string, configPath, baselinePath, basePath string) (*Model, error) {
	skills, err := skill.Discover(paths)
	if err != nil {
		return nil, err
	}
	res, err := lint.Run(lint.Options{Paths: paths, ConfigPath: configPath, BaselinePath: baselinePath})
	if err != nil {
		return nil, err
	}
	bySkill := map[string][]lint.Finding{}
	for _, f := range res.Findings {
		bySkill[f.Skill] = append(bySkill[f.Skill], f)
	}

	allNames := map[string]bool{}
	var mentionNames []string
	keysByName := map[string][]string{}
	for _, s := range skills {
		allNames[s.Name] = true
		keysByName[s.Name] = append(keysByName[s.Name], s.Key())
		// Only hyphenated names are scanned for prose mentions: short single words like
		// "setup" would match everywhere.
		if strings.Contains(s.Name, "-") {
			mentionNames = append(mentionNames, s.Name)
		}
	}
	sort.Strings(mentionNames)

	entries := make([]Entry, 0, len(skills))
	for _, s := range skills {
		fs := bySkill[s.Key()]
		e := Entry{
			Key:        s.Key(),
			Plugin:     s.Plugin,
			Name:       s.Name,
			Desc:       s.Meta.Str("description"),
			Status:     s.Meta.Str("status"),
			Canonical:  firstNonEmpty(s.Meta.Str("canonical"), s.Meta.Str("duplicate_of")),
			Requires:   orEmpty(s.Meta.List("requires")),
			DependsOn:  orEmpty(s.Meta.List("depends_on")),
			Mentions:   mentionsIn(s, mentionNames),
			RefFormats: refFormats(s.Dir),
			HasScripts: s.HasDir("scripts"),
			HasTests:   s.HasDir("tests") || s.HasDir("test"),
			HasRefs:    s.HasDir("references"),
			HasEvals:   s.HasDir("evals"),
			DependedBy: []string{},
			Heuristic:  map[string][]string{},
			Findings:   []EntryFinding{},
			Flow:       []EntryFlowStep{},
		}
		if v, ok := s.Meta.Bool("user-invocable"); ok {
			e.UserInvocable = v
		} else {
			e.UserInvocable = ""
		}
		for _, f := range fs {
			e.Findings = append(e.Findings, EntryFinding{f.Rule, f.Severity, f.Message})
			if f.Severity == lint.Error {
				e.Errors++
			} else {
				e.Warns++
			}
		}
		ct := skill.ParseContract(s.Body)
		e.Contract = EntryContract{Present: ct.Present, Complete: ct.Complete(), Items: ct.Items}
		if e.Contract.Items == nil {
			e.Contract.Items = map[string]string{}
		}
		if !ct.Present {
			e.Heuristic = heuristicIO(s.Body)
		}
		e.EvalResult = evalResult(s, fs)
		e.Flow, e.FlowSource = extractFlow(s, allNames)
		e.IsOrchestrator = len(e.Flow) > 0
		e.Tier = tierOf(s, ct, fs)
		entries = append(entries, e)
	}

	// Reverse index: requires / depends_on / prose mentions / flow targets all count as
	// "depends on", so a hub skill shows who relies on it.
	byKey := map[string]*Entry{}
	for i := range entries {
		byKey[entries[i].Key] = &entries[i]
	}
	for i := range entries {
		e := &entries[i]
		deps := map[string]bool{}
		for _, n := range e.Requires {
			deps[n] = true
		}
		for _, n := range e.DependsOn {
			deps[n] = true
		}
		for _, n := range e.Mentions {
			deps[n] = true
		}
		for _, st := range e.Flow {
			deps[st.Skill] = true
		}
		for name := range deps {
			for _, target := range keysByName[name] {
				if target == e.Key {
					continue
				}
				if t, ok := byKey[target]; ok {
					t.DependedBy = append(t.DependedBy, e.Key)
				}
			}
		}
	}
	for i := range entries {
		entries[i].DependedBy = uniqueSorted(entries[i].DependedBy)
	}

	m := &Model{Skills: entries, Notes: res.Notes}
	if basePath != "" {
		notes, err := applyDiff(m, basePath, res)
		if err != nil {
			return nil, err
		}
		m.Notes = append(m.Notes, notes...)
	}
	return m, nil
}

// applyDiff compares this run against a base snapshot and annotates the model.
func applyDiff(m *Model, basePath string, res *lint.Result) ([]string, error) {
	base, err := lint.LoadSnapshot(basePath)
	if err != nil {
		return nil, err
	}
	d := lint.Compare(base, &lint.Snapshot{Findings: res.Findings, Skills: res.SkillKeys})

	addedErrBySkill := map[string]int{}
	for _, f := range d.Added {
		if f.Severity == lint.Error {
			addedErrBySkill[f.Skill]++
		}
	}
	resolvedBySkill := map[string]int{}
	for _, f := range d.Resolved {
		resolvedBySkill[f.Skill]++
	}

	diff := &Diff{
		Added:         d.Summary.Added,
		AddedErrors:   d.Summary.AddedErrors,
		Resolved:      d.Summary.Resolved,
		NewSkills:     []string{},
		RemovedSkills: []string{},
	}
	var notes []string
	if len(base.Skills) == 0 {
		// Written by a mekiki that predates the skills list. Marking every skill new would be
		// a guess dressed as a fact.
		notes = append(notes, "the base snapshot carries no skills list (written by an older "+
			"mekiki), so new and removed skills are not shown")
	} else {
		inBase := map[string]bool{}
		for _, k := range base.Skills {
			inBase[k] = true
		}
		inHead := map[string]bool{}
		for _, k := range res.SkillKeys {
			inHead[k] = true
			if !inBase[k] {
				diff.NewSkills = append(diff.NewSkills, k)
			}
		}
		for _, k := range base.Skills {
			if !inHead[k] {
				diff.RemovedSkills = append(diff.RemovedSkills, k)
			}
		}
		sort.Strings(diff.NewSkills)
		sort.Strings(diff.RemovedSkills)
	}

	isNew := map[string]bool{}
	for _, k := range diff.NewSkills {
		isNew[k] = true
	}
	for i := range m.Skills {
		e := &m.Skills[i]
		e.DiffNew = isNew[e.Key]
		e.DiffAddedErrors = addedErrBySkill[e.Key]
		e.DiffResolved = resolvedBySkill[e.Key]
	}
	m.Diff = diff
	return notes, nil
}

// tierOf computes the maturity tier.
//
// The tier conditions map one-to-one onto lint rules, and each level is cumulative
// (modelled on SLSA's assurance levels, where L0 is simply "not yet"). "No applicable
// finding" counts as satisfied, which is why a pure knowledge skill can reach tier 2:
// having nothing to guard is a safe state.
func tierOf(s *skill.Skill, ct skill.Contract, fs []lint.Finding) int {
	hit := map[string]bool{}
	errHit := map[string]bool{}
	for _, f := range fs {
		hit[f.Rule] = true
		if f.Severity == lint.Error {
			errHit[f.Rule] = true
		}
	}
	boots := !errHit["L1"] && !errHit["L2"] && !errHit["L3"]
	if !boots || !ct.Complete() {
		return 0
	}
	safe := !hit["L6"] && !hit["L8"] && !hit["L9"] && !hit["L18"] && !hit["L20"]
	if !safe {
		return 1
	}
	// Verified requires both official eval formats: output quality and trigger accuracy.
	// Carrying the files proves the cases exist, not that they pass, so a recorded run that
	// reports a failure (or a results file we could not read) blocks the tier — L25 covers
	// both. No results file means nothing was claimed either way, and the tier is unaffected.
	verified := s.HasFile(filepath.Join("evals", "evals.json")) &&
		s.HasFile(filepath.Join("evals", "eval_queries.json")) &&
		!errHit["L13"] && !hit["L25"]
	if !verified {
		return 2
	}
	return 3
}

// evalResult summarises evals/results.json for the dossier. Any L25 finding — a failing
// case or a malformed file — reads as "failing": in both situations the recorded run does
// not support calling the skill verified.
func evalResult(s *skill.Skill, fs []lint.Finding) string {
	if !s.HasFile(filepath.Join("evals", "results.json")) {
		return ""
	}
	for _, f := range fs {
		if f.Rule == "L25" {
			return "failing"
		}
	}
	return "passed"
}

func extractFlow(s *skill.Skill, known map[string]bool) ([]EntryFlowStep, string) {
	if f, err := lint.LoadFlow(s.Dir); err == nil && f != nil {
		var out []EntryFlowStep
		for _, e := range f.Flow {
			if e.Skill == "" {
				continue
			}
			out = append(out, EntryFlowStep{e.Phase, e.Skill, orEmpty(e.Inputs), orEmpty(e.Outputs)})
		}
		if len(out) > 0 {
			return out, "declared"
		}
	}
	if out := phaseRegistry(s.Body, known); len(out) >= 2 {
		return out, "heuristic"
	}
	return []EntryFlowStep{}, ""
}

var (
	tokenRe = regexp.MustCompile(`[a-z][a-z0-9-]{2,}`)
	digitRe = regexp.MustCompile(`\d+`)
)

// phaseRegistry infers a flow from a markdown "Phase Registry" table for orchestrators that
// predate flow.json.
func phaseRegistry(body string, known map[string]bool) []EntryFlowStep {
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		s := strings.TrimSpace(l)
		if !strings.HasPrefix(s, "|") || !strings.Contains(s, "Phase") {
			continue
		}
		lower := strings.ToLower(s)
		if !strings.Contains(s, "委譲") && !strings.Contains(lower, "skill") &&
			!strings.Contains(lower, "delegat") {
			continue
		}
		header := splitRow(s)
		col := func(keys ...string) int {
			for idx, h := range header {
				hl := strings.ToLower(h)
				for _, k := range keys {
					if strings.Contains(hl, strings.ToLower(k)) || strings.Contains(h, k) {
						return idx
					}
				}
			}
			return -1
		}
		ciSkill := col("委譲", "skill", "delegat")
		ciOut := col("output", "出力", "成果")
		ciPhase := col("phase")
		if ciSkill < 0 {
			continue
		}
		var out []EntryFlowStep
		for j := i + 2; j < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[j]), "|"); j++ {
			cells := splitRow(strings.TrimSpace(lines[j]))
			if len(cells) <= ciSkill || cells[ciSkill] == "" {
				continue
			}
			// Collect every candidate token and pick one that names a real skill: a fully
			// qualified `plugin:skill` reference would otherwise resolve to the plugin name.
			var chosen string
			for _, cand := range tokenRe.FindAllString(cells[ciSkill], -1) {
				if known[cand] {
					chosen = cand
					break
				}
			}
			if chosen == "" {
				continue
			}
			step := EntryFlowStep{Skill: chosen, Inputs: []string{}, Outputs: []string{}}
			if ciPhase >= 0 && len(cells) > ciPhase {
				if m := digitRe.FindString(cells[ciPhase]); m != "" {
					n := 0
					for _, r := range m {
						n = n*10 + int(r-'0')
					}
					step.Phase = &n
				}
			}
			if ciOut >= 0 && len(cells) > ciOut {
				for _, o := range strings.FieldsFunc(cells[ciOut], func(r rune) bool {
					return r == '/' || r == ',' || r == '、' || r == '／'
				}) {
					if o = strings.TrimSpace(o); o != "" {
						step.Outputs = append(step.Outputs, o)
					}
				}
			}
			out = append(out, step)
		}
		return out
	}
	return nil
}

func splitRow(s string) []string {
	s = strings.Trim(s, "|")
	parts := strings.Split(s, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func heuristicIO(body string) map[string][]string {
	out := map[string][]string{}
	lines := strings.Split(body, "\n")
	for i := 0; i < len(lines); {
		m := heuristicHeading.FindStringSubmatch(strings.TrimSpace(lines[i]))
		if m == nil {
			i++
			continue
		}
		key := m[1]
		var buf []string
		j := i + 1
		for j < len(lines) && !strings.HasPrefix(strings.TrimLeft(lines[j], " \t"), "#") && len(buf) < 3 {
			if s := strings.TrimSpace(lines[j]); s != "" {
				if len([]rune(s)) > 120 {
					s = string([]rune(s)[:120])
				}
				buf = append(buf, s)
			}
			j++
		}
		if len(buf) > 0 {
			if _, exists := out[key]; !exists {
				out[key] = buf
			}
		}
		i = j
	}
	return out
}

func refFormats(dir string) map[string]int {
	out := map[string]int{}
	refs := filepath.Join(dir, "references")
	filepath.WalkDir(refs, func(p string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		k, ok := formatByExt[strings.ToLower(filepath.Ext(d.Name()))]
		if !ok {
			k = "other"
		}
		out[k]++
		return nil
	})
	return out
}

func mentionsIn(s *skill.Skill, names []string) []string {
	out := []string{}
	for _, n := range names {
		if n == s.Name {
			continue
		}
		if mentionRe(n).MatchString(s.Body) {
			out = append(out, n)
		}
	}
	return out
}

var mentionCache = map[string]*regexp.Regexp{}

// mentionRe matches a skill name as a whole token.
//
// Go's RE2 has no lookbehind, so the boundary is expressed as an optional captured prefix
// and checked positionally instead of with (?<!...).
func mentionRe(name string) *regexp.Regexp {
	if re, ok := mentionCache[name]; ok {
		return re
	}
	re := regexp.MustCompile(`(^|[^a-z0-9-])` + regexp.QuoteMeta(name) + `($|[^a-z0-9-])`)
	mentionCache[name] = re
	return re
}

// Render inlines the model into the embedded template.
func Render(m *Model) (string, error) {
	tpl, err := templateFS.ReadFile("template.html")
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	// The placeholder is a JS-comment-wrapped null so the template stays a valid,
	// openable page on its own.
	return strings.Replace(string(tpl), "/*__DATA__*/null", string(payload), 1), nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func orEmpty(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func uniqueSorted(v []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, x := range v {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}
