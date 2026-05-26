package statusline

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// schemaVersionStateFile is the basename of the state file that
// remembers the last seen `schema_version` value. It lives next to
// the captures/ directory under $XDG_STATE_HOME/ccsb/, so its
// presence travels with the rest of ccsb's per-host state.
const schemaVersionStateFile = "schema_version"

// schemaVersionStatePath returns the absolute path of the state file
// for the given capture directory. Empty captureDir → empty result
// (callers should treat that as "tracking disabled").
func schemaVersionStatePath(captureDir string) string {
	if captureDir == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(captureDir), schemaVersionStateFile)
}

// loadSchemaVersion reads the persisted last-seen schema_version
// value from path. A missing or unreadable file is reported as the
// empty string so callers can treat both "no history" and
// "first-seen" identically without distinguishing error cases.
// Trailing whitespace and newlines are trimmed.
func loadSchemaVersion(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// saveSchemaVersion writes ver to path atomically (temp + rename).
// The parent directory is created with 0o700 and the file with 0o600
// — consistent with the rest of ccsb's per-user state (config,
// captures).
func saveSchemaVersion(path, ver string) error {
	if path == "" {
		return errors.New("statusline: schema-version path empty")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("statusline: mkdir state dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".schema_version-*.tmp")
	if err != nil {
		return fmt.Errorf("statusline: create temp: %w", err)
	}
	tmpPath := tmp.Name()
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("statusline: chmod: %w", err)
	}
	if _, err := tmp.WriteString(ver + "\n"); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("statusline: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("statusline: close temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("statusline: rename: %w", err)
	}
	return nil
}
