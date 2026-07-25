// Package cli is ccsb's command dispatcher.
//
// Without arguments it runs the proxy/fallback statusLine flow used by
// Claude Code. With a subcommand it manages the install/uninstall/status of
// the ~/.claude/settings.json hook.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/capture"
	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/claudesettings"
	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/config"
	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/render"
	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/statusline"
)

// Paths bundles the filesystem locations ccsb needs at runtime. They are
// supplied by the caller (resolved from the environment) rather than read
// inside this package so tests can inject temporary paths.
type Paths struct {
	Settings        string // ~/.claude/settings.json
	Config          string // $XDG_CONFIG_HOME/ccsb/config.json
	Capture         string // $XDG_STATE_HOME/ccsb/captures
	State           string // $XDG_STATE_HOME/ccsb
	Self            string // absolute path of the running ccsb binary
	ClaudeSkillsDir string // ~/.claude/skills/
}

// Env carries the environment values used to derive Paths.
type Env struct {
	Home          string
	XDGConfigHome string
	XDGStateHome  string
	Self          string
}

// Flags carries environment-derived booleans that travel alongside Paths
// into Run. Keeping these separate from Paths preserves Paths' "filesystem
// locations" semantic.
type Flags struct {
	// NoColor disables ANSI emission in the native renderer. Resolved by
	// the caller from the NO_COLOR environment variable.
	NoColor bool
}

// ResolvePaths derives Paths from environment values.
func ResolvePaths(e Env) Paths {
	return Paths{
		Settings:        claudesettings.DefaultPath(e.Home),
		Config:          config.DefaultPath(e.Home, e.XDGConfigHome),
		Capture:         capture.DefaultDir(e.Home, e.XDGStateHome),
		State:           capture.StateBase(e.Home, e.XDGStateHome),
		Self:            e.Self,
		ClaudeSkillsDir: filepath.Join(e.Home, ".claude", "skills"),
	}
}

// NewFromOS reads ccsb's runtime configuration from the process environment:
// HOME, XDG_CONFIG_HOME, XDG_STATE_HOME for filesystem locations, NO_COLOR
// for ANSI suppression, and os.Executable for the self-path. Returns the
// resolved Paths and Flags ready to pass into Run. Kept here (not in main)
// so the wiring is unit-testable via os.Setenv in tests.
func NewFromOS() (Paths, Flags, error) {
	self, err := os.Executable()
	if err != nil {
		return Paths{}, Flags{}, fmt.Errorf("ccsb: resolve self: %w", err)
	}
	paths := ResolvePaths(Env{
		Home:          os.Getenv("HOME"),
		XDGConfigHome: os.Getenv("XDG_CONFIG_HOME"),
		XDGStateHome:  os.Getenv("XDG_STATE_HOME"),
		Self:          self,
	})
	flags := Flags{
		NoColor: os.Getenv("NO_COLOR") != "",
	}
	return paths, flags, nil
}

// UnknownSubcommandError is returned for unrecognised subcommands so callers
// can distinguish parse errors from genuine runtime failures via errors.As.
type UnknownSubcommandError struct {
	Name string
}

func (e *UnknownSubcommandError) Error() string {
	return fmt.Sprintf("ccsb: unknown subcommand %q (valid: install, uninstall, status, mode, config, captures, doctor, install-skill, uninstall-skill, version, help)", e.Name)
}

// Run dispatches based on args[0]. Without args, runs the proxy/fallback
// statusLine flow.
func Run(ctx context.Context, p Paths, f Flags, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return runProxy(ctx, p, f, stdin, stdout, stderr)
	}
	switch args[0] {
	case "-h", "--help", "help":
		printHelp(stdout)
		return nil
	case "-v", "--version", "version":
		fmt.Fprintf(stdout, "ccsb version %s\n", Version)
		return nil
	case "install":
		return runInstall(p, stdout)
	case "uninstall":
		return runUninstall(p, stdout)
	case "status":
		return runStatus(p, stdout)
	case "mode":
		return runMode(p, args[1:], stdout)
	case "config":
		return runConfig(p, args[1:], stdout)
	case "captures":
		return runCaptures(p, args[1:], stdout)
	case "doctor":
		return runDoctor(p, stdout)
	case "refresh-git-dirty":
		return runRefreshGitDirty(p, args[1:])
	case "install-skill":
		return runInstallSkill(p, stdout)
	case "uninstall-skill":
		return runUninstallSkill(p, stdout)
	default:
		return &UnknownSubcommandError{Name: args[0]}
	}
}

func runProxy(ctx context.Context, p Paths, f Flags, stdin io.Reader, stdout, stderr io.Writer) error {
	cfg, err := config.Load(p.Config)
	if err != nil {
		return err
	}
	return statusline.Run(ctx, statusline.Options{
		ProxyCommand: cfg.Proxy.Command,
		ProxyArgs:    cfg.Proxy.Args,
		CaptureDir:   p.Capture,
		StateDir:     p.State,
		Render:       cfg.Render,
		NoColor:      f.NoColor,
		Version:      Version,
	}, stdin, stdout, stderr)
}

