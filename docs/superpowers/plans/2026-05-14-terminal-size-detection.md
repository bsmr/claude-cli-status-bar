# Terminal-Size Detection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Auto-detect the terminal column and row count via `/dev/tty` ioctl plus a `/proc` parent-process walk fallback, with an explicit `Config.Width` override and a new `tty_size` diagnostic segment.

**Architecture:** Bottom-up TDD. Start with the pure parser (`parseProcStat`), build the testable walker (`walkProcForTTY`) on top, wrap both in the orchestrator (`discoverTermSize`) that consults three function-pointer-backed readers (`devTTYWinsizeReader`, `procStatReader`, `procFDWinsizeReader`). Wire the orchestrator into `Render` to replace the 0.2.0 `ttyColsFunc` path, retiring the old API. Add the `tty_size` segment last so it composes against the established detection pipeline.

**Tech Stack:** Go, package `go.muehmer.eu/claude-cli-status-bar/internal/pkg/render` (in-package tests). Existing deps `golang.org/x/sys/unix` and `github.com/mattn/go-runewidth` are reused; no new dependencies.

**Branch:** `development-0.2.2-work` already exists with spec commit `9074dde` as the only commit beyond `main`. Plan executes on this branch.

---

## File Structure

- `internal/pkg/render/tty.go` — rewritten end-to-end. Final contents: three function-pointer reader vars (`procStatReader`, `procFDWinsizeReader`, `devTTYWinsizeReader`), `procWalkDepth` constant, `parseProcStat`, `walkProcForTTY`, `discoverTermSize`. The 0.2.0 `readTTYCols` function and `ttyColsFunc` var disappear.
- `internal/pkg/render/tty_test.go` — rewritten. Final contents: table-driven `TestParseProcStat`, injection-based `TestWalkProcForTTY_*`, orchestration-level `TestDiscoverTermSize_*`, plus a smoke test that the production `devTTYWinsizeReader` default does not panic. The two 0.2.0 tests (`TestReadTTYColsFunc_DefaultReturnsZeroOrPositive`, `TestTTYColsFunc_IsIndirectedForTests`) are removed because the symbols they reference no longer exist.
- `internal/pkg/render/render.go` — three edits: add `Width int` field to `Config`, add `ttyRows int` field to `renderEnv`, swap the `env.ttyCols = ttyColsFunc()` call site to `env.ttyCols, env.ttyRows = discoverTermSize(opts.Config)` and drop the Powerline-only guard so detection runs on every `Render`.
- `internal/pkg/render/segments.go` — add `renderTTYSize` function and register `segmentFuncs["tty_size"] = renderTTYSize` in `init()`. Add `strconv` import.
- `internal/pkg/render/segments_test.go` — append `TestRenderTTYSize_*` cases.
- `docs/configuration.md` — add `width` row to the `### Fields` schema table, bump the segment-count line, append a `tty_size` entry to the segment vocabulary.

No new packages, no new files outside this list.

---

## Task 1: `parseProcStat` table-driven test + implementation

**Files:**
- Modify: `internal/pkg/render/tty.go` (add `parseProcStat` alongside existing code)
- Modify: `internal/pkg/render/tty_test.go` (add `TestParseProcStat`)

### Step 1: Write the failing test

Append to `internal/pkg/render/tty_test.go`:

```go
func TestParseProcStat(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		wantPPID   int
		wantTTYNr  int
		wantErrSub string // substring that the error must contain; "" = no error
	}{
		{
			name:      "simple claude line",
			input:     "51437 (claude) S 50912 51437 50912 34818 51437 4194304 12345 0 0 0 1 2 3 4 20 0 1 0 100 0 0",
			wantPPID:  50912,
			wantTTYNr: 34818,
		},
		{
			name:      "comm with embedded colon and space",
			input:     "50911 (tmux: server) S 1 50911 50911 0 -1 4194304 0 0 0 0 0 0 0 0 20 0 1 0 100 0 0",
			wantPPID:  1,
			wantTTYNr: 0,
		},
		{
			name:      "comm with embedded parens",
			input:     "99 (foo(bar)baz) S 1 99 99 0 -1 4194304 0 0 0 0 0 0 0 0 20 0 1 0 100 0 0",
			wantPPID:  1,
			wantTTYNr: 0,
		},
		{
			name:       "empty input",
			input:      "",
			wantErrSub: "no closing paren",
		},
		{
			name:       "no closing paren",
			input:      "51437 (claude S 50912",
			wantErrSub: "no closing paren",
		},
		{
			name:       "fewer than 5 fields after comm",
			input:      "51437 (claude) S 50912",
			wantErrSub: "fields after comm",
		},
		{
			name:       "non-numeric ppid",
			input:      "51437 (claude) S X 51437 50912 34818 51437 4194304",
			wantErrSub: "ppid",
		},
		{
			name:       "non-numeric tty_nr",
			input:      "51437 (claude) S 1 51437 50912 X 51437 4194304",
			wantErrSub: "tty_nr",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotPPID, gotTTYNr, err := parseProcStat([]byte(c.input))
			if c.wantErrSub != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (ppid=%d tty_nr=%d)", c.wantErrSub, gotPPID, gotTTYNr)
				}
				if !strings.Contains(err.Error(), c.wantErrSub) {
					t.Fatalf("error %q does not contain %q", err.Error(), c.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotPPID != c.wantPPID {
				t.Errorf("ppid: got %d, want %d", gotPPID, c.wantPPID)
			}
			if gotTTYNr != c.wantTTYNr {
				t.Errorf("tty_nr: got %d, want %d", gotTTYNr, c.wantTTYNr)
			}
		})
	}
}
```

