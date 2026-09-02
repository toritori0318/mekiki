package lint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixtures below are reduced copies of defects observed in a real corpus during the
// audit that produced these conventions. Names are neutralised.

const goodDesc = "Drafts the weekly plan for an assigned account. " +
	"Use this when the user asks for a weekly plan or a draft of next week's schedule, " +
	"or when preparing for the regular review meeting. " +
	"If the account state has not been set up yet, use setting-up-account-state first. " +
	"If the goal is only to inspect schedule deltas, use reviewing-schedule-delta instead."

const goodContract = "## Contract\n" +
	"- **Trigger**: the user asks for a weekly plan draft.\n" +
	"- **Inputs**: required: account_id (given by the user). optional: target week.\n" +
	"- **Preconditions**: the account state directory exists.\n" +
	"- **Outputs**: out/<account>/drafting-weekly-plan/{YYYYMMDD}_plan/01_plan.md\n" +
	"- **Postconditions**: 01_plan.md exists.\n" +
	"- **Non-goals**: does not inspect schedule deltas (delegated to reviewing-schedule-delta).\n"

type corpus struct {
	root string
	t    *testing.T
}

func newCorpus(t *testing.T) *corpus {
	return &corpus{root: t.TempDir(), t: t}
}

func (c *corpus) write(rel, content string) {
	c.t.Helper()
	p := filepath.Join(c.root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		c.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		c.t.Fatal(err)
	}
}

func (c *corpus) skill(plugin, name, desc, body, extraFM string) {
	c.write(filepath.Join("plugins", plugin, "skills", name, "SKILL.md"),
		"---\nname: "+name+"\ndescription: "+desc+"\n"+extraFM+"---\n\n"+body+"\n")
}

// run evaluates the corpus. baseline == "" means "no baseline", i.e. everything is
// pre-existing; pass "empty" to freeze nothing so that every skill counts as new.
func (c *corpus) run(baseline string, cfg map[string]any) *Result {
	c.t.Helper()
	opts := Options{Paths: []string{c.root}}
	if baseline == "empty" {
		p := filepath.Join(c.root, "baseline.json")
		if err := os.WriteFile(p, []byte(`{"skills":[]}`), 0o644); err != nil {
			c.t.Fatal(err)
		}
		opts.BaselinePath = p
	}
	if cfg != nil {
		raw, _ := json.Marshal(cfg)
		p := filepath.Join(c.root, "config.json")
		if err := os.WriteFile(p, raw, 0o644); err != nil {
			c.t.Fatal(err)
		}
		opts.ConfigPath = p
	}
	res, err := Run(opts)
	if err != nil {
		c.t.Fatal(err)
	}
	return res
}

func pick(res *Result, rule, skillKey string) []Finding {
	var out []Finding
	for _, f := range res.Findings {
		if f.Rule == rule && (skillKey == "" || f.Skill == skillKey) {
			out = append(out, f)
		}
	}
	return out
}

func TestL1OfficialNameConstraints(t *testing.T) {
	c := newCorpus(t)
	c.skill("new", "MySkill_v2", "Dummy violating the official name constraints.", "body", "")
	c.skill("new", "pdf--processing", "Dummy with consecutive hyphens.", "body", "")
	// A four-word non-gerund name is officially valid and must not be flagged.
	c.skill("new", "cloud-ads-account-setup", goodDesc, goodContract, "")
	res := c.run("empty", nil)

	for _, name := range []string{"MySkill_v2", "pdf--processing"} {
		fs := pick(res, "L1", "new:"+name)
		if len(fs) == 0 || fs[0].Severity != Error {
			t.Errorf("%s should be an L1 error, got %+v", name, fs)
		}
	}
	if fs := pick(res, "L1", "new:cloud-ads-account-setup"); len(fs) != 0 {
		t.Errorf("officially valid name was flagged: %+v", fs)
	}
}

func TestL1NameDirectoryMismatch(t *testing.T) {
	c := newCorpus(t)
	c.write("plugins/p/skills/mismatched-dir/SKILL.md",
		"---\nname: some-other-name\ndescription: Dummy.\n---\n\nbody\n")
	fs := pick(c.run("empty", nil), "L1", "p:mismatched-dir")
	if len(fs) == 0 || !strings.Contains(fs[0].Message, "does not match directory name") {
		t.Errorf("mismatch not detected: %+v", fs)
	}
}

func TestL2LengthTriggerAndListingCap(t *testing.T) {
	c := newCorpus(t)
	c.skill("p", "enriching-product-page",
		"Supports research and content design for product detail pages.", "body", "")
	longWTU := strings.Repeat("Describes an activation situation. ", 60)
	c.skill("p", "overflowing-listing", goodDesc, "body", "when_to_use: "+longWTU+"\n")
	res := c.run("", nil)

	fs := pick(res, "L2", "p:enriching-product-page")
	var sawLen, sawTrigger bool
	for _, f := range fs {
		if strings.Contains(f.Message, "characters (convention") {
			sawLen = true
		}
		if strings.Contains(f.Message, "no trigger condition") {
			sawTrigger = true
		}
	}
	if !sawLen || !sawTrigger {
		t.Errorf("expected both length and trigger findings, got %+v", fs)
	}
	for _, f := range fs {
		if f.Severity != Warn {
			t.Errorf("pre-existing skills should warn, got %s", f.Severity)
		}
	}
	if fs := pick(res, "L2", "p:overflowing-listing"); !anyContains(fs, "truncates") {
		t.Errorf("listing cap overflow not reported: %+v", fs)
	}
}

func TestL2TriggerCountsWhenToUse(t *testing.T) {
	c := newCorpus(t)
	// No trigger wording in the description, but when_to_use supplies it. Claude Code
	// concatenates the two, so the combination is what matters.
	desc := "Formats an initiative list and drafts the weekly summary. " +
		"Data retrieval is delegated to querying-warehouse; this skill only formats and writes prose. " +
		"For deck styling, deck is the appropriate skill instead of this one. " +
		"This fixture exists to verify combined trigger detection and is not a real skill."
	c.skill("p", "triggering-via-listing", desc, "body",
		"when_to_use: Use this when the user asks for a weekly summary.\n")
	fs := pick(c.run("", nil), "L2", "p:triggering-via-listing")
	if anyContains(fs, "no trigger condition") {
		t.Errorf("trigger wording in when_to_use was ignored: %+v", fs)
	}
}

