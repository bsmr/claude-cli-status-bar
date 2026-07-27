package render

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseSemver_ValidAndInvalid(t *testing.T) {
	cases := []struct {
		in   string
		want Semver
		ok   bool
	}{
		{"v0.4.6", Semver{0, 4, 6}, true},
		{"0.4.6", Semver{0, 4, 6}, true},
		{"v2.0.0", Semver{2, 0, 0}, true},
		{"", Semver{}, false},
		{"v1.2", Semver{}, false},
		{"v1.2.3.4", Semver{}, false},
		{"vX.Y.Z", Semver{}, false},
		{"dev", Semver{}, false},
	}
	for _, c := range cases {
		got, ok := ParseSemver(c.in)
		if ok != c.ok {
			t.Errorf("ParseSemver(%q): ok = %v, want %v", c.in, ok, c.ok)
			continue
		}
		if ok && got != c.want {
			t.Errorf("ParseSemver(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestCompareSeverity(t *testing.T) {
	cases := []struct {
		name            string
		current, latest Semver
		want            Severity
	}{
		{"equal", Semver{0, 4, 6}, Semver{0, 4, 6}, SeverityNone},
		{"older latest", Semver{0, 4, 6}, Semver{0, 4, 5}, SeverityNone},
		{"patch newer", Semver{0, 4, 6}, Semver{0, 4, 9}, SeverityPatch},
		{"minor newer", Semver{0, 4, 6}, Semver{0, 5, 0}, SeverityMinor},
		{"minor newer ignores lower patch", Semver{0, 4, 6}, Semver{0, 5, 0}, SeverityMinor},
		{"major one ahead", Semver{0, 4, 6}, Semver{1, 0, 0}, SeverityMajor},
		{"major two ahead", Semver{0, 4, 6}, Semver{2, 0, 0}, SeverityMajorFar},
		{"major far ahead", Semver{0, 4, 6}, Semver{5, 1, 2}, SeverityMajorFar},
	}
	for _, c := range cases {
		if got := CompareSeverity(c.current, c.latest); got != c.want {
			t.Errorf("%s: CompareSeverity(%+v, %+v) = %v, want %v", c.name, c.current, c.latest, got, c.want)
		}
	}
}

func TestParseUpdateCheckInterval(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"", defaultUpdateCheckInterval},
		{"garbage", defaultUpdateCheckInterval},
		{"0h", defaultUpdateCheckInterval},
		{"-1h", defaultUpdateCheckInterval},
		{"6h", 6 * time.Hour},
		{"90m", 90 * time.Minute},
	}
	for _, c := range cases {
		if got := parseUpdateCheckInterval(c.in); got != c.want {
			t.Errorf("parseUpdateCheckInterval(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestUpdateCachePath_IsBelowStateDir(t *testing.T) {
	got := UpdateCachePath("/state")
	want := filepath.Join("/state", "update-check.json")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReadUpdateCache_MissingOrMalformedIsNotFound(t *testing.T) {
	dir := t.TempDir()

	if _, ok := readUpdateCache(filepath.Join(dir, "absent.json")); ok {
		t.Error("missing file reported as found")
	}

	bad := filepath.Join(dir, "bad.json")
	mustWriteFile(t, bad, "{not json")
	if _, ok := readUpdateCache(bad); ok {
		t.Error("malformed file reported as found")
	}
}

func TestFetchLatestTag_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request sent without a User-Agent header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name": "v0.4.9"}`))
	}))
	defer srv.Close()

	got, err := FetchLatestTag(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "v0.4.9" {
		t.Errorf("got %q, want v0.4.9", got)
	}
}

func TestFetchLatestTag_NonOKStatusIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if _, err := FetchLatestTag(context.Background(), srv.URL); err == nil {
		t.Fatal("expected an error for a 404 response")
	}
}

func TestFetchLatestTag_MalformedJSONIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{not json"))
	}))
	defer srv.Close()

	if _, err := FetchLatestTag(context.Background(), srv.URL); err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
}

