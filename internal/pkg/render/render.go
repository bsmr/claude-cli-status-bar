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
	"sort"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"
)

// Config is the on-disk render schema, embedded into config.Config.
type Config struct {
	Rows      []Row  `json:"rows,omitzero"`
	Separator string `json:"separator,omitempty"`
	Powerline bool   `json:"powerline,omitempty"`
	// Width overrides automatic terminal-width detection when non-zero.
	// Powerline bg-fill and truncation use this value directly instead
	// of querying /dev/tty or the /proc parent chain. Rows are not
	// overridable via this field; discoverTermSize returns rows=0 when
	// this branch is taken, so a tty_size segment will render as
	// "<width>×0" until either /dev/tty or the /proc walk supplies a
	// row count.
	Width int `json:"width,omitempty"`
	// Margin reserves N columns of plain (non-bg) space at the start
	// of every row and shrinks the usable bg-fill width by 2*N. The
	// goal is to leave room for Claude Code's built-in statusLine
	// chrome on each side so two-row Powerline configurations are not
	// truncated. Nil defaults to defaultMargin (= 2). Set to 0 to
	// disable; negative values clamp to 0.
	Margin *int `json:"margin,omitempty"`
	// PowerlineStyle selects the chevron glyph between segments when
	// Powerline is enabled. "" or "thin" (default) renders U+E0B1, the
	// thin chevron. "solid" renders U+E0B0, the filled wedge. Unknown
	// values silently fall back to thin so a typo cannot break
	// rendering.
	PowerlineStyle string `json:"powerline_style,omitempty"`
	// CapStyle selects the end-cap glyph variant when a row's Caps
	// field is true. "" or "round" (default) renders U+E0B6 / U+E0B4
	// filled half-circles. "square" emits a 1-col plain bg-painted
	// space on each side. "slant" renders U+E0BC / U+E0BA filled
	// triangles. Unknown values fall back to "round".
	CapStyle string `json:"cap_style,omitempty"`
	// Palette is a single shared list of ANSI 256-color strings used
	// as the global background gradient across all output rows. Output
	// row N starts at index N*PaletteStride and rotates from there per
	// visible segment. Row.Palette (when set) overrides this on a
	// per-row basis. When empty, defaultGlobalPalette is used.
	Palette []string `json:"palette,omitempty"`
	// PaletteStride is the per-row offset into the global Palette,
	// applied as paletteStart = outputRowIndex * PaletteStride. When
	// 0, defaultPaletteStride (= 2) is used so every output row starts
	// two shades brighter than the previous one. Negative values clamp
	// to 0 (no row-to-row progression).
	PaletteStride int `json:"palette_stride,omitempty"`
}

// defaultMargin is the implicit Config.Margin when the field is unset.
const defaultMargin = 2

// defaultPaletteStride is the implicit Config.PaletteStride when the
// field is unset (or non-positive). Two shades per row produces a
// clearly visible brighten step per output row without exhausting a
// 9-entry palette across the typical 2–3 visible rows.
const defaultPaletteStride = 2

// defaultGlobalPalette is the implicit Config.Palette when the field
// is unset. Nine monotonic dark→light greys covering ANSI 232..240 so
// the per-row stride math never has to wrap around within a real
// bar (4 output rows × stride 2 = offset 6 + a few cells fits cleanly).
var defaultGlobalPalette = []string{
	"232", "233", "234", "235", "236", "237", "238", "239", "240",
}

// effectiveMargin returns the column count to reserve as plain leading
// space and to subtract twice from the usable bg-fill width. A nil
// Margin uses defaultMargin; a negative Margin clamps to 0.
func (c Config) effectiveMargin() int {
	if c.Margin == nil {
		return defaultMargin
	}
	if *c.Margin < 0 {
		return 0
	}
	return *c.Margin
}

// effectivePaletteStride returns the per-output-row offset into the
// global palette. Zero or unset uses defaultPaletteStride; negative
// values clamp to 0 so a malformed config cannot mathematically run
// off the start of the slice.
func (c Config) effectivePaletteStride() int {
	if c.PaletteStride == 0 {
		return defaultPaletteStride
	}
	if c.PaletteStride < 0 {
		return 0
	}
	return c.PaletteStride
}

// Row is one output line. Powerline mode reads Bg to colour-fill the
// row from column 0 to the terminal's right edge; without Powerline
// or with an empty Bg, the row renders as a plain join of its
// segments.
type Row struct {
	Segments []Segment `json:"segments"`
	Bg       string    `json:"bg,omitempty"`
	// Palette, when non-empty, rotates through these ANSI 256-color
	// strings across the visible segments of this row. Indexed by
	// visible-segment rank (empty segments don't claim a palette
	// slot), modulo len(Palette). Per-segment Segment.BG overrides
	// the palette at that position. When Palette is empty, the
	// resolution falls through to Bg (uniform fill) and finally to
	// the package-level defaultPalette when Powerline is active.
	Palette []string `json:"palette,omitempty"`
	// Caps, when true, emits a 1-col cap glyph on each end where
	// the outermost visible segment has an effective bg. Both
	// sides are evaluated independently — a row whose first segment
	// has a bg but whose last does not gets a left cap only. The
	// glyph variant is selected globally via Config.CapStyle. Each
	// enabled cap consumes 1 column from the row's usable bg-fill
	// width.
	Caps bool `json:"caps,omitempty"`
	// Align, when "right", renders segments flush to the right edge of
	// the terminal (margin respected). Powerline is bypassed for this
	// row so it renders as plain text regardless of Config.Powerline.
	// Unknown values are treated as left-aligned (the default).
	Align string `json:"align,omitempty"`
}