func TestProfileGenericDropsClaudeCodeSpecifics(t *testing.T) {
	// The listing cap and the extension key set are Claude Code behaviours, not rules of the
	// Agent Skills specification. On a runtime that implements the specification and nothing
	// more, `when_to_use` really is a non-standard key and nothing concatenates it onto the
	// description, so there is no cap to overflow.
	c := newCorpus(t)
	longWTU := strings.Repeat("Describes an activation situation. ", 60)
	c.skill("p", "overflowing-listing", goodDesc, "body", "when_to_use: "+longWTU+"\n")

	def := c.run("", nil)
	if fs := pick(def, "L2", "p:overflowing-listing"); !anyContains(fs, "truncates") {
		t.Fatalf("the default profile must still report the overflow: %+v", fs)
	}
	if fs := pick(def, "L12", "p:overflowing-listing"); anyContains(fs, "when_to_use") {
		t.Fatalf("when_to_use is official under claude-code: %+v", fs)
	}

	gen := c.run("", map[string]any{"profile": "generic"})
	if fs := pick(gen, "L2", "p:overflowing-listing"); anyContains(fs, "truncates") {
		t.Errorf("generic has no listing cap, so no overflow can be reported: %+v", fs)
	}
	if fs := pick(gen, "L12", "p:overflowing-listing"); !anyContains(fs, "when_to_use") {
		t.Errorf("under generic, a Claude Code extension is non-standard: %+v", fs)
	}
}

func TestProfileLimitsOverrideAndUnknownProfileFallsBack(t *testing.T) {
	c := newCorpus(t)
	c.skill("p", "drafting-weekly-plan", goodDesc, goodContract, "")

	tight := c.run("", map[string]any{"limits": map[string]int{"max_body_tokens": 10}})
	if fs := pick(tight, "L19", "p:drafting-weekly-plan"); !anyContains(fs, "estimated tokens") {
		t.Errorf("an explicit limit must bind: %+v", fs)
	}
	// An explicit limits entry wins over the profile's value, in either direction.
	if fs := pick(c.run("", nil), "L19", "p:drafting-weekly-plan"); len(fs) != 0 {
		t.Errorf("the default limits leave a small skill alone: %+v", fs)
	}

	bad := c.run("", map[string]any{"profile": "no-such-runtime"})
	var sawNote bool
	for _, n := range bad.Notes {
		if strings.Contains(n, "unknown profile") {
			sawNote = true
		}
	}
	if !sawNote {
		t.Errorf("an unknown profile should produce a note: %v", bad.Notes)
	}
	if fs := pick(bad, "L12", "p:drafting-weekly-plan"); len(fs) != 0 {
		t.Errorf("an unknown profile must fall back to claude-code, not to nothing: %+v", fs)
	}
}

func TestL3LifecycleWording(t *testing.T) {
	c := newCorpus(t)
	c.skill("p", "legacy-sheet-setup",
		"DEPRECATED spreadsheet approach, replaced by sheet-install.", "body", "")
	fs := pick(c.run("", nil), "L3", "p:legacy-sheet-setup")
	if len(fs) == 0 || fs[0].Severity != Error {
		t.Errorf("lifecycle wording should be an error: %+v", fs)
	}
}

func TestL4ContractCompliantPasses(t *testing.T) {
	c := newCorpus(t)
	c.skill("p", "drafting-weekly-plan", goodDesc, goodContract, "")
	if fs := pick(c.run("empty", nil), "L4", "p:drafting-weekly-plan"); len(fs) != 0 {
		t.Errorf("a compliant Contract must produce no findings: %+v", fs)
	}
}

func TestL4NonGoalsMissingIsErrorEvenForExisting(t *testing.T) {
	c := newCorpus(t)
	body := "## Contract\n- **Trigger**: t\n- **Inputs**: i\n- **Preconditions**: p\n" +
		"- **Outputs**: o\n- **Postconditions**: q\n"
	c.skill("p", "half-contract", goodDesc, body, "")
	fs := pick(c.run("", nil), "L4", "p:half-contract")
	var nonGoals *Finding
	for i := range fs {
		if strings.Contains(fs[i].Message, "Non-goals") {
			nonGoals = &fs[i]
		}
	}
	if nonGoals == nil || nonGoals.Severity != Error {
		t.Errorf("missing Non-goals must be an error even for pre-existing skills: %+v", fs)
	}
}

func TestL5SingularReferenceDir(t *testing.T) {
	c := newCorpus(t)
	c.skill("p", "querying-warehouse", goodDesc, "See reference/ for details.", "")
	c.write("plugins/p/skills/querying-warehouse/reference/guide.md", "guide\n")
	fs := pick(c.run("", nil), "L5", "p:querying-warehouse")
	if len(fs) == 0 || fs[0].Severity != Warn {
		t.Errorf("reference/ (singular) should warn for pre-existing skills: %+v", fs)
	}
}

func TestL6DeterministicProseWithoutScripts(t *testing.T) {
	c := newCorpus(t)
	c.skill("p", "flash-sale", goodDesc,
		"## Measurement\n\nCompute sale revenue divided by baseline revenue and "+
			"evaluate against the 1.3x threshold.\n", "")
	fs := pick(c.run("", nil), "L6", "p:flash-sale")
	if len(fs) == 0 || fs[0].Severity != Warn {
		t.Errorf("deterministic prose without scripts/ should warn: %+v", fs)
	}
}

func TestL6SuppressedWithReason(t *testing.T) {
	c := newCorpus(t)
	c.skill("p", "calculating-impact", goodDesc,
		"<!-- mekiki: disable L6 -- explains judgement criteria; the calculation lives in scripts/calc.py -->\n\n"+
			"Compute the threshold when reasoning about stock days.\n", "")
	if fs := pick(c.run("", nil), "L6", "p:calculating-impact"); len(fs) != 0 {
		t.Errorf("a suppression with a reason must silence the rule: %+v", fs)
	}
}

func TestSuppressionWithoutReasonIsIgnored(t *testing.T) {
	c := newCorpus(t)
	c.skill("p", "unreasoned-suppress", goodDesc,
		"<!-- mekiki: disable L6 -->\n\nCompute the threshold.\n", "")
	if fs := pick(c.run("", nil), "L6", "p:unreasoned-suppress"); len(fs) == 0 {
		t.Error("a suppression without a reason must be invalid")
	}
}

