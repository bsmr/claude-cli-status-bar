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

// skillName is both the directory Claude Code discovers the skill under and
// the slash command it exposes (/ccsb-wizard).
const skillName = "ccsb-wizard"

// skillPaths returns the discoverable location and the pre-0.4.6 one.
//
// Claude Code loads personal skills from ~/.claude/skills/<name>/SKILL.md.
// Before 0.4.6 ccsb wrote a flat ccsb-wizard.md straight into the skills
// directory, which is not a skill at all — only .claude/commands/ accepts
// bare .md files — so /ccsb-wizard never resolved. Installs still clean the
// legacy path up so an upgrade does not strand a stale file.
func skillPaths(p Paths) (current, legacy string) {
	return filepath.Join(p.ClaudeSkillsDir, skillName, "SKILL.md"),
		filepath.Join(p.ClaudeSkillsDir, skillName+".md")
}

func runInstallSkill(p Paths, stdout io.Writer) error {
	dest, legacy := skillPaths(p)
	_, statErr := os.Stat(dest)
	exists := statErr == nil
	// WriteAtomic creates the parent directories (0o700) and lands the file
	// as 0o600 via temp+rename, matching every other persistent writer in
	// ccsb — a concurrent /ccsb-wizard load can never read a half-written
	// skill file.
	if err := fileutil.WriteAtomic(dest, wizardSkill); err != nil {
		return fmt.Errorf("ccsb: write skill file: %w", err)
	}
	if err := os.Remove(legacy); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("ccsb: remove legacy skill file: %w", err)
	}
	if !exists {
		fmt.Fprintf(stdout, "ccsb: installed %s\n", dest)
	} else {
		fmt.Fprintf(stdout, "ccsb: updated %s\n", dest)
	}
	return nil
}

func runUninstallSkill(p Paths, stdout io.Writer) error {
	dest, legacy := skillPaths(p)
	var removed bool

	// RemoveAll takes the whole ccsb-wizard/ directory: the skill owns it,
	// and leaving an empty directory behind would keep an entry in the
	// user's skills listing.
	if _, err := os.Stat(dest); err == nil {
		if err := os.RemoveAll(filepath.Dir(dest)); err != nil {
			return fmt.Errorf("ccsb: remove skill directory: %w", err)
		}
		removed = true
	}
	if err := os.Remove(legacy); err == nil {
		removed = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("ccsb: remove legacy skill file: %w", err)
	}

	if !removed {
		fmt.Fprintln(stdout, "ccsb: skill not installed")
		return nil
	}
	fmt.Fprintf(stdout, "ccsb: uninstalled %s\n", skillName)
	return nil
}
