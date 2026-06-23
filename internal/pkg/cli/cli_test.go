package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/claudesettings"
	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/cli"
	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/config"
	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/render"
)

type env struct {
	paths cli.Paths
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	return &env{
		paths: cli.Paths{
			Settings:        filepath.Join(dir, "settings.json"),
			Config:          filepath.Join(dir, "ccsb-config.json"),
			Capture:         filepath.Join(dir, "captures"),
			Self:            "/usr/local/bin/ccsb-test",
			ClaudeSkillsDir: filepath.Join(dir, "claude-skills"),
		},
	}
}

func (e *env) writeSettings(t *testing.T, body string) {
	t.Helper()
	if err := os.WriteFile(e.paths.Settings, []byte(body), 0o600); err != nil {
		t.Fatalf("writeSettings: %v", err)
	}
}

func (e *env) loadSettings(t *testing.T) claudesettings.Settings {
	t.Helper()
	s, err := claudesettings.Load(e.paths.Settings)
	if err != nil {
		t.Fatalf("loadSettings: %v", err)
	}
	return s
}

func (e *env) loadConfig(t *testing.T) config.Config {
	t.Helper()
	c, err := config.Load(e.paths.Config)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	return c
}

func (e *env) saveConfig(t *testing.T, c config.Config) {
	t.Helper()
	if err := config.Save(e.paths.Config, c); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}
}

// Default (no-args) path: behaves like the proxy/fallback statusline.

func TestRun_NoArgsRunsFallbackWhenNoProxyConfigured(t *testing.T) {
	e := newEnv(t)
	in := strings.NewReader(`{"model":{"display_name":"Opus"},"workspace":{"current_dir":"/x"}}`)
	var out, errOut bytes.Buffer

	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{}, in, &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := strings.TrimRight(out.String(), "\n"); !strings.Contains(got, "Opus") {
		t.Errorf("expected fallback render, got %q", got)
	}
}

func TestRun_NoArgsUsesConfiguredProxy(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat not available")
	}
	e := newEnv(t)
	e.saveConfig(t, config.Config{Proxy: config.Proxy{Command: "cat"}})

	body := `{"model":{"display_name":"Opus"}}`
	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{}, strings.NewReader(body), &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.String() != body {
		t.Errorf("expected proxy passthrough, got %q", out.String())
	}
}

// Install.

func TestInstall_CanonicalCcstatuslineDefaultsToNative(t *testing.T) {
	e := newEnv(t)
	e.writeSettings(t, `{
		"statusLine": {"type":"command","command":"npx -y ccstatusline@latest"},
		"theme": "dark"
	}`)

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"install"}, nil, &out, &errOut); err != nil {
		t.Fatalf("install: %v", err)
	}

	// Settings now points to ccsb.
	s := e.loadSettings(t)
	sl, ok := claudesettings.GetStatusLine(s)
	if !ok {
		t.Fatal("statusLine missing after install")
	}
	var slObj struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	if err := json.Unmarshal(sl, &slObj); err != nil {
		t.Fatalf("statusLine: %v", err)
	}
	if slObj.Type != "command" || slObj.Command != e.paths.Self {
		t.Errorf("statusLine: got %+v, want command=%s", slObj, e.paths.Self)
	}

	// Other top-level keys preserved.
	if _, ok := s["theme"]; !ok {
		t.Error("theme key was lost across install")
	}

	// Backup is preserved so uninstall can restore the canonical line.
	c := e.loadConfig(t)
	if len(c.Backup.PreviousStatusLine) == 0 {
		t.Error("backup.previous_status_line is empty")
	}

	// Canonical ccstatusline is detected: install defaults to native mode
	// instead of seeding the proxy block from the previous command.
	if c.Proxy.Command != "" {
		t.Errorf("proxy.command should be empty for canonical ccstatusline, got %q", c.Proxy.Command)
	}
	if c.Proxy.Args != nil {
		t.Errorf("proxy.args should be nil for canonical ccstatusline, got %#v", c.Proxy.Args)
	}
}

