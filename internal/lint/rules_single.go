package lint

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/toritori0318/mekiki/internal/skill"
)

// ---- L1: name matches the official constraints and the directory name ----
//
// Only the official (Agent Skills spec) constraints are enforced: 1-64 characters,
// lowercase alphanumerics and hyphens, no leading/trailing or consecutive hyphens, and it
// must equal the parent directory name.
//
// Category-specific shapes (gerund form and so on) are a SHOULD and deliberately not
// enforced: when they were, every one of the 25 findings on a real corpus was a perfectly
// valid official name, 62% of skills slipped through a catch-all pattern, and the problem
// worth catching (name/directory mismatch) occurred zero times. Renaming also breaks
// existing references, so the finding was not actionable.

var officialName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

const maxNameLen = 64

func L1(s *skill.Skill, c *Context) []Finding {
	var out []Finding
	md := s.SkillMD()
	if fmName := s.Meta.Str("name"); fmName != "" && fmName != s.Name {
		out = append(out, Finding{"L1", Scaled(s.IsNew), s.Key(), md, 2,
			fmt.Sprintf("name %q does not match directory name %q", fmName, s.Name)})
	}
	switch {
	case !officialName.MatchString(s.Name):
		out = append(out, Finding{"L1", Scaled(s.IsNew), s.Key(), md, 2,
			fmt.Sprintf("%q violates the official name constraints "+
				"(lowercase alphanumerics and hyphens only; no leading, trailing or consecutive hyphens)",
				s.Name)})
	case len([]rune(s.Name)) > maxNameLen:
		out = append(out, Finding{"L1", Scaled(s.IsNew), s.Key(), md, 2,
			fmt.Sprintf("%q is %d characters (official limit is %d)",
				s.Name, len([]rune(s.Name)), maxNameLen)})
	}
	return out
}

// ---- L2: description length, trigger wording, and the skill-listing cap ----
//
// Trigger wording is judged on description + when_to_use combined, because Claude Code
// concatenates when_to_use onto the description in the skill listing — wording in either
// field influences activation. The listing truncates that combination at 1,536 characters,
// so an overflow is reported too: anything past the cap never reaches the activation decision.
//
// The cap is a Claude Code behaviour, not a specification rule, so it comes from the
// runtime profile: a profile that does not concatenate the two fields sets it to 0 and the
// overflow is not reported at all.

func L2(s *skill.Skill, c *Context) []Finding {
	md := s.SkillMD()
	desc := s.Meta.Str("description")
	if desc == "" {
		return []Finding{{"L2", Scaled(s.IsNew), s.Key(), md, 2, "description is missing"}}
	}
	var out []Finding
	n := len([]rune(desc))
	if n < 150 || n > 400 {
		out = append(out, Finding{"L2", Scaled(s.IsNew), s.Key(), md, 2,
			fmt.Sprintf("description is %d characters (convention is 150-400)", n)})
	}
	wtu := s.Meta.Str("when_to_use")
	if !c.Config.Pattern("trigger").MatchString(desc + wtu) {
		out = append(out, Finding{"L2", Warn, s.Key(), md, 2,
			"description (+ when_to_use) states no trigger condition (when to use this skill)"})
	}
	if cap := c.Config.Limit("listing_cap"); cap > 0 {
		if combined := n + len([]rune(wtu)); combined > cap {
			out = append(out, Finding{"L2", Warn, s.Key(), md, 2,
				fmt.Sprintf("description + when_to_use is %d characters — the skill listing truncates "+
					"the combination at %s, so the excess never reaches the activation decision",
					combined, comma(cap))})
		}
	}
	return out
}

// ---- L3: no lifecycle wording inside description (use status: instead) ----

func L3(s *skill.Skill, c *Context) []Finding {
	desc := s.Meta.Str("description")
	if m := c.Config.Pattern("lifecycle").FindString(desc); m != "" {
		return []Finding{{"L3", Error, s.Key(), s.SkillMD(), 2,
			fmt.Sprintf("description contains lifecycle wording %q — move it to the status: field", m)}}
	}
	return nil
}

// ---- L4: the Contract block ----
//
// Severity is two-tiered. A pre-existing skill with no Contract at all is a warning, so
// that adopting the convention does not flood an existing corpus with errors. But a
// Contract that exists while missing Non-goals is an error: preventing scope creep is the
// single most valuable entry.

