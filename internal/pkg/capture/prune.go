package capture

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Prune removes every capture file in dir whose basename timestamp lies
// before cutoff, and reports how many it removed. Passing time.Now() as the
// cutoff therefore empties the directory — every capture was written in the
// past — so "remove everything" needs no separate code path.
//
// Files whose name carries no parsable timestamp are never removed: the
// directory is the user's own state dir and may hold notes of their own.
// Subdirectories are left alone for the same reason.
//
// A missing directory is not an error — there is nothing to prune. The first
// file that cannot be removed aborts the sweep and is reported along with the
// count removed so far; re-running after fixing the cause resumes where it
// stopped, and in practice a permission failure applies to the whole
// directory anyway.
func Prune(dir string, cutoff time.Time) (int, error) {
	if dir == "" {
		return 0, errors.New("capture: empty dir")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("capture: read capture dir: %w", err)
	}
	var removed int
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		at, ok := TimeFromName(e.Name())
		if !ok || !at.Before(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			return removed, fmt.Errorf("capture: remove %s: %w", e.Name(), err)
		}
		removed++
	}
	return removed, nil
}

// TimeFromName parses the timestamp basename puts in front of the session id.
// The timestamp is always formatted in UTC, so it ends at the first "Z" in the
// name. Returns false for any name that does not carry one — callers treat
// those as foreign files and leave them alone.
//
// Two layouts are accepted. timeLayout is what ccsb writes since 0.4.20;
// RFC3339Nano is what it wrote before, and a directory that survived the
// upgrade is full of it. Dropping the old one would orphan every existing
// capture: unparsable names are skipped rather than pruned, so `ccsb captures
// clean` would never remove them and `ccsb doctor` would never pick the
// newest one.
//
// The fractional second is not fixed width in either layout (trailing zeros
// are trimmed), so names must be compared as time.Time rather than lexically.
func TimeFromName(name string) (time.Time, bool) {
	i := strings.IndexByte(name, 'Z')
	if i < 0 {
		return time.Time{}, false
	}
	stamp := name[:i+1]
	for _, layout := range []string{timeLayout, time.RFC3339Nano} {
		if t, err := time.Parse(layout, stamp); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