func TestFetchLatestTag_EmptyTagNameIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name": ""}`))
	}))
	defer srv.Close()

	if _, err := FetchLatestTag(context.Background(), srv.URL); err == nil {
		t.Fatal("expected an error for an empty tag_name")
	}
}

func TestFetchLatestTag_UnreachableServerIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // now guaranteed unreachable

	if _, err := FetchLatestTag(context.Background(), url); err == nil {
		t.Fatal("expected an error for an unreachable server")
	}
}

func TestFetchLatestTag_OversizedBodyIsBounded(t *testing.T) {
	// The genuine tag_name only appears after maxUpdateCheckResponseBytes
	// of padding. If FetchLatestTag read the body without a bound, the
	// full JSON would parse and "v1.0.0" would come back with no error;
	// bounded to maxUpdateCheckResponseBytes, the decoder only ever sees
	// the still-open padding string and must fail.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		padding := strings.Repeat("x", maxUpdateCheckResponseBytes+1)
		_, _ = w.Write([]byte(`{"padding":"` + padding + `","tag_name":"v1.0.0"}`))
	}))
	defer srv.Close()

	if _, err := FetchLatestTag(context.Background(), srv.URL); err == nil {
		t.Fatal("expected an error for a response exceeding the size bound")
	}
}

func TestRefreshUpdateCheck_WritesCacheOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name": "v0.4.9"}`))
	}))
	defer srv.Close()
	prev := updateCheckURL
	updateCheckURL = srv.URL
	t.Cleanup(func() { updateCheckURL = prev })

	state := t.TempDir()
	if err := RefreshUpdateCheck(state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cached, ok := readUpdateCache(UpdateCachePath(state))
	if !ok {
		t.Fatal("no cache written")
	}
	if cached.LatestTag != "v0.4.9" {
		t.Errorf("got %q, want v0.4.9", cached.LatestTag)
	}
}

func TestRefreshUpdateCheck_ReleasesLockEvenOnFailure(t *testing.T) {
	prev := updateCheckURL
	updateCheckURL = "http://127.0.0.1:1" // guaranteed-unreachable: connection refused, fast
	t.Cleanup(func() { updateCheckURL = prev })

	state := t.TempDir()
	if !acquireLock(updateLockPath(state), updateCheckLockTTL) {
		t.Fatal("seed acquire")
	}
	if err := RefreshUpdateCheck(state); err == nil {
		t.Fatal("expected an error against an unreachable endpoint")
	}
	if !acquireLock(updateLockPath(state), updateCheckLockTTL) {
		t.Error("lock was not released after RefreshUpdateCheck returned")
	}
}

// TestRefreshUpdateCheck_FailureStampsCacheWithFreshTimestamp is finding 1
// from the final review: a failed fetch must still stamp the cache so the
// TTL gate suppresses the next retry, instead of leaving "no cache" (which
// renderUpdateSuffix treats identically to "stale cache" and retries on
// every single render).
func TestRefreshUpdateCheck_FailureStampsCacheWithFreshTimestamp(t *testing.T) {
	prev := updateCheckURL
	updateCheckURL = "http://127.0.0.1:1" // guaranteed-unreachable: connection refused, fast
	t.Cleanup(func() { updateCheckURL = prev })

	state := t.TempDir()
	before := nowFunc().Unix()
	if err := RefreshUpdateCheck(state); err == nil {
		t.Fatal("expected an error against an unreachable endpoint")
	}

	cached, ok := readUpdateCache(UpdateCachePath(state))
	if !ok {
		t.Fatal("failed refresh must still write a cache entry")
	}
	if cached.LatestTag != "" {
		t.Errorf("got LatestTag %q, want empty (no prior tag to preserve)", cached.LatestTag)
	}
	if cached.Unix < before {
		t.Errorf("cache timestamp %d predates the refresh attempt (before %d)", cached.Unix, before)
	}
}

