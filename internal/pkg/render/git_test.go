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

// submoduleFixture builds a superproject at <root>/super on branch
// superBranch, with a submodule working tree at <super>/sub whose gitdir
// lives at <super>/.git/modules/foo on branch subBranch. The submodule .git
// file uses a relative "gitdir:" pointer, exactly as git emits. It returns
// the submodule working-tree path.
func submoduleFixture(t *testing.T, root, superBranch, subBranch string) string {
	t.Helper()
	super := filepath.Join(root, "super")
	mustMkdir(t, filepath.Join(super, ".git"))
	mustWriteFile(t, filepath.Join(super, ".git", "HEAD"), "ref: refs/heads/"+superBranch+"\n")

	modGit := filepath.Join(super, ".git", "modules", "foo")
	mustMkdir(t, modGit)
	mustWriteFile(t, filepath.Join(modGit, "HEAD"), "ref: refs/heads/"+subBranch+"\n")

	sub := filepath.Join(super, "sub")
	mustMkdir(t, sub)
	// Relative pointer that escapes the working tree, the canonical submodule shape.
	mustWriteFile(t, filepath.Join(sub, ".git"), "gitdir: ../.git/modules/foo\n")
	return sub
}

func TestBranchScoped_SubmoduleLocalShowsSubmoduleBranch(t *testing.T) {
	sub := submoduleFixture(t, t.TempDir(), "main", "feature-sub")

	if got := branchScoped(sub, "local"); got != "feature-sub" {
		t.Errorf("local: got %q, want feature-sub", got)
	}
	if got := branchScoped(sub, ""); got != "feature-sub" {
		t.Errorf("default scope: got %q, want feature-sub", got)
	}
}

func TestBranchScoped_SubmoduleToplevelShowsSuperprojectBranch(t *testing.T) {
	sub := submoduleFixture(t, t.TempDir(), "main", "feature-sub")

	if got := branchScoped(sub, "toplevel"); got != "main" {
		t.Errorf("toplevel: got %q, want main", got)
	}
}

func TestBranchScoped_NestedSubmoduleToplevelReachesOutermost(t *testing.T) {
	root := t.TempDir()
	super := filepath.Join(root, "super")
	mustMkdir(t, filepath.Join(super, ".git"))
	mustWriteFile(t, filepath.Join(super, ".git", "HEAD"), "ref: refs/heads/main\n")

	// git flattens nested submodule gitdirs under <top>/.git/modules/a/modules/b.
	innerGit := filepath.Join(super, ".git", "modules", "a", "modules", "b")
	mustMkdir(t, innerGit)
	mustWriteFile(t, filepath.Join(innerGit, "HEAD"), "ref: refs/heads/inner\n")

	inner := filepath.Join(super, "a", "b")
	mustMkdir(t, inner)
	mustWriteFile(t, filepath.Join(inner, ".git"), "gitdir: ../../.git/modules/a/modules/b\n")

	if got := branchScoped(inner, "toplevel"); got != "main" {
		t.Errorf("nested toplevel: got %q, want main", got)
	}
	if got := branchScoped(inner, "local"); got != "inner" {
		t.Errorf("nested local: got %q, want inner", got)
	}
}

func TestBranchScoped_RegularRepoToplevelEqualsLocal(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, ".git"))
	mustWriteFile(t, filepath.Join(dir, ".git", "HEAD"), "ref: refs/heads/main\n")

	if got := branchScoped(dir, "toplevel"); got != "main" {
		t.Errorf("regular toplevel: got %q, want main", got)
	}
	if got := branchScoped(dir, "local"); got != "main" {
		t.Errorf("regular local: got %q, want main", got)
	}
}

func TestBranchScoped_WorktreeToplevelKeepsWorktreeBranch(t *testing.T) {
	root := t.TempDir()
	realGit := filepath.Join(root, "main-repo", ".git")
	mustMkdir(t, realGit)
	mustWriteFile(t, filepath.Join(realGit, "HEAD"), "ref: refs/heads/main\n")
	worktreeGit := filepath.Join(realGit, "worktrees", "wt1")
	mustMkdir(t, worktreeGit)
	mustWriteFile(t, filepath.Join(worktreeGit, "HEAD"), "ref: refs/heads/wt-branch\n")

	wtDir := filepath.Join(root, "wt1")
	mustMkdir(t, wtDir)
	mustWriteFile(t, filepath.Join(wtDir, ".git"), "gitdir: "+worktreeGit+"\n")

	// A worktree gitdir has no "modules" component, so toplevel must NOT
	// collapse to the main worktree's branch.
	if got := branchScoped(wtDir, "toplevel"); got != "wt-branch" {
		t.Errorf("worktree toplevel: got %q, want wt-branch", got)
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
