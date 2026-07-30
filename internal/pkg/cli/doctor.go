package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/capture"
	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/claudesettings"
	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/config"
	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/render"
	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/selfupdate"
)

// proxyIssue returns a non-empty human-readable description when cmd is a
// problematic proxy target relative to self, plus whether the problem
// warrants clearing the proxy block. The three cases are: cmd equals self
// (circular loop), cmd has the same base name as self (another ccsb
// binary), and cmd cannot be resolved to an executable.
//
// Only the first two are cleared. They are genuine misconfigurations —
// ccsb proxying to ccsb spawns itself on every status update — and no
// later event makes them correct. An unresolvable target is different: the
// same config on a second machine, or an invocation with a trimmed PATH,
// produces it without anything being wrong with the user's intent, and
// clearing it destroys a setting ccsb cannot reconstruct. It is therefore
// reported and left alone. Nothing is at risk in leaving it: a proxy that
// cannot start is caught in statusline.Run, which falls back to the native
// renderer rather than exiting non-zero.
//
// Resolution goes through exec.LookPath, which is what actually launching the
// proxy will do (proxy.Run -> exec.CommandContext). A bare command name is a
// PATH lookup, not a relative path: the documented default proxy is
// "npx -y ccstatusline@latest", and os.Stat("npx") tests the process cwd,
// finds nothing, and reports a working configuration as broken. Since the
// caller reacts to that by clearing and saving the proxy block, the mistake
// destroyed the very configuration `ccsb mode proxy` had just written.
// LookPath also covers the path-like forms — a name containing a separator is
// tried directly and PATH is not consulted — and additionally requires the
// executable bit, which a proxy target needs anyway.
func proxyIssue(cmd, self string) (issue string, clear bool) {
	switch {
	case cmd == self:
		return "proxy points to self (circular)", true
	case filepath.Base(cmd) == filepath.Base(self):
		return "proxy appears to be another ccsb binary", true
	default:
		if _, err := exec.LookPath(cmd); err != nil {
			return "proxy target not found or not executable", false
		}
		return "", false
	}
}

// schemaCheckResult bundles the outcome of comparing a capture's
// top-level keys against the set ccsb's renderer expects.
//
//   - CapturePath is the file inspected (empty when no capture was
//     available).
//   - Missing are keys ccsb's renderer expects that are absent from
//     this capture. Some may legitimately be absent at session start
//     (e.g. rate_limits) — the schema_health indicator's narrow
//     detection treats those as informational.
//   - Extra are keys present in the capture that ccsb does not
//     recognise. These are likely additive schema changes from Claude
//     Code; harmless to ccsb (Go ignores unknown JSON keys) but worth
//     flagging so the user knows the renderer could grow new
//     segments.
//   - Note carries a human-readable hint when no useful comparison
//     could be performed (no captures, capture unreadable, capture
//     not a JSON object). CapturePath / Missing / Extra are then
//     empty.
type schemaCheckResult struct {
	CapturePath string
	Missing     []string
	Extra       []string
	Note        string
}

// latestCaptureJSON returns the absolute path to the chronologically
// newest *.json file in dir. Capture names carry an RFC3339Nano UTC
// timestamp (see capture.basename), whose fractional second is NOT fixed
// width — the format trims trailing zeros, so ".1Z" sorts after ".10001Z"
// lexicographically even though it is earlier. The timestamp is therefore
// parsed and compared as a time.Time; files without one are ignored.
// Returns "" when no such file exists or dir is unreadable.
func latestCaptureJSON(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var latest string
	var latestAt time.Time
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		at, ok := capture.TimeFromName(name)
		if !ok {
			continue
		}
		if latest == "" || at.After(latestAt) {
			latest, latestAt = name, at
		}
	}
	if latest == "" {
		return ""
	}
	return filepath.Join(dir, latest)
}

