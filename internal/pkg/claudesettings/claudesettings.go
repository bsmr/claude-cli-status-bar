// Package claudesettings reads and writes ~/.claude/settings.json while
// preserving unknown top-level keys.
//
// Values are kept as json.RawMessage so any nested shape (objects, arrays,
// scalars) round-trips exactly. Top-level key order is not preserved (Go
// maps are unordered) - the file is rewritten with two-space indentation
// and a trailing newline. Writes are atomic via temp file + rename.
package claudesettings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/fileutil"
)

// Settings is the parsed top-level object of settings.json.
type Settings map[string]json.RawMessage

const statusLineKey = "statusLine"

// DefaultPath returns ~/.claude/settings.json or "" if home is empty.
func DefaultPath(home string) string {
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".claude", "settings.json")
}

// Load reads settings from path. A missing file returns empty Settings and
// no error so callers can treat absence as "no overrides set".
func Load(path string) (Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Settings{}, nil
		}
		return nil, fmt.Errorf("claudesettings: read %s: %w", path, err)
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("claudesettings: parse %s: %w", path, err)
	}
	if s == nil {
		s = Settings{}
	}
	return s, nil
}

// Save writes s to path with an atomic rename. The parent directory is
// created with 0700, the file with 0600 - settings.json may carry secrets.
func Save(path string, s Settings) error {
	if path == "" {
		return errors.New("claudesettings: empty path")
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("claudesettings: marshal: %w", err)
	}
	data = append(data, '\n')
	if err := fileutil.WriteAtomic(path, data); err != nil {
		return fmt.Errorf("claudesettings: %w", err)
	}
	return nil
}

// GetStatusLine returns the current "statusLine" value, if any.
func GetStatusLine(s Settings) (json.RawMessage, bool) {
	v, ok := s[statusLineKey]
	return v, ok
}

// SetStatusLine sets the "statusLine" value, overwriting any existing one.
func SetStatusLine(s Settings, value json.RawMessage) {
	s[statusLineKey] = value
}

// RemoveStatusLine deletes the "statusLine" key, leaving the rest untouched.
func RemoveStatusLine(s Settings) {
	delete(s, statusLineKey)
}
