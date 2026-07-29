package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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

func TestLoadUpdateAuto(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	raw := `{"update":{"auto":"patch"},"proxy":{"command":"foo"}}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Update.Auto != "patch" {
		t.Errorf("Update.Auto = %q, want %q", c.Update.Auto, "patch")
	}
	if c.Proxy.Command != "foo" {
		t.Errorf("Proxy.Command = %q — the update block must not disturb siblings", c.Proxy.Command)
	}
}

func TestUpdateAutoRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(path, config.Config{Update: config.Update{Auto: "minor"}}); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Update.Auto != "minor" {
		t.Errorf("Update.Auto = %q after round-trip, want %q", c.Update.Auto, "minor")
	}
}

// An absent update block must stay absent on save — opt-in means an
// untouched config gains no new keys.
func TestUpdateBlockOmittedWhenEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(path, config.Config{Proxy: config.Proxy{Command: "foo"}}); err != nil {
		t.Fatal(err)
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Unmarshal into a key/value map: the exact-key check catches the
	// ordinary regression with a precise message, and the case-insensitive
	// walk also catches a dropped/mistyped `omitzero` tag, which would
	// serialize the field as "Update" instead of "update".
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(blob, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := fields["update"]; ok {
		t.Errorf("saved config has an \"update\" key:\n%s", blob)
	}
	for k := range fields {
		if strings.EqualFold(k, "update") {
			t.Errorf("saved config has key %q — check the json tag:\n%s", k, blob)
		}
	}
}

func TestProxyTimeout(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"absent falls back to the default", "", config.DefaultProxyTimeout},
		{"explicit duration", "3s", 3 * time.Second},
		{"minutes", "2m", 2 * time.Minute},
		// An explicit zero is an opt-out, not a missing value: it must not be
		// turned back into the default, or there would be no way to keep the
		// old unbounded behaviour.
		{"explicit zero means no limit", "0", 0},
		{"explicit zero with unit", "0s", 0},
		// Garbage falls back rather than failing the whole config: a status
		// bar that vanishes because of a typo in a timeout would be a worse
		// outcome than a sane default.
		{"unparsable falls back", "soon", config.DefaultProxyTimeout},
		{"negative clamps to no limit", "-5s", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := config.Proxy{Timeout: tc.value}.ProxyTimeout()
			if got != tc.want {
				t.Errorf("ProxyTimeout(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

// The timeout must survive a config round-trip, or `ccsb mode` and doctor
// would quietly drop a value the user set.
func TestProxyTimeoutRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	want := config.Config{Proxy: config.Proxy{Command: "npx", Timeout: "45s"}}
	if err := config.Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Proxy.Timeout != "45s" {
		t.Errorf("timeout: got %q, want %q", got.Proxy.Timeout, "45s")
	}
	if got.Proxy.ProxyTimeout() != 45*time.Second {
		t.Errorf("resolved: got %v, want 45s", got.Proxy.ProxyTimeout())
	}
}

// config.json is the user's file, not ccsb's. Until 0.4.21 Save was a plain
// MarshalIndent of the struct, so every top-level key the running binary did
// not model was dropped the next time anything wrote the file — and `install`,
// `mode` and `doctor` all write it. The realistic loss is not an exotic
// hand-added key but a version skew: an OLDER ccsb (say one still on the
// system from a package manager) runs `ccsb mode native` over a config a
// NEWER one wrote, and silently deletes the block it never learnt about.
// claudesettings has preserved unknown keys for exactly this reason since the
// beginning; this brings config in line.
func TestSaveLoadRoundtrip_PreservesUnknownTopLevelKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := `{
  "proxy": {"command": "npx", "args": ["-y", "ccstatusline@latest"]},
  "future_block": {"nested": {"deep": [1, 2, 3]}, "flag": true},
  "scalar_from_the_future": 42
}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Proxy.Command != "npx" {
		t.Fatalf("modelled key did not survive Load: %+v", cfg.Proxy)
	}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var got map[string]json.RawMessage
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("rewritten config does not parse: %v\n%s", err, raw)
	}
	for _, key := range []string{"future_block", "scalar_from_the_future"} {
		if _, ok := got[key]; !ok {
			t.Errorf("Save dropped the unmodelled key %q:\n%s", key, raw)
		}
	}
	// Values, not just presence — a preserved key that lost its contents
	// would be no better.
	var block struct {
		Nested struct{ Deep []int } `json:"nested"`
		Flag   bool                 `json:"flag"`
	}
	if err := json.Unmarshal(got["future_block"], &block); err != nil {
		t.Fatalf("future_block no longer decodes: %v", err)
	}
	if !reflect.DeepEqual(block.Nested.Deep, []int{1, 2, 3}) || !block.Flag {
		t.Errorf("future_block lost content: %+v", block)
	}
	if string(got["scalar_from_the_future"]) != "42" {
		t.Errorf("scalar key: got %s, want 42", got["scalar_from_the_future"])
	}
}

// The modelled keys must not also end up in the carried-over set, or a stale
// copy of one would outlive an edit to it.
func TestSaveLoadRoundtrip_ModelledKeyEditsWinOverTheLoadedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"proxy":{"command":"old"},"mystery":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Proxy.Command = "new"
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Proxy.Command != "new" {
		t.Errorf("proxy.command: got %q, want new", reloaded.Proxy.Command)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "mystery") {
		t.Errorf("the unmodelled key was dropped:\n%s", raw)
	}
}

// `ccsb config reset` deliberately builds a fresh Config carrying only the
// uninstall backup, and must NOT resurrect anything: the whole point is that
// the previous file moved aside to .bak. This is why the carried-over keys
// live on the loaded value rather than being re-read from disk inside Save.
func TestSave_FreshConfigCarriesNothingOver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"mystery":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(path, config.Config{}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "mystery") {
		t.Errorf("Save resurrected a key from the file it overwrote:\n%s", raw)
	}
}