// schemaCheck reads the most recent capture in captureDir, decodes
// its top-level keys, and diffs them against render.ExpectedPayloadKeys().
// A missing capture, unreadable file, or non-object payload is reported
// via the Note field rather than as an error — schemaCheck is a
// diagnostic helper, not a hard-fail step.
func schemaCheck(captureDir string) schemaCheckResult {
	path := latestCaptureJSON(captureDir)
	if path == "" {
		return schemaCheckResult{Note: "no capture available — run ccsb at least once first"}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return schemaCheckResult{CapturePath: path, Note: fmt.Sprintf("read capture: %s", err)}
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return schemaCheckResult{CapturePath: path, Note: fmt.Sprintf("capture is not a JSON object: %s", err)}
	}
	expected := render.ExpectedPayloadKeys()
	expectedSet := make(map[string]bool, len(expected))
	for _, k := range expected {
		expectedSet[k] = true
	}
	var missing, extra []string
	for _, k := range expected {
		if _, ok := top[k]; !ok {
			missing = append(missing, k)
		}
	}
	for k := range top {
		if !expectedSet[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return schemaCheckResult{CapturePath: path, Missing: missing, Extra: extra}
}

// runDoctor diagnoses common configuration problems and fixes them
// automatically:
//
//   - settings.json not hooked → installs ccsb
//   - proxy command is circular, another ccsb, or missing → native mode
//
// It also runs a non-fixing schema-check against the most recent
// capture, listing any top-level JSON keys that are missing relative
// to ccsb's renderer expectations or present as additive schema
// extensions. Schema drift is informational — ccsb cannot fix the
// upstream payload — but surfacing it explicitly helps explain a
// surprising schema_health indicator.
func runDoctor(p Paths, stdout io.Writer) error {
	// fixed counts what doctor changed; reported counts what it found and
	// deliberately left alone. Keeping them apart is what lets the verdict
	// below distinguish "clean" from "nothing I may touch".
	fixed, reported := 0, 0

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
		switch issue, clear := proxyIssue(cmd, p.Self); {
		case issue == "":
		case clear:
			fmt.Fprintf(stdout, "ccsb: doctor: %s — switching to native mode\n", issue)
			cfg.Proxy.Command = ""
			cfg.Proxy.Args = nil
			if err := config.Save(p.Config, cfg); err != nil {
				return err
			}
			fixed++
		default:
			// Reported, never repaired — like the stale skill copy and the
			// inert update.auto, and so deliberately not counted as fixed.
			fmt.Fprintf(stdout,
				"ccsb: doctor: %s: %s — leaving the config alone; ccsb renders natively until it resolves\n",
				issue, cmd)
			reported++
		}
	}

	// "no issues found" is reserved for a run that found nothing at all.
	// It used to be printed whenever nothing was REPAIRED, which read as
	// an all-clear directly underneath a problem doctor had just named.
	switch {
	case fixed > 0:
		fmt.Fprintf(stdout, "ccsb: doctor: fixed %d issue(s)\n", fixed)
	case reported > 0:
		fmt.Fprintf(stdout, "ccsb: doctor: nothing repaired; %d finding(s) above are yours to decide on\n", reported)
	default:
		fmt.Fprintln(stdout, "ccsb: doctor: no issues found")
	}

	// Schema-check section — informational, never returns an error.
	res := schemaCheck(p.Capture)
	switch {
	case res.Note != "":
		fmt.Fprintf(stdout, "ccsb: doctor: schema-check: %s\n", res.Note)
	case len(res.Missing) == 0 && len(res.Extra) == 0:
		fmt.Fprintf(stdout, "ccsb: doctor: schema-check: %s — all expected keys present, no extras\n",
			filepath.Base(res.CapturePath))
	default:
		fmt.Fprintf(stdout, "ccsb: doctor: schema-check: %s\n", filepath.Base(res.CapturePath))
		if len(res.Missing) > 0 {
			fmt.Fprintf(stdout, "ccsb: doctor: schema-check: missing keys: %s\n", strings.Join(res.Missing, ", "))
		}
		if len(res.Extra) > 0 {
			fmt.Fprintf(stdout, "ccsb: doctor: schema-check: additive keys: %s\n", strings.Join(res.Extra, ", "))
		}
	}

	// Ask the guards directly. doctor is not the render path, so the
	// writability probe is free here — and it means this line agrees with the
	// ⊘ glyph even before any update has been attempted.
	blocked := selfupdate.Guard(selfupdate.Options{StateDir: p.State, Self: p.Self})
	if diag := updateDiagnostic(blocked); diag != "" {
		fmt.Fprintf(stdout, "ccsb: doctor: %s\n", diag)
	}
	if diag := autoUpdateDiagnostic(cfg.Update.Auto); diag != "" {
		fmt.Fprintf(stdout, "ccsb: doctor: %s\n", diag)
	}
	if diag := inertAutoUpdateDiagnostic(cfg); diag != "" {
		fmt.Fprintf(stdout, "ccsb: doctor: %s\n", diag)
	}
	// Reported, never repaired — see skillDiagnostic. It is also not counted in
	// `fixed`, so "no issues found" still refers to what doctor actually acted on.
	if diag := skillDiagnostic(p); diag != "" {
		fmt.Fprintf(stdout, "ccsb: doctor: %s\n", diag)
	}
	return nil
}

// updateDiagnostic explains a self-update block reason, or returns "" when
// nothing blocks it.
//
// The reason is supplied by the caller rather than read from the updater's
// attempt record. That record only exists once `ccsb update` has actually
// run, which would leave doctor silent for a user who sees the ⊘ glyph and
// comes here for the explanation — the glyph appears immediately for the
// in-process reasons. Running the guards instead means both always agree.
// The renderer cannot do the same: its writability probe is filesystem work,
// which the render path must not perform.
func updateDiagnostic(reason render.BlockReason) string {
	if reason == render.BlockNone {
		return ""
	}
	switch reason {
	case render.BlockWindows:
		return "self-update: blocked (windows — swap the release archive manually)"
	case render.BlockLocalBuild:
		return "self-update: blocked (local build — rebuild with go build -o bin/ccsb ./cmd/ccsb)"
	case render.BlockNotWritable:
		return "self-update: blocked (target directory not writable — reinstall with the privileges that own it; ccsb never elevates by itself)"
	default:
		return fmt.Sprintf("self-update: blocked (%s)", reason)
	}
}

// inertAutoUpdateDiagnostic reports an update.auto that can never fire, or ""
// when it can — or when it is not asked for, or already unusable for a reason
// autoUpdateDiagnostic prints itself.
//
// The auto-update trigger sits inside the version segment's background check:
// renderVersion returns early unless that segment sets check_update, so
// update.auto is never consulted at all. check_update is a plain Go bool whose
// zero value is false and only defaultConfig sets it — and defaultConfig applies
// only when the config carries no rows. Every hand-written or wizard-generated
// layout therefore disables the whole update path while update.auto sits in the
// config looking enabled; a real machine stayed on v0.4.10 that way.
//
// Reported, never repaired, and not counted in doctor's `fixed` tally: the
// layout is the user's (same reasoning as skillDiagnostic).
func inertAutoUpdateDiagnostic(cfg config.Config) string {
	if cfg.Update.Auto == "" || autoUpdateDiagnostic(cfg.Update.Auto) != "" || len(cfg.Render.Rows) == 0 {
		return ""
	}
	for _, row := range cfg.Render.Rows {
		for _, seg := range row.Segments {
			if seg.Type == "version" && seg.CheckUpdate {
				return ""
			}
		}
	}
	return fmt.Sprintf("update.auto: %q never runs — no version segment sets \"check_update\": true, "+
		"and the update check only runs from there", cfg.Update.Auto)
}

// autoUpdateDiagnostic reports an update.auto value ccsb does not
// understand, or "" when the value is valid or absent. An unrecognised
// value disables automatic updating, and the renderer says nothing about it
// — so without this line a typo would look exactly like a deliberate
// opt-out.
func autoUpdateDiagnostic(auto string) string {
	switch auto {
	case "", "patch", "minor", "major":
		return ""
	default:
		return fmt.Sprintf("update.auto: %q is not one of patch, minor, major — automatic updating is off", auto)
	}
}
