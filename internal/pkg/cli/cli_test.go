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
	"time"

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

// A second install after settings.json was re-pointed elsewhere is NOT
// alreadyHooked, so the backup guard cannot key on that flag: the very
// first statusLine ccsb displaced must survive, otherwise uninstall
// restores an intermediate command the user never chose.
func TestInstall_KeepsOriginalBackupWhenSettingsWereRepointedExternally(t *testing.T) {
	e := newEnv(t)
	e.writeSettings(t, `{"statusLine":{"type":"command","command":"/usr/local/bin/original-statusline"}}`)

	var buf bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"install"}, nil, &buf, &buf); err != nil {
		t.Fatalf("install (1): %v", err)
	}
	first := e.loadConfig(t)
	if len(first.Backup.PreviousStatusLine) == 0 {
		t.Fatal("first install did not record a backup")
	}

	// Another tool (or the user) re-points statusLine at a third command.
	e.writeSettings(t, `{"statusLine":{"type":"command","command":"/usr/local/bin/third-tool"}}`)

	buf.Reset()
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"install"}, nil, &buf, &buf); err != nil {
		t.Fatalf("install (2): %v", err)
	}
	second := e.loadConfig(t)

	if !bytes.Equal(first.Backup.PreviousStatusLine, second.Backup.PreviousStatusLine) {
		t.Errorf("backup must survive a re-pointed settings.json:\n first=%s\nsecond=%s",
			first.Backup.PreviousStatusLine, second.Backup.PreviousStatusLine)
	}
	// The proxy block is derived only alongside the backup, so it must not
	// be re-seeded from the intermediate command either.
	if second.Proxy.Command != first.Proxy.Command {
		t.Errorf("proxy.command: got %q, want %q (unchanged)", second.Proxy.Command, first.Proxy.Command)
	}
}

// The backup is the only copy of the statusLine install is about to
// overwrite, so it must reach disk BEFORE settings.json is rewritten. With
// the writes in the other order a failing config.Save leaves settings.json
// already pointing at ccsb and the user's original command nowhere on disk —
// and the later uninstall then deletes the key instead of restoring it.
func TestInstall_LeavesSettingsUntouchedWhenBackupCannotBePersisted(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	e := newEnv(t)
	const original = `{"statusLine":{"type":"command","command":"npx -y my-precious-bar"},"theme":"dark"}`
	e.writeSettings(t, original)

	// Only the write may fail: the directory stays readable so config.Load
	// still succeeds (missing file => zero config) and install gets far
	// enough to attempt both writes.
	cfgDir := filepath.Join(filepath.Dir(e.paths.Config), "cfgdir")
	if err := os.Mkdir(cfgDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	e.paths.Config = filepath.Join(cfgDir, "ccsb-config.json")
	if err := os.Chmod(cfgDir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(cfgDir, 0o700) })

	var buf bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"install"}, nil, &buf, &buf); err == nil {
		t.Fatal("install must fail when the backup cannot be persisted")
	}

	got, err := os.ReadFile(e.paths.Settings)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if string(got) != original {
		t.Errorf("settings.json must be untouched when the backup write fails:\ngot:  %s\nwant: %s", got, original)
	}
}

// `ccsb config reset` restores the in-code defaults, but
// backup.previous_status_line is not a setting — it is the only copy of the
// statusLine ccsb displaced and owes the user back. Renaming the whole config
// file away takes that copy with it, so the later uninstall falls into the
// "no previous" branch and deletes statusLine instead of restoring it.
func TestConfigReset_PreservesUninstallBackup(t *testing.T) {
	e := newEnv(t)
	e.writeSettings(t, `{"statusLine":{"type":"command","command":"npx -y my-precious-bar"},"theme":"dark"}`)

	var buf bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"install"}, nil, &buf, &buf); err != nil {
		t.Fatalf("install: %v", err)
	}
	want := e.loadConfig(t).Backup.PreviousStatusLine
	if len(want) == 0 {
		t.Fatal("install recorded no backup")
	}

	// A genuine setting that reset MUST clear, so "just keep the old file"
	// cannot pass this test.
	cfg := e.loadConfig(t)
	cfg.Proxy.Command = "some-proxy"
	e.saveConfig(t, cfg)

	buf.Reset()
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"config", "reset"}, nil, &buf, &buf); err != nil {
		t.Fatalf("config reset: %v", err)
	}

	after := e.loadConfig(t)
	if after.Proxy.Command != "" {
		t.Errorf("config reset must clear settings: proxy.command = %q, want empty", after.Proxy.Command)
	}
	if !bytes.Equal(after.Backup.PreviousStatusLine, want) {
		t.Fatalf("backup must survive config reset:\ngot:  %s\nwant: %s", after.Backup.PreviousStatusLine, want)
	}

	buf.Reset()
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"uninstall"}, nil, &buf, &buf); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	got, ok := claudesettings.GetStatusLine(e.loadSettings(t))
	if !ok {
		t.Fatal("uninstall deleted statusLine instead of restoring the backup")
	}
	// Compared as JSON, not as bytes: config.Save marshals with
	// MarshalIndent, which re-indents an embedded json.RawMessage on every
	// write, so carrying the backup through the extra save/load cycle reset
	// now performs changes its whitespace. The value is what uninstall owes
	// the user; the surrounding indentation is not. (The separate claim that
	// this round-trip is "byte-for-byte" — config/config.go:7-9, README.md:63
	// — is inaccurate for that reason and for HTML-escaping of & < >; that is
	// a documentation fix of its own, not this one.)
	if !jsonEqual(t, got, want) {
		t.Errorf("restored statusLine: got %s, want %s", got, want)
	}
}