Then add `"strings"` to the imports of `tty_test.go` if it is not already imported:

```go
import (
	"strings"
	"testing"
)
```

### Step 2: Run test to verify it fails

Run: `go test -run TestParseProcStat ./internal/pkg/render -v`

Expected output: compile error reporting `undefined: parseProcStat`.

### Step 3: Implement `parseProcStat`

Append to `internal/pkg/render/tty.go` (do not modify the existing `readTTYCols` / `ttyColsFunc` yet — they go in Task 5):

```go
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
```

Update the `tty.go` import block so it contains all of:

```go
import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)
```

(`os` and `golang.org/x/sys/unix` are already there from the 0.2.0 file; `bytes`, `errors`, `fmt`, and `strconv` are new.)

### Step 4: Run the test to verify it passes

Run: `go test -run TestParseProcStat ./internal/pkg/render -v`

Expected: 8 sub-cases all PASS.

### Step 5: Run the full render-package suite for regression

Run: `go test ./internal/pkg/render -v`

Expected: all tests PASS (including the unchanged `TestReadTTYColsFunc_DefaultReturnsZeroOrPositive` and `TestTTYColsFunc_IsIndirectedForTests`).

### Step 6: Commit

```bash
git add internal/pkg/render/tty.go internal/pkg/render/tty_test.go
git commit -s -m "$(cat <<'EOF'
feat(render): parseProcStat helper for /proc/<pid>/stat lines

Pure parser that returns (ppid, tty_nr) from the kernel's stat
format. Splits on the LAST ')' so comm fields containing spaces
and embedded parens parse correctly. Table-driven test covers
the claude line shape, tmux/space cases, malformed inputs.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: `walkProcForTTY` with injection-based tests

**Files:**
- Modify: `internal/pkg/render/tty.go` (append `walkProcForTTY` and `procWalkDepth`)
- Modify: `internal/pkg/render/tty_test.go` (append walker tests)

### Step 1: Write the failing tests

Append to `internal/pkg/render/tty_test.go`:

```go
// procStatStub builds a /proc/<pid>/stat-shaped byte slice with the
// given (ppid, tty_nr). Used by the walker tests.
func procStatStub(comm string, ppid, ttyNr int) []byte {
	return []byte(fmt.Sprintf("1 (%s) S %d 1 1 %d 1 0 0 0 0 0 0 0 0 0 20 0 1 0 100 0 0",
		comm, ppid, ttyNr))
}

func TestWalkProcForTTY_ImmediateHit(t *testing.T) {
	stat := func(pid int) ([]byte, error) {
		return procStatStub("parent", 1, 42), nil
	}
	size := func(path string) (int, int, error) {
		if path != "/proc/100/fd/0" {
			t.Fatalf("unexpected path: %s", path)
		}
		return 128, 37, nil
	}
	cols, rows := walkProcForTTY(100, 16, stat, size)
	if cols != 128 || rows != 37 {
		t.Errorf("got (%d, %d), want (128, 37)", cols, rows)
	}
}

func TestWalkProcForTTY_DepthTwoHit(t *testing.T) {
	// depth 0 (pid 100) → tty_nr=0, ppid=200
	// depth 1 (pid 200) → tty_nr=0, ppid=300
	// depth 2 (pid 300) → tty_nr=42, ppid=400
	stat := func(pid int) ([]byte, error) {
		switch pid {
		case 100:
			return procStatStub("go", 200, 0), nil
		case 200:
			return procStatStub("bash", 300, 0), nil
		case 300:
			return procStatStub("claude", 400, 42), nil
		}
		t.Fatalf("unexpected pid: %d", pid)
		return nil, nil
	}
	size := func(path string) (int, int, error) {
		if path != "/proc/300/fd/0" {
			t.Fatalf("size called for wrong path: %s", path)
		}
		return 128, 37, nil
	}
	cols, rows := walkProcForTTY(100, 16, stat, size)
	if cols != 128 || rows != 37 {
		t.Errorf("got (%d, %d), want (128, 37)", cols, rows)
	}
}

func TestWalkProcForTTY_StopsAtPID1(t *testing.T) {
	// depth 0 (pid 100) → tty_nr=0, ppid=1. Loop must stop because
	// pid 1 is unreachable / not interesting.
	stat := func(pid int) ([]byte, error) {
		if pid != 100 {
			t.Fatalf("unexpected pid: %d", pid)
		}
		return procStatStub("orphan", 1, 0), nil
	}
	size := func(path string) (int, int, error) {
		t.Fatalf("sizeReader must not be called when no ancestor has tty: %s", path)
		return 0, 0, nil
	}
	cols, rows := walkProcForTTY(100, 16, stat, size)
	if cols != 0 || rows != 0 {
		t.Errorf("got (%d, %d), want (0, 0)", cols, rows)
	}
}

func TestWalkProcForTTY_DepthCap(t *testing.T) {
	calls := 0
	stat := func(pid int) ([]byte, error) {
		calls++
		// Every ancestor has tty_nr=0 and a fresh, ever-increasing PPID
		// so the walk never reaches PID 1 — depth must cap the loop.
		return procStatStub("deep", pid+10, 0), nil
	}
	size := func(path string) (int, int, error) {
		t.Fatalf("sizeReader must not be called: %s", path)
		return 0, 0, nil
	}
	cols, rows := walkProcForTTY(100, 16, stat, size)
	if cols != 0 || rows != 0 {
		t.Errorf("got (%d, %d), want (0, 0)", cols, rows)
	}
	if calls > 16 {
		t.Errorf("statReader called %d times, want <= 16", calls)
	}
}

