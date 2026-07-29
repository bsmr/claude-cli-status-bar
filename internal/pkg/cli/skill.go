package cli

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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

// skillDiagnostic returns a human-readable note when the installed wizard
// skill no longer matches this binary, or "" when there is nothing to say.
//
// install-skill writes a COPY of the embedded asset into the user's skills
// directory, so upgrading ccsb leaves that copy untouched. 0.4.13 corrected
// wizard guidance that could blank the status bar, and every installation made
// before it kept serving the dangerous text with nothing to indicate it — this
// is the check that surfaces exactly that.
//
// Reporting only, never repair: the file lives in the user's own
// ~/.claude/skills/ and they may have edited it deliberately. Overwriting it
// from a diagnostic would repeat the mistake 0.4.12 had to undo, where doctor
// silently destroyed a configuration it merely disagreed with. `ccsb
// install-skill` is one command away and says what it did.
//
// Not installed at all is not a problem: the skill is opt-in.
func skillDiagnostic(p Paths) string {
	if p.ClaudeSkillsDir == "" {
		return ""
	}
	current, legacy := skillPaths(p)

	var notes []string
	if got, err := os.ReadFile(current); err == nil && !bytes.Equal(got, wizardSkill) {
		// Deliberately not distinguishing "older" from "user-edited": ccsb
		// carries no version marker inside the asset, and both cases have the
		// same answer.
		notes = append(notes, "installed wizard skill differs from this binary's version")
	}
	if _, err := os.Stat(legacy); err == nil {
		// Pre-0.4.6 layout: a flat .md that Claude Code never loaded as a
		// skill. install-skill removes it.
		notes = append(notes, "a legacy "+skillName+".md is still present and is never loaded")
	}
	if len(notes) == 0 {
		return ""
	}
	return "skill: " + strings.Join(notes, "; ") + " — run ccsb install-skill to refresh it"
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
