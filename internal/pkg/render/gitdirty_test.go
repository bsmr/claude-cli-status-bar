package render

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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
	spawnDirtyRefresh = func(string) { called = true }
	t.Cleanup(func() { spawnDirtyRefresh = prev })
	return &called
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