// UnmarshalJSON accepts two shapes:
//   - The legacy 0.1.x bare array of segments: [{...},{...}]
//     → unmarshals into a Row whose Segments field carries the decoded
//     array; all other Row fields are zero-valued (Bg "", Palette nil,
//     Caps false).
//   - The 0.2.0 native object form: {"segments":[...], "bg":"234", ...}.
//
// Detection is by the first non-whitespace byte: '[' for array, '{' for
// object. All other tokens (null, numbers, strings, booleans) are
// rejected with an explicit error.
func (r *Row) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) == 0 {
		return errors.New("render: row: empty value")
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
		return fmt.Errorf("render: row: unexpected JSON token %q", string(trimmed[:1]))
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

	// TokenPosition controls where the used/total token fraction sits in a
	// context segment rendered with the "bar+pct" style: "after" (default)
	// trails the percentage, "before" leads the bar, "hidden" omits it.
	// Empty or unknown values fall back to "after". Only the context
	// segment emits a token fraction, so the field is ignored elsewhere.
	TokenPosition string `json:"token_position,omitempty"` // type=context

	// BarWidth overrides the circle-bar length (in cells) for segments
	// that draw a bar: context, limit_5h, limit_7d. Each cell has five
	// quarter-fill states. Zero or negative falls back to the package
	// default (barCells = 16). Lets compact bars sit on limit_5h /
	// limit_7d while context keeps the wider default.
	BarWidth int `json:"bar_width,omitempty"` // type=context|limit_5h|limit_7d

	// BarGlyphs overrides the bar fill ramp for segments that draw a bar
	// (context, limit_5h, limit_7d). It is an ordered ramp from empty
	// (index 0) to full (last index); each cell renders one of len-1
	// sub-steps. Must have at least two entries; fewer falls back to the
	// built-in ramp. When set it overrides BarStyle.
	BarGlyphs []string `json:"bar_glyphs,omitempty"` // type=context|limit_5h|limit_7d

	// BarStyle selects a built-in fill ramp: "circles" (default, ○◔◑◕●)
	// or "blocks" (░▏▎▍▌▋▊▉█). Unknown values fall back to "circles".
	// Ignored when BarGlyphs is set.
	BarStyle string `json:"bar_style,omitempty"` // type=context|limit_5h|limit_7d

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

	// BarFG overrides the foreground of the bar glyphs only, independent
	// of the segment FG and the percentage colour, on bar-drawing
	// segments (context, limit_5h, limit_7d). BarThresholds is the
	// reactive variant: a percentage-keyed palette (highest matching Min
	// wins) that overrides BarFG. Together they let a dimmed bar sit next
	// to a bright, threshold-reactive number. The bar wrap closes back to
	// the segment's ambient FG, so it composes with both threshold_target
	// modes. Empty (the default) leaves the bar in the ambient colour.
	BarFG         string      `json:"bar_fg,omitempty"`
	BarThresholds []Threshold `json:"bar_thresholds,omitempty"`

	// LabelFG overrides the foreground of the label prefix only (e.g. a
	// limit segment's "5h:" / "7d:"), independent of the bar and the
	// number; LabelThresholds is the reactive variant. Enables e.g. a
	// grey label beside a colour-reactive bar. Empty leaves the label in
	// the ambient colour.
	LabelFG         string      `json:"label_fg,omitempty"`
	LabelThresholds []Threshold `json:"label_thresholds,omitempty"`

	// Align, when "right", anchors this segment (and every following
	// segment in the same row) to the right edge of the usable width.
	// The slack between the preceding left-group and the first
	// right-aligned segment becomes padding. In Powerline mode the
	// padding inherits the bg of the last left-aligned visible
	// segment so it reads as a visual extension of that segment.
	// Degrades to inline (no padding) when terminal width is unknown
	// or the row already overflows. Unknown values are ignored
	// (treated as left).
	Align string `json:"align,omitempty"`

	// Wrap, when true, marks this segment as eligible for row-overflow
	// reflow. If the containing row's visible content would exceed the
	// usable width (ttyCols - 2*margin, minus the cap columns when
	// Caps is active), every wrap-marked segment is extracted into a
	// new row inserted directly after the original. The new row
	// inherits the parent row's Bg, Palette, Caps, and Powerline mode;
	// palette rotation restarts at index 0. Reflow degrades to a
	// no-op (segments stay inline) when ttyCols is unknown or the
	// row already fits.
	Wrap bool `json:"wrap,omitempty"`

	// MaxWidth, when positive, caps the visible column width of this
	// segment's rendered body. Bodies longer than MaxWidth are
	// shortened to MaxWidth-1 columns and suffixed with "…" (U+2026
	// HORIZONTAL ELLIPSIS); shorter bodies pass through unchanged.
	// Zero (the default) and negative values disable truncation.
	// Truncation runs on the segment's plain body BEFORE the style()
	// wrap, so it is safe to apply to text-only segments
	// (text, cwd, git_branch, model, …). Segments that embed internal
	// ANSI escape sequences for sub-styling (e.g. context, limit_5h,
	// limit_7d when threshold_target is "pct") should leave MaxWidth
	// at 0 — truncating across an embedded escape can leave the
	// terminal in an unintended SGR state.
	MaxWidth int `json:"max_width,omitempty"`

	// MinCols, when positive, suppresses this segment when the
	// detected terminal width is narrower than MinCols. The hidden
	// segment behaves exactly like an empty render: no body, no
	// chevron, no palette slot. The gate runs before the segment
	// function fires, so hidden segments cost nothing. When the
	// terminal width is unknown (ttyCols == 0) MinCols is ignored —
	// "no info to gate on" defaults to "keep the segment". Zero
	// (the default) and negative values disable the gate.
	MinCols int `json:"min_cols,omitempty"`

	// Shrink, when true, marks this segment as the one that yields
	// display width when its row would overflow. After reflow, a
	// pre-pass (applyShrink) measures the row; if the visible content
	// exceeds the usable width (ttyCols - 2*margin, minus cap columns
	// when Caps is active), every Shrink-marked segment has its
	// effective MaxWidth lowered — in row order, each yielding down to
	// a 1-column floor (the "…" glyph) — until the deficit is covered.
	// The truncation itself reuses the MaxWidth path (renderSegment →
	// truncateToWidth), so it is safe only on text-style segments
	// (cwd, git_branch, text, model, …) for the same reason MaxWidth is.
	// A user-supplied MaxWidth is only ever lowered further, never
	// raised. When ttyCols is unknown (0) or the row already fits, the
	// flag is a no-op. Lets the right-aligned version stamp stay intact
	// on narrow terminals while cwd shortens to absorb the overflow.
	Shrink bool `json:"shrink,omitempty"`

	// Scope selects which repository the git_branch segment reports when cwd
	// is inside a submodule working tree. "local" (the default, also "" and
	// any unknown value) reports the nearest repository — the submodule
	// itself. "toplevel" reports the outermost superproject of the submodule
	// chain. Ignored by all other segment types.
	Scope string `json:"scope,omitempty"`
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
	// Version is the running ccsb version string, forwarded to the
	// "version" segment type. Empty string hides the segment.
	Version string
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
	SessionID     string    `json:"session_id"`
	SessionName   string    `json:"session_name"`
	Model         modelF    `json:"model"`
	Workspace     workspace `json:"workspace"`
	OutputStyle   outputF   `json:"output_style"`
	Effort        effortF   `json:"effort"`
	Cost          costF     `json:"cost"`
	Context       contextF  `json:"context_window"`
	Limits        limitsF   `json:"rate_limits"`
	FastMode      bool      `json:"fast_mode"`
	Thinking      thinkF    `json:"thinking"`
	Exceeds200kT  bool      `json:"exceeds_200k_tokens"`
	SchemaVersion string    `json:"schema_version"`
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

// parseErrors collects the parse failures detected by parsePayload when
// the inbound JSON is unmarshalled in per-segment-isolated mode. The
// segregation prevents one broken field from killing the rest of the
// payload — only that field's segment loses its data.
//
//   - topLevel is non-nil when the raw bytes are not a JSON object at
//     all (syntax error, top-level array, etc.). In that case
//     fieldErrors is meaningless because no per-field unmarshal was
//     attempted.
//   - fieldErrors carries one entry per known top-level key whose
//     individual unmarshal returned an error. A missing key is NOT an
//     error — it lands in the payload as a zero-value, and the
//     segment renderer hides itself based on its own data-presence
//     check.
type parseErrors struct {
	topLevel    error
	fieldErrors map[string]error
}

// hasIssue reports whether parsePayload saw any kind of trouble.
// Used by detectSchemaIssue to decide whether the schema_health
// indicator should fire.
func (e parseErrors) hasIssue() bool {
	return e.topLevel != nil || len(e.fieldErrors) > 0
}

// expectedPayloadKeys lists every top-level JSON key parsePayload
// knows how to extract. It is the canonical contract between ccsb and
// Claude Code's statusLine payload: every key listed here is parsed
// individually by parsePayload, every key NOT listed is silently
// ignored as an additive schema extension. The list must stay in
// sync with the field() calls inside parsePayload — drift is caught
// by TestExpectedPayloadKeys_MatchesParsePayload, which feeds a
// payload containing every key from this list and asserts the parser
// reports no surprises.
//
// External consumers (ccsb doctor) use ExpectedPayloadKeys() to
// compare a real capture against this contract.
var expectedPayloadKeys = []string{
	"session_id",
	"session_name",
	"model",
	"workspace",
	"output_style",
	"effort",
	"cost",
	"context_window",
	"rate_limits",
	"fast_mode",
	"thinking",
	"exceeds_200k_tokens",
	"schema_version",
}

// ExpectedPayloadKeys returns a fresh copy of the top-level JSON keys
// that parsePayload extracts. Callers can compare this list against a
// real capture's actual top-level keys to spot missing or additive
// schema drift.
func ExpectedPayloadKeys() []string {
	out := make([]string, len(expectedPayloadKeys))
	copy(out, expectedPayloadKeys)
	return out
}

// parsePayload unmarshals raw using per-segment isolation: the bytes
// are first decoded into a map[string]json.RawMessage, then each
// known top-level key is unmarshalled into its specific destination
// field individually. A type error in one field stops at that field —
// the rest of the payload still populates normally, and only the
// broken segment loses its data.
//
// Missing top-level keys are left at the destination's zero value
// and do NOT show up in fieldErrors; segment renderers hide
// themselves on zero-value data via their own checks.
//
// The list of keys this function recognises is duplicated in
// expectedPayloadKeys for ccsb doctor's schema-check; the two
// lists must stay in sync (enforced by test).
func parsePayload(raw []byte) (payload, parseErrors) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return payload{}, parseErrors{topLevel: err}
	}
	var p payload
	errs := parseErrors{}
	field := func(key string, dst any) {
		bytes, ok := top[key]
		if !ok {
			return
		}
		if err := json.Unmarshal(bytes, dst); err != nil {
			if errs.fieldErrors == nil {
				errs.fieldErrors = make(map[string]error)
			}
			errs.fieldErrors[key] = err
		}
	}
	field("session_id", &p.SessionID)
	field("session_name", &p.SessionName)
	field("model", &p.Model)
	field("workspace", &p.Workspace)
	field("output_style", &p.OutputStyle)
	field("effort", &p.Effort)
	field("cost", &p.Cost)
	field("context_window", &p.Context)
	field("rate_limits", &p.Limits)
	field("fast_mode", &p.FastMode)
	field("thinking", &p.Thinking)
	field("exceeds_200k_tokens", &p.Exceeds200kT)
	field("schema_version", &p.SchemaVersion)
	return p, errs
}

