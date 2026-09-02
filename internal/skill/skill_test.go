package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverFindsSkillsAndPlugin(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "plugins/alpha/skills/drafting-plan/SKILL.md"),
		"---\nname: drafting-plan\ndescription: x\n---\n\n本文\n")
	// Also discover layouts without a skills/ level (e.g. .claude/skills/<name>/).
	write(t, filepath.Join(root, "flat/querying-warehouse/SKILL.md"),
		"---\nname: querying-warehouse\ndescription: y\n---\n")

	got, err := Discover([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("discovered %d skills, want 2", len(got))
	}
	byKey := map[string]*Skill{}
	for _, s := range got {
		byKey[s.Key()] = s
	}
	if _, ok := byKey["alpha:drafting-plan"]; !ok {
		t.Errorf("plugin resolution is wrong: %v", byKey)
	}
	if _, ok := byKey["flat:querying-warehouse"]; !ok {
		t.Errorf("layout without skills/ was not discovered: %v", byKey)
	}
}

func TestDiscoverRecordsBrokenFrontmatter(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "p/skills/broken/SKILL.md"), "# frontmatter が無い\n")
	got, _ := Discover([]string{root})
	if len(got) != 1 || !got[0].Broken {
		t.Fatalf("broken flag was not set: %+v", got)
	}
}

func TestParseContract(t *testing.T) {
	body := "## Contract\n" +
		"- **Trigger**: t\n- **Inputs**: i\n- **Preconditions**: p\n" +
		"- **Outputs**: o\n- **Postconditions**: q\n- **Non-goals**: n\n\n## 進め方\n"
	c := ParseContract(body)
	if !c.Present || !c.FirstHeading || !c.Complete() {
		t.Fatalf("contract = %+v", c)
	}
	if c.Items["Outputs"] != "o" {
		t.Errorf("Outputs = %q", c.Items["Outputs"])
	}
}

func TestParseContractIncomplete(t *testing.T) {
	c := ParseContract("## 概要\n\n## Contract\n- **Trigger**: t\n")
	if !c.Present {
		t.Fatal("present should be true")
	}
	if c.FirstHeading {
		t.Error("should not be reported as the first heading")
	}
	if c.Complete() {
		t.Error("reported complete without all six entries")
	}
}

func TestParseContractAbsent(t *testing.T) {
	if c := ParseContract("## 手順\n\n1. やる\n"); c.Present {
		t.Error("absent Contract reported as present")
	}
}

func TestEstimateTokens(t *testing.T) {
	// 1.2 tokens per CJK rune; 1 token per 4 other runes.
	if got := EstimateTokens(repeat("あ", 100)); got != 120 {
		t.Errorf("100 CJK runes = %d, want 120", got)
	}
	if got := EstimateTokens(repeat("a", 100)); got != 25 {
		t.Errorf("100 ASCII runes = %d, want 25", got)
	}
}

func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
