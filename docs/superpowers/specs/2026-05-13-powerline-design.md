# Powerline renderer — 0.2.0 design

Status: approved 2026-05-13.

## Motivation

ccsb 0.1.x renders rows as `strings.Join(parts, separator)` — a flat
sequence of segment strings glued together with a single separator
character. The user, comparing against ccstatusline's Powerline-flavour
output, asked on 2026-05-13 for the same block-style layout but with a
more restrained palette: per-row background fill, thin chevron
separators between segments, and the bar extending to the full terminal
width on both rows so the asymmetric Row-2-shorter-than-Row-1 look from
ccstatusline-default is avoided.

Phase 3 of the 2026-05-12 statusline overhaul originally targeted this
work plus terminal-aware layout (right-align, truncation, `min_cols`) in
one 0.2.0 release. After scope-check on 2026-05-13 the bundle was
trimmed: 0.2.0 ships Powerline plus the *minimum* TTY-awareness needed
for full-width bg fill. The remaining layout primitives stay 0.2.1.

## Scope

In scope for 0.2.0:

- Powerline rendering: bg-fill per row, thin chevron separators between
  segments.
- Full-width bg fill: row bg extends from column 0 to the terminal's
  right edge.
- Minimal TTY awareness: a `readTTYSize()` helper that opens `/dev/tty`
  and queries `TIOCGWINSZ`, used only to determine the right-edge
  column. On any error, fall back to "natural width" (bg ends after the
  last chevron).
- Schema upgrade: `Row` becomes a struct with `segments` and an
  optional `bg` field; `Config` gains a top-level `Powerline bool`
  toggle.
- Backward compatibility: existing `Rows: [][]Segment` configs continue
  to parse via a custom JSON unmarshaler on `Row`.
- Format spacing: every `<label>:<value>` segment switches to
  `<label>: <value>` (space after the colon). Applies to `limit_5h`,
  `limit_7d`, `effort`, `output_style`, and `git_branch` (when its
  `label` is non-empty).
- Documentation: `docs/configuration.md` learns the Powerline section,
  the new Row shape, the spacing change, and the NO_COLOR fallback.

Out of scope (deferred to 0.2.1 / later):

- Right-alignment (`align: "right"` per row or via a spacer segment).
- Truncation (`max_width` per segment, ellipsis when overflowing
  terminal width).
- `min_cols` per segment (segment suppression below a threshold).
- 24-bit color support. Powerline keeps the existing 256-color schema.
- Configurable chevron glyph. `` is hard-coded for 0.2.0; future
  releases can add a `Config.Powerline.Chevron` knob.
- A `tty_size` debug segment.

## Schema

### Top-level

```go
type Config struct {
    Rows      []Row  `json:"rows,omitempty"`
    Separator string `json:"separator,omitempty"`
    Powerline bool   `json:"powerline,omitempty"`
}
```

`Powerline` is a single boolean toggle. When `false` (the default), the
renderer behaves exactly as 0.1.12 — `strings.Join(parts, separator)`
per row, no row-bg, no chevrons. Existing configs that never mention
`powerline` keep their look bit-for-bit (modulo the universal
colon-space fix; see "Format spacing" below).

### `Row`

```go
type Row struct {
    Segments []Segment `json:"segments"`
    Bg       string    `json:"bg,omitempty"`
}
```

`Row.Bg` is an ANSI 256-color decimal string (`"0"`–`"255"`). When set
and `Config.Powerline` is true, the bg fills the entire row from
column 0 to the terminal's right edge.

### Backward-compatible JSON

`Row` has a custom `UnmarshalJSON` that accepts both shapes:

- **Array shape (0.1.x compatibility):** `[{...}, {...}]` unmarshals
  into `Row{Segments: [...], Bg: ""}`. No bg, behaves as it did before.
- **Object shape (0.2.0 native):** `{"segments": [...], "bg": "234"}`
  unmarshals directly.

The custom unmarshaler peeks at the first non-whitespace byte:

```go
func (r *Row) UnmarshalJSON(data []byte) error {
    data = bytes.TrimLeft(data, " \t\r\n")
    if len(data) == 0 {
        return errors.New("render.Row: empty value")
    }
    if data[0] == '[' {
        var segs []Segment
        if err := json.Unmarshal(data, &segs); err != nil {
            return err
        }
        r.Segments = segs
        r.Bg = ""
        return nil
    }
    // Object shape — use an alias to avoid recursion into this method.
    type rowAlias Row
    var a rowAlias
    if err := json.Unmarshal(data, &a); err != nil {
        return err
    }
    *r = Row(a)
    return nil
}
```

