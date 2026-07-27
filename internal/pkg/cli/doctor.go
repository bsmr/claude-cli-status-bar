package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
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
