package capture_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/capture"
)

func TestSave_WritesPayloadAtExpectedPath(t *testing.T) {
	dir := t.TempDir()
	payload := []byte(`{"hello":"world"}`)
	now := time.Date(2026, 5, 10, 14, 30, 45, 123456789, time.UTC)

	path, err := capture.Save(dir, "sess-abc", payload, now)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	want := filepath.Join(dir, "2026-05-10T14:30:45.123456789Z-sess-abc.json")
	if path != want {
		t.Errorf("path: got %q, want %q", path, want)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("file content: got %q, want %q", got, payload)
	}
}

func TestSave_CreatesDirectoryRecursively(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "captures")
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	if _, err := capture.Save(dir, "s", []byte(`{}`), now); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("expected directory at %s", dir)
	}
}

func TestSave_EmptySessionIDUsesPlaceholder(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	path, err := capture.Save(dir, "", []byte(`{}`), now)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !strings.HasSuffix(filepath.Base(path), "-unknown.json") {
		t.Errorf("filename should end with -unknown.json: %s", path)
	}
}

func TestSave_SanitizesSessionID(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	path, err := capture.Save(dir, "../etc/passwd", []byte(`{}`), now)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	parent := filepath.Dir(path)
	if parent != dir {
		t.Errorf("file escaped target dir: parent=%q want=%q", parent, dir)
	}
	base := filepath.Base(path)
	if strings.Contains(base, "/") || strings.Contains(base, "..") {
		t.Errorf("base name not sanitized: %q", base)
	}
}

func TestSave_RejectsEmptyPayload(t *testing.T) {
	if _, err := capture.Save(t.TempDir(), "s", nil, time.Now()); err == nil {
		t.Error("expected error for nil payload")
	}
	if _, err := capture.Save(t.TempDir(), "s", []byte{}, time.Now()); err == nil {
		t.Error("expected error for empty payload")
	}
}

func TestSave_RejectsEmptyDir(t *testing.T) {
	if _, err := capture.Save("", "s", []byte(`{}`), time.Now()); err == nil {
		t.Error("expected error for empty dir")
	}
}

func TestSave_FilePermsAreUserPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "captures")
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	path, err := capture.Save(dir, "s", []byte(`{}`), now)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat file: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("file perm = %#o, want 0o600", perm)
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir perm = %#o, want 0o700", perm)
	}
}

func TestSave_FileContentMatchesExactBytes(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("\x00\x01raw bytes\n{\"x\":1}\n")
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	path, err := capture.Save(dir, "s", payload, now)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("got %q, want %q", got, payload)
	}
}

func TestSave_DoesNotLeaveTempFileOnSuccess(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	if _, err := capture.Save(dir, "s", []byte(`{}`), now); err != nil {
		t.Fatalf("Save: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") || strings.HasPrefix(e.Name(), ".capture-") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

// SaveOutput

func TestSaveOutput_WritesNeighbourFileWithGivenSuffix(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 10, 14, 30, 45, 123456789, time.UTC)

	inPath, err := capture.Save(dir, "sess-abc", []byte(`{"x":1}`), now)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	outPath, err := capture.SaveOutput(dir, "sess-abc", []byte("rendered line\n"), now, "out")
	if err != nil {
		t.Fatalf("SaveOutput: %v", err)
	}

	wantOut := filepath.Join(dir, "2026-05-10T14:30:45.123456789Z-sess-abc.out")
	if outPath != wantOut {
		t.Errorf("output path: got %q, want %q", outPath, wantOut)
	}
	// Same prefix as the input file, just different extension.
	if filepath.Base(outPath[:len(outPath)-len(".out")]) !=
		filepath.Base(inPath[:len(inPath)-len(".json")]) {
		t.Errorf("output should share basename with input:\n in:  %s\n out: %s", inPath, outPath)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "rendered line\n" {
		t.Errorf("content: got %q", got)
	}
}

func TestSaveOutput_AcceptsAnyTextSuffix(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	for _, suffix := range []string{"out", "err", "stdout", "log"} {
		path, err := capture.SaveOutput(dir, "s", []byte("x"), now, suffix)
		if err != nil {
			t.Fatalf("SaveOutput %q: %v", suffix, err)
		}
		if !strings.HasSuffix(path, "."+suffix) {
			t.Errorf("path %q should end in .%s", path, suffix)
		}
	}
}

func TestSaveOutput_RejectsEmptySuffix(t *testing.T) {
	if _, err := capture.SaveOutput(t.TempDir(), "s", []byte("x"), time.Now(), ""); err == nil {
		t.Error("expected error for empty suffix")
	}
}

func TestSaveOutput_RejectsEmptyData(t *testing.T) {
	if _, err := capture.SaveOutput(t.TempDir(), "s", nil, time.Now(), "out"); err == nil {
		t.Error("expected error for nil data")
	}
}

func TestSaveOutput_RejectsEmptyDir(t *testing.T) {
	if _, err := capture.SaveOutput("", "s", []byte("x"), time.Now(), "out"); err == nil {
		t.Error("expected error for empty dir")
	}
}

func TestSaveOutput_PreservesRawBytesIncludingANSI(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	payload := []byte("\x1b[31mred\x1b[0m\n")

	path, err := capture.SaveOutput(dir, "s", payload, now, "out")
	if err != nil {
		t.Fatalf("SaveOutput: %v", err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, payload) {
		t.Errorf("ANSI bytes mangled: got %q, want %q", got, payload)
	}
}

func TestDefaultDir_UsesXDGStateHomeWhenSet(t *testing.T) {
	got := capture.DefaultDir("/home/u", "/var/state")
	want := filepath.Join("/var/state", "ccsb", "captures")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDefaultDir_FallsBackToHomeLocalState(t *testing.T) {
	got := capture.DefaultDir("/home/u", "")
	want := filepath.Join("/home/u", ".local", "state", "ccsb", "captures")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDefaultDir_EmptyWhenNoHomeAndNoXDG(t *testing.T) {
	if got := capture.DefaultDir("", ""); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