func TestL7DuplicateReferencesAcrossPlugins(t *testing.T) {
	c := newCorpus(t)
	shared := strings.Repeat("Shared knowledge body for the growth model overview.\n", 200)
	for _, p := range []string{"alpha", "gamma"} {
		c.skill(p, "knowledge-shared-master",
			"Shared knowledge. Other skills invoke this via the Skill tool when they need it. "+
				"Use this when a skill needs the shared master data. "+
				"Do not use it for anything outside that shared master.",
			"See references/ for the body.", "user-invocable: false\n")
		c.write(filepath.Join("plugins", p, "skills/knowledge-shared-master/references/master.md"), shared)
	}
	res := c.run("", nil)
	for _, p := range []string{"alpha", "gamma"} {
		fs := pick(res, "L7", p+":knowledge-shared-master")
		if len(fs) == 0 || fs[0].Severity != Error {
			t.Errorf("%s: identical references should be an error: %+v", p, fs)
		}
	}
}

func TestL7GeneratedBannerIsWarn(t *testing.T) {
	c := newCorpus(t)
	banner := "<!-- AUTO-GENERATED from master/. Do not edit manually.\n" +
		"     Regenerate with: mekiki sync -->\n"
	shared := banner + strings.Repeat("Stamped shared knowledge body.\n", 300)
	for i, p := range []string{"alpha", "gamma"} {
		name := []string{"knowledge-stamped-copy", "knowledge-stamped-target"}[i]
		c.skill(p, name, goodDesc, "See references/.", "user-invocable: false\n")
		c.write(filepath.Join("plugins", p, "skills", name, "references/master.md"), shared)
	}
	res := c.run("", nil)
	fs := pick(res, "L7", "")
	if len(fs) == 0 {
		t.Fatal("expected L7 findings")
	}
	for _, f := range fs {
		if f.Severity != Warn {
			t.Errorf("a generated banner should downgrade to warn: %+v", f)
		}
	}
}

func TestL8BillingRequiresConfiguredGuard(t *testing.T) {
	c := newCorpus(t)
	c.skill("alpha", "paid-ads-setup",
		"Sets up paid ads campaigns. Use this when the user asks to launch ads. "+
			"For creative production use another skill instead of this one. "+
			"This dummy exists to exercise the billing guard rule and is padded to length.",
		"## Steps\n\n1. Set the ad spend budget in the business manager.\n", "")
	cfg := map[string]any{"guards": map[string][]string{"billing": {"billing-guard"}}}

	if fs := pick(c.run("", cfg), "L8", "alpha:paid-ads-setup"); len(fs) == 0 || fs[0].Severity != Error {
		t.Errorf("billing wording without the configured guard should error: %+v", fs)
	}
	// Without config, the guard name is unknown, so the check must not fire.
	if fs := pick(c.run("", nil), "L8", "alpha:paid-ads-setup"); len(fs) != 0 {
		t.Errorf("org-specific guards must not be demanded without config: %+v", fs)
	}
}

func TestL8PublishGateIsOrgNeutral(t *testing.T) {
	c := newCorpus(t)
	c.skill("alpha", "publishing-store-page",
		"Publishes a store page. Use this when the user asks to publish a page. "+
			"Content authoring is out of scope for this skill and belongs elsewhere. "+
			"This dummy is padded so the description length rule stays quiet.",
		"## Steps\n\n1. Publish the page.\n", "")
	for _, cfg := range []map[string]any{nil, {"guards": map[string][]string{"billing": {"billing-guard"}}}} {
		fs := pick(c.run("", cfg), "L8", "alpha:publishing-store-page")
		if len(fs) == 0 || fs[0].Severity != Error {
			t.Errorf("the publish gate must fire regardless of config: %+v", fs)
		}
	}
}

func TestL9ScriptsWithoutTests(t *testing.T) {
	c := newCorpus(t)
	c.skill("alpha", "signup-form-setup", goodDesc, "Run scripts/generate_form.py.", "")
	c.write("plugins/alpha/skills/signup-form-setup/scripts/generate_form.py", "print('x')\n")
	fs := pick(c.run("", nil), "L9", "alpha:signup-form-setup")
	if len(fs) == 0 || fs[0].Severity != Warn {
		t.Errorf("scripts without tests should warn for pre-existing skills: %+v", fs)
	}
}

func TestL10BuildArtifactInTree(t *testing.T) {
	c := newCorpus(t)
	c.skill("alpha", "drafting-weekly-plan", goodDesc, goodContract, "")
	c.write("plugins/legacy-bundle.zip", "PK\x03\x04 dummy")
	fs := pick(c.run("", nil), "L10", "")
	if len(fs) == 0 || fs[0].Severity != Error {
		t.Errorf("a build artifact in the tree should error: %+v", fs)
	}
	if !strings.Contains(fs[0].File, "legacy-bundle.zip") {
		t.Errorf("unexpected file: %s", fs[0].File)
	}
}

func TestL11InternalWithoutFlag(t *testing.T) {
	c := newCorpus(t)
	c.skill("delta", "resolving-queue-tasks",
		"Internal skill. Invoked by other skills to resolve the task queue and return JSON. "+
			"Use this only from other skills. Humans should not invoke it directly. "+
			"This dummy is padded so the length rule stays quiet.",
		"Returns JSON.", "")
	fs := pick(c.run("", nil), "L11", "delta:resolving-queue-tasks")
	if len(fs) == 0 || fs[0].Severity != Error {
		t.Errorf("internal skill without user-invocable: false should error: %+v", fs)
	}
}

func TestL12NonStandardKeyButOfficialKeysPass(t *testing.T) {
	c := newCorpus(t)
	c.skill("p", "flash-sale", goodDesc, "body", "when_to_use: at planning time\nx-note: dev memo\n")
	c.skill("p", "rendering-charts", goodDesc, "body",
		"argument-hint: '[csv]'\narguments: [csvfile]\nmodel: sonnet\neffort: low\n"+
			"context: fork\nagent: general-purpose\nbackground: false\n"+
			"paths: ['**/*.csv']\nshell: bash\ncompatibility: Designed for Claude Code\n"+
			"disallowed-tools: AskUserQuestion\ndisable-model-invocation: true\n")
	res := c.run("", nil)

	fs := pick(res, "L12", "p:flash-sale")
	if !anyContains(fs, "x-note") {
		t.Errorf("a genuinely non-standard key should warn: %+v", fs)
	}
	if anyContains(fs, "when_to_use") {
		t.Errorf("when_to_use is an official key and must not be flagged: %+v", fs)
	}
	if fs := pick(res, "L12", "p:rendering-charts"); len(fs) != 0 {
		t.Errorf("official Claude Code keys must not be flagged: %+v", fs)
	}
}

