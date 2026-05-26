package statusline

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/fileutil"
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
	if err := fileutil.WriteAtomic(path, []byte(ver+"\n")); err != nil {
		return fmt.Errorf("statusline: %w", err)
	}
	return nil
}