// SchemaVersionOf extracts payload.schema_version from raw without
// running the full parsePayload (which fails fast on a top-level
// non-object). It is exported so statusline can persist and diff
// the value across invocations without re-implementing the parse.
//
// Returns "" when raw is not a JSON object or the field is absent
// or empty.
func SchemaVersionOf(raw []byte) string {
	var loose struct {
		SchemaVersion string `json:"schema_version"`
	}
	_ = json.Unmarshal(raw, &loose)
	return loose.SchemaVersion
}

// Diagnostic summarises everything ccsb noticed about a raw inbound
// JSON payload: top-level parse trouble, per-field unmarshal errors,
// missing critical fields, and additive top-level keys ccsb does not
// (yet) consume. It is the public face of the schema-robustness
// ladder — fed by parsePayload and consumed by schema_health, ccsb
// doctor, and the 0.2.19 drift logger.
type Diagnostic struct {
	// TopLevelError is non-nil when the bytes are not a JSON object.
	// All other fields are zero in that case because no per-field
	// inspection could run.
	TopLevelError error
	// FieldErrors carries per-field unmarshal errors keyed by the
	// top-level JSON key.
	FieldErrors map[string]error
	// MissingCritical lists the three always-required fields that
	// came in empty: "session_id", "model.display_name",
	// "workspace.current_dir".
	MissingCritical []string
	// AdditiveKeys are top-level JSON keys present in the payload
	// that ccsb does not know about. Informational only — does NOT
	// count as Issue() so that harmless schema evolution (Claude
	// Code growing a new field) does not trigger the schema_health
	// indicator. ccsb doctor surfaces these as "additive keys"; the
	// drift logger includes them in .diag for traceability.
	AdditiveKeys []string
}

