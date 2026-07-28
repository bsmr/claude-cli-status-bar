package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/config"
)

// runConfig dispatches the `ccsb config <verb>` subcommand. Only verb so
// far is "reset"; the function intentionally returns a hard error rather
// than printing help, so a typo in a script does not silently succeed.
func runConfig(p Paths, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("ccsb: config requires a subcommand (reset)")
	}
	switch args[0] {
	case "reset":
		if len(args) > 1 {
			return fmt.Errorf("ccsb: config reset takes no arguments, got %d", len(args)-1)
		}
		return runConfigReset(p, stdout)
	default:
		return fmt.Errorf("ccsb: unknown config subcommand %q (valid: reset)", args[0])
	}
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

	// RFC3339Nano makes the timestamp sortable and unambiguous; UTC
	// removes the local-timezone surprise when reading backups later.
	backupPath := p.Config + ".bak." + time.Now().UTC().Format(time.RFC3339Nano)
	if err := os.Rename(p.Config, backupPath); err != nil {
		return fmt.Errorf("ccsb: backup config: %w", err)
	}
	if loadErr == nil && len(cfg.Backup.PreviousStatusLine) > 0 {
		if err := config.Save(p.Config, config.Config{Backup: cfg.Backup}); err != nil {
			return fmt.Errorf("ccsb: preserve uninstall backup: %w", err)
		}
	}
	fmt.Fprintf(stdout, "ccsb: backed up previous config to %s\n", backupPath)
	fmt.Fprintf(stdout, "ccsb: config reset; defaults will apply on next invocation\n")
	return nil
}
