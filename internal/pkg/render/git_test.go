package render

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBranch_RegularRepoOnMain(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, ".git"))
	mustWriteFile(t, filepath.Join(dir, ".git", "HEAD"), "ref: refs/heads/main\n")

	if got := branch(dir); got != "main" {
		t.Errorf("got %q, want main", got)
	}
}

func TestBranch_FromSubdirectoryWalksUp(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, ".git"))
	mustWriteFile(t, filepath.Join(dir, ".git", "HEAD"), "ref: refs/heads/feature-x\n")
	sub := filepath.Join(dir, "a", "b", "c")
	mustMkdir(t, sub)

	if got := branch(sub); got != "feature-x" {
		t.Errorf("from sub: got %q, want feature-x", got)
	}
}

func TestBranch_DetachedHeadReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, ".git"))
	mustWriteFile(t, filepath.Join(dir, ".git", "HEAD"), "0123456789abcdef0123456789abcdef01234567\n")

	if got := branch(dir); got != "" {
		t.Errorf("detached should be empty, got %q", got)
	}
}

func TestBranch_NotInRepoReturnsEmpty(t *testing.T) {
	if got := branch(t.TempDir()); got != "" {
		t.Errorf("non-repo should be empty, got %q", got)
	}
}

func TestBranch_EmptyStartReturnsEmpty(t *testing.T) {
	if got := branch(""); got != "" {
		t.Errorf("empty start should be empty, got %q", got)
	}
}

func TestBranch_WorktreeViaGitdirFile(t *testing.T) {
	root := t.TempDir()
	realGit := filepath.Join(root, "main-repo", ".git")
	mustMkdir(t, realGit)
	worktreeGit := filepath.Join(realGit, "worktrees", "wt1")
	mustMkdir(t, worktreeGit)
	mustWriteFile(t, filepath.Join(worktreeGit, "HEAD"), "ref: refs/heads/wt-branch\n")

	wtDir := filepath.Join(root, "wt1")
	mustMkdir(t, wtDir)
	mustWriteFile(t, filepath.Join(wtDir, ".git"), "gitdir: "+worktreeGit+"\n")

	if got := branch(wtDir); got != "wt-branch" {
		t.Errorf("worktree: got %q, want wt-branch", got)
	}
}

func TestBranch_StopsAtRoot(t *testing.T) {
	if got := branch("/"); got != "" {
		t.Errorf("root should be empty, got %q", got)
	}
}

func TestBranch_RejectsGitdirThatEscapesParent(t *testing.T) {
	root := t.TempDir()
	wt := filepath.Join(root, "wt")
	mustMkdir(t, wt)
	// .git points outside the wt directory via relative traversal.
	mustWriteFile(t, filepath.Join(wt, ".git"), "gitdir: ../../../etc\n")

	if got := branch(wt); got != "" {
		t.Errorf("traversal must be rejected, got %q", got)
	}
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
}

func mustWriteFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}
