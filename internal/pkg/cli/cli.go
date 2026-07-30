// Package cli is ccsb's command dispatcher.
//
// Without arguments it runs the proxy/fallback statusLine flow used by
// Claude Code, unless stdin is a terminal, in which case it prints usage
// instead. With a subcommand it manages the install/uninstall/status of
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
	// StdinIsTTY reports whether stdin is a character device. Resolved by
	// the caller, like NoColor, so Run stays testable without a
	// pseudo-terminal. Claude Code always pipes a payload, so it is only
	// ever true when a person invoked ccsb from a shell. It describes the
	// same stdin the caller hands to Run as the stdin io.Reader argument —
	// a caller passing a different reader would get an inconsistent pair.
	StdinIsTTY bool
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
		NoColor:    os.Getenv("NO_COLOR") != "",
		StdinIsTTY: stdinIsTTY(os.Stdin),
	}
	return paths, flags, nil
}

// stdinIsTTY reports whether f is a character device — a terminal, as far as
// ccsb needs to care. It takes an *os.File rather than reading os.Stdin
// directly so tests can pass a pipe or a regular file without a
// pseudo-terminal.
//
// Claude Code always pipes the payload, so this is only ever true when a
// person invoked ccsb from a shell. os.DevNull is a character device too, so
// a redirect from it counts as interactive; telling the two apart needs a
// real ioctl probe plus a Windows stub, and nothing invokes ccsb that way.
func stdinIsTTY(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// UnknownSubcommandError is returned for unrecognised subcommands so callers
// can distinguish parse errors from genuine runtime failures via errors.As.
type UnknownSubcommandError struct {
	Name string
}

func (e *UnknownSubcommandError) Error() string {
	return fmt.Sprintf("ccsb: unknown subcommand %q (valid: install, uninstall, status, mode, config, captures, doctor, update, install-skill, uninstall-skill, version, help)", e.Name)
}

// Run dispatches based on args[0]. Without args, runs the proxy/fallback
// statusLine flow.
func Run(ctx context.Context, p Paths, f Flags, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		// A terminal on stdin means a person typed "ccsb" to find out what
		// it does. Rendering would block in io.ReadAll until they guessed
		// Ctrl-D. Claude Code always pipes, so it never lands here.
		if f.StdinIsTTY {
			printInteractiveHint(stdout)
			printHelp(stdout)
			return nil
		}
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
	case "update":
		return runUpdate(ctx, p, stdout, stderr)
	case "refresh-git-dirty":
		return runRefreshGitDirty(p, args[1:])
	case "refresh-update-check":
		return runRefreshUpdateCheck(p)
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
		ProxyTimeout: cfg.Proxy.ProxyTimeout(),
		CaptureDir:   p.Capture,
		StateDir:     p.State,
		Render:       cfg.Render,
		NoColor:      f.NoColor,
		Version:      Version,
		AutoUpdate:   cfg.Update.Auto,
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

// runRefreshUpdateCheck backs the hidden `refresh-update-check` subcommand.
// The renderer starts it detached when the version segment's update-check
// cache has gone stale; it hits the GitHub API out of band and writes the
// refreshed tag for the NEXT render to pick up. Deliberately silent —
// nothing reads its output, and a failure must not surface anywhere near
// the status line.
func runRefreshUpdateCheck(p Paths) error {
	if p.State == "" {
		return nil
	}
	_ = render.RefreshUpdateCheck(p.State)
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
	// The flip side of that write-once invariant: when a backup already
	// exists but settings.json points somewhere else, the entry found here
	// is about to be overwritten and CANNOT be saved — keeping it would
	// destroy the original the uninstall owes back. It is still the user's
	// configuration, so name it instead of dropping it in silence. Reported
	// below, after both writes have succeeded.
	var discarded json.RawMessage
	if !alreadyHooked && len(cfg.Backup.PreviousStatusLine) > 0 {
		if existing, ok := claudesettings.GetStatusLine(s); ok {
			discarded = existing
		}
	}

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

	// Order matters: the backup is the only copy of the statusLine this
	// install is about to overwrite, so it has to reach disk first. If
	// config.Save fails we abort with settings.json untouched — a recoverable
	// state. The reverse order loses the user's original command for good,
	// and the later uninstall would delete the key instead of restoring it.
	// A backup written without settings.json being hooked is harmless: it is
	// exactly the state the first-install guard above already tolerates.
	if err := config.Save(p.Config, cfg); err != nil {
		return err
	}
	if err := claudesettings.Save(p.Settings, s); err != nil {
		return err
	}

	if alreadyHooked {
		fmt.Fprintln(stdout, "ccsb: already installed; backup preserved")
	} else {
		fmt.Fprintln(stdout, "ccsb: installed")
	}
	if len(discarded) > 0 {
		fmt.Fprintf(stdout, "ccsb: replaced statusLine %s — not saved: the backup from the "+
			"first install is kept instead (see `ccsb status`)\n", discarded)
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
		if issue, _ := proxyIssue(cfg.Proxy.Command, p.Self); issue != "" {
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

// printInteractiveHint explains why usage is being printed. The help text
// alone does not tell a first-time caller that ccsb normally consumes a
// payload on stdin, which is the whole reason a bare invocation used to look
// like a hang.
func printInteractiveHint(w io.Writer) {
	fmt.Fprint(w, `ccsb: stdin is a terminal, so there is no statusLine payload to render.
      Claude Code invokes ccsb with the payload piped in; see "install" below.

`)
}

func printHelp(w io.Writer) {
	const help = `ccsb - Claude Code statusLine provider

Without arguments, ccsb reads the JSON payload from stdin and renders the
configured statusLine (proxy or built-in fallback). Claude Code invokes it
via the "statusLine.command" entry in ~/.claude/settings.json. Run from a
terminal, where there is no payload to read, ccsb prints this help instead.

Subcommands:
  install     Replace the statusLine in settings.json with this binary so
              Claude Code calls ccsb. On the FIRST install the entry found
              there is saved into ccsb's config as the uninstall backup; an
              existing backup is never overwritten, so an entry set after
              that is replaced without being saved — ccsb names it when it
              happens.
  uninstall   Restore the previous statusLine from the saved backup. Refuses
              unless settings.json currently points at this binary. With no
              backup recorded it removes the statusLine key instead.
  status      Print whether settings.json points at ccsb and show the
              current proxy/backup state.
  mode        Print the current mode (native or proxy) when invoked with no
              argument. With "native", clear the proxy block; with "proxy",
              set it to "npx -y ccstatusline@latest" by default or to the
              given command and arguments.
  config      "config reset" moves the existing config.json aside (as a
              timestamped .bak file) so the next run picks up the in-code
              defaults. The uninstall backup of your previous statusLine is
              carried over rather than reset — it is state ccsb owes you
              back, not a setting. A no-op when no config exists, and it
              still works on a config too broken to parse.
              "config auto [level]" prints the highest version jump the
              renderer may install by itself, or sets it: patch, minor,
              major, or off to clear it. Setting a level also reports any
              reason it cannot take effect (see doctor), without refusing.
  captures    "captures clean" removes capture files (.json payloads, .out
              rendered output, plus any .err and .diag siblings).
              Without arguments it empties the capture directory; with
              --older-than <duration> it keeps anything newer (e.g. 7d, 24h).
              Captures are diagnostic only and safe to remove at any time.
  doctor      Diagnose and auto-fix configuration problems: re-installs if
              settings.json is not hooked; switches to native mode if the
              proxy command is circular or is another ccsb binary, both of
              which would make ccsb call itself.
              It also reports — without changing them — a proxy command that
              cannot be resolved to an executable (resolution uses PATH, so a
              bare name like "npx" counts as found when installed; an
              unresolvable one is kept, because the same config on another
              machine is not wrong and ccsb renders natively meanwhile), an
              installed ccsb-wizard skill that differs from this binary's
              copy, since updating ccsb does not update a skill already
              written to ~/.claude/skills/, and an update.auto that cannot
              fire because no version segment in "rows" sets
              "check_update": true.
  update      Replace this binary with the newest GitHub release. Refuses
              on locally built binaries, Windows, and non-writable targets.
  install-skill   Extract the ccsb-wizard Claude Code skill to
              ~/.claude/skills/ccsb-wizard/SKILL.md. Run /ccsb-wizard inside
              Claude Code to start an AI-guided configuration dialogue.
              Re-run after updating ccsb to get the latest skill version.
  uninstall-skill Remove ~/.claude/skills/ccsb-wizard/ (and any flat
              ccsb-wizard.md left by a pre-0.4.6 install).
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
