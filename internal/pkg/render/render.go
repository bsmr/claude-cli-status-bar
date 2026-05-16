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
}

// defaultMargin is the implicit Config.Margin when the field is unset.
const defaultMargin = 2

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
	powerlineChevronWidth = 1
	// powerlineSeparatorWidth is the per-join layout cost in display
	// columns: one space, the chevron glyph, one space.
	powerlineSeparatorWidth = powerlineChevronWidth + 2

	// Style identifiers for Config.PowerlineStyle.
	powerlineStyleThin  = "thin"
	powerlineStyleSolid = "solid"

	// Glyphs used by pickGlyph based on the style identifier.
	powerlineThinGlyph  = "" // U+E0B1 RIGHT TRIANGLE LINE
	powerlineSolidGlyph = "" // U+E0B0 RIGHT TRIANGLE FILL

	// Cap-style identifiers for Config.CapStyle.
	capStyleRound  = "round"
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

// defaultPalette rotates per visible segment when neither Row.Bg
// nor Row.Palette is configured and Powerline is enabled. Three
// subtle dark greys produce a classic alternating-Powerline look
// out of the box.
var defaultPalette = []string{"234", "236", "238"}

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
//  2. Row.Palette[visibleIndex % len] (per-row rotation)
//  3. Row.Bg (uniform row fill)
//  4. defaultPalette[visibleIndex % len] when powerlineActive is true
//  5. "" (no bg, natural-mode fallback)
//
// visibleIndex is the rank of the segment among the row's non-empty
// segments — empty segments do not claim a palette slot.
func effectiveSegmentBg(row Row, seg Segment, visibleIndex int, powerlineActive bool) string {
	if seg.BG != "" {
		return seg.BG
	}
	if n := len(row.Palette); n > 0 {
		return row.Palette[visibleIndex%n]
	}
	if row.Bg != "" {
		return row.Bg
	}
	if powerlineActive {
		return defaultPalette[visibleIndex%len(defaultPalette)]
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
		cwd:            cwd,
		colorEnabled:   !opts.NoColor,
		nowUnix:        nowFunc().Unix(),
		powerlineStyle: opts.Config.PowerlineStyle,
		capStyle:       opts.Config.CapStyle,
	}
	env.ttyCols, env.ttyRows = discoverTermSize(opts.Config)
	env.margin = opts.Config.effectiveMargin()
	env.version = opts.Version
	// Degrade gracefully if the terminal is narrower than 2*margin
	// — keep at least one column of usable bg-fill width.
	if env.ttyCols > 0 && env.ttyCols <= 2*env.margin {
		env.margin = 0
	}

	var lines []string
	for _, row := range rows {
		var line string
		switch {
		case row.Align == "right":
			line = renderRowRight(&p, row, env, sep)
		case opts.Config.Powerline && env.colorEnabled:
			line = renderRowPowerline(&p, row, env)
		default:
			line = renderRowNatural(&p, row, env, sep)
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n"), nil
}

// renderRowNatural is the 0.1.x row builder: render each non-empty
// segment, join them with the configured separator, then prepend any
// configured margin. Returns "" when every segment renders empty so
// the row joiner skips the row.
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
	joined := strings.Join(parts, sep)
	if env.margin > 0 {
		return strings.Repeat(" ", env.margin) + joined
	}
	return joined
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
	pad := usable - displayWidth(content)
	if pad < 0 {
		pad = 0
	}
	return prefix + strings.Repeat(" ", pad) + content
}

// renderEnv carries per-call state from Render to each segment renderer.
// Segment functions read these fields but never mutate them.
type renderEnv struct {
	cwd            string // resolved cwd (Options.Cwd or payload.Workspace.CurrentDir)
	colorEnabled   bool   // false when NoColor was set on Options
	nowUnix        int64  // wall clock at the start of Render, for time-based segments
	ttyCols        int    // detected terminal columns, 0 when unknown
	ttyRows        int    // detected terminal rows, 0 when unknown
	margin         int    // plain leading spaces per row; usable bg-fill width = ttyCols - 2*margin
	powerlineStyle string // "thin" (default) | "solid"; used by renderRowPowerline via pickGlyph
	capStyle       string // "" / "round" / "square" / "slant"; "" → round
	version        string // ccsb version forwarded from Options.Version
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

	// 1. Render visible segments and capture each one's effective bg.
	type renderedSeg struct {
		body string
		bg   string
	}
	var visible []renderedSeg
	for _, seg := range row.Segments {
		s := renderSegment(p, seg, env)
		if s == "" {
			continue
		}
		bg := effectiveSegmentBg(row, seg, len(visible), true)
		visible = append(visible, renderedSeg{body: s, bg: bg})
	}
	if len(visible) == 0 {
		return ""
	}

	glyph := pickGlyph(env.powerlineStyle)
	var b strings.Builder

	// 2. Leading margin: plain spaces with no bg, so Claude Code's
	//    statusLine chrome shows through.
	if env.margin > 0 {
		b.WriteString(strings.Repeat(" ", env.margin))
	}

	// 2.5. Resolve cap presence and glyphs once per row.
	caps := pickCapGlyphs(env.capStyle)
	hasLeftCap := row.Caps && visible[0].bg != ""
	hasRightCap := row.Caps && visible[len(visible)-1].bg != ""

	// 2.6. Left cap: glyph in fg=first.bg on default bg, or a 1-col
	//      bg-painted plain space for "square" style.
	if hasLeftCap {
		firstBg := visible[0].bg
		if caps.left == "" {
			// Square: 1 col of plain first.bg-painted space.
			b.WriteString(bg256(firstBg))
			b.WriteString(" ")
		} else {
			// Round / slant: glyph in fg=firstBg on default bg.
			// Use \x1b[49m (default-bg only) instead of full reset
			// because the margin spaces had no styling — there's no
			// stale fg / bold to clear here.
			b.WriteString("\x1b[49m") // default bg
			b.WriteString(fg256(firstBg))
			b.WriteString(caps.left)
			b.WriteString(reset)
		}
	}

	// 3. Walk segments, interleaving chevrons.
	for i, seg := range visible {
		if i == 0 {
			// First segment: open its bg.
			if seg.bg != "" {
				b.WriteString(bg256(seg.bg))
			}
		} else {
			// Chevron transition between visible[i-1] and visible[i].
			prevBg := visible[i-1].bg
			nextBg := seg.bg

			// Per-style asymmetric chev-cell geometry:
			//   solid (U+E0B0): chev-cell bg=next, fg=prev — the
			//     filled wedge in prev colour visually flows into
			//     next bg.
			//   thin  (U+E0B1): chev-cell bg=prev, fg=next — the
			//     line in next colour sits at the trailing edge of
			//     prev; the bg switch happens at the post-space.
			var chevCellBg, chevFg string
			if env.powerlineStyle == powerlineStyleSolid {
				chevCellBg, chevFg = nextBg, prevBg
			} else {
				chevCellBg, chevFg = prevBg, nextBg
			}
			if prevBg == nextBg {
				// Same-bg fallback: keep the glyph visible via a
				// static fg so legacy uniform-bg configs do not lose
				// the separator.
				chevFg = defaultSameBgChevronFG
			}

			// Powerline transition layout (corrects the 0.2.3 spec
			// error where both spaces rendered in nextBg):
			//   pre-space in prevBg (extends prev segment by 1 col),
			//   chev-cell in (chevCellBg, chevFg),
			//   post-space in nextBg (extends next segment by 1 col).
			b.WriteString(bg256(prevBg))
			b.WriteString(" ")
			b.WriteString(bg256(chevCellBg))
			b.WriteString(fg256(chevFg))
			b.WriteString(glyph)
			b.WriteString(reset)
			b.WriteString(bg256(nextBg))
			b.WriteString(" ")
		}
		b.WriteString(seg.body)
		if seg.bg != "" {
			b.WriteString(bg256(seg.bg)) // segment body's [0m killed bg; restore
		}
	}

	// 4. Pad to (ttyCols - 2*margin - 2*cap_cols) total visible cols
	//    of bg-fill so the optional left and right caps each occupy
	//    1 col within the usable region.
	if env.ttyCols > 0 {
		usableCols := env.ttyCols - 2*env.margin
		if hasLeftCap {
			usableCols--
		}
		if hasRightCap {
			usableCols--
		}
		used := 0
		for _, seg := range visible {
			used += displayWidth(seg.body)
		}
		used += (len(visible) - 1) * powerlineSeparatorWidth
		if remaining := usableCols - used; remaining > 0 {
			b.WriteString(strings.Repeat(" ", remaining))
		}
	}

	// 4.5. Right cap: glyph in fg=last.bg on default bg, or a 1-col
	//      bg-painted plain space for "square" style.
	if hasRightCap {
		lastBg := visible[len(visible)-1].bg
		if caps.right == "" {
			// Square: 1 col of plain last.bg-painted space. The bg
			// is already set to lastBg from the last segment's
			// trailing bg-restore; padding spaces (if any) inherit
			// it unchanged.
			b.WriteString(" ")
		} else {
			// Round / slant: full reset clears any fg / bold left
			// over from the last segment, then fg256(lastBg) + glyph
			// paints the cap on the terminal's default bg.
			b.WriteString(reset)
			b.WriteString(fg256(lastBg))
			b.WriteString(caps.right)
		}
	}

	// 5. Close.
	b.WriteString(reset)
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
