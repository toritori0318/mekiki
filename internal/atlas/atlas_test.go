package atlas

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toritori0318/mekiki/internal/lint"
)

const goodDesc = "Drafts a report. Use this when the user asks for a report or a summary " +
	"of aggregated results, or when preparing the regular review. " +
	"If the goal is only to fetch data, use querying-warehouse instead of this skill. " +
	"This description is padded so the length rule stays quiet in these fixtures."

const goodContract = "## Contract\n" +
	"- **Trigger**: the user asks for a report.\n" +
	"- **Inputs**: required: account_id.\n" +
	"- **Preconditions**: data has been fetched.\n" +
	"- **Outputs**: out/01_report.md\n" +
	"- **Postconditions**: 01_report.md exists.\n" +
	"- **Non-goals**: does not fetch data (delegated to querying-warehouse).\n"

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func corpus(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	sk := func(name, desc, body, extraFM string) {
		write(t, filepath.Join(root, "plugins/px/skills", name, "SKILL.md"),
			"---\nname: "+name+"\ndescription: "+desc+"\n"+extraFM+"---\n\n"+body+"\n")
	}

	// Pre-Contract skill: requires, a prose mention and I/O headings to infer from.
	sk("querying-warehouse", "Fetches data.",
		"## Inputs\n- account_id (given by the user)\n\n"+
			"## Outputs\n- out/report.md\n\n"+
			"## Steps\n\nAfterwards, invoke building-report.\n",
		"requires: [guard-skill]\n")
	for _, f := range []string{"guide.md", "state.yaml", "history.jsonl", "sql/q.sql"} {
		write(t, filepath.Join(root, "plugins/px/skills/querying-warehouse/references", f), "x\n")
	}

	// Fully compliant skill with both official eval formats: reaches tier 3.
	sk("building-report", goodDesc, goodContract, "")
	write(t, filepath.Join(root, "plugins/px/skills/building-report/evals/evals.json"),
		`{"skill_name":"building-report","evals":[{"id":1,"prompt":"make a report","expected_output":"01_report.md"}]}`)
	write(t, filepath.Join(root, "plugins/px/skills/building-report/evals/eval_queries.json"),
		`[{"query":"make a report","should_trigger":true},{"query":"just fetch data","should_trigger":false}]`)

	sk("guard-skill", goodDesc, "body", "")

	// Declared flow.
	sk("running-flow", goodDesc, goodContract, "")
	write(t, filepath.Join(root, "plugins/px/skills/running-flow/flow.json"),
		`{"flow":[{"phase":1,"skill":"querying-warehouse","inputs":["account_id"],"outputs":["01_ctx.json"]},`+
			`{"phase":2,"skill":"building-report","inputs":["01_ctx.json"],"outputs":["02_report.md"]}]}`)

	// Inferred flow from a Phase Registry table.
	sk("legacy-flow", goodDesc,
		"### Phase Registry\n\n"+
			"| Phase | Task | Delegate | Output |\n|---|---|---|---|\n"+
			"| 1 | Intake | querying-warehouse | ctx.json |\n"+
			"| 2 | Report | building-report | report.md |\n", "")

	// Inferred flow whose delegates are fully qualified `plugin:skill` references. Taking
	// the first token would resolve to the plugin name and drop every row.
	sk("qualified-flow", goodDesc,
		"| Phase | Name | Delegate | In -> Out |\n|---|---|---|---|\n"+
			"| R1 | Intake | `acme-toolkit:querying-warehouse` | account_id -> ctx.json |\n"+
			"| R2 | Report | `acme-toolkit:building-report` | ctx.json -> report.md |\n", "")

	return root
}

func build(t *testing.T) map[string]Entry {
	t.Helper()
	m, err := Build([]string{corpus(t)}, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]Entry{}
	for _, e := range m.Skills {
		byKey[e.Key] = e
	}
	return byKey
}

func TestRequiresAndMentionsExtracted(t *testing.T) {
	byKey := build(t)
	q := byKey["px:querying-warehouse"]
	if len(q.Requires) != 1 || q.Requires[0] != "guard-skill" {
		t.Errorf("requires = %v", q.Requires)
	}
	if !containsStr(q.Mentions, "building-report") {
		t.Errorf("prose mention not detected: %v", q.Mentions)
	}
}

func TestDependedByReverseIndex(t *testing.T) {
	byKey := build(t)
	if !containsStr(byKey["px:guard-skill"].DependedBy, "px:querying-warehouse") {
		t.Errorf("guard-skill dependedBy = %v", byKey["px:guard-skill"].DependedBy)
	}
	// Flow delegation also counts as a dependency.
	if !containsStr(byKey["px:building-report"].DependedBy, "px:running-flow") {
		t.Errorf("building-report dependedBy = %v", byKey["px:building-report"].DependedBy)
	}
}