func TestL13OfficialEvalFormats(t *testing.T) {
	c := newCorpus(t)
	c.skill("p", "diagnosing-usage-health", goodDesc, goodContract, "")
	c.write("plugins/p/skills/diagnosing-usage-health/evals/evals.json",
		`{"skill_name":"diagnosing-usage-health","notes":"memo",
		  "evals":[{"id":1,"prompt":"check health","expected_output":"a report","files":[],"assertions":["ok"]}]}`)
	c.write("plugins/p/skills/diagnosing-usage-health/evals/eval_queries.json",
		`[{"query":"check the health","should_trigger":true},{"query":"convert json to yaml","should_trigger":false}]`)

	c.skill("p", "checking-eval-schema", goodDesc, goodContract, "")
	c.write("plugins/p/skills/checking-eval-schema/evals/evals.json",
		`{"skill_name":"checking-eval-schema","evals":[{"id":1,"prompt":"do it"}]}`)

	c.skill("p", "checking-trigger-eval", goodDesc, goodContract, "")
	c.write("plugins/p/skills/checking-trigger-eval/evals/eval_queries.json",
		`[{"query":"do it","should_trigger":"yes"}]`)

	c.skill("p", "legacy-eval-format", goodDesc, goodContract, "")
	c.write("plugins/p/skills/legacy-eval-format/evals/cases.json",
		`{"schema_version":"1.0","skill":"p:legacy-eval-format","cases":[]}`)

	c.skill("p", "odd-eval-name", goodDesc, goodContract, "")
	c.write("plugins/p/skills/odd-eval-name/evals/skip_schema_check_regression.json", "{}\n")

	res := c.run("", nil)
	if fs := pick(res, "L13", "p:diagnosing-usage-health"); len(fs) != 0 {
		t.Errorf("both official formats must pass cleanly: %+v", fs)
	}
	if fs := pick(res, "L13", "p:checking-eval-schema"); !anyContains(fs, "expected_output") {
		t.Errorf("missing expected_output should error: %+v", fs)
	}
	if fs := pick(res, "L13", "p:checking-trigger-eval"); !anyContains(fs, "should_trigger") {
		t.Errorf("non-boolean should_trigger should error: %+v", fs)
	}
	if fs := pick(res, "L13", "p:legacy-eval-format"); !anyContains(fs, "legacy proprietary format") {
		t.Errorf("cases.json should prompt migration: %+v", fs)
	}
	if fs := pick(res, "L13", "p:odd-eval-name"); !anyContains(fs, "non-standard eval file") {
		t.Errorf("an unknown eval filename should warn: %+v", fs)
	}
}

func TestL25EvalResults(t *testing.T) {
	c := newCorpus(t)
	mk := func(name, results string) {
		c.skill("p", name, goodDesc, goodContract, "")
		if results != "" {
			c.write(filepath.Join("plugins/p/skills", name, "evals/results.json"), results)
		}
	}
	mk("running-no-results", "")
	mk("running-all-pass",
		`{"ran_at":"2026-09-03","evals":[{"id":"case-1","pass":true},{"id":"case-2","pass":true}]}`)
	mk("running-with-failures",
		`{"ran_at":"2026-09-03","evals":[{"id":"case-1","pass":true},{"id":"case-2","pass":false}]}`)
	mk("running-queries-only", `{"ran_at":"2026-09-03","queries":{"total":20,"correct":18}}`)
	mk("running-broken-json", `{"evals":[`)
	mk("running-bad-schema", `{"evals":[{"id":"case-1"}]}`)
	mk("running-empty-report", `{"ran_at":"2026-09-03"}`)

	res := c.run("", nil)
	for _, n := range []string{"running-no-results", "running-all-pass", "running-queries-only"} {
		if fs := pick(res, "L25", "p:"+n); len(fs) != 0 {
			t.Errorf("%s should produce no L25 finding: %+v", n, fs)
		}
	}
	fs := pick(res, "L25", "p:running-with-failures")
	if len(fs) == 0 || fs[0].Severity != Warn {
		t.Errorf("a failing case should warn: %+v", fs)
	}
	if !anyContains(fs, "1 of 2") {
		t.Errorf("the message should count the failures: %+v", fs)
	}
	for _, n := range []string{"running-broken-json", "running-bad-schema", "running-empty-report"} {
		fs := pick(res, "L25", "p:"+n)
		if len(fs) == 0 || fs[0].Severity != Error {
			t.Errorf("%s: a results file that exists must be well formed: %+v", n, fs)
		}
	}
}

func TestL14SameNameWithoutCanonical(t *testing.T) {
	c := newCorpus(t)
	for _, p := range []string{"alpha", "gamma"} {
		c.skill(p, "knowledge-shared-master", goodDesc, "body", "user-invocable: false\n")
	}
	res := c.run("", nil)
	for _, p := range []string{"alpha", "gamma"} {
		if fs := pick(res, "L14", p+":knowledge-shared-master"); len(fs) == 0 {
			t.Errorf("%s: duplicate name without canonical should error", p)
		}
	}
}