func TestWalkProcForTTY_SizeReaderFailsThenSucceeds(t *testing.T) {
	// depth 0 (pid 100) → tty_nr=42, but sizeReader fails.
	// depth 1 (pid 200) → tty_nr=42 and sizeReader succeeds.
	stat := func(pid int) ([]byte, error) {
		switch pid {
		case 100:
			return procStatStub("first", 200, 42), nil
		case 200:
			return procStatStub("second", 300, 42), nil
		}
		t.Fatalf("unexpected pid: %d", pid)
		return nil, nil
	}
	size := func(path string) (int, int, error) {
		switch path {
		case "/proc/100/fd/0":
			return 0, 0, errors.New("simulated failure")
		case "/proc/200/fd/0":
			return 128, 37, nil
		}
		t.Fatalf("unexpected path: %s", path)
		return 0, 0, nil
	}
	cols, rows := walkProcForTTY(100, 16, stat, size)
	if cols != 128 || rows != 37 {
		t.Errorf("got (%d, %d), want (128, 37)", cols, rows)
	}
}

func TestWalkProcForTTY_ProcUnavailable(t *testing.T) {
	stat := func(pid int) ([]byte, error) {
		return nil, errors.New("simulated ENOENT")
	}
	size := func(path string) (int, int, error) {
		t.Fatalf("sizeReader must not be called: %s", path)
		return 0, 0, nil
	}
	cols, rows := walkProcForTTY(100, 16, stat, size)
	if cols != 0 || rows != 0 {
		t.Errorf("got (%d, %d), want (0, 0)", cols, rows)
	}
}

func TestWalkProcForTTY_StatReaderErrorMidWalk(t *testing.T) {
	// depth 0 → tty_nr=0, then depth 1 stat fails. Result must be (0, 0)
	// — a failure to read the stat line aborts the walk.
	stat := func(pid int) ([]byte, error) {
		switch pid {
		case 100:
			return procStatStub("ok", 200, 0), nil
		case 200:
			return nil, errors.New("simulated stat failure")
		}
		t.Fatalf("unexpected pid: %d", pid)
		return nil, nil
	}
	size := func(path string) (int, int, error) {
		t.Fatalf("sizeReader must not be called: %s", path)
		return 0, 0, nil
	}
	cols, rows := walkProcForTTY(100, 16, stat, size)
	if cols != 0 || rows != 0 {
		t.Errorf("got (%d, %d), want (0, 0)", cols, rows)
	}
}
```

Ensure `tty_test.go` imports `errors` and `fmt` (in addition to `strings`, `testing` from Task 1):

```go
import (
	"errors"
	"fmt"
	"strings"
	"testing"
)
```

### Step 2: Run the tests to verify they fail

Run: `go test -run TestWalkProcForTTY ./internal/pkg/render -v`

Expected: compile error reporting `undefined: walkProcForTTY` (and `procWalkDepth` is not referenced in tests, so no error from that yet).

### Step 3: Implement `walkProcForTTY` and the depth constant

Append to `internal/pkg/render/tty.go`:

```go
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
```

### Step 4: Run the tests to verify they pass

Run: `go test -run TestWalkProcForTTY ./internal/pkg/render -v`

Expected: all 7 sub-cases PASS.

### Step 5: Run the full render-package suite

Run: `go test ./internal/pkg/render -v`

Expected: all tests PASS.

### Step 6: Commit

```bash
git add internal/pkg/render/tty.go internal/pkg/render/tty_test.go
git commit -s -m "$(cat <<'EOF'
feat(render): walkProcForTTY traverses the parent-PID chain

For each ancestor with tty_nr != 0, opens /proc/<pid>/fd/0 via
the injected sizeReader and runs TIOCGWINSZ. Depth-capped at 16,
stops at PID 1, recovers from per-ancestor sizeReader failures by
continuing to the next ancestor. Stat-read failures abort the
walk. Seven injection-based test cases cover the matrix.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: `discoverTermSize` orchestrator + production readers

**Files:**
- Modify: `internal/pkg/render/tty.go` (add `discoverTermSize` and the three function-pointer vars)
- Modify: `internal/pkg/render/tty_test.go` (add orchestration tests)

### Step 1: Write the failing tests

Append to `internal/pkg/render/tty_test.go`:

