package main

import "testing"

func TestResolveVersionPrefersTheBuildStamp(t *testing.T) {
	// An ldflags value is a deliberate statement about which release this is, so it wins over
	// anything the toolchain inferred.
	orig := version
	t.Cleanup(func() { version = orig })

	version = "v1.4.0"
	if got := resolveVersion(); got != "v1.4.0" {
		t.Errorf("resolveVersion() = %q, want the ldflags value", got)
	}

	// Unstamped, the result must still be something a reader can act on: either a module
	// version the toolchain recorded, or the honest "dev" — never an empty string, which
	// would produce a SARIF log naming a tool with no version at all.
	version = "dev"
	if got := resolveVersion(); got == "" || got == "(devel)" {
		t.Errorf("resolveVersion() = %q, want a usable value", got)
	}
}
