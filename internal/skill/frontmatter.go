// Package skill discovers SKILL.md files and interprets their frontmatter.
package skill

import (
	"errors"
	"regexp"
	"strings"
)

// Meta is a one-level view of the frontmatter. Values are string, bool, []string or
// map[string]string. No YAML library is used: keeping the binary dependency-free keeps its
// supply chain minimal, which matters for a tool whose subject is supply-chain discipline.
type Meta map[string]any

// Str returns a string value (empty when unset or not a string).
func (m Meta) Str(key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// Bool returns the boolean value and whether the key existed as a boolean.
func (m Meta) Bool(key string) (bool, bool) {
	v, ok := m[key].(bool)
	return v, ok
}

// List returns a string list. A bare string is treated as a single-element list so that
// `requires: guard` and `requires: [guard]` behave identically.
func (m Meta) List(key string) []string {
	switch v := m[key].(type) {
	case []string:
		return v
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	}
	return nil
}

// Has reports whether the key is present.
func (m Meta) Has(key string) bool {
	_, ok := m[key]
	return ok
}

// Keys returns the key set in unspecified order (callers sort).
func (m Meta) Keys() []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Frontmatter is the parse result.
type Frontmatter struct {
	Meta      Meta
	Body      string   // body with the frontmatter block removed
	Problems  []string // lines we could not interpret (parse leniently, report as warnings)
	BodyStart int      // 1-indexed line where the body starts (for finding line offsets)
}

var (
	keyRe   = regexp.MustCompile(`^([A-Za-z0-9_-]+):(?:[ \t]+(.*))?$`)
	blockRe = regexp.MustCompile(`^[|>][+-]?\d*$`)
)

// ErrNoFrontmatter is returned when frontmatter is absent or unterminated.
var ErrNoFrontmatter = errors.New("frontmatter is missing or unterminated")

func scalar(v string) any {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && v[0] == v[len(v)-1] && (v[0] == '"' || v[0] == '\'') {
		return v[1 : len(v)-1]
	}
	switch v {
	case "true":
		return true
	case "false":
		return false
	}
	return v
}

func scalarStr(v string) string {
	if s, ok := scalar(v).(string); ok {
		return s
	}
	return strings.TrimSpace(v)
}

func indented(line string) bool {
	return line != "" && (line[0] == ' ' || line[0] == '\t')
}

// Parse interprets a whole SKILL.md. It handles only the YAML subset that real skill
// corpora use: flat key: value, one level of nesting, inline lists, `- ` item lists and
// block scalars (| and >).
func Parse(text string) (*Frontmatter, error) {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, ErrNoFrontmatter
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, ErrNoFrontmatter
	}

	fm := lines[1:end]
	out := &Frontmatter{
		Meta:      Meta{},
		Body:      strings.Join(lines[end+1:], "\n"),
		BodyStart: end + 2,
	}

	for i := 0; i < len(fm); {
		line := fm[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			i++
			continue
		}
		if indented(line) {
			out.problem(trimmed)
			i++
			continue
		}
		m := keyRe.FindStringSubmatch(line)
		if m == nil {
			out.problem(trimmed)
			i++
			continue
		}
		key, raw := m[1], strings.TrimRight(m[2], " \t")

		// Block scalar: join the following indented lines into a single value.
		if blockRe.MatchString(raw) {
			var block []string
			i++
			for i < len(fm) && (strings.TrimSpace(fm[i]) == "" || indented(fm[i])) {
				if s := strings.TrimSpace(fm[i]); s != "" {
					block = append(block, s)
				}
				i++
			}
			joiner := "\n"
			if strings.HasPrefix(raw, ">") {
				joiner = " "
			}
			out.Meta[key] = strings.TrimSpace(strings.Join(block, joiner))
			continue
		}

		// Empty value: either a `- ` item list or a one-level nested map.
		if raw == "" {
			var items []string
			sub := map[string]string{}
			i++
			for i < len(fm) && (strings.TrimSpace(fm[i]) == "" || indented(fm[i])) {
				s := strings.TrimSpace(fm[i])
				switch {
				case s == "":
				case strings.HasPrefix(s, "- "):
					items = append(items, scalarStr(s[2:]))
				default:
					if m2 := keyRe.FindStringSubmatch(s); m2 != nil {
						sub[m2[1]] = scalarStr(m2[2])
					} else {
						out.problem(s)
					}
				}
				i++
			}
			switch {
			case len(items) > 0:
				out.Meta[key] = items
			case len(sub) > 0:
				out.Meta[key] = sub
			default:
				out.Meta[key] = ""
			}
			continue
		}

		// Inline list.
		if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
			inner := strings.TrimSpace(raw[1 : len(raw)-1])
			var items []string
			if inner != "" {
				for _, x := range strings.Split(inner, ",") {
					if strings.TrimSpace(x) != "" {
						items = append(items, scalarStr(x))
					}
				}
			}
			if items == nil {
				items = []string{}
			}
			out.Meta[key] = items
			i++
			continue
		}

		out.Meta[key] = scalar(raw)
		i++
	}
	return out, nil
}

func (f *Frontmatter) problem(line string) {
	if len(line) > 40 {
		line = line[:40]
	}
	f.Problems = append(f.Problems, "uninterpretable line: "+line)
}
