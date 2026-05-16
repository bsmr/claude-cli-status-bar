package cli

import (
	"runtime/debug"
	"testing"
)

func TestVersionFromBuildInfo_NilReturnsEmpty(t *testing.T) {
	if got := versionFromBuildInfo(nil); got != "" {
		t.Errorf("got %q, want \"\"", got)
	}
}

func TestVersionFromBuildInfo_EmptyVersionReturnsEmpty(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: ""}}
	if got := versionFromBuildInfo(info); got != "" {
		t.Errorf("got %q, want \"\"", got)
	}
}

func TestVersionFromBuildInfo_DevelReturnsEmpty(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}
	if got := versionFromBuildInfo(info); got != "" {
		t.Errorf("got %q, want \"\"", got)
	}
}

func TestVersionFromBuildInfo_TaggedReleaseStripsV(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: "v0.2.8"}}
	if got := versionFromBuildInfo(info); got != "0.2.8" {
		t.Errorf("got %q, want 0.2.8", got)
	}
}

func TestVersionFromBuildInfo_PseudoVersionPassesThrough(t *testing.T) {
	// Pseudo-versions (non-tagged commits) are preserved so the user sees
	// something useful rather than a bare "dev".
	info := &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20240101000000-abcdef012345"}}
	if got := versionFromBuildInfo(info); got != "0.0.0-20240101000000-abcdef012345" {
		t.Errorf("got %q", got)
	}
}