func L4(s *skill.Skill, c *Context) []Finding {
	ct := skill.ParseContract(s.Body)
	md := s.SkillMD()
	if !ct.Present {
		return []Finding{{"L4", Scaled(s.IsNew), s.Key(), md, s.BodyStart,
			"no ## Contract block"}}
	}
	var out []Finding
	if !ct.FirstHeading {
		out = append(out, Finding{"L4", Warn, s.Key(), md, s.BodyLine(ct.StartLine),
			"## Contract is not the first heading in the body"})
	}
	for _, item := range skill.ContractItems {
		if _, ok := ct.Items[item]; ok {
			continue
		}
		sev := Scaled(s.IsNew)
		if item == "Non-goals" {
			sev = Error
		}
		out = append(out, Finding{"L4", sev, s.Key(), md, s.BodyLine(ct.StartLine),
			fmt.Sprintf("Contract is missing **%s**", item)})
	}
	return out
}

// ---- L5: directory conventions (references/ plural, scripts/, assets/) ----

var badDirs = []string{"reference", "bin", "assets/scripts", "references/scripts"}

func L5(s *skill.Skill, c *Context) []Finding {
	var out []Finding
	for _, rel := range badDirs {
		if s.HasDir(rel) {
			out = append(out, Finding{"L5", Scaled(s.IsNew), s.Key(),
				filepath.Join(s.Dir, rel), 1,
				fmt.Sprintf("%s/ is not a conventional location (use references/ and scripts/)", rel)})
		}
	}
	return out
}

// ---- L6: deterministic logic described in prose while scripts/ is absent ----
//
// Heuristic, so it is warn-only and suppressible.

func L6(s *skill.Skill, c *Context) []Finding {
	if s.HasDir("scripts") {
		return nil
	}
	re := c.Config.Pattern("deterministic")
	for i, line := range strings.Split(s.Body, "\n") {
		if m := re.FindString(stripReferences(line)); m != "" {
			return []Finding{{"L6", Warn, s.Key(), s.SkillMD(), s.BodyLine(i + 1),
				fmt.Sprintf("deterministic logic (%q) is described in prose but there is no scripts/", m)}}
		}
	}
	return nil
}

var (
	codeSpanRe = regexp.MustCompile("`[^`]*`")
	linkRe     = regexp.MustCompile(`\[[^\]]*\]\([^)]*\)`)
	quotedRe   = regexp.MustCompile(`"[^"]*"|'[^']*'|「[^」]*」`)
	pathRe     = regexp.MustCompile(`\S+\.(md|json|jsonl|ya?ml|py|sh|ts|js|sql|csv)\b`)
)

// stripReferences removes spans where a term merely names something — inline code, link
// targets, quoted strings and bare file paths — before the prose heuristics look at a line.
// A skill name in backticks or a filename is a reference, not a description of work, and
// counting those produced every false positive L6 had on a real corpus.
func stripReferences(line string) string {
	for _, re := range []*regexp.Regexp{codeSpanRe, linkRe, quotedRe, pathRe} {
		line = re.ReplaceAllString(line, " ")
	}
	return line
}

// ---- L8: risk tier to required guard mapping ----
//
// Guard skill names are org-specific and therefore read from config; a risk kind with no
// configured guard is not checked, because nobody outside the org knows the name. The
// external-publication check is the exception: it is satisfied inside the Contract itself
// and depends on no org-specific name, so it always runs.

var risks = []struct {
	label string
	pkey  string
	kind  string
}{
	{"billing", "risk_billing", "billing"},
	{"destructive write", "risk_write", "write"},
	{"browser automation", "risk_browser", "browser"},
	{"external publication", "risk_publish", "publish"},
}

