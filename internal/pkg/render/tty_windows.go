//go:build windows

// Windows-side terminal-size detection. ccsb has no win32-console
// dependency (yet), so on Windows the renderer falls back to the
// explicit Config.Width override or treats the terminal width as
// unknown. The terminal-aware layout features (wrap, max_width,
// min_cols, right-align padding, Powerline bg-fill) gracefully
// degrade to "no detection" mode in that case.

package render

import "errors"

// discoverTermSize on Windows honours the explicit Config.Width
// override and otherwise returns (0, 0). The rows component is
// always 0 here; the tty_size segment will render as "<width>×0"
// until win32 console support is added.
func discoverTermSize(cfg Config) (cols, rows int) {
	if cfg.Width > 0 {
		return cfg.Width, 0
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
