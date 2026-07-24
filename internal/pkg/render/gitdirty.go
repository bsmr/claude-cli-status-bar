package render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/fileutil"
)

const (
	// dirtyCacheTTL is how long a cached count is served before the next
	// render triggers a background refresh. Short enough that the number
	// tracks editing, long enough that a burst of status updates does not
	// spawn a git process each time.
	dirtyCacheTTL = 3 * time.Second

	// dirtyTimeout bounds the git subprocess in the *background* refresher.
	// It never delays a render — it only stops a pathological repository
	// from leaving a refresher running indefinitely.
	dirtyTimeout = 10 * time.Second

	// refreshGitDirtySubcommand is the hidden subcommand the renderer
	// re-executes itself with to perform the refresh out of band.
	refreshGitDirtySubcommand = "refresh-git-dirty"
)

// dirtyCache is the on-disk shape of one repository's cached count.
type dirtyCache struct {
	Count int   `json:"count"`
	Unix  int64 `json:"unix"`
}

// DirtyCachePath returns the cache file backing the git_dirty segment for
// the repository identified by gitDir. The name is a hash so arbitrary
// repository paths cannot escape the cache directory.
func DirtyCachePath(stateDir, gitDir string) string {
	sum := sha256.Sum256([]byte(gitDir))
	return filepath.Join(stateDir, "git-dirty", hex.EncodeToString(sum[:])+".json")
}

// readDirtyCache loads a cached count. ok is false for a missing,
// unreadable, or malformed file — every one of which simply means
// "no number to show yet".
func readDirtyCache(path string) (dirtyCache, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return dirtyCache{}, false
	}
	var c dirtyCache
	if err := json.Unmarshal(raw, &c); err != nil {
		return dirtyCache{}, false
	}
	return c, true
}

// RefreshGitDirty counts the changed paths in the repository containing dir
// and writes the result to the cache the git_dirty segment reads. It is the
// body of the hidden `ccsb refresh-git-dirty <dir>` subcommand, which the
// renderer starts in the background — this is the only code path in ccsb
// that runs git, and it never executes inside a render.
func RefreshGitDirty(stateDir, dir string) error {
	gitDir, ok := nearestGitDir(dir)
	if !ok {
		return nil // not a repository: nothing to cache
	}
	n, err := countDirty(dir)
	if err != nil {
		return err
	}
	blob, err := json.Marshal(dirtyCache{Count: n, Unix: nowFunc().Unix()})
	if err != nil {
		return err
	}
	path := DirtyCachePath(stateDir, gitDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return fileutil.WriteAtomic(path, blob)
}

// countDirty runs `git status --porcelain` in dir and returns the number of
// reported paths — modified, staged, deleted, renamed, and untracked alike.
//
// Unlike the branch name, which is a single file read (see git.go), a dirty
// count is a comparison of the index against the working tree: there is no
// cheap file to read it from. Confining that cost to the background
// refresher is what keeps the render path free of subprocesses.
func countDirty(dir string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dirtyTimeout)
	defer cancel()

	// -C makes git resolve the repository from dir, so the refresher needs
	// no working-directory manipulation of its own process.
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "status", "--porcelain").Output()
	if err != nil {
		return 0, err
	}
	return parseDirtyCount(out), nil
}

// parseDirtyCount counts the entries in `git status --porcelain` output.
// Porcelain v1 emits exactly one line per path, so the count is the number
// of non-empty lines; the trailing newline of the last entry is ignored.
func parseDirtyCount(out []byte) int {
	n := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// spawnDirtyRefresh re-executes ccsb as `ccsb refresh-git-dirty <dir>` and
// returns immediately without waiting. The child inherits the environment,
// so it resolves the same state directory this process did. Every failure
// is silent by design: a refresh that cannot start simply leaves the cached
// value in place for the next render to retry.
//
// Overridable so tests can observe the trigger without starting processes.
var spawnDirtyRefresh = func(dir string) {
	self, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(self, refreshGitDirtySubcommand, dir)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return
	}
	// Release the handle rather than Wait: ccsb exits as soon as the line
	// is printed, and the refresher is reparented to init to finish on its
	// own. Holding a handle we never wait on would only leak it.
	_ = cmd.Process.Release()
}
