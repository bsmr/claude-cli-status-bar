// Package config persists ccsb's user-editable configuration as JSON at
// $XDG_CONFIG_HOME/ccsb/config.json (with $HOME/.config/ccsb fallback).
//
// The config holds four things:
//
//   - the proxied statusLine command and its arguments;
//   - a verbatim backup of the original "statusLine" value from
//     ~/.claude/settings.json, captured at install time so uninstall can
//     restore the same value (see Backup for what "same" means here);
//   - the renderer's segment/row layout;
//   - the self-update preferences.
//
// Any OTHER top-level key survives the round trip untouched (see
// Config.unknown), because the file is the user's and every rewrite here is
// a side effect of doing something else. Note the cost: a config carrying
// such a key comes back with its top-level keys alphabetised, since the
// merge goes through a map.
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
	"reflect"
	"strings"
	"time"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/fileutil"
	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/render"
)

// Config is the on-disk shape.
type Config struct {
	Proxy  Proxy         `json:"proxy,omitzero"`
	Backup Backup        `json:"backup,omitzero"`
	Render render.Config `json:"render,omitzero"`
	Update Update        `json:"update,omitzero"`

	// unknown holds the top-level keys this binary does not model, captured
	// by Load and written back by Save so that rewriting the file cannot
	// delete them. config.json belongs to the user, and `install`, `mode` and
	// `doctor` all rewrite it as a side effect of doing something else.
	//
	// The loss this prevents is version skew rather than exotic hand-editing:
	// an older ccsb running `ccsb mode native` over a config a newer one
	// wrote used to silently drop the block it had never heard of — this is
	// how a 0.4.14 binary would have eaten 0.4.15's proxy.timeout.
	//
	// It rides on the loaded value on purpose, rather than Save re-reading
	// the file: `ccsb config reset` builds a fresh Config carrying only the
	// uninstall backup, and must NOT resurrect what it just moved aside to
	// .bak. Unexported so no caller outside this package can forge it.
	unknown map[string]json.RawMessage
}

// modelledKeys returns the top-level JSON keys Config itself represents.
// Derived by reflection rather than listed, so adding a field to Config
// cannot leave a stale list behind and have Load file the new key as
// unknown — which would then shadow the struct's own value on Save.
func modelledKeys() []string {
	t := reflect.TypeFor[Config]()
	keys := make([]string, 0, t.NumField())
	for i := range t.NumField() {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			keys = append(keys, name)
		}
	}
	return keys
}

// Proxy describes the external statusLine implementation that ccsb forwards
// payloads to.
type Proxy struct {
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	// Timeout bounds the proxy child, as a Go duration string ("10s", "2m").
	// Empty or unparsable means DefaultProxyTimeout; an explicit "0" means no
	// limit at all — the pre-0.4.15 behaviour, for a proxy known to be slow.
	Timeout string `json:"timeout,omitempty"`
}

// DefaultProxyTimeout bounds a proxy child when the config does not say
// otherwise. Generous on purpose: the documented default proxy is
// `npx -y ccstatusline@latest`, and a cold npx run fetches from the registry
// before printing anything. The point is to make a hang finite, not to police
// a slow proxy.
const DefaultProxyTimeout = 10 * time.Second

// ProxyTimeout resolves Proxy.Timeout, falling back to DefaultProxyTimeout for
// an empty or unparsable value. A parsed zero (or negative) is returned as
// zero, which callers treat as "no limit" — that is an explicit opt-out and
// must not be silently turned back into the default.
func (p Proxy) ProxyTimeout() time.Duration {
	if p.Timeout == "" {
		return DefaultProxyTimeout
	}
	d, err := time.ParseDuration(p.Timeout)
	if err != nil {
		return DefaultProxyTimeout
	}
	if d < 0 {
		return 0
	}
	return d
}

// Backup holds state ccsb needs to undo its own changes.
type Backup struct {
	// PreviousStatusLine is the original "statusLine" JSON value found in
	// ~/.claude/settings.json before install. Stored as raw JSON so any
	// shape (string, object, missing) survives the round trip.
	//
	// The VALUE is preserved, including key order — but not the exact bytes,
	// and the difference has bitten before. Both writers marshal with
	// json.MarshalIndent, which re-indents an embedded json.RawMessage to its
	// new nesting depth, and Go's encoder HTML-escapes the three characters
	// & < > into their \u0026 \u003c \u003e forms. So a compact one-line
	// statusLine comes back pretty-printed, and `sh -c "a && b"` comes back
	// with the ampersands escaped. Both decode to the original value, so
	// nothing is lost — but compare such round trips as JSON, never with
	// bytes.Equal.
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
	// Second pass for the keys the struct does not model — see Config.unknown.
	// The unmarshal above already rejected anything that is not a JSON object,
	// so this one cannot fail on input the caller is allowed to see.
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err == nil {
		for _, k := range modelledKeys() {
			delete(all, k)
		}
		if len(all) > 0 {
			c.unknown = all
		}
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
	data, err := marshalConfig(c)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := fileutil.WriteAtomic(path, data); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	return nil
}

// marshalConfig renders c as the bytes Save writes, folding in any top-level
// keys Load captured that this binary does not model.
//
// Without such keys it marshals the struct directly, which keeps the field
// order stable (proxy, backup, render, update). The merged path has to go
// through a map, and Go sorts map keys, so a config carrying unmodelled keys
// comes back alphabetised — cosmetic, and the same caveat claudesettings
// documents. The struct's own keys win over the captured set, so an edit to a
// modelled field is never shadowed by the value it was loaded with.
func marshalConfig(c Config) ([]byte, error) {
	if len(c.unknown) == 0 {
		data, err := json.MarshalIndent(c, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("config: marshal: %w", err)
		}
		return data, nil
	}
	known, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("config: marshal: %w", err)
	}
	merged := make(map[string]json.RawMessage, len(c.unknown)+len(modelledKeys()))
	if err := json.Unmarshal(known, &merged); err != nil {
		return nil, fmt.Errorf("config: marshal: %w", err)
	}
	for k, v := range c.unknown {
		if _, taken := merged[k]; !taken {
			merged[k] = v
		}
	}
	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("config: marshal: %w", err)
	}
	return data, nil
}
