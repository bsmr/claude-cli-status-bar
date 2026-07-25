package capture_test

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
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

func TestTimeFromName_ParsesTheBasenameTimestamp(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want time.Time
		ok   bool
	}{
		{"json capture", "2026-05-10T14:30:45.123456789Z-sess-abc.json", time.Date(2026, 5, 10, 14, 30, 45, 123456789, time.UTC), true},
		{"out sibling", "2026-05-10T14:30:45.123456789Z-sess-abc.out", time.Date(2026, 5, 10, 14, 30, 45, 123456789, time.UTC), true},
		{"trailing zeros trimmed", "2026-01-01T00:00:00Z-s.json", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), true},
		{"no Z at all", "notes.json", time.Time{}, false},
		{"Z but unparsable", "2026-13-45T99:99:99Z-x.json", time.Time{}, false},
		{"empty", "", time.Time{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := capture.TimeFromName(tt.in)
			if ok != tt.ok {
				t.Fatalf("ok: got %v, want %v", ok, tt.ok)
			}
			if !got.Equal(tt.want) {
				t.Errorf("time: got %v, want %v", got, tt.want)
			}
		})
	}
}

// writeCapture creates a capture-shaped file so Prune has something with a
// parsable name to act on. Content is irrelevant to pruning.
func writeCapture(t *testing.T, dir string, at time.Time, session, ext string) string {
	t.Helper()
	name := at.UTC().Format(time.RFC3339Nano) + "-" + session + "." + ext
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile %s: %v", name, err)
	}
	return name
}

func remaining(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return names
}

func TestPrune_RemovesOnlyFilesOlderThanCutoff(t *testing.T) {
	dir := t.TempDir()
	old := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	writeCapture(t, dir, old, "a", "json")
	writeCapture(t, dir, old, "a", "out")
	keepJSON := writeCapture(t, dir, recent, "b", "json")

	cutoff := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	removed, err := capture.Prune(dir, cutoff)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if removed != 2 {
		t.Errorf("removed: got %d, want 2", removed)
	}
	if got, want := remaining(t, dir), []string{keepJSON}; !slices.Equal(got, want) {
		t.Errorf("remaining: got %v, want %v", got, want)
	}
}

// "before cutoff" is exclusive: a capture stamped exactly at the cutoff is
// newer-or-equal and stays. Unobservable in practice (cutoffs come from
// time.Now() and basenames carry nanoseconds) but it is the documented edge.
func TestPrune_KeepsACaptureStampedExactlyAtTheCutoff(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	name := writeCapture(t, dir, at, "edge", "json")

	removed, err := capture.Prune(dir, at)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if removed != 0 {
		t.Errorf("removed: got %d, want 0", removed)
	}
	if got, want := remaining(t, dir), []string{name}; !slices.Equal(got, want) {
		t.Errorf("remaining: got %v, want %v", got, want)
	}
}

func TestPrune_CutoffNowRemovesEverything(t *testing.T) {
	dir := t.TempDir()
	writeCapture(t, dir, time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC), "a", "json")
	writeCapture(t, dir, time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC), "b", "diag")

	removed, err := capture.Prune(dir, time.Now())
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if removed != 2 {
		t.Errorf("removed: got %d, want 2", removed)
	}
	if got := remaining(t, dir); len(got) != 0 {
		t.Errorf("remaining: got %v, want none", got)
	}
}

func TestPrune_NeverTouchesFilesWithoutAParsableTimestamp(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"notes.json", "2026-13-45T99:99:99Z-x.json", "README"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}

	removed, err := capture.Prune(dir, time.Now())
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if removed != 0 {
		t.Errorf("removed: got %d, want 0", removed)
	}
	if got := remaining(t, dir); len(got) != 3 {
		t.Errorf("remaining: got %v, want all 3 kept", got)
	}
}

func TestPrune_MissingDirectoryIsNotAnError(t *testing.T) {
	removed, err := capture.Prune(filepath.Join(t.TempDir(), "absent"), time.Now())
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if removed != 0 {
		t.Errorf("removed: got %d, want 0", removed)
	}
}

func TestPrune_SkipsSubdirectories(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "2026-05-01T12:00:00Z-nested")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	removed, err := capture.Prune(dir, time.Now())
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if removed != 0 {
		t.Errorf("removed: got %d, want 0", removed)
	}
	if _, err := os.Stat(sub); err != nil {
		t.Errorf("subdirectory was removed: %v", err)
	}
}

func TestPrune_EmptyDirArgumentIsAnError(t *testing.T) {
	if _, err := capture.Prune("", time.Now()); err == nil {
		t.Fatal("expected an error for an empty dir")
	}
}

func TestPrune_UnreadableDirectoryIsReported(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	dir := filepath.Join(t.TempDir(), "captures")
	if err := os.Mkdir(dir, 0o000); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if _, err := capture.Prune(dir, time.Now()); err == nil {
		t.Fatal("expected an error for an unreadable dir")
	}
}

// A file cannot be unlinked when its PARENT directory is not writable, so an
// r-x capture dir drives the remove failure without touching the file itself.
func TestPrune_RemoveFailureAbortsAndIsReported(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	dir := t.TempDir()
	writeCapture(t, dir, time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC), "a", "json")
	writeCapture(t, dir, time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC), "b", "json")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	removed, err := capture.Prune(dir, time.Now())
	if err == nil {
		t.Fatal("expected the remove failure to be reported")
	}
	if !strings.Contains(err.Error(), "remove") {
		t.Errorf("error should name the failing operation, got: %v", err)
	}
	if removed != 0 {
		t.Errorf("removed: got %d, want 0", removed)
	}
	if got := remaining(t, dir); len(got) != 2 {
		t.Errorf("both files should remain, %d do: %v", len(got), got)
	}
}
