// Package scaffold generates a convention-compliant skill skeleton.
//
// The point is that a new skill is born compliant, rather than being corrected by the
// linter afterwards. With twenty rules in play, "write it then fix what lint says" is a bad
// first experience and tends to produce skills that satisfy the letter of each rule
// separately.
package scaffold

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/toritori0318/mekiki/internal/config"
)

// TODO is the placeholder marker left in generated files.
const TODO = "[TODO]"

// officialName mirrors the Agent Skills constraint checked by rule L1.
var officialName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

const maxNameLen = 64

// reserved words cannot appear in a skill name.
var reserved = []string{"anthropic", "claude"}

// Kind selects the frontmatter shape.
type Kind string

// Skill kinds.
const (
	KindAction    Kind = "action"
	KindKnowledge Kind = "knowledge"
	KindUtil      Kind = "util"
)

// Risk tiers. billing and publish carry irreversible side effects, so they also get
// disable-model-invocation.
const (
	RiskBilling = "billing"
	RiskWrite   = "write"
	RiskBrowser = "browser"
	RiskPublish = "publish"
)

var riskLabels = map[string]string{
	RiskBilling: "billing",
	RiskWrite:   "destructive writes",
	RiskBrowser: "browser automation",
	RiskPublish: "external publication",
}

// needsManualOnly lists the risks whose side effects are irreversible enough that the model
// must not start them on its own.
var needsManualOnly = map[string]bool{RiskBilling: true, RiskPublish: true}

// Options configures generation.
type Options struct {
	Name string
	Out  string
	Kind Kind
	Risk string
	Cfg  *config.Config
}

// Result describes what was generated.
type Result struct {
	Dir       string
	TODOCount int
	Guards    []string
	RiskSet   bool
}

// ValidateName applies the official constraints before anything is written, so an invalid
// skill is never created.
func ValidateName(name string) error {
	if !officialName.MatchString(name) {
		return errors.New("lowercase alphanumerics and hyphens only; " +
			"no leading, trailing or consecutive hyphens (official name constraint)")
	}
	if n := len([]rune(name)); n > maxNameLen {
		return fmt.Errorf("%d characters (official limit is %d)", n, maxNameLen)
	}
	for _, w := range reserved {
		if strings.Contains(name, w) {
			return fmt.Errorf("contains the reserved word %q", w)
		}
	}
	return nil
}

// Generate writes the skeleton. It refuses to overwrite an existing directory.
func Generate(opts Options) (*Result, error) {
	if err := ValidateName(opts.Name); err != nil {
		return nil, fmt.Errorf("skill name %q is not usable: %w", opts.Name, err)
	}
	dir := filepath.Join(opts.Out, opts.Name)
	if _, err := os.Stat(dir); err == nil {
		return nil, fmt.Errorf("%s already exists (refusing to overwrite)", dir)
	}

	var guards []string
	if opts.Risk != "" && opts.Cfg != nil {
		guards = opts.Cfg.Guard(opts.Risk)
	}

	body := skillMD(opts, guards)
	if err := os.MkdirAll(filepath.Join(dir, "evals"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "references"), 0o755); err != nil {
		return nil, err
	}
	writes := []struct {
		path    string
		content []byte
	}{
		{filepath.Join(dir, "SKILL.md"), []byte(body)},
		{filepath.Join(dir, "references", ".gitkeep"), []byte("")},
		{filepath.Join(dir, "evals", "evals.json"), mustJSON(evalsTemplate(opts.Name))},
		{filepath.Join(dir, "evals", "eval_queries.json"), mustJSON(queriesTemplate())},
	}
	for _, w := range writes {
		if err := os.WriteFile(w.path, w.content, 0o644); err != nil {
			return nil, err
		}
	}
	return &Result{
		Dir:       dir,
		TODOCount: strings.Count(body, TODO),
		Guards:    guards,
		RiskSet:   opts.Risk != "",
	}, nil
}