func TestInstall_CanonicalCcstatuslineWithExtraFieldsDetected(t *testing.T) {
	e := newEnv(t)
	// Real-world shape with the `padding` field Claude Code sometimes adds.
	e.writeSettings(t, `{
		"statusLine": {"type":"command","command":"npx -y ccstatusline@latest","padding":0}
	}`)

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"install"}, nil, &out, &errOut); err != nil {
		t.Fatalf("install: %v", err)
	}

	c := e.loadConfig(t)
	if c.Proxy.Command != "" {
		t.Errorf("proxy.command should be empty, got %q", c.Proxy.Command)
	}
	if c.Proxy.Args != nil {
		t.Errorf("proxy.args should be nil, got %#v", c.Proxy.Args)
	}
	if len(c.Backup.PreviousStatusLine) == 0 {
		t.Error("backup.previous_status_line should still be saved")
	}
}

func TestInstall_NonCanonicalCommandSeedsProxy(t *testing.T) {
	e := newEnv(t)
	// Different command (no -y flag) — not the canonical default, so install
	// must still seed the proxy block from it so the user keeps getting
	// the same external rendering they had before install.
	e.writeSettings(t, `{
		"statusLine": {"type":"command","command":"npx ccstatusline"}
	}`)

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"install"}, nil, &out, &errOut); err != nil {
		t.Fatalf("install: %v", err)
	}

	c := e.loadConfig(t)
	if c.Proxy.Command != "npx" {
		t.Errorf("proxy.command: got %q, want npx", c.Proxy.Command)
	}
	if !reflect.DeepEqual(c.Proxy.Args, []string{"ccstatusline"}) {
		t.Errorf("proxy.args: got %#v, want [ccstatusline]", c.Proxy.Args)
	}
}

func TestInstall_NoExistingStatusLineIsAccepted(t *testing.T) {
	e := newEnv(t)
	e.writeSettings(t, `{"theme":"dark"}`)

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"install"}, nil, &out, &errOut); err != nil {
		t.Fatalf("install: %v", err)
	}

	s := e.loadSettings(t)
	if _, ok := claudesettings.GetStatusLine(s); !ok {
		t.Error("statusLine should be set after install")
	}
	c := e.loadConfig(t)
	if len(c.Backup.PreviousStatusLine) != 0 {
		t.Errorf("backup.previous_status_line should be empty for absent prior, got %s", c.Backup.PreviousStatusLine)
	}
}

func TestInstall_WhenSettingsFileAbsentCreatesIt(t *testing.T) {
	e := newEnv(t)
	// no settings.json at all

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"install"}, nil, &out, &errOut); err != nil {
		t.Fatalf("install: %v", err)
	}
	s := e.loadSettings(t)
	if _, ok := claudesettings.GetStatusLine(s); !ok {
		t.Error("statusLine should be set even when settings.json was absent")
	}
}

func TestInstall_IsIdempotentWhenAlreadyHookedAndDoesNotOverwriteBackup(t *testing.T) {
	e := newEnv(t)
	e.writeSettings(t, `{
		"statusLine": {"type":"command","command":"some original"}
	}`)

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"install"}, nil, &out, &errOut); err != nil {
		t.Fatalf("install (1): %v", err)
	}
	first := e.loadConfig(t).Backup.PreviousStatusLine

	out.Reset()
	errOut.Reset()
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"install"}, nil, &out, &errOut); err != nil {
		t.Fatalf("install (2): %v", err)
	}
	second := e.loadConfig(t).Backup.PreviousStatusLine

	if !bytes.Equal(first, second) {
		t.Errorf("backup must not be overwritten on second install:\n first=%s\nsecond=%s", first, second)
	}
}

// Uninstall.

func TestUninstall_RestoresPreviousStatusLine(t *testing.T) {
	e := newEnv(t)
	e.writeSettings(t, `{
		"statusLine": {"type":"command","command":"old original"}
	}`)
	var buf bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"install"}, nil, &buf, &buf); err != nil {
		t.Fatalf("install: %v", err)
	}

	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"uninstall"}, nil, &buf, &buf); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	s := e.loadSettings(t)
	sl, ok := claudesettings.GetStatusLine(s)
	if !ok {
		t.Fatal("statusLine missing after uninstall")
	}
	var slObj struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	if err := json.Unmarshal(sl, &slObj); err != nil {
		t.Fatalf("statusLine: %v", err)
	}
	if slObj.Command != "old original" {
		t.Errorf("expected restored command, got %q", slObj.Command)
	}

	// Backup should be cleared.
	c := e.loadConfig(t)
	if len(c.Backup.PreviousStatusLine) != 0 {
		t.Errorf("backup should be cleared on uninstall, got %s", c.Backup.PreviousStatusLine)
	}
}

