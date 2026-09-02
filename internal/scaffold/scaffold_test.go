package scaffold

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toritori0318/mekiki/internal/config"
	"github.com/toritori0318/mekiki/internal/lint"
)

func gen(t *testing.T, opts Options) (*Result, string) {
	t.Helper()
	if opts.Out == "" {
		opts.Out = t.TempDir()
	}
	if opts.Name == "" {
		opts.Name = "drafting-weekly-plan"
	}
	if opts.Kind == "" {
		opts.Kind = KindAction
	}
	if opts.Cfg == nil {
		opts.Cfg, _ = config.Load("")
	}
	res, err := Generate(opts)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(res.Dir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	return res, string(body)
}

func TestRejectsInvalidNames(t *testing.T) {
	out := t.TempDir()
	for _, bad := range []string{"MySkill_v2", "pdf--processing", "-leading", "trailing-",
		strings.Repeat("a", 65), "claude-helper", "anthropic-tools"} {
		if _, err := Generate(Options{Name: bad, Out: out}); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
		if _, err := os.Stat(filepath.Join(out, bad)); err == nil {
			t.Errorf("%q must not be created", bad)
		}
	}
}

func TestGeneratesExpectedFiles(t *testing.T) {
	res, _ := gen(t, Options{})
	for _, rel := range []string{"SKILL.md", "evals/evals.json", "evals/eval_queries.json",
		"references/.gitkeep"} {
		if _, err := os.Stat(filepath.Join(res.Dir, rel)); err != nil {
			t.Errorf("missing %s", rel)
		}
	}
}

func TestContractIsFirstHeadingWithAllItems(t *testing.T) {
	_, body := gen(t, Options{})
	var headings []string
	for _, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(l, "## ") {
			headings = append(headings, l)
		}
	}
	if len(headings) == 0 || headings[0] != "## Contract" {
		t.Fatalf("first heading = %v", headings)
	}
	for _, item := range []string{"Trigger", "Inputs", "Preconditions", "Outputs",
		"Postconditions", "Non-goals"} {
		if !strings.Contains(body, "- **"+item+"**:") {
			t.Errorf("Contract is missing %s", item)
		}
	}
}

func TestHasGotchasSection(t *testing.T) {
	if _, body := gen(t, Options{}); !strings.Contains(body, "## Gotchas") {
		t.Error("the template must include a Gotchas section")
	}
}

func TestEvalsMatchOfficialSchemas(t *testing.T) {
	res, _ := gen(t, Options{})
	var doc struct {
		SkillName string `json:"skill_name"`
		Evals     []struct {
			ID             int    `json:"id"`
			Prompt         string `json:"prompt"`
			ExpectedOutput string `json:"expected_output"`
		} `json:"evals"`
	}
	raw, _ := os.ReadFile(filepath.Join(res.Dir, "evals/evals.json"))
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.SkillName == "" || len(doc.Evals) == 0 {
		t.Fatalf("evals.json = %s", raw)
	}
	for _, e := range doc.Evals {
		if e.Prompt == "" || e.ExpectedOutput == "" {
			t.Errorf("incomplete eval case: %+v", e)
		}
	}

	var queries []struct {
		Query         string `json:"query"`
		ShouldTrigger bool   `json:"should_trigger"`
	}
	raw, _ = os.ReadFile(filepath.Join(res.Dir, "evals/eval_queries.json"))
	if err := json.Unmarshal(raw, &queries); err != nil {
		t.Fatal(err)
	}
	var pos, neg bool
	for _, q := range queries {
		if q.ShouldTrigger {
			pos = true
		} else {
			neg = true
		}
	}
	if !pos || !neg {
		t.Error("trigger eval needs both a should-trigger and a near-miss case")
	}
}

func TestRiskBillingAddsGuardAndDisablesModelInvocation(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath,
		[]byte(`{"guards":{"billing":["billing-guard"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(cfgPath)
	_, body := gen(t, Options{Name: "charging-ad-budget", Risk: RiskBilling, Cfg: cfg})

	if !strings.Contains(body, "disable-model-invocation: true") {
		t.Error("billing carries irreversible spend, so the flag must be set")
	}
	if !strings.Contains(body, "requires: [billing-guard]") {
		t.Error("the configured guard must be declared")
	}
	if !strings.Contains(body, "invoke billing-guard with the `Skill` tool") {
		t.Error("the body needs an imperative invocation step (rule L18)")
	}
}

func TestRiskWithoutConfiguredGuardLeavesTODO(t *testing.T) {
	_, body := gen(t, Options{Name: "charging-ad-budget", Risk: RiskBilling})
	if !strings.Contains(body, "disable-model-invocation: true") {
		t.Error("the flag does not depend on org config")
	}
	if strings.Contains(body, "\nrequires:") {
		t.Error("must not invent a guard name that was never configured")
	}
	if !strings.Contains(body, TODO) {
		t.Error("the unconfigured guard should be left as a TODO")
	}
}

func TestNoRiskOmitsGuardKeys(t *testing.T) {
	_, body := gen(t, Options{})
	if strings.Contains(body, "requires:") || strings.Contains(body, "disable-model-invocation") {
		t.Error("guard keys must not appear without --risk")
	}
}

func TestKnowledgeKindIsNotUserInvocable(t *testing.T) {
	_, body := gen(t, Options{Name: "knowledge-shared-master", Kind: KindKnowledge})
	if !strings.Contains(body, "user-invocable: false") {
		t.Error("knowledge skills must set user-invocable: false (rule L11)")
	}
}

func TestRefusesToOverwrite(t *testing.T) {
	out := t.TempDir()
	if _, err := Generate(Options{Name: "drafting-weekly-plan", Out: out}); err != nil {
		t.Fatal(err)
	}
	_, err := Generate(Options{Name: "drafting-weekly-plan", Out: out})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("a second generation must refuse: %v", err)
	}
}

func TestReportsTODOCount(t *testing.T) {
	res, _ := gen(t, Options{})
	if res.TODOCount < 5 {
		t.Errorf("TODOCount = %d, expected the template to mark several spots", res.TODOCount)
	}
}

// The strongest check: the skeleton must not itself violate the conventions it teaches.
func TestGeneratedSkillPassesLintExceptDescription(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "plugins", "myplugin", "skills")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(Options{Name: "drafting-weekly-plan", Out: out,
		Kind: KindAction}); err != nil {
		t.Fatal(err)
	}
	res, err := lint.Run(lint.Options{Paths: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	// The description is still a placeholder, so L2 is expected. Nothing else may fire.
	for _, f := range res.Findings {
		if f.Rule != "L2" {
			t.Errorf("the skeleton violates its own conventions: %s %s — %s",
				f.Rule, f.Severity, f.Message)
		}
	}
}