func skillMD(opts Options, guards []string) string {
	var fm []string
	fm = append(fm, "name: "+opts.Name)
	// The description is the activation mechanism, so the template fixes the order of the
	// three elements rather than leaving the author a blank line.
	fm = append(fm, "description: "+TODO+" One or two sentences on what this does, in the third person. "+
		"Then list activation conditions with concrete user wording: \"use this when the user asks ...\". "+
		"Finish with what it does NOT cover: \"for X, use <neighbouring-skill> instead\". "+
		"(150-400 characters, and it must contain trigger wording — rule L2 checks both.)")
	if opts.Kind == KindKnowledge {
		fm = append(fm, "user-invocable: false")
	}
	if opts.Risk != "" {
		if len(guards) > 0 {
			fm = append(fm, "requires: ["+strings.Join(guards, ", ")+"]")
		} else {
			// Never invent a guard name: an unknown name would be a declaration that
			// silently protects nothing.
			fm = append(fm, "# requires: [<guard-skill>]  # "+TODO+
				" set guards."+opts.Risk+" in config.json to enable this")
		}
		if needsManualOnly[opts.Risk] {
			fm = append(fm, "disable-model-invocation: true")
		}
	}

	var b strings.Builder
	b.WriteString("---\n")
	for _, l := range fm {
		b.WriteString(l + "\n")
	}
	b.WriteString("---\n\n")

	b.WriteString("## Contract\n")
	b.WriteString("- **Trigger**: " + TODO + " one sentence, consistent with the description.\n")
	b.WriteString("- **Inputs**: required: " + TODO + " (and how to obtain it). optional: " + TODO + "\n")
	b.WriteString("- **Preconditions**: " + TODO + " what must be true before running (a verification command if possible).\n")
	b.WriteString("- **Outputs**: " + TODO + " <base>/<target>/" + opts.Name + "/{YYYYMMDD}_<slug>/01_<artifact>.md\n")
	b.WriteString("- **Postconditions**: " + TODO + " how completion is judged (an executable gate if possible).\n")
	b.WriteString("- **Non-goals**: " + TODO + " what this does not do, and which skill owns it instead (most important entry).\n\n")

	if opts.Risk != "" {
		guard := TODO + "<guard-skill>"
		if len(guards) > 0 {
			guard = guards[0]
		}
		b.WriteString("## Guard\n\n")
		b.WriteString(fmt.Sprintf("This skill involves %s, so **invoke %s with the `Skill` tool before doing any work**.\n",
			riskLabels[opts.Risk], guard))
		b.WriteString("(A `requires:` declaration is not interpreted by the runtime; this imperative step is the part that acts.)\n\n")
	}

	b.WriteString("## Steps\n\n")
	b.WriteString("1. " + TODO + " write the procedure. Anything the conventions class as deterministic\n")
	b.WriteString("   belongs in `scripts/` with a test beside it, not in prose here.\n")
	b.WriteString("2. " + TODO + " write outputs to files (do not finish by pasting into the conversation).\n\n")

	b.WriteString("## Gotchas\n\n")
	b.WriteString("- " + TODO + " record only environment-specific facts that defy reasonable assumptions;\n")
	b.WriteString("  general advice does not belong here. For example: \"the `users` table uses soft\n")
	b.WriteString("  deletes, so a query without `WHERE deleted_at IS NULL` includes deactivated accounts\".\n")
	b.WriteString("- Whenever you have to correct the model, add a line here. It is the shortest path\n")
	b.WriteString("  from a mistake to a durable fix.\n")
	return b.String()
}

type evalCase struct {
	ID             int      `json:"id"`
	Prompt         string   `json:"prompt"`
	ExpectedOutput string   `json:"expected_output"`
	Files          []string `json:"files"`
	Assertions     []string `json:"assertions"`
}

type evalsDoc struct {
	SkillName string     `json:"skill_name"`
	Notes     string     `json:"notes"`
	Evals     []evalCase `json:"evals"`
}

func evalsTemplate(name string) evalsDoc {
	return evalsDoc{
		SkillName: name,
		Notes: TODO + " write assertions after the first run: you rarely know what " +
			"\"good\" means until you have seen the output.",
		Evals: []evalCase{
			{1, TODO + " a realistic user request (include file paths and proper nouns)",
				TODO + " a human-readable description of success", []string{}, []string{}},
			{2, TODO + " a boundary case (malformed input, or an ambiguous request)",
				TODO + " the expected behaviour", []string{}, []string{}},
		},
	}
}

type queryCase struct {
	Query         string `json:"query"`
	ShouldTrigger bool   `json:"should_trigger"`
}

func queriesTemplate() []queryCase {
	return []queryCase{
		{TODO + " a request that should activate this skill (include phrasings that omit the domain word)", true},
		{TODO + " a near miss: shares vocabulary but belongs to a different skill", false},
	}
}

func mustJSON(v any) []byte {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err) // templates are static; a failure here is a programming error
	}
	return append(raw, '\n')
}
