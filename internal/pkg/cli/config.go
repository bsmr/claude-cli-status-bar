package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"
	"time"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/config"
	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/selfupdate"
)

// rescueBackup extracts the backup block from a config file that
// config.Load could not parse, by decoding only the top level and then
// only that one key. Everything else is left to fail as it should.
//
// It exists because backup.previous_status_line is not a setting: it is
// the only copy of the statusLine ccsb displaced at install time. A parse
// error anywhere else in the file — render.palette given as an object is
// the documented case — used to take it down with the rest, and the next
// uninstall then deleted the user's statusLine while reporting success.
// Returns a zero Backup when the file is unreadable, is not even a JSON
// object, or carries no usable backup key.
func rescueBackup(path string) config.Backup {
	raw, err := os.ReadFile(path)
	if err != nil {
		return config.Backup{}
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return config.Backup{}
	}
	var b config.Backup
	if err := json.Unmarshal(top["backup"], &b); err != nil {
		return config.Backup{}
	}
	return b
}

// runConfig dispatches the `ccsb config <verb>` subcommand. The function
// intentionally returns a hard error rather than printing help, so a typo in
// a script does not silently succeed.
func runConfig(p Paths, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("ccsb: config requires a subcommand (reset, auto)")
	}
	switch args[0] {
	case "reset":
		if len(args) > 1 {
			return fmt.Errorf("ccsb: config reset takes no arguments, got %d", len(args)-1)
		}
		return runConfigReset(p, stdout)
	case "auto":
		return runConfigAuto(p, args[1:], stdout)
	default:
		return fmt.Errorf("ccsb: unknown config subcommand %q (valid: reset, auto)", args[0])
	}
}

// autoLevels are the values update.auto accepts, in escalating order. "off"
// is CLI-only sugar for clearing the block and deliberately not a config
// value: config.Update's zero value has to mean "do nothing", since opt-in is
// the whole point of the feature.
var autoLevels = []string{"patch", "minor", "major"}

// runConfigAuto prints or sets update.auto.
//
// With no argument it prints the current level, or "off" when none is set —
// the same word that clears it, so whatever is printed can be typed back in.
// `ccsb mode` has the same property with native/proxy.
//
// With a level it saves, then reports whichever reason would keep the setting
// from ever firing: a rows block with no version segment setting
// check_update (0.4.17's finding), or a binary that cannot self-update at all.
// Both come from doctor's own diagnostics so there is no second copy of the
// wording to drift. The save happens either way — the note is information,
// not a veto, because the binary may be replaced by an installed release
// tomorrow and the config should already be right.
func runConfigAuto(p Paths, args []string, stdout io.Writer) error {
	if len(args) > 1 {
		return fmt.Errorf("ccsb: config auto takes at most one argument, got %d", len(args))
	}

	cfg, err := config.Load(p.Config)
	if err != nil {
		return err
	}

	if len(args) == 0 {
		level := cfg.Update.Auto
		if level == "" {
			level = "off"
		}
		fmt.Fprintln(stdout, level)
		return nil
	}

	if args[0] == "off" {
		// Null the struct rather than blanking the field: Update is tagged
		// omitzero, so this drops the key instead of leaving {"update":{}}.
		// Same move as `ccsb mode native` makes on the proxy block.
		cfg.Update = config.Update{}
		if err := config.Save(p.Config, cfg); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "ccsb: update.auto cleared")
		return nil
	}

	if !slices.Contains(autoLevels, args[0]) {
		return fmt.Errorf("ccsb: invalid auto level %q (valid: %s, off)",
			args[0], strings.Join(autoLevels, ", "))
	}

	cfg.Update.Auto = args[0]
	if err := config.Save(p.Config, cfg); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "ccsb: update.auto set to %q\n", args[0])

	if diag := inertAutoUpdateDiagnostic(cfg); diag != "" {
		fmt.Fprintf(stdout, "ccsb: note: %s\n", diag)
	}
	blocked := selfupdate.Guard(selfupdate.Options{StateDir: p.State, Self: p.Self})
	if diag := updateDiagnostic(blocked); diag != "" {
		fmt.Fprintf(stdout, "ccsb: note: %s — the setting is saved but cannot "+
			"take effect until ccsb runs from an installed release\n", diag)
	}
	return nil
}

// runConfigReset moves the existing ccsb config.json to a timestamped
// backup so the next invocation falls back to the in-code defaults.
// Missing-config is a friendly no-op — no backup file is created.
func runConfigReset(p Paths, stdout io.Writer) error {
	if p.Config == "" {
		return fmt.Errorf("ccsb: config path is empty")
	}
	if _, err := os.Stat(p.Config); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintf(stdout, "ccsb: no config at %s — defaults already apply\n", p.Config)
			return nil
		}
		return fmt.Errorf("ccsb: stat config: %w", err)
	}
	// backup.previous_status_line is not a setting: it is the only copy of
	// the statusLine ccsb displaced at install time and owes the user back on
	// uninstall. Reset restores default *settings*, so that value has to be
	// read out before the file moves and carried into the fresh config —
	// otherwise uninstall falls into its "no previous" branch and deletes the
	// user's statusLine instead of restoring it.
	// A load failure must not block the reset: a config too broken to parse
	// is exactly when the user needs it moved aside, and then there is simply
	// no backup to carry over. The unreadable original stays recoverable in
	// the timestamped .bak file either way.
	cfg, loadErr := config.Load(p.Config)

	// A file too broken to parse still has to give up the uninstall
	// backup — see rescueBackup. Read it before the rename, while the
	// path still holds the original bytes.
	keep := cfg.Backup
	if loadErr != nil {
		keep = rescueBackup(p.Config)
	}

	// RFC3339Nano makes the timestamp sortable and unambiguous; UTC
	// removes the local-timezone surprise when reading backups later.
	backupPath := p.Config + ".bak." + time.Now().UTC().Format(time.RFC3339Nano)
	if err := os.Rename(p.Config, backupPath); err != nil {
		return fmt.Errorf("ccsb: backup config: %w", err)
	}
	if len(keep.PreviousStatusLine) > 0 {
		if err := config.Save(p.Config, config.Config{Backup: keep}); err != nil {
			return fmt.Errorf("ccsb: preserve uninstall backup: %w", err)
		}
	}
	fmt.Fprintf(stdout, "ccsb: backed up previous config to %s\n", backupPath)
	fmt.Fprintf(stdout, "ccsb: config reset; defaults will apply on next invocation\n")
	return nil
}
