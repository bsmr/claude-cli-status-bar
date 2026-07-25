package render

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/fileutil"
)

const (
	// defaultUpdateCheckInterval is how often the version segment's update
	// check re-runs when Segment.UpdateCheckInterval is empty or invalid.
	// Releases don't happen more than a few times a day even during a
	// burst, so a check this coarse never misses one for long.
	defaultUpdateCheckInterval = 24 * time.Hour

	// updateCheckTimeout bounds the HTTP call in the background refresher.
	// It never delays a render — it only stops a hung network call from
	// leaving a refresher running indefinitely.
	updateCheckTimeout = 5 * time.Second

	// updateCheckLockTTL bounds how long the single-flight marker
	// suppresses new refreshers; must exceed updateCheckTimeout so a
	// legitimately-running refresh is never pre-empted (mirrors
	// gitdirty.go's refreshLockTTL).
	updateCheckLockTTL = updateCheckTimeout + 5*time.Second

	// githubLatestReleaseURL is the public release channel this project
	// actually publishes to (GoReleaser + GitHub Actions on v*.*.* tags).
	// Not user-configurable — there is exactly one ccsb release stream.
	githubLatestReleaseURL = "https://api.github.com/repos/bsmr/claude-cli-status-bar/releases/latest"

	// maxUpdateCheckResponseBytes bounds how much of the HTTP response body
	// fetchLatestTag will read. 1 MiB is generously larger than any real
	// GitHub releases-API JSON payload — this is ccsb's only code path that
	// decodes data from a third party over the network, so it gets a bound.
	maxUpdateCheckResponseBytes = 1 << 20
)

// updateCheckURL is the endpoint RefreshUpdateCheck queries. A package var
// (not the constant directly) purely so tests can redirect it to an
// httptest.Server instead of depending on real network access; production
// code never reassigns it.
var updateCheckURL = githubLatestReleaseURL

// updateCache is the on-disk shape of the cached latest-release check.
type updateCache struct {
	LatestTag string `json:"latest_tag"`
	Unix      int64  `json:"unix"`
}

// UpdateCachePath returns the cache file backing the version segment's
// update check. Global (unlike DirtyCachePath) — there is exactly one
// ccsb release stream to track, not one per repository.
func UpdateCachePath(stateDir string) string {
	return filepath.Join(stateDir, "update-check.json")
}

// updateLockPath returns the single-flight marker guarding the update
// check refresh — a sibling of the cache entry.
func updateLockPath(stateDir string) string {
	return UpdateCachePath(stateDir) + ".pending"
}

// readUpdateCache loads the cached latest-release check. ok is false for a
// missing, unreadable, or malformed file — every one of which simply means
// "no cached answer yet".
func readUpdateCache(path string) (updateCache, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return updateCache{}, false
	}
	var c updateCache
	if err := json.Unmarshal(raw, &c); err != nil {
		return updateCache{}, false
	}
	return c, true
}

// parseUpdateCheckInterval parses a Segment.UpdateCheckInterval value as a
// Go duration. Empty, unparsable, or non-positive falls back to
// defaultUpdateCheckInterval.
func parseUpdateCheckInterval(v string) time.Duration {
	if v == "" {
		return defaultUpdateCheckInterval
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return defaultUpdateCheckInterval
	}
	return d
}

// semver is a parsed X.Y.Z version, used to compare the running ccsb
// version against the latest GitHub release tag.
type semver struct {
	major, minor, patch int
}

// parseSemver parses "vX.Y.Z" or "X.Y.Z" into a semver. Any other shape
// (missing component, non-numeric component, extra suffix like "-rc1")
// reports ok=false — an update comparison that can't be trusted is
// treated as "no update to show", never guessed at.
func parseSemver(s string) (semver, bool) {
	s = strings.TrimPrefix(s, "v")
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	nums := make([]int, 3)
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return semver{}, false
		}
		nums[i] = n
	}
	return semver{major: nums[0], minor: nums[1], patch: nums[2]}, true
}

// updateSeverity classifies how far the latest release is ahead of the
// running version, driving both the glyph and the color the version
// segment renders.
type updateSeverity int

const (
	updateNone     updateSeverity = iota // not newer, or unparsable
	updatePatch                          // same major.minor, newer patch
	updateMinor                          // same major, newer minor
	updateMajor                          // exactly one major version ahead
	updateMajorFar                       // two or more major versions ahead
)

// compareSeverity classifies latest relative to current. A latest that is
// equal to or older than current is updateNone.
func compareSeverity(current, latest semver) updateSeverity {
	switch {
	case latest.major > current.major:
		if latest.major-current.major >= 2 {
			return updateMajorFar
		}
		return updateMajor
	case latest.major < current.major:
		return updateNone
	case latest.minor > current.minor:
		return updateMinor
	case latest.minor < current.minor:
		return updateNone
	case latest.patch > current.patch:
		return updatePatch
	default:
		return updateNone
	}
}

// fetchLatestTag fetches the tag_name of the latest release from the
// GitHub Releases API at url. Requires a User-Agent header — GitHub
// rejects unauthenticated requests without one (403).
func fetchLatestTag(ctx context.Context, url string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, updateCheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "ccsb")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("update check: unexpected status %d", resp.StatusCode)
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	bounded := io.LimitReader(resp.Body, maxUpdateCheckResponseBytes)
	if err := json.NewDecoder(bounded).Decode(&body); err != nil {
		return "", fmt.Errorf("update check: decode response: %w", err)
	}
	if body.TagName == "" {
		return "", errors.New("update check: empty tag_name")
	}
	return body.TagName, nil
}

// RefreshUpdateCheck fetches the latest ccsb release tag and writes it to
// the cache the version segment reads. It is the body of the hidden
// `ccsb refresh-update-check` subcommand, which the renderer starts in the
// background — this is the only code path in ccsb that calls the GitHub
// API, and it never runs inside a render.
//
// A fetch error still stamps the cache with a fresh timestamp (preserving
// whatever tag was previously cached, or "" if there was none) before
// returning. Without this, "no cache" and "stale cache" are the same
// retry branch in renderUpdateSuffix, and a fetch that fails forever (no
// network, rate-limited) would never produce a cache entry to age out of
// freshness — spawning a new refresher on every single render instead of
// at most once per Segment.UpdateCheckInterval. An empty LatestTag is
// handled gracefully downstream: parseSemver("") fails, so
// renderUpdateSuffix shows nothing, exactly like "no cache yet".
func RefreshUpdateCheck(stateDir string) error {
	// Clear the single-flight marker on the way out — whether the fetch
	// succeeds or fails — so the next stale render is not blocked until
	// the marker's TTL expires.
	defer releaseLock(updateLockPath(stateDir))

	tag, err := fetchLatestTag(context.Background(), updateCheckURL)
	if err != nil {
		prev, _ := readUpdateCache(UpdateCachePath(stateDir))
		if blob, marshalErr := json.Marshal(updateCache{LatestTag: prev.LatestTag, Unix: nowFunc().Unix()}); marshalErr == nil {
			_ = fileutil.WriteAtomic(UpdateCachePath(stateDir), blob)
		}
		return err
	}
	blob, err := json.Marshal(updateCache{LatestTag: tag, Unix: nowFunc().Unix()})
	if err != nil {
		return err
	}
	return fileutil.WriteAtomic(UpdateCachePath(stateDir), blob)
}
