package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/render"
)

func TestLatestCaptureJSON_EmptyDir(t *testing.T) {
	if got := latestCaptureJSON(t.TempDir()); got != "" {
		t.Errorf("empty dir should return empty, got %q", got)
	}
}

func TestLatestCaptureJSON_MissingDir(t *testing.T) {
	if got := latestCaptureJSON(filepath.Join(t.TempDir(), "does-not-exist")); got != "" {
		t.Errorf("missing dir should return empty, got %q", got)
	}
}

func TestLatestCaptureJSON_NoMatchingFiles(t *testing.T) {
	dir := t.TempDir()
	// Neither the paired capture outputs, a foreign .json without a
	// capture timestamp, nor a name whose leading "…Z" is not a valid
	// RFC3339Nano stamp may be picked up.
	for _, n := range []string{"x.out", "x.err", "notes.json", "2026-13-45T99:99:99Z-x.json"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatalf("setup %s: %v", n, err)
		}
	}
	if got := latestCaptureJSON(dir); got != "" {
		t.Errorf("dir without a timestamped .json should return empty, got %q", got)
	}
}

func TestLatestCaptureJSON_PicksChronologicallyLatest(t *testing.T) {
	dir := t.TempDir()
	// Names follow capture.basename: <RFC3339Nano>-<id>.json.
	names := []string{
		"2026-05-10T10:00:00Z-aaa.json",
		"2026-05-12T09:00:00Z-bbb.json",
		"2026-05-11T23:59:59Z-ccc.json",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(`{}`), 0o600); err != nil {
			t.Fatalf("setup %s: %v", n, err)
		}
	}
	got := latestCaptureJSON(dir)
	want := filepath.Join(dir, "2026-05-12T09:00:00Z-bbb.json")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// RFC3339Nano trims trailing zeros from the fractional second, so capture
// names are not fixed width and lexicographic order is not chronological:
// ".1Z" (100ms) sorts after ".10001Z" (100.01ms) because 'Z' > '0'.
func TestLatestCaptureJSON_SameSecondDifferentNanoWidths(t *testing.T) {
	dir := t.TempDir()
	older := "2026-05-26T12:00:00.1Z-aaa.json"
	newer := "2026-05-26T12:00:00.10001Z-bbb.json"
	for _, n := range []string{older, newer} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(`{}`), 0o600); err != nil {
			t.Fatalf("setup %s: %v", n, err)
		}
	}
	if !(older > newer) {
		t.Fatal("premise broken: the older name no longer sorts last")
	}
	got := latestCaptureJSON(dir)
	want := filepath.Join(dir, newer)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSchemaCheck_NoCapture(t *testing.T) {
	got := schemaCheck(t.TempDir())
	if got.Note == "" {
		t.Errorf("expected a Note when no capture is available; got %+v", got)
	}
	if got.CapturePath != "" || len(got.Missing) > 0 || len(got.Extra) > 0 {
		t.Errorf("only Note should be set when no capture is available; got %+v", got)
	}
}