func TestUninstall_RemovesStatusLineWhenNoPreviousExisted(t *testing.T) {
	e := newEnv(t)
	e.writeSettings(t, `{"theme":"dark"}`)
	var buf bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"install"}, nil, &buf, &buf); err != nil {
		t.Fatalf("install: %v", err)
	}

	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"uninstall"}, nil, &buf, &buf); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	s := e.loadSettings(t)
	if _, ok := claudesettings.GetStatusLine(s); ok {
		t.Error("statusLine should be removed if there was no prior")
	}
	if _, ok := s["theme"]; !ok {
		t.Error("unrelated keys must be preserved")
	}
}

func TestUninstall_WithoutPriorInstallReturnsError(t *testing.T) {
	e := newEnv(t)
	e.writeSettings(t, `{"theme":"dark"}`)

	var out, errOut bytes.Buffer
	err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"uninstall"}, nil, &out, &errOut)
	if err == nil {
		t.Error("expected error when uninstalling without prior install")
	}
}

// Status.

func TestStatus_ReportsHookedAfterInstall(t *testing.T) {
	e := newEnv(t)
	e.writeSettings(t, `{}`)
	var buf bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"install"}, nil, &buf, &buf); err != nil {
		t.Fatalf("install: %v", err)
	}

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"status"}, nil, &out, &errOut); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out.String(), "hooked: yes") {
		t.Errorf("expected 'hooked: yes' in status output, got:\n%s", out.String())
	}
}

func TestStatus_ReportsNotHookedWhenSettingsPointElsewhere(t *testing.T) {
	e := newEnv(t)
	e.writeSettings(t, `{"statusLine":{"type":"command","command":"/elsewhere"}}`)

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"status"}, nil, &out, &errOut); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out.String(), "hooked: no") {
		t.Errorf("expected 'hooked: no', got:\n%s", out.String())
	}
}

// Help & unknown.

func TestRun_UnknownSubcommandReturnsError(t *testing.T) {
	e := newEnv(t)
	var out, errOut bytes.Buffer
	err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"frobnicate"}, nil, &out, &errOut)
	if err == nil {
		t.Error("expected error for unknown subcommand")
	}
	if !strings.Contains(err.Error(), "frobnicate") {
		t.Errorf("error should reference the bad subcommand, got: %v", err)
	}
}

func TestRun_HelpFlagsPrintUsage(t *testing.T) {
	e := newEnv(t)
	for _, flag := range []string{"-h", "--help", "help"} {
		var out, errOut bytes.Buffer
		err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{flag}, nil, &out, &errOut)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", flag, err)
		}
		if !strings.Contains(out.String(), "ccsb") {
			t.Errorf("%s: usage output should mention ccsb, got:\n%s", flag, out.String())
		}
	}
}

// Path resolution.

func TestResolvePaths_PrefersXDGOverHomeDefaults(t *testing.T) {
	got := cli.ResolvePaths(cli.Env{
		Home:          "/home/u",
		XDGConfigHome: "/etc/xdg",
		XDGStateHome:  "/var/state",
		Self:          "/usr/local/bin/ccsb",
	})
	wantConfig := filepath.Join("/etc/xdg", "ccsb", "config.json")
	wantCapture := filepath.Join("/var/state", "ccsb", "captures")
	wantSettings := filepath.Join("/home/u", ".claude", "settings.json")
	if got.Config != wantConfig {
		t.Errorf("Config: got %q, want %q", got.Config, wantConfig)
	}
	if got.Capture != wantCapture {
		t.Errorf("Capture: got %q, want %q", got.Capture, wantCapture)
	}
	if got.Settings != wantSettings {
		t.Errorf("Settings: got %q, want %q", got.Settings, wantSettings)
	}
	if got.Self != "/usr/local/bin/ccsb" {
		t.Errorf("Self: got %q", got.Self)
	}
}

func TestNewFromOS_ResolvesXDGAndNoColor(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	t.Setenv("XDG_CONFIG_HOME", "/etc/xdg")
	t.Setenv("XDG_STATE_HOME", "/var/state")
	t.Setenv("NO_COLOR", "1")

	paths, flags, err := cli.NewFromOS()
	if err != nil {
		t.Fatalf("NewFromOS: %v", err)
	}
	if paths.Config != filepath.Join("/etc/xdg", "ccsb", "config.json") {
		t.Errorf("Config: got %q", paths.Config)
	}
	if paths.Capture != filepath.Join("/var/state", "ccsb", "captures") {
		t.Errorf("Capture: got %q", paths.Capture)
	}
	if paths.Settings != filepath.Join("/home/u", ".claude", "settings.json") {
		t.Errorf("Settings: got %q", paths.Settings)
	}
	if paths.Self == "" {
		t.Errorf("Self: empty (os.Executable should resolve in tests)")
	}
	if !flags.NoColor {
		t.Errorf("Flags.NoColor: want true (NO_COLOR=1)")
	}
}