func L8(s *skill.Skill, c *Context) []Finding {
	text := s.Meta.Str("description") + "\n" + s.Body
	req := s.Meta.List("requires")
	md := s.SkillMD()
	var out []Finding
	for _, r := range risks {
		m := c.Config.Pattern(r.pkey).FindString(text)
		if m == "" {
			continue
		}
		guards := c.Config.Guard(r.kind)
		satisfied := false
		for _, g := range guards {
			if contains(req, g) {
				satisfied = true
			}
		}
		switch r.kind {
		case "billing", "browser":
			if len(guards) > 0 && !satisfied {
				out = append(out, Finding{"L8", Error, s.Key(), md, 1,
					fmt.Sprintf("%s wording (%q) is present but requires: does not declare %s",
						r.label, m, strings.Join(guards, " / "))})
			}
		case "write":
			if len(guards) > 0 && !satisfied && !hasGuardScript(s) {
				out = append(out, Finding{"L8", Error, s.Key(), md, 1,
					fmt.Sprintf("%s wording (%q) is present but there is neither %s in requires: "+
						"nor a write guard script", r.label, m, strings.Join(guards, " / "))})
			}
		case "publish":
			ct := skill.ParseContract(s.Body)
			if !hasConfirmation(ct.Items["Preconditions"]) {
				out = append(out, Finding{"L8", Error, s.Key(), md, 1,
					fmt.Sprintf("%s wording (%q) is present but Contract Preconditions "+
						"states no human confirmation step", r.label, m)})
			}
		}
	}
	return out
}