// jsonEqual reports whether a and b are the same JSON value, ignoring
// insignificant whitespace.
func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var ca, cb bytes.Buffer
	if err := json.Compact(&ca, a); err != nil {
		t.Fatalf("compact a: %v", err)
	}
	if err := json.Compact(&cb, b); err != nil {
		t.Fatalf("compact b: %v", err)
	}
	return bytes.Equal(ca.Bytes(), cb.Bytes())
}

// A config too broken to parse is precisely when reset is needed: a wrongly
// shaped render.palette, say, is a hard unmarshal error that blanks the whole
// bar, and moving the file aside is the documented way out. Reading the
// backup out first must therefore not turn a parse failure into a refusal —
// there is simply no backup to carry over in that case.
func TestConfigReset_WorksOnUnparsableConfig(t *testing.T) {
	e := newEnv(t)
	if err := os.WriteFile(e.paths.Config, []byte(`{"render":{"palette":{"bad":"shape"}}}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"config", "reset"}, nil, &buf, &buf); err != nil {
		t.Fatalf("config reset must succeed on an unparsable config: %v", err)
	}
	if _, err := os.Stat(e.paths.Config); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("config must have been moved aside (stat err: %v)", err)
	}
}

// Hidden refresh-git-dirty subcommand: the wiring the renderer's detached
// refresher invokes. Exercised without a real repository — nearestGitDir is
// pure filesystem walking, so every branch is reachable git-free.

// assertNoDirtyCache fails when refresh-git-dirty wrote a cache under state.
func assertNoDirtyCache(t *testing.T, state string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(state, "git-dirty")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected no git-dirty cache dir under %s (stat err: %v)", state, err)
	}
}

func TestRun_RefreshGitDirty_WithoutDirArgIsANoop(t *testing.T) {
	e := newEnv(t)
	e.paths.State = t.TempDir()

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"refresh-git-dirty"}, nil, &out, &errOut); err != nil {
		t.Fatalf("refresh-git-dirty: %v", err)
	}
	if out.Len() != 0 || errOut.Len() != 0 {
		t.Errorf("refresh-git-dirty must stay silent, got stdout=%q stderr=%q", out.String(), errOut.String())
	}
	assertNoDirtyCache(t, e.paths.State)
}

func TestRun_RefreshGitDirty_WithoutStateDirIsANoop(t *testing.T) {
	e := newEnv(t) // Paths.State is empty

	var out, errOut bytes.Buffer
	err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"refresh-git-dirty", t.TempDir()}, nil, &out, &errOut)
	if err != nil {
		t.Fatalf("refresh-git-dirty: %v", err)
	}
}

func TestRun_RefreshGitDirty_OutsideRepositoryWritesNothing(t *testing.T) {
	e := newEnv(t)
	e.paths.State = t.TempDir()

	var out, errOut bytes.Buffer
	err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"refresh-git-dirty", t.TempDir()}, nil, &out, &errOut)
	if err != nil {
		t.Fatalf("refresh-git-dirty: %v", err)
	}
	assertNoDirtyCache(t, e.paths.State)
}

// Hidden refresh-update-check subcommand: the wiring the renderer's
// detached refresher invokes to update the cached latest GitHub release.

func TestRun_RefreshUpdateCheck_WithoutStateDirIsANoop(t *testing.T) {
	e := newEnv(t) // Paths.State is empty

	var out, errOut bytes.Buffer
	err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"refresh-update-check"}, nil, &out, &errOut)
	if err != nil {
		t.Fatalf("refresh-update-check: %v", err)
	}
	if out.Len() != 0 || errOut.Len() != 0 {
		t.Errorf("refresh-update-check must stay silent, got stdout=%q stderr=%q", out.String(), errOut.String())
	}
}

// The "GitHub unreachable" branch — refresh-update-check must never
// surface a fetch failure as a CLI error (mirrors runRefreshGitDirty's
// silence-on-failure contract) — is exercised without any real network
// dependency by TestRefreshUpdateCheck_ReleasesLockEvenOnFailure in
// internal/pkg/render/updatecheck_test.go (against 127.0.0.1:1). A
// same-shape test here would either hit the real api.github.com on any
// networked machine, or — since the subcommand swallows all errors by
// design — assert nothing beyond what
// TestRun_RefreshUpdateCheck_WithoutStateDirIsANoop above already covers.

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

func TestRun_CapturesCleanIsDispatched(t *testing.T) {
	e := newEnv(t)
	if err := os.MkdirAll(e.paths.Capture, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	name := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano) + "-sess.json"
	if err := os.WriteFile(filepath.Join(e.paths.Capture, name), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"captures", "clean"}, nil, &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}

	entries, err := os.ReadDir(e.paths.Capture)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("capture not removed via dispatch: %v", entries)
	}
}

func TestRun_HelpMentionsCapturesVerb(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), cli.Paths{}, cli.Flags{}, []string{"help"}, nil, &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "captures") {
		t.Errorf("help should document the captures verb:\n%s", out.String())
	}
}
