package lint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// decode navigates the SARIF document generically: asserting on a full set of typed structs
// would restate the writer rather than check it.
func decodeSARIF(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	if !json.Valid(raw) {
		t.Fatalf("output is not valid JSON: %s", raw)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func firstRun(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	runs, ok := doc["runs"].([]any)
	if !ok || len(runs) != 1 {
		t.Fatalf("expected exactly one run, got %v", doc["runs"])
	}
	return runs[0].(map[string]any)
}

func TestSARIFDocumentShape(t *testing.T) {
	fs := []Finding{
		{"L8", Error, "p:paid-ads-setup", "/corpus/p/skills/paid-ads-setup/SKILL.md", 12,
			"billing wording is present but requires: does not declare billing-guard"},
		{"L6", Warn, "p:flash-sale", "/corpus/p/skills/flash-sale/SKILL.md", 3,
			"deterministic logic is described in prose but there is no scripts/"},
	}
	doc := decodeSARIF(t, mustSARIF(t, fs, "1.2.3"))

	if doc["version"] != "2.1.0" {
		t.Errorf("version = %v, want 2.1.0", doc["version"])
	}
	if s, _ := doc["$schema"].(string); !strings.Contains(s, "sarif") {
		t.Errorf("$schema should name the SARIF schema, got %v", doc["$schema"])
	}

	run := firstRun(t, doc)
	driver := run["tool"].(map[string]any)["driver"].(map[string]any)
	if driver["name"] != "mekiki" {
		t.Errorf("driver.name = %v", driver["name"])
	}
	if driver["version"] != "1.2.3" {
		t.Errorf("driver.version should carry the binary's version, got %v", driver["version"])
	}
	// Every rule that produced a result must be declared, so a consumer can name it.
	var ids []string
	for _, r := range driver["rules"].([]any) {
		ids = append(ids, r.(map[string]any)["id"].(string))
	}
	if len(ids) != 2 || ids[0] != "L6" || ids[1] != "L8" {
		t.Errorf("rules should list each rule once, sorted; got %v", ids)
	}

	results := run["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("expected two results, got %d", len(results))
	}
	first := results[0].(map[string]any)
	if first["ruleId"] != "L8" || first["level"] != "error" {
		t.Errorf("an error finding should map to level error: %v", first)
	}
	if second := results[1].(map[string]any); second["level"] != "warning" {
		t.Errorf("a warn finding should map to SARIF level \"warning\", got %v", second["level"])
	}
	msg := first["message"].(map[string]any)["text"].(string)
	if !strings.Contains(msg, "billing-guard") {
		t.Errorf("the finding message should survive intact, got %q", msg)
	}
	loc := first["locations"].([]any)[0].(map[string]any)["physicalLocation"].(map[string]any)
	if uri := loc["artifactLocation"].(map[string]any)["uri"].(string); strings.Contains(uri, `\`) {
		t.Errorf("uri must be slash-separated, got %q", uri)
	}
	if line := loc["region"].(map[string]any)["startLine"].(float64); line != 12 {
		t.Errorf("startLine = %v, want 12", line)
	}
}

func TestSARIFEmptyResultsIsAnArray(t *testing.T) {
	// A null here is rejected by GitHub's SARIF upload, so the empty case is load-bearing:
	// it is exactly what a clean corpus produces.
	raw := mustSARIF(t, nil, "dev")
	if !strings.Contains(string(raw), `"results": []`) {
		t.Errorf("empty results must serialise as [], got:\n%s", raw)
	}
	run := firstRun(t, decodeSARIF(t, raw))
	if len(run["results"].([]any)) != 0 {
		t.Error("expected zero results")
	}
}

func TestSARIFPathsAreRelativeToTheWorkingDirectory(t *testing.T) {
	// GitHub resolves a result's uri against the repository root, so an absolute path from
	// the build machine would point nowhere in the PR view.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(cwd, "corpus", "p", "skills", "a", "SKILL.md")
	doc := decodeSARIF(t, mustSARIF(t, []Finding{{"L1", Error, "p:a", inside, 2, "m"}}, "dev"))
	run := firstRun(t, doc)
	loc := run["results"].([]any)[0].(map[string]any)["locations"].([]any)[0].(map[string]any)
	uri := loc["physicalLocation"].(map[string]any)["artifactLocation"].(map[string]any)["uri"].(string)
	if uri != "corpus/p/skills/a/SKILL.md" {
		t.Errorf("uri = %q, want a path relative to the working directory", uri)
	}
}

func TestSARIFClampsMissingLineNumbers(t *testing.T) {
	// startLine is 1-based in SARIF; a zero would make the document invalid.
	doc := decodeSARIF(t, mustSARIF(t, []Finding{{"L10", Error, "-", "/x/a.zip", 0, "m"}}, "dev"))
	run := firstRun(t, doc)
	loc := run["results"].([]any)[0].(map[string]any)["locations"].([]any)[0].(map[string]any)
	line := loc["physicalLocation"].(map[string]any)["region"].(map[string]any)["startLine"].(float64)
	if line != 1 {
		t.Errorf("startLine = %v, want it clamped to 1", line)
	}
}

func mustSARIF(t *testing.T, fs []Finding, version string) []byte {
	t.Helper()
	raw, err := SARIF(fs, version)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
