package lint

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Scoping exists because a pull request is answerable only for the skills it touched.
// Linting the whole corpus on every review reports an inherited backlog as this change's
// work, which is the fastest way to teach a team to ignore the gate.

// dirs builds the key-to-directory map Scope works from, the way a lint run hands it over.
func dirs(root string, keys ...string) map[string]string {
	m := map[string]string{}
	for _, k := range keys {
		plugin, name, _ := strings.Cut(k, ":")
		m[k] = filepath.Join(root, "plugins", plugin, "skills", name)
	}
	return m
}

func TestScopeKeepsOnlyTouchedSkills(t *testing.T) {
	root := "/repo"
	skills := dirs(root, "acme:alpha", "acme:beta")
	findings := []Finding{
		f("L6", "acme:alpha", Warn, "prose arithmetic", 3),
		f("L6", "acme:beta", Warn, "prose arithmetic", 3),
	}
	changed := []string{filepath.Join(root, "plugins/acme/skills/alpha/SKILL.md")}

	got := Scope(findings, skills, changed).Findings

	if len(got) != 1 || got[0].Skill != "acme:alpha" {
		t.Errorf("Scope kept %+v, want only the touched skill's finding", got)
	}
}

func TestScopeSaysSoWhenNoSkillWasTouched(t *testing.T) {
	// Silence here reads as "the corpus is clean", which is the opposite of the truth: the
	// change simply never reached a skill.
	root := "/repo"
	skills := dirs(root, "acme:alpha")
	findings := []Finding{f("L6", "acme:alpha", Warn, "prose arithmetic", 3)}

	r := Scope(findings, skills, []string{filepath.Join(root, "README.md")})
	got, notes := r.Findings, r.Notes

	if len(got) != 0 {
		t.Errorf("Scope kept %+v, want nothing", got)
	}
	if !anyNoteContains(notes, "no skill") {
		t.Errorf("notes = %q, want one saying no skill was touched", notes)
	}
}

func TestScopeReportsChangedFilesOutsideEverySkill(t *testing.T) {
	root := "/repo"
	skills := dirs(root, "acme:alpha")
	findings := []Finding{f("L6", "acme:alpha", Warn, "prose arithmetic", 3)}
	changed := []string{
		filepath.Join(root, "plugins/acme/skills/alpha/SKILL.md"),
		filepath.Join(root, "plugins/acme/.claude-plugin/plugin.json"),
		filepath.Join(root, "README.md"),
	}

	notes := Scope(findings, skills, changed).Notes

	if !anyNoteContains(notes, "2 changed file") {
		t.Errorf("notes = %q, want the two out-of-skill files counted", notes)
	}
}

func anyNoteContains(notes []string, want string) bool {
	for _, n := range notes {
		if strings.Contains(n, want) {
			return true
		}
	}
	return false
}

func TestScopeReportsASkillThePullRequestDeleted(t *testing.T) {
	// A deleted skill carries no findings of its own — it is gone — but it is the most
	// dangerous thing a skills pull request can do, so it must never vanish from the report.
	root := "/repo"
	skills := dirs(root, "acme:beta")
	changed := []string{filepath.Join(root, "plugins/acme/skills/alpha/SKILL.md")}

	notes := Scope(nil, skills, changed).Notes

	if !anyNoteContains(notes, "acme:alpha") || !anyNoteContains(notes, "no longer present") {
		t.Errorf("notes = %q, want the deleted skill named", notes)
	}
}

func TestScopeKeepsAFindingAnUntouchedSkillGotFromThisChange(t *testing.T) {
	// Deleting alpha breaks beta's flow. The finding lands on beta, which the change never
	// edited — dropping it would hide the damage the change actually did.
	root := "/repo"
	skills := dirs(root, "acme:beta")
	findings := []Finding{
		f("L16", "acme:beta", Error, `flow references skill "alpha", which does not exist`, 1),
		f("L6", "acme:beta", Warn, "prose arithmetic", 3),
	}
	changed := []string{filepath.Join(root, "plugins/acme/skills/alpha/SKILL.md")}

	got := Scope(findings, skills, changed).Findings

	if len(got) != 1 || got[0].Rule != "L16" {
		t.Errorf("Scope kept %+v, want only the finding naming the touched skill", got)
	}
}

