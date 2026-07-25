package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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