func TestNewFromOS_NoColorUnset(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("NO_COLOR", "")

	_, flags, err := cli.NewFromOS()
	if err != nil {
		t.Fatalf("NewFromOS: %v", err)
	}
	if flags.NoColor {
		t.Errorf("Flags.NoColor: want false when NO_COLOR is unset/empty")
	}
}

func TestResolvePaths_ClaudeSkillsDir(t *testing.T) {
	got := cli.ResolvePaths(cli.Env{Home: "/home/u"})
	want := "/home/u/.claude/skills"
	if got.ClaudeSkillsDir != want {
		t.Errorf("ClaudeSkillsDir: got %q, want %q", got.ClaudeSkillsDir, want)
	}
}

// Sanity: errors.As works with the unknown-subcommand error to keep callers
// from depending on string matching forever.
func TestRun_UnknownSubcommandErrorIsTyped(t *testing.T) {
	e := newEnv(t)
	err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"nope"}, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("want error")
	}
	var ue *cli.UnknownSubcommandError
	if !errors.As(err, &ue) {
		t.Errorf("expected *cli.UnknownSubcommandError, got %T", err)
	}
	if ue != nil && ue.Name != "nope" {
		t.Errorf("Name: got %q, want %q", ue.Name, "nope")
	}
}

// Version subcommand.

func TestRun_VersionPrintsVersion(t *testing.T) {
	e := newEnv(t)
	prev := cli.Version
	cli.Version = "1.2.3-test"
	t.Cleanup(func() { cli.Version = prev })

	for _, arg := range []string{"version", "-v", "--version"} {
		var out, errOut bytes.Buffer
		if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{arg}, nil, &out, &errOut); err != nil {
			t.Fatalf("Run(%s): %v", arg, err)
		}
		if got := strings.TrimRight(out.String(), "\n"); got != "ccsb version 1.2.3-test" {
			t.Errorf("Run(%s): got %q", arg, got)
		}
	}
}

func TestRun_PassesNoColorThroughToStatusline(t *testing.T) {
	e := newEnv(t)
	e.saveConfig(t, config.Config{
		Render: render.Config{
			Rows: []render.Row{{Segments: []render.Segment{{Type: "model", FG: "131"}}}},
		},
	})
	body := `{"model":{"display_name":"Opus"}}`
	var out, errOut bytes.Buffer
	err := cli.Run(context.Background(), e.paths, cli.Flags{NoColor: true}, []string{},
		strings.NewReader(body), &out, &errOut)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Errorf("NoColor should suppress ANSI, got %q", out.String())
	}
}

// Install: no proxy when previous statusLine was another ccsb binary.

func TestInstall_PreviousCcsbBinaryDefaultsToNative(t *testing.T) {
	e := newEnv(t)
	// Previous statusLine points to a different path but same binary name as self.
	e.writeSettings(t, `{"statusLine":{"type":"command","command":"/old/path/ccsb-test"}}`)

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"install"}, nil, &out, &errOut); err != nil {
		t.Fatalf("install: %v", err)
	}

	c := e.loadConfig(t)
	if c.Proxy.Command != "" {
		t.Errorf("proxy should be empty (native mode) when previous was ccsb, got %q", c.Proxy.Command)
	}
	// Backup must still be preserved for uninstall.
	if len(c.Backup.PreviousStatusLine) == 0 {
		t.Error("backup should be preserved even when proxy is skipped")
	}
}

func TestInstall_NonCcsbPreviousStillSetsProxy(t *testing.T) {
	e := newEnv(t)
	e.writeSettings(t, `{"statusLine":{"type":"command","command":"/usr/local/bin/other-statusline"}}`)

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"install"}, nil, &out, &errOut); err != nil {
		t.Fatalf("install: %v", err)
	}

	c := e.loadConfig(t)
	if c.Proxy.Command != "/usr/local/bin/other-statusline" {
		t.Errorf("proxy should be set for non-ccsb previous, got %q", c.Proxy.Command)
	}
}

// Status: proxy warnings.