func TestContractAndTier(t *testing.T) {
	byKey := build(t)
	b := byKey["px:building-report"]
	if !b.Contract.Complete {
		t.Fatalf("contract = %+v", b.Contract)
	}
	if b.Contract.Items["Outputs"] != "out/01_report.md" {
		t.Errorf("Outputs = %q", b.Contract.Items["Outputs"])
	}
	// Compliant Contract, nothing unsafe, and both official eval formats present.
	if b.Tier != 3 {
		t.Errorf("tier = %d, want 3 (findings: %+v)", b.Tier, b.Findings)
	}
	if byKey["px:querying-warehouse"].Tier != 0 {
		t.Errorf("a skill with no Contract must stay at tier 0")
	}
}

func TestTierRequiresBothEvalFormats(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "plugins/px/skills/building-report/SKILL.md"),
		"---\nname: building-report\ndescription: "+goodDesc+"\n---\n\n"+goodContract+"\n")
	// Output-quality eval only: trigger accuracy is unproven, so tier stays at 2.
	write(t, filepath.Join(root, "plugins/px/skills/building-report/evals/evals.json"),
		`{"skill_name":"building-report","evals":[{"id":1,"prompt":"p","expected_output":"o"}]}`)
	m, err := Build([]string{root}, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if m.Skills[0].Tier != 2 {
		t.Errorf("tier = %d, want 2 without eval_queries.json", m.Skills[0].Tier)
	}
}

func TestTierFallsBackWhenRecordedEvalsFail(t *testing.T) {
	// Carrying both eval files proves the cases exist, not that they pass. A recorded run
	// with a failure is the one case where "verified" would be a lie.
	both := func(root, name string) {
		write(t, filepath.Join(root, "plugins/px/skills", name, "SKILL.md"),
			"---\nname: "+name+"\ndescription: "+goodDesc+"\n---\n\n"+goodContract+"\n")
		write(t, filepath.Join(root, "plugins/px/skills", name, "evals/evals.json"),
			`{"skill_name":"`+name+`","evals":[{"id":1,"prompt":"p","expected_output":"o"}]}`)
		write(t, filepath.Join(root, "plugins/px/skills", name, "evals/eval_queries.json"),
			`[{"query":"q","should_trigger":true}]`)
	}
	root := t.TempDir()
	both(root, "building-report")
	both(root, "building-summary")
	both(root, "building-digest")
	write(t, filepath.Join(root, "plugins/px/skills/building-summary/evals/results.json"),
		`{"ran_at":"2026-09-03","evals":[{"id":1,"pass":false}]}`)
	write(t, filepath.Join(root, "plugins/px/skills/building-digest/evals/results.json"),
		`{"ran_at":"2026-09-03","evals":[{"id":1,"pass":true}]}`)

	m, err := Build([]string{root}, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]Entry{}
	for _, e := range m.Skills {
		byKey[e.Key] = e
	}
	if got := byKey["px:building-report"]; got.Tier != 3 || got.EvalResult != "" {
		t.Errorf("no results file must leave the tier alone: tier=%d result=%q", got.Tier, got.EvalResult)
	}
	if got := byKey["px:building-summary"]; got.Tier != 2 || got.EvalResult != "failing" {
		t.Errorf("a failing run must cap the tier at 2: tier=%d result=%q", got.Tier, got.EvalResult)
	}
	if got := byKey["px:building-digest"]; got.Tier != 3 || got.EvalResult != "passed" {
		t.Errorf("a passing run stays verified: tier=%d result=%q", got.Tier, got.EvalResult)
	}
}

// writeBase writes a base snapshot for the --base tests.
func writeBase(t *testing.T, snap lint.Snapshot) string {
	t.Helper()
	raw, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "base.json")
	write(t, p, string(raw))
	return p
}

