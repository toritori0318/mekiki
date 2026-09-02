package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCfg(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadAbsentIsUsable(t *testing.T) {
	// A missing config must not break anything; only org-specific checks are skipped.
	c, notes := Load(filepath.Join(t.TempDir(), "missing.json"))
	if len(notes) != 0 {
		t.Errorf("absent config should produce no notes: %v", notes)
	}
	if got := c.Guard("billing"); got != nil {
		t.Errorf("guards should be empty when unset: %v", got)
	}
}

func TestDefaultPatternsAreBilingual(t *testing.T) {
	// Defaults must work on both English and Japanese corpora without configuration.
	c, _ := Load("")
	cases := []struct{ key, text string }{
		{"trigger", "Use this when the user asks for a chart"},
		{"trigger", "レポートを作ってと依頼した場合に使用する"},
		{"lifecycle", "DEPRECATED legacy reporting flow"},
		{"lifecycle", "【非推奨】旧方式です"},
		{"deterministic", "Compute the ratio and compare against the threshold"},
		{"deterministic", "売上を計算し閾値で判定する"},
		{"risk_billing", "Sets the ad spend budget"},
		{"risk_billing", "広告費の予算を設定する"},
		{"internal", "Internal skill invoked by other skills"},
		{"internal", "内部スキル。他スキルから起動される"},
	}
	for _, tc := range cases {
		if !c.Pattern(tc.key).MatchString(tc.text) {
			t.Errorf("pattern %q did not match %q", tc.key, tc.text)
		}
	}
}

func TestGuardsFromConfig(t *testing.T) {
	c, _ := Load(writeCfg(t, `{"guards":{"billing":["billing-guard"]}}`))
	got := c.Guard("billing")
	if len(got) != 1 || got[0] != "billing-guard" {
		t.Errorf("guards = %v", got)
	}
	if !c.GuardNames()["billing-guard"] {
		t.Error("billing-guard missing from GuardNames")
	}
}

func TestPatternsOverrideReplacesDefault(t *testing.T) {
	c, _ := Load(writeCfg(t, `{"patterns":{"trigger":"fire on|activate for"}}`))
	if !c.Pattern("trigger").MatchString("Fire on any CSV request") {
		t.Error("override should match case-insensitively")
	}
	if c.Pattern("trigger").MatchString("使用する") {
		t.Error("override must replace, not extend, the default")
	}
}

func TestInvalidPatternFallsBackWithNote(t *testing.T) {
	c, notes := Load(writeCfg(t, `{"patterns":{"trigger":"(["}}`))
	found := false
	for _, n := range notes {
		if strings.Contains(n, "patterns.trigger") {
			found = true
		}
	}
	if !found {
		t.Fatalf("an invalid regexp must be reported as a note: %v", notes)
	}
	if !c.Pattern("trigger").MatchString("使用する") {
		t.Error("did not fall back to the bundled default")
	}
}
