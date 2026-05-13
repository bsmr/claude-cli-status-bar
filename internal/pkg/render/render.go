// Package render builds the configurable statusLine output for ccsb.
//
// The public surface is Config, Segment, Options, and Render. Per-segment
// renderers are registered in segments.go; ANSI helpers live in ansi.go;
// .git/HEAD branch lookup in git.go.
package render

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"
)

// Config is the on-disk render schema, embedded into config.Config.
type Config struct {
	Rows      []Row  `json:"rows,omitzero"`
	Separator string `json:"separator,omitempty"`
	Powerline bool   `json:"powerline,omitempty"`
}

// Row is one output line. Powerline mode reads Bg to colour-fill the
// row from column 0 to the terminal's right edge; without Powerline
// or with an empty Bg, the row renders as a plain join of its
// segments.
type Row struct {
	Segments []Segment `json:"segments"`
	Bg       string    `json:"bg,omitempty"`
}

// UnmarshalJSON accepts two shapes:
//   - The legacy 0.1.x bare array of segments: [{...},{...}]
//     → unmarshals into Row{Segments: ..., Bg: ""}.
//   - The 0.2.0 native object form: {"segments":[...], "bg":"234"}.
//
// Detection is by the first non-whitespace byte: '[' for array, '{' for
// object. All other tokens (null, numbers, strings, booleans) are
// rejected with an explicit error.
func (r *Row) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) == 0 {
		return errors.New("render.Row: empty value")
	}
	if trimmed[0] == '[' {
		var segs []Segment
		if err := json.Unmarshal(trimmed, &segs); err != nil {
			return err
		}
		r.Segments = segs
		r.Bg = ""
		return nil
	}
	// Only '{' is a valid object start; anything else (null, number,
	// string, boolean) is rejected.
	if trimmed[0] != '{' {
		return fmt.Errorf("render.Row: unexpected JSON token %q", string(trimmed[:1]))
	}
	// Object shape — use an alias to bypass this UnmarshalJSON method.
	type rowAlias Row
	var a rowAlias
	if err := json.Unmarshal(trimmed, &a); err != nil {
		return err
	}
	*r = Row(a)
	return nil
}

// Segment is one element in a row. Type drives dispatch; the remaining
// fields are interpreted per-type.
type Segment struct {
	Type   string `json:"type"`
	Format string `json:"format,omitempty"`
	Label  string `json:"label,omitempty"`
	FG     string `json:"fg,omitempty"`
	BG     string `json:"bg,omitempty"`
	Bold   bool   `json:"bold,omitempty"`

	// Type-specific knobs:
	Show1MFlag bool   `json:"show_1m_flag,omitempty"` // type=model
	Style      string `json:"style,omitempty"`        // type=context|limit_5h|limit_7d

	// Thresholds let percentage-bearing segments (context, limit_5h,
	// limit_7d) override FG based on the current used-percentage. The
	// highest matching Min wins; thresholds with empty FG are skipped;
	// segments without a percentage metric ignore the field.
	Thresholds []Threshold `json:"thresholds,omitempty"`

	// ThresholdTarget selects which part of the segment is colored when
	// a threshold matches. "" or "all" (default) wraps the whole
	// segment via the renderer's outer style() call. "pct" causes the
	// segment to color only the percentage digits internally, leaving
	// bar cells, tokens, and labels in the segment's static FG.
	// Unknown values fall back to "all".
	ThresholdTarget string `json:"threshold_target,omitempty"`
}

// Threshold is one entry in Segment.Thresholds. Min is a percentage
// value (0-100, inclusive on the lower bound) the segment's metric must
// reach for FG to apply.
type Threshold struct {
	Min float64 `json:"min"`
	FG  string  `json:"fg,omitempty"`
}

// Options configures a single Render call.
type Options struct {
	Config Config
	// Cwd, when non-empty, overrides payload.workspace.current_dir as the
	// starting directory for git-state lookup.
	Cwd string
	// NoColor disables ANSI escape emission. Resolved by the caller from
	// the NO_COLOR environment variable (no-color.org convention).
	NoColor bool
}

