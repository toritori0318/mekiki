package lint

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/toritori0318/mekiki/internal/skill"
)

// ScopeResult is a report narrowed to one change.
type ScopeResult struct {
	Findings []Finding
	Notes    []string
	Keys     []string // the touched skills, sorted, deleted ones included
	Skills   int      // len(Keys)
}

// Scope narrows findings to the skills a change touched.
//
// Every rule still runs over the whole corpus — the cross-cutting ones (duplicated
// references, a flow naming a skill that does not exist, two skills claiming the same
// trigger) cannot say anything without seeing every skill. Only the report is narrowed.
//
// dirs maps each discovered `plugin:name` to its directory.
func Scope(findings []Finding, dirs map[string]string, changed []string) ScopeResult {
	touched := map[string]bool{}
	claimed := map[string]bool{} // changed paths that fell inside some skill
	for key, d := range dirs {
		dir := abs(d) + string(filepath.Separator)
		for _, p := range changed {
			if strings.HasPrefix(abs(p)+string(filepath.Separator), dir) {
				touched[key] = true
				claimed[p] = true
			}
		}
	}

	// A skill the change deleted is discoverable nowhere, so its own findings are gone. It is
	// still the most consequential thing a skills pull request can do.
	//
	// Absent from dirs is not the same as deleted, so the file itself is what decides. The
	// change is read from the whole repository while dirs holds only what the linted PATH
	// discovered, and PATH may be one plugin or one skill directory; a skill edited outside it
	// is missing from dirs and still right there on disk. Trusting dirs alone reported every
	// such edit as a deletion — the loudest note the report has, on the calmest change.
	var removed []string
	for _, p := range changed {
		if claimed[p] || filepath.Base(p) != "SKILL.md" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			continue // outside the linted path, not gone: counted below as unexamined
		}
		claimed[p] = true
		key := skill.KeyFor(filepath.Dir(p))
		touched[key] = true
		removed = append(removed, key)
	}
	sort.Strings(removed)

	// A rule that names another skill reports on the skill that is broken, not on the one
	// that broke it: deleting alpha produces an L16 finding on beta. Scoping by the finding's
	// own skill alone would drop exactly the damage the change did.
	names := map[string]bool{}
	for k := range touched {
		if _, name, ok := strings.Cut(k, ":"); ok {
			names[name] = true
		}
	}

	out := []Finding{}
	for _, f := range findings {
		if touched[f.Skill] || mentionsAny(f.Message, touched, names) {
			out = append(out, f)
		}
	}

	var notes []string
	for _, k := range removed {
		notes = append(notes, "skill "+k+" is no longer present at this revision "+
			"(its own findings cannot appear below; findings it broke elsewhere can)")
	}
	if len(touched) == 0 {
		notes = append(notes, "no skill was touched by this change; "+
			"an empty report here means the change never reached a skill, not that the corpus is clean")
	}
	if n := len(changed) - len(claimed); n > 0 {
		notes = append(notes, fmt.Sprintf(
			"%d changed file(s) belong to no skill and were not examined", n))
	}
	keys := make([]string, 0, len(touched))
	for k := range touched {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return ScopeResult{Findings: out, Notes: notes, Keys: keys, Skills: len(keys)}
}

// abs normalises a path for comparison. Two differences have to be absorbed before a changed
// file can be matched against a skill directory: a corpus given as `skills/` is discovered
// relative to the working directory while git reports absolute paths, and git resolves every
// symlink while the path as typed does not (macOS temp directories, a checkout under a linked
// home). Left unnormalised, either one makes every touched skill look deleted.
//
// The tail is kept unresolved on purpose: a file the change deleted no longer exists, and
// that deletion is the case the report must not lose.
func abs(p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	rest := ""
	for cur := a; ; {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return a
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// mentionsAny reports whether a finding's message names one of the touched skills, either by
// its `plugin:name` key or by bare name in the two forms a rule uses to point at another skill:
// a flow target (`skill "name"`) or a declared dependency (`declares "name"`). A bare quoted
// name on its own is not enough — a trigger phrase equal to a skill's name is a collision
// between two other skills, not this change's doing.
func mentionsAny(msg string, keys, names map[string]bool) bool {
	for k := range keys {
		if strings.Contains(msg, k) {
			return true
		}
	}
	for n := range names {
		q := `"` + n + `"`
		if strings.Contains(msg, "skill "+q) || strings.Contains(msg, "declares "+q) {
			return true
		}
	}
	return false
}

// DefaultBase is what `--changed` compares against when `--base` is not given. It is the
// remote's default branch, which is the base of a pull request and needs no typing.
const DefaultBase = "origin/HEAD"

// ChangedFiles returns the absolute paths a change touched: everything that differs from the
// merge base with base, plus files not yet added to the index. A local check is usually
// uncommitted and a pull request is usually committed, and both are "the change".
func ChangedFiles(dir, base string) ([]string, error) {
	top, err := gitOut(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("%s is not inside a git repository, so there is no change to scope to", dir)
	}
	point, err := gitOut(dir, "merge-base", base, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("cannot resolve the base revision %q; name an existing one with --base "+
			"(for example --base main, or a commit SHA)", base)
	}

	seen := map[string]bool{}
	var out []string
	collect := func(args ...string) error {
		raw, err := gitOut(dir, args...)
		if err != nil {
			return err
		}
		for _, rel := range strings.Split(raw, "\x00") {
			if rel == "" || seen[rel] {
				continue
			}
			seen[rel] = true
			out = append(out, filepath.Join(top, rel))
		}
		return nil
	}
	if err := collect("diff", "--name-only", "-z", point); err != nil {
		return nil, err
	}
	if err := collect("ls-files", "--others", "--exclude-standard", "-z"); err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	raw, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.Trim(string(raw), "\n"), nil
}

// Summary counts the scoped report. It is what the exit code follows under `--changed`:
// failing a pull request on an error it neither introduced nor touched is the behaviour
// scoping exists to remove.
func (r ScopeResult) Summary() Summary {
	s := Summary{Skills: r.Skills}
	for _, f := range r.Findings {
		if f.Severity == Error {
			s.Errors++
		} else {
			s.Warnings++
		}
	}
	return s
}
