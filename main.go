// Command mekiki inspects Agent Skills (SKILL.md) against the SkillProtocol conventions.
//
// mekiki (目利き) is Japanese for the trained eye that judges quality and authenticity at a
// glance — which is what these subcommands do to a skill corpus.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/toritori0318/mekiki/internal/atlas"
	"github.com/toritori0318/mekiki/internal/config"
	"github.com/toritori0318/mekiki/internal/lint"
	"github.com/toritori0318/mekiki/internal/scaffold"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

// resolveVersion reports the version to print and to stamp into a SARIF log.
//
// A release build sets `version` through ldflags. `go install <module>@<tag>` — the way the
// README tells people to install this — does not, and would otherwise report "dev" forever,
// including in the SARIF log where the version names the tool that produced the findings.
// The toolchain records the module version in the build info, so read it there instead.
func resolveVersion() string {
	if version != "dev" {
		return version // an explicit ldflags value always wins
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		// "(devel)" is what a `go build` from a working tree reports; it is no more
		// informative than "dev" and reads like a real version, so keep ours.
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return version
}

const usage = `mekiki — a discerning eye for your agent skills

Usage:
  mekiki lint  [PATH...]        check a skill corpus against the conventions
                                --changed narrows the report to the skills a change touched
  mekiki new   NAME             generate a convention-compliant skill skeleton
  mekiki atlas [PATH...]        build the Skill Atlas (single self-contained HTML page)
  mekiki diff  BASE HEAD        report what two lint snapshots added and resolved
  mekiki version

Run "mekiki <command> -h" for the flags of a command.

Exit codes: 0 = no errors, 1 = errors present, 2 = usage or runtime failure.
Warnings never fail the build: rules that can produce false positives are warn-only
by design, and are meant to be reviewed rather than to block a merge.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	args := os.Args[2:]
	var code int
	switch os.Args[1] {
	case "lint":
		code = cmdLint(args)
	case "new":
		code = cmdNew(args)
	case "atlas":
		code = cmdAtlas(args)
	case "diff":
		code = cmdDiff(args)
	case "version", "--version", "-v":
		fmt.Println("mekiki", resolveVersion())
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		code = 2
	}
	os.Exit(code)
}

// ---- shared flag helpers ----

type flagSet struct {
	name  string
	about string
	flags map[string]*string
	bools map[string]*bool
	help  map[string]string
	order []string
	args  []string
}

func newFlagSet(name, about string) *flagSet {
	return &flagSet{name: name, about: about,
		flags: map[string]*string{}, bools: map[string]*bool{}, help: map[string]string{}}
}

func (f *flagSet) str(name, def, help string) *string {
	v := def
	f.flags[name] = &v
	f.help[name] = help
	f.order = append(f.order, name)
	return &v
}

func (f *flagSet) bool(name, help string) *bool {
	v := false
	f.bools[name] = &v
	f.help[name] = help
	f.order = append(f.order, name)
	return &v
}

// parse handles `--flag value`, `--flag=value` and boolean `--flag`, and treats everything
// else as a positional argument. Written by hand to keep the binary dependency-free.
func (f *flagSet) parse(argv []string) error {
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "-h" || a == "--help" {
			f.printUsage(os.Stdout)
			os.Exit(0)
		}
		if !strings.HasPrefix(a, "--") {
			f.args = append(f.args, a)
			continue
		}
		name, inline, hasInline := strings.Cut(strings.TrimPrefix(a, "--"), "=")
		if p, ok := f.bools[name]; ok {
			*p = true
			continue
		}
		p, ok := f.flags[name]
		if !ok {
			return fmt.Errorf("unknown flag --%s", name)
		}
		if hasInline {
			*p = inline
			continue
		}
		if i+1 >= len(argv) {
			return fmt.Errorf("flag --%s needs a value", name)
		}
		i++
		*p = argv[i]
	}
	return nil
}

func (f *flagSet) printUsage(w *os.File) {
	fmt.Fprintf(w, "mekiki %s — %s\n\nFlags:\n", f.name, f.about)
	for _, n := range f.order {
		fmt.Fprintf(w, "  --%-12s %s\n", n, f.help[n])
	}
}

// defaultPath returns a path next to the executable's module root, used for baseline and
// config defaults. Falls back to the working directory.
func defaultPath(name string) string {
	if exe, err := os.Executable(); err == nil {
		if p := filepath.Join(filepath.Dir(exe), name); fileExists(p) {
			return p
		}
	}
	return name
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func resolvePaths(args []string) ([]string, error) {
	paths := args
	if len(paths) == 0 {
		if env := os.Getenv("MEKIKI_TARGET"); env != "" {
			paths = []string{env}
		}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no path given (pass a path or set MEKIKI_TARGET)")
	}
	for _, p := range paths {
		if _, err := os.Stat(expandHome(p)); err != nil {
			return nil, fmt.Errorf("path does not exist: %s", p)
		}
	}
	return paths, nil
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// ---- mekiki lint ----

func cmdLint(argv []string) int {
	fs := newFlagSet("lint", "check a skill corpus against the conventions")
	format := fs.str("format", "text", "output format: text | json | sarif")
	severity := fs.str("severity", "warn", "lowest severity to display (does not affect the exit code)")
	baseline := fs.str("baseline", defaultPath("baseline.json"),
		"baseline file marking pre-existing skills")
	cfgPath := fs.str("config", defaultPath("config.json"),
		"org-specific settings (guard names, detection patterns)")
	update := fs.bool("update-baseline", "freeze the current skills as pre-existing and exit")
	changed := fs.bool("changed", "report only the skills this change touched (see --base)")
	base := fs.str("base", lint.DefaultBase, "revision --changed measures the change against")
	if err := fs.parse(argv); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	paths, err := resolvePaths(fs.args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	if *update {
		n, err := lint.WriteBaseline(paths, *baseline)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		fmt.Printf("baseline written: %s (%d skills)\n", *baseline, n)
		return 0
	}

	res, err := lint.Run(lint.Options{Paths: paths, BaselinePath: *baseline, ConfigPath: *cfgPath})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	sum := res.Summary()
	found := res.Findings
	if *changed {
		// The repository is located from the first inspected path, so the corpus may sit in a
		// checkout that is not the working directory.
		files, err := lint.ChangedFiles(gitDirFor(paths[0]), *base)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		scoped := lint.Scope(found, res.SkillDirs, files)
		found, sum = scoped.Findings, scoped.Summary()
		res.Notes = append(res.Notes, scoped.Notes...)
	}
	shown := found
	if *severity == "error" {
		shown = nil
		for _, f := range found {
			if f.Severity == lint.Error {
				shown = append(shown, f)
			}
		}
	}

	switch {
	case *format == "sarif":
		raw, err := lint.SARIF(shown, resolveVersion())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		os.Stdout.Write(raw)
	case *format == "json":
		out := lint.Snapshot{Summary: sum, Findings: shown, Skills: res.SkillKeys, Notes: res.Notes}
		if out.Findings == nil {
			out.Findings = []lint.Finding{}
		}
		if out.Skills == nil {
			out.Skills = []string{}
		}
		if out.Notes == nil {
			out.Notes = []string{}
		}
		raw, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(raw))
	default:
		for _, n := range res.Notes {
			fmt.Fprintln(os.Stderr, "note:", n)
		}
		for _, f := range shown {
			fmt.Printf("%-5s %-4s %s  %s:%d  %s\n",
				f.Severity, f.Rule, f.Skill, f.File, f.Line, f.Message)
		}
		fmt.Printf("\n%d errors, %d warnings (%d skills)\n", sum.Errors, sum.Warnings, sum.Skills)
	}
	if sum.Errors > 0 {
		return 1
	}
	return 0
}

// gitDirFor returns the directory git should be run from for an inspected path. A file is
// asked about from the directory holding it.
func gitDirFor(p string) string {
	p = expandHome(p)
	if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
		return filepath.Dir(p)
	}
	return p
}

// ---- mekiki new ----

func cmdNew(argv []string) int {
	fs := newFlagSet("new", "generate a convention-compliant skill skeleton")
	out := fs.str("out", ".", "parent directory to create the skill in")
	kind := fs.str("type", "action", "skill kind: action | knowledge | util")
	risk := fs.str("risk", "", "risk tier: billing | write | browser | publish")
	cfgPath := fs.str("config", defaultPath("config.json"), "settings file holding guard names")
	if err := fs.parse(argv); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if len(fs.args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: mekiki new NAME [--out DIR] [--type KIND] [--risk TIER]")
		return 2
	}
	if *risk != "" {
		switch *risk {
		case scaffold.RiskBilling, scaffold.RiskWrite, scaffold.RiskBrowser, scaffold.RiskPublish:
		default:
			fmt.Fprintf(os.Stderr, "--risk must be one of: billing, write, browser, publish (got %q)\n", *risk)
			return 2
		}
	}
	cfg, notes := config.Load(*cfgPath)
	for _, n := range notes {
		fmt.Fprintln(os.Stderr, "note:", n)
	}

	res, err := scaffold.Generate(scaffold.Options{
		Name: fs.args[0], Out: expandHome(*out), Kind: scaffold.Kind(*kind), Risk: *risk, Cfg: cfg,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("created:", res.Dir)
	fmt.Println("Before you fill this in: a new skill is a last resort. If an existing skill's " +
		"Gotchas or description, a line in the agent instructions, or a paths/hooks setting " +
		"would do, delete this and grow the existing skill instead — the always-on catalog " +
		"cost scales with the number of skills.")
	if res.RiskSet && len(res.Guards) == 0 {
		fmt.Printf("warning: no guard configured for --risk; set guards.* in %s\n", *cfgPath)
	}
	fmt.Printf("next: fill in the %d [TODO] markers in SKILL.md "+
		"(the description drives activation — see the three-part order in the template)\n", res.TODOCount)
	fmt.Println("      then the two eval files: evals.json (output quality) and " +
		"eval_queries.json (trigger accuracy)")
	fmt.Println("      once you have run them, record the outcome in evals/results.json — " +
		"lint and the Atlas read it, and a skill only counts as verified when it passes")
	return 0
}

// ---- mekiki atlas ----

func cmdAtlas(argv []string) int {
	fs := newFlagSet("atlas", "build the Skill Atlas as a single self-contained HTML page")
	// The default writes to the working directory on purpose: the page embeds every
	// description in the corpus, so it must never default into a tracked location.
	out := fs.str("out", "skill-atlas.html", "output HTML path")
	cfgPath := fs.str("config", defaultPath("config.json"), "org-specific settings")
	baseline := fs.str("baseline", defaultPath("baseline.json"), "baseline file")
	base := fs.str("base", "",
		"lint snapshot (--format json) to mark the delta against; omitted means no delta")
	if err := fs.parse(argv); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	paths, err := resolvePaths(fs.args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	m, err := atlas.Build(paths, *cfgPath, *baseline, expandHome(*base))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	html, err := atlas.Render(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	dest := expandHome(*out)
	if dir := filepath.Dir(dest); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	if err := os.WriteFile(dest, []byte(html), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Printf("wrote %s (%d skills)\n", dest, len(m.Skills))
	return 0
}

// ---- mekiki diff ----

func cmdDiff(argv []string) int {
	fs := newFlagSet("diff", "report what two lint snapshots added and resolved")
	format := fs.str("format", "text", "output format: text | json | sarif")
	if err := fs.parse(argv); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if len(fs.args) != 2 {
		fmt.Fprintln(os.Stderr,
			"usage: mekiki diff BASE.json HEAD.json   (snapshots from `mekiki lint --format json`)")
		return 2
	}
	base, err := lint.LoadSnapshot(fs.args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	head, err := lint.LoadSnapshot(fs.args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	d := lint.Compare(base, head)

	switch {
	case *format == "sarif":
		// Added findings only: annotating the inherited backlog on every pull request is
		// exactly what `diff` exists to avoid.
		raw, err := lint.SARIF(d.Added, resolveVersion())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		os.Stdout.Write(raw)
	case *format == "json":
		raw, _ := json.MarshalIndent(d, "", "  ")
		fmt.Println(string(raw))
	default:
		for _, f := range d.Added {
			fmt.Printf("added    %-5s %-4s %s  %s\n", f.Severity, f.Rule, f.Skill, f.Message)
		}
		for _, f := range d.Resolved {
			fmt.Printf("resolved %-5s %-4s %s  %s\n", f.Severity, f.Rule, f.Skill, f.Message)
		}
		if d.Summary.Added == 0 && d.Summary.Resolved == 0 {
			fmt.Println("no change")
		} else {
			fmt.Printf("\nadded %d (%d errors), resolved %d\n",
				d.Summary.Added, d.Summary.AddedErrors, d.Summary.Resolved)
		}
	}
	// Only added errors fail: an added warning is information for the reviewer, not a gate.
	if d.Summary.AddedErrors > 0 {
		return 1
	}
	return 0
}
