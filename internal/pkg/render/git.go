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

// branch returns the current branch name, walking up from start until it
// finds a .git directory or .git pointer file. Returns "" for: empty start,
// not in a repo, detached HEAD, malformed HEAD, or I/O error.
func branch(start string) string {
	if start == "" {
		return ""
	}
	dir := filepath.Clean(start)
	for range maxWalkDepth {
		gitDir, ok := resolveGitDir(dir)
		if ok {
			return readHeadBranch(gitDir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
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
		// Reject relative gitdir: that escapes the parent directory.
		rel, err := filepath.Rel(dir, resolved)
		if err != nil || strings.HasPrefix(rel, "..") {
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