```go
func TestDiscoverTermSize_WidthOverrideWins(t *testing.T) {
	// When Config.Width > 0, the orchestrator must short-circuit
	// before any reader is invoked.
	prevDev, prevStat, prevFD := devTTYWinsizeReader, procStatReader, procFDWinsizeReader
	defer func() {
		devTTYWinsizeReader = prevDev
		procStatReader = prevStat
		procFDWinsizeReader = prevFD
	}()
	devTTYWinsizeReader = func() (int, int, bool) {
		t.Fatal("devTTYWinsizeReader must not be called when Width > 0")
		return 0, 0, false
	}
	procStatReader = func(pid int) ([]byte, error) {
		t.Fatal("procStatReader must not be called when Width > 0")
		return nil, nil
	}
	procFDWinsizeReader = func(path string) (int, int, error) {
		t.Fatal("procFDWinsizeReader must not be called when Width > 0")
		return 0, 0, nil
	}
	cols, rows := discoverTermSize(Config{Width: 128})
	if cols != 128 || rows != 0 {
		t.Errorf("got (%d, %d), want (128, 0)", cols, rows)
	}
}

func TestDiscoverTermSize_DevTTYWinsThenProcSkipped(t *testing.T) {
	prevDev, prevStat, prevFD := devTTYWinsizeReader, procStatReader, procFDWinsizeReader
	defer func() {
		devTTYWinsizeReader = prevDev
		procStatReader = prevStat
		procFDWinsizeReader = prevFD
	}()
	devTTYWinsizeReader = func() (int, int, bool) { return 96, 24, true }
	procStatReader = func(pid int) ([]byte, error) {
		t.Fatal("procStatReader must not be called when /dev/tty succeeds")
		return nil, nil
	}
	procFDWinsizeReader = func(path string) (int, int, error) {
		t.Fatal("procFDWinsizeReader must not be called when /dev/tty succeeds")
		return 0, 0, nil
	}
	cols, rows := discoverTermSize(Config{})
	if cols != 96 || rows != 24 {
		t.Errorf("got (%d, %d), want (96, 24)", cols, rows)
	}
}

func TestDiscoverTermSize_FallsBackToProcWalk(t *testing.T) {
	prevDev, prevStat, prevFD := devTTYWinsizeReader, procStatReader, procFDWinsizeReader
	defer func() {
		devTTYWinsizeReader = prevDev
		procStatReader = prevStat
		procFDWinsizeReader = prevFD
	}()
	devTTYWinsizeReader = func() (int, int, bool) { return 0, 0, false }
	procStatReader = func(pid int) ([]byte, error) {
		// First ancestor has tty_nr=42.
		return procStatStub("parent", 1, 42), nil
	}
	procFDWinsizeReader = func(path string) (int, int, error) {
		return 128, 37, nil
	}
	cols, rows := discoverTermSize(Config{})
	if cols != 128 || rows != 37 {
		t.Errorf("got (%d, %d), want (128, 37)", cols, rows)
	}
}

func TestDiscoverTermSize_AllSourcesFail(t *testing.T) {
	prevDev, prevStat, prevFD := devTTYWinsizeReader, procStatReader, procFDWinsizeReader
	defer func() {
		devTTYWinsizeReader = prevDev
		procStatReader = prevStat
		procFDWinsizeReader = prevFD
	}()
	devTTYWinsizeReader = func() (int, int, bool) { return 0, 0, false }
	procStatReader = func(pid int) ([]byte, error) {
		return nil, errors.New("simulated ENOENT")
	}
	procFDWinsizeReader = func(path string) (int, int, error) {
		t.Fatal("procFDWinsizeReader must not be called when stat fails")
		return 0, 0, nil
	}
	cols, rows := discoverTermSize(Config{})
	if cols != 0 || rows != 0 {
		t.Errorf("got (%d, %d), want (0, 0)", cols, rows)
	}
}

func TestDevTTYWinsizeReader_DefaultDoesNotPanic(t *testing.T) {
	// The production default opens /dev/tty. Under `go test` the
	// controlling tty may or may not be reachable. Assert only that
	// the function does not panic and returns sensible values.
	cols, rows, ok := devTTYWinsizeReader()
	if cols < 0 || rows < 0 {
		t.Errorf("negative size: cols=%d rows=%d ok=%t", cols, rows, ok)
	}
}
```

### Step 2: Run the tests to verify they fail

Run: `go test -run TestDiscoverTermSize ./internal/pkg/render -v`

Expected: compile error reporting `undefined: discoverTermSize`, `undefined: devTTYWinsizeReader`, `undefined: procStatReader`, `undefined: procFDWinsizeReader`.

### Step 3: Implement `discoverTermSize` and the three reader vars

Append to `internal/pkg/render/tty.go`:

```go
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
```

### Step 4: Run the tests to verify they pass

Run: `go test -run "TestDiscoverTermSize|TestDevTTYWinsizeReader" ./internal/pkg/render -v`

Expected: all five tests PASS.

### Step 5: Run the full render-package suite

Run: `go test ./internal/pkg/render -v`

Expected: all tests PASS. Note that `TestReadTTYColsFunc_DefaultReturnsZeroOrPositive` and `TestTTYColsFunc_IsIndirectedForTests` still exist and still pass (the old API is still alive — Task 5 retires it).

### Step 6: Commit