func TestStatus_WarnsWhenProxyPointsToSelf(t *testing.T) {
	e := newEnv(t)
	e.writeSettings(t, `{"statusLine":{"type":"command","command":"/usr/local/bin/ccsb-test"}}`)
	e.saveConfig(t, config.Config{Proxy: config.Proxy{Command: e.paths.Self}})

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"status"}, nil, &out, &errOut); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out.String(), "WARNING") {
		t.Errorf("expected WARNING in status output when proxy points to self, got:\n%s", out.String())
	}
}

func TestStatus_WarnsWhenProxyHasSameBasename(t *testing.T) {
	e := newEnv(t)
	e.writeSettings(t, `{"statusLine":{"type":"command","command":"/usr/local/bin/ccsb-test"}}`)
	e.saveConfig(t, config.Config{Proxy: config.Proxy{Command: "/another/path/ccsb-test"}})

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"status"}, nil, &out, &errOut); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out.String(), "WARNING") {
		t.Errorf("expected WARNING for same-basename proxy, got:\n%s", out.String())
	}
}

func TestStatus_WarnsWhenProxyNotFound(t *testing.T) {
	e := newEnv(t)
	e.writeSettings(t, `{"statusLine":{"type":"command","command":"/usr/local/bin/ccsb-test"}}`)
	e.saveConfig(t, config.Config{Proxy: config.Proxy{Command: "/nonexistent/path/other-tool"}})

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"status"}, nil, &out, &errOut); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out.String(), "WARNING") {
		t.Errorf("expected WARNING for missing proxy, got:\n%s", out.String())
	}
}

// Doctor subcommand.

func TestDoctor_NoIssuesFound(t *testing.T) {
	e := newEnv(t)
	e.writeSettings(t, `{"statusLine":{"type":"command","command":"/usr/local/bin/ccsb-test"}}`)

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"doctor"}, nil, &out, &errOut); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if !strings.Contains(out.String(), "no issues found") {
		t.Errorf("expected 'no issues found', got:\n%s", out.String())
	}
}

func TestDoctor_FixesProxyPointingToSelf(t *testing.T) {
	e := newEnv(t)
	e.writeSettings(t, `{"statusLine":{"type":"command","command":"/usr/local/bin/ccsb-test"}}`)
	e.saveConfig(t, config.Config{Proxy: config.Proxy{Command: e.paths.Self}})

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"doctor"}, nil, &out, &errOut); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if c := e.loadConfig(t); c.Proxy.Command != "" {
		t.Errorf("doctor should have cleared proxy, got %q", c.Proxy.Command)
	}
	if !strings.Contains(out.String(), "fixed 1 issue") {
		t.Errorf("expected 'fixed 1 issue', got:\n%s", out.String())
	}
}

func TestDoctor_FixesProxySameBasename(t *testing.T) {
	e := newEnv(t)
	e.writeSettings(t, `{"statusLine":{"type":"command","command":"/usr/local/bin/ccsb-test"}}`)
	e.saveConfig(t, config.Config{Proxy: config.Proxy{Command: "/other/path/ccsb-test"}})

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"doctor"}, nil, &out, &errOut); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if c := e.loadConfig(t); c.Proxy.Command != "" {
		t.Errorf("doctor should have cleared proxy, got %q", c.Proxy.Command)
	}
}

func TestDoctor_FixesProxyNotFound(t *testing.T) {
	e := newEnv(t)
	e.writeSettings(t, `{"statusLine":{"type":"command","command":"/usr/local/bin/ccsb-test"}}`)
	e.saveConfig(t, config.Config{Proxy: config.Proxy{Command: "/nonexistent/path/other-tool"}})

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"doctor"}, nil, &out, &errOut); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if c := e.loadConfig(t); c.Proxy.Command != "" {
		t.Errorf("doctor should have cleared proxy, got %q", c.Proxy.Command)
	}
}

func TestDoctor_InstallsWhenNotHooked(t *testing.T) {
	e := newEnv(t)
	// settings.json points to something else, not self.
	e.writeSettings(t, `{"statusLine":{"type":"command","command":"/some/other/tool"}}`)

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"doctor"}, nil, &out, &errOut); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	// After doctor, settings should point to self.
	s := e.loadSettings(t)
	sl, ok := claudesettings.GetStatusLine(s)
	if !ok {
		t.Fatal("statusLine missing after doctor install")
	}
	var obj struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(sl, &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if obj.Command != e.paths.Self {
		t.Errorf("after doctor: statusLine command = %q, want %q", obj.Command, e.paths.Self)
	}
}
