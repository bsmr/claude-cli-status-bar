package render

import (
	"os"
	"runtime/debug"
	"testing"
)

// TestMain neutralises the build-info default for the whole package. Without
// it every test would run as a "local build" — go test compiles inside the
// VCS tree — and the blocked-update glyph would replace ↑ in the existing
// version-segment assertions. Tests that exercise blocking opt in explicitly
// via stubBuildInfo.
func TestMain(m *testing.M) {
	buildInfoFunc = func() (*debug.BuildInfo, bool) { return &debug.BuildInfo{}, true }
	os.Exit(m.Run())
}