func TestFlowRules(t *testing.T) {
	c := newCorpus(t)
	c.skill("p", "collecting-context", goodDesc, goodContract, "")
	c.skill("p", "drafting-strategy", goodDesc, goodContract, "")

	c.skill("p", "running-good-flow", goodDesc, goodContract, "")
	c.write("plugins/p/skills/running-good-flow/flow.json",
		`{"flow":[{"phase":1,"skill":"collecting-context","inputs":["target_id"],"outputs":["01_ctx.json"]},`+
			`{"phase":2,"skill":"drafting-strategy","inputs":["01_ctx.json"],"outputs":["02_plan.json"]}]}`)

	c.skill("p", "running-bad-schema", goodDesc, goodContract, "")
	c.write("plugins/p/skills/running-bad-schema/flow.json",
		`{"flow":[{"skill":"drafting-strategy","inputs":[],"outputs":[]}]}`)

	c.skill("p", "running-bad-ref", goodDesc, goodContract, "")
	c.write("plugins/p/skills/running-bad-ref/flow.json",
		`{"flow":[{"phase":1,"skill":"no-such-skill","inputs":[],"outputs":["x.json"]}]}`)

	c.skill("p", "running-bad-order", goodDesc, goodContract, "")
	c.write("plugins/p/skills/running-bad-order/flow.json",
		`{"flow":[{"phase":1,"skill":"collecting-context","inputs":["99_late.json"],"outputs":["01_a.json"]},`+
			`{"phase":2,"skill":"drafting-strategy","inputs":["01_a.json"],"outputs":["99_late.json"]}]}`)

	res := c.run("", nil)
	for _, rule := range []string{"L15", "L16", "L17"} {
		if fs := pick(res, rule, "p:running-good-flow"); len(fs) != 0 {
			t.Errorf("%s fired on a valid flow: %+v", rule, fs)
		}
		if fs := pick(res, rule, "p:collecting-context"); len(fs) != 0 {
			t.Errorf("%s fired on a skill with no flow.json: %+v", rule, fs)
		}
	}
	if fs := pick(res, "L15", "p:running-bad-schema"); len(fs) == 0 || fs[0].Severity != Error {
		t.Errorf("invalid flow schema should error: %+v", fs)
	}
	if fs := pick(res, "L16", "p:running-bad-ref"); !anyContains(fs, "no-such-skill") {
		t.Errorf("a missing delegation target should error: %+v", fs)
	}
	if fs := pick(res, "L17", "p:running-bad-order"); len(fs) == 0 || fs[0].Severity != Warn {
		t.Errorf("an ordering violation should warn: %+v", fs)
	}
}

func TestL24UnconsumedIntermediateArtifact(t *testing.T) {
	c := newCorpus(t)
	for _, n := range []string{"collecting-context", "drafting-strategy", "publishing-result"} {
		c.skill("p", n, goodDesc, goodContract, "")
	}

	// The middle phase writes two files; only one of them is ever read again.
	c.skill("p", "running-leaky-flow", goodDesc, goodContract, "")
	c.write("plugins/p/skills/running-leaky-flow/flow.json",
		`{"flow":[{"phase":1,"skill":"collecting-context","inputs":["target_id"],"outputs":["01_ctx.json"]},`+
			`{"phase":2,"skill":"drafting-strategy","inputs":["01_ctx.json"],"outputs":["02_plan.json","02_scratch.json"]},`+
			`{"phase":3,"skill":"publishing-result","inputs":["02_plan.json"],"outputs":["03_report.md"]}]}`)

	// Every intermediate output is consumed; the final phase's output is the deliverable.
	c.skill("p", "running-tight-flow", goodDesc, goodContract, "")
	c.write("plugins/p/skills/running-tight-flow/flow.json",
		`{"flow":[{"phase":1,"skill":"collecting-context","inputs":["target_id"],"outputs":["01_ctx.json"]},`+
			`{"phase":2,"skill":"publishing-result","inputs":["01_ctx.json"],"outputs":["02_report.md"]}]}`)

	res := c.run("", nil)
	fs := pick(res, "L24", "p:running-leaky-flow")
	if len(fs) != 1 || fs[0].Severity != Warn {
		t.Fatalf("expected exactly one warn for the unread artifact, got: %+v", fs)
	}
	if !strings.Contains(fs[0].Message, "02_scratch.json") {
		t.Errorf("the message should name the artifact nobody reads: %+v", fs[0])
	}
	if strings.Contains(fs[0].Message, "03_report.md") {
		t.Errorf("the last phase's output is the deliverable, not a leak: %+v", fs[0])
	}
	if fs := pick(res, "L24", "p:running-tight-flow"); len(fs) != 0 {
		t.Errorf("a flow whose artifacts are all read must be silent: %+v", fs)
	}
	if fs := pick(res, "L24", "p:collecting-context"); len(fs) != 0 {
		t.Errorf("a skill with no flow.json must be silent: %+v", fs)
	}
}

func TestL24IgnoresAnInvalidFlow(t *testing.T) {
	// Schema problems are L15's to report; saying anything else about a file we could not
	// read would be guessing.
	c := newCorpus(t)
	c.skill("p", "running-broken-flow", goodDesc, goodContract, "")
	c.write("plugins/p/skills/running-broken-flow/flow.json", `{"flow":[{"skill":`)
	if fs := pick(c.run("", nil), "L24", "p:running-broken-flow"); len(fs) != 0 {
		t.Errorf("an unparseable flow should produce no L24 finding: %+v", fs)
	}
}

func TestL18GuardDeclaredButNotInvoked(t *testing.T) {
	c := newCorpus(t)
	c.skill("alpha", "launching-ads-declared-only", goodDesc,
		"## Steps\n\n1. Set the budget and launch.\n", "requires: [billing-guard]\n")
	c.skill("alpha", "launching-ads-with-guard", goodDesc,
		"## Steps\n\n1. First invoke billing-guard with the Skill tool.\n2. Launch.\n",
		"requires: [billing-guard]\n")
	res := c.run("", nil)
	if fs := pick(res, "L18", "alpha:launching-ads-declared-only"); len(fs) == 0 || fs[0].Severity != Warn {
		t.Errorf("a declaration with no body mention should warn: %+v", fs)
	}
	if fs := pick(res, "L18", "alpha:launching-ads-with-guard"); len(fs) != 0 {
		t.Errorf("an imperative invocation in the body satisfies the rule: %+v", fs)
	}
}

func TestL19SizeDiscipline(t *testing.T) {
	c := newCorpus(t)
	// Over 500 lines.
	longBody := goodContract + "\n## Notes\n\n" + strings.Repeat("- filler line\n", 520)
	c.skill("p", "explaining-basics", goodDesc, longBody, "")
	// Under 500 lines but over the token budget: dense CJK text, which is exactly the case
	// line counting misses.
	// One long line per entry: ~120 CJK runes each, 120 entries -> well past 5,000 tokens
	// while staying comfortably under 500 lines. This is the shape line counting misses.
	longLine := strings.Repeat("この段落はモデルが既に知っている一般論を述べており削除しても挙動は変わらない。", 3)
	dense := goodContract + "\n## Notes\n\n" + strings.Repeat("- "+longLine+"\n", 120)
	c.skill("p", "narrating-theory", goodDesc, dense, "")
	c.skill("p", "drafting-weekly-plan", goodDesc, goodContract, "")

	res := c.run("", nil)
	if fs := pick(res, "L19", "p:explaining-basics"); !anyContains(fs, "lines") {
		t.Errorf("over 500 lines should warn: %+v", fs)
	}
	fs := pick(res, "L19", "p:narrating-theory")
	if !anyContains(fs, "estimated tokens") {
		t.Errorf("over the token budget should warn even under 500 lines: %+v", fs)
	}
	if anyContains(fs, " lines (official") {
		t.Errorf("this fixture is under 500 lines; no line finding expected: %+v", fs)
	}
	for _, f := range append(fs, pick(res, "L19", "p:explaining-basics")...) {
		if f.Severity != Warn {
			t.Errorf("size is a proxy metric and must stay warn: %+v", f)
		}
	}
	if fs := pick(res, "L19", "p:drafting-weekly-plan"); len(fs) != 0 {
		t.Errorf("a small compliant skill must produce no findings: %+v", fs)
	}
}

