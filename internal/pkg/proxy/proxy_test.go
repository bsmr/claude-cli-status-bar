package proxy_test

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/proxy"
)

func TestRun_PipesPayloadAsStdinToChild(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	payload := []byte(`{"k":"v"}`)
	var out, errOut bytes.Buffer

	if err := proxy.Run(ctx, "cat", nil, payload, &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := out.String(); got != string(payload) {
		t.Errorf("stdout: got %q, want %q", got, string(payload))
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr unexpectedly non-empty: %q", errOut.String())
	}
}

func TestRun_StreamsChildStdoutToWriter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var out, errOut bytes.Buffer

	if err := proxy.Run(ctx, "sh", []string{"-c", "printf hello"}, nil, &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := out.String(); got != "hello" {
		t.Errorf("stdout: got %q, want %q", got, "hello")
	}
}

func TestRun_StreamsChildStderrToWriter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var out, errOut bytes.Buffer

	if err := proxy.Run(ctx, "sh", []string{"-c", "printf oops 1>&2"}, nil, &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := errOut.String(); got != "oops" {
		t.Errorf("stderr: got %q, want %q", got, "oops")
	}
	if out.Len() != 0 {
		t.Errorf("stdout unexpectedly non-empty: %q", out.String())
	}
}

func TestRun_PropagatesNonZeroExit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var out, errOut bytes.Buffer

	err := proxy.Run(ctx, "sh", []string{"-c", "exit 7"}, nil, &out, &errOut)
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("expected *exec.ExitError, got %T (%v)", err, err)
	}
	if ee.ExitCode() != 7 {
		t.Errorf("exit code: got %d, want 7", ee.ExitCode())
	}
}

func TestRun_RejectsEmptyCommand(t *testing.T) {
	t.Parallel()
	if err := proxy.Run(context.Background(), "", nil, nil, nil, nil); err == nil {
		t.Error("expected error for empty command")
	}
}

func TestRun_NonexistentCommandReturnsError(t *testing.T) {
	t.Parallel()
	err := proxy.Run(context.Background(), "/nonexistent/ccsb-test-binary", nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for nonexistent command")
	}
	if !strings.Contains(err.Error(), "/nonexistent/ccsb-test-binary") {
		t.Errorf("error should reference the command: %v", err)
	}
}

func TestRun_RespectsContextCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var out, errOut bytes.Buffer

	start := time.Now()
	err := proxy.Run(ctx, "sh", []string{"-c", "sleep 5"}, nil, &out, &errOut)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Run did not return promptly after cancel: %v", elapsed)
	}
}

func TestRun_NilStdoutAndStderrAreAccepted(t *testing.T) {
	t.Parallel()
	if err := proxy.Run(context.Background(), "sh", []string{"-c", "printf x; printf y 1>&2"}, nil, nil, nil); err != nil {
		t.Errorf("nil writers should be accepted: %v", err)
	}
}

// A cancelled context already returned promptly before this change. What was
// missing is that nobody ever SET a deadline, so a proxy that hangs — npx
// stalling on a dead network is the realistic case — hung ccsb with it, on
// every status update, forever. The error has to name the timeout: without
// that the user sees only "signal: killed" and has nothing to act on.
func TestRun_DeadlineExceededIsReportedAsATimeout(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := proxy.Run(ctx, "sh", []string{"-c", "sleep 5"}, nil, nil, nil)
	if err == nil {
		t.Fatal("expected an error when the deadline expires")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error must name the timeout, got: %v", err)
	}
	// The message also states the limit, but the exact figure is not asserted:
	// it is derived from the context's remaining time at start, so scheduling
	// turns an even 50ms into 49.9ms often enough to make that flaky.
	if !strings.Contains(err.Error(), "sh") {
		t.Errorf("error should still name the command, got: %v", err)
	}
}

// A caller-cancelled context (SIGINT/SIGTERM) is not a proxy fault and must
// not be reported as one.
func TestRun_PlainCancellationIsNotReportedAsATimeout(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()

	err := proxy.Run(ctx, "sh", []string{"-c", "sleep 5"}, nil, nil, nil)
	if err == nil {
		t.Fatal("expected an error when cancelled")
	}
	if strings.Contains(err.Error(), "timed out") {
		t.Errorf("a plain cancel must not be described as a timeout, got: %v", err)
	}
}

func TestRun_StartFailureIsMarkedNotStarted(t *testing.T) {
	t.Parallel()
	// A command that cannot be resolved never runs, so it cannot have
	// written a byte. Callers rely on that to fall back safely.
	err := proxy.Run(context.Background(), "/nonexistent/ccsb-test-binary", nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for nonexistent command")
	}
	if !errors.Is(err, proxy.ErrNotStarted) {
		t.Errorf("expected ErrNotStarted, got %v", err)
	}
}

func TestRun_RanAndFailedIsNotMarkedNotStarted(t *testing.T) {
	t.Parallel()
	// The counterpart: a child that ran and exited non-zero MAY have
	// written a partial line, so it must not be reported as unstarted.
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	err := proxy.Run(context.Background(), "sh", []string{"-c", "exit 3"}, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
	if errors.Is(err, proxy.ErrNotStarted) {
		t.Errorf("a child that ran must not be reported as unstarted: %v", err)
	}
}
