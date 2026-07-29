// Terminal-size detection from the environment. Deliberately carries no
// build tag: both discoverTermSize implementations use it, and on Windows
// it is the only real source there is.

package render

import (
	"os"
	"strconv"
)

// maxEnvWinsize bounds COLUMNS/LINES. A real TIOCGWINSZ winsize is uint16,
// so nothing legitimate exceeds it; the environment, unlike that ioctl, is
// inherited from an arbitrary process tree. Without the bound a bogus
// COLUMNS would make the renderer build a line that many columns wide.
const maxEnvWinsize = 65535

// envWinsizeReader reads COLUMNS and LINES. ok reports whether a usable
// column count was found; rows is 0 when LINES is absent or unusable,
// matching the Config.Width branch of discoverTermSize, which also reports
// rows 0. Package-level var so tests can swap it, like the readers in
// tty.go.
//
// Claude Code exports both to the statusLine child and keeps them current
// per invocation, so this is the cheapest accurate source available there —
// and on Windows it is the only one.
var envWinsizeReader = func() (cols, rows int, ok bool) {
	cols = parseWinsizeEnv("COLUMNS")
	if cols == 0 {
		return 0, 0, false
	}
	return cols, parseWinsizeEnv("LINES"), true
}

// parseWinsizeEnv returns name's value as a terminal dimension, or 0 when it
// is unset, unparsable, non-positive, or beyond maxEnvWinsize.
func parseWinsizeEnv(name string) int {
	v, err := strconv.Atoi(os.Getenv(name))
	if err != nil || v <= 0 || v > maxEnvWinsize {
		return 0
	}
	return v
}