func TestL20BillingNeedsDisableModelInvocation(t *testing.T) {
	c := newCorpus(t)
	billing := "## Steps\n\n1. Set the ad spend budget.\n"
	c.skill("alpha", "paid-ads-setup", goodDesc, billing, "")
	c.skill("alpha", "launching-ads-guarded-manual", goodDesc, billing, "disable-model-invocation: true\n")
	c.skill("alpha", "billing-guard",
		"Confirmation guard for billing operations. Invoked by other skills before spend. "+
			"Use this before any billable operation. It performs no spend itself. "+
			"This dummy is padded so the length rule stays quiet.",
		"## Checks\n\n Confirm the ad spend cap before proceeding.\n", "user-invocable: false\n")
	c.skill("p", "charging-ad-budget", goodDesc, billing, "")
	c.skill("p", "orchestrating-ad-launch", goodDesc, goodContract, "")
	c.skill("p", "reporting-ad-result", goodDesc, goodContract, "")
	c.write("plugins/p/skills/orchestrating-ad-launch/flow.json",
		`{"flow":[{"phase":1,"skill":"charging-ad-budget","inputs":["x"],"outputs":["y"]},`+
			`{"phase":2,"skill":"reporting-ad-result","inputs":["y"],"outputs":["z"]}]}`)

	cfg := map[string]any{"guards": map[string][]string{"billing": {"billing-guard"}}}
	res := c.run("", cfg)

	if fs := pick(res, "L20", "alpha:launching-ads-guarded-manual"); len(fs) != 0 {
		t.Errorf("the flag satisfies the rule: %+v", fs)
	}
	if fs := pick(res, "L20", "alpha:billing-guard"); len(fs) != 0 {
		t.Errorf("a configured guard skill is exempt: %+v", fs)
	}
	if fs := pick(res, "L20", "p:charging-ad-budget"); len(fs) != 0 {
		t.Errorf("a flow delegation target is exempt (the flag would break the orchestrator): %+v", fs)
	}
	if fs := pick(res, "L20", "alpha:paid-ads-setup"); len(fs) == 0 || fs[0].Severity != Warn {
		t.Errorf("billing wording without the flag should warn: %+v", fs)
	}
}

func TestCompliantSkillHasNoFindings(t *testing.T) {
	c := newCorpus(t)
	c.skill("delta", "drafting-weekly-plan", goodDesc, goodContract, "")
	res := c.run("empty", nil)
	var got []Finding
	for _, f := range res.Findings {
		if f.Skill == "delta:drafting-weekly-plan" {
			got = append(got, f)
		}
	}
	if len(got) != 0 {
		t.Errorf("a fully compliant skill must produce zero findings: %+v", got)
	}
}

func TestBrokenFrontmatterSkipsOtherRules(t *testing.T) {
	c := newCorpus(t)
	c.write("plugins/p/skills/broken/SKILL.md", "# no frontmatter\n\nbody\n")
	res := c.run("", nil)
	if fs := pick(res, "L0", "p:broken"); len(fs) == 0 || fs[0].Severity != Error {
		t.Errorf("broken frontmatter should be an L0 error: %+v", fs)
	}
	for _, f := range res.Findings {
		if f.Skill == "p:broken" && f.Rule != "L0" {
			t.Errorf("other rules must be skipped for a broken skill: %+v", f)
		}
	}
}

func TestMissingBaselineTreatsAllAsExisting(t *testing.T) {
	c := newCorpus(t)
	c.skill("new", "MySkill_v2", "Dummy.", "body", "")
	res, err := Run(Options{Paths: []string{c.root},
		BaselinePath: filepath.Join(c.root, "no-such-baseline.json")})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Notes) == 0 || !strings.Contains(res.Notes[0], "baseline") {
		t.Errorf("a missing baseline should produce a note: %v", res.Notes)
	}
	fs := pick(res, "L1", "new:MySkill_v2")
	if len(fs) == 0 || fs[0].Severity != Warn {
		t.Errorf("without a baseline every skill is pre-existing, so warn: %+v", fs)
	}
}

func TestWriteBaseline(t *testing.T) {
	c := newCorpus(t)
	c.skill("p", "drafting-weekly-plan", goodDesc, goodContract, "")
	dest := filepath.Join(t.TempDir(), "baseline.json")
	n, err := WriteBaseline([]string{c.root}, dest)
	if err != nil || n != 1 {
		t.Fatalf("WriteBaseline = %d, %v", n, err)
	}
	raw, _ := os.ReadFile(dest)
	if !strings.Contains(string(raw), "p:drafting-weekly-plan") {
		t.Errorf("baseline content = %s", raw)
	}
}

