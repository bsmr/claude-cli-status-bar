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
	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/render"
)

func TestWizardSkillContent_NotEmpty(t *testing.T) {
	if len(cli.WizardSkillContent()) == 0 {
		t.Fatal("embedded wizard skill is empty")
	}
}

// wizardTableTypes extracts the segment type names listed in the wizard's
// "Available segment types" table — the backtick-quoted first column of each
// row between the table header and the "Each segment accepts:" line.
func wizardTableTypes(t *testing.T, asset string) []string {
	t.Helper()
	start := strings.Index(asset, "Available segment types:")
	end := strings.Index(asset, "Each segment accepts:")
	if start < 0 || end <= start {
		t.Fatalf("wizard table markers not found (start=%d end=%d)", start, end)
	}
	var types []string
	for _, line := range strings.Split(asset[start:end], "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "| `") {
			continue // not a table data row
		}
		rest := line[len("| `"):]
		if i := strings.IndexByte(rest, '`'); i > 0 {
			types = append(types, rest[:i])
		}
	}
	return types
}

// TestWizardAsset_SegmentTypesMatchRegistry guards the embedded ccsb-wizard
// skill against segment-type drift: an LLM following the wizard writes configs
// against the names it lists, so every listed name must be a real registered
// segment, every real segment must be documented, and the historical phantom
// names that once produced broken configs must be gone.
func TestWizardAsset_SegmentTypesMatchRegistry(t *testing.T) {
	asset := string(cli.WizardSkillContent())

	real := make(map[string]bool)
	for _, s := range render.SegmentTypes() {
		real[s] = true
	}

	// (a) every type the wizard's table lists must be a registered segment.
	listed := wizardTableTypes(t, asset)
	if len(listed) == 0 {
		t.Fatal("no segment types parsed from the wizard table")
	}
	for _, ty := range listed {
		if !real[ty] {
			t.Errorf("wizard table lists %q, which is not a registered segment type", ty)
		}
	}

	// (b) every registered segment type must be documented, so it can be suggested.
	for ty := range real {
		if !strings.Contains(asset, "`"+ty+"`") {
			t.Errorf("registered segment type %q is not documented in the wizard", ty)
		}
	}

	// (c) the historical phantom names must appear nowhere — table or prose.
	for _, phantom := range []string{"context_window", "workspace", "session_id", "thinking", "lines_changed"} {
		if strings.Contains(asset, "`"+phantom+"`") {
			t.Errorf("wizard still references phantom segment type %q", phantom)
		}
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
