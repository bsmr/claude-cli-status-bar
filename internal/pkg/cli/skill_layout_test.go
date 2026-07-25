package cli

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Claude Code discovers a personal skill at ~/.claude/skills/<name>/SKILL.md
// carrying YAML frontmatter. A flat .md in the skills dir is not a skill —
// only .claude/commands/ accepts those — so writing ccsb-wizard.md meant
// /ccsb-wizard never resolved and 0.3.0's headline feature was unreachable.
func TestRunInstallSkill_WritesADiscoverableSkill(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer

	if err := runInstallSkill(Paths{ClaudeSkillsDir: dir}, &out); err != nil {
		t.Fatalf("runInstallSkill: %v", err)
	}

	want := filepath.Join(dir, "ccsb-wizard", "SKILL.md")
	if _, err := os.Stat(want); err != nil {
		entries, _ := os.ReadDir(dir)
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected the skill at %s; skills dir holds %v", want, names)
	}
}

func TestWizardAsset_HasFrontmatterClaudeCodeCanRead(t *testing.T) {
	content := string(wizardSkill)
	if !strings.HasPrefix(content, "---\n") {
		t.Fatalf("asset must open with YAML frontmatter, got %.40q", content)
	}
	end := strings.Index(content[4:], "\n---\n")
	if end < 0 {
		t.Fatal("frontmatter block is not terminated by a --- line")
	}
	front := content[4 : 4+end]
	if !strings.Contains(front, "name: ccsb-wizard") {
		t.Errorf("skill name must be ccsb-wizard so /ccsb-wizard resolves:\n%s", front)
	}
	if !strings.Contains(front, "description:") {
		t.Errorf("description drives when Claude offers the skill:\n%s", front)
	}
}

// An upgrade from a pre-0.4.6 install must not leave the old flat file behind:
// it would be dead weight the user has to find and delete themselves.
func TestRunInstallSkill_RemovesTheLegacyFlatFile(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "ccsb-wizard.md")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(legacy, []byte("old"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var out bytes.Buffer
	if err := runInstallSkill(Paths{ClaudeSkillsDir: dir}, &out); err != nil {
		t.Fatalf("runInstallSkill: %v", err)
	}
	if _, err := os.Stat(legacy); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("legacy flat file survived the upgrade: %v", err)
	}
}

func TestRunUninstallSkill_RemovesBothLayouts(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "ccsb-wizard.md")
	if err := os.MkdirAll(filepath.Join(dir, "ccsb-wizard"), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ccsb-wizard", "SKILL.md"), []byte("new"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(legacy, []byte("old"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var out bytes.Buffer
	if err := runUninstallSkill(Paths{ClaudeSkillsDir: dir}, &out); err != nil {
		t.Fatalf("runUninstallSkill: %v", err)
	}
	for _, path := range []string{legacy, filepath.Join(dir, "ccsb-wizard")} {
		if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s survived uninstall: %v", path, err)
		}
	}
}

// The wizard tells an LLM which config keys to write. A key that does not
// exist is silently dropped on the next Load→Save cycle at best, and takes
// the whole bar down at worst: render.palette given as an object instead of
// an array is a hard unmarshal error, so ccsb exits 1 and Claude Code shows
// nothing until the user repairs the file by hand.
func TestWizardAsset_MentionsNoPhantomConfigKeys(t *testing.T) {
	content := string(wizardSkill)
	for _, phantom := range []string{
		"symbols.nerd_font",
		"nerd_font",
		"named palette entry",
		"per-segment separator override",
	} {
		if strings.Contains(content, phantom) {
			t.Errorf("asset still references %q, which config.Config does not have", phantom)
		}
	}
}

// The wizard predates 0.4.0 and could not offer anything that release added.
func TestWizardAsset_DocumentsThePostV040SegmentFields(t *testing.T) {
	content := string(wizardSkill)
	for _, field := range []string{
		"bar_glyphs", "bar_style", "token_position",
		"bar_fg", "label_fg", "git_dirty",
	} {
		if !strings.Contains(content, field) {
			t.Errorf("asset never mentions %q, so the wizard cannot configure it", field)
		}
	}
}

// Overwriting config.json wholesale destroys proxy and backup.
// backup.previous_status_line is what `ccsb uninstall` restores from, so
// losing it strands the user's original statusLine.
func TestWizardAsset_PreservesProxyAndBackupOnWrite(t *testing.T) {
	content := string(wizardSkill)
	if !strings.Contains(content, "backup") || !strings.Contains(content, "proxy") {
		t.Fatal("asset must tell the LLM to preserve the proxy and backup blocks")
	}
	if strings.Contains(content, "cat > \"${XDG_CONFIG_HOME:-$HOME/.config}/ccsb/config.json\"") {
		t.Error("asset still instructs a whole-file overwrite, which drops proxy and backup")
	}
}
