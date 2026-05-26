//go:build !windows

// Unix-side terminal-size detection. The /dev/tty open + TIOCGWINSZ
// ioctl and the /proc parent-chain walk are Linux/Darwin/BSD features;
// Windows uses the stub in tty_windows.go which always reports an
// unknown size unless cfg.Width is set explicitly.

package render

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

// procWalkDepth caps the parent-process traversal in walkProcForTTY so a
// malformed /proc cannot pin the renderer in an unbounded loop.
const procWalkDepth = 16

// walkProcForTTY walks the parent-PID chain starting at pid, bounded by
// maxDepth. For each ancestor whose /proc/<pid>/stat line reports a
// non-zero tty_nr, it opens /proc/<pid>/fd/0 via sizeReader and runs
// TIOCGWINSZ. The first success wins. Returns (0, 0) when the walk
// reaches PID 1, exhausts maxDepth, or any statReader call fails.
// A sizeReader failure for one ancestor does NOT stop the walk —
// the next ancestor with tty_nr != 0 still gets a chance.
func walkProcForTTY(
	pid int,
	maxDepth int,
	statReader func(pid int) ([]byte, error),
	sizeReader func(path string) (cols, rows int, err error),
) (cols, rows int) {
	for depth := 0; depth < maxDepth && pid > 1; depth++ {
		content, err := statReader(pid)
		if err != nil {
			return 0, 0
		}
		ppid, ttyNr, err := parseProcStat(content)
		if err != nil {
			return 0, 0
		}
		if ttyNr != 0 {
			if c, r, err := sizeReader(fmt.Sprintf("/proc/%d/fd/0", pid)); err == nil {
				return c, r
			}
		}
		pid = ppid
	}
	return 0, 0
}

// devTTYWinsizeReader opens /dev/tty and runs TIOCGWINSZ. Returns
// (0, 0, false) on any error. Package-level var so tests can swap it.
var devTTYWinsizeReader = func() (cols, rows int, ok bool) {
	f, err := os.Open("/dev/tty")
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return 0, 0, false
	}
	return int(ws.Col), int(ws.Row), true
}

// procStatReader reads /proc/<pid>/stat. Package-level var so tests
// can swap it.
var procStatReader = func(pid int) ([]byte, error) {
	return os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
}

// procFDWinsizeReader opens path (typically /proc/<pid>/fd/0) and runs
// TIOCGWINSZ. Package-level var so tests can swap it.
var procFDWinsizeReader = func(path string) (cols, rows int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return 0, 0, err
	}
	return int(ws.Col), int(ws.Row), nil
}

// discoverTermSize returns the detected terminal cols×rows. Detection
// order, first non-zero cols wins: Config.Width > /dev/tty > /proc
// parent-chain walk from PPID > (0, 0). The rows component may be 0
// even when cols is non-zero (the Config.Width branch only sets cols).
// Callers receive (0, 0) when every source fails — that signals "size
// unknown" and downstream renderers (Powerline pad, tty_size segment)
// gracefully degrade.
func discoverTermSize(cfg Config) (cols, rows int) {
	if cfg.Width > 0 {
		return cfg.Width, 0
	}
	if c, r, ok := devTTYWinsizeReader(); ok {
		return c, r
	}
	return walkProcForTTY(os.Getppid(), procWalkDepth, procStatReader, procFDWinsizeReader)
}

// parseProcStat extracts the PPID (field 4 of /proc/<pid>/stat) and the
// controlling tty device number (field 7) from one stat line. The comm
// field (parenthesised, field 2) may contain spaces and parens, so the
// parser splits the line on the LAST ')' before tokenising.
func parseProcStat(content []byte) (ppid int, ttyNr int, err error) {
	last := bytes.LastIndexByte(content, ')')
	if last < 0 {
		return 0, 0, errors.New("parseProcStat: no closing paren")
	}
	fields := bytes.Fields(content[last+1:])
	if len(fields) < 5 {
		return 0, 0, fmt.Errorf("parseProcStat: only %d fields after comm", len(fields))
	}
	// Layout after ')': state(0) ppid(1) pgrp(2) session(3) tty_nr(4) ...
	ppid, err = strconv.Atoi(string(fields[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("parseProcStat: ppid: %w", err)
	}
	ttyNr, err = strconv.Atoi(string(fields[4]))
	if err != nil {
		return 0, 0, fmt.Errorf("parseProcStat: tty_nr: %w", err)
	}
	return ppid, ttyNr, nil
}
