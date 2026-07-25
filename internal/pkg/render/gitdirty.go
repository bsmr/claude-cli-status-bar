package render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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

	// refreshLockTTL bounds how long a single-flight marker suppresses new
	// refreshers. A marker older than this is treated as orphaned — the
	// refresher that wrote it crashed or was killed before clearing it — and
	// is reclaimed. It must exceed dirtyTimeout so a legitimately-running
	// refresh (bounded by dirtyTimeout) is never pre-empted.
	refreshLockTTL = dirtyTimeout + 5*time.Second
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

// dirtyLockPath returns the single-flight marker guarding refreshes for the
// repository identified by gitDir — a sibling of its cache entry.
func dirtyLockPath(stateDir, gitDir string) string {
	return DirtyCachePath(stateDir, gitDir) + ".pending"
}

// acquireRefreshLock reports whether the caller should start a refresher for
// gitDir. It atomically creates a marker with O_CREATE|O_EXCL: across every
// process, exactly one caller wins the create and may spawn; all others see
// the existing marker and back off. That is what keeps a stale cache — hit by
// the layout engine's per-render measurement passes, by consecutive renders,
// and by every parallel Claude session on the same repo — down to ONE
// refresher instead of a pile of concurrent git processes.
//
// A marker older than refreshLockTTL is orphaned (its refresher died before
// clearing it) and is reclaimed. Every error path backs off (returns false):
// a refresh that cannot be coordinated is skipped, never duplicated — the
// cache simply stays as-is for the next render to retry.
func acquireRefreshLock(stateDir, gitDir string) bool {
	path := dirtyLockPath(stateDir, gitDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false
	}
	switch f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600); {
	case err == nil:
		_ = f.Close()
		return true
	case !errors.Is(err, os.ErrExist):
		return false // cannot coordinate — do not risk a duplicate spawn
	}
	// Marker present: reclaim it only if it is older than the TTL (orphaned —
	// its refresher crashed or was killed before releasing). Remove and
	// recreate.
	//
	// ponytail: the reclaim branch is best-effort, NOT strict single-flight.
	// Replacing an orphaned marker atomically is a compare-and-swap on a file,
	// which POSIX does not offer without flock; any Remove/rename briefly
	// empties the path and lets a peer's O_EXCL create win alongside this one.
	// So two sessions reclaiming the SAME orphan at the SAME instant can each
	// spawn — bounded by the number of concurrent sessions, self-healing, and
	// never a wrong count (the cache is written only via atomic rename and the
	// marker never gates cache contents). It costs a few extra `git status`
	// processes in the rare window after a refresher crash. A real lock (flock,
	// + a Windows port, + moving the lock into the detached refresher) is not
	// worth that. The steady-state create path above IS strict single-flight.
	if info, err := os.Stat(path); err != nil || time.Since(info.ModTime()) < refreshLockTTL {
		return false // a refresh is in flight
	}
	if err := os.Remove(path); err != nil {
		return false
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return false // a peer recreated the marker first
	}
	_ = f.Close()
	return true
}

// releaseRefreshLock clears the single-flight marker for gitDir. The refresher
// calls it when finished so the next stale-cache render can start a new one; a
// missing marker (already reclaimed via the TTL) is fine.
func releaseRefreshLock(stateDir, gitDir string) {
	_ = os.Remove(dirtyLockPath(stateDir, gitDir))
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
	// Clear the single-flight marker on the way out — whether the count
	// succeeds or git fails — so the next stale render is not blocked until
	// the marker's TTL expires.
	defer releaseRefreshLock(stateDir, gitDir)
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
// refresher is what keeps git out of the render path.
func countDirty(dir string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dirtyTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "git", dirtyStatusArgs(dir)...).Output()
	if err != nil {
		return 0, err
	}
	return parseDirtyCount(out), nil
}

// dirtyStatusArgs builds the argv for the dirty count.
//
// -C makes git resolve the repository from dir, so the refresher needs no
// working-directory manipulation of its own process. dir comes from the
// untrusted payload (workspace.current_dir), and `git status` honours the
// target repository's own .git/config — where core.fsmonitor names a program
// git EXECUTES during the status walk. git's safe.directory guard only fires
// for cross-owner repositories, so a same-owner hostile repository is not
// covered by it. Command-line -c has the highest config precedence, so
// clearing core.fsmonitor here cannot be re-enabled by the repository. The
// count needs no fsmonitor acceleration, so nothing is lost.
func dirtyStatusArgs(dir string) []string {
	return []string{"-c", "core.fsmonitor=", "-C", dir, "status", "--porcelain"}
}

// parseDirtyCount counts the entries in `git status --porcelain` output.
// Porcelain v1 emits exactly one line per path, so the count is the number
// of non-empty lines; the trailing newline of the last entry is ignored.
func parseDirtyCount(out []byte) int {
	n := 0
	for line := range strings.SplitSeq(string(out), "\n") {
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
// spawnDirtyRefresh reports whether the detached refresher actually started.
// The caller holds the single-flight marker and releases it on false, so a
// transient fork failure does not lock out the refresh for the whole TTL.
var spawnDirtyRefresh = func(dir string) bool {
	self, err := os.Executable()
	if err != nil {
		return false
	}
	cmd := exec.Command(self, refreshGitDirtySubcommand, dir)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return false
	}
	// Release the handle rather than Wait: ccsb exits as soon as the line
	// is printed, and the refresher is reparented to init to finish on its
	// own. Holding a handle we never wait on would only leak it.
	_ = cmd.Process.Release()
	return true
}