```bash
git add internal/pkg/render/tty.go internal/pkg/render/tty_test.go
git commit -s -m "$(cat <<'EOF'
feat(render): discoverTermSize orchestrator + three reader vars

discoverTermSize threads Config.Width > /dev/tty > /proc walk >
(0,0). The /dev/tty, /proc/<pid>/stat, and /proc/<pid>/fd/0 paths
are wrapped in function-pointer vars (devTTYWinsizeReader,
procStatReader, procFDWinsizeReader) so tests can stub each layer.
The new orchestrator coexists with the 0.2.0 readTTYCols/ttyColsFunc
for now — the call site swap happens in the next task.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: `Config.Width` field and `renderEnv.ttyRows`

**Files:**
- Modify: `internal/pkg/render/render.go` (Config struct, renderEnv struct, and JSON round-trip test surface)
- Modify: `internal/pkg/render/render_test.go` (append a Config-level JSON test)

### Step 1: Write the failing test

Append to `internal/pkg/render/render_test.go`:

```go
func TestConfigJSON_WidthRoundTrip(t *testing.T) {
	// Config.Width must marshal as the "width" JSON key, omitempty
	// when zero, and unmarshal back to the same int.
	zero := Config{}
	out, err := json.Marshal(zero)
	if err != nil {
		t.Fatalf("marshal zero: %v", err)
	}
	if strings.Contains(string(out), `"width"`) {
		t.Errorf("zero Config must omit width, got %s", out)
	}

	set := Config{Width: 200}
	out, err = json.Marshal(set)
	if err != nil {
		t.Fatalf("marshal set: %v", err)
	}
	if !strings.Contains(string(out), `"width":200`) {
		t.Errorf("Config{Width:200} must encode width, got %s", out)
	}

	var back Config
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Width != 200 {
		t.Errorf("round-trip width: got %d, want 200", back.Width)
	}
}
```

Ensure `render_test.go` imports `encoding/json` and `strings` (both are likely already present from other tests).

### Step 2: Run the test to verify it fails

Run: `go test -run TestConfigJSON_WidthRoundTrip ./internal/pkg/render -v`

Expected: compile error reporting `unknown field Width in struct literal of type render.Config`.

### Step 3: Add the `Width` field to `Config`

In `internal/pkg/render/render.go`, locate the `Config` struct (currently around lines 20-25):

```go
// Config is the on-disk render schema, embedded into config.Config.
type Config struct {
	Rows      []Row  `json:"rows,omitzero"`
	Separator string `json:"separator,omitempty"`
	Powerline bool   `json:"powerline,omitempty"`
}
```

Replace with:

```go
// Config is the on-disk render schema, embedded into config.Config.
type Config struct {
	Rows      []Row  `json:"rows,omitzero"`
	Separator string `json:"separator,omitempty"`
	Powerline bool   `json:"powerline,omitempty"`
	// Width, when > 0, is the explicit terminal column count used for
	// Powerline padding and the tty_size segment. The default 0 means
	// "rely on auto-detection" (/dev/tty + /proc walk).
	Width int `json:"width,omitempty"`
}
```

### Step 4: Add the `ttyRows` field to `renderEnv`

In the same file, locate the `renderEnv` struct (currently around lines 287-294):

```go
// renderEnv carries per-call state from Render to each segment renderer.
// Segment functions read these fields but never mutate them.
type renderEnv struct {
	cwd          string // resolved cwd (Options.Cwd or payload.Workspace.CurrentDir)
	colorEnabled bool   // false when NoColor was set on Options
	nowUnix      int64  // wall clock at the start of Render, for time-based segments
	ttyCols      int    // populated only when Config.Powerline is true and colour is on
}
```

Replace with:

```go
// renderEnv carries per-call state from Render to each segment renderer.
// Segment functions read these fields but never mutate them.
type renderEnv struct {
	cwd          string // resolved cwd (Options.Cwd or payload.Workspace.CurrentDir)
	colorEnabled bool   // false when NoColor was set on Options
	nowUnix      int64  // wall clock at the start of Render, for time-based segments
	ttyCols      int    // detected terminal columns, 0 when unknown
	ttyRows      int    // detected terminal rows, 0 when unknown
}
```

The doc comment for `ttyCols` changes too — detection now runs on every `Render` call regardless of Powerline (the next task swaps the call site to make this true).

### Step 5: Run the test and the full suite

Run: `go test -run TestConfigJSON_WidthRoundTrip ./internal/pkg/render -v`
Expected: PASS.

Run: `go test ./internal/pkg/render -v`
Expected: all tests PASS (the new struct fields are zero-valued in every existing test, so behaviour is identical to today).

### Step 6: Commit

```bash
git add internal/pkg/render/render.go internal/pkg/render/render_test.go
git commit -s -m "$(cat <<'EOF'
feat(render): add Config.Width and renderEnv.ttyRows

Config.Width is the explicit terminal-cols escape hatch for cases
where auto-detection fails or needs to be overridden. JSON tag is
omitempty so existing configs without the field unmarshal unchanged.
renderEnv.ttyRows joins ttyCols as the per-call cached terminal
dimension. Neither field is wired into Render yet — the next task
swaps the call site from ttyColsFunc to discoverTermSize.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Cut `Render` over to `discoverTermSize`, retire the 0.2.0 API

**Files:**
- Modify: `internal/pkg/render/render.go` (the `env := renderEnv{...}` block in `Render`)
- Modify: `internal/pkg/render/tty.go` (delete `readTTYCols` and `ttyColsFunc`)
- Modify: `internal/pkg/render/tty_test.go` (delete the two 0.2.0 tests)
- Modify: `internal/pkg/render/render_test.go` (append integration test)

### Step 1: Write the failing integration test

Append to `internal/pkg/render/render_test.go`:

```go
func TestRender_PopulatesTTYColsAndRowsViaDiscover(t *testing.T) {
	// Stub the three reader vars so detection returns a deterministic
	// (cols, rows). The tty_size segment surfaces those values to the
	// output so the test can check the integration end-to-end.
	prevDev, prevStat, prevFD := devTTYWinsizeReader, procStatReader, procFDWinsizeReader
	defer func() {
		devTTYWinsizeReader = prevDev
		procStatReader = prevStat
		procFDWinsizeReader = prevFD
	}()
	devTTYWinsizeReader = func() (int, int, bool) { return 96, 24, true }
	procStatReader = func(pid int) ([]byte, error) {
		t.Fatal("procStatReader must not be called when /dev/tty succeeds")
		return nil, nil
	}
	procFDWinsizeReader = func(path string) (int, int, error) {
		t.Fatal("procFDWinsizeReader must not be called when /dev/tty succeeds")
		return 0, 0, nil
	}

	cfg := Config{
		Rows: []Row{{Segments: []Segment{{Type: "tty_size"}}}},
	}
	got, err := Render([]byte(`{}`), Options{Config: cfg, NoColor: true})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(got, "96×24") {
		t.Errorf("expected output to contain %q, got %q", "96×24", got)
	}
}
```

### Step 2: Run the test to verify it fails

Run: `go test -run TestRender_PopulatesTTYColsAndRowsViaDiscover ./internal/pkg/render -v`

Expected: FAIL. Two reasons it currently fails:
1. The `tty_size` segment type is unknown — Render emits `?tty_size?` instead of `96×24`. (Task 6 adds the segment.)
2. The call site in `Render` still uses `ttyColsFunc()` and skips detection when Powerline is off.

Both are addressed by this task and Task 6 together. Task 5 fixes #2; the test stays red until Task 6 fixes #1.