func TestSchemaCheck_ValidPayloadHasNoDrift(t *testing.T) {
	dir := t.TempDir()
	// Include every key from render.ExpectedPayloadKeys with a
	// reasonable shape so neither missing nor extra fires.
	raw := `{
		"session_id": "s",
		"session_name": "name",
		"model": {"display_name": "Opus"},
		"workspace": {"current_dir": "/x"},
		"output_style": {"name": "default"},
		"effort": {"level": "high"},
		"cost": {"total_cost_usd": 0},
		"context_window": {"used_percentage": 0, "context_window_size": 0},
		"rate_limits": {},
		"fast_mode": false,
		"thinking": {"enabled": false},
		"exceeds_200k_tokens": false,
		"schema_version": "1.0"
	}`
	if err := os.WriteFile(filepath.Join(dir, "2026-05-26T12:00:00Z-x.json"), []byte(raw), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	got := schemaCheck(dir)
	if got.Note != "" {
		t.Errorf("expected no Note, got %q", got.Note)
	}
	if len(got.Missing) > 0 {
		t.Errorf("expected no Missing, got %v", got.Missing)
	}
	if len(got.Extra) > 0 {
		t.Errorf("expected no Extra, got %v", got.Extra)
	}
}

func TestSchemaCheck_MissingKeys(t *testing.T) {
	dir := t.TempDir()
	// Only the three critical fields plus a couple extras.
	raw := `{
		"session_id": "s",
		"model": {"display_name": "Opus"},
		"workspace": {"current_dir": "/x"}
	}`
	if err := os.WriteFile(filepath.Join(dir, "2026-05-26T12:00:00Z-x.json"), []byte(raw), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	got := schemaCheck(dir)
	for _, want := range []string{"cost", "context_window", "rate_limits"} {
		if !slices.Contains(got.Missing, want) {
			t.Errorf("expected %q in Missing, got %v", want, got.Missing)
		}
	}
	if len(got.Extra) != 0 {
		t.Errorf("expected no Extra, got %v", got.Extra)
	}
}

func TestSchemaCheck_ExtraKeys(t *testing.T) {
	dir := t.TempDir()
	raw := `{
		"session_id": "s",
		"session_name": "name",
		"model": {"display_name": "Opus"},
		"workspace": {"current_dir": "/x"},
		"output_style": {"name": "default"},
		"effort": {"level": "high"},
		"cost": {"total_cost_usd": 0},
		"context_window": {},
		"rate_limits": {},
		"fast_mode": false,
		"thinking": {"enabled": false},
		"exceeds_200k_tokens": false,
		"schema_version": "1.0",
		"shiny_new_field": {"a": 1},
		"another_one": "yes"
	}`
	if err := os.WriteFile(filepath.Join(dir, "2026-05-26T12:00:00Z-x.json"), []byte(raw), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	got := schemaCheck(dir)
	if len(got.Missing) > 0 {
		t.Errorf("expected no Missing, got %v", got.Missing)
	}
	for _, want := range []string{"shiny_new_field", "another_one"} {
		if !slices.Contains(got.Extra, want) {
			t.Errorf("expected %q in Extra, got %v", want, got.Extra)
		}
	}
}

func TestSchemaCheck_NotJSONObject(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "2026-05-26T12:00:00Z-x.json"), []byte(`[1,2,3]`), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	got := schemaCheck(dir)
	if !strings.Contains(got.Note, "not a JSON object") {
		t.Errorf("expected Note about JSON object, got %q", got.Note)
	}
}

// updateDiagnostic maps a block reason to its explanation. Every reason gets
// a case, so a user who sees the ⊘ glyph always finds the same answer here —
// the glyph and this line are driven by the same guards.
func TestUpdateDiagnostic(t *testing.T) {
	tests := []struct {
		name   string
		reason render.BlockReason
		want   string // substring the message must contain
	}{
		{"windows", render.BlockWindows, "windows"},
		{"local build", render.BlockLocalBuild, "local build"},
		{"not writable", render.BlockNotWritable, "not writable"},
		{"unknown reason", render.BlockReason("something-new"), "something-new"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := updateDiagnostic(tt.reason)
			if !strings.Contains(got, tt.want) {
				t.Errorf("updateDiagnostic(%q) = %q, want it to mention %q", tt.reason, got, tt.want)
			}
			if !strings.HasPrefix(got, "self-update: ") {
				t.Errorf("updateDiagnostic(%q) = %q, want the self-update: prefix", tt.reason, got)
			}
			if strings.Contains(got, "ccsb: doctor:") {
				t.Errorf("updateDiagnostic(%q) = %q, must not repeat the caller's prefix", tt.reason, got)
			}
		})
	}
}

func TestUpdateDiagnosticSilentWhenNotBlocked(t *testing.T) {
	if got := updateDiagnostic(render.BlockNone); got != "" {
		t.Errorf("updateDiagnostic(BlockNone) = %q, want empty", got)
	}
}

// runDoctor must reach updateDiagnostic through the real guard, not just have
// the mapping tested in isolation. A read-only directory holding the binary is
// the reason that reproduces reliably here: a test binary carries no vcs.*
// stamps, so the local-build guard does not fire for it.
func TestDoctorReportsBlockedSelfUpdate(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	home := t.TempDir()
	settings := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o700); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	self := filepath.Join(binDir, "ccsb")
	if err := os.WriteFile(self, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte(`{"statusLine":{"type":"command","command":"`+self+`"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(binDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(binDir, 0o700) })

	var out strings.Builder
	p := Paths{Settings: settings, Config: filepath.Join(t.TempDir(), "config.json"),
		Capture: t.TempDir(), State: t.TempDir(), Self: self}
	if err := runDoctor(p, &out); err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
	if !strings.Contains(out.String(), "self-update: blocked (target directory not writable") {
		t.Errorf("doctor output lacks the blocked line:\n%s", out.String())
	}
}

func TestAutoUpdateDiagnostic(t *testing.T) {
	tests := []struct {
		in   string
		want string // "" means silent
	}{
		{"", ""},
		{"patch", ""},
		{"minor", ""},
		{"major", ""},
		{"PATCH", "PATCH"},
		{"always", "always"},
	}
	for _, tt := range tests {
		got := autoUpdateDiagnostic(tt.in)
		if tt.want == "" {
			if got != "" {
				t.Errorf("autoUpdateDiagnostic(%q) = %q, want silence", tt.in, got)
			}
			continue
		}
		if !strings.Contains(got, tt.want) {
			t.Errorf("autoUpdateDiagnostic(%q) = %q, want it to quote the bad value", tt.in, got)
		}
		if !strings.Contains(got, "patch") || !strings.Contains(got, "minor") || !strings.Contains(got, "major") {
			t.Errorf("autoUpdateDiagnostic(%q) = %q, want it to list the accepted values", tt.in, got)
		}
	}
}

// The wizard asset ships inside the binary, but `ccsb install-skill` writes a
// COPY into ~/.claude/skills/. Updating ccsb therefore does not update an
// already-installed skill: v0.4.13 corrected guidance that could blank the
// user's status bar, and every prior installation kept serving the dangerous
// version with nothing to indicate it. doctor is where that gets noticed.
func TestSkillDiagnostic(t *testing.T) {
	embedded := WizardSkillContent()

	tests := []struct {
		name    string
		write   func(t *testing.T, current, legacy string)
		wantSub string // "" means: report nothing
	}{
		{
			name:  "not installed is not a problem",
			write: func(*testing.T, string, string) {},
		},
		{
			name: "up to date",
			write: func(t *testing.T, current, _ string) {
				writeSkillFile(t, current, embedded)
			},
		},
		{
			name: "stale copy",
			write: func(t *testing.T, current, _ string) {
				writeSkillFile(t, current, []byte("# an older ccsb-wizard\n"))
			},
			wantSub: "differs from this binary",
		},
		{
			name: "legacy flat file left behind",
			write: func(t *testing.T, current, legacy string) {
				writeSkillFile(t, current, embedded)
				writeSkillFile(t, legacy, embedded)
			},
			wantSub: "legacy",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := Paths{ClaudeSkillsDir: filepath.Join(t.TempDir(), "skills")}
			current, legacy := skillPaths(p)
			tc.write(t, current, legacy)

			got := skillDiagnostic(p)
			if tc.wantSub == "" {
				if got != "" {
					t.Errorf("expected no diagnostic, got %q", got)
				}
				return
			}
			if !strings.Contains(got, tc.wantSub) {
				t.Errorf("diagnostic %q does not mention %q", got, tc.wantSub)
			}
			if !strings.Contains(got, "install-skill") {
				t.Errorf("diagnostic %q does not name the fix (install-skill)", got)
			}
		})
	}
}

func writeSkillFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
