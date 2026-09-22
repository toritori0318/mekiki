package lint

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/toritori0318/mekiki/internal/skill"
)

// ---- L7: byte-identical files copied across skills (cross-cutting) ----
//
// Two or more skills sharing an md5 under references/ is an error. There are two paths to
// a warn: a canonical/duplicate_of declaration, or a generated banner — if the source of
// truth and the regeneration command are machine-readable, the copy is a managed fan-out
// rather than drift waiting to happen.

const minDupSize = 8 * 1024 // below 8KB, coincidental matches between template fragments dominate

func L7(c *Context) []Finding {
	type entry struct {
		s    *skill.Skill
		path string
	}
	bySum := map[string][]entry{}
	for _, s := range c.Skills {
		refs := filepath.Join(s.Dir, "references")
		filepath.WalkDir(refs, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d == nil || d.IsDir() {
				return nil
			}
			fi, ferr := d.Info()
			if ferr != nil || fi.Size() < minDupSize {
				return nil
			}
			raw, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			sum := md5.Sum(raw)
			key := hex.EncodeToString(sum[:])
			bySum[key] = append(bySum[key], entry{s, p})
			return nil
		})
	}
	var out []Finding
	var keys []string
	for k, v := range bySum {
		if len(v) >= 2 {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		group := bySum[k]
		var others []string
		for _, e := range group {
			others = append(others, e.s.Key())
		}
		for _, e := range group {
			sev := Error
			reason := ""
			switch {
			case e.s.Meta.Str("canonical") != "" || e.s.Meta.Str("duplicate_of") != "":
				sev, reason = Warn, " (declared via canonical/duplicate_of)"
			case hasGeneratedBanner(e.path):
				sev, reason = Warn, " (generated banner present: source and regeneration command are machine-readable)"
			}
			out = append(out, Finding{"L7", sev, e.s.Key(), e.path, 1,
				fmt.Sprintf("references content is byte-identical to %s%s",
					strings.Join(others, " / "), reason)})
		}
	}
	return out
}

