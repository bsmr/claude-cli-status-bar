// Package capture persists the raw payloads and rendered output of each ccsb
// invocation so they can be inspected later in an editor.
//
// Each invocation produces files that share a common basename:
//
//	<UTC timestamp>-<sanitized session id>
//
// with extensions per kind: ".json" for the stdin payload, ".out" for the
// rendered statusLine bytes, ".err" for any stderr the proxy emitted. Pass
// the same time.Time to Save and SaveOutput so paired files share a basename.
//
// The timestamp is RFC3339Nano with '-' in place of ':' (see timeLayout) and
// the id is length-bounded (see maxSessionIDLen); before 0.4.20 it was plain
// RFC3339Nano with no bound, and both omissions made every write for an
// invocation fail — on Windows always, and on any platform for an
// overlong id. TimeFromName still reads the old shape so a capture
// directory that predates the change stays prunable.
//
// All writes are atomic (temp file + rename) so readers never see partial
// content.
package capture

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/fileutil"
)

const sessionPlaceholder = "unknown"

// timeLayout stamps the capture basename. It is RFC3339Nano with the time
// separator changed from ':' to '-', because ':' cannot appear in a filename
// on NTFS — README advertises Windows and GoReleaser builds it, so the
// original layout made every capture write fail there, silently: the bar
// still renders and exits 0, only the files never appear.
//
// The trailing Z is a literal (Go only reads "Z07:00"-shaped tokens as a zone),
// which is correct because the stamp is always formatted in UTC. The fractional
// second keeps RFC3339Nano's trailing-zero trimming, so it is NOT fixed width
// and names must still be ordered by parsed time rather than lexically.
const timeLayout = "2006-01-02T15-04-05.999999999Z"

// maxSessionIDLen bounds the sanitized session id in a basename. sanitize
// already restricts the character set, but a hostile or buggy producer can
// still send an arbitrarily long id: past NAME_MAX (255 on ext4) every capture
// write failed with ENAMETOOLONG and one stderr line, killing the feature for
// that session while the bar carried on.
//
// 64 is generous — Claude Code sends a 36-char UUID — while leaving the atomic
// write room for its ".<basename>.<ext>-<random>.tmp", which needs roughly 21
// bytes on top of the final name.
const maxSessionIDLen = 64

// Save writes payload to dir as <basename>.json. dir is created with 0o700 if
// missing and the file lands as 0o600 — captures are user-private state. The
// session id is sanitised so path traversal and slashes cannot escape dir.
func Save(dir, sessionID string, payload []byte, now time.Time) (string, error) {
	if len(payload) == 0 {
		return "", errors.New("capture: empty payload")
	}
	return writeFile(dir, basename(sessionID, now)+".json", payload)
}

// SaveOutput writes data to dir as <basename>.<suffix> where suffix is the
// extension without a leading dot (typically "out" for stdout, "err" for
// stderr). Pass the same now as the matching Save call so the two files
// share a basename and can be paired by readers.
func SaveOutput(dir, sessionID string, data []byte, now time.Time, suffix string) (string, error) {
	if suffix == "" {
		return "", errors.New("capture: empty suffix")
	}
	if len(data) == 0 {
		return "", errors.New("capture: empty data")
	}
	return writeFile(dir, basename(sessionID, now)+"."+suffix, data)
}

// DefaultDir resolves the default capture directory.
//
// Precedence: xdgStateHome, then home/.local/state. Returns "" if both are
// empty so callers can detect the missing-environment case explicitly.
func DefaultDir(home, xdgStateHome string) string {
	base := StateBase(home, xdgStateHome)
	if base == "" {
		return ""
	}
	return filepath.Join(base, "captures")
}

// StateBase resolves ccsb's state directory — the parent of the capture
// directory, shared with any other on-disk state ccsb keeps. Same
// precedence and same empty-means-unresolvable contract as DefaultDir.
func StateBase(home, xdgStateHome string) string {
	base := xdgStateHome
	if base == "" {
		if home == "" {
			return ""
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "ccsb")
}

// basename returns "<UTC timestamp>-<sanitized session id>" — the prefix
// shared by every file written for one invocation. See timeLayout for the
// stamp and sanitize for what happens to the id.
func basename(sessionID string, now time.Time) string {
	ts := now.UTC().Format(timeLayout)
	sid := sanitize(sessionID)
	if sid == "" {
		sid = sessionPlaceholder
	}
	return fmt.Sprintf("%s-%s", ts, sid)
}

// writeFile writes data atomically to dir/name and returns the absolute path.
// The parent directory is created 0o700 and the file lands as 0o600 — captures
// contain session_id, cwd, and (post-render) the rendered statusLine bytes
// which can include cost figures, so they are treated as user-private state.
func writeFile(dir, name string, data []byte) (string, error) {
	if dir == "" {
		return "", errors.New("capture: empty dir")
	}
	final := filepath.Join(dir, name)
	if err := fileutil.WriteAtomic(final, data); err != nil {
		return "", fmt.Errorf("capture: %w", err)
	}
	return final, nil
}

// sanitize keeps ASCII letters, digits, '-' and '_'. Anything else becomes '_'
// so a hostile session id cannot escape the target directory or include
// shell-hostile characters, and the result is truncated to maxSessionIDLen so
// it cannot overflow NAME_MAX either.
//
// Truncation can make two ids that differ only past the cut share a basename,
// in which case the later write wins. That is accepted: captures are
// diagnostic state, no real producer sends ids that long, and losing one
// capture beats losing every capture in the session to ENAMETOOLONG.
//
// Truncating by bytes is safe here because every rune this function emits is
// single-byte ASCII, whatever came in.
func sanitize(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if len(out) > maxSessionIDLen {
		out = out[:maxSessionIDLen]
	}
	return out
}