`MarshalJSON` is intentionally **not** defined. When ccsb writes a
config back to disk (e.g., from `ccsb install`), it uses the
canonical object form. There is no need to re-emit the array form.

## Renderer

### TTY size detection

A new file `internal/pkg/render/tty.go`:

```go
package render

import (
    "os"

    "golang.org/x/sys/unix"
)

// readTTYCols returns the number of columns of the controlling
// terminal, or 0 if the size cannot be determined. Used only by the
// Powerline renderer to know where to stop the row-bg fill.
//
// The probe opens /dev/tty rather than reading stdin/stdout/stderr
// because ccsb's stdin is the JSON payload pipe and its stdout is the
// pipe to Claude Code. /dev/tty is the controlling terminal of the
// spawned process. If the process has no controlling terminal (rare
// for a Claude-Code-spawned child) or the ioctl fails, the function
// returns 0 and the caller falls back to natural width.
func readTTYCols() int {
    f, err := os.Open("/dev/tty")
    if err != nil {
        return 0
    }
    defer f.Close()
    ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
    if err != nil || ws == nil {
        return 0
    }
    return int(ws.Col)
}
```

The dependency `golang.org/x/sys/unix` is added via `go get`. (The
package was already discussed in the 2026-05-11 Memory note's TTY
probe.)

For tests, `readTTYCols` is captured in a package-level `var ttyColsFunc
= readTTYCols` so tests can swap it for a deterministic fake:

```go
var ttyColsFunc = readTTYCols
```

### `Render` dispatch

`Render(opts Options, raw []byte) (string, error)` gains a new branch:

```go
env := renderEnv{
    cwd:          cwd,
    colorEnabled: !opts.NoColor,
    nowUnix:      nowFunc().Unix(),
    ttyCols:      0,
}
if opts.Config.Powerline && env.colorEnabled {
    env.ttyCols = ttyColsFunc()
}
```

`ttyCols == 0` is the "couldn't read" signal; the Powerline renderer
handles it by skipping the bg-extension-to-right-edge step (natural
width).

For each row, `Render` chooses between the existing row-join path and a
new `renderRowPowerline(row, env)` path:

```go
for _, row := range rows {
    var line string
    switch {
    case opts.Config.Powerline && env.colorEnabled:
        line = renderRowPowerline(&p, row, env)
    default:
        line = renderRowNatural(&p, row, env, sep)
    }
    if line != "" {
        lines = append(lines, line)
    }
}
```

`renderRowNatural` is the existing path, refactored out so the dispatch
reads cleanly.

### `renderRowPowerline`

Pseudocode of the Powerline row builder:

```go
const (
    powerlineChevron      = "" // U+E0B1 thin right chevron
    powerlineChevronWidth = 1     // narrow Nerd Font glyph
    powerlineChevronFG    = "245" // muted grey; visible against any row-bg
)

func renderRowPowerline(p *payload, row Row, env renderEnv) string {
    // 1. Render every segment, dropping empties.
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

    // 2. Build the chevron. Muted-grey fg with no bg, so the row-bg
    //    shows through; closed with the default-foreground SGR so the
    //    surrounding segment-fgs survive the wrap.
    chev := powerlineChevron
    if open := fg256(powerlineChevronFG); open != "" {
        chev = open + powerlineChevron + "\x1b[39m"
    }
    var b strings.Builder
    if row.Bg != "" {
        b.WriteString(bg256(row.Bg))   // open the row bg once
    }
    for i, part := range parts {
        if i > 0 {
            b.WriteString(chev)
        }
        b.WriteString(part)
    }

    // 3. Fill the rest of the line with bg-padded spaces if we know
    //    the terminal width and the row has a bg.
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
    if row.Bg != "" {
        b.WriteString(reset)   // close the row bg
    }
    return b.String()
}
```

Key invariants:

- The row-bg is opened **once** at the start of the row and closed
  **once** at the end. Per-segment fg/bg from `style()` nest inside.
- The chevron carries an explicit muted-grey fg (`245`) closed with the
  default-foreground SGR `\x1b[39m`. It has no bg of its own, so the
  row-bg shows through. This is the "thin chevron, uniform bg" look the
  user picked.
- When `ttyCols == 0`, step 4 is skipped — bg ends after the last
  segment. Cheap graceful degradation.
- An empty row (every segment dropped) returns `""` so the row joiner
  skips it. Same behaviour as 0.1.x.
