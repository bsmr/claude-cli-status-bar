package statusline_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/statusline"
)

// Fallback path: no proxy, no capture dir.

func TestRun_FallbackRendersModelAndCurrentDir(t *testing.T) {
	ctx := context.Background()
	in := strings.NewReader(`{
		"model": {"display_name": "Opus"},
		"workspace": {"current_dir": "/tmp/proj"}
	}`)
	var out, errOut bytes.Buffer

	if err := statusline.Run(ctx, statusline.Options{}, in, &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := strings.TrimRight(out.String(), "\n")
	if want := "Opus · /tmp/proj"; got != want {
		t.Errorf("stdout: got %q, want %q", got, want)
	}
}

func TestRun_FallbackRendersFromProjectDir(t *testing.T) {
	ctx := context.Background()
	in := strings.NewReader(`{
		"model": {"display_name": "Sonnet"},
		"workspace": {"project_dir": "/home/u/repo"}
	}`)
	var out, errOut bytes.Buffer

	if err := statusline.Run(ctx, statusline.Options{}, in, &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := strings.TrimRight(out.String(), "\n")
	if want := "Sonnet · /home/u/repo"; got != want {
		t.Errorf("stdout: got %q, want %q", got, want)
	}
}

func TestRun_FallbackEmptyJSONRendersPlaceholder(t *testing.T) {
	ctx := context.Background()
	in := strings.NewReader(`{}`)
	var out, errOut bytes.Buffer

	if err := statusline.Run(ctx, statusline.Options{}, in, &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := strings.TrimRight(out.String(), "\n"); got == "" {
		t.Error("output must not be empty")
	}
}

func TestRun_FallbackInvalidJSONStillRendersPlaceholder(t *testing.T) {
	ctx := context.Background()
	in := strings.NewReader("not json at all")
	var out, errOut bytes.Buffer

	if err := statusline.Run(ctx, statusline.Options{}, in, &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Len() == 0 {
		t.Error("fallback must still emit something for invalid JSON")
	}
}

func TestRun_FallbackWritesExactlyOneLine(t *testing.T) {
	ctx := context.Background()
	in := strings.NewReader(`{"model":{"display_name":"Opus"},"workspace":{"current_dir":"/x"}}`)
	var out, errOut bytes.Buffer

	if err := statusline.Run(ctx, statusline.Options{}, in, &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.HasSuffix(out.String(), "\n") {
		t.Errorf("output must end with newline, got %q", out.String())
	}
	if n := strings.Count(out.String(), "\n"); n != 1 {
		t.Errorf("want exactly one newline, got %d in %q", n, out.String())
	}
}

// Capture path.

func TestRun_CapturesPayloadToCaptureDir(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	body := `{"session_id":"abc123","model":{"display_name":"Opus"}}`
	in := strings.NewReader(body)
	var out, errOut bytes.Buffer

	if err := statusline.Run(ctx, statusline.Options{CaptureDir: dir}, in, &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var saved string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			saved = filepath.Join(dir, e.Name())
		}
	}
	if saved == "" {
		t.Fatal("expected a captured .json file")
	}
	got, err := os.ReadFile(saved)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != body {
		t.Errorf("captured content: got %q, want %q", got, body)
	}
	if !strings.Contains(filepath.Base(saved), "abc123") {
		t.Errorf("filename should embed session id: %s", saved)
	}
}

func TestRun_DoesNotCaptureWhenDirEmpty(t *testing.T) {
	ctx := context.Background()
	in := strings.NewReader(`{"session_id":"x"}`)
	var out, errOut bytes.Buffer

	if err := statusline.Run(ctx, statusline.Options{}, in, &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Nothing to assert beyond no error and no panic; test exists to lock the
	// behaviour against future regressions.
}

func TestRun_CaptureErrorIsLoggedNotFatal(t *testing.T) {
	ctx := context.Background()
	// Use a path where a file blocks directory creation.
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	bad := filepath.Join(blocker, "captures") // mkdir under a file => ENOTDIR

	in := strings.NewReader(`{"session_id":"x","model":{"display_name":"Opus"}}`)
	var out, errOut bytes.Buffer

	if err := statusline.Run(ctx, statusline.Options{CaptureDir: bad}, in, &out, &errOut); err != nil {
		t.Fatalf("Run should not fail on capture error: %v", err)
	}
	if !strings.Contains(errOut.String(), "capture") {
		t.Errorf("capture error must be reported on stderr; got %q", errOut.String())
	}
	if out.Len() == 0 {
		t.Error("fallback render must still happen when capture fails")
	}
}

// Proxy path.

func TestRun_ProxyForwardsPayloadAndOutput(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat not available")
	}
	ctx := context.Background()
	body := `{"session_id":"s","model":{"display_name":"Opus"}}`
	in := strings.NewReader(body)
	var out, errOut bytes.Buffer

	err := statusline.Run(ctx, statusline.Options{ProxyCommand: "cat"}, in, &out, &errOut)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := out.String(); got != body {
		t.Errorf("proxy stdout: got %q, want %q", got, body)
	}
}

func TestRun_ProxyErrorPropagated(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	ctx := context.Background()
	in := strings.NewReader(`{}`)
	var out, errOut bytes.Buffer

	err := statusline.Run(ctx, statusline.Options{
		ProxyCommand: "sh",
		ProxyArgs:    []string{"-c", "exit 3"},
	}, in, &out, &errOut)
	if err == nil {
		t.Fatal("expected proxy error to propagate")
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Errorf("expected *exec.ExitError, got %T", err)
	}
}

func TestRun_ProxyAndCaptureBothRun(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat not available")
	}
	ctx := context.Background()
	dir := t.TempDir()
	body := `{"session_id":"sid","model":{"display_name":"Opus"}}`
	in := strings.NewReader(body)
	var out, errOut bytes.Buffer

	err := statusline.Run(ctx, statusline.Options{
		ProxyCommand: "cat",
		CaptureDir:   dir,
	}, in, &out, &errOut)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := out.String(); got != body {
		t.Errorf("proxy stdout: got %q, want %q", got, body)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) == 0 {
		t.Error("capture file expected alongside proxy run")
	}
}

func TestRun_CancelledContextReturnsError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	in := strings.NewReader(`{}`)
	var out, errOut bytes.Buffer

	if err := statusline.Run(ctx, statusline.Options{}, in, &out, &errOut); err == nil {
		t.Fatal("expected error from cancelled context")
	}
}