// hasConfirmation looks for a human confirmation step in the Preconditions text.
// Bilingual so that the check works on either corpus without configuration.
func hasConfirmation(text string) bool {
	lower := strings.ToLower(text)
	for _, kw := range []string{"confirm", "approval", "approve", "sign-off", "確認", "承認"} {
		if strings.Contains(lower, kw) || strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

func hasGuardScript(s *skill.Skill) bool {
	found := false
	filepath.WalkDir(filepath.Join(s.Dir, "scripts"), func(p string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		if strings.Contains(strings.ToLower(d.Name()), "guard") {
			found = true
		}
		return nil
	})
	return found
}

// ---- L9: scripts/ contains executable code but no test ----

func L9(s *skill.Skill, c *Context) []Finding {
	if !s.HasDir("scripts") {
		return nil
	}
	hasCode, hasTest := false, false
	if s.HasDir("tests") || s.HasDir("test") {
		hasTest = true
	}
	filepath.WalkDir(filepath.Join(s.Dir, "scripts"), func(p string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		n := d.Name()
		switch filepath.Ext(n) {
		case ".py", ".sh", ".js", ".ts", ".go", ".rb":
			hasCode = true
		}
		if strings.HasPrefix(n, "test_") || strings.HasSuffix(n, "_test.py") ||
			strings.HasSuffix(n, "_test.sh") || strings.HasSuffix(n, "_test.go") ||
			strings.HasSuffix(n, ".test.js") || strings.HasSuffix(n, ".test.ts") {
			hasTest = true
		}
		return nil
	})
	if hasCode && !hasTest {
		return []Finding{{"L9", Scaled(s.IsNew), s.Key(), filepath.Join(s.Dir, "scripts"), 1,
			"scripts/ contains executable code but ships no test"}}
	}
	return nil
}

// ---- L11: internal skills must set user-invocable: false ----

func L11(s *skill.Skill, c *Context) []Finding {
	desc := s.Meta.Str("description")
	if !c.Config.Pattern("internal").MatchString(desc) {
		return nil
	}
	if v, ok := s.Meta.Bool("user-invocable"); ok && !v {
		return nil
	}
	return []Finding{{"L11", Error, s.Key(), s.SkillMD(), 2,
		"description declares an internal skill but user-invocable: false is not set"}}
}

// ---- L12: non-standard frontmatter keys ----
//
// Official keys must never be penalised as "non-standard". Which keys are official depends
// on the runtime, so the set comes from the profile: under claude-code it is the Agent
// Skills specification plus the Claude Code extensions (when_to_use among them — it is
// concatenated onto the description and reaches the skill listing), while under a
// specification-only profile those extensions are genuinely non-standard.

// protocolKeys are this protocol's own declarations. The runtime does not interpret them;
// only mekiki and audits read them.
var protocolKeys = map[string]bool{
	"status": true, "canonical": true, "duplicate_of": true,
	"requires": true, "depends_on": true,
}

func L12(s *skill.Skill, c *Context) []Finding {
	var extra []string
	official := c.Config.OfficialKeys()
	for _, k := range s.Meta.Keys() {
		if !official[k] && !protocolKeys[k] {
			extra = append(extra, k)
		}
	}
	if len(extra) == 0 {
		return nil
	}
	sort.Strings(extra)
	return []Finding{{"L12", Warn, s.Key(), s.SkillMD(), 2,
		"non-standard frontmatter keys: " + strings.Join(extra, ", ")}}
}

// ---- L13: eval file format ----
//
// There are two official eval formats and both are authoritative — do not invent a
// proprietary one:
//   - output quality: evals/evals.json  {skill_name, evals[{id, prompt, expected_output}]}
//   - trigger accuracy: evals/eval_queries.json  [{query, should_trigger}]

func L13(s *skill.Skill, c *Context) []Finding {
	dir := filepath.Join(s.Dir, "evals")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var out []Finding
	for _, name := range names {
		p := filepath.Join(dir, name)
		switch name {
		case "cases.json":
			out = append(out, Finding{"L13", Warn, s.Key(), p, 1,
				"legacy proprietary format cases.json — migrate to the official " +
					"evals/evals.json (skill_name / evals[id, prompt, expected_output])"})
			continue
		case "evals.json", "eval_queries.json":
		default:
			out = append(out, Finding{"L13", Warn, s.Key(), p, 1,
				fmt.Sprintf("non-standard eval file: %s "+
					"(the convention is evals/evals.json and evals/eval_queries.json)", name)})
			continue
		}
		raw, rerr := os.ReadFile(p)
		if rerr != nil {
			continue
		}
		var problems []string
		label := "official eval schema violation: "
		if name == "eval_queries.json" {
			problems = checkQueries(raw)
			label = "official trigger-eval schema violation: "
		} else {
			problems = checkEvals(raw)
		}
		if len(problems) > 0 {
			if len(problems) > 5 {
				problems = problems[:5]
			}
			out = append(out, Finding{"L13", Error, s.Key(), p, 1, label + strings.Join(problems, "; ")})
		}
	}
	return out
}

func checkEvals(raw []byte) []string {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return []string{fmt.Sprintf("not valid JSON: %v", err)}
	}
	var problems []string
	for _, f := range []string{"skill_name", "evals"} {
		if _, ok := doc[f]; !ok {
			problems = append(problems, f+" is missing")
		}
	}
	rawEvals, ok := doc["evals"]
	if !ok {
		return problems
	}
	list, ok := rawEvals.([]any)
	if !ok {
		return append(problems, "evals is not an array")
	}
	if len(list) == 0 {
		problems = append(problems, "evals is empty (the official guidance is to start with 2-3 cases)")
	}
	for i, item := range list {
		obj, ok := item.(map[string]any)
		if !ok {
			problems = append(problems, fmt.Sprintf("evals[%d] is not an object", i))
			continue
		}
		for _, f := range []string{"id", "prompt", "expected_output"} {
			if _, ok := obj[f]; !ok {
				problems = append(problems, fmt.Sprintf("evals[%d] is missing %s", i, f))
			}
		}
		for _, f := range []string{"files", "assertions"} {
			if v, ok := obj[f]; ok {
				if _, isList := v.([]any); !isList {
					problems = append(problems, fmt.Sprintf("evals[%d].%s is not an array", i, f))
				}
			}
		}
	}
	return problems
}

func checkQueries(raw []byte) []string {
	var list []any
	if err := json.Unmarshal(raw, &list); err != nil {
		return []string{"top level is not an array"}
	}
	var problems []string
	if len(list) == 0 {
		problems = append(problems,
			"empty (the official guidance is about 20 queries: 8-10 should-trigger plus 8-10 near-misses)")
	}
	for i, item := range list {
		obj, ok := item.(map[string]any)
		if !ok {
			problems = append(problems, fmt.Sprintf("[%d] is not an object", i))
			continue
		}
		if q, ok := obj["query"].(string); !ok || q == "" {
			problems = append(problems, fmt.Sprintf("[%d] is missing query (string)", i))
		}
		if _, ok := obj["should_trigger"].(bool); !ok {
			problems = append(problems, fmt.Sprintf("[%d].should_trigger is not a boolean", i))
		}
	}
	return problems
}

// ---- L25: recorded eval results ----
//
// L13 checks that the eval *files* are in an official format, and the maturity tier used to
// treat their presence as verification. Presence is not a result: a skill can carry both
// files and fail every case in them. This rule reads `evals/results.json` — a protocol file,
// like flow.json, that records what happened when the cases were run.
//
// Running the evals stays delegated to `skill-creator`; only the outcome is read here. An
// absent file says nothing and is silent, which is what lets an existing corpus adopt this
// gradually. A file that exists must be well formed, on the same reasoning as L13 and L15:
// having written it down, get it right.
//
// Trigger accuracy is recorded but not judged. A pass mark for it would need a threshold,
// and there is no measurement here that could set one honestly.

func L25(s *skill.Skill, c *Context) []Finding {
	p := filepath.Join(s.Dir, "evals", "results.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return []Finding{{"L25", Error, s.Key(), p, 1, fmt.Sprintf("not valid JSON: %v", err)}}
	}

	var problems []string
	failing, cases := 0, 0
	rawEvals, hasEvals := doc["evals"]
	if hasEvals {
		list, ok := rawEvals.([]any)
		if !ok {
			problems = append(problems, "evals is not an array")
		}
		for i, item := range list {
			obj, ok := item.(map[string]any)
			if !ok {
				problems = append(problems, fmt.Sprintf("evals[%d] is not an object", i))
				continue
			}
			if _, ok := obj["id"]; !ok {
				problems = append(problems, fmt.Sprintf("evals[%d] is missing id", i))
			}
			pass, ok := obj["pass"].(bool)
			if !ok {
				problems = append(problems, fmt.Sprintf("evals[%d].pass is not a boolean", i))
				continue
			}
			cases++
			if !pass {
				failing++
			}
		}
	}
	rawQueries, hasQueries := doc["queries"]
	if hasQueries {
		obj, ok := rawQueries.(map[string]any)
		if !ok {
			problems = append(problems, "queries is not an object")
		} else {
			for _, f := range []string{"total", "correct"} {
				if _, ok := obj[f].(float64); !ok {
					problems = append(problems, "queries."+f+" is not a number")
				}
			}
		}
	}
	if !hasEvals && !hasQueries {
		problems = append(problems,
			"reports neither evals nor queries (the file records a run that measured nothing)")
	}
	if len(problems) > 0 {
		if len(problems) > 5 {
			problems = problems[:5]
		}
		return []Finding{{"L25", Error, s.Key(), p, 1,
			"results schema violation: " + strings.Join(problems, "; ")}}
	}
	if failing > 0 {
		return []Finding{{"L25", Warn, s.Key(), p, 1,
			fmt.Sprintf("eval results report %d of %d cases failing — the skill is not in a "+
				"verified state", failing, cases)}}
	}
	return nil
}

// ---- L15 / L17: flow.json (orchestration declaration) ----

// Flow declares the call order of an orchestrator skill.
type Flow struct {
	Flow []FlowStep `json:"flow"`
}

// FlowStep is one phase.
type FlowStep struct {
	Phase   *int     `json:"phase"`
	Skill   string   `json:"skill"`
	Inputs  []string `json:"inputs"`
	Outputs []string `json:"outputs"`
}

// LoadFlow reads flow.json. A missing file yields (nil, nil).
func LoadFlow(dir string) (*Flow, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "flow.json"))
	if err != nil {
		return nil, nil
	}
	var f Flow
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("flow.json is not valid JSON: %w", err)
	}
	return &f, nil
}

