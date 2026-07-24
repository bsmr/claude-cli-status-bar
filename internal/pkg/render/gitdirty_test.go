package render

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseDirtyCount_EmptyOutputIsZero(t *testing.T) {
	if got := parseDirtyCount(nil); got != 0 {
		t.Errorf("nil: got %d, want 0", got)
	}
	if got := parseDirtyCount([]byte("")); got != 0 {
		t.Errorf("empty: got %d, want 0", got)
	}
	if got := parseDirtyCount([]byte("\n")); got != 0 {
		t.Errorf("bare newline: got %d, want 0", got)
	}
}

func TestParseDirtyCount_CountsOneEntryPerLine(t *testing.T) {
	out := []byte(" M internal/pkg/render/git.go\n?? notes.txt\nA  cmd/new.go\n")
	if got := parseDirtyCount(out); got != 3 {
		t.Errorf("got %d, want 3", got)
	}
}

func TestParseDirtyCount_IgnoresMissingTrailingNewline(t *testing.T) {
	if got := parseDirtyCount([]byte(" M a.go\n?? b.go")); got != 2 {
		t.Errorf("got %d, want 2", got)
	}
}

func TestDirtyCachePath_IsHashedAndBelowStateDir(t *testing.T) {
	p := DirtyCachePath("/state", "/repo/.git")
	if dir := filepath.Dir(p); dir != filepath.Join("/state", "git-dirty") {
		t.Errorf("dir: got %q, want /state/git-dirty", dir)
	}
	// A repository path must never leak into the filename — a hashed name
	// keeps traversal and length limits out of the picture.
	if base := filepath.Base(p); base == "repo.json" || len(base) != 64+len(".json") {
		t.Errorf("base %q is not a hashed name", base)
	}
	if same := DirtyCachePath("/state", "/repo/.git"); same != p {
		t.Error("path is not stable for the same repository")
	}
	if other := DirtyCachePath("/state", "/elsewhere/.git"); other == p {
		t.Error("distinct repositories share a cache file")
	}
}

func TestReadDirtyCache_MissingOrMalformedIsNotFound(t *testing.T) {
	dir := t.TempDir()

	if _, ok := readDirtyCache(filepath.Join(dir, "absent.json")); ok {
		t.Error("missing file reported as found")
	}

	bad := filepath.Join(dir, "bad.json")
	mustWriteFile(t, bad, "{not json")
	if _, ok := readDirtyCache(bad); ok {
		t.Error("malformed file reported as found")
	}
}

func TestRefreshGitDirty_OutsideRepoWritesNothing(t *testing.T) {
	state := t.TempDir()
	if err := RefreshGitDirty(state, t.TempDir()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(state, "git-dirty")); len(entries) != 0 {
		t.Errorf("wrote %d cache files outside a repository", len(entries))
	}
}