// runRefreshGitDirty backs the hidden `refresh-git-dirty <dir>` subcommand.
// The renderer starts it detached when the git_dirty segment's cache has
// gone stale; it runs git out of band and writes the refreshed count for
// the NEXT render to pick up. Deliberately silent — nothing reads its
// output, and a failure must not surface anywhere near the status line.
func runRefreshGitDirty(p Paths, args []string) error {
	if len(args) == 0 || p.State == "" {
		return nil
	}
	_ = render.RefreshGitDirty(p.State, args[0])
	return nil
}

func runInstall(p Paths, stdout io.Writer) error {
	s, err := claudesettings.Load(p.Settings)
	if err != nil {
		return err
	}
	cfg, err := config.Load(p.Config)
	if err != nil {
		return err
	}

	alreadyHooked := pointsToSelf(s, p.Self)

	// Seed backup and proxy only on the first install: ccsb must not be
	// hooked yet (otherwise the "previous" line is ccsb itself) and no
	// backup may exist yet. Guarding on the empty backup rather than on
	// alreadyHooked alone keeps the documented "never overwrites an
	// existing backup" invariant even when settings.json has meanwhile
	// been re-pointed at some third command — uninstall must still restore
	// the statusLine ccsb originally displaced, not that intermediate one.
	// Proxy derivation stays inside the same guard on purpose: once the
	// backup exists the proxy block belongs to the user (`ccsb mode`,
	// `ccsb doctor`), and re-deriving it here would silently undo a
	// deliberate native-mode choice.
	if !alreadyHooked && len(cfg.Backup.PreviousStatusLine) == 0 {
		if existing, ok := claudesettings.GetStatusLine(s); ok {
			cfg.Backup.PreviousStatusLine = existing
			// When the previous statusLine is the canonical ccstatusline
			// default, leave the proxy block empty so install lands in
			// native mode. The backup is still preserved for uninstall.
			if !isCanonicalCcstatusline(existing) {
				if cmd, args, ok := extractCommand(existing); ok {
					// Don't proxy another ccsb binary — use native mode instead.
					if filepath.Base(cmd) != filepath.Base(p.Self) {
						cfg.Proxy.Command = cmd
						cfg.Proxy.Args = args
					}
				}
			}
		}
	}

	sl, err := json.Marshal(map[string]string{
		"type":    "command",
		"command": p.Self,
	})
	if err != nil {
		return fmt.Errorf("ccsb: marshal statusLine: %w", err)
	}
	claudesettings.SetStatusLine(s, sl)

	if err := claudesettings.Save(p.Settings, s); err != nil {
		return err
	}
	if err := config.Save(p.Config, cfg); err != nil {
		return err
	}

	if alreadyHooked {
		fmt.Fprintln(stdout, "ccsb: already installed; backup preserved")
	} else {
		fmt.Fprintln(stdout, "ccsb: installed")
	}
	return nil
}

func runUninstall(p Paths, stdout io.Writer) error {
	cfg, err := config.Load(p.Config)
	if err != nil {
		return err
	}
	s, err := claudesettings.Load(p.Settings)
	if err != nil {
		return err
	}

	if !pointsToSelf(s, p.Self) {
		return errors.New("ccsb: not installed (settings.json statusLine does not point at this binary)")
	}

	if len(cfg.Backup.PreviousStatusLine) > 0 {
		claudesettings.SetStatusLine(s, cfg.Backup.PreviousStatusLine)
	} else {
		claudesettings.RemoveStatusLine(s)
	}
	cfg.Backup.PreviousStatusLine = nil

	if err := claudesettings.Save(p.Settings, s); err != nil {
		return err
	}
	if err := config.Save(p.Config, cfg); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "ccsb: uninstalled")
	return nil
}

