package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/cli"
	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/config"
)

func TestRun_mode_read_native(t *testing.T) {
	e := newEnv(t)
	// no config written -> Proxy.Command is empty -> mode is native

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"mode"}, nil, &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.String() != "native\n" {
		t.Errorf("stdout: got %q, want %q", out.String(), "native\n")
	}
}

func TestRun_mode_read_proxy(t *testing.T) {
	e := newEnv(t)
	e.saveConfig(t, config.Config{Proxy: config.Proxy{
		Command: "npx",
		Args:    []string{"-y", "ccstatusline@latest"},
	}})

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"mode"}, nil, &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.String() != "proxy\n" {
		t.Errorf("stdout: got %q, want %q", out.String(), "proxy\n")
	}
}

func TestRun_mode_native_clearsProxy(t *testing.T) {
	e := newEnv(t)
	e.saveConfig(t, config.Config{
		Proxy: config.Proxy{Command: "npx", Args: []string{"-y", "ccstatusline@latest"}},
		Backup: config.Backup{
			PreviousStatusLine: json.RawMessage(`{"type":"command","command":"old"}`),
		},
	})

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"mode", "native"}, nil, &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}

	c := e.loadConfig(t)
	if c.Proxy.Command != "" {
		t.Errorf("Proxy.Command: got %q, want empty", c.Proxy.Command)
	}
	if c.Proxy.Args != nil {
		t.Errorf("Proxy.Args: got %#v, want nil", c.Proxy.Args)
	}

	// Backup must be preserved across a mode flip. Compare compacted JSON to
	// tolerate MarshalIndent re-formatting by config.Save.
	var gotBuf, wantBuf bytes.Buffer
	if err := json.Compact(&gotBuf, c.Backup.PreviousStatusLine); err != nil {
		t.Fatalf("compact got: %v", err)
	}
	if err := json.Compact(&wantBuf, json.RawMessage(`{"type":"command","command":"old"}`)); err != nil {
		t.Fatalf("compact want: %v", err)
	}
	if gotBuf.String() != wantBuf.String() {
		t.Errorf("Backup.PreviousStatusLine clobbered: got %s", c.Backup.PreviousStatusLine)
	}

	// Serialised JSON must not carry an empty "proxy" key.
	raw, err := os.ReadFile(e.paths.Config)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(raw), `"proxy"`) {
		t.Errorf("config still mentions \"proxy\":\n%s", raw)
	}
}

func TestRun_mode_native_idempotent(t *testing.T) {
	e := newEnv(t)

	var out1, errOut1 bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"mode", "native"}, nil, &out1, &errOut1); err != nil {
		t.Fatalf("Run (1): %v", err)
	}
	first, err := os.ReadFile(e.paths.Config)
	if err != nil {
		t.Fatalf("read config (1): %v", err)
	}

	var out2, errOut2 bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"mode", "native"}, nil, &out2, &errOut2); err != nil {
		t.Fatalf("Run (2): %v", err)
	}
	second, err := os.ReadFile(e.paths.Config)
	if err != nil {
		t.Fatalf("read config (2): %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Errorf("mode native is not byte-identical on re-run:\n first=%s\nsecond=%s", first, second)
	}
}

func TestRun_mode_proxy_defaults(t *testing.T) {
	e := newEnv(t)

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"mode", "proxy"}, nil, &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}

	c := e.loadConfig(t)
	if c.Proxy.Command != "npx" {
		t.Errorf("Proxy.Command: got %q, want %q", c.Proxy.Command, "npx")
	}
	want := []string{"-y", "ccstatusline@latest"}
	if !reflect.DeepEqual(c.Proxy.Args, want) {
		t.Errorf("Proxy.Args: got %#v, want %#v", c.Proxy.Args, want)
	}
}

func TestRun_mode_proxy_explicit(t *testing.T) {
	e := newEnv(t)

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"mode", "proxy", "/usr/bin/foo", "--x", "y"}, nil, &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}

	c := e.loadConfig(t)
	if c.Proxy.Command != "/usr/bin/foo" {
		t.Errorf("Proxy.Command: got %q", c.Proxy.Command)
	}
	want := []string{"--x", "y"}
	if !reflect.DeepEqual(c.Proxy.Args, want) {
		t.Errorf("Proxy.Args: got %#v, want %#v", c.Proxy.Args, want)
	}
}

func TestRun_mode_proxy_overwrite(t *testing.T) {
	e := newEnv(t)
	e.saveConfig(t, config.Config{Proxy: config.Proxy{
		Command: "/old/cmd",
		Args:    []string{"--legacy", "--flag"},
	}})

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"mode", "proxy", "/usr/bin/bar"}, nil, &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}

	c := e.loadConfig(t)
	if c.Proxy.Command != "/usr/bin/bar" {
		t.Errorf("Proxy.Command: got %q, want %q", c.Proxy.Command, "/usr/bin/bar")
	}
	if c.Proxy.Args != nil {
		t.Errorf("Proxy.Args should be nil after overwrite without trailing args, got %#v", c.Proxy.Args)
	}
}

