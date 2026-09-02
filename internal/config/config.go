// Package config holds org-specific settings: guard skill names and detection patterns.
//
// Why these are not hardcoded: baking org-specific values into the binary makes another
// org's install demand a `requires:` declaration for a skill that does not exist there,
// which fails their CI unconditionally. The same applies to detection patterns — hardcoding
// them to one language makes the heuristic rules silently inert everywhere else.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// DefaultPatterns are bilingual (English + Japanese) so that mekiki is useful on either
// corpus without configuration. Override any key via config.json `patterns`.
//
// Word boundaries are applied deliberately and asymmetrically, because the two families of
// rule fail in opposite directions:
//
//   - Safety rules (risk_*) drive error-severity findings, so a miss is worse than a false
//     positive: a skill that really does spend money must not slip through. Their terms are
//     distinctive enough to stand without boundaries — and boundaries actively hurt here,
//     since Go's RE2 counts "_" as a word character, so `\bbrowser\b` would not match
//     "09_browser-operation-notes.md".
//   - Quality heuristics (deterministic) drive warn-severity findings that a human triages,
//     so noise is the greater cost. These are word-bounded and stemmed narrowly: a bare
//     "ratio" also matches generation, iteration and migration (61 false positives on a real
//     corpus), and a bare "comput" matches "computer.type", which is a tool name rather than
//     a calculation.
var DefaultPatterns = map[string]string{
	"trigger": `(?i)use (this|when)|when the user|invoke this|` +
		`使用|使う|場合|とき|依頼|したい|と言った`,
	"lifecycle": `(?i)\bdeprecated\b|\bobsolete\b|\bsuperseded\b|\blegacy method\b|` +
		`【非推奨】|非推奨|旧方式|置き換え済み`,
	// Every English stem is bounded on both sides. Open-ended stems leak badly here:
	// "comput" matches "computer.type" (a tool name), "divid" matches a "divider" UI block,
	// and an unbounded "ratio" matches generation, iteration and migration.
	"deterministic": `(?i)` +
		`\bcalculat(e|es|ed|ing|ion|ions)\b|` +
		`\bcomput(e|es|ed|ing|ation|ations)\b|` +
		`\bdivid(e|es|ed|ing)\b|\bdivision\b|` +
		`\bthresholds?\b|\bratios?\b|` +
		`\bnormali[sz](e|es|ed|ing|ation)\b|` +
		`\bpars(e|es|ed|ing)\b|` +
		`\baggregat(e|es|ed|ing|ion)\b|` +
		`\breconcil(e|es|ed|ing|iation)\b|` +
		`camelCase|` +
		`計算|閾値|除算|÷|倍以上|突合|正規化|パース|集計|変換.{0,6}(規則|ルール)`,
	"internal": `(?i)internal skill|invoked (only )?(by|from) other skills|` +
		`内部スキル|他スキルから`,
	"risk_billing": `(?i)ad spend|billing|invoice|charge the|budget.{0,12}(set|allocat)|` +
		`広告費|出稿|課金|請求|予算.{0,6}(設定|投入)`,
	"risk_write": `(?i)mutation|delet|uninstall|overwrit|DROP\s|` +
		`external api.{0,12}(write|push|sync)|` +
		`アンインストール|上書き|外部API.{0,10}(write|書き込み|送信|同期)`,
	"risk_browser": `(?i)browser|playwright|puppeteer|headless chrome|` +
		`ブラウザ操作|スクリーンショット操作`,
	"risk_publish": `(?i)publish|send email|broadcast|post to|go live|` +
		`公開|投稿|メール送信|配信`,
}

// Runtime profiles.
//
// Several of the checks encode Claude Code's behaviour rather than the Agent Skills
// specification: the 1,536-character listing cap that L2 measures against is how Claude Code
// concatenates when_to_use onto the description, and L12's allow-list includes that key and
// a dozen other Claude Code extensions. Both are correct there and wrong for a runtime that
// implements the specification and nothing more, where `when_to_use` really is a
// non-standard key.
//
// The default is claude-code, so an install that sets no profile behaves exactly as it did
// before profiles existed.
const (
	ProfileClaudeCode = "claude-code"
	ProfileGeneric    = "generic"
)

// Limits are the numeric thresholds a profile carries. Zero means "not checked".
//
// The body-size limits come from the official guidance ("under 500 lines and 5,000 tokens")
// and are therefore the same under either profile; only listing_cap is runtime-specific.
var profileLimits = map[string]map[string]int{
	ProfileClaudeCode: {"listing_cap": 1536, "max_body_lines": 500, "max_body_tokens": 5000},
	ProfileGeneric:    {"listing_cap": 0, "max_body_lines": 500, "max_body_tokens": 5000},
}