- The caller (`Render`) only enters this path when `env.colorEnabled`
  is true. `colorEnabled == false` routes through `renderRowNatural`
  unconditionally; see "NO_COLOR" below.

`displayWidth` computes the visible column count of a string by
stripping ANSI escapes and counting runes (with terminal-width-aware
counting for wide glyphs like emoji — see "Display-width caveat"
below). A new helper goes alongside `stripANSI` in render.go:

```go
func displayWidth(s string) int {
    stripped := stripANSIRegexp.ReplaceAllString(s, "")
    return runewidth.StringWidth(stripped)
}
```

A new dependency `github.com/mattn/go-runewidth` is needed for correct
emoji and CJK glyph widths (e.g., 🧠 is 2 columns wide, not 1).

### Display-width caveat

Computing display width of a UTF-8 string with embedded ANSI escapes is
the load-bearing primitive for full-width fill. The implementation must:

- Strip every ANSI escape sequence before counting (the existing
  `stripANSI` helper in `render_test.go` is the right regex; move it
  to render.go for production use, keep the helper named).
- Use a widths table that knows emoji width (`🧠` = 2), zero-width
  joiners (skin-tone modifiers), and CJK fullwidth glyphs. `runewidth`
  is the standard Go choice here.
- Handle the bar segment's `█` / `░` glyphs (both width 1, plain ASCII
  fallback case for `runewidth`).

Tests pin the width calculation against known cases including the
emoji (`🧠` = 2), the chevron itself (`` = 1 in most Nerd Fonts).

### NO_COLOR

Powerline is purely a visual layer. When `Options.NoColor == true`,
the renderer treats `Config.Powerline` as if it were `false`:

- `renderRowPowerline` is **not** entered. Every row routes through
  `renderRowNatural` with the configured `separator` (` | ` default,
  or whatever the user set).
- No bg, no chevrons, no row-padding. The output looks identical to
  0.1.x NoColor — predictable plain text.

This is the simplest contract: NO_COLOR means "no ANSI", and
Powerline-without-ANSI would degenerate to a separator anyway. By
routing through the existing natural path we get that for free and
also keep the existing separator preference honoured.

## Format spacing

`<label>:<value>` becomes `<label>: <value>` everywhere a label is
emitted:

| Segment        | Before (0.1.12) | After (0.2.0)   |
| -------------- | --------------- | --------------- |
| `effort`       | `effort:xhigh`  | `effort: xhigh` |
| `output_style` | `style:concise` | `style: concise` |
| `limit_5h`     | `5h:51% (31m)`  | `5h: 51% (31m)` |
| `limit_7d`     | `7d:74% (2d22h)` | `7d: 74% (2d22h)` |
| `git_branch` (with `label: "git"`) | `git:main` | `git: main` |

Implementation: replace `"%s:%s"` in `renderEffort`, `renderOutputStyle`,
`renderLimit`, and `renderGitBranch` with `"%s: %s"`. Same for the
`"%s:%s %s (%s)"` and `"%s:%s (%s)"` format strings in `renderLimit`.

This is a user-visible behaviour change. The five existing golden
fixtures (`testdata/golden/*.txt`) need to be regenerated with `go test
./internal/pkg/render -run TestRender_GoldenFixtures -update`.

## Threshold interaction

`threshold_target: "pct"` from 0.1.12 is unchanged. In Powerline mode:

- The row-bg is opened by the renderer once at the row's start.
- `style()` wraps each segment with its own fg (and bold), nested
  inside the row-bg. Per-segment `bg` (if set) overrides the row-bg
  for that segment's region — `style()` already handles that.
- `wrapPct` inner-wraps just the percentage digits in the threshold fg,
  closing back to the segment's static fg. The row-bg remains
  untouched at every step because nothing in the path emits a bg
  change for the pct region.

No code changes to `wrapPct` or `chooseFG` are needed for 0.2.0.
Existing tests for `threshold_target: "pct"` keep passing.

## Empty segments

The 0.1.11 fix that drops empty segments (no styling wrap, no
contribution to the row) is unchanged. In Powerline mode:

- Empty segments are dropped *before* chevron interleaving (step 1 of
  `renderRowPowerline`). No chevron is emitted for the dropped slot.
- The chain stays nahtlos: when `mode` (between `model` and `context`)
  renders empty, the chevron between the surviving `model` and
  `context` is the only one — no doubled chevron, no bg-Loch.

## Tests

New tests in `internal/pkg/render/render_test.go` and `tty_test.go`:

