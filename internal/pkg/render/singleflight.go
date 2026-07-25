// Package render — see render.go for the package doc.
//
// singleflight.go holds the on-disk single-flight marker primitives shared
// by every out-of-band refresher in this package (git_dirty, version's
// update check): acquireLock/releaseLock, keyed on a caller-supplied path.
package render

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

// acquireLock reports whether the caller should proceed past the
// single-flight gate at path. It atomically creates path with
// O_CREATE|O_EXCL: across every process, exactly one caller wins the
// create and may proceed; all others see the existing marker and back
// off — this is what keeps concurrent renders/sessions down to ONE
// in-flight refresher instead of a pile of redundant work.
//
// A marker older than ttl is orphaned (its refresher died before
// clearing it) and is reclaimed. Every error path backs off (returns
// false): work that cannot be coordinated is skipped, never duplicated.
//
// ponytail: the reclaim branch is best-effort, NOT strict single-flight.
// Replacing an orphaned marker atomically is a compare-and-swap on a file,
// which POSIX does not offer without flock; any Remove/rename briefly
// empties the path and lets a peer's O_EXCL create win alongside this
// one. Two callers reclaiming the SAME orphan at the SAME instant can
// each proceed — bounded, self-healing, never incorrect (callers only
// ever produce idempotent refreshes). The steady-state create path above
// IS strict single-flight.
func acquireLock(path string, ttl time.Duration) bool {
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
	if info, err := os.Stat(path); err != nil || time.Since(info.ModTime()) < ttl {
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

// releaseLock clears the single-flight marker at path. The refresher calls
// it when finished so the next stale-cache render can start a new one; a
// missing marker (already reclaimed via the TTL) is fine.
func releaseLock(path string) {
	_ = os.Remove(path)
}
