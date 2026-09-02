package skill

import "testing"

// What: the parser handles the YAML subset that real skill corpora use (docs/DESIGN.md §3).
// It is hand-rolled to avoid a YAML dependency, and parses leniently: unreadable lines are
// recorded as problems rather than aborting (the official client-implementation guide also
// recommends lenient validation).

func TestParseFlatKeyValue(t *testing.T) {
	fm, err := Parse("---\nname: drafting-plan\ndescription: 説明文。\n---\n\n本文\n")
	if err != nil {
		t.Fatal(err)
	}
	if fm.Meta.Str("name") != "drafting-plan" {
		t.Errorf("name = %q", fm.Meta.Str("name"))
	}
	if fm.Meta.Str("description") != "説明文。" {
		t.Errorf("description = %q", fm.Meta.Str("description"))
	}
	if fm.Body != "\n本文\n" {
		t.Errorf("body = %q", fm.Body)
	}
	if fm.BodyStart != 5 {
		t.Errorf("bodyStart = %d, want 5", fm.BodyStart)
	}
}

func TestParseBlockScalarLiteral(t *testing.T) {
	// Required: multiple real skills write description as a literal block scalar.
	fm, _ := Parse("---\nname: x\ndescription: |\n  一行目。\n  二行目。\n---\n\n本文\n")
	got := fm.Meta.Str("description")
	if got != "一行目。\n二行目。" {
		t.Errorf("block scalar = %q", got)
	}
}

func TestParseBlockScalarFolded(t *testing.T) {
	fm, _ := Parse("---\nname: x\ndescription: >\n  一行目。\n  二行目。\n---\n")
	if got := fm.Meta.Str("description"); got != "一行目。 二行目。" {
		t.Errorf("folded = %q", got)
	}
}

func TestParseLists(t *testing.T) {
	fm, _ := Parse("---\nname: x\nrequires: [a, b]\ndepends_on:\n  - c\n  - d\n---\n")
	if got := fm.Meta.List("requires"); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("inline list = %v", got)
	}
	if got := fm.Meta.List("depends_on"); len(got) != 2 || got[0] != "c" || got[1] != "d" {
		t.Errorf("line list = %v", got)
	}
}

func TestParseBoolAndQuoted(t *testing.T) {
	fm, _ := Parse("---\nname: x\nuser-invocable: false\ndisable-model-invocation: true\nargument-hint: '[issue]'\n---\n")
	if v, ok := fm.Meta.Bool("user-invocable"); !ok || v {
		t.Errorf("user-invocable = %v %v", v, ok)
	}
	if v, ok := fm.Meta.Bool("disable-model-invocation"); !ok || !v {
		t.Errorf("disable-model-invocation = %v %v", v, ok)
	}
	if got := fm.Meta.Str("argument-hint"); got != "[issue]" {
		t.Errorf("quoted = %q", got)
	}
}

func TestParseNestedOneLevel(t *testing.T) {
	fm, _ := Parse("---\nname: x\nmetadata:\n  author: acme\n  version: \"1.0\"\n---\n")
	if !fm.Meta.Has("metadata") {
		t.Error("metadata was not parsed")
	}
}

func TestParseMissingFrontmatter(t *testing.T) {
	if _, err := Parse("# 見出しから始まる\n"); err == nil {
		t.Error("absent frontmatter must be an error")
	}
	if _, err := Parse("---\nname: x\n本文が来て閉じない\n"); err == nil {
		t.Error("unterminated frontmatter must be an error")
	}
}

func TestParseRecordsUnreadableLines(t *testing.T) {
	fm, err := Parse("---\nname: x\n@@ 壊れた行 @@\n---\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(fm.Problems) == 0 {
		t.Error("uninterpretable lines must be recorded in problems")
	}
	if fm.Meta.Str("name") != "x" {
		t.Error("keys that parsed must be kept (lenient parsing)")
	}
}