1. `TestReadTTYCols_FakeReturnsValue` — `ttyColsFunc` swapped to a fake
   returning 128; `Render` with Powerline enabled propagates it via
   `renderEnv.ttyCols`. Assert via a probe segment or by inspecting the
   output's padding length.
2. `TestReadTTYCols_FailureFallsBackToNaturalWidth` — `ttyColsFunc`
   returns 0; assert the rendered row has no trailing padding.
3. `TestRenderPowerline_RowBgWrapsSegments` — config with
   `Powerline: true` and `Row.Bg: "234"`; assert the output starts with
   `\x1b[48;5;234m`, contains the chevron `` once between the two
   segments, and ends with `\x1b[0m`.
4. `TestRenderPowerline_TwoRowsDifferentBgs` — Row 1 bg=234, Row 2
   bg=237; assert two separate `\x1b[48;5;234m`/`\x1b[48;5;237m`
   sequences appear.
5. `TestRenderPowerline_FullWidthFill` — fake `ttyColsFunc` returning
   80; render a row whose visible content is 20 columns wide; assert
   the output contains `strings.Repeat(" ", 60)` *inside* the bg span
   (before the `\x1b[0m`).
6. `TestRenderPowerline_EmptySegmentDropsChevron` — three segments where
   the middle one renders empty; assert exactly one chevron, not two.
7. `TestRenderPowerline_NoColorFallsBackToNatural` — `Options.NoColor:
   true` plus `Powerline: true`; assert the output contains the
   configured `separator` and no ANSI sequences and no chevron.
8. `TestRow_UnmarshalArrayShape` — JSON `[{"type":"text","label":"A"}]`
   unmarshals into `Row{Segments: [...], Bg: ""}`.
9. `TestRow_UnmarshalObjectShape` —
   `{"segments":[...], "bg":"234"}` unmarshals correctly.
10. `TestRow_UnmarshalRejectsMalformed` — `null`, `42`, `"string"`
    produce a non-nil error.
11. `TestRender_LabelColonSpacing_*` (one per segment family: effort,
    output_style, limit_5h, limit_7d, git_branch) — assert the
    rendered output contains `"<label>: <value>"`, not
    `"<label>:<value>"`. These act as regression tests for the
    universal spacing fix.
12. `TestRender_GoldenFixtures` — the existing five fixtures are
    regenerated as part of the implementation work; no new test code,
    just refreshed golden files committed alongside.
13. `TestDisplayWidth_*` — sanity checks against the new
    `displayWidth` helper: ASCII (width 1 per rune), emoji
    `🧠` (width 2), embedded ANSI (`\x1b[1mfoo\x1b[0m` = 3),
    block bar `[████░░] 50%` (width 13), chevron `` (width 1).

## Documentation

`docs/configuration.md` updates:

- New top-level fields: `Config.Powerline bool` documented in the
  top-level shape section.
- New "Row" subsection under `## render`: explains the two shapes
  (array for compat, object for new), `Row.Bg` semantics.
- New "Powerline" subsection at the end of `## Segments` (or in its
  own `## Powerline` section): describes the chevron, full-width fill,
  the NO_COLOR fallback, and the worked example below.
- Per-segment entries (limit_5h, limit_7d, effort, output_style,
  git_branch) updated for the new `<label>: <value>` spacing.
- A worked example showing the user's chosen two-row two-tone config
  ([powerline: true, Row 1 bg 234, Row 2 bg 237]).

## Backward compatibility

| Existing config shape                  | Behaviour in 0.2.0          |
| -------------------------------------- | --------------------------- |
| `Rows: [][]Segment` (no Powerline key) | Renders exactly as 0.1.12, modulo the colon-space fix on label segments. |
| `Rows: []Row` with `Bg` set, but `Powerline: false` | `Bg` is ignored; renders without Powerline. The bg key parses, just doesn't take effect. |
| `Powerline: true` with `Rows: [][]Segment` (array shape) | Powerline rendering with no row-bgs (chevron between segments, no full-width fill since `Bg` is empty). Probably not a useful state, but allowed. |
| `Powerline: true` with `Rows: []Row` and `Bg` set | The intended new look. |

The colon-space fix is the only existing-config behaviour change. It
applies regardless of Powerline. Memory note: regenerate golden
fixtures during implementation.

## Out of scope (reminder)

- Right-alignment, truncation, `min_cols` — 0.2.1.
- Configurable chevron glyph — future enhancement.
- 24-bit color support — future enhancement.
- A `tty_size` debug segment — future enhancement.
- Removing or changing the 0.1.12 `threshold_target: "pct"` mechanism —
  unchanged; orthogonal to Powerline.