// specKeys are the Agent Skills specification's own frontmatter keys — the set every
// conforming runtime understands.
var specKeys = []string{
	"name", "description", "license", "compatibility", "metadata", "allowed-tools",
}

// claudeCodeKeys are the extensions Claude Code adds on top of the specification.
var claudeCodeKeys = []string{
	"when_to_use", "argument-hint", "arguments", "disable-model-invocation",
	"user-invocable", "disallowed-tools", "model", "effort", "context", "agent",
	"background", "hooks", "paths", "shell",
}

// Config is the settings file plus compiled patterns.
type Config struct {
	Guards   map[string][]string `json:"guards"`
	Patterns map[string]string   `json:"patterns"`
	Profile  string              `json:"profile"`
	Limits   map[string]int      `json:"limits"`

	compiled map[string]*regexp.Regexp
}

// ProfileName returns the effective runtime profile.
func (c *Config) ProfileName() string {
	if c == nil || c.Profile == "" {
		return ProfileClaudeCode
	}
	return c.Profile
}

// Limit returns a numeric threshold. An explicit `limits` entry wins over the profile's
// value; 0 means the check does not apply to this runtime.
func (c *Config) Limit(key string) int {
	if c != nil {
		if v, ok := c.Limits[key]; ok {
			return v
		}
	}
	return profileLimits[c.ProfileName()][key]
}

// OfficialKeys returns the frontmatter keys this runtime understands, which is what L12
// treats as standard.
func (c *Config) OfficialKeys() map[string]bool {
	out := map[string]bool{}
	for _, k := range specKeys {
		out[k] = true
	}
	if c.ProfileName() == ProfileClaudeCode {
		for _, k := range claudeCodeKeys {
			out[k] = true
		}
	}
	return out
}

// Load reads the settings file. A missing file is fine — mekiki then skips the checks that
// depend on org-specific names. An invalid regex yields a note and falls back to the
// bundled default for that key only.
func Load(path string) (*Config, []string) {
	c := &Config{compiled: map[string]*regexp.Regexp{}}
	var notes []string
	if path != "" {
		if raw, err := os.ReadFile(path); err == nil {
			if err := json.Unmarshal(raw, c); err != nil {
				notes = append(notes, fmt.Sprintf("config is not valid JSON (%s): %v", path, err))
				c.Guards, c.Patterns = nil, nil
			}
		}
	}
	if c.Profile != "" {
		if _, ok := profileLimits[c.Profile]; !ok {
			notes = append(notes, fmt.Sprintf(
				"unknown profile %q — falling back to %s (known: %s, %s)",
				c.Profile, ProfileClaudeCode, ProfileClaudeCode, ProfileGeneric))
			c.Profile = ""
		}
	}
	for key, src := range c.Patterns {
		if _, err := regexp.Compile(src); err != nil {
			notes = append(notes, fmt.Sprintf(
				"patterns.%s is not a valid regexp (%v) — falling back to the bundled default", key, err))
			delete(c.Patterns, key)
		}
	}
	return c, notes
}

// Pattern returns the detection regexp for a key (config takes precedence over the default).
func (c *Config) Pattern(key string) *regexp.Regexp {
	if re, ok := c.compiled[key]; ok {
		return re
	}
	src, ok := c.Patterns[key]
	if !ok || src == "" {
		src = DefaultPatterns[key]
	} else if !strings.HasPrefix(src, "(?") {
		// Overrides are case-insensitive by default so users need not enumerate
		// "Use when" / "DEPRECATED" spellings. An explicit flag group is respected.
		src = "(?i)" + src
	}
	re, err := regexp.Compile(src)
	if err != nil { // validated in Load; this is a belt-and-braces fallback
		re = regexp.MustCompile(regexp.QuoteMeta(src))
	}
	if c.compiled == nil {
		c.compiled = map[string]*regexp.Regexp{}
	}
	c.compiled[key] = re
	return re
}

// Guard returns the guard skill names for a risk kind. Empty means the kind is not
// checked — nobody but the org knows the name.
func (c *Config) Guard(kind string) []string {
	if c == nil || c.Guards == nil {
		return nil
	}
	return c.Guards[kind]
}

// GuardNames returns the set of all configured guard skill names.
func (c *Config) GuardNames() map[string]bool {
	out := map[string]bool{}
	if c == nil {
		return out
	}
	for _, names := range c.Guards {
		for _, n := range names {
			out[n] = true
		}
	}
	return out
}
