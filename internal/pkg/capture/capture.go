// Package capture persists the raw payloads and rendered output of each ccsb
// invocation so they can be inspected later in an editor.
//
// Each invocation produces files that share a common basename:
//
//	<RFC3339Nano UTC timestamp>-<sanitized session id>
//
// with extensions per kind: ".json" for the stdin payload, ".out" for the
// rendered statusLine bytes, ".err" for any stderr the proxy emitted. Pass
// the same time.Time to Save and SaveOutput so paired files share a basename.
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

// Save writes payload to dir as <basename>.json. dir is created with 0755 if
// missing. The session id is sanitised so path traversal and slashes cannot
// escape dir.
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
	base := xdgStateHome
	if base == "" {
		if home == "" {
			return ""
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "ccsb", "captures")
}

// basename returns "<RFC3339Nano UTC>-<sanitized session id>" — the prefix
// shared by every file written for one invocation.
func basename(sessionID string, now time.Time) string {
	ts := now.UTC().Format(time.RFC3339Nano)
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
// shell-hostile characters.
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
	return b.String()
}
