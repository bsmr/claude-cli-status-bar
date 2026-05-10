// Package capture persists raw stdin payloads received from Claude Code so
// they can be inspected later in an editor.
//
// One file is written per invocation, named
//
//	<RFC3339Nano UTC timestamp>-<sanitized session id>.json
//
// inside the configured directory. Writes are atomic (temp file + rename) so
// readers never see partial content.
package capture

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const sessionPlaceholder = "unknown"

// Save writes payload to dir and returns the absolute path of the resulting
// file. dir is created with 0755 if missing. The session id is sanitised so
// path traversal and slashes cannot escape dir.
func Save(dir, sessionID string, payload []byte, now time.Time) (string, error) {
	if dir == "" {
		return "", errors.New("capture: empty dir")
	}
	if len(payload) == 0 {
		return "", errors.New("capture: empty payload")
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("capture: mkdir %s: %w", dir, err)
	}

	final := filepath.Join(dir, filename(sessionID, now))

	tmp, err := os.CreateTemp(dir, ".capture-*.tmp")
	if err != nil {
		return "", fmt.Errorf("capture: create temp: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("capture: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("capture: close temp: %w", err)
	}
	if err := os.Rename(tmpPath, final); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("capture: rename: %w", err)
	}
	return final, nil
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

func filename(sessionID string, now time.Time) string {
	ts := now.UTC().Format(time.RFC3339Nano)
	sid := sanitize(sessionID)
	if sid == "" {
		sid = sessionPlaceholder
	}
	return fmt.Sprintf("%s-%s.json", ts, sid)
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