func TestAtlasWithoutABaseCarriesNoDiff(t *testing.T) {
	m, err := Build([]string{corpus(t)}, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if m.Diff != nil {
		t.Errorf("no base was given, so the model must carry no diff: %+v", m.Diff)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"diff"`) {
		t.Error("the payload must omit the diff key entirely when no base was given")
	}
	for _, e := range m.Skills {
		if e.DiffNew || e.DiffAddedErrors != 0 || e.DiffResolved != 0 {
			t.Errorf("%s carries diff data without a base: %+v", e.Key, e)
		}
	}
}

func TestAtlasDiffAgainstABase(t *testing.T) {
	root := t.TempDir()
	sk := func(name, body string) {
		write(t, filepath.Join(root, "plugins/px/skills", name, "SKILL.md"),
			"---\nname: "+name+"\ndescription: "+goodDesc+"\n---\n\n"+body+"\n")
	}
	sk("steady-skill", goodContract)
	// A Contract missing Non-goals is an error whatever the baseline says, which gives the
	// delta an added error to attribute.
	sk("half-contract", "## Contract\n- **Trigger**: t\n- **Inputs**: i\n"+
		"- **Preconditions**: p\n- **Outputs**: o\n- **Postconditions**: q\n")

	base := writeBase(t, lint.Snapshot{
		// half-contract is absent here, so the head sees it as new; retired-skill is the
		// other way round.
		Skills: []string{"px:steady-skill", "px:retired-skill"},
		Findings: []lint.Finding{
			{Rule: "L10", Severity: "error", Skill: "px:steady-skill",
				File: "x.zip", Line: 1, Message: "a build artifact that has since been removed"},
		},
	})

	m, err := Build([]string{root}, "", "", base)
	if err != nil {
		t.Fatal(err)
	}
	if m.Diff == nil {
		t.Fatal("expected a diff")
	}
	byKey := map[string]Entry{}
	for _, e := range m.Skills {
		byKey[e.Key] = e
	}
	if !byKey["px:half-contract"].DiffNew {
		t.Error("a skill absent from the base snapshot is new")
	}
	if byKey["px:steady-skill"].DiffNew {
		t.Error("a skill present in the base is not new")
	}
	if !contains(m.Diff.NewSkills, "px:half-contract") ||
		!contains(m.Diff.RemovedSkills, "px:retired-skill") {
		t.Errorf("new/removed = %v / %v", m.Diff.NewSkills, m.Diff.RemovedSkills)
	}
	if m.Diff.Resolved != 1 {
		t.Errorf("the base's L10 finding is gone from the head, so resolved = %d, want 1",
			m.Diff.Resolved)
	}
	if m.Diff.AddedErrors == 0 || byKey["px:half-contract"].DiffAddedErrors == 0 {
		t.Fatalf("the missing Non-goals should register as an added error: %+v", m.Diff)
	}
	sum := 0
	for _, e := range m.Skills {
		sum += e.DiffAddedErrors
	}
	if sum != m.Diff.AddedErrors {
		t.Errorf("per-skill added errors sum to %d, header says %d", sum, m.Diff.AddedErrors)
	}
}

func TestAtlasBaseWithoutASkillListSaysSo(t *testing.T) {
	// A snapshot from an older mekiki has no skills list. Reporting every skill as new would
	// be a guess; the page says the information is missing instead.
	base := writeBase(t, lint.Snapshot{Findings: []lint.Finding{}})
	m, err := Build([]string{corpus(t)}, "", "", base)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range m.Skills {
		if e.DiffNew {
			t.Errorf("%s must not be reported as new when the base lists no skills", e.Key)
		}
	}
	if !anyNote(m.Notes, "skills list") {
		t.Errorf("expected a note about the missing skills list, got %v", m.Notes)
	}
}

func TestAtlasRejectsAnUnreadableBase(t *testing.T) {
	p := filepath.Join(t.TempDir(), "base.json")
	write(t, p, "{not json")
	if _, err := Build([]string{corpus(t)}, "", "", p); err == nil {
		t.Error("a malformed base snapshot must be an error, not a silently empty diff")
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func anyNote(notes []string, sub string) bool {
	for _, n := range notes {
		if strings.Contains(n, sub) {
			return true
		}
	}
	return false
}

func TestRenderIsSafeAgainstAHostileCorpus(t *testing.T) {
	// The threat model is a third-party skill tree: mekiki's own conventions tell people to
	// read such a corpus before trusting it, and the Atlas is precisely the page a reviewer
	// opens to do that reading — so the page must stay inert whatever the corpus contains.
	//
	// Directory names reach the payload verbatim (a POSIX directory name may contain quotes
	// and parentheses), and descriptions are arbitrary text. Two properties keep that safe:
	// Go's json.Marshal escapes <, > and & by default, so no payload string can close the
	// page's script tag; and the template never interpolates a string literal into an inline
	// handler, so there is no quoted JS context to break out of.
	root := t.TempDir()
	dir := filepath.Join(root, "plugins", `p');alert(1);//`, "skills", "breaking-out")
	write(t, filepath.Join(dir, "SKILL.md"),
		"---\nname: breaking-out\ndescription: </script><script>alert(1)</script> "+goodDesc+"\n---\n\n"+goodContract+"\n")

	m, err := Build([]string{root}, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	html, err := Render(m)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "<script>alert(1)") {
		t.Error("a description must not be able to close the page's script tag")
	}

	// The template-side invariant: no inline handler receives a string literal built by
	// interpolation. esc() is an HTML escaper, and the browser decodes entities in an
	// attribute before the JS parser runs, so onclick="fn('${esc(x)}')" is exactly the
	// pattern that reopens the hole this test exists to keep shut.
	tpl, err := templateFS.ReadFile("template.html")
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(tpl), "\n") {
		if strings.Contains(line, "onclick") && strings.Contains(line, "('${") {
			t.Errorf("template.html:%d interpolates a string into an inline handler: %s",
				i+1, strings.TrimSpace(line))
		}
	}
}

func TestHeuristicIOForPreContractSkills(t *testing.T) {
	q := build(t)["px:querying-warehouse"]
	if len(q.Heuristic) == 0 {
		t.Fatal("no inferred I/O for a skill without a Contract")
	}
	joined := strings.Join(q.Heuristic["Inputs"], " ")
	if !strings.Contains(joined, "account_id") {
		t.Errorf("inferred inputs = %v", q.Heuristic)
	}
}

func TestRefFormatsComposition(t *testing.T) {
	got := build(t)["px:querying-warehouse"].RefFormats
	want := map[string]int{"md": 1, "yaml": 1, "jsonl": 1, "sql": 1}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("refFormats[%s] = %d, want %d (all: %v)", k, got[k], v, got)
		}
	}
	if len(build(t)["px:guard-skill"].RefFormats) != 0 {
		t.Error("a skill without references/ should report no formats")
	}
}

