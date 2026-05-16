package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/claudesettings"
	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/config"
)

// proxyIssue returns a non-empty human-readable description when cmd is a
// problematic proxy target relative to self. The three cases are: cmd equals
// self (circular loop), cmd has the same base name as self (another ccsb
// binary), and cmd does not exist on disk.
func proxyIssue(cmd, self string) string {
	switch {
	case cmd == self:
		return "proxy points to self (circular)"
	case filepath.Base(cmd) == filepath.Base(self):
		return "proxy appears to be another ccsb binary"
	default:
		if _, err := os.Stat(cmd); errors.Is(err, fs.ErrNotExist) {
			return "proxy target not found on disk"
		}
		return ""
	}
}

// runDoctor diagnoses common configuration problems and fixes them
// automatically:
//
//   - settings.json not hooked → installs ccsb
//   - proxy command is circular, another ccsb, or missing → native mode
func runDoctor(p Paths, stdout io.Writer) error {
	fixed := 0

	s, err := claudesettings.Load(p.Settings)
	if err != nil {
		return err
	}
	if !pointsToSelf(s, p.Self) {
		fmt.Fprintln(stdout, "ccsb: doctor: settings.json not hooked — installing")
		if err := runInstall(p, stdout); err != nil {
			return err
		}
		fixed++
	}

	cfg, err := config.Load(p.Config)
	if err != nil {
		return err
	}
	if cmd := cfg.Proxy.Command; cmd != "" {
		if issue := proxyIssue(cmd, p.Self); issue != "" {
			fmt.Fprintf(stdout, "ccsb: doctor: %s — switching to native mode\n", issue)
			cfg.Proxy.Command = ""
			cfg.Proxy.Args = nil
			if err := config.Save(p.Config, cfg); err != nil {
				return err
			}
			fixed++
		}
	}

	if fixed == 0 {
		fmt.Fprintln(stdout, "ccsb: doctor: no issues found")
	} else {
		fmt.Fprintf(stdout, "ccsb: doctor: fixed %d issue(s)\n", fixed)
	}
	return nil
}
