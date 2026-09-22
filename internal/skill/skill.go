package skill

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Skill is one SKILL.md and its surrounding assets.
type Skill struct {
	Plugin    string // plugin name (parent of skills/, else the grandparent of SKILL.md)
	Name      string // directory name (authoritative)
	Dir       string
	Meta      Meta
	Body      string
	Text      string   // full text (used for suppress comments)
	Problems  []string // frontmatter lines that could not be interpreted
	BodyStart int
	IsNew     bool // absent from baseline, i.e. a new skill
	Broken    bool // frontmatter absent or unterminated
}

// Key is the `plugin:name` identifier.
func (s *Skill) Key() string { return s.Plugin + ":" + s.Name }

// SkillMD is the path to SKILL.md.
func (s *Skill) SkillMD() string { return filepath.Join(s.Dir, "SKILL.md") }

// BodyLine converts a 1-indexed body line number into an absolute SKILL.md line number.
func (s *Skill) BodyLine(i int) int { return s.BodyStart + i - 1 }

// HasDir reports whether a directory exists under the skill.
func (s *Skill) HasDir(rel string) bool {
	fi, err := os.Stat(filepath.Join(s.Dir, rel))
	return err == nil && fi.IsDir()
}

// HasFile reports whether a file exists under the skill.
func (s *Skill) HasFile(rel string) bool {
	fi, err := os.Stat(filepath.Join(s.Dir, rel))
	return err == nil && !fi.IsDir()
}

// Discover enumerates directories containing a SKILL.md under the given paths.
// A symlinked target is evaluated once only, to avoid duplicate findings.
func Discover(paths []string) ([]*Skill, error) {
	var out []*Skill
	seen := map[string]bool{}
	for _, root := range paths {
		root = expand(root)
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // silently skip unreadable paths; the target tree is read-only
			}
			if d.IsDir() || d.Name() != "SKILL.md" {
				return nil
			}
			dir := filepath.Dir(p)
			real, rerr := filepath.EvalSymlinks(dir)
			if rerr != nil {
				real = dir
			}
			if seen[real] {
				return nil
			}
			seen[real] = true

			text, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			s := &Skill{
				Plugin: pluginOf(dir),
				Name:   filepath.Base(dir),
				Dir:    dir,
				Text:   string(text),
			}
			fm, perr := Parse(s.Text)
			if perr != nil {
				s.Broken = true
				s.Meta = Meta{}
				s.Body = s.Text
				s.BodyStart = 1
				s.Problems = []string{perr.Error()}
			} else {
				s.Meta, s.Body, s.Problems, s.BodyStart = fm.Meta, fm.Body, fm.Problems, fm.BodyStart
			}
			out = append(out, s)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out, nil
}

func pluginOf(dir string) string {
	parent := filepath.Dir(dir)
	if filepath.Base(parent) == "skills" {
		return filepath.Base(filepath.Dir(parent))
	}
	return filepath.Base(parent)
}

func expand(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// Expand expands a leading `~/` (also used by the CLI).
func Expand(p string) string { return expand(p) }

// ContractItems are the six required entries of the Contract block. Non-goals matters
// most: it is what prevents scope creep and re-implementation of upstream steps.
var ContractItems = []string{"Trigger", "Inputs", "Preconditions", "Outputs", "Postconditions", "Non-goals"}

var contractItemRe = regexp.MustCompile(`^-\s+\*\*([A-Za-z-]+)\*\*:\s*(.*)`)

// Contract is the parsed I/O contract block from the top of the body.
type Contract struct {
	Present      bool
	FirstHeading bool // whether it is the first ## heading in the body
	Items        map[string]string
	StartLine    int // 1-indexed line within the body
}

// Complete reports whether all six entries are present.
func (c Contract) Complete() bool {
	if !c.Present {
		return false
	}
	for _, k := range ContractItems {
		if _, ok := c.Items[k]; !ok {
			return false
		}
	}
	return true
}

// ParseContract extracts the `## Contract` block from a body.
func ParseContract(body string) Contract {
	lines := strings.Split(body, "\n")
	type heading struct {
		idx  int
		text string
	}
	var headings []heading
	for i, l := range lines {
		if strings.HasPrefix(l, "## ") {
			headings = append(headings, heading{i, strings.TrimSpace(l)})
		}
	}
	target := -1
	for k, h := range headings {
		if h.text == "## Contract" {
			target = k
			break
		}
	}
	if target < 0 {
		return Contract{Items: map[string]string{}}
	}
	start := headings[target].idx
	end := len(lines)
	if target+1 < len(headings) {
		end = headings[target+1].idx
	}
	items := map[string]string{}
	for j := start + 1; j < end; j++ {
		if m := contractItemRe.FindStringSubmatch(lines[j]); m != nil {
			items[m[1]] = m[2]
		}
	}
	return Contract{Present: true, FirstHeading: target == 0, Items: items, StartLine: start + 1}
}

// EstimateTokens approximates token count: 1.2 tokens per CJK rune, 1 per 4 other runes.
//
// This is a proxy for the second half of the official guidance ("under 500 lines and
// 5,000 tokens") without depending on a real tokenizer. The 1.2 factor: Japanese is
// roughly one token per character, and measurements report newer tokenizers consuming
// 15-35% more tokens for CJK than the previous generation.
func EstimateTokens(text string) int {
	cjk, other := 0, 0
	for _, r := range text {
		if isCJK(r) {
			cjk++
		} else {
			other++
		}
	}
	return cjk*6/5 + other/4
}

func isCJK(r rune) bool {
	return (r >= 0x3000 && r <= 0x30FF) ||
		(r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0x4E00 && r <= 0x9FFF) ||
		(r >= 0xFF00 && r <= 0xFFEF)
}

// KeyFor derives the `plugin:name` identifier from a skill directory path, without the
// directory having to exist. A pull request that deletes a skill leaves nothing to
// discover, and that deletion is exactly what a reviewer must be told about.
func KeyFor(dir string) string {
	dir = filepath.Clean(dir)
	return pluginOf(dir) + ":" + filepath.Base(dir)
}