func TestDeclaredFlow(t *testing.T) {
	e := build(t)["px:running-flow"]
	if !e.IsOrchestrator || e.FlowSource != "declared" {
		t.Fatalf("flow = %+v source = %q", e.Flow, e.FlowSource)
	}
	if len(e.Flow) != 2 || e.Flow[0].Skill != "querying-warehouse" {
		t.Errorf("flow = %+v", e.Flow)
	}
	if len(e.Flow[1].Inputs) != 1 || e.Flow[1].Inputs[0] != "01_ctx.json" {
		t.Errorf("phase 2 inputs = %v", e.Flow[1].Inputs)
	}
}

func TestInferredFlowFromPhaseRegistry(t *testing.T) {
	e := build(t)["px:legacy-flow"]
	if !e.IsOrchestrator || e.FlowSource != "heuristic" {
		t.Fatalf("flow = %+v source = %q", e.Flow, e.FlowSource)
	}
	if len(e.Flow) != 2 {
		t.Errorf("flow = %+v", e.Flow)
	}
}

func TestInferredFlowResolvesQualifiedNames(t *testing.T) {
	e := build(t)["px:qualified-flow"]
	if !e.IsOrchestrator {
		t.Fatalf("a qualified-name Phase Registry was not recognised: %+v", e.Flow)
	}
	if len(e.Flow) != 2 || e.Flow[0].Skill != "querying-warehouse" {
		t.Errorf("flow = %+v", e.Flow)
	}
}

func TestNonOrchestratorHasNoFlow(t *testing.T) {
	e := build(t)["px:guard-skill"]
	if e.IsOrchestrator || len(e.Flow) != 0 {
		t.Errorf("flow = %+v", e.Flow)
	}
}

func TestRenderInlinesPayload(t *testing.T) {
	m, err := Build([]string{corpus(t)}, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	html, err := Render(m)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "/*__DATA__*/null") {
		t.Error("the payload placeholder was not replaced")
	}
	if !strings.Contains(html, "querying-warehouse") {
		t.Error("skill data is missing from the rendered page")
	}
	// All three views and the legend must ship: the tabs are the page's only navigation,
	// so a template edit that drops one leaves the corresponding data unreachable rather
	// than broken, and without the legend the tier and contract marks are unreadable.
	for _, id := range []string{"view-board", "view-flow", "view-cat", "legend"} {
		if !strings.Contains(html, `id="`+id+`"`) {
			t.Errorf("the rendered page is missing the %s section", id)
		}
	}
	// The page must stay self-contained: no external fetches.
	for _, bad := range []string{"src=\"http", "href=\"http://", "cdn."} {
		if strings.Contains(html, bad) {
			t.Errorf("page references an external resource: %s", bad)
		}
	}
	// And the inlined payload must be valid JSON.
	start := strings.Index(html, "const DATA = ")
	if start < 0 {
		t.Fatal("payload assignment not found")
	}
	rest := html[start+len("const DATA = "):]
	end := strings.Index(rest, ";\n")
	if end < 0 {
		t.Fatal("payload terminator not found")
	}
	var probe Model
	if err := json.Unmarshal([]byte(rest[:end]), &probe); err != nil {
		t.Fatalf("inlined payload is not valid JSON: %v", err)
	}
}

func containsStr(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
