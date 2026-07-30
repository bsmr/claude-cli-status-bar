package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/config"
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

// --- ccsb config auto (0.4.26) ---

// autoEnv writes cfg JSON to a temp config path and returns Paths pointing at
// it, plus a Self that is a real file so the self-update guard can be probed.
func autoEnv(t *testing.T, cfgJSON string) Paths {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	if cfgJSON != "" {
		if err := os.WriteFile(cfgPath, []byte(cfgJSON), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	self := filepath.Join(t.TempDir(), "ccsb")
	if err := os.WriteFile(self, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return Paths{Config: cfgPath, State: t.TempDir(), Self: self}
}

func TestConfigAuto_PrintsOffWhenUnset(t *testing.T) {
	// Symmetry with `ccsb mode`: what is printed can be typed back in, so the
	// empty state prints the word that clears it rather than "(none)".
	var out bytes.Buffer
	if err := runConfigAuto(autoEnv(t, ""), nil, &out); err != nil {
		t.Fatalf("runConfigAuto: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "off" {
		t.Errorf("got %q, want %q", got, "off")
	}
}

func TestConfigAuto_PrintsTheConfiguredLevel(t *testing.T) {
	var out bytes.Buffer
	if err := runConfigAuto(autoEnv(t, `{"update":{"auto":"minor"}}`), nil, &out); err != nil {
		t.Fatalf("runConfigAuto: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "minor" {
		t.Errorf("got %q, want %q", got, "minor")
	}
}

func TestConfigAuto_SetsEachValidLevel(t *testing.T) {
	for _, level := range []string{"patch", "minor", "major"} {
		t.Run(level, func(t *testing.T) {
			p := autoEnv(t, "")
			var out bytes.Buffer
			if err := runConfigAuto(p, []string{level}, &out); err != nil {
				t.Fatalf("runConfigAuto: %v", err)
			}
			cfg, err := config.Load(p.Config)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Update.Auto != level {
				t.Errorf("update.auto = %q, want %q", cfg.Update.Auto, level)
			}
		})
	}
}

func TestConfigAuto_OffRemovesTheWholeUpdateKey(t *testing.T) {
	// Not merely auto:"" — Update is tagged omitzero, so nulling the struct
	// must leave no empty {"update":{}} object behind. Same as `mode native`.
	p := autoEnv(t, `{"update":{"auto":"major"},"proxy":{"command":"keepme"}}`)
	var out bytes.Buffer
	if err := runConfigAuto(p, []string{"off"}, &out); err != nil {
		t.Fatalf("runConfigAuto: %v", err)
	}
	raw, err := os.ReadFile(p.Config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "update") {
		t.Errorf("the update key survived `off`:\n%s", raw)
	}
	// And an unrelated setting is untouched.
	if !strings.Contains(string(raw), "keepme") {
		t.Errorf("`off` disturbed the rest of the config:\n%s", raw)
	}
}

func TestConfigAuto_RejectsUnknownLevelAndLeavesConfigAlone(t *testing.T) {
	const original = `{"update":{"auto":"patch"}}`
	p := autoEnv(t, original)
	before, err := os.ReadFile(p.Config)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err = runConfigAuto(p, []string{"quarterly"}, &out)
	if err == nil {
		t.Fatal("expected an error for an unknown level")
	}
	if !strings.Contains(err.Error(), "quarterly") {
		t.Errorf("error should name the bad level: %v", err)
	}
	for _, valid := range []string{"patch", "minor", "major", "off"} {
		if !strings.Contains(err.Error(), valid) {
			t.Errorf("error should list %q as valid: %v", valid, err)
		}
	}
	after, err := os.ReadFile(p.Config)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("a rejected level rewrote the config:\nbefore: %s\nafter:  %s", before, after)
	}
}

func TestConfigAuto_RejectsExtraArguments(t *testing.T) {
	var out bytes.Buffer
	err := runConfigAuto(autoEnv(t, ""), []string{"patch", "please"}, &out)
	if err == nil {
		t.Fatal("expected an error for extra arguments")
	}
}

func TestConfigAuto_NotesThatSelfUpdateIsBlocked(t *testing.T) {
	// The user's own case: the setting is saved, but the running binary cannot
	// install anything, so say so at the moment of the decision rather than
	// leaving it for `ccsb doctor`.
	//
	// The blocking reason used here is an unwritable target directory, the
	// same lever doctor's own test pulls: "local build" depends on the VCS
	// stamp of whatever binary runs the test and so cannot be forced.
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	p := autoEnv(t, "")
	binDir := filepath.Dir(p.Self)
	if err := os.Chmod(binDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(binDir, 0o700) })

	var out bytes.Buffer
	if err := runConfigAuto(p, []string{"patch"}, &out); err != nil {
		t.Fatalf("runConfigAuto: %v", err)
	}
	if !strings.Contains(out.String(), "self-update") {
		t.Errorf("expected a self-update note for a non-release binary:\n%s", out.String())
	}
	// Saved regardless — the note is information, not a veto.
	cfg, err := config.Load(p.Config)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Update.Auto != "patch" {
		t.Errorf("the level was not saved: %q", cfg.Update.Auto)
	}
}

func TestConfigAuto_NotesAnInertLayout(t *testing.T) {
	// A rows block with no version segment setting check_update makes the
	// setting dead on arrival — 0.4.17's finding, reported here too.
	p := autoEnv(t, `{"render":{"rows":[{"segments":[{"type":"model"}]}]}}`)
	var out bytes.Buffer
	if err := runConfigAuto(p, []string{"patch"}, &out); err != nil {
		t.Fatalf("runConfigAuto: %v", err)
	}
	if !strings.Contains(out.String(), "check_update") {
		t.Errorf("expected a note about the inert layout:\n%s", out.String())
	}
}

func TestConfigAuto_OffSaysNothingAboutBlocking(t *testing.T) {
	// Switching a feature off needs no warning that it will not run.
	//
	// The environment is made ACTIVELY blocking (unwritable target, plus a
	// rows block with no check_update), because otherwise both notes stay
	// silent for the ordinary reason and this test cannot tell "off suppresses
	// the notes" from "there was nothing to say". Verified by mutation: with a
	// neutral environment, copying the notes into the off branch left this
	// test green.
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	p := autoEnv(t, `{"update":{"auto":"patch"},`+
		`"render":{"rows":[{"segments":[{"type":"model"}]}]}}`)
	binDir := filepath.Dir(p.Self)
	if err := os.Chmod(binDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(binDir, 0o700) })

	var out bytes.Buffer
	if err := runConfigAuto(p, []string{"off"}, &out); err != nil {
		t.Fatalf("runConfigAuto: %v", err)
	}
	if strings.Contains(out.String(), "self-update") || strings.Contains(out.String(), "check_update") {
		t.Errorf("`off` must not warn about a feature being disabled:\n%s", out.String())
	}
}

func TestConfigAuto_PreservesUnmodelledTopLevelKeys(t *testing.T) {
	// The 0.4.21 invariant: config.json is the user's file. A newer ccsb may
	// have written a block this binary does not model, and setting auto must
	// not delete it.
	p := autoEnv(t, `{"update":{"auto":"patch"},"future_block":{"kept":true}}`)
	var out bytes.Buffer
	if err := runConfigAuto(p, []string{"major"}, &out); err != nil {
		t.Fatalf("runConfigAuto: %v", err)
	}
	raw, err := os.ReadFile(p.Config)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "future_block") {
		t.Errorf("an unmodelled top-level key was dropped:\n%s", raw)
	}
}