func TestScopeKeepsACollisionWithATouchedSkill(t *testing.T) {
	root := "/repo"
	skills := dirs(root, "acme:alpha", "acme:beta")
	findings := []Finding{
		f("L23", "acme:beta", Warn, `trigger phrase "週次レポート" is also claimed by acme:alpha`, 2),
	}
	changed := []string{filepath.Join(root, "plugins/acme/skills/alpha/SKILL.md")}

	got := Scope(findings, skills, changed).Findings

	if len(got) != 1 {
		t.Errorf("Scope kept %+v, want the collision the change created", got)
	}
}

// ---- reading the change from git ----

func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestChangedFilesSeesCommittedAndUncommittedWork(t *testing.T) {
	// A pull request is usually committed and a local check usually is not. Both are "the
	// change" to the person asking, so both have to be read.
	dir := gitRepo(t)
	os.WriteFile(filepath.Join(dir, "base.txt"), []byte("x"), 0o644)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "base")
	basePoint := git(t, dir, "rev-parse", "HEAD")

	os.WriteFile(filepath.Join(dir, "committed.txt"), []byte("y"), 0o644)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "work")
	os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("z"), 0o644)
	os.WriteFile(filepath.Join(dir, "base.txt"), []byte("modified"), 0o644)

	got, err := ChangedFiles(dir, basePoint)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"committed.txt", "untracked.txt", "base.txt"} {
		if !anyNoteContains(got, want) {
			t.Errorf("ChangedFiles = %v, missing %s", got, want)
		}
	}
	for _, p := range got {
		if !filepath.IsAbs(p) {
			t.Errorf("ChangedFiles returned %q, want an absolute path to match against skill dirs", p)
		}
	}
}

func TestChangedFilesRefusesOutsideAGitRepository(t *testing.T) {
	if _, err := ChangedFiles(t.TempDir(), "main"); err == nil {
		t.Error("ChangedFiles succeeded outside a repository, want an error")
	}
}

func TestChangedFilesExplainsAnUnresolvableBase(t *testing.T) {
	// The default base is a remote ref, which a freshly cloned-less repository does not have.
	// The message has to point at the way out rather than leaking git's own wording.
	dir := gitRepo(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "base")

	_, err := ChangedFiles(dir, DefaultBase)
	if err == nil {
		t.Fatal("ChangedFiles resolved a base that does not exist")
	}
	if !strings.Contains(err.Error(), "--base") {
		t.Errorf("error = %q, want it to name the --base flag", err)
	}
}

func TestScopeSummaryCountsOnlyWhatSurvivedTheScope(t *testing.T) {
	// The exit code follows the scoped report, not the corpus: a pull request must not be
	// failed by an error it neither introduced nor touched.
	root := "/repo"
	findings := []Finding{
		f("L8", "acme:alpha", Error, "publication wording without a confirmation step", 1),
		f("L8", "acme:beta", Error, "publication wording without a confirmation step", 1),
		f("L6", "acme:alpha", Warn, "prose arithmetic", 3),
	}
	changed := []string{filepath.Join(root, "plugins/acme/skills/alpha/SKILL.md")}

	sum := Scope(findings, dirs(root, "acme:alpha", "acme:beta"), changed).Summary()

	if sum.Errors != 1 || sum.Warnings != 1 || sum.Skills != 1 {
		t.Errorf("Summary() = %+v, want 1 error, 1 warning, 1 skill", sum)
	}
}

func TestScopeMatchesARelativeCorpusPathAgainstAbsoluteChanges(t *testing.T) {
	// `mekiki lint skills/ --changed` discovers relative skill directories while git reports
	// absolute paths. Comparing the two as written makes every touched skill look deleted.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative := map[string]string{"acme:alpha": filepath.Join("plugins", "acme", "skills", "alpha")}
	changed := []string{filepath.Join(wd, "plugins", "acme", "skills", "alpha", "SKILL.md")}

	r := Scope([]Finding{f("L6", "acme:alpha", Warn, "prose arithmetic", 3)}, relative, changed)

	if len(r.Findings) != 1 {
		t.Errorf("Findings = %+v, want the touched skill's finding", r.Findings)
	}
	if anyNoteContains(r.Notes, "no longer present") {
		t.Errorf("notes = %q, want no deletion claimed for a skill that is right there", r.Notes)
	}
}

