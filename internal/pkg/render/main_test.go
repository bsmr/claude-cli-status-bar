package render

import (
	"os"
	"runtime/debug"
	"testing"
)

// TestMain pins the unblocked build-info default for the whole package, so
// the version-segment assertions that expect ↑ cannot be flipped to ⊘ by
// whatever the test binary happens to report.
//
// Measured, not assumed: `go test` does NOT stamp vcs.* into test binaries
// (verify with `go test -c -o x . && go version -m x`), so today the default
// is already unblocked and this is belt-and-braces. It is kept because the
// alternative is seven assertions silently depending on a toolchain detail
// nobody restates when it changes. Tests that exercise blocking opt in
// explicitly via stubBuildInfo.
//
// It also clears COLUMNS and LINES for the whole package, so no test
// silently depends on the terminal that happens to run `go test`: the
// env stage of discoverTermSize would otherwise answer for every render
// call that leaves Config.Width at its zero value. Tests that need the
// variables set use t.Setenv, which restores the prior value (here,
// unset) per test.
func TestMain(m *testing.M) {
	buildInfoFunc = func() (*debug.BuildInfo, bool) { return &debug.BuildInfo{}, true }
	os.Unsetenv("COLUMNS")
	os.Unsetenv("LINES")
	os.Exit(m.Run())
}
