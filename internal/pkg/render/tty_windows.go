//go:build windows

// Windows-side terminal-size detection. ccsb has no win32-console
// dependency (yet), so on Windows the renderer resolves the size from the
// explicit Config.Width override, then from COLUMNS/LINES (see
// tty_env.go, which carries no build tag for exactly this reason), and
// otherwise treats the terminal width as unknown. The terminal-aware
// layout features (wrap, max_width, min_cols, right-align padding,
// Powerline bg-fill) gracefully degrade to "no detection" mode in that
// case.

package render

import "errors"

// discoverTermSize on Windows honours the explicit Config.Width override,
// then COLUMNS and LINES. It returns (0, 0) when neither is available.
// The rows component is 0 unless LINES supplied one; the tty_size segment
// renders "<width>×0" in that case until win32 console support is added.
//
// Note that this function's behaviour is not exercised by the test suite:
// the pipeline cross-compiles for Windows but runs tests on Linux only.
func discoverTermSize(cfg Config) (cols, rows int) {
	if cfg.Width > 0 {
		return cfg.Width, 0
	}
	if c, r, ok := envWinsizeReader(); ok {
		return c, r
	}
	return 0, 0
}

// errWindowsTTY backs the package-level reader stubs below so a
// caller (or a test) that swaps them can still observe a real,
// stable error sentinel.
var errWindowsTTY = errors.New("ccsb: tty-size detection not supported on windows")

// devTTYWinsizeReader, procStatReader, procFDWinsizeReader are
// package-level vars on the unix side (tty.go). The same variables
// exist here so that render_test.go (which has no build tag) can
// still compile on Windows. They always report failure so a Windows
// build never accidentally claims a tty size from these sources.
var (
	devTTYWinsizeReader = func() (cols, rows int, ok bool) { return 0, 0, false }
	procStatReader      = func(pid int) ([]byte, error) { return nil, errWindowsTTY }
	procFDWinsizeReader = func(path string) (cols, rows int, err error) { return 0, 0, errWindowsTTY }
)
