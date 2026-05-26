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