func runStatus(p Paths, stdout io.Writer) error {
	cfg, err := config.Load(p.Config)
	if err != nil {
		return err
	}
	s, err := claudesettings.Load(p.Settings)
	if err != nil {
		return err
	}

	hooked := pointsToSelf(s, p.Self)
	fmt.Fprintf(stdout, "ccsb: hooked: %s\n", yesNo(hooked))
	fmt.Fprintf(stdout, "ccsb: self:     %s\n", p.Self)
	fmt.Fprintf(stdout, "ccsb: settings: %s\n", p.Settings)
	fmt.Fprintf(stdout, "ccsb: config:   %s\n", p.Config)
	fmt.Fprintf(stdout, "ccsb: capture:  %s\n", p.Capture)
	fmt.Fprintf(stdout, "ccsb: mode:     %s\n", currentMode(cfg))
	if cfg.Proxy.Command != "" {
		args := strings.Join(cfg.Proxy.Args, " ")
		if issue := proxyIssue(cfg.Proxy.Command, p.Self); issue != "" {
			fmt.Fprintf(stdout, "ccsb: proxy:    %s %s  [WARNING: %s]\n", cfg.Proxy.Command, args, issue)
		} else {
			fmt.Fprintf(stdout, "ccsb: proxy:    %s %s\n", cfg.Proxy.Command, args)
		}
	} else {
		fmt.Fprintln(stdout, "ccsb: proxy:    (none — built-in fallback)")
	}
	if len(cfg.Backup.PreviousStatusLine) > 0 {
		fmt.Fprintf(stdout, "ccsb: backup:   %s\n", string(cfg.Backup.PreviousStatusLine))
	} else {
		fmt.Fprintln(stdout, "ccsb: backup:   (none)")
	}
	return nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func printHelp(w io.Writer) {
	const help = `ccsb - Claude Code statusLine provider

Without arguments, ccsb reads the JSON payload from stdin and renders the
configured statusLine (proxy or built-in fallback). Claude Code invokes it
via the "statusLine.command" entry in ~/.claude/settings.json.

Subcommands:
  install     Save the current statusLine into ccsb's config and replace it
              in settings.json with this binary so Claude Code calls ccsb.
  uninstall   Restore the previous statusLine from the saved backup.
  status      Print whether settings.json points at ccsb and show the
              current proxy/backup state.
  mode        Print the current mode (native or proxy) when invoked with no
              argument. With "native", clear the proxy block; with "proxy",
              set it to "npx -y ccstatusline@latest" by default or to the
              given command and arguments.
  config      "config reset" moves the existing config.json aside (as a
              timestamped .bak file) so the next run picks up the in-code
              defaults. The only verb today; a no-op when no config exists.
  captures    "captures clean" removes capture files (.json payloads, .out
              rendered output, plus any .err and .diag siblings).
              Without arguments it empties the capture directory; with
              --older-than <duration> it keeps anything newer (e.g. 7d, 24h).
              Captures are diagnostic only and safe to remove at any time.
  doctor      Diagnose and auto-fix configuration problems: re-installs if
              settings.json is not hooked; switches to native mode if the
              proxy command is circular, another ccsb binary, or missing.
  install-skill   Extract the ccsb-wizard Claude Code skill to
              ~/.claude/skills/ccsb-wizard.md. Run /ccsb-wizard inside
              Claude Code to start an AI-guided configuration dialogue.
              Re-run after updating ccsb to get the latest skill version.
  uninstall-skill Remove ccsb-wizard.md from ~/.claude/skills/.
  version     Print the ccsb version. Aliases: -v, --version.
  help        Print this message. Aliases: -h, --help.
`
	fmt.Fprint(w, help)
}

// statusLineCommand is the {"type":"command","command":"<cmd> <args...>"}
// shape of a statusLine entry in ~/.claude/settings.json that Claude Code
// invokes as an external command. It is the only shape ccsb produces and
// the only one install/uninstall/status need to introspect; richer shapes
// (e.g. inline scripts) are out of scope.
type statusLineCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// parseStatusLineCommand decodes raw into a statusLineCommand. The bool
// reports whether raw was a well-formed type=command entry with a
// non-empty Command — any other shape (parse error, wrong type, blank
// command) returns ok=false so callers can treat them uniformly.
func parseStatusLineCommand(raw json.RawMessage) (statusLineCommand, bool) {
	var obj statusLineCommand
	if err := json.Unmarshal(raw, &obj); err != nil {
		return statusLineCommand{}, false
	}
	if obj.Type != "command" || obj.Command == "" {
		return statusLineCommand{}, false
	}
	return obj, true
}

// pointsToSelf reports whether the current statusLine in s is a
// {"type":"command","command":selfPath} entry.
func pointsToSelf(s claudesettings.Settings, selfPath string) bool {
	sl, ok := claudesettings.GetStatusLine(s)
	if !ok {
		return false
	}
	obj, ok := parseStatusLineCommand(sl)
	return ok && obj.Command == selfPath
}

// extractCommand parses a statusLine value of the form
// {"type":"command","command":"<cmd> <args...>"} and returns the first
// whitespace-separated token as the command and the rest as args. Returns
// ok=false if the input does not match. Quoted arguments are not handled —
// users with quoted commands must edit ccsb's config directly.
func extractCommand(raw json.RawMessage) (cmd string, args []string, ok bool) {
	obj, ok := parseStatusLineCommand(raw)
	if !ok {
		return "", nil, false
	}
	fields := strings.Fields(obj.Command)
	if len(fields) == 0 {
		return "", nil, false
	}
	return fields[0], fields[1:], true
}

// canonicalCcstatusline is the documented npx invocation that bootstraps
// ccstatusline from npm — the shape Claude Code's quick-start instructions
// produce. install treats it as a sentinel and defaults to native mode
// instead of seeding it into proxy mode.
const canonicalCcstatusline = "npx -y ccstatusline@latest"

// isCanonicalCcstatusline reports whether raw is a statusLine entry whose
// command string is exactly the canonical ccstatusline invocation. Extra
// top-level fields (e.g. "padding") are ignored.
func isCanonicalCcstatusline(raw json.RawMessage) bool {
	obj, ok := parseStatusLineCommand(raw)
	return ok && strings.TrimSpace(obj.Command) == canonicalCcstatusline
}