func TestRun_mode_proxy_preservesBackup(t *testing.T) {
	e := newEnv(t)
	e.saveConfig(t, config.Config{
		Backup: config.Backup{
			PreviousStatusLine: json.RawMessage(`{"type":"command","command":"old"}`),
		},
	})

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"mode", "proxy"}, nil, &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}

	c := e.loadConfig(t)
	// Compare compacted JSON to tolerate MarshalIndent re-formatting by config.Save.
	var gotBuf, wantBuf bytes.Buffer
	if err := json.Compact(&gotBuf, c.Backup.PreviousStatusLine); err != nil {
		t.Fatalf("compact got: %v", err)
	}
	if err := json.Compact(&wantBuf, json.RawMessage(`{"type":"command","command":"old"}`)); err != nil {
		t.Fatalf("compact want: %v", err)
	}
	if gotBuf.String() != wantBuf.String() {
		t.Errorf("Backup.PreviousStatusLine clobbered: got %s", c.Backup.PreviousStatusLine)
	}
}

func TestRun_mode_unknown_target(t *testing.T) {
	e := newEnv(t)

	var out, errOut bytes.Buffer
	err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"mode", "unicorn"}, nil, &out, &errOut)
	if err == nil {
		t.Fatal("expected error for unknown mode")
	}
	if !strings.Contains(err.Error(), "invalid mode") || !strings.Contains(err.Error(), "unicorn") {
		t.Errorf("error should name the invalid mode, got: %v", err)
	}

	// Config must not exist after a rejected mode call.
	if _, statErr := os.Stat(e.paths.Config); !os.IsNotExist(statErr) {
		t.Errorf("config.json must not be created on error, got stat err=%v", statErr)
	}
}

func TestRun_mode_native_rejectsExtraArgs(t *testing.T) {
	e := newEnv(t)

	var out, errOut bytes.Buffer
	err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"mode", "native", "extra"}, nil, &out, &errOut)
	if err == nil {
		t.Fatal("expected error for trailing args after native")
	}
	if !strings.Contains(err.Error(), "native") || !strings.Contains(err.Error(), "arguments") {
		t.Errorf("error should describe the extra-args rejection, got: %v", err)
	}

	if _, statErr := os.Stat(e.paths.Config); !os.IsNotExist(statErr) {
		t.Errorf("config.json must not be created on error, got stat err=%v", statErr)
	}
}

func TestRun_status_modeLine_native(t *testing.T) {
	e := newEnv(t)
	e.writeSettings(t, `{}`)

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"status"}, nil, &out, &errOut); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out.String(), "mode:     native") {
		t.Errorf("status should report native mode, got:\n%s", out.String())
	}
}

func TestRun_status_modeLine_proxy(t *testing.T) {
	e := newEnv(t)
	e.writeSettings(t, `{}`)
	e.saveConfig(t, config.Config{Proxy: config.Proxy{
		Command: "npx",
		Args:    []string{"-y", "ccstatusline@latest"},
	}})

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"status"}, nil, &out, &errOut); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out.String(), "mode:     proxy") {
		t.Errorf("status should report proxy mode, got:\n%s", out.String())
	}
}

func TestRun_status_modeLine_ordering(t *testing.T) {
	e := newEnv(t)
	e.writeSettings(t, `{}`)

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"status"}, nil, &out, &errOut); err != nil {
		t.Fatalf("status: %v", err)
	}

	got := out.String()
	captureIdx := strings.Index(got, "capture:")
	modeIdx := strings.Index(got, "mode:")
	proxyIdx := strings.Index(got, "proxy:")
	backupIdx := strings.Index(got, "backup:")
	if captureIdx < 0 || modeIdx < 0 || proxyIdx < 0 || backupIdx < 0 {
		t.Fatalf("status output missing one of capture/mode/proxy/backup:\n%s", got)
	}
	if !(captureIdx < modeIdx && modeIdx < proxyIdx && proxyIdx < backupIdx) {
		t.Errorf("status lines must appear in order capture -> mode -> proxy -> backup, got order indices c=%d m=%d p=%d b=%d in:\n%s",
			captureIdx, modeIdx, proxyIdx, backupIdx, got)
	}
}

func TestRun_help_listsMode(t *testing.T) {
	e := newEnv(t)
	for _, flag := range []string{"-h", "--help", "help"} {
		var out, errOut bytes.Buffer
		if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{flag}, nil, &out, &errOut); err != nil {
			t.Fatalf("%s: %v", flag, err)
		}
		if !strings.Contains(out.String(), "mode") {
			t.Errorf("%s: help should mention the mode subcommand, got:\n%s", flag, out.String())
		}
	}
}

func TestRun_unknownSubcommand_listsMode(t *testing.T) {
	e := newEnv(t)
	var out, errOut bytes.Buffer
	err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"frobnicate"}, nil, &out, &errOut)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "mode") {
		t.Errorf("unknown-subcommand error should list 'mode' as valid, got: %v", err)
	}
}
