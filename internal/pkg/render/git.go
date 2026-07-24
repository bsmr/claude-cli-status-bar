package render

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	branchRefPrefix = "ref: refs/heads/"
	// maxWalkDepth bounds the parent-walk from start so a circular symlink or
	// pathological filesystem cannot pin the renderer.
	maxWalkDepth = 30
)

// branch returns the current branch name for the nearest repository, walking
// up from start. Equivalent to branchScoped(start, "local").
func branch(start string) string {
	return branchScoped(start, "local")
}

// branchScoped returns the current branch name, walking up from start until it
// finds a .git directory or .git pointer file. Returns "" for: empty start,
// not in a repo, detached HEAD, malformed HEAD, or I/O error.
//
// scope selects which repository's branch to report when start is inside a
// submodule working tree:
//   - "local" (also "" and any unknown value): the nearest repository — the
//     submodule itself when start lies within one.
//   - "toplevel": the outermost superproject of the submodule chain.
func branchScoped(start, scope string) string {
	gitDir, ok := nearestGitDir(start)
	if !ok {
		return ""
	}
	if scope == "toplevel" {
		if top := submoduleTopLevelGitDir(gitDir); top != "" {
			return readHeadBranch(top)
		}
	}
	return readHeadBranch(gitDir)
}

// nearestGitDir walks up from start until resolveGitDir finds a repository,
// and reports the resolved git dir. ok is false for an empty start, a
// filesystem root reached without a hit, or a walk longer than
// maxWalkDepth.
func nearestGitDir(start string) (string, bool) {
	if start == "" {
		return "", false
	}
	dir := filepath.Clean(start)
	for range maxWalkDepth {
		if gitDir, ok := resolveGitDir(dir); ok {
			return gitDir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
	return "", false
}

// submoduleTopLevelGitDir returns the git dir of the outermost superproject
// when gitDir has the canonical submodule shape
// <top>/.git/modules/<name>[/modules/<name>…] — the prefix up to and including
// the FIRST ".git" component immediately followed by "modules", i.e.
// <top>/.git. git flattens nested submodule git dirs under that single
// modules tree, so the first match is always the outermost repository.
// Returns "" when gitDir is not a submodule git dir (regular repo, worktree,
// …), in which case toplevel scope falls back to the local branch.
func submoduleTopLevelGitDir(gitDir string) string {
	parts := strings.Split(gitDir, string(filepath.Separator))
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == ".git" && parts[i+1] == "modules" {
			return strings.Join(parts[:i+1], string(filepath.Separator))
		}
	}
	return ""
}

// resolveGitDir checks <dir>/.git. If it's a directory, returns it as the
// git dir. If it's a file, parses the "gitdir: <path>" pointer (relative
// paths are resolved against <dir>) and returns the resolved path.
func resolveGitDir(dir string) (string, bool) {
	p := filepath.Join(dir, ".git")
	// Lstat avoids following a symlink at the .git slot itself, which would be
	// unusual but possible.
	info, err := os.Lstat(p)
	if err != nil {
		return "", false
	}
	if info.IsDir() {
		return p, true
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	line := strings.TrimSpace(string(data))
	rest, ok := strings.CutPrefix(line, "gitdir: ")
	if !ok {
		return "", false
	}
	var resolved string
	if filepath.IsAbs(rest) {
		resolved = filepath.Clean(rest)
	} else {
		// Relative paths are resolved against dir.
		resolved = filepath.Clean(filepath.Join(dir, rest))
		rel, err := filepath.Rel(dir, resolved)
		if err != nil {
			return "", false
		}
		// A relative gitdir: that escapes the working tree is only legitimate
		// when it points into a ".git/modules/" tree — the canonical submodule
		// layout git emits (e.g. "../../.git/modules/<name>"). Any other escape
		// (e.g. "../../../etc") is rejected.
		if strings.HasPrefix(rel, "..") && submoduleTopLevelGitDir(resolved) == "" {
			return "", false
		}
	}
	return resolved, true
}

// readHeadBranch parses gitDir/HEAD; returns "" for detached HEAD or any
// read/parse error.
func readHeadBranch(gitDir string) string {
	data, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(data))
	rest, ok := strings.CutPrefix(s, branchRefPrefix)
	if !ok {
		return ""
	}
	return rest
}
