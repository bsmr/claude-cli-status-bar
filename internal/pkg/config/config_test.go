package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/config"
	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/render"
)

func TestDefaultPath_UsesXDGConfigHomeWhenSet(t *testing.T) {
	got := config.DefaultPath("/home/u", "/etc/xdg")
	want := filepath.Join("/etc/xdg", "ccsb", "config.json")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDefaultPath_FallsBackToHomeConfig(t *testing.T) {
	got := config.DefaultPath("/home/u", "")
	want := filepath.Join("/home/u", ".config", "ccsb", "config.json")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDefaultPath_EmptyWhenNoHomeAndNoXDG(t *testing.T) {
	if got := config.DefaultPath("", ""); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestLoad_MissingFileReturnsZeroConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(cfg, config.Config{}) {
		t.Errorf("expected zero Config, got %+v", cfg)
	}
}

func TestLoad_InvalidJSONReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := config.Load(path); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestSave_EmptyPathReturnsError(t *testing.T) {
	err := config.Save("", config.Config{})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	if !strings.Contains(err.Error(), "empty path") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSave_CreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "ccsb", "config.json")
	cfg := config.Config{
		Proxy: config.Proxy{Command: "npx", Args: []string{"-y", "ccstatusline@latest"}},
	}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat: %v", err)
	}
}

func TestSaveLoadRoundtrip_ProxyFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	in := config.Config{
		Proxy: config.Proxy{Command: "npx", Args: []string{"-y", "ccstatusline@latest"}},
	}
	if err := config.Save(path, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Errorf("roundtrip mismatch:\n got %+v\nwant %+v", out, in)
	}
}

func TestSaveLoadRoundtrip_PreservesPreviousStatusLineRawJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := json.RawMessage(`{"type":"command","command":"echo hi","padding":2}`)
	in := config.Config{
		Backup: config.Backup{PreviousStatusLine: original},
	}
	if err := config.Save(path, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Compare by parsing both — JSON whitespace and field order may differ.
	var gotV, wantV any
	if err := json.Unmarshal(out.Backup.PreviousStatusLine, &gotV); err != nil {
		t.Fatalf("unmarshal got: %v", err)
	}
	if err := json.Unmarshal(original, &wantV); err != nil {
		t.Fatalf("unmarshal want: %v", err)
	}
	if !reflect.DeepEqual(gotV, wantV) {
		t.Errorf("PreviousStatusLine roundtrip:\n got %#v\nwant %#v", gotV, wantV)
	}
}

func TestSave_FilePermissionsAreUserReadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(path, config.Config{}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	mode := info.Mode().Perm()
	if mode&0o400 == 0 {
		t.Errorf("file should be user-readable, got mode %o", mode)
	}
	if mode&0o077 != 0 {
		t.Errorf("file should not be group/other readable, got mode %o", mode)
	}
}

func TestSave_DoesNotLeaveTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := config.Save(path, config.Config{}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".tmp") || strings.HasPrefix(name, ".config-") {
			t.Errorf("leftover temp file: %s", name)
		}
	}
}

func TestSaveLoadRoundtrip_PreservesRenderConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	in := config.Config{
		Render: render.Config{
			Rows: []render.Row{
				{Segments: []render.Segment{{Type: "model", Show1MFlag: true}, {Type: "cost"}}},
				{Segments: []render.Segment{{Type: "git_branch"}}},
			},
			Separator: " · ",
		},
	}
	if err := config.Save(path, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Errorf("roundtrip mismatch:\n got %+v\nwant %+v", out, in)
	}
}