// payload mirrors the subset of the Claude Code statusLine payload that
// segments consume. Unknown fields are ignored.
//
// Field-shape helper structs use the F suffix (modelF, costF, ...) so they
// stay grep-distinct from any future public domain types and are clearly
// JSON-shape carriers, not domain models.
type payload struct {
	SessionID    string    `json:"session_id"`
	SessionName  string    `json:"session_name"`
	Model        modelF    `json:"model"`
	Workspace    workspace `json:"workspace"`
	OutputStyle  outputF   `json:"output_style"`
	Effort       effortF   `json:"effort"`
	Cost         costF     `json:"cost"`
	Context      contextF  `json:"context_window"`
	Limits       limitsF   `json:"rate_limits"`
	FastMode     bool      `json:"fast_mode"`
	Thinking     thinkF    `json:"thinking"`
	Exceeds200kT bool      `json:"exceeds_200k_tokens"`
}

type modelF struct {
	DisplayName string `json:"display_name"`
}
type workspace struct {
	CurrentDir string `json:"current_dir"`
	ProjectDir string `json:"project_dir"`
}
type outputF struct {
	Name string `json:"name"`
}
type effortF struct {
	Level string `json:"level"`
}
type costF struct {
	TotalCostUSD      float64 `json:"total_cost_usd"`
	TotalDurationMS   int64   `json:"total_duration_ms"`
	TotalLinesAdded   int64   `json:"total_lines_added"`
	TotalLinesRemoved int64   `json:"total_lines_removed"`
}
type contextF struct {
	UsedPercentage    float64 `json:"used_percentage"`
	ContextWindowSize int64   `json:"context_window_size"`
	TotalInputTokens  int64   `json:"total_input_tokens"`
	TotalOutputTokens int64   `json:"total_output_tokens"`
	CurrentUsage      struct {
		InputTokens              int64 `json:"input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	} `json:"current_usage"`
}
type limitsF struct {
	FiveHour rateLimitF `json:"five_hour"`
	SevenDay rateLimitF `json:"seven_day"`
}
type rateLimitF struct {
	UsedPercentage float64 `json:"used_percentage"`
	ResetsAt       int64   `json:"resets_at"`
}
type thinkF struct {
	Enabled bool `json:"enabled"`
}

// nowFunc returns the current time. Tests swap this for a fixed clock so
// countdown formatting is deterministic.
var nowFunc = time.Now

// defaultRows is used when Config.Rows is empty.
var defaultRows = []Row{
	{Segments: []Segment{{Type: "model", Show1MFlag: true}, {Type: "context", Style: "bar+pct"}}},
	{Segments: []Segment{{Type: "cost"}, {Type: "limit_5h"}, {Type: "limit_7d"}}},
	{Segments: []Segment{{Type: "git_branch"}, {Type: "cwd"}}},
}

const defaultSeparator = " | "

const (
	powerlineChevron      = ""
	powerlineChevronWidth = 1
	powerlineChevronFG    = "245"
)

// ansiRegexp matches ANSI SGR escape sequences so displayWidth can
// strip them before counting visible columns.
var ansiRegexp = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// displayWidth returns the visible column count of s after stripping
// ANSI escape sequences. Uses go-runewidth so emoji (width 2), CJK
// fullwidth (width 2), and zero-width joiners are handled correctly.
// The Powerline renderer uses this to know where the bg fill ends.
func displayWidth(s string) int {
	stripped := ansiRegexp.ReplaceAllString(s, "")
	return runewidth.StringWidth(stripped)
}

// Render parses raw, walks Config.Rows, and returns the joined multi-line
// output. An empty Config.Rows triggers defaultRows. A global JSON-parse
// failure falls back to a hardcoded "<model> · <cwd>" so Claude Code never
// gets an empty statusLine.
func Render(opts Options, raw []byte) (string, error) {
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		// last-resort single-line so the bar is never blank.
		return lastResort(opts, raw), nil
	}

	rows := opts.Config.Rows
	if len(rows) == 0 {
		rows = defaultRows
	}
	sep := opts.Config.Separator
	if sep == "" {
		sep = defaultSeparator
	}

	cwd := opts.Cwd
	if cwd == "" {
		cwd = p.Workspace.CurrentDir
	}

	env := renderEnv{
		cwd:          cwd,
		colorEnabled: !opts.NoColor,
		nowUnix:      nowFunc().Unix(),
	}
	if opts.Config.Powerline && env.colorEnabled {
		env.ttyCols = ttyColsFunc()
	}

	var lines []string
	for _, row := range rows {
		var line string
		if opts.Config.Powerline && env.colorEnabled {
			line = renderRowPowerline(&p, row, env)
		} else {
			line = renderRowNatural(&p, row, env, sep)
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n"), nil
}

// renderRowNatural is the 0.1.x row builder: render each non-empty
// segment, join them with the configured separator. Returns "" when
// every segment renders empty so the row joiner skips the row.
func renderRowNatural(p *payload, row Row, env renderEnv, sep string) string {
	var parts []string
	for _, seg := range row.Segments {
		s := renderSegment(p, seg, env)
		if s == "" {
			continue
		}
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, sep)
}

// renderEnv carries per-call state from Render to each segment renderer.
// Segment functions read these fields but never mutate them.
type renderEnv struct {
	cwd          string // resolved cwd (Options.Cwd or payload.Workspace.CurrentDir)
	colorEnabled bool   // false when NoColor was set on Options
	nowUnix      int64  // wall clock at the start of Render, for time-based segments
	ttyCols      int    // populated only when Config.Powerline is true and colour is on
}

// renderSegment dispatches one segment via the registry. Unknown types
// render "?<type>?" so typos are visible without breaking the row. The
// effective outer FG comes from chooseFG (threshold override) unless
// ThresholdTarget=="pct", in which case the segment self-styles its
// percentage substring internally and the outer wrap uses the static
// FG so non-pct regions stay neutral. Segments whose body is empty
// short-circuit to "" so they neither contribute to the row nor emit
// styling escapes for an invisible payload.
func renderSegment(p *payload, s Segment, env renderEnv) string {
	fn, ok := segmentFuncs[s.Type]
	if !ok {
		return "?" + s.Type + "?"
	}
	out := fn(p, s, env)
	if out == "" {
		return ""
	}
	// For ThresholdTarget == "pct", the segment function has already
	// colored its percentage substring internally; the outer wrap
	// must therefore use the static FG so non-pct regions render in
	// the segment's neutral color, not in the threshold override.
	fg := s.FG
	if s.ThresholdTarget != "pct" {
		fg = chooseFG(s, p)
	}
	return style(out, fg, s.BG, s.Bold, env.colorEnabled)
}

// chooseFG picks the foreground color for a segment, honouring
// Segment.Thresholds. Behaviour:
//
//   - No thresholds, or segment without a percentage metric, or no
//     threshold matching: returns Segment.FG verbatim.
//   - Multiple matching thresholds: the one with the highest Min wins.
//   - A threshold with FG=="" is skipped, as if absent.
func chooseFG(s Segment, p *payload) string {
	if len(s.Thresholds) == 0 {
		return s.FG
	}
	pct, ok := segmentMetric(s.Type, p)
	if !ok {
		return s.FG
	}
	chosen := s.FG
	var chosenMin float64
	matched := false
	for _, t := range s.Thresholds {
		if t.FG == "" {
			continue
		}
		if pct < t.Min {
			continue
		}
		if !matched || t.Min > chosenMin {
			chosen = t.FG
			chosenMin = t.Min
			matched = true
		}
	}
	return chosen
}

// segmentMetric returns the percentage value (0-100) that drives a
// percentage-aware segment's threshold logic. The boolean is false for
// segments that have no such metric — in that case Thresholds are
// ignored.
func segmentMetric(typ string, p *payload) (float64, bool) {
	switch typ {
	case "context":
		return p.Context.UsedPercentage, true
	case "limit_5h":
		return p.Limits.FiveHour.UsedPercentage, true
	case "limit_7d":
		return p.Limits.SevenDay.UsedPercentage, true
	}
	return 0, false
}

// renderRowPowerline builds one Powerline-styled row: row-bg fill,
// thin chevrons between non-empty segments, full-width padding when
// the TTY column count is known.
//
// The row-bg must be re-emitted after every segment because each
// segment's outer style() wrap ends with \x1b[0m, which resets both
// fg AND bg. Re-emitting bg256(row.Bg) between segments and before
// the padding step keeps the bar visually continuous.
func renderRowPowerline(p *payload, row Row, env renderEnv) string {
	// Powerline emits ANSI unconditionally; callers must only invoke
	// this when colour is on. The Render dispatch enforces this
	// guarantee; we double-check here so the function is
	// self-contained.
	if !env.colorEnabled {
		return ""
	}

	// 1. Render each segment; drop empties.
	var parts []string
	for _, seg := range row.Segments {
		s := renderSegment(p, seg, env)
		if s == "" {
			continue
		}
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		return ""
	}

	// 2. Build the chevron: muted-grey fg, no bg of its own, closed
	//    with the default-foreground SGR so the surrounding bg
	//    continues to show through.
	chev := powerlineChevron
	if open := fg256(powerlineChevronFG); open != "" {
		chev = open + powerlineChevron + "\x1b[39m"
	}

	// 3. bgOpen is re-emitted after each [0m-terminated segment.
	var bgOpen string
	if row.Bg != "" {
		bgOpen = bg256(row.Bg)
	}

	// 4. Interleave.
	var b strings.Builder
	b.WriteString(bgOpen)
	for i, part := range parts {
		if i > 0 {
			b.WriteString(chev)
			b.WriteString(bgOpen) // chev was fg-only; ensure bg before next segment
		}
		b.WriteString(part)
		b.WriteString(bgOpen) // segment's [0m killed bg; restore before chev/padding/reset
	}

	// 5. Pad to terminal width.
	if env.ttyCols > 0 && row.Bg != "" {
		used := 0
		for _, part := range parts {
			used += displayWidth(part)
		}
		used += (len(parts) - 1) * powerlineChevronWidth
		if remaining := env.ttyCols - used; remaining > 0 {
			b.WriteString(strings.Repeat(" ", remaining))
		}
	}

	// 6. Close.
	if row.Bg != "" {
		b.WriteString(reset)
	}
	return b.String()
}

// wrapPct conditionally wraps pctText in the threshold-chosen
// foreground when Segment.ThresholdTarget == "pct" and a threshold
// matches. The wrap closes back to the segment's static FG (or to
// "\x1b[39m" terminal-default if the segment has no static FG) so
// segment text outside the wrap continues in the surrounding color.
//
// Returns pctText unchanged when ThresholdTarget is anything other
// than "pct", when color is disabled, when no threshold matches, when
// the threshold FG equals the segment's static FG, or when fg256
// rejects the threshold FG value.
func wrapPct(pctText string, s Segment, p *payload, colorEnabled bool) string {
	if !colorEnabled || s.ThresholdTarget != "pct" {
		return pctText
	}
	fg := chooseFG(s, p)
	if fg == "" || fg == s.FG {
		return pctText
	}
	openInner := fg256(fg)
	if openInner == "" {
		return pctText
	}
	closeInner := "\x1b[39m"
	if reopen := fg256(s.FG); reopen != "" {
		closeInner = reopen
	}
	return openInner + pctText + closeInner
}

// lastResort renders a single line from whatever fields we can salvage when
// the payload is unparsable.
func lastResort(opts Options, raw []byte) string {
	// Try a relaxed second pass: pick model and cwd if they happen to be
	// present as flat strings somewhere. If not, fall back to a literal.
	var loose struct {
		Model     modelF    `json:"model"`
		Workspace workspace `json:"workspace"`
	}
	_ = json.Unmarshal(raw, &loose)
	model := loose.Model.DisplayName
	cwd := opts.Cwd
	if cwd == "" {
		cwd = loose.Workspace.CurrentDir
	}
	switch {
	case model != "" && cwd != "":
		return model + " · " + cwd
	case model != "":
		return model
	case cwd != "":
		return cwd
	default:
		return "claude-cli-status-bar"
	}
}