### Step 3: Replace the detection call site in `Render`

In `internal/pkg/render/render.go`, locate the `Render` function's env-setup block (currently around lines 245-252):

```go
	env := renderEnv{
		cwd:          cwd,
		colorEnabled: !opts.NoColor,
		nowUnix:      nowFunc().Unix(),
	}
	if opts.Config.Powerline && env.colorEnabled {
		env.ttyCols = ttyColsFunc()
	}
```

Replace with:

```go
	env := renderEnv{
		cwd:          cwd,
		colorEnabled: !opts.NoColor,
		nowUnix:      nowFunc().Unix(),
	}
	env.ttyCols, env.ttyRows = discoverTermSize(opts.Config)
```

### Step 4: Remove the 0.2.0 API from `tty.go`

In `internal/pkg/render/tty.go`, delete the `readTTYCols` function and the `ttyColsFunc` var (currently the first ~30 lines of the file from the 0.2.0 state). The file's surviving content is everything Tasks 1-3 added: imports, the three reader vars, `procWalkDepth`, `parseProcStat`, `walkProcForTTY`, `discoverTermSize`. Verify the imports list is exactly:

```go
import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)
```

### Step 5: Remove the two obsolete tests from `tty_test.go`

In `internal/pkg/render/tty_test.go`, delete the two 0.2.0 tests:

- `TestReadTTYColsFunc_DefaultReturnsZeroOrPositive` (currently lines 5-14)
- `TestTTYColsFunc_IsIndirectedForTests` (currently lines 16-26)

The file's surviving tests are everything Tasks 1-3 appended.

### Step 6: Run the full render-package suite

Run: `go test ./internal/pkg/render -v`

Expected: every test PASSES except `TestRender_PopulatesTTYColsAndRowsViaDiscover`, which still fails because the `tty_size` segment is not yet registered. Task 6 will green it.

Verify the failure is exactly the segment-unknown failure (not a compile error):

```
TestRender_PopulatesTTYColsAndRowsViaDiscover:
  expected output to contain "96×24", got "?tty_size?"
```

If the test fails with a different error (e.g. a compile error in another file because something else referenced `ttyColsFunc`), stop and report — that signals an orphaned reference the plan did not anticipate.

### Step 7: Commit

```bash
git add internal/pkg/render/render.go internal/pkg/render/tty.go internal/pkg/render/tty_test.go internal/pkg/render/render_test.go
git commit -s -m "$(cat <<'EOF'
feat(render): wire discoverTermSize into Render, drop ttyColsFunc

Render now calls discoverTermSize(opts.Config) on every invocation
and stores the result in renderEnv.ttyCols/ttyRows. The 0.2.0
Powerline-only guard is removed because detection cost is
microseconds and tty_size will need the value in natural mode too.
The 0.2.0 readTTYCols function and ttyColsFunc var are deleted
along with their two tests, since nothing references them anymore.
The integration test TestRender_PopulatesTTYColsAndRowsViaDiscover
remains red until Task 6 registers the tty_size segment.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: `tty_size` segment

**Files:**
- Modify: `internal/pkg/render/segments.go` (add `renderTTYSize`, register in `init`, add `strconv` import)
- Modify: `internal/pkg/render/segments_test.go` (append `TestRenderTTYSize_*` cases)

### Step 1: Write the failing tests

Append to `internal/pkg/render/segments_test.go`:

```go
func TestRenderTTYSize_DefaultFormat(t *testing.T) {
	env := renderEnv{ttyCols: 128, ttyRows: 37}
	got := renderTTYSize(&payload{}, Segment{Type: "tty_size"}, env)
	if got != "128×37" {
		t.Errorf("got %q, want %q", got, "128×37")
	}
}

func TestRenderTTYSize_ColsOnlyFormat(t *testing.T) {
	env := renderEnv{ttyCols: 128, ttyRows: 37}
	got := renderTTYSize(&payload{}, Segment{Type: "tty_size", Format: "{cols}c"}, env)
	if got != "128c" {
		t.Errorf("got %q, want %q", got, "128c")
	}
}

func TestRenderTTYSize_LabelPrefix(t *testing.T) {
	env := renderEnv{ttyCols: 128, ttyRows: 37}
	got := renderTTYSize(&payload{}, Segment{Type: "tty_size", Label: "term"}, env)
	if got != "term: 128×37" {
		t.Errorf("got %q, want %q", got, "term: 128×37")
	}
}

func TestRenderTTYSize_HiddenWhenBothZero(t *testing.T) {
	env := renderEnv{ttyCols: 0, ttyRows: 0}
	got := renderTTYSize(&payload{}, Segment{Type: "tty_size"}, env)
	if got != "" {
		t.Errorf("got %q, want \"\"", got)
	}
}

func TestRenderTTYSize_LabelDoesNotPreventHide(t *testing.T) {
	env := renderEnv{ttyCols: 0, ttyRows: 0}
	got := renderTTYSize(&payload{}, Segment{Type: "tty_size", Label: "term"}, env)
	if got != "" {
		t.Errorf("got %q, want \"\" (hide must precede label prefix)", got)
	}
}

