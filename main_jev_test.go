package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The --jev boundary, asserted end to end through cmdLint: no flag means no socket, a dry
// run prices without a key, a missing key stops with the way out, and a judged run reports
// warnings that leave the exit code alone.

func writeSkill(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, "plugins", "acme", "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\nname: " + name + "\ndescription: Drafts the weekly plan for an account. Use this when the user asks for a weekly plan draft or next week's schedule, or before the regular review meeting. If only schedule deltas are needed, use reviewing-schedule-delta instead. Not for account setup, which setting-up-account-state owns.\n---\n\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
}

const fullContract = "## Contract\n" +
	"- **Trigger**: the user asks for a weekly plan draft.\n" +
	"- **Inputs**: account_id.\n" +
	"- **Preconditions**: the account state directory exists.\n" +
	"- **Outputs**: 01_plan.md\n" +
	"- **Postconditions**: 01_plan.md exists.\n" +
	"- **Non-goals**: does not inspect schedule deltas (reviewing-schedule-delta owns that).\n"

// capture runs cmdLint and returns its exit code with what it printed to stdout.
func capture(t *testing.T, argv ...string) (int, string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	code := cmdLint(argv)
	w.Close()
	os.Stdout = saved
	out, _ := io.ReadAll(r)
	return code, string(out)
}

// tripwire is a Jev endpoint that fails the test if anything reaches it.
func tripwire(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a request reached the Jev endpoint: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(500)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("TYPESAFE_BASE_URL", srv.URL)
}

func TestLintWithoutJevOpensNoSocket(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "drafting-weekly-plan", fullContract)
	tripwire(t)
	t.Setenv("TYPESAFE_API_KEY", "k") // even with a key in the environment

	code, _ := capture(t, root, "--baseline", filepath.Join(root, "none.json"))

	if code != 0 {
		t.Errorf("exit %d, want 0", code)
	}
}

func TestDryRunWithoutJevIsAUsageError(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "drafting-weekly-plan", fullContract)

	if code, _ := capture(t, root, "--dry-run"); code != 2 {
		t.Errorf("exit %d, want 2: --dry-run has nothing to price without --jev", code)
	}
}

func TestJevWithoutAKeyStopsAndNamesTheWayOut(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "drafting-weekly-plan", fullContract)
	tripwire(t)
	t.Setenv("TYPESAFE_API_KEY", "")
	stderr := captureStderr(t)

	code, _ := capture(t, root, "--jev", "--baseline", filepath.Join(root, "none.json"))

	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if msg := stderr(); !strings.Contains(msg, "TYPESAFE_API_KEY") || !strings.Contains(msg, "--dry-run") {
		t.Errorf("stderr = %q, want the variable and the dry-run way out named", msg)
	}
}

func TestJevDryRunPricesWithoutAKeyOrASocket(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "drafting-weekly-plan", fullContract)
	tripwire(t)
	t.Setenv("TYPESAFE_API_KEY", "")

	code, out := capture(t, root, "--jev", "--dry-run", "--baseline", filepath.Join(root, "none.json"))

	if code != 0 {
		t.Errorf("exit %d, want 0", code)
	}
	for _, want := range []string{"1 skill(s)", "1 request(s)", "3 question(s)", "payload tokens", "$", "nothing was sent"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry run output %q lacks %q", out, want)
		}
	}
}

func TestJevReportsWarningsAndLeavesTheExitCodeAlone(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "drafting-weekly-plan", fullContract)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"answers":{"J2":{"type":"noul","noul":0.93},"J3":{"type":"noul","noul":0.05},"J5":{"type":"noul","noul":0.04}}}`))
	}))
	defer srv.Close()
	t.Setenv("TYPESAFE_BASE_URL", srv.URL)
	t.Setenv("TYPESAFE_API_KEY", "k")

	code, out := capture(t, root, "--jev", "--baseline", filepath.Join(root, "none.json"))

	if code != 0 {
		t.Errorf("exit %d, want 0: a judged rule is a warning", code)
	}
	if !strings.Contains(out, "warn  J2") || !strings.Contains(out, "0.93") {
		t.Errorf("output %q lacks the J2 warning with its probability", out)
	}
	if strings.Contains(out, "J3") || strings.Contains(out, "J5") {
		t.Errorf("output %q reports a rule whose answer was under the cutoff", out)
	}
	if !strings.Contains(out, "1 warnings") {
		t.Errorf("output %q, want the summary to count the judged warning", out)
	}
}

func captureStderr(t *testing.T) func() string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stderr
	os.Stderr = w
	return func() string {
		w.Close()
		os.Stderr = saved
		b, _ := io.ReadAll(r)
		return string(b)
	}
}

func TestChangedWithJevSendsOnlyTheTouchedSkills(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha", fullContract)
	writeSkill(t, root, "beta", fullContract)
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"}, {"add", "."}, {"commit", "-qm", "base"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	writeSkill(t, root, "beta", fullContract+"\nOne more line.\n")
	tripwire(t)

	code, out := capture(t, root, "--changed", "--base", "HEAD", "--jev", "--dry-run", "--baseline", filepath.Join(root, "none.json"))

	if code != 0 || !strings.Contains(out, "1 skill(s)") {
		t.Errorf("exit %d, output %q: want a dry run over the one touched skill", code, out)
	}
}
