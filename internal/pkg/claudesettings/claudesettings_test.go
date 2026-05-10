package claudesettings_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/claudesettings"
)

func TestDefaultPath_UsesHome(t *testing.T) {
	got := claudesettings.DefaultPath("/home/u")
	want := filepath.Join("/home/u", ".claude", "settings.json")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDefaultPath_EmptyWhenNoHome(t *testing.T) {
	if got := claudesettings.DefaultPath(""); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestLoad_MissingFileReturnsEmptySettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	s, err := claudesettings.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(s) != 0 {
		t.Errorf("expected empty settings, got %v", s)
	}
}

func TestLoad_ParsesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	body := `{
		"statusLine": {"type": "command", "command": "echo hi"},
		"theme": "dark"
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	s, err := claudesettings.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := s["statusLine"]; !ok {
		t.Error("statusLine key missing")
	}
	if _, ok := s["theme"]; !ok {
		t.Error("theme key missing")
	}
}

func TestSaveLoadRoundtrip_PreservesUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	body := `{
		"statusLine": {"type":"command","command":"echo hi","padding":0},
		"theme": "dark",
		"customField": [1, 2, 3]
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	s, err := claudesettings.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := claudesettings.Save(path, s); err != nil {
		t.Fatalf("Save: %v", err)
	}

	s2, err := claudesettings.Load(path)
	if err != nil {
		t.Fatalf("Load round 2: %v", err)
	}

	for _, key := range []string{"statusLine", "theme", "customField"} {
		v1, v2 := s[key], s2[key]
		if !equalJSON(t, v1, v2) {
			t.Errorf("key %q changed across roundtrip:\n round1=%s\n round2=%s", key, v1, v2)
		}
	}
}

func TestGetStatusLine_PresentReturnsValueAndTrue(t *testing.T) {
	s := claudesettings.Settings{
		"statusLine": json.RawMessage(`{"type":"command","command":"x"}`),
	}
	v, ok := claudesettings.GetStatusLine(s)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !equalJSON(t, v, json.RawMessage(`{"type":"command","command":"x"}`)) {
		t.Errorf("value mismatch: %s", v)
	}
}

func TestGetStatusLine_AbsentReturnsFalse(t *testing.T) {
	s := claudesettings.Settings{}
	if _, ok := claudesettings.GetStatusLine(s); ok {
		t.Error("expected ok=false for missing statusLine")
	}
}

func TestSetStatusLine_AddsKeyAndOverwrites(t *testing.T) {
	s := claudesettings.Settings{}
	claudesettings.SetStatusLine(s, json.RawMessage(`{"type":"command","command":"first"}`))
	if _, ok := s["statusLine"]; !ok {
		t.Error("expected key to be added")
	}
	claudesettings.SetStatusLine(s, json.RawMessage(`{"type":"command","command":"second"}`))
	if !equalJSON(t, s["statusLine"], json.RawMessage(`{"type":"command","command":"second"}`)) {
		t.Errorf("expected overwrite, got %s", s["statusLine"])
	}
}

func TestRemoveStatusLine_DeletesKey(t *testing.T) {
	s := claudesettings.Settings{
		"statusLine": json.RawMessage(`{"type":"command","command":"x"}`),
		"theme":      json.RawMessage(`"dark"`),
	}
	claudesettings.RemoveStatusLine(s)
	if _, ok := s["statusLine"]; ok {
		t.Error("expected statusLine to be removed")
	}
	if _, ok := s["theme"]; !ok {
		t.Error("expected unrelated key to remain")
	}
}

func TestSave_CreatesParentDirAndAtomicallyWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", ".claude", "settings.json")
	s := claudesettings.Settings{"theme": json.RawMessage(`"dark"`)}
	if err := claudesettings.Save(path, s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if e.Name() != "settings.json" {
			t.Errorf("leftover file: %s", e.Name())
		}
	}
}

func TestSave_FileIsValidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := claudesettings.Settings{
		"statusLine": json.RawMessage(`{"type":"command","command":"x"}`),
		"theme":      json.RawMessage(`"dark"`),
	}
	if err := claudesettings.Save(path, s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var any map[string]any
	if err := json.Unmarshal(data, &any); err != nil {
		t.Errorf("written file is not valid JSON: %v\ncontent: %s", err, data)
	}
}

func TestSave_FilePermissionsAreUserOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := claudesettings.Save(path, claudesettings.Settings{}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Errorf("file should not be group/other readable, got mode %o", mode)
	}
}

func TestLoad_InvalidJSONReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := claudesettings.Load(path); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// equalJSON compares two json.RawMessage values by parsing both, ignoring
// whitespace and key order.
func equalJSON(t *testing.T, a, b json.RawMessage) bool {
	t.Helper()
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		t.Fatalf("unmarshal a: %v", err)
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		t.Fatalf("unmarshal b: %v", err)
	}
	return reflect.DeepEqual(av, bv)
}
