//go:build !windows

package render

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

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

// procStatStub builds a /proc/<pid>/stat-shaped byte slice with the
// given (ppid, tty_nr). Used by the walker tests.
func procStatStub(comm string, ppid, ttyNr int) []byte {
	return fmt.Appendf(nil, "1 (%s) S %d 1 1 %d 1 0 0 0 0 0 0 0 0 0 20 0 1 0 100 0 0",
		comm, ppid, ttyNr)
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
	// Walk starts at pid 100 (depth 0). statReader is called for each
	// ancestor; sizeReader only when tty_nr != 0:
	//   depth 0 → pid 100 → tty_nr=0, ppid=200
	//   depth 1 → pid 200 → tty_nr=0, ppid=300
	//   depth 2 → pid 300 → tty_nr=42, ppid=400 (size attempted, success)
	// The pid=400 stat is never read because depth-2 sizeReader returns.
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
	if calls != 16 {
		t.Errorf("statReader called %d times, want exactly 16", calls)
	}
}

func TestWalkProcForTTY_SizeReaderFailsThenSucceeds(t *testing.T) {
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

func TestWalkProcForTTY_ParseStatErrorAbortsWalk(t *testing.T) {
	// statReader succeeds but returns bytes that parseProcStat cannot
	// decode. The walk must treat this identically to a statReader
	// failure and return (0, 0) immediately — falling through would
	// loop forever on the same malformed PID.
	stat := func(pid int) ([]byte, error) {
		return []byte("not a stat line"), nil
	}
	size := func(path string) (int, int, error) {
		t.Fatalf("sizeReader must not be called when parse fails: %s", path)
		return 0, 0, nil
	}
	cols, rows := walkProcForTTY(100, 16, stat, size)
	if cols != 0 || rows != 0 {
		t.Errorf("got (%d, %d), want (0, 0)", cols, rows)
	}
}

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
