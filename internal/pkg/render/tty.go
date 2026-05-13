package render

import (
	"os"

	"golang.org/x/sys/unix"
)

// readTTYCols returns the number of columns of the controlling
// terminal, or 0 if the size cannot be determined.
//
// ccsb's stdin is the JSON payload pipe and its stdout is the pipe to
// Claude Code, so neither carries terminal dimensions. /dev/tty is the
// controlling terminal of the spawned process, which Claude Code
// inherits from its own controlling tty. If the process has no
// controlling tty or the ioctl fails, this returns 0 and the
// Powerline renderer falls back to natural width.
func readTTYCols() int {
	f, err := os.Open("/dev/tty")
	if err != nil {
		return 0
	}
	defer f.Close()
	ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return 0
	}
	return int(ws.Col)
}

// ttyColsFunc is the indirection point so tests can swap in a
// deterministic fake. Production code calls ttyColsFunc, never
// readTTYCols directly.
var ttyColsFunc = readTTYCols