// Diagnose inspects raw and returns everything notable about its
// shape relative to ccsb's renderer expectations.
func Diagnose(raw []byte) Diagnostic {
	p, errs := parsePayload(raw)
	d := Diagnostic{
		TopLevelError: errs.topLevel,
		FieldErrors:   errs.fieldErrors,
	}
	if errs.topLevel != nil {
		return d
	}
	if p.SessionID == "" {
		d.MissingCritical = append(d.MissingCritical, "session_id")
	}
	if p.Model.DisplayName == "" {
		d.MissingCritical = append(d.MissingCritical, "model.display_name")
	}
	if p.Workspace.CurrentDir == "" {
		d.MissingCritical = append(d.MissingCritical, "workspace.current_dir")
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err == nil {
		expected := make(map[string]bool, len(expectedPayloadKeys))
		for _, k := range expectedPayloadKeys {
			expected[k] = true
		}
		for k := range top {
			if !expected[k] {
				d.AdditiveKeys = append(d.AdditiveKeys, k)
			}
		}
		sort.Strings(d.AdditiveKeys)
	}
	return d
}

// Issue reports whether the diagnostic represents a real schema
// problem worth flagging via the schema_health indicator and worth
// logging as a .diag file. AdditiveKeys deliberately do NOT count —
// they describe harmless schema evolution, not breakage.
func (d Diagnostic) Issue() bool {
	return d.TopLevelError != nil || len(d.FieldErrors) > 0 || len(d.MissingCritical) > 0
}

// Format renders the diagnostic as plain text for human inspection.
// The 0.2.19 drift logger writes this output next to the matching
// capture as a .diag file when Issue() is true. The format is
// stable line-oriented text — easy to grep, paste into bug
// reports, or diff between captures.
func (d Diagnostic) Format() []byte {
	var b strings.Builder
	b.WriteString("ccsb schema diagnostic\n")
	if d.TopLevelError != nil {
		fmt.Fprintf(&b, "top-level parse error: %s\n", d.TopLevelError)
		return []byte(b.String())
	}
	if len(d.MissingCritical) > 0 {
		fmt.Fprintf(&b, "missing critical fields: %s\n", strings.Join(d.MissingCritical, ", "))
	}
	if len(d.FieldErrors) > 0 {
		keys := make([]string, 0, len(d.FieldErrors))
		for k := range d.FieldErrors {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString("per-field parse errors:\n")
		for _, k := range keys {
			fmt.Fprintf(&b, "  %s: %s\n", k, d.FieldErrors[k])
		}
	}
	if len(d.AdditiveKeys) > 0 {
		fmt.Fprintf(&b, "additive keys: %s\n", strings.Join(d.AdditiveKeys, ", "))
	}
	return []byte(b.String())
}

// nowFunc returns the current time. Tests swap this for a fixed clock so
// countdown formatting is deterministic.
var nowFunc = time.Now

// defaultConfig is applied when Config.Rows is empty: the full
// out-of-the-box look (Powerline with round end caps, solid chevrons,
// single 9-grey monotonic-dark→light global palette with a stride of
// 2 per output row, threshold-coloured percentage digits, right-aligned
// version stamp). Top-level scalars (Margin, Width, Separator) are left
// to the caller's Config so test fixtures that pin those values
// continue to work. The shared Palette plus per-row paletteStart
// guarantees every output row (including reflow-inserted rows) starts
// exactly two shades brighter than the one before it.
var defaultConfig = Config{
	Powerline:      true,
	PowerlineStyle: "solid",
	CapStyle:       "round",
	Palette:        defaultGlobalPalette,
	PaletteStride:  defaultPaletteStride,
	Rows: []Row{
		{
			Caps: true,
			Segments: []Segment{
				{Type: "model", FG: "33", Bold: true, Show1MFlag: true},
				{Type: "mode"},
				{Type: "context", FG: "245", Style: "bar+pct", ThresholdTarget: "pct", Thresholds: []Threshold{
					{Min: 70, FG: "136"},
					{Min: 90, FG: "160"},
				}},
				{Type: "limit_5h", FG: "245", Style: "bar+pct", BarWidth: 8, Wrap: true, ThresholdTarget: "pct", Thresholds: []Threshold{
					{Min: 70, FG: "136"},
					{Min: 90, FG: "160"},
				}},
				{Type: "limit_7d", FG: "245", Style: "bar+pct", BarWidth: 8, Wrap: true, ThresholdTarget: "pct", Thresholds: []Threshold{
					{Min: 70, FG: "136"},
					{Min: 90, FG: "160"},
				}},
				// schema_health: hidden when env.schemaIssue is false, so
				// it costs no palette slot and no chevron. When the
				// indicator fires it paints itself as a dark-red block
				// (bg=52) with a bright-red bold skull (fg=160), pulled
				// to the right edge to break the monotonic grey streak —
				// the visual alarm the user explicitly asked for.
				{Type: "schema_health", FG: "160", BG: "52", Bold: true, Align: "right"},
			},
		},
		{
			Caps: true,
			Segments: []Segment{
				{Type: "git_branch", FG: "33"},
				{Type: "lines", FG: "245"},
				{Type: "cwd", FG: "245", Shrink: true},
				{Type: "version", FG: "245", Align: "right"},
			},
		},
	},
}

const defaultSeparator = " | "

const (
	powerlineChevronWidth = 1
	// powerlineSeparatorWidth is the per-join layout cost in display
	// columns: one space, the chevron glyph, one space.
	powerlineSeparatorWidth = powerlineChevronWidth + 2

	// Style identifier for Config.PowerlineStyle. "thin" / "" /
	// unknown fall through to the default glyph in pickGlyph.
	powerlineStyleSolid = "solid"

	// Glyphs used by pickGlyph based on the style identifier.
	powerlineThinGlyph  = "" // U+E0B1 RIGHT TRIANGLE LINE
	powerlineSolidGlyph = "" // U+E0B0 RIGHT TRIANGLE FILL

	// Cap-style identifiers for Config.CapStyle. "round" / "" /
	// unknown fall through to the round cap pair in pickCapGlyphs.
	capStyleSquare = "square"
	capStyleSlant  = "slant"

	// Round caps: filled half-circles.
	powerlineLeftCapRound  = "" // U+E0B6 LEFT HALF CIRCLE THICK
	powerlineRightCapRound = "" // U+E0B4 RIGHT HALF CIRCLE THICK

	// Slant caps: filled triangles.
	powerlineLeftCapSlant  = "" // U+E0BC UPPER-LEFT TRIANGLE FILLED
	powerlineRightCapSlant = "" // U+E0BA LOWER-RIGHT TRIANGLE FILLED

	// defaultSameBgChevronFG is the chevron foreground when the two
	// adjacent segments share the same effective bg (no real
	// transition). Preserves the 0.2.0 visual for legacy uniform-bg
	// configs.
	defaultSameBgChevronFG = "245"
)

// capGlyphs carries the (left, right) glyph pair for a cap style.
// Empty strings are the "square" sentinel: emit a 1-col bg-painted
// space instead of a glyph.
type capGlyphs struct {
	left, right string
}

// pickCapGlyphs maps Config.CapStyle to its (left, right) glyph
// pair. Empty/unknown styles fall back to round.
func pickCapGlyphs(style string) capGlyphs {
	switch style {
	case capStyleSquare:
		return capGlyphs{} // sentinel: square path
	case capStyleSlant:
		return capGlyphs{left: powerlineLeftCapSlant, right: powerlineRightCapSlant}
	default: // round, "", or unknown
		return capGlyphs{left: powerlineLeftCapRound, right: powerlineRightCapRound}
	}
}

// Legacy defaultPalette removed in 0.2.32: when Row.Palette / Row.Bg
// are unset the resolution now falls through to env.globalPalette
// (Config.Palette, with defaultGlobalPalette as the package-level
// fallback). The per-output-row paletteStart is computed in Render
// after expandWrappedRows so the gradient stays monotonic across
// reflowed layouts.

// pickGlyph maps Config.PowerlineStyle to its chevron glyph.
// Unknown or empty values yield the thin glyph.
func pickGlyph(style string) string {
	if style == powerlineStyleSolid {
		return powerlineSolidGlyph
	}
	return powerlineThinGlyph
}

// effectiveSegmentBg returns the background colour for a given segment
// at a given visible-segment position. Priority ladder:
//
//  1. Segment.BG (explicit per-segment override)
//  2. Row.Palette[visibleIndex % len] (per-row rotation, starts at 0)
//  3. Row.Bg (uniform row fill)
//  4. globalPalette[(paletteStart + visibleIndex) % len] when
//     powerlineActive is true and globalPalette is non-empty
//  5. "" (no bg, natural-mode fallback)
//
// visibleIndex is the rank of the segment among the row's non-empty
// segments — empty segments do not claim a palette slot.
// paletteStart shifts the global-palette rotation per output row so
// each row starts a configurable number of shades brighter than the
// previous one; it has no effect when Row.Palette is set.
func effectiveSegmentBg(row Row, seg Segment, visibleIndex int, globalPalette []string, paletteStart int, powerlineActive bool) string {
	if seg.BG != "" {
		return seg.BG
	}
	if n := len(row.Palette); n > 0 {
		return row.Palette[visibleIndex%n]
	}
	if row.Bg != "" {
		return row.Bg
	}
	if powerlineActive && len(globalPalette) > 0 {
		return globalPalette[(paletteStart+visibleIndex)%len(globalPalette)]
	}
	return ""
}

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

// hasAnyWrap reports whether any segment in segs carries the Wrap
// flag. Cheap pre-check for the reflow logic so rows without any
// wrap-eligible segments skip the more expensive width measurement.
func hasAnyWrap(segs []Segment) bool {
	for _, s := range segs {
		if s.Wrap {
			return true
		}
	}
	return false
}

// shrinkFloor is the minimum MaxWidth an applyShrink candidate is reduced
// to — one column, which truncateToWidth renders as the single "…" glyph.
const shrinkFloor = 1

// hasAnyShrink reports whether any segment carries the Shrink flag.
// Cheap pre-check so applyShrink can skip the per-segment render loop
// on rows with nothing to shrink.
func hasAnyShrink(segs []Segment) bool {
	for _, s := range segs {
		if s.Shrink {
			return true
		}
	}
	return false
}

// applyShrink returns row.Segments unchanged, or — when the row's
// visible content would overflow the usable width AND the row carries
// at least one Shrink-marked segment — a copy in which each Shrink
// segment's effective MaxWidth is lowered (in row order, flooring at
// shrinkFloor) until the overflow deficit is absorbed. The actual
// truncation happens later in renderSegment → truncateToWidth; this
// function only computes and stamps the cap.
//
// Measurement mirrors rowOverflows exactly (segment body widths via
// displayWidth, which strips ANSI; per-style separator cost; cap
// columns when powerlineActive && Caps), so the gap-free width here
// matches the real overflow condition — when used > usable a
// right-aligned segment's padding gap is already zero. Like
// rowOverflows, the separator term counts visible-1 joins; a
// natural-mode row with a right-aligned segment actually renders one
// fewer separator, so the estimate is conservative there (it may shrink
// a few columns more than strictly necessary, never fewer — the
// protected segment always stays intact).
//
// When ttyCols is unknown (0), no Shrink segment exists, only one
// segment is visible, or the row already fits, the input slice is
// returned untouched. A pre-existing MaxWidth is only ever lowered,
// never raised, because the measured width already reflects it and the
// new cap is strictly smaller.
func applyShrink(p *payload, row Row, env renderEnv, powerlineActive bool, sep string) []Segment {
	if env.ttyCols == 0 || !hasAnyShrink(row.Segments) {
		return row.Segments
	}
	type cand struct {
		idx   int
		width int
	}
	var cands []cand
	used := 0
	visible := 0
	for i, seg := range row.Segments {
		body := renderSegment(p, seg, env)
		if body == "" {
			continue
		}
		w := displayWidth(body)
		used += w
		visible++
		if seg.Shrink {
			cands = append(cands, cand{idx: i, width: w})
		}
	}
	if len(cands) == 0 || visible <= 1 {
		return row.Segments
	}
	if powerlineActive {
		used += (visible - 1) * powerlineSeparatorWidth
	} else {
		used += (visible - 1) * displayWidth(sep)
	}
	usable := env.ttyCols - 2*env.margin
	if powerlineActive && row.Caps {
		usable -= 2
	}
	deficit := used - usable
	if deficit <= 0 {
		return row.Segments
	}
	out := make([]Segment, len(row.Segments))
	copy(out, row.Segments)
	for _, c := range cands {
		if deficit <= 0 {
			break
		}
		canGive := c.width - shrinkFloor
		if canGive <= 0 {
			continue
		}
		take := min(canGive, deficit)
		out[c.idx].MaxWidth = c.width - take
		deficit -= take
	}
	return out
}

// rowOverflows reports whether the row's visible rendered width would
// exceed the usable column count (ttyCols - 2*margin, minus cap
// columns when Caps is active). Returns false when ttyCols is 0
// (no measurement possible) so reflow degrades to a no-op.
//
// The width estimate sums each visible segment's body width, the
// per-style separator cost between visible pairs (powerline chevron
// triplet or natural separator string), and approximates the two
// cap glyphs as 1 col each when Caps is active. It does NOT account
// for the right-align padding gap; a row that uses right-align AND
// reflow is unusual and the estimate stays conservative.
func rowOverflows(p *payload, row Row, env renderEnv, powerlineActive bool, sep string) bool {
	if env.ttyCols == 0 {
		return false
	}
	used := 0
	visible := 0
	for _, seg := range row.Segments {
		body := renderSegment(p, seg, env)
		if body == "" {
			continue
		}
		used += displayWidth(body)
		visible++
	}
	if visible <= 1 {
		return false
	}
	if powerlineActive {
		used += (visible - 1) * powerlineSeparatorWidth
	} else {
		used += (visible - 1) * displayWidth(sep)
	}
	usable := env.ttyCols - 2*env.margin
	if powerlineActive && row.Caps {
		usable -= 2
	}
	return used > usable
}

// splitWrap partitions row.Segments into a left group (segments
// without the Wrap flag) and a right group (segments with Wrap=true).
// The returned (left, wrapped) pair shares the parent row's Bg,
// Palette, Caps, and Align fields. The Wrap flag is cleared on the
// moved segments so the new row never re-triggers reflow on a
// further pass.
func splitWrap(row Row) (Row, Row) {
	leftSegs := make([]Segment, 0, len(row.Segments))
	wrapSegs := make([]Segment, 0)
	for _, s := range row.Segments {
		if s.Wrap {
			s2 := s
			s2.Wrap = false
			wrapSegs = append(wrapSegs, s2)
		} else {
			leftSegs = append(leftSegs, s)
		}
	}
	left := row
	left.Segments = leftSegs
	wrapped := row
	wrapped.Segments = wrapSegs
	return left, wrapped
}

// expandWrappedRows walks the configured rows and, for each row that
// overflows AND contains at least one wrap-marked segment, splits the
// row into a left half (non-wrap segments) and a right half (wrap
// segments) inserted directly after it. Rows that fit, rows without
// any Wrap segments, and right-aligned rows (whose reflow behaviour
// would be ambiguous) pass through unchanged.
func expandWrappedRows(p *payload, rows []Row, env renderEnv, powerlineActive bool, sep string) []Row {
	out := make([]Row, 0, len(rows))
	for _, row := range rows {
		if row.Align == "right" {
			out = append(out, row)
			continue
		}
		if !hasAnyWrap(row.Segments) {
			out = append(out, row)
			continue
		}
		if !rowOverflows(p, row, env, powerlineActive, sep) {
			out = append(out, row)
			continue
		}
		left, wrapped := splitWrap(row)
		out = append(out, left, wrapped)
	}
	return out
}

// Render parses raw, walks Config.Rows, and returns the joined multi-line
// output. An empty Config.Rows triggers defaultConfig (rows plus
// Powerline/PowerlineStyle/CapStyle so the out-of-the-box look matches
// the documented default). A global JSON-parse failure falls back to a
// hardcoded "<model> · <cwd>" so Claude Code never gets an empty
// statusLine.
func Render(opts Options, raw []byte) (string, error) {
	// Per-segment isolated parse: a type error in any one top-level
	// field is contained — only that field's segment loses its data,
	// the rest of the payload still renders. parseErrors carries both
	// the top-level failure (if any) and the per-field failures so
	// detectSchemaIssue can decide whether the indicator should fire.
	p, parseErrs := parsePayload(raw)
	schemaIssue := detectSchemaIssue(&p, parseErrs)

	cfg := opts.Config
	usingDefault := len(cfg.Rows) == 0
	if usingDefault {
		cfg.Rows = defaultConfig.Rows
		cfg.Powerline = defaultConfig.Powerline
		cfg.PowerlineStyle = defaultConfig.PowerlineStyle
		cfg.CapStyle = defaultConfig.CapStyle
		if len(cfg.Palette) == 0 {
			cfg.Palette = defaultConfig.Palette
		}
		if cfg.PaletteStride == 0 {
			cfg.PaletteStride = defaultConfig.PaletteStride
		}
	}
	rows := cfg.Rows
	sep := cfg.Separator
	if sep == "" {
		sep = defaultSeparator
	}

	cwd := opts.Cwd
	if cwd == "" {
		cwd = p.Workspace.CurrentDir
	}

	globalPalette := cfg.Palette
	if len(globalPalette) == 0 {
		globalPalette = defaultGlobalPalette
	}
	env := renderEnv{
		cwd:            cwd,
		colorEnabled:   !opts.NoColor,
		nowUnix:        nowFunc().Unix(),
		powerlineStyle: cfg.PowerlineStyle,
		capStyle:       cfg.CapStyle,
		schemaIssue:    schemaIssue,
		globalPalette:  globalPalette,
	}
	env.ttyCols, env.ttyRows = discoverTermSize(cfg)
	env.margin = cfg.effectiveMargin()
	env.version = opts.Version
	// Degrade gracefully if the terminal is narrower than 2*margin
	// — keep at least one column of usable bg-fill width.
	if env.ttyCols > 0 && env.ttyCols <= 2*env.margin {
		env.margin = 0
	}

	// Responsive row overflow (reflow): rows that overflow ttyCols
	// AND contain at least one Wrap-marked segment are split into
	// two — non-wrap segments stay in place, wrap segments move into
	// a new row inserted directly after the original. The new row
	// inherits Bg/Palette/Caps from its parent. The expansion runs
	// before per-row dispatch so the downstream renderers see only
	// already-split rows and need no awareness of wrap.
	powerlineActive := cfg.Powerline && env.colorEnabled
	rows = expandWrappedRows(&p, rows, env, powerlineActive, sep)

	// paletteStart for each output row is computed AFTER reflow so a
	// wrap-inserted row claims its own offset (= its output index
	// times the stride) and continues the monotonic dark→light
	// gradient instead of restarting at the parent row's offset.
	stride := cfg.effectivePaletteStride()
	var lines []string
	for outputIdx, row := range rows {
		rowEnv := env
		rowEnv.paletteStart = outputIdx * stride
		row.Segments = applyShrink(&p, row, rowEnv, powerlineActive, sep)
		var line string
		switch {
		case row.Align == "right":
			line = renderRowRight(&p, row, rowEnv, sep)
		case cfg.Powerline && env.colorEnabled:
			line = renderRowPowerline(&p, row, rowEnv)
		default:
			line = renderRowNatural(&p, row, rowEnv, sep)
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 && usingDefault {
		// Default layout produced nothing — e.g. `{}` payload where
		// every default segment hides when its data is missing.
		// Fall through to the last-resort line so the bar is never
		// blank. A user-supplied config that intentionally renders
		// empty stays empty. When the top-level parse failed, p is
		// zero and we pass nil to opt back into the relaxed
		// second-pass parse inside lastResort.
		var lrPayload *payload
		if parseErrs.topLevel == nil {
			lrPayload = &p
		}
		return lastResort(opts, lrPayload, raw), nil
	}
	return strings.Join(lines, "\n"), nil
}

// renderRowNatural is the 0.1.x row builder: render each non-empty
// segment, join them with the configured separator, then prepend any
// configured margin. Returns "" when every segment renders empty so
// the row joiner skips the row.
//
// Per-segment Align="right" splits the row into a left group and a
// right group; the first right-aligned segment marks the split point
// and every later segment joins the right group regardless of its
// own Align. The gap between groups is padded with spaces so the
// right group ends flush with the usable width (ttyCols - 2*margin).
// Degrades to the inline join (no padding) when ttyCols is unknown
// or the row already overflows the usable width.
func renderRowNatural(p *payload, row Row, env renderEnv, sep string) string {
	type renderedPart struct {
		body  string
		align string
	}
	var parts []renderedPart
	for _, seg := range row.Segments {
		s := renderSegment(p, seg, env)
		if s == "" {
			continue
		}
		parts = append(parts, renderedPart{body: s, align: seg.Align})
	}
	if len(parts) == 0 {
		return ""
	}

	splitIdx := len(parts)
	for i, p := range parts {
		if p.align == "right" {
			splitIdx = i
			break
		}
	}

	prefix := strings.Repeat(" ", env.margin)

	if splitIdx == len(parts) {
		bodies := make([]string, len(parts))
		for i, p := range parts {
			bodies[i] = p.body
		}
		return prefix + strings.Join(bodies, sep)
	}

	leftBodies := make([]string, splitIdx)
	for i := range splitIdx {
		leftBodies[i] = parts[i].body
	}
	rightBodies := make([]string, len(parts)-splitIdx)
	for i := splitIdx; i < len(parts); i++ {
		rightBodies[i-splitIdx] = parts[i].body
	}
	leftJoined := strings.Join(leftBodies, sep)
	rightJoined := strings.Join(rightBodies, sep)

	if env.ttyCols <= 0 {
		bodies := append(leftBodies, rightBodies...)
		return prefix + strings.Join(bodies, sep)
	}
	usable := env.ttyCols - 2*env.margin
	pad := usable - displayWidth(leftJoined) - displayWidth(rightJoined)
	if pad <= 0 {
		bodies := append(leftBodies, rightBodies...)
		return prefix + strings.Join(bodies, sep)
	}
	return prefix + leftJoined + strings.Repeat(" ", pad) + rightJoined
}

// renderRowRight renders segments as plain text, right-justified within the
// usable terminal width (ttyCols - 2*margin). When ttyCols is unknown (0)
// it degrades to margin + content. Powerline is intentionally bypassed so
// right-aligned rows always render as neutral plain text.
func renderRowRight(p *payload, row Row, env renderEnv, sep string) string {
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
	content := strings.Join(parts, sep)
	prefix := strings.Repeat(" ", env.margin)
	if env.ttyCols <= 0 {
		return prefix + content
	}
	usable := env.ttyCols - 2*env.margin
	pad := max(usable-displayWidth(content), 0)
	return prefix + strings.Repeat(" ", pad) + content
}

// renderEnv carries per-call state from Render to each segment renderer.
// Segment functions read these fields but never mutate them.
type renderEnv struct {
	cwd            string   // resolved cwd (Options.Cwd or payload.Workspace.CurrentDir)
	colorEnabled   bool     // false when NoColor was set on Options
	nowUnix        int64    // wall clock at the start of Render, for time-based segments
	ttyCols        int      // detected terminal columns, 0 when unknown
	ttyRows        int      // detected terminal rows, 0 when unknown
	margin         int      // plain leading spaces per row; usable bg-fill width = ttyCols - 2*margin
	powerlineStyle string   // "thin" (default) | "solid"; used by renderRowPowerline via pickGlyph
	capStyle       string   // "" / "round" / "square" / "slant"; "" → round
	version        string   // ccsb version forwarded from Options.Version
	schemaIssue    bool     // true when the inbound payload looks broken; drives the schema_health segment
	globalPalette  []string // Config.Palette (or defaultGlobalPalette when unset); see effectiveSegmentBg
	paletteStart   int      // offset into globalPalette for the current output row (= rowOutputIndex * stride)
}

// detectSchemaIssue returns true when the inbound JSON payload looks
// broken enough to warrant the visible "schema health" indicator. It
// fires when:
//
//   - the top-level JSON parse failed outright (errs.topLevel != nil), or
//   - one of the per-field unmarshals returned a type error
//     (len(errs.fieldErrors) > 0) — a real schema regression that
//     0.2.16's coarse top-level check could not see, and
//   - one of the three critical fields ccsb always expects from Claude
//     Code is empty: session_id, model.display_name,
//     workspace.current_dir.
//
// Optional fields that legitimately arrive empty during the first
// status updates of a session (cost, rate_limits, context_window) are
// still NOT checked here — they only contribute to detection when a
// type error was seen.
func detectSchemaIssue(p *payload, errs parseErrors) bool {
	if errs.hasIssue() {
		return true
	}
	if p.SessionID == "" || p.Model.DisplayName == "" || p.Workspace.CurrentDir == "" {
		return true
	}
	return false
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
	// MinCols gate runs first — a suppressed segment costs nothing
	// (no body, no chevron, no palette slot). When ttyCols is unknown
	// the gate is bypassed so we never hide a segment based on
	// guesswork.
	if s.MinCols > 0 && env.ttyCols > 0 && env.ttyCols < s.MinCols {
		return ""
	}
	fn, ok := segmentFuncs[s.Type]
	if !ok {
		return "?" + s.Type + "?"
	}
	out := fn(p, s, env)
	if out == "" {
		return ""
	}
	// MaxWidth truncation runs on the raw body BEFORE the style()
	// wrap. Safe on plain-text bodies (text, cwd, git_branch, model,
	// …); segments that self-embed ANSI escapes for sub-styling
	// (context/limit_* with threshold_target=pct) should leave
	// MaxWidth at 0 — runewidth.Truncate is not ANSI-aware and a
	// mid-escape cut would leave the terminal in an unintended SGR
	// state.
	if s.MaxWidth > 0 {
		out = truncateToWidth(out, s.MaxWidth)
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

// truncateToWidth shortens s to at most max display columns, suffixing
// with "…" (U+2026) when truncation actually happens. Returns s
// unchanged when it already fits or when max <= 0. The width
// accounting goes through go-runewidth so emoji, CJK fullwidth, and
// zero-width joiners are handled correctly; ANSI escape sequences in
// s would NOT be accounted for and may be split mid-escape — callers
// must avoid passing pre-styled bodies here.
func truncateToWidth(s string, max int) string {
	if max <= 0 {
		return s
	}
	if runewidth.StringWidth(s) <= max {
		return s
	}
	return runewidth.Truncate(s, max, "…")
}

// chooseFG picks the foreground color for a segment, honouring
// Segment.Thresholds. Behaviour:
//
//   - No thresholds, or segment without a percentage metric, or no
//     threshold matching: returns Segment.FG verbatim.
//   - Multiple matching thresholds: the one with the highest Min wins.
//   - A threshold with FG=="" is skipped, as if absent.
//
// pickThresholdFG returns the FG of the highest matching threshold
// (Min <= pct, highest Min wins), skipping entries with an empty FG.
// When the list is empty, the segment has no percentage metric, or no
// threshold matches, it returns fallback. It is the shared core behind
// chooseFG (segment / number colour) and the per-part bar and label
// colours (BarThresholds / LabelThresholds).
func pickThresholdFG(fallback string, thresholds []Threshold, typ string, p *payload) string {
	if len(thresholds) == 0 {
		return fallback
	}
	pct, ok := segmentMetric(typ, p)
	if !ok {
		return fallback
	}
	chosen := fallback
	var chosenMin float64
	matched := false
	for _, t := range thresholds {
		if t.FG == "" || pct < t.Min {
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

// chooseFG resolves the segment's (and, under threshold_target "pct", its
// percentage's) foreground from Thresholds, falling back to the static FG.
func chooseFG(s Segment, p *payload) string {
	return pickThresholdFG(s.FG, s.Thresholds, s.Type, p)
}

// ambientFG is the foreground the outer style() wrap applies to the
// segment body: the static FG under threshold_target "pct" (the number is
// coloured internally by wrapPct), otherwise the threshold-resolved FG.
// Per-part wraps (bar, label) close back to this so untouched regions keep
// the ambient colour in both threshold_target modes.
func ambientFG(s Segment, p *payload) string {
	if s.ThresholdTarget == "pct" {
		return s.FG
	}
	return chooseFG(s, p)
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

// renderRowPowerline builds one Powerline-styled row: each visible
// segment in its effective bg, joined with chevron transitions
// coloured to bleed prev.bg onto next.bg, prepended by margin plain
// spaces, padded to (ttyCols - 2*margin). When colorEnabled is
// false the function returns "" so the caller falls back to the
// natural-mode path.
func renderRowPowerline(p *payload, row Row, env renderEnv) string {
	if !env.colorEnabled {
		return ""
	}
	visible := collectVisibleSegments(p, row, env)
	if len(visible) == 0 {
		return ""
	}

	caps := pickCapGlyphs(env.capStyle)
	hasLeftCap := row.Caps && visible[0].bg != ""
	hasRightCap := row.Caps && visible[len(visible)-1].bg != ""
	splitIdx := findAlignmentSplit(visible)
	usable, used := segmentLayoutMetrics(env, visible, hasLeftCap, hasRightCap)

	var b strings.Builder
	if env.margin > 0 {
		b.WriteString(strings.Repeat(" ", env.margin))
	}
	if hasLeftCap {
		writeLeftCap(&b, caps, visible[0].bg)
	}

	// Right-align gap: usable cols minus body+chevrons. When the row
	// would overflow (gap <= 0), the gap is skipped and the row
	// degrades to inline rendering.
	var rightAlignGap int
	if env.ttyCols > 0 && splitIdx < len(visible) {
		rightAlignGap = max(usable-used, 0)
	}

	glyph := pickGlyph(env.powerlineStyle)
	for i, seg := range visible {
		// Right-align gap inserted before the chevron at splitIdx so
		// the padding renders in the previous segment's bg (Powerline
		// continuation) and the chevron then carries prev.bg → next.bg
		// as usual. At splitIdx == 0 there is no previous bg — the
		// padding renders as plain spaces.
		if i == splitIdx && rightAlignGap > 0 {
			if i == 0 {
				b.WriteString(strings.Repeat(" ", rightAlignGap))
			} else {
				b.WriteString(bg256(visible[i-1].bg))
				b.WriteString(strings.Repeat(" ", rightAlignGap))
			}
		}
		if i == 0 {
			if seg.bg != "" {
				b.WriteString(bg256(seg.bg))
			}
		} else {
			b.WriteString(renderChevron(visible[i-1].bg, seg.bg, glyph, env.powerlineStyle))
		}
		b.WriteString(seg.body)
		if seg.bg != "" {
			b.WriteString(bg256(seg.bg)) // segment body's [0m killed bg; restore
		}
	}

	// Trailing pad to fill the usable region. Skipped when a
	// right-aligned segment exists — that slack was already consumed
	// by the gap between the left and right groups above.
	if env.ttyCols > 0 && splitIdx == len(visible) {
		if remaining := usable - used; remaining > 0 {
			b.WriteString(strings.Repeat(" ", remaining))
		}
	}

	if hasRightCap {
		writeRightCap(&b, caps, visible[len(visible)-1].bg)
	}
	b.WriteString(reset)
	return b.String()
}

// renderedSeg is one element of renderRowPowerline's working list —
// a segment whose body rendered to non-empty text, with the effective
// background it should paint on and the alignment requested by the
// source Segment.
type renderedSeg struct {
	body  string
	bg    string
	align string
}

// collectVisibleSegments renders each segment in row, drops empty
// renders, and records each survivor's body, effective bg, and align.
func collectVisibleSegments(p *payload, row Row, env renderEnv) []renderedSeg {
	var visible []renderedSeg
	for _, seg := range row.Segments {
		s := renderSegment(p, seg, env)
		if s == "" {
			continue
		}
		bg := effectiveSegmentBg(row, seg, len(visible), env.globalPalette, env.paletteStart, true)
		visible = append(visible, renderedSeg{body: s, bg: bg, align: seg.Align})
	}
	return visible
}

// findAlignmentSplit returns the index of the first right-aligned
// visible segment, or len(visible) if none exists. Once the split is
// crossed every subsequent segment joins the right group regardless
// of its own Align.
func findAlignmentSplit(visible []renderedSeg) int {
	for i, v := range visible {
		if v.align == "right" {
			return i
		}
	}
	return len(visible)
}

// segmentLayoutMetrics returns the usable col count (ttyCols minus
// margins and optional caps) and the total width consumed by the
// visible segment bodies plus the chevron separators between them.
// Both are zero when ttyCols is unknown.
func segmentLayoutMetrics(env renderEnv, visible []renderedSeg, hasLeftCap, hasRightCap bool) (usable, used int) {
	if env.ttyCols == 0 {
		return 0, 0
	}
	usable = env.ttyCols - 2*env.margin
	if hasLeftCap {
		usable--
	}
	if hasRightCap {
		usable--
	}
	for _, seg := range visible {
		used += displayWidth(seg.body)
	}
	used += (len(visible) - 1) * powerlineSeparatorWidth
	return usable, used
}

// renderChevron returns one Powerline transition: pre-space in prevBg,
// the chev-cell with the style's asymmetric (bg, fg) pair, and
// post-space in nextBg. Solid style fills the wedge in prevBg over
// nextBg; thin style draws the line in nextBg trailing prevBg. When
// prevBg == nextBg the static defaultSameBgChevronFG keeps the glyph
// visible against the uniform background.
func renderChevron(prevBg, nextBg, glyph, style string) string {
	var chevCellBg, chevFg string
	if style == powerlineStyleSolid {
		chevCellBg, chevFg = nextBg, prevBg
	} else {
		chevCellBg, chevFg = prevBg, nextBg
	}
	if prevBg == nextBg {
		chevFg = defaultSameBgChevronFG
	}
	var b strings.Builder
	b.WriteString(bg256(prevBg))
	b.WriteString(" ")
	b.WriteString(bg256(chevCellBg))
	b.WriteString(fg256(chevFg))
	b.WriteString(glyph)
	b.WriteString(reset)
	b.WriteString(bg256(nextBg))
	b.WriteString(" ")
	return b.String()
}

// writeLeftCap writes the leading end-cap to b. Square style emits a
// 1-col bg-painted space; round and slant emit a glyph in fg=firstBg
// on the default bg, opened with \x1b[49m so prior margin spaces stay
// unstyled.
func writeLeftCap(b *strings.Builder, caps capGlyphs, firstBg string) {
	if caps.left == "" {
		b.WriteString(bg256(firstBg))
		b.WriteString(" ")
		return
	}
	b.WriteString("\x1b[49m")
	b.WriteString(fg256(firstBg))
	b.WriteString(caps.left)
	b.WriteString(reset)
}

// writeRightCap writes the trailing end-cap to b. Square style emits
// a 1-col plain space that inherits the lastBg already painted by the
// final segment's bg-restore. Round and slant fully reset, then emit
// fg=lastBg + glyph on the default bg.
func writeRightCap(b *strings.Builder, caps capGlyphs, lastBg string) {
	if caps.right == "" {
		b.WriteString(" ")
		return
	}
	b.WriteString(reset)
	b.WriteString(fg256(lastBg))
	b.WriteString(caps.right)
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

// lastResort renders a single line from whatever fields we can salvage.
// Called in two situations:
//
//   - Initial json.Unmarshal of raw failed. p is nil; we run a relaxed
//     second-pass parse against raw to pick up flat strings.
//   - Render produced no segments against the default layout (e.g. `{}`
//     payload). p is the already-parsed payload from the main pass, so
//     we reuse its fields instead of unmarshaling raw a second time.
func lastResort(opts Options, p *payload, raw []byte) string {
	var model, cwd string
	if p != nil {
		model = p.Model.DisplayName
		cwd = p.Workspace.CurrentDir
	} else {
		var loose struct {
			Model     modelF    `json:"model"`
			Workspace workspace `json:"workspace"`
		}
		_ = json.Unmarshal(raw, &loose)
		model = loose.Model.DisplayName
		cwd = loose.Workspace.CurrentDir
	}
	if opts.Cwd != "" {
		cwd = opts.Cwd
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