func TestRenderTTYSize_RowsZeroIsNotHidden(t *testing.T) {
	// Config.Width sets cols only; rows stays 0. The segment must still
	// render — "size unknown" is the both-zero case, not rows-zero.
	env := renderEnv{ttyCols: 128, ttyRows: 0}
	got := renderTTYSize(&payload{}, Segment{Type: "tty_size"}, env)
	if got != "128×0" {
		t.Errorf("got %q, want %q", got, "128×0")
	}
}
```

### Step 2: Run the tests to verify they fail

Run: `go test -run TestRenderTTYSize ./internal/pkg/render -v`

Expected: compile error reporting `undefined: renderTTYSize`.

### Step 3: Implement `renderTTYSize` and register it

In `internal/pkg/render/segments.go`, add `"strconv"` to the imports so the block becomes:

```go
import (
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
)
```

In the `init()` function (currently around lines 25-39), add one new registration line so the function ends with:

```go
func init() {
	segmentFuncs["model"] = renderModel
	segmentFuncs["effort"] = renderEffort
	segmentFuncs["session_name"] = renderSessionName
	segmentFuncs["output_style"] = renderOutputStyle
	segmentFuncs["cwd"] = renderCwd
	segmentFuncs["cost"] = renderCost
	segmentFuncs["duration"] = renderDuration
	segmentFuncs["lines"] = renderLines
	segmentFuncs["context"] = renderContext
	segmentFuncs["limit_5h"] = renderLimit5h
	segmentFuncs["limit_7d"] = renderLimit7d
	segmentFuncs["mode"] = renderMode
	segmentFuncs["git_branch"] = renderGitBranch
	segmentFuncs["tty_size"] = renderTTYSize
}
```

Append `renderTTYSize` at the bottom of `segments.go`:

```go
// renderTTYSize formats env.ttyCols × env.ttyRows. Returns "" when
// detection failed (both dimensions 0), so the empty-segment-drop path
// in both renderers omits the segment plus its surrounding separator
// or chevron. s.Format supports the placeholders {cols} and {rows};
// the default format is "{cols}×{rows}" using U+00D7 (multiplication
// sign), not ASCII 'x'. s.Label, when set, prefixes "<label>: ".
func renderTTYSize(_ *payload, s Segment, env renderEnv) string {
	if env.ttyCols == 0 && env.ttyRows == 0 {
		return ""
	}
	format := s.Format
	if format == "" {
		format = "{cols}×{rows}"
	}
	out := strings.ReplaceAll(format, "{cols}", strconv.Itoa(env.ttyCols))
	out = strings.ReplaceAll(out, "{rows}", strconv.Itoa(env.ttyRows))
	if s.Label != "" {
		out = s.Label + ": " + out
	}
	return out
}
```

### Step 4: Run the tests to verify they pass

Run: `go test -run TestRenderTTYSize ./internal/pkg/render -v`

Expected: all 6 sub-cases PASS.

### Step 5: Run the full render-package suite

Run: `go test ./internal/pkg/render -v`

Expected: all tests PASS, including `TestRender_PopulatesTTYColsAndRowsViaDiscover` which was red at the end of Task 5 and is now green because the `tty_size` dispatch is registered.

### Step 6: Commit

```bash
git add internal/pkg/render/segments.go internal/pkg/render/segments_test.go
git commit -s -m "$(cat <<'EOF'
feat(render): tty_size segment surfaces detected terminal size

New segment type "tty_size" formats env.ttyCols × env.ttyRows.
Default format "{cols}×{rows}" uses U+00D7 multiplication sign,
not ASCII 'x'. Hides when both dimensions are 0 (detection failed)
so the empty-segment-drop pattern naturally omits the surrounding
separator or chevron. Supports {cols}/{rows} placeholders in
seg.Format and a "<label>: " prefix via seg.Label. Greens the
integration test from Task 5.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Documentation refresh

**Files:**
- Modify: `docs/configuration.md` (schema-table row + segment-vocabulary entry + count update)

### Step 1: Add the `width` row to the schema table

In `docs/configuration.md`, locate the `### Fields` table (currently around lines 39-43):

```
| Field       | Type            | Default       | Effect                                                      |
| ----------- | --------------- | ------------- | ----------------------------------------------------------- |
| `rows`      | array of arrays | default rows  | Each inner array is one output line. Segments in a row are joined with `separator`. |
| `separator` | string          | `" \| "`      | Joiner between segments in the same row.                    |
| `powerline` | bool            | `false`       | When true, each row's `bg` fills the terminal width and segments are joined with the U+E0B1 thin chevron. See [Powerline](#powerline). |
```

Append one row so the table ends with:

```
| `powerline` | bool            | `false`       | When true, each row's `bg` fills the terminal width and segments are joined with the U+E0B1 thin chevron. See [Powerline](#powerline). |
| `width`     | int             | `0`           | Explicit terminal-cols override. When `> 0`, ccsb skips terminal-size detection and uses this value. Default `0` runs the detection chain (`/dev/tty` ioctl, then `/proc` parent-process walk). |
```

### Step 2: Bump the segment count

In `docs/configuration.md`, locate the line (currently around line 145):

```
Every segment is an object with at least a `type` field. The renderer
recognises 14 types.
```

Replace with:

```
Every segment is an object with at least a `type` field. The renderer
recognises 15 types.
```

### Step 3: Append the `tty_size` entry to the segment vocabulary

In `docs/configuration.md`, locate the `#### git_branch` section (currently around lines 387-401). After its closing fenced example block — i.e. immediately before the line `## Colors` — insert:

```markdown
#### `tty_size`

Detected terminal columns × rows. Format supports `{cols}` and `{rows}`
placeholders; the default format is `"{cols}×{rows}"` using the Unicode
multiplication sign `U+00D7` (not ASCII `x`). Hidden when detection
fails — both dimensions zero — so the surrounding separator or
chevron is dropped naturally. With a non-empty `label`, the output is
prefixed `"<label>: "`.

Detection chain (first non-zero `cols` wins): `Config.Width` >
`/dev/tty` ioctl > `/proc` parent-PID walk. When ccsb is invoked by
Claude Code, `/dev/tty` is unreachable and the `/proc` walk supplies
the size. When ccsb is invoked directly from a shell, `/dev/tty`
satisfies the chain on the first try.

```json
{"type": "tty_size"}                            // "128×37"
{"type": "tty_size", "format": "{cols}c"}       // "128c"
{"type": "tty_size", "label": "term"}           // "term: 128×37"
```
```