func TestScopeSeesThroughASymlinkedCorpusPath(t *testing.T) {
	// git reports paths with every symlink resolved; a corpus reached through a linked
	// directory (macOS /var, a checkout under a symlinked home) does not. Compared as
	// written, every touched skill looks deleted and its findings arrive under a false note.
	root := t.TempDir()
	real := filepath.Join(root, "real")
	dir := filepath.Join(real, "plugins", "acme", "skills", "alpha")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks are not available here")
	}
	viaLink := map[string]string{"acme:alpha": filepath.Join(link, "plugins", "acme", "skills", "alpha")}
	changed := []string{filepath.Join(dir, "SKILL.md")}

	r := Scope([]Finding{f("L6", "acme:alpha", Warn, "prose arithmetic", 3)}, viaLink, changed)

	if anyNoteContains(r.Notes, "no longer present") {
		t.Errorf("notes = %q, want no deletion claimed for a skill reached through a symlink", r.Notes)
	}
	if r.Skills != 1 {
		t.Errorf("Skills = %d, want the linked skill counted as touched", r.Skills)
	}
}

func TestScopeDoesNotCallAnUnlintedSkillDeleted(t *testing.T) {
	// `mekiki lint pluginA --changed` reads the whole repository's diff but discovers only
	// the skills under pluginA. A skill edited under pluginB is absent from dirs without
	// having been deleted, and "deleted" is the one claim this note must never get wrong.
	// Out of the linted path is not gone: it belongs with the files nothing examined.
	root := t.TempDir()
	outside := filepath.Join(root, "plugins", "other", "skills", "beta")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	edited := filepath.Join(outside, "SKILL.md")
	if err := os.WriteFile(edited, []byte("---\nname: beta\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	linted := map[string]string{"acme:alpha": filepath.Join(root, "plugins", "acme", "skills", "alpha")}

	r := Scope(nil, linted, []string{edited})

	if anyNoteContains(r.Notes, "no longer present") {
		t.Errorf("notes = %q, want no deletion claimed for a skill that is still on disk", r.Notes)
	}
	if !anyNoteContains(r.Notes, "1 changed file") {
		t.Errorf("notes = %q, want the unlinted skill counted as unexamined", r.Notes)
	}
}

func TestScopeIgnoresATouchedNameQuotedAsSomethingElse(t *testing.T) {
	// The bare-name form exists for rules that name a skill by name alone (a flow target, a
	// declared dependency). A trigger phrase that happens to equal a touched skill's name is
	// a collision between two other skills and has nothing to do with this change.
	root := "/repo"
	skills := dirs(root, "acme:alpha", "acme:beta", "acme:gamma")
	findings := []Finding{
		f("L23", "acme:beta", Warn, `trigger phrase "alpha" is also claimed by acme:gamma`, 2),
		f("L16", "acme:beta", Error, `flow references skill "alpha", which does not exist`, 1),
		f("L21", "acme:beta", Warn, `depends_on: declares "alpha" but no skill of that name exists`, 2),
	}
	changed := []string{filepath.Join(root, "plugins/acme/skills/alpha/SKILL.md")}

	got := Scope(findings, skills, changed).Findings

	if len(got) != 2 || got[0].Rule != "L16" || got[1].Rule != "L21" {
		t.Errorf("Scope kept %+v, want only the findings naming alpha as a skill", got)
	}
}

func TestScopeListsTheTouchedKeysForTheSnapshot(t *testing.T) {
	// A `--format json` snapshot taken under `--changed` must list only the skills its
	// findings cover. Listing the whole corpus next to a narrowed report makes `diff` read
	// every finding the narrowing dropped as resolved.
	root := "/repo"
	skills := dirs(root, "acme:beta", "acme:gamma")
	changed := []string{
		filepath.Join(root, "plugins/acme/skills/gamma/SKILL.md"),
		filepath.Join(root, "plugins/acme/skills/alpha/SKILL.md"), // deleted
	}

	got := Scope(nil, skills, changed).Keys

	if strings.Join(got, ",") != "acme:alpha,acme:gamma" {
		t.Errorf("Keys = %v, want the touched skills sorted, deleted ones included", got)
	}
}