func TestBundledSkillPassesItsOwnLinter(t *testing.T) {
	// The skill this repository ships is held to the conventions this repository enforces.
	// A rule that cannot be satisfied by the one skill written specifically to satisfy it is
	// a rule worth reconsidering, and finding that out here is cheaper than finding it out
	// in someone else's corpus.
	res, err := Run(Options{Paths: []string{filepath.Join("..", "..", "skills")}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Skills == 0 {
		t.Fatal("no skill discovered under skills/ — the fixture path is wrong")
	}
	for _, f := range res.Findings {
		t.Errorf("the bundled skill must stay clean: %s %s %s:%d %s",
			f.Severity, f.Rule, f.File, f.Line, f.Message)
	}
}

func anyContains(fs []Finding, sub string) bool {
	for _, f := range fs {
		if strings.Contains(f.Message, sub) {
			return true
		}
	}
	return false
}

func TestL6IgnoresIdentifiersAndQuotedStrings(t *testing.T) {
	// A term inside a code span, a link target or a quoted string is naming something, not
	// describing a computation. Real-corpus false positives that motivated this:
	// `aggregating-usage-dashboard` (a skill name), requirement-reconciliation.md (a path),
	// and "Aspect ratio 16:9" (a prompt string passed to an image model).
	c := newCorpus(t)
	c.skill("p", "naming-things-only", goodDesc,
		"## Notes\n\n"+
			"- `aggregating-usage-dashboard` — a sibling skill that fetches the same data.\n"+
			"- Read [references/requirement-reconciliation.md](references/requirement-reconciliation.md).\n"+
			"- Put \"Aspect ratio 16:9\" at the end of the prompt.\n", "")
	// A genuine description of deterministic work must still be caught.
	c.skill("p", "really-computes", goodDesc,
		"## Measurement\n\nCompute the ratio of sale revenue to baseline revenue.\n", "")

	res := c.run("", nil)
	if fs := pick(res, "L6", "p:naming-things-only"); len(fs) != 0 {
		t.Errorf("identifiers and quoted strings must not trigger L6: %+v", fs)
	}
	if fs := pick(res, "L6", "p:really-computes"); len(fs) == 0 {
		t.Error("genuine deterministic prose must still be caught")
	}
}

func TestL21DependencyTargetsMustExist(t *testing.T) {
	c := newCorpus(t)
	c.skill("p", "collecting-context", goodDesc, goodContract, "")
	c.skill("p", "using-existing-dep", goodDesc,
		"Invoke collecting-context with the Skill tool first.\n", "requires: [collecting-context]\n")
	c.skill("p", "using-qualified-dep", goodDesc,
		"Invoke p:collecting-context with the Skill tool first.\n", "requires: [p:collecting-context]\n")
	c.skill("p", "using-missing-dep", goodDesc,
		"Invoke ghost-guard with the Skill tool first.\n", "requires: [ghost-guard]\n")
	c.skill("p", "depending-on-missing", goodDesc,
		"Consumes the artifact that ghost-phase produced.\n", "depends_on: [ghost-phase]\n")

	res := c.run("", nil)
	for _, k := range []string{"p:using-existing-dep", "p:using-qualified-dep"} {
		if fs := pick(res, "L21", k); len(fs) != 0 {
			t.Errorf("%s: an existing target must not be flagged: %+v", k, fs)
		}
	}
	fs := pick(res, "L21", "p:using-missing-dep")
	if len(fs) == 0 || fs[0].Severity != Warn {
		t.Errorf("a missing requires target should warn: %+v", fs)
	}
	if !anyContains(fs, "ghost-guard") {
		t.Errorf("the message should name the missing target: %+v", fs)
	}
	if fs := pick(res, "L21", "p:depending-on-missing"); !anyContains(fs, "ghost-phase") {
		t.Errorf("depends_on gets the same treatment as requires: %+v", fs)
	}
}

func TestL21ConfiguredGuardCountsAsExisting(t *testing.T) {
	// L8 demands the guard be declared. L21 must not then report that same declaration as
	// dangling, or a skill could not satisfy both rules at once.
	c := newCorpus(t)
	c.skill("p", "launching-ads", goodDesc,
		"Invoke billing-guard with the Skill tool, then set the ad spend budget.\n",
		"requires: [billing-guard]\n")
	cfg := map[string]any{"guards": map[string][]string{"billing": {"billing-guard"}}}
	if fs := pick(c.run("", cfg), "L21", "p:launching-ads"); len(fs) != 0 {
		t.Errorf("a configured guard name must count as existing: %+v", fs)
	}
}

// triggerDesc builds a description whose only quoted phrase is the given one, padded so the
// L2 length rule stays quiet.
func triggerDesc(phrase string) string {
	return "Researches the competitive landscape for an online store. " +
		"Use this when the user says " + phrase + ", or asks for a rival breakdown. " +
		"Market sizing belongs to ec-market-research rather than to this skill. " +
		"This fixture is padded so that the description length rule stays quiet."
}

func TestL23CollidingTriggerPhrases(t *testing.T) {
	c := newCorpus(t)
	c.skill("p", "researching-competitors", triggerDesc("「競合を調べたい」"), goodContract, "")
	c.skill("p", "analysing-rivals", triggerDesc("「競合を調べたい」"), goodContract, "")
	c.skill("p", "drafting-weekly-plan", goodDesc, goodContract, "")

	res := c.run("", nil)
	for _, k := range []string{"p:researching-competitors", "p:analysing-rivals"} {
		fs := pick(res, "L23", k)
		if len(fs) == 0 || fs[0].Severity != Warn {
			t.Errorf("%s: a shared trigger phrase should warn: %+v", k, fs)
		}
		if !anyContains(fs, "競合を調べたい") {
			t.Errorf("%s: the message should quote the colliding phrase: %+v", k, fs)
		}
	}
	if fs := pick(res, "L23", "p:researching-competitors"); !anyContains(fs, "p:analysing-rivals") {
		t.Errorf("the message should name the other claimant: %+v", fs)
	}
	if fs := pick(res, "L23", "p:drafting-weekly-plan"); len(fs) != 0 {
		t.Errorf("a skill sharing no phrase must not be flagged: %+v", fs)
	}
}

func TestL23IgnoresShortPhrasesAndApostrophes(t *testing.T) {
	// Below four runes a quoted fragment is a word, not a request: 「診断」 is shared by every
	// diagnostic skill in a corpus and says nothing about which one should fire. Single
	// quotes are not treated as quotation at all, because English prose is full of
	// apostrophes and a pair of them would manufacture a phrase out of ordinary text.
	c := newCorpus(t)
	for _, n := range []string{"diagnosing-traffic-drop", "diagnosing-ad-health"} {
		c.skill("p", n, triggerDesc("「診断」"), goodContract, "")
	}
	apostrophes := "Drafts the plan and reviews last month's numbers against this quarter's. " +
		"Use this when the user asks for a plan draft, or for a monthly review. " +
		"Scheduling belongs to another skill rather than to this one. " +
		"This fixture is padded so the description length rule stays quiet."
	c.skill("p", "drafting-one", apostrophes, goodContract, "")
	c.skill("p", "drafting-two", apostrophes, goodContract, "")

	res := c.run("", nil)
	for _, k := range []string{"p:diagnosing-traffic-drop", "p:drafting-one", "p:drafting-two"} {
		if fs := pick(res, "L23", k); len(fs) != 0 {
			t.Errorf("%s should produce no L23 finding: %+v", k, fs)
		}
	}
}

func TestL23IgnoresSingleWordsAndVariableReferences(t *testing.T) {
	// Both classes came out of measurement. A rune floor alone cannot rule out "analyze":
	// four runes is a whole word in English and a fragment in Japanese, so a Latin phrase
	// additionally has to contain a space. And a quoted variable is an instruction to an
	// implementer, never something a user says.
	c := newCorpus(t)
	for _, n := range []string{"analysing-report", "analysing-transactions"} {
		c.skill("p", n, triggerDesc("\"analyze\""), goodContract, "")
	}
	for _, n := range []string{"developing-hooks", "structuring-plugins"} {
		c.skill("q", n, triggerDesc("\"use ${PLUGIN_ROOT} for paths\""), goodContract, "")
	}
	res := c.run("", nil)
	for _, k := range []string{"p:analysing-report", "q:developing-hooks"} {
		if fs := pick(res, "L23", k); len(fs) != 0 {
			t.Errorf("%s: a word or a variable is not a trigger phrase: %+v", k, fs)
		}
	}
}

func TestL23ReportsAVendoredDuplicateOnce(t *testing.T) {
	// A corpus that vendors the same plugin twice discovers one skill key at two paths.
	// That is L14's subject; L23 must not say the skill competes with itself.
	c := newCorpus(t)
	desc := triggerDesc("「競合を調べたい」")
	c.skill("p", "researching-competitors", desc, goodContract, "")
	c.write("vendor/p/skills/researching-competitors/SKILL.md",
		"---\nname: researching-competitors\ndescription: "+desc+"\n---\n\n"+goodContract+"\n")
	c.skill("p", "analysing-rivals", desc, goodContract, "")

	fs := pick(c.run("", nil), "L23", "p:researching-competitors")
	if len(fs) != 1 {
		t.Errorf("expected one finding for the duplicated key, got: %+v", fs)
	}
	if anyContains(fs, "researching-competitors / ") || anyContains(fs, "/ p:researching-competitors") {
		t.Errorf("a skill must not be listed as its own rival: %+v", fs)
	}
}

func TestL23CanonicalDeclarationSilencesTheCollision(t *testing.T) {
	// Two copies that declare their relationship are a managed duplicate, which is L7 and
	// L14's subject rather than an activation problem.
	c := newCorpus(t)
	c.skill("alpha", "researching-competitors", triggerDesc("「競合を調べたい」"), goodContract, "")
	c.skill("gamma", "researching-competitors", triggerDesc("「競合を調べたい」"), goodContract,
		"canonical: alpha:researching-competitors\n")
	res := c.run("", nil)
	for _, k := range []string{"alpha:researching-competitors", "gamma:researching-competitors"} {
		if fs := pick(res, "L23", k); len(fs) != 0 {
			t.Errorf("%s: a declared duplicate is not an activation collision: %+v", k, fs)
		}
	}
}

func TestL23ReadsWhenToUseToo(t *testing.T) {
	// The listing concatenates when_to_use onto the description, so a phrase there competes
	// for activation exactly as one in the description does.
	c := newCorpus(t)
	c.skill("p", "publishing-report", goodDesc, goodContract,
		"when_to_use: use this when the user says \"publish the weekly report\"\n")
	c.skill("p", "sharing-report", triggerDesc("\"publish the weekly report\""), goodContract, "")
	res := c.run("", nil)
	for _, k := range []string{"p:publishing-report", "p:sharing-report"} {
		if fs := pick(res, "L23", k); len(fs) == 0 {
			t.Errorf("%s: a phrase in when_to_use must count: %+v", k, fs)
		}
	}
}

func TestL22BrokenLocalLink(t *testing.T) {
	c := newCorpus(t)
	c.skill("p", "linking-references", goodDesc,
		"## Steps\n\n"+
			"1. Read [the rate card](references/pricing.md).\n"+
			"2. Read [the missing note](references/missing.md).\n"+
			"3. Read [the same missing note](references/missing.md).\n"+
			"4. Browse [the directory](references/).\n"+
			"5. Jump to [an anchor](references/pricing.md#rates).\n", "")
	c.write("plugins/p/skills/linking-references/references/pricing.md", "rates\n")

	fs := pick(c.run("", nil), "L22", "p:linking-references")
	if len(fs) != 1 {
		t.Fatalf("expected exactly one finding: an existing file, a directory and an anchor "+
			"all resolve, and the repeat is deduped. Got: %+v", fs)
	}
	if !strings.Contains(fs[0].Message, "references/missing.md") {
		t.Errorf("unexpected message: %+v", fs[0])
	}
	if fs[0].Severity != Warn {
		t.Errorf("a references file may legitimately be generated, so this stays warn: %+v", fs[0])
	}
	// Four frontmatter lines, then the helper's blank line, "## Steps", a blank, then the
	// steps: the first broken link is on SKILL.md line 9.
	if fs[0].Line != 9 {
		t.Errorf("the finding should point at the first broken link (line 9), got %d", fs[0].Line)
	}
}

func TestL22OnlyLinksIntoThisSkillAreChecked(t *testing.T) {
	// Measured on a real corpus of 154 skills: checking every path-like string in the body
	// produced 53 findings and not one defect. A path in prose is nearly always an
	// illustrative example from a skill-authoring guide, or another skill's file named in
	// passing. Only a markdown link into this skill's own directories claims the file is here.
	c := newCorpus(t)
	c.skill("p", "documenting-layout", goodDesc,
		"- **Examples**: `references/finance.md` for financial schemas.\n"+
			"- Run `scripts/rotate_pdf.py` for PDF rotation tasks.\n"+
			"- See querying-warehouse's references/04_RECONCILIATION_GATES.md for the detail.\n"+
			"- A sibling's file: [elsewhere](../other/references/gone.md).\n"+
			"- An external page: [docs](https://example.com/references/gone.md).\n"+
			"- A placeholder target: [link](<link>).\n"+
			"- A variable target: [job](${CLAUDE_SKILL_DIR}/jobs/scan.md).\n", "")
	if fs := pick(c.run("", nil), "L22", "p:documenting-layout"); len(fs) != 0 {
		t.Errorf("only links into this skill's own directories are checked: %+v", fs)
	}
}