### Step 4: Confirm the edits

Run: `grep -n "15 types\|tty_size\|\`width\`" docs/configuration.md`

Expected: at least four lines reporting the new `width` schema row, the bumped `15 types` sentence, the `#### \`tty_size\`` heading, and the JSON example lines.

### Step 5: Commit

```bash
git add docs/configuration.md
git commit -s -m "$(cat <<'EOF'
docs(configuration): document width field and tty_size segment

Adds Config.width to the schema table and tty_size to the segment
vocabulary. Bumps the segment-count sentence from 14 to 15.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: Final verification gate

**Files:** none modified — verification only.

- [ ] **Step 1: Format check**

Run: `gofmt -l .`
Expected: empty.

- [ ] **Step 2: Static analysis**

Run: `go vet ./...`
Expected: empty.

- [ ] **Step 3: Race + coverage suite**

Run: `go test -race -cover ./...`
Expected: every package PASS. Render-package coverage at or above the 94.3% baseline.

- [ ] **Step 4: Smoke test the binary, no Powerline**

Run:

```bash
go build -o bin/ccsb ./cmd/ccsb
echo '{"session_id":"x","model":{"display_name":"S"},"workspace":{"current_dir":"/tmp"}}' \
  | XDG_CONFIG_HOME=$(mktemp -d) XDG_STATE_HOME=$(mktemp -d) ./bin/ccsb \
  | cat -A
```

Expected: three rows of default natural-mode output. No errors on stderr. Exit code 0.

- [ ] **Step 5: Smoke test the binary with `tty_size` segment**

Run:

```bash
TMPCFG=$(mktemp -d) && mkdir -p "$TMPCFG/ccsb" && cat > "$TMPCFG/ccsb/config.json" <<'EOF'
{"render": {"rows": [[{"type":"tty_size","label":"term"}, {"type":"text","label":"hello"}]]}}
EOF
echo '{"session_id":"x","model":{"display_name":"S"},"workspace":{"current_dir":"/tmp"}}' \
  | XDG_CONFIG_HOME=$TMPCFG XDG_STATE_HOME=$(mktemp -d) ./bin/ccsb \
  | cat -A
```

Expected: a single rendered row containing either `term: N×M | hello` (when detection succeeds, e.g. real shell run) or just `hello` (when detection returns 0,0 and the tty_size segment correctly hides plus the natural separator collapses). Either output is correct — both confirm the segment is wired and the hide-on-zero behaves.

- [ ] **Step 6: Smoke test `Config.Width` override**

Run:

```bash
TMPCFG=$(mktemp -d) && mkdir -p "$TMPCFG/ccsb" && cat > "$TMPCFG/ccsb/config.json" <<'EOF'
{"render": {"width": 200, "powerline": true, "rows": [{"bg":"234","segments":[{"type":"text","label":"A"},{"type":"text","label":"B"}]}]}}
EOF
echo '{"session_id":"x","model":{"display_name":"S"},"workspace":{"current_dir":"/tmp"}}' \
  | XDG_CONFIG_HOME=$TMPCFG XDG_STATE_HOME=$(mktemp -d) ./bin/ccsb \
  | cat -A
```

Expected: a single Powerline row whose bg-fill reaches column 200 (visible as a trailing run of bg-painted spaces between the last segment and the closing `\x1b[0m`). The exact byte count of trailing spaces is `200 - 1 - 3 - 1 = 195` (200 cols minus segment A's "A" width minus " <chev> " width minus segment B's "B" width). This is the visible proof that `Config.Width` propagates from config → `discoverTermSize` → `renderEnv.ttyCols` → padding step.

- [ ] **Step 7: Log branch state for handoff**

Run: `git log main..HEAD --oneline`

Expected: nine commits on `development-0.2.2-work`:

1. `9074dde` `docs: spec for 0.2.2 terminal-size detection` (already present)
2. Plan commit (this file)
3. Task 1 — `parseProcStat`
4. Task 2 — `walkProcForTTY`
5. Task 3 — `discoverTermSize`
6. Task 4 — `Config.Width` + `renderEnv.ttyRows`
7. Task 5 — wire `Render` to `discoverTermSize`
8. Task 6 — `tty_size` segment
9. Task 7 — docs

After Task 8 passes, the subagent-driven-development controller hands back to the user, who runs the three release gates (squash → no-ff merge → production tag + push) per project memory `release_workflow_gates`. Those gates are out of scope for this plan.

---

## Notes for the implementer

- All tests live in `package render` (in-package). New tests append to the existing `*_test.go` files; never create a new test file.
- Use existing helpers `ansiRegexp`, `displayWidth`, `payload` without redefining them. They are already in `internal/pkg/render/render.go`.
- Always reference the chevron and other multi-byte glyphs (U+00D7 multiplication sign, U+E0B1 chevron) via named constants or `×` / `` in test source — tool-channel encoding can drop bytes during transmission. The default format string in `renderTTYSize` uses the literal `×` glyph in source; if your editor refuses to insert it, use the escape `"×"` instead — both compile to the same bytes.
- The `/dev/tty` open in the production `devTTYWinsizeReader` will return ENXIO under `go test` (no controlling terminal), so `TestDevTTYWinsizeReader_DefaultDoesNotPanic` exercises the error path. That is the expected — and only — coverage of the unfakeable syscall layer.
- Do not skip hooks, do not amend commits, do not push to remotes. Release gates happen after this plan and are user-driven.
