package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunConfig_NoSubcommandIsError(t *testing.T) {
	var out bytes.Buffer
	err := runConfig(Paths{Config: "/dev/null"}, nil, &out)
	if err == nil {
		t.Error("expected error for missing subcommand")
	}
	if !strings.Contains(err.Error(), "reset") {
		t.Errorf("error should hint at valid verb: %v", err)
	}
}

func TestRunConfig_UnknownSubcommandIsError(t *testing.T) {
	var out bytes.Buffer
	err := runConfig(Paths{Config: "/dev/null"}, []string{"frobnicate"}, &out)
	if err == nil {
		t.Error("expected error for unknown subcommand")
	}
	if !strings.Contains(err.Error(), "frobnicate") {
		t.Errorf("error should name the bad verb: %v", err)
	}
}

func TestRunConfigReset_NoExistingConfig_IsFriendlyNoop(t *testing.T) {
	dir := t.TempDir()
	p := Paths{Config: filepath.Join(dir, "config.json")}
	var out bytes.Buffer
	if err := runConfigReset(p, &out); err != nil {
		t.Fatalf("runConfigReset: %v", err)
	}
	if !strings.Contains(out.String(), "no config") {
		t.Errorf("output should mention 'no config', got %q", out.String())
	}
	// Confirm no backup file was created.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("no file should be created when config is absent; got %d", len(entries))
	}
}

func TestRunConfigReset_ExistingConfig_BackupCreatedOriginalGone(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	original := []byte(`{"render":{"powerline":true}}`)
	if err := os.WriteFile(cfgPath, original, 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	var out bytes.Buffer
	if err := runConfigReset(Paths{Config: cfgPath}, &out); err != nil {
		t.Fatalf("runConfigReset: %v", err)
	}

	// Original gone.
	if _, err := os.Stat(cfgPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("original config should be removed, stat err = %v", err)
	}

	// Exactly one backup file, prefix matches.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	var backups []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "config.json.bak.") {
			backups = append(backups, e.Name())
		}
	}
	if len(backups) != 1 {
		t.Fatalf("expected exactly 1 backup file, got %d: %v", len(backups), backups)
	}

	// Backup content matches original byte-for-byte.
	got, err := os.ReadFile(filepath.Join(dir, backups[0]))
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(got) != string(original) {
		t.Errorf("backup content mismatch:\n got: %q\nwant: %q", got, original)
	}

	// Stdout output mentions both the backup path and the reset confirmation.
	if !strings.Contains(out.String(), backups[0]) {
		t.Errorf("output should mention the backup path, got %q", out.String())
	}
	if !strings.Contains(out.String(), "reset") {
		t.Errorf("output should mention 'reset', got %q", out.String())
	}
}

func TestRunConfigReset_EmptyPathIsError(t *testing.T) {
	var out bytes.Buffer
	err := runConfigReset(Paths{Config: ""}, &out)
	if err == nil {
		t.Error("expected error for empty config path")
	}
}

func TestRunConfig_ResetWithExtraArgsIsError(t *testing.T) {
	var out bytes.Buffer
	err := runConfig(Paths{Config: "/dev/null"}, []string{"reset", "extra"}, &out)
	if err == nil {
		t.Error("expected error for extra args to reset")
	}
}
