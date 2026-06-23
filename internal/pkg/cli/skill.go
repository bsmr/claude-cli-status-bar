package cli

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed assets/ccsb-wizard.md
var wizardSkill []byte

// WizardSkillContent returns the embedded ccsb-wizard.md bytes.
// Exported for tests; production code uses wizardSkill directly.
func WizardSkillContent() []byte { return wizardSkill }

func runInstallSkill(p Paths, stdout io.Writer) error {
	if err := os.MkdirAll(p.ClaudeSkillsDir, 0o755); err != nil {
		return fmt.Errorf("ccsb: create skills dir: %w", err)
	}
	dest := filepath.Join(p.ClaudeSkillsDir, "ccsb-wizard.md")
	_, statErr := os.Stat(dest)
	exists := statErr == nil
	if err := os.WriteFile(dest, wizardSkill, 0o644); err != nil {
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
