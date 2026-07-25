package cli

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/fileutil"
)

//go:embed assets/ccsb-wizard.md
var wizardSkill []byte

// WizardSkillContent returns the embedded ccsb-wizard.md bytes.
// Exported for tests; production code uses wizardSkill directly.
func WizardSkillContent() []byte { return wizardSkill }

func runInstallSkill(p Paths, stdout io.Writer) error {
	dest := filepath.Join(p.ClaudeSkillsDir, "ccsb-wizard.md")
	_, statErr := os.Stat(dest)
	exists := statErr == nil
	// WriteAtomic creates the skills dir (0o700) and lands the file as
	// 0o600 via temp+rename, matching every other persistent writer in
	// ccsb — a concurrent /ccsb-wizard load can never read a half-written
	// skill file.
	if err := fileutil.WriteAtomic(dest, wizardSkill); err != nil {
		return fmt.Errorf("ccsb: write skill file: %w", err)
	}
	if !exists {
		fmt.Fprintln(stdout, "ccsb: installed ccsb-wizard.md")
	} else {
		fmt.Fprintln(stdout, "ccsb: updated ccsb-wizard.md")
	}
	return nil
}

func runUninstallSkill(p Paths, stdout io.Writer) error {
	dest := filepath.Join(p.ClaudeSkillsDir, "ccsb-wizard.md")
	err := os.Remove(dest)
	if errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintln(stdout, "ccsb: skill not installed")
		return nil
	}
	if err != nil {
		return fmt.Errorf("ccsb: remove skill file: %w", err)
	}
	fmt.Fprintln(stdout, "ccsb: uninstalled ccsb-wizard.md")
	return nil
}