// withDirtyCache seeds a cache entry for gitDir and returns the state dir.
func withDirtyCache(t *testing.T, gitDir string, count int, age time.Duration) string {
	t.Helper()
	state := t.TempDir()
	path := DirtyCachePath(state, gitDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	blob, err := json.Marshal(dirtyCache{Count: count, Unix: nowFunc().Add(-age).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		t.Fatal(err)
	}
	return state
}

// stubSpawn replaces the background refresher for the duration of a test and
// reports whether it was triggered.
func stubSpawn(t *testing.T) *bool {
	t.Helper()
	called := false
	prev := spawnDirtyRefresh
	spawnDirtyRefresh = func(string) bool { called = true; return true }
	t.Cleanup(func() { spawnDirtyRefresh = prev })
	return &called
}

// stubSpawnCount replaces the background refresher with a call counter, so a
// test can assert HOW MANY spawns a sequence of renders triggers (the
// single-flight contract), not merely whether one fired.
func stubSpawnCount(t *testing.T) *int {
	t.Helper()
	var n int
	prev := spawnDirtyRefresh
	spawnDirtyRefresh = func(string) bool { n++; return true }
	t.Cleanup(func() { spawnDirtyRefresh = prev })
	return &n
}

// dirtyRepo creates a repository-shaped directory and returns its working
// tree and git dir.
func dirtyRepo(t *testing.T) (workTree, gitDir string) {
	t.Helper()
	workTree = t.TempDir()
	gitDir = filepath.Join(workTree, ".git")
	mustMkdir(t, gitDir)
	mustWriteFile(t, filepath.Join(gitDir, "HEAD"), "ref: refs/heads/main\n")
	return workTree, gitDir
}

func TestRenderGitDirty_FreshCacheRendersWithoutRefresh(t *testing.T) {
	work, gitDir := dirtyRepo(t)
	state := withDirtyCache(t, gitDir, 2, 0)
	called := stubSpawn(t)

	env := renderEnv{cwd: work, stateDir: state, nowUnix: nowFunc().Unix()}
	if got := renderGitDirty(nil, Segment{}, env); got != "*2" {
		t.Errorf("got %q, want *2", got)
	}
	if *called {
		t.Error("refresh triggered although the cache was fresh")
	}
}

func TestRenderGitDirty_StaleCacheStillRendersAndTriggersRefresh(t *testing.T) {
	work, gitDir := dirtyRepo(t)
	state := withDirtyCache(t, gitDir, 7, dirtyCacheTTL+time.Second)
	called := stubSpawn(t)

	env := renderEnv{cwd: work, stateDir: state, nowUnix: nowFunc().Unix()}
	// The stale number is shown immediately — the point of the async
	// design is that a render never waits for the refresh.
	if got := renderGitDirty(nil, Segment{}, env); got != "*7" {
		t.Errorf("got %q, want *7", got)
	}
	if !*called {
		t.Error("stale cache did not trigger a refresh")
	}
}

func TestRenderGitDirty_NoCacheIsHiddenAndTriggersRefresh(t *testing.T) {
	work, _ := dirtyRepo(t)
	called := stubSpawn(t)

	env := renderEnv{cwd: work, stateDir: t.TempDir(), nowUnix: nowFunc().Unix()}
	if got := renderGitDirty(nil, Segment{}, env); got != "" {
		t.Errorf("got %q, want empty", got)
	}
	if !*called {
		t.Error("missing cache did not trigger a refresh")
	}
}

func TestRenderGitDirty_CleanTreeIsHidden(t *testing.T) {
	work, gitDir := dirtyRepo(t)
	state := withDirtyCache(t, gitDir, 0, 0)
	stubSpawn(t)

	env := renderEnv{cwd: work, stateDir: state, nowUnix: nowFunc().Unix()}
	if got := renderGitDirty(nil, Segment{}, env); got != "" {
		t.Errorf("clean tree: got %q, want empty", got)
	}
}

func TestRenderGitDirty_OutsideRepoIsHiddenAndSkipsRefresh(t *testing.T) {
	called := stubSpawn(t)

	env := renderEnv{cwd: t.TempDir(), stateDir: t.TempDir(), nowUnix: nowFunc().Unix()}
	if got := renderGitDirty(nil, Segment{}, env); got != "" {
		t.Errorf("got %q, want empty", got)
	}
	if *called {
		t.Error("refresh triggered outside a repository")
	}
}

func TestRenderGitDirty_WithoutStateDirIsHidden(t *testing.T) {
	work, _ := dirtyRepo(t)
	called := stubSpawn(t)

	env := renderEnv{cwd: work, nowUnix: nowFunc().Unix()}
	if got := renderGitDirty(nil, Segment{}, env); got != "" {
		t.Errorf("got %q, want empty", got)
	}
	if *called {
		t.Error("refresh triggered without a cache location")
	}
}

func TestRenderGitDirty_FormatAndLabel(t *testing.T) {
	work, gitDir := dirtyRepo(t)
	state := withDirtyCache(t, gitDir, 4, 0)
	stubSpawn(t)
	env := renderEnv{cwd: work, stateDir: state, nowUnix: nowFunc().Unix()}

	if got := renderGitDirty(nil, Segment{Format: "±{n}"}, env); got != "±4" {
		t.Errorf("format: got %q, want ±4", got)
	}
	if got := renderGitDirty(nil, Segment{Label: "dirty"}, env); got != "dirty: *4" {
		t.Errorf("label: got %q, want \"dirty: *4\"", got)
	}
}

func TestRenderGitDirty_RegisteredAsSegmentType(t *testing.T) {
	if _, ok := segmentFuncs["git_dirty"]; !ok {
		t.Error("git_dirty is not registered in the segment registry")
	}
}

// TestRefreshGitDirty_RealRepository exercises the one code path that needs
// the git binary. Skipped where git is unavailable so the suite stays
// runnable without it.
func TestRefreshGitDirty_RealRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	work := t.TempDir()
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
	} {
		cmd := exec.Command("git", append([]string{"-C", work}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	mustWriteFile(t, filepath.Join(work, "untracked.txt"), "hello\n")

	state := t.TempDir()
	if err := RefreshGitDirty(state, work); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	gitDir, ok := nearestGitDir(work)
	if !ok {
		t.Fatal("initialised repository not found by nearestGitDir")
	}
	cached, found := readDirtyCache(DirtyCachePath(state, gitDir))
	if !found {
		t.Fatal("refresh wrote no cache entry")
	}
	if cached.Count != 1 {
		t.Errorf("count: got %d, want 1", cached.Count)
	}
}

// TestRenderGitDirty_SingleFlightAcrossRepeatedCalls pins the single-flight
// contract for REPEATED IN-PROCESS calls: the layout engine invokes a segment
// function several times per render (measurement passes in rowOverflows /
// applyShrink plus the final render), and consecutive renders repeat within
// the cache TTL. A stale cache must spawn AT MOST ONE refresher across all of
// them — not one per call — while still showing the stale value each time.
// (Cross-process contention from parallel Claude sessions is covered by the
// -race lock tests below.)
func TestRenderGitDirty_SingleFlightAcrossRepeatedCalls(t *testing.T) {
	work, gitDir := dirtyRepo(t)
	state := withDirtyCache(t, gitDir, 7, dirtyCacheTTL+time.Second) // stale
	n := stubSpawnCount(t)

	env := renderEnv{cwd: work, stateDir: state, nowUnix: nowFunc().Unix()}
	for i := range 5 {
		if got := renderGitDirty(nil, Segment{}, env); got != "*7" {
			t.Fatalf("call %d: got %q, want *7", i, got)
		}
	}
	if *n != 1 {
		t.Errorf("stale cache spawned %d refreshers across 5 renders, want 1 (single-flight)", *n)
	}
}

// TestRenderGitDirty_ReleasesLockWhenSpawnFails covers recovery from a failed
// fork: if the detached refresher never starts, renderGitDirty must release
// the single-flight marker right away so the NEXT render retries — otherwise a
// single transient failure would suppress the refresh for the whole lock TTL.
func TestRenderGitDirty_ReleasesLockWhenSpawnFails(t *testing.T) {
	work, gitDir := dirtyRepo(t)
	state := t.TempDir() // no cache -> render wants to refresh
	prev := spawnDirtyRefresh
	spawnDirtyRefresh = func(string) bool { return false } // simulate fork failure
	t.Cleanup(func() { spawnDirtyRefresh = prev })

	env := renderEnv{cwd: work, stateDir: state, nowUnix: nowFunc().Unix()}
	renderGitDirty(nil, Segment{}, env)

	// The marker must have been released, so a fresh acquire succeeds.
	if !acquireRefreshLock(state, gitDir) {
		t.Error("marker not released after a failed spawn — refresh is locked out for the TTL")
	}
}

func TestAcquireRefreshLock_SecondCallerBacksOffUntilReleased(t *testing.T) {
	state := t.TempDir()
	const gitDir = "/repo/.git"

	if !acquireRefreshLock(state, gitDir) {
		t.Fatal("first caller should acquire the lock")
	}
	if acquireRefreshLock(state, gitDir) {
		t.Error("second caller should back off while a refresh is in flight")
	}
	// A distinct repository has its own lock — sessions in different repos
	// never contend.
	if !acquireRefreshLock(state, "/other/.git") {
		t.Error("distinct repository should acquire independently")
	}
	// Once the refresher releases, the next stale render may start a new one.
	releaseRefreshLock(state, gitDir)
	if !acquireRefreshLock(state, gitDir) {
		t.Error("caller should re-acquire after release")
	}
}

func TestAcquireRefreshLock_ReclaimsOrphanedMarker(t *testing.T) {
	state := t.TempDir()
	const gitDir = "/repo/.git"

	if !acquireRefreshLock(state, gitDir) {
		t.Fatal("first acquire")
	}
	// Simulate a refresher that died without releasing: backdate the marker
	// past its TTL so it reads as orphaned.
	path := dirtyLockPath(state, gitDir)
	old := time.Now().Add(-refreshLockTTL - time.Minute)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if !acquireRefreshLock(state, gitDir) {
		t.Error("an orphaned (past-TTL) marker should be reclaimed")
	}
	// The just-reclaimed marker is fresh again, so the next caller backs off.
	if acquireRefreshLock(state, gitDir) {
		t.Error("reclaimed marker should be fresh and block the next caller")
	}
}

// TestAcquireRefreshLock_ConcurrentSingleWinner is the real race test: it runs
// under `go test -race`. The lock's cross-process guarantee rests on the
// atomicity of O_CREATE|O_EXCL; here many goroutines contend for the same
// marker at once and exactly one must win. -race additionally proves the Go
// code around the syscall carries no data race of its own.
func TestAcquireRefreshLock_ConcurrentSingleWinner(t *testing.T) {
	state := t.TempDir()
	const gitDir = "/repo/.git"
	const goroutines = 50

	var wins atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range goroutines {
		wg.Go(func() {
			<-start // release all at once to maximise contention
			if acquireRefreshLock(state, gitDir) {
				wins.Add(1)
			}
		})
	}
	close(start)
	wg.Wait()

	if got := wins.Load(); got != 1 {
		t.Errorf("%d goroutines won the lock, want exactly 1", got)
	}
}

// TestAcquireRefreshLock_ConcurrentReclaimIsLiveAndSelfHealing pins the
// reclaim path's REAL contract. Reclaiming an orphaned marker is best-effort,
// not strict single-flight — a lock-free orphan replacement cannot be (see
// acquireRefreshLock's ceiling comment). What MUST hold is:
//   - Liveness: an orphaned marker never wedges the cache; at least one racing
//     caller reclaims it and spawns (never zero).
//   - Self-healing: afterward exactly one fresh marker remains, so the next
//     render backs off.
//
// Strict exactly-one-winner is the STEADY-STATE guarantee, covered by the
// create path in TestAcquireRefreshLock_ConcurrentSingleWinner. Runs -race.
func TestAcquireRefreshLock_ConcurrentReclaimIsLiveAndSelfHealing(t *testing.T) {
	const goroutines = 8
	const trials = 100
	for trial := range trials {
		state := t.TempDir()
		const gitDir = "/repo/.git"

		// Seed an orphaned marker: acquire once, then backdate it past the TTL.
		if !acquireRefreshLock(state, gitDir) {
			t.Fatalf("trial %d: seed acquire", trial)
		}
		old := time.Now().Add(-refreshLockTTL - time.Minute)
		if err := os.Chtimes(dirtyLockPath(state, gitDir), old, old); err != nil {
			t.Fatalf("trial %d: backdate: %v", trial, err)
		}

		var wins atomic.Int32
		var wg sync.WaitGroup
		start := make(chan struct{})
		for range goroutines {
			wg.Go(func() {
				<-start
				if acquireRefreshLock(state, gitDir) {
					wins.Add(1)
				}
			})
		}
		close(start)
		wg.Wait()

		if got := wins.Load(); got < 1 {
			t.Fatalf("trial %d: orphaned marker wedged, %d reclaimers, want >= 1", trial, got)
		}
		// Self-healing: a fresh marker remains, so a further caller backs off.
		if acquireRefreshLock(state, gitDir) {
			t.Fatalf("trial %d: marker not fresh after reclaim", trial)
		}
	}
}