// TestRefreshUpdateCheck_FailurePreservesPreviousTag confirms a failed
// refresh does not clobber a tag learned by an earlier successful check —
// only the timestamp is refreshed.
func TestRefreshUpdateCheck_FailurePreservesPreviousTag(t *testing.T) {
	state := t.TempDir()
	seed, err := json.Marshal(updateCache{LatestTag: "v0.4.6", Unix: nowFunc().Add(-48 * time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, UpdateCachePath(state), string(seed))

	prev := updateCheckURL
	updateCheckURL = "http://127.0.0.1:1" // guaranteed-unreachable: connection refused, fast
	t.Cleanup(func() { updateCheckURL = prev })

	if err := RefreshUpdateCheck(state); err == nil {
		t.Fatal("expected an error against an unreachable endpoint")
	}

	cached, ok := readUpdateCache(UpdateCachePath(state))
	if !ok {
		t.Fatal("failed refresh must still write a cache entry")
	}
	if cached.LatestTag != "v0.4.6" {
		t.Errorf("got LatestTag %q, want v0.4.6 (previous tag preserved)", cached.LatestTag)
	}
}

// TestRefreshUpdateCheck_FailureThenStaleCheckWithinTTLDoesNotRetrigger is
// the second half of finding 1: after a failed refresh stamps the cache,
// a render observing that cache within the TTL window must NOT trigger
// another background spawn.
func TestRefreshUpdateCheck_FailureThenStaleCheckWithinTTLDoesNotRetrigger(t *testing.T) {
	prev := updateCheckURL
	updateCheckURL = "http://127.0.0.1:1" // guaranteed-unreachable: connection refused, fast
	t.Cleanup(func() { updateCheckURL = prev })

	state := t.TempDir()
	if err := RefreshUpdateCheck(state); err == nil {
		t.Fatal("expected an error against an unreachable endpoint")
	}

	called := false
	prevSpawn := spawnUpdateCheckRefresh
	spawnUpdateCheckRefresh = func() bool { called = true; return true }
	t.Cleanup(func() { spawnUpdateCheckRefresh = prevSpawn })

	env := renderEnv{version: "0.4.6", stateDir: state, colorEnabled: true, nowUnix: nowFunc().Unix()}
	if got := renderVersion(nil, Segment{CheckUpdate: true}, env); got != "v0.4.6" {
		t.Errorf("got %q, want v0.4.6", got)
	}
	if called {
		t.Error("a cache freshly stamped by a failed refresh must not retrigger a spawn within the TTL")
	}
}

func TestWriteAndReadUpdateAttempt(t *testing.T) {
	dir := t.TempDir()
	restore := nowFunc
	nowFunc = func() time.Time { return time.Unix(1753660000, 0) }
	defer func() { nowFunc = restore }()

	if err := WriteUpdateAttempt(dir, BlockNotWritable); err != nil {
		t.Fatalf("WriteUpdateAttempt: %v", err)
	}
	got, ok := ReadUpdateAttempt(dir)
	if !ok {
		t.Fatal("ReadUpdateAttempt: ok = false, want true")
	}
	if got.Unix != 1753660000 {
		t.Errorf("Unix = %d, want 1753660000", got.Unix)
	}
	if got.Blocked != BlockNotWritable {
		t.Errorf("Blocked = %q, want %q", got.Blocked, BlockNotWritable)
	}
}

func TestReadUpdateAttemptMissing(t *testing.T) {
	if _, ok := ReadUpdateAttempt(t.TempDir()); ok {
		t.Error("ok = true for a missing file, want false")
	}
}

func TestReadUpdateAttemptMalformed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(UpdateAttemptPath(dir), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadUpdateAttempt(dir); ok {
		t.Error("ok = true for a malformed file, want false")
	}
}

func TestWriteUpdateAttemptSuccessClearsBlock(t *testing.T) {
	dir := t.TempDir()
	if err := WriteUpdateAttempt(dir, BlockNotWritable); err != nil {
		t.Fatal(err)
	}
	if err := WriteUpdateAttempt(dir, BlockNone); err != nil {
		t.Fatal(err)
	}
	got, ok := ReadUpdateAttempt(dir)
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if got.Blocked != BlockNone {
		t.Errorf("Blocked = %q, want empty", got.Blocked)
	}
}
