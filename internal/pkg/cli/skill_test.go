package cli_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/cli"
)

func TestWizardSkillContent_NotEmpty(t *testing.T) {
	if len(cli.WizardSkillContent()) == 0 {
		t.Fatal("embedded wizard skill is empty")
	}
}

func TestRun_InstallSkill_CreatesFileAndDir(t *testing.T) {
	e := newEnv(t) // ClaudeSkillsDir does not exist yet
	var out bytes.Buffer
	err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"install-skill"}, nil, &out, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dest := filepath.Join(e.paths.ClaudeSkillsDir, "ccsb-wizard.md")
	got, readErr := os.ReadFile(dest)
	if readErr != nil {
		t.Fatalf("skill file not written: %v", readErr)
	}
	if len(got) == 0 {
		t.Fatal("written skill file is empty")
	}
	if !strings.Contains(out.String(), "installed") {
		t.Errorf("expected 'installed' in output, got %q", out.String())
	}
}

func TestRun_InstallSkill_OverwritesAndReportsUpdated(t *testing.T) {
	e := newEnv(t)
	if err := os.MkdirAll(e.paths.ClaudeSkillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(e.paths.ClaudeSkillsDir, "ccsb-wizard.md")
	if err := os.WriteFile(dest, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"install-skill"}, nil, &out, io.Discard); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "updated") {
		t.Errorf("expected 'updated' in output, got %q", out.String())
	}
	written, _ := os.ReadFile(dest)
	if string(written) == "old" {
		t.Fatal("file was not overwritten")
	}
}

func TestRun_UninstallSkill_RemovesFile(t *testing.T) {
	e := newEnv(t)
	if err := os.MkdirAll(e.paths.ClaudeSkillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(e.paths.ClaudeSkillsDir, "ccsb-wizard.md")
	if err := os.WriteFile(dest, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"uninstall-skill"}, nil, &out, io.Discard); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("skill file still exists after uninstall")
	}
	if !strings.Contains(out.String(), "uninstalled") {
		t.Errorf("expected 'uninstalled' in output, got %q", out.String())
	}
}

func TestRun_UninstallSkill_WhenNotInstalled(t *testing.T) {
	e := newEnv(t) // ClaudeSkillsDir does not exist
	var out bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"uninstall-skill"}, nil, &out, io.Discard); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "not installed") {
		t.Errorf("expected 'not installed' in output, got %q", out.String())
	}
}
