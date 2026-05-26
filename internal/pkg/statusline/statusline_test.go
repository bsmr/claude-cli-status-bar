package statusline_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/render"
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

	if err := statusline.Run(ctx, statusline.Options{NoColor: true}, in, &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "Opus") {
		t.Errorf("output must contain model name, got %q", out.String())
	}
	if !strings.Contains(out.String(), "proj") {
		t.Errorf("output must contain workspace dir basename, got %q", out.String())
	}
}

func TestRun_FallbackRendersFromProjectDir(t *testing.T) {
	ctx := context.Background()
	in := strings.NewReader(`{
		"model": {"display_name": "Sonnet"},
		"workspace": {"project_dir": "/home/u/repo"}
	}`)
	var out, errOut bytes.Buffer

	if err := statusline.Run(ctx, statusline.Options{NoColor: true}, in, &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "Sonnet") {
		t.Errorf("output must contain model name, got %q", out.String())
	}
	if out.String() == "" {
		t.Errorf("output must not be empty when only project_dir is set, got %q", out.String())
	}
}

func TestRun_FallbackEmptyJSONRendersPlaceholder(t *testing.T) {
	ctx := context.Background()
	in := strings.NewReader(`{}`)
	var out, errOut bytes.Buffer

	if err := statusline.Run(ctx, statusline.Options{NoColor: true}, in, &out, &errOut); err != nil {
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

	if err := statusline.Run(ctx, statusline.Options{NoColor: true}, in, &out, &errOut); err != nil {
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

	cfg := render.Config{Rows: []render.Row{{Segments: []render.Segment{{Type: "model"}}}}}
	if err := statusline.Run(ctx, statusline.Options{Render: cfg, NoColor: true}, in, &out, &errOut); err != nil {
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

func TestRun_CapturesFallbackOutputAlongsideInput(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	body := `{"session_id":"sid","model":{"display_name":"Opus"},"workspace":{"current_dir":"/x"}}`
	var out, errOut bytes.Buffer

	if err := statusline.Run(ctx, statusline.Options{CaptureDir: dir}, strings.NewReader(body), &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}

	in, outFile := pairForSession(t, dir, "sid")
	if in == "" || outFile == "" {
		t.Fatalf("expected paired .json + .out files, got in=%q out=%q", in, outFile)
	}
	if base(in, ".json") != base(outFile, ".out") {
		t.Errorf("input and output basenames should match:\n in:  %s\n out: %s", in, outFile)
	}
	gotOut, _ := os.ReadFile(outFile)
	if string(gotOut) != out.String() {
		t.Errorf("captured .out should equal real stdout:\n captured=%q\n stdout=%q", gotOut, out.String())
	}
	if !strings.Contains(string(gotOut), "Opus") {
		t.Errorf("expected Opus in captured output, got %q", gotOut)
	}
}

func TestRun_CapturesProxyStdoutAlongsideInput(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat not available")
	}
	ctx := context.Background()
	dir := t.TempDir()
	body := `{"session_id":"px","model":{"display_name":"Opus"}}`
	var out, errOut bytes.Buffer

	if err := statusline.Run(ctx, statusline.Options{
		ProxyCommand: "cat",
		CaptureDir:   dir,
	}, strings.NewReader(body), &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}

	in, outFile := pairForSession(t, dir, "px")
	if in == "" || outFile == "" {
		t.Fatalf("expected paired .json + .out files, got in=%q out=%q", in, outFile)
	}
	gotIn, _ := os.ReadFile(in)
	gotOut, _ := os.ReadFile(outFile)
	if string(gotIn) != body {
		t.Errorf(".json content: got %q", gotIn)
	}
	if string(gotOut) != body { // cat passes input verbatim
		t.Errorf(".out content: got %q, want %q", gotOut, body)
	}
}

func TestRun_CapturesProxyStderrSeparately(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	ctx := context.Background()
	dir := t.TempDir()
	body := `{"session_id":"ex"}`
	var out, errOut bytes.Buffer

	if err := statusline.Run(ctx, statusline.Options{
		ProxyCommand: "sh",
		ProxyArgs:    []string{"-c", "printf to-stdout; printf to-stderr 1>&2"},
		CaptureDir:   dir,
	}, strings.NewReader(body), &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	var inFile, outFile, errFile string
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		switch {
		case strings.HasSuffix(e.Name(), ".json"):
			inFile = p
		case strings.HasSuffix(e.Name(), ".out"):
			outFile = p
		case strings.HasSuffix(e.Name(), ".err"):
			errFile = p
		}
	}
	if inFile == "" || outFile == "" || errFile == "" {
		t.Fatalf(".json/.out/.err all expected, got %q %q %q", inFile, outFile, errFile)
	}
	if got, _ := os.ReadFile(outFile); string(got) != "to-stdout" {
		t.Errorf(".out: got %q", got)
	}
	if got, _ := os.ReadFile(errFile); string(got) != "to-stderr" {
		t.Errorf(".err: got %q", got)
	}
}

func TestRun_DoesNotWriteOutFileWhenOutputIsEmpty(t *testing.T) {
	if _, err := exec.LookPath("true"); err != nil {
		t.Skip("true not available")
	}
	ctx := context.Background()
	dir := t.TempDir()
	body := `{"session_id":"silent"}`
	var out, errOut bytes.Buffer

	if err := statusline.Run(ctx, statusline.Options{
		ProxyCommand: "true", // exits 0 with no output
		CaptureDir:   dir,
	}, strings.NewReader(body), &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, e := range mustReadDir(t, dir) {
		if strings.HasSuffix(e.Name(), ".out") || strings.HasSuffix(e.Name(), ".err") {
			t.Errorf("unexpected empty capture file %s", e.Name())
		}
	}
}

// pairForSession finds the .json and .out files in dir whose basename embeds
// sessionID. Returns ("", "") if either is missing.
func pairForSession(t *testing.T, dir, sessionID string) (in, out string) {
	t.Helper()
	for _, e := range mustReadDir(t, dir) {
		if !strings.Contains(e.Name(), sessionID) {
			continue
		}
		p := filepath.Join(dir, e.Name())
		switch {
		case strings.HasSuffix(e.Name(), ".json"):
			in = p
		case strings.HasSuffix(e.Name(), ".out"):
			out = p
		}
	}
	return in, out
}

func mustReadDir(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	return es
}

// base trims the given suffix off a path's basename.
func base(path, suffix string) string {
	b := filepath.Base(path)
	return strings.TrimSuffix(b, suffix)
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

	if err := statusline.Run(ctx, statusline.Options{CaptureDir: bad, NoColor: true}, in, &out, &errOut); err != nil {
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

func TestRun_NativeRenderWhenNoProxy(t *testing.T) {
	ctx := context.Background()
	body := `{"model":{"display_name":"Opus"},"workspace":{"current_dir":"/x"}}`
	zero := 0
	cfg := render.Config{
		Margin: &zero,
		Rows:   []render.Row{{Segments: []render.Segment{{Type: "model"}, {Type: "cwd"}}}},
	}
	var out, errOut bytes.Buffer
	err := statusline.Run(ctx, statusline.Options{Render: cfg, NoColor: true}, strings.NewReader(body), &out, &errOut)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := "Opus | x\n"
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}

func TestRun_NoColorIsRespected(t *testing.T) {
	ctx := context.Background()
	body := `{"model":{"display_name":"Opus"}}`
	cfg := render.Config{
		Rows: []render.Row{{Segments: []render.Segment{{Type: "model", FG: "131"}}}},
	}
	var out bytes.Buffer
	err := statusline.Run(ctx, statusline.Options{Render: cfg, NoColor: true},
		strings.NewReader(body), &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Errorf("NoColor should suppress ANSI, got %q", out.String())
	}
}

func TestRun_DefaultRenderUsedWhenNoConfigAndNoProxy(t *testing.T) {
	ctx := context.Background()
	body := `{"model":{"display_name":"Opus"},"workspace":{"current_dir":"/tmp"}}`
	var out bytes.Buffer
	err := statusline.Run(ctx, statusline.Options{NoColor: true}, strings.NewReader(body), &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// default layout is multi-line and includes the model name
	if !strings.Contains(out.String(), "Opus") {
		t.Errorf("default layout should include model name, got %q", out.String())
	}
	if !strings.Contains(out.String(), "\n") {
		t.Errorf("default layout should be multi-line, got %q", out.String())
	}
}

func TestRun_StdinIsCapped(t *testing.T) {
	// A pathological producer that streams forever would otherwise pin
	// ccsb in io.ReadAll. The 10 MiB LimitReader returns short-read EOF
	// and Run continues to render whatever was already buffered.
	ctx := context.Background()
	in := io.LimitReader(neverEndingReader{b: 'A'}, 64<<20) // 64 MiB upstream
	var out bytes.Buffer
	if err := statusline.Run(ctx, statusline.Options{NoColor: true}, in, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Output is the last-resort fallback: the payload is not valid JSON,
	// so render emits the placeholder line.
	if out.Len() == 0 {
		t.Error("expected fallback output, got empty")
	}
}

type neverEndingReader struct{ b byte }

func (r neverEndingReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.b
	}
	return len(p), nil
}

// --- 0.2.19 schema-drift logger (.diag files) ------------------------------

func TestRun_WritesDiagFileWhenSchemaIssue(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	// Missing session_id and workspace.current_dir — trips MissingCritical.
	body := `{"model":{"display_name":"Opus"}}`
	var out, errOut bytes.Buffer

	if err := statusline.Run(ctx, statusline.Options{CaptureDir: dir, NoColor: true}, strings.NewReader(body), &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	var diagPath string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".diag") {
			diagPath = filepath.Join(dir, e.Name())
		}
	}
	if diagPath == "" {
		t.Fatalf("expected a .diag file in capture dir, got entries:\n%v", entries)
	}
	got, err := os.ReadFile(diagPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(got), "missing critical fields") {
		t.Errorf(".diag content should mention missing critical fields:\n%s", got)
	}
	if !strings.Contains(string(got), "session_id") {
		t.Errorf(".diag content should name the specific missing field:\n%s", got)
	}
}

func TestRun_NoDiagFileOnValidPayload(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	body := `{"session_id":"sid","model":{"display_name":"Opus"},"workspace":{"current_dir":"/x"}}`
	var out, errOut bytes.Buffer

	if err := statusline.Run(ctx, statusline.Options{CaptureDir: dir, NoColor: true}, strings.NewReader(body), &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".diag") {
			t.Errorf("no .diag file should appear for a healthy payload, found %s", e.Name())
		}
	}
}

func TestRun_DiagFileSharesBasenameWithCapture(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	body := `{"model":{"display_name":"Opus"}}` // missing critical → diag fires
	var out, errOut bytes.Buffer

	if err := statusline.Run(ctx, statusline.Options{CaptureDir: dir, NoColor: true}, strings.NewReader(body), &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	var jsonName, diagName string
	for _, e := range entries {
		switch {
		case strings.HasSuffix(e.Name(), ".json"):
			jsonName = e.Name()
		case strings.HasSuffix(e.Name(), ".diag"):
			diagName = e.Name()
		}
	}
	if jsonName == "" || diagName == "" {
		t.Fatalf("need both .json and .diag, got json=%q diag=%q", jsonName, diagName)
	}
	jsonStem := strings.TrimSuffix(jsonName, ".json")
	diagStem := strings.TrimSuffix(diagName, ".diag")
	if jsonStem != diagStem {
		t.Errorf("basenames must match for pairing: json=%s diag=%s", jsonStem, diagStem)
	}
}

// --- 0.2.20 schema-version acknowledge -------------------------------------

// schemaVersionTestEnv prepares a capture directory plus its sibling
// state file location, so the schema_version tests do not have to
// duplicate the layout. Returns (captureDir, stateDir) where
// stateDir is the parent that schemaVersionStatePath() derives.
func schemaVersionTestEnv(t *testing.T) (captureDir, stateFile string) {
	t.Helper()
	root := t.TempDir()
	captureDir = filepath.Join(root, "captures")
	if err := os.MkdirAll(captureDir, 0o700); err != nil {
		t.Fatalf("mkdir captureDir: %v", err)
	}
	stateFile = filepath.Join(root, "schema_version")
	return captureDir, stateFile
}

func TestRun_SchemaVersionFirstSeenIsRecordedSilently(t *testing.T) {
	ctx := context.Background()
	captureDir, statePath := schemaVersionTestEnv(t)
	body := `{"session_id":"s","model":{"display_name":"O"},"workspace":{"current_dir":"/x"},"schema_version":"1.0"}`
	var out, errOut bytes.Buffer

	if err := statusline.Run(ctx, statusline.Options{CaptureDir: captureDir, NoColor: true}, strings.NewReader(body), &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("expected state file written, ReadFile: %v", err)
	}
	if strings.TrimSpace(string(got)) != "1.0" {
		t.Errorf("state file content = %q, want %q", got, "1.0")
	}
	entries, _ := os.ReadDir(captureDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".diag") {
			t.Errorf("first-seen schema_version must not trigger a .diag, got %s", e.Name())
		}
	}
}

func TestRun_SchemaVersionUnchangedIsNoop(t *testing.T) {
	ctx := context.Background()
	captureDir, statePath := schemaVersionTestEnv(t)
	if err := os.WriteFile(statePath, []byte("1.0\n"), 0o600); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	body := `{"session_id":"s","model":{"display_name":"O"},"workspace":{"current_dir":"/x"},"schema_version":"1.0"}`
	var out, errOut bytes.Buffer

	if err := statusline.Run(ctx, statusline.Options{CaptureDir: captureDir, NoColor: true}, strings.NewReader(body), &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}

	entries, _ := os.ReadDir(captureDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".diag") {
			t.Errorf("unchanged schema_version must not trigger a .diag, got %s", e.Name())
		}
	}
}

func TestRun_SchemaVersionChangeTriggersDiagWithFooter(t *testing.T) {
	ctx := context.Background()
	captureDir, statePath := schemaVersionTestEnv(t)
	if err := os.WriteFile(statePath, []byte("1.0\n"), 0o600); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	// Healthy payload otherwise — only the version differs.
	body := `{"session_id":"s","model":{"display_name":"O"},"workspace":{"current_dir":"/x"},"schema_version":"2.0"}`
	var out, errOut bytes.Buffer

	if err := statusline.Run(ctx, statusline.Options{CaptureDir: captureDir, NoColor: true}, strings.NewReader(body), &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// .diag must exist and mention the transition.
	entries, _ := os.ReadDir(captureDir)
	var diagPath string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".diag") {
			diagPath = filepath.Join(captureDir, e.Name())
		}
	}
	if diagPath == "" {
		t.Fatalf("expected a .diag file for schema_version change")
	}
	got, err := os.ReadFile(diagPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(got), "schema_version changed: 1.0 -> 2.0") {
		t.Errorf(".diag should announce the transition, got:\n%s", got)
	}

	// State file updated to the new value.
	stateGot, _ := os.ReadFile(statePath)
	if strings.TrimSpace(string(stateGot)) != "2.0" {
		t.Errorf("state should be updated to 2.0, got %q", stateGot)
	}
}

func TestRun_SchemaVersionMissingDoesNotEraseState(t *testing.T) {
	// If a later payload drops schema_version (downgrade or upstream
	// regression), the previously stored value must persist so the
	// next reappearance can still be diffed against it.
	ctx := context.Background()
	captureDir, statePath := schemaVersionTestEnv(t)
	if err := os.WriteFile(statePath, []byte("1.0\n"), 0o600); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	body := `{"session_id":"s","model":{"display_name":"O"},"workspace":{"current_dir":"/x"}}` // no schema_version
	var out, errOut bytes.Buffer

	if err := statusline.Run(ctx, statusline.Options{CaptureDir: captureDir, NoColor: true}, strings.NewReader(body), &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}

	stateGot, _ := os.ReadFile(statePath)
	if strings.TrimSpace(string(stateGot)) != "1.0" {
		t.Errorf("state should remain at 1.0 when payload omits schema_version, got %q", stateGot)
	}
}