// hasGeneratedBanner reports whether the leading comment contains both an
// auto-generated marker and a regeneration/sync command.
//
// Written procedurally rather than as a regexp: Go's RE2 engine has no lookahead, and the
// intent reads more clearly this way.
func hasGeneratedBanner(path string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	head := string(raw)
	if len(head) > 1200 {
		head = head[:1200]
	}
	trimmed := strings.TrimLeft(head, " \t\r\n")
	if !strings.HasPrefix(trimmed, "<!--") {
		return false
	}
	end := strings.Index(trimmed, "-->")
	if end < 0 {
		return false
	}
	comment := trimmed[:end]
	marker := containsAny(comment, "AUTO-GENERATED", "CANONICAL COPY", "自動生成")
	regen := containsAny(comment, "Regenerate", "regenerate", "sync_master", "再生成", "同期")
	return marker && regen
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// ---- L10: build artifacts inside the inspected tree ----
//
// The working tree is walked regardless of git tracking: an untracked distribution zip is
// invisible to `git ls-files`, which is exactly how one slipped into a real corpus.

var artifactExts = []string{".zip", ".tar.gz", ".tgz"}

func L10(c *Context) []Finding {
	var out []Finding
	seen := map[string]bool{}
	for _, root := range c.Roots {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d == nil || d.IsDir() {
				return nil
			}
			name := strings.ToLower(d.Name())
			for _, ext := range artifactExts {
				if strings.HasSuffix(name, ext) && !seen[p] {
					seen[p] = true
					out = append(out, Finding{"L10", Error, ownerSkill(p, c), p, 1,
						fmt.Sprintf("build artifact %s exists inside the inspected tree "+
							"(do not commit build output)", d.Name())})
				}
			}
			return nil
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out
}

// ownerSkill returns the skill an artifact belongs to, or empty when it belongs to none.
func ownerSkill(path string, c *Context) string {
	best := ""
	for _, s := range c.Skills {
		if strings.HasPrefix(path, s.Dir+string(filepath.Separator)) && len(s.Dir) > len(best) {
			best = s.Dir
		}
	}
	for _, s := range c.Skills {
		if s.Dir == best {
			return s.Key()
		}
	}
	return "-" // artifact outside any skill directory
}

// ---- L14: the same skill name defined in multiple plugins ----

func L14(c *Context) []Finding {
	byName := map[string][]*skill.Skill{}
	for _, s := range c.Skills {
		byName[s.Name] = append(byName[s.Name], s)
	}
	var names []string
	for n, group := range byName {
		if len(group) >= 2 {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	var out []Finding
	for _, n := range names {
		group := byName[n]
		declared := false
		for _, s := range group {
			if s.Meta.Str("canonical") != "" || s.Meta.Str("duplicate_of") != "" {
				declared = true
			}
		}
		if declared {
			continue
		}
		var keys []string
		for _, s := range group {
			keys = append(keys, s.Key())
		}
		for _, s := range group {
			out = append(out, Finding{"L14", Error, s.Key(), s.SkillMD(), 2,
				fmt.Sprintf("the same skill name exists in multiple plugins (%s) "+
					"with no canonical declaration", strings.Join(keys, " / "))})
		}
	}
	return out
}

// ---- L16: flow[].skill must name an existing skill (cross-cutting) ----

func L16(c *Context) []Finding {
	names := map[string]bool{}
	for _, s := range c.Skills {
		names[s.Name] = true
	}
	var out []Finding
	for _, s := range c.Skills {
		f, err := LoadFlow(s.Dir)
		if err != nil || f == nil {
			continue // schema problems are L15's job
		}
		seen := map[string]bool{}
		for _, e := range f.Flow {
			if e.Skill == "" || names[e.Skill] || seen[e.Skill] {
				continue
			}
			seen[e.Skill] = true
			out = append(out, Finding{"L16", Error, s.Key(),
				filepath.Join(s.Dir, "flow.json"), 1,
				fmt.Sprintf("flow references skill %q, which does not exist", e.Skill)})
		}
	}
	return out
}

// ---- L21: requires / depends_on must name a skill that exists ----
//
// L16 does this for flow.json delegation targets; requires and depends_on were the remaining
// way one skill can name another, and a rename there fails silently — neither declaration is
// interpreted by the runtime, so nothing complains until a human reads the file and wonders
// why the guard never ran.
//
// Warn rather than error, for two reasons. Lint is often pointed at a single plugin rather
// than the whole estate, which makes a legitimate cross-plugin reference unresolvable here.
// And a configured guard name counts as existing: L8 demands that declaration, so treating
// it as dangling would leave a skill unable to satisfy both rules at once.

func L21(c *Context) []Finding {
	known := map[string]bool{}
	for _, s := range c.Skills {
		known[s.Name] = true
		known[s.Key()] = true
	}
	for g := range c.Config.GuardNames() {
		known[g] = true
	}
	var out []Finding
	for _, s := range c.Skills {
		seen := map[string]bool{}
		for _, field := range []string{"requires", "depends_on"} {
			for _, dep := range s.Meta.List(field) {
				dep = strings.TrimSpace(dep)
				if dep == "" || known[dep] || seen[dep] {
					continue
				}
				seen[dep] = true
				out = append(out, Finding{"L21", Warn, s.Key(), s.SkillMD(), 2,
					fmt.Sprintf("%s: declares %q but no skill of that name exists in the inspected "+
						"corpus (linting only part of the estate? suppress with a reason)", field, dep)})
			}
		}
	}
	return out
}

// ---- L23: two skills claim the same trigger phrase ----
//
// Activation is decided from the description, so two skills quoting the same user request
// are asking the model to pick between them with nothing to go on. At corpus scale this is
// how a skill comes to fire late, or never — the failure the conventions name but no rule
// caught until now.
//
// Only *quoted* phrases are compared, and only from four runes up. Description similarity as
// a whole was considered and left out: it needs a threshold nobody can defend, and the
// measurement to set one honestly does not exist. A quoted phrase, by contrast, is the
// author explicitly writing down the words a user will say, so two authors writing the same
// words is a claim on the same request rather than a coincidence of vocabulary.
//
// Single quotes are excluded on purpose: English prose is full of apostrophes, and a pair of
// them would manufacture a phrase out of ordinary text. A group whose members declare
// canonical/duplicate_of is silent — a managed duplicate is L7 and L14's subject.
//
// What counts as a phrase was set by measurement on a 154-skill corpus, where the first
// version fired 14 times and two of those classes were noise. `"analyze"` is a word rather
// than a request, and a rune floor alone cannot say so: four runes is a whole word in
// English while it is a fragment in Japanese. `"use ${CLAUDE_PLUGIN_ROOT}"` is an
// instruction naming a variable, which no user will ever say. Excluding both leaves six
// findings on that corpus, each a genuine pair of skills claiming one request.

var quotedPhraseRe = regexp.MustCompile(`「([^」]+)」|『([^』]+)』|"([^"]+)"|“([^”]+)”`)

// minTriggerPhraseRunes: below this a quoted fragment is a word rather than a request.
// 「診断」 is shared by every diagnostic skill in a corpus and distinguishes none of them.
const minTriggerPhraseRunes = 4

// isRequestPhrase reports whether a quoted fragment reads as something a user would say.
func isRequestPhrase(p string) bool {
	if len([]rune(p)) < minTriggerPhraseRunes {
		return false
	}
	if strings.Contains(p, "${") {
		return false // a variable reference, not an utterance
	}
	// Japanese requests carry no spaces, so the multi-word test applies only where words are
	// separated: without it every common English verb becomes a collision.
	return strings.ContainsAny(p, " \t") || hasCJK(p)
}

func hasCJK(s string) bool {
	for _, r := range s {
		if (r >= 0x3000 && r <= 0x30FF) || (r >= 0x3400 && r <= 0x4DBF) ||
			(r >= 0x4E00 && r <= 0x9FFF) || (r >= 0xFF00 && r <= 0xFFEF) {
			return true
		}
	}
	return false
}

func L23(c *Context) []Finding {
	claimants := map[string][]*skill.Skill{}
	display := map[string]string{}
	for _, s := range c.Skills {
		text := s.Meta.Str("description") + "\n" + s.Meta.Str("when_to_use")
		seen := map[string]bool{}
		for _, m := range quotedPhraseRe.FindAllStringSubmatch(text, -1) {
			raw := strings.TrimSpace(firstNonEmpty(m[1:]...))
			key := strings.ToLower(raw)
			if seen[key] || !isRequestPhrase(key) {
				continue
			}
			seen[key] = true
			claimants[key] = append(claimants[key], s)
			if _, ok := display[key]; !ok {
				display[key] = raw
			}
		}
	}

	var phrases []string
	for p := range claimants {
		phrases = append(phrases, p)
	}
	sort.Strings(phrases)

	var out []Finding
	for _, p := range phrases {
		// The same key can be discovered twice when a corpus vendors a plugin; that is L14's
		// subject, and reporting it here would say a skill competes with itself.
		var keys []string
		first := map[string]*skill.Skill{}
		declared := false
		for _, s := range claimants[p] {
			if s.Meta.Str("canonical") != "" || s.Meta.Str("duplicate_of") != "" {
				declared = true
			}
			if _, ok := first[s.Key()]; !ok {
				first[s.Key()] = s
				keys = append(keys, s.Key())
			}
		}
		if declared || len(keys) < 2 {
			continue
		}
		for _, k := range keys {
			others := make([]string, 0, len(keys)-1)
			for _, other := range keys {
				if other != k {
					others = append(others, other)
				}
			}
			out = append(out, Finding{"L23", Warn, k, first[k].SkillMD(), 2,
				fmt.Sprintf("trigger phrase %q is also claimed by %s — overlapping triggers "+
					"leave activation to chance", display[p], strings.Join(others, " / "))})
		}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// DelegationTargets is every skill name some flow.json delegates to. Setting
// disable-model-invocation on one would stop its orchestrator calling it, so L20 and the
// judged rule J4 both leave these alone.
func DelegationTargets(skills []*skill.Skill) map[string]bool {
	targets := map[string]bool{}
	for _, s := range skills {
		if f, err := LoadFlow(s.Dir); err == nil && f != nil {
			for _, e := range f.Flow {
				if e.Skill != "" {
					targets[e.Skill] = true
				}
			}
		}
	}
	return targets
}

// ---- L20: autonomous invocation of side-effecting skills (disable-model-invocation) ----
//
// Unlike requires:, this flag is the one activation control the runtime actually enforces.
//
// Detection is deliberately narrowed to billing (irreversible spend). External publication
// and destructive writes are equally required by the convention, but their wording is broad
// enough to hit 68% of a real corpus, which makes the finding unactionable noise.
//
// Two exemptions:
//   - flow.json delegation targets: the flag also blocks programmatic invocation, so
//     setting it would stop the orchestrator from calling the skill. Never break one
//     mechanism in the name of another.
//   - configured guard skills themselves: they describe billing risk without performing it.

func L20(c *Context) []Finding {
	targets := DelegationTargets(c.Skills)
	guards := c.Config.GuardNames()
	var out []Finding
	for _, s := range c.Skills {
		if v, ok := s.Meta.Bool("disable-model-invocation"); ok && v {
			continue
		}
		if targets[s.Name] || guards[s.Name] {
			continue
		}
		text := s.Meta.Str("description") + "\n" + s.Body
		m := c.Config.Pattern("risk_billing").FindString(text)
		if m == "" {
			continue
		}
		out = append(out, Finding{"L20", Warn, s.Key(), s.SkillMD(), 1,
			fmt.Sprintf("billing wording (%q) is present but disable-model-invocation: true "+
				"is not set — the model can start a billable operation on its own", m)})
	}
	return out
}