func L15(s *skill.Skill, c *Context) []Finding {
	p := filepath.Join(s.Dir, "flow.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var doc struct {
		Flow []map[string]any `json:"flow"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return []Finding{{"L15", Error, s.Key(), p, 1,
			fmt.Sprintf("flow.json is not valid JSON: %v", err)}}
	}
	var problems []string
	if doc.Flow == nil {
		problems = append(problems, "flow is not an array")
	}
	prev := -1
	seen := map[int]bool{}
	for i, e := range doc.Flow {
		phaseVal, ok := e["phase"]
		phase, isNum := phaseVal.(float64)
		if !ok || !isNum {
			problems = append(problems, fmt.Sprintf("flow[%d].phase is not an integer", i))
		} else {
			pi := int(phase)
			if seen[pi] {
				problems = append(problems, fmt.Sprintf("phase %d is duplicated", pi))
			}
			seen[pi] = true
			if pi <= prev {
				problems = append(problems, fmt.Sprintf("phase %d is not ascending", pi))
			}
			prev = pi
		}
		if sv, ok := e["skill"].(string); !ok || sv == "" {
			problems = append(problems, fmt.Sprintf("flow[%d].skill is not a string", i))
		}
		for _, f := range []string{"inputs", "outputs"} {
			if v, ok := e[f]; !ok {
				problems = append(problems, fmt.Sprintf("flow[%d] is missing %s", i, f))
			} else if _, isList := v.([]any); !isList {
				problems = append(problems, fmt.Sprintf("flow[%d].%s is not an array", i, f))
			}
		}
	}
	if len(problems) == 0 {
		return nil
	}
	if len(problems) > 5 {
		problems = problems[:5]
	}
	return []Finding{{"L15", Error, s.Key(), p, 1,
		"schema violation: " + strings.Join(problems, "; ")}}
}

func L17(s *skill.Skill, c *Context) []Finding {
	f, err := LoadFlow(s.Dir)
	if err != nil || f == nil {
		return nil // schema problems are L15's job
	}
	producedAt := map[string]int{}
	for _, e := range f.Flow {
		if e.Phase == nil {
			continue
		}
		for _, o := range e.Outputs {
			if _, ok := producedAt[o]; !ok {
				producedAt[o] = *e.Phase
			}
		}
	}
	var out []Finding
	for _, e := range f.Flow {
		if e.Phase == nil {
			continue
		}
		for _, in := range e.Inputs {
			if at, ok := producedAt[in]; ok && at >= *e.Phase {
				out = append(out, Finding{"L17", Warn, s.Key(),
					filepath.Join(s.Dir, "flow.json"), 1,
					fmt.Sprintf("phase %d consumes %q, which phase %d produces — "+
						"it is read before it exists", *e.Phase, in, at)})
			}
		}
	}
	return out
}

// ---- L24: a phase produces an artifact that no later phase reads ----
//
// The mirror of L17. L17 catches an input read before it exists; this catches an output
// written that nothing consumes — a phase kept after the step that needed it was removed, or
// an input declaration missing downstream. The Atlas already draws both sides in its
// artifact ledger; only the read-before-written half was ever a lint finding.
//
// The last phase is exempt: its outputs are what the workflow delivers. An earlier phase can
// legitimately end a branch too, which is one reason this stays warn rather than error, and
// why the message offers that reading rather than asserting a defect.

func L24(s *skill.Skill, c *Context) []Finding {
	f, err := LoadFlow(s.Dir)
	if err != nil || f == nil {
		return nil // schema problems are L15's job
	}
	last, havePhase := 0, false
	consumedAt := map[string][]int{}
	for _, e := range f.Flow {
		if e.Phase == nil {
			continue
		}
		if !havePhase || *e.Phase > last {
			last, havePhase = *e.Phase, true
		}
		for _, in := range e.Inputs {
			consumedAt[in] = append(consumedAt[in], *e.Phase)
		}
	}
	if !havePhase {
		return nil
	}

	var out []Finding
	reported := map[string]bool{}
	for _, e := range f.Flow {
		if e.Phase == nil || *e.Phase == last {
			continue
		}
		for _, o := range e.Outputs {
			if reported[o] {
				continue
			}
			readLater := false
			for _, at := range consumedAt[o] {
				if at > *e.Phase {
					readLater = true
				}
			}
			if readLater {
				continue
			}
			reported[o] = true
			out = append(out, Finding{"L24", Warn, s.Key(),
				filepath.Join(s.Dir, "flow.json"), 1,
				fmt.Sprintf("phase %d produces %q, which no later phase consumes — either the "+
					"artifact is dead, or a downstream phase is missing it from inputs",
					*e.Phase, o)})
		}
	}
	return out
}

// ---- L18: a guard declared in requires: never appears in the body ----
//
// The declaration is not interpreted by the runtime, so on its own it starts nothing.
// This check backs the imperative invocation instruction in the body. Only the presence of
// the name can be judged mechanically, hence warn.

func L18(s *skill.Skill, c *Context) []Finding {
	var out []Finding
	for _, g := range s.Meta.List("requires") {
		if g == "" || strings.Contains(s.Body, g) {
			continue
		}
		out = append(out, Finding{"L18", Warn, s.Key(), s.SkillMD(), 2,
			fmt.Sprintf("requires: declares %q but the body never mentions it "+
				"(a declaration alone starts no guard — write an imperative invocation step)", g)})
	}
	return out
}

// ---- L19: SKILL.md body size discipline ----
//
// The official guidance has two halves: "under 500 lines and 5,000 tokens". Line count
// alone misses the point on a CJK corpus — measured at ~76 characters per line, one corpus
// had a single skill over 500 lines while 93 exceeded 5,000 characters of body text.
//
// Both limits come from the official guidance rather than from any one runtime, so every
// profile carries the same values; they are read through the config only so that an
// organisation can tighten or relax them deliberately.

const splitHint = "move knowledge that is not needed on every run to references/, " +
	"and deterministic logic to scripts/"

func L19(s *skill.Skill, c *Context) []Finding {
	var out []Finding
	maxLines, maxTokens := c.Config.Limit("max_body_lines"), c.Config.Limit("max_body_tokens")
	n := len(strings.Split(s.Body, "\n"))
	if maxLines > 0 && n > maxLines {
		out = append(out, Finding{"L19", Warn, s.Key(), s.SkillMD(), s.BodyLine(maxLines + 1),
			fmt.Sprintf("body is %d lines (official guidance is under %d); %s", n, maxLines, splitHint)})
	}
	if t := skill.EstimateTokens(s.Body); maxTokens > 0 && t > maxTokens {
		out = append(out, Finding{"L19", Warn, s.Key(), s.SkillMD(), s.BodyStart,
			fmt.Sprintf("body is ~%s estimated tokens (official guidance is under %s); %s",
				comma(t), comma(maxTokens), splitHint)})
	}
	return out
}

// ---- L22: a markdown link into the skill's own directories must resolve ----
//
// The counterpart to L16 for files rather than skills: a body that sends the model to
// references/pricing.md after that file was renamed fails silently, because the model simply
// cannot read it and carries on with whatever it already had.
//
// Measurement decided the shape of this rule. A first version checked every path-like string
// in the body and was pure noise: 53 findings across 154 real skills, not one of them a
// defect. Two things dominate a skill body and neither is a claim that the file is here —
// illustrative examples in skill-authoring guides ("Examples: `references/finance.md` for
// financial schemas") and prose references to another skill's file ("see
// querying-warehouse's references/04_RECONCILIATION_GATES.md").
//
// A markdown link is such a claim. Restricting to the four conventional directories removes
// the remaining noise, since `<link>` placeholders and `${CLAUDE_SKILL_DIR}/...` variables
// are not conventional-directory paths. On the same corpus this shape covers 1,381 links
// with none broken: a check with a large healthy subject, not one with no subject at all.
//
// Note the deliberate contrast with L6, which strips inline code before looking at a line.
// Here a code span is exactly what must NOT be checked — naming a path is not linking to it.
//
// Warn rather than error: a references/ file can legitimately be generated rather than
// committed, and lint must not fail a build over that.

var localLinkRe = regexp.MustCompile(`\[[^\]]*\]\(((?:references|scripts|assets|evals)/[^)\s]*)\)`)

func L22(s *skill.Skill, c *Context) []Finding {
	var out []Finding
	seen := map[string]bool{}
	for i, line := range strings.Split(s.Body, "\n") {
		for _, m := range localLinkRe.FindAllStringSubmatch(line, -1) {
			rel, _, _ := strings.Cut(m[1], "#")
			if rel == "" || seen[rel] {
				continue
			}
			seen[rel] = true
			// Stat rather than HasFile: a link to references/ names a directory and resolves.
			if _, err := os.Stat(filepath.Join(s.Dir, rel)); err == nil {
				continue
			}
			out = append(out, Finding{"L22", Warn, s.Key(), s.SkillMD(), s.BodyLine(i + 1),
				fmt.Sprintf("link target %q does not exist in the skill", rel)})
		}
	}
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func comma(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}
