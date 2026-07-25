package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeCaptureAt drops a capture-shaped file into dir so the clean verb has
// something with a parsable timestamp to act on.
func writeCaptureAt(t *testing.T, dir string, at time.Time, session, ext string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	name := at.UTC().Format(time.RFC3339Nano) + "-" + session + "." + ext
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile %s: %v", name, err)
	}
	return name
}

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	return len(entries)
}

func TestRunCaptures_NoSubcommandIsError(t *testing.T) {
	var out bytes.Buffer
	err := runCaptures(Paths{Capture: t.TempDir()}, nil, &out)
	if err == nil {
		t.Fatal("expected error for missing subcommand")
	}
	if !strings.Contains(err.Error(), "clean") {
		t.Errorf("error should hint at the valid verb: %v", err)
	}
}

func TestRunCaptures_UnknownSubcommandIsError(t *testing.T) {
	var out bytes.Buffer
	err := runCaptures(Paths{Capture: t.TempDir()}, []string{"frobnicate"}, &out)
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
	if !strings.Contains(err.Error(), "frobnicate") {
		t.Errorf("error should name the bad verb: %v", err)
	}
}

func TestRunCapturesClean_WithoutFlagsRemovesEverything(t *testing.T) {
	dir := t.TempDir()
	writeCaptureAt(t, dir, time.Now().Add(-48*time.Hour), "a", "json")
	writeCaptureAt(t, dir, time.Now().Add(-1*time.Minute), "b", "json")
	writeCaptureAt(t, dir, time.Now().Add(-1*time.Minute), "b", "out")

	var out bytes.Buffer
	if err := runCaptures(Paths{Capture: dir}, []string{"clean"}, &out); err != nil {
		t.Fatalf("runCaptures: %v", err)
	}
	if n := countFiles(t, dir); n != 0 {
		t.Errorf("%d files remain, want 0", n)
	}
	// Anchor on "removed 3 " — a bare "3" would also match the random digits
	// t.TempDir() puts in the path, which is how a hardcoded count slipped
	// past this test once already. The trailing space stops "3" matching "30".
	if !strings.Contains(out.String(), "removed 3 ") {
		t.Errorf("output should report the true count, got: %q", out.String())
	}
}

func TestRunCapturesClean_OlderThanKeepsRecentCaptures(t *testing.T) {
	dir := t.TempDir()
	writeCaptureAt(t, dir, time.Now().Add(-10*24*time.Hour), "old", "json")
	keep := writeCaptureAt(t, dir, time.Now().Add(-1*time.Hour), "new", "json")

	var out bytes.Buffer
	if err := runCaptures(Paths{Capture: dir}, []string{"clean", "--older-than", "7d"}, &out); err != nil {
		t.Fatalf("runCaptures: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != keep {
		t.Errorf("expected only %s to survive, got %v", keep, entries)
	}
}

func TestRunCapturesClean_AcceptsGoDurationSyntax(t *testing.T) {
	dir := t.TempDir()
	writeCaptureAt(t, dir, time.Now().Add(-48*time.Hour), "old", "json")
	keep := writeCaptureAt(t, dir, time.Now().Add(-1*time.Hour), "new", "json")

	var out bytes.Buffer
	if err := runCaptures(Paths{Capture: dir}, []string{"clean", "--older-than", "24h"}, &out); err != nil {
		t.Fatalf("runCaptures: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != keep {
		t.Errorf("expected only %s to survive, got %v", keep, entries)
	}
}

// Prune returns the count removed before it hit the failure, and the docs
// promise the user is told what a partial sweep accomplished. Without a
// writable directory nothing can be removed, so the count here is 0 — what
// this pins is that the caller REPORTS it instead of discarding it.
func TestRunCapturesClean_AbortReportsWhatWasAlreadyRemoved(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	dir := t.TempDir()
	writeCaptureAt(t, dir, time.Now().Add(-time.Hour), "a", "json")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	var out bytes.Buffer
	err := runCaptures(Paths{Capture: dir}, []string{"clean"}, &out)
	if err == nil {
		t.Fatal("expected the remove failure to surface")
	}
	if !strings.Contains(err.Error(), "remove") {
		t.Errorf("error should name the failing operation, got: %v", err)
	}
	if !strings.Contains(out.String(), "removed 0 ") {
		t.Errorf("a partial sweep must report its count, got: %q", out.String())
	}
}

func TestRunCapturesClean_MissingDirectoryIsFriendlyNoop(t *testing.T) {
	var out bytes.Buffer
	p := Paths{Capture: filepath.Join(t.TempDir(), "never-created")}
	if err := runCaptures(p, []string{"clean"}, &out); err != nil {
		t.Fatalf("runCaptures: %v", err)
	}
	if out.Len() == 0 {
		t.Error("expected a friendly message, got no output")
	}
}

func TestRunCapturesClean_RejectsBadArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"flag without value", []string{"clean", "--older-than"}, "older-than"},
		{"unparsable duration", []string{"clean", "--older-than", "next tuesday"}, "next tuesday"},
		// Quoted so the assertion cannot be satisfied by the "d" in
		// "duration" — every error message in this file contains one.
		{"bare day suffix", []string{"clean", "--older-than", "d"}, `"d"`},
		{"negative duration", []string{"clean", "--older-than", "-3d"}, "negative"},
		{"day count overflowing time.Duration", []string{"clean", "--older-than", "1000000d"}, "out of range"},
		{"unknown flag", []string{"clean", "--force"}, "--force"},
		{"stray argument", []string{"clean", "7d"}, "7d"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			err := runCaptures(Paths{Capture: t.TempDir()}, tt.args, &out)
			if err == nil {
				t.Fatalf("expected an error for %v", tt.args)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error should mention %q, got: %v", tt.want, err)
			}
		})
	}
}

func TestParseRetention(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"7d", 7 * 24 * time.Hour, true},
		{"1d", 24 * time.Hour, true},
		{"90d", 90 * 24 * time.Hour, true},
		{"24h", 24 * time.Hour, true},
		{"30m", 30 * time.Minute, true},
		{"1h30m", 90 * time.Minute, true},
		{"0d", 0, true},
		{"d", 0, false},
		{"7dd", 0, false},
		{"-3d", 0, false},
		{"-1h", 0, false},
		{"", 0, false},
		{"next tuesday", 0, false},
		{"7 d", 0, false},
		// time.Duration is int64 nanoseconds, so it tops out near 292 years.
		// Beyond that the day multiply wraps: 106752d goes negative (cutoff
		// lands in the FUTURE and everything is deleted) and 213504d wraps
		// back to a tiny positive 25m26s. Both must be rejected outright.
		{"106751d", 106751 * 24 * time.Hour, true},
		{"106752d", 0, false},
		{"213504d", 0, false},
		{"9223372036854775807d", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseRetention(tt.in)
			if tt.ok && err != nil {
				t.Fatalf("parseRetention(%q): unexpected error %v", tt.in, err)
			}
			if !tt.ok {
				if err == nil {
					t.Fatalf("parseRetention(%q): expected an error, got %v", tt.in, got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("parseRetention(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
