// Package config persists ccsb's user-editable configuration as JSON at
// $XDG_CONFIG_HOME/ccsb/config.json (with $HOME/.config/ccsb fallback).
//
// The config holds four things:
//
//   - the proxied statusLine command and its arguments;
//   - a verbatim backup of the original "statusLine" value from
//     ~/.claude/settings.json, captured at install time so uninstall can
//     restore it byte-for-byte;
//   - the renderer's segment/row layout;
//   - the self-update preferences.
//
// Writes are atomic (temp file + rename); reads of a missing file return a
// zero Config and no error so callers can treat absence as default state.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/fileutil"
	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/render"
)

// Config is the on-disk shape.
type Config struct {
	Proxy  Proxy         `json:"proxy,omitzero"`
	Backup Backup        `json:"backup,omitzero"`
	Render render.Config `json:"render,omitzero"`
	Update Update        `json:"update,omitzero"`
}

// Proxy describes the external statusLine implementation that ccsb forwards
// payloads to.
type Proxy struct {
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
}

// Backup holds state ccsb needs to undo its own changes.
type Backup struct {
	// PreviousStatusLine is the original "statusLine" JSON value found in
	// ~/.claude/settings.json before install. Stored as raw JSON so any
	// shape (string, object, missing) round-trips exactly.
	PreviousStatusLine json.RawMessage `json:"previous_status_line,omitempty"`
}

// Update holds the self-update preferences.
type Update struct {
	// Auto is the highest jump the renderer may install without being
	// asked: "patch", "minor" or "major". Absent, empty or unrecognised
	// disables automatic updating entirely — opt-in is the whole point, so
	// the zero value must do nothing.
	Auto string `json:"auto,omitempty"`
}

// DefaultPath resolves the default config path.
//
// Precedence: xdgConfigHome, then home/.config. Returns "" when both are
// empty so callers can detect the missing-environment case.
func DefaultPath(home, xdgConfigHome string) string {
	base := xdgConfigHome
	if base == "" {
		if home == "" {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "ccsb", "config.json")
}

// Load reads the config from path. A missing file is reported as a zero
// Config with no error.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("config: read %s: %w", path, err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
	}
	return c, nil
}

// Save writes c to path with an atomic rename. The parent directory is
// created with 0700, the file with 0600 - the config may eventually contain
// the user's exact statusLine command, which can include private paths.
func Save(path string, c Config) error {
	if path == "" {
		return errors.New("config: empty path")
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	data = append(data, '\n')
	if err := fileutil.WriteAtomic(path, data); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	return nil
}
