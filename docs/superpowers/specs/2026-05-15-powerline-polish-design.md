# 0.2.3 — Powerline Polish

## Summary

Bundle two visual-quality improvements for the Powerline renderer:

1. **Horizontal margin** — every rendered row gets a configurable
   number of plain (no-bg) leading spaces, and the usable bg-fill
   width is shrunk by twice that amount. Default 2. This leaves
   room for Claude Code's built-in statusLine chrome on each side
   so multi-row Powerline configurations are not truncated.
2. **Alternating per-segment backgrounds with proper chevron
   transitions** — a new `Row.Palette []string` field rotates
   background colours across the segments of a row. The chevron
   between two segments transitions in classic Powerline fashion:
   foreground = the leaving segment's background, background = the
   entering segment's background. A new `Config.PowerlineStyle`
   field selects the chevron glyph (`"thin"` = U+E0B1, default;
   `"solid"` = U+E0B0). A built-in default palette renders the
   alternating look out-of-the-box when neither `Row.Bg` nor
   `Row.Palette` is configured.

The two parts share one user-facing concept: "Powerline polish for
Claude Code rendering". Combining them in one release means the
margin behaviour reaches users alongside the palette feature instead
of churning the renderer twice.

## Motivation

After 0.2.2 shipped terminal-size detection, two visual issues
surfaced under Claude Code spawn:

- The Powerline rows padded to the full terminal width. Combined
  with Claude Code's built-in chrome on each side of the statusLine
  area, this caused row 1 to overflow into row 2's slot, triggering
  Claude Code's "..." truncation marker and hiding row 2. The
  margin formalisation addresses this directly.
- The 0.2.0 chevron renders in a constant muted-grey foreground
  between segments that all share the same row background. The
  chevron visually pretends to be a Powerline transition wedge
  without there actually being a colour transition. The classic
  Powerline aesthetic uses alternating bgs so the chevron *is* a
  transition, which is what this release enables.

A quick-hack of the margin already landed in the working tree this
session and was confirmed visually by the user. It is discarded
before this release branches off; the formal implementation re-does
the change with tests.

## Scope

In:

- `Config.Margin *int` with `defaultMargin = 2`, JSON
  `"margin,omitempty"`, nil → default 2, negative clamped to 0,
  explicit 0 means "no margin".
- `Config.PowerlineStyle string` with `omitempty`, values `"thin"`
  (default when empty) or `"solid"`.
- `Row.Palette []string` with `omitempty`. Rotates per visible
  segment.
- Built-in `defaultPalette = []string{"234", "236", "238"}` used
  only when `Powerline` is true AND neither `Row.Bg` nor
  `Row.Palette` is set.
- `renderEnv.margin int` carrying the resolved margin per render
  call.
- New helper `effectiveSegmentBg(row Row, seg Segment, visibleIndex
  int, powerlineActive bool) string` implementing the priority
  ladder.
- `renderRowNatural` prepends `margin` plain spaces.
- `renderRowPowerline` prepends `margin` plain spaces, uses
  `ttyCols - 2*margin` as the usable width, and emits chevrons with
  the new transition colours and the configured glyph.
- Same-bg fallback in the chevron: when adjacent effective bgs are
  equal, the chevron fg falls back to `defaultSameBgChevronFG =
  "245"` so the visual remains identical to 0.2.0/0.2.2 for legacy
  uniform-bg configs.
- Documentation refresh in `docs/configuration.md`: new schema
  fields, migration note from uniform `Row.Bg` to palette, default
  palette description, chevron-transition explanation, glyph
  selector.

Out:

- Right-align, `max_width` truncation, `min_cols` segment
  suppression — still 0.2.4+ per memory `02x_terminal_aware_layout`.
- Per-row `chevron_fg` override — not requested.
- Theme presets (e.g. `"powerline_theme": "solarized"`) — out.
- Migration tooling to rewrite existing configs — out; users update
  manually.

## Design

### Schema additions

`render.Config`:

```go
type Config struct {
    Rows           []Row  `json:"rows,omitzero"`
    Separator      string `json:"separator,omitempty"`
    Powerline      bool   `json:"powerline,omitempty"`
    Width          int    `json:"width,omitempty"`
    Margin         *int   `json:"margin,omitempty"`           // NEW
    PowerlineStyle string `json:"powerline_style,omitempty"`  // NEW: "thin" (default) | "solid"
}
```

`render.Row`:

```go
type Row struct {
    Segments []Segment `json:"segments"`
    Bg       string    `json:"bg,omitempty"`
    Palette  []string  `json:"palette,omitempty"`             // NEW
}
```

`Segment.BG` stays unchanged; it overrides the palette/Row.Bg
resolution.

### Constants

```go
const (
    defaultMargin           = 2
    powerlineStyleThin      = "thin"
    powerlineStyleSolid     = "solid"
    powerlineThinGlyph      = ""    // U+E0B1 RIGHT TRIANGLE LINE
    powerlineSolidGlyph     = ""    // U+E0B0 RIGHT TRIANGLE FILL
    defaultSameBgChevronFG  = "245"  // fallback when prev.bg == next.bg
)

var defaultPalette = []string{"234", "236", "238"}
```

`powerlineSeparatorWidth = powerlineChevronWidth + 2` is unchanged
from 0.2.1 — the `" <glyph> "` spacing rule still applies regardless
of the glyph chosen.

`powerlineChevron` and `powerlineChevronFG` from 0.2.0 are removed;
the glyph is now selected per render call from PowerlineStyle, and
the chevron fg is computed from adjacent bgs (or the same-bg
fallback).

### Effective margin

```go
const defaultMargin = 2

func (c Config) effectiveMargin() int {
    if c.Margin == nil {
        return defaultMargin
    }
    if *c.Margin < 0 {
        return 0
    }
    return *c.Margin
}
```

### Effective segment background

```go
func effectiveSegmentBg(row Row, seg Segment, visibleIndex int, powerlineActive bool) string {
    if seg.BG != "" {
        return seg.BG
    }
    if len(row.Palette) > 0 {
        return row.Palette[visibleIndex%len(row.Palette)]
    }
    if row.Bg != "" {
        return row.Bg
    }
    if powerlineActive {
        return defaultPalette[visibleIndex%len(defaultPalette)]
    }
    return ""
}
```

`visibleIndex` is the rank of the segment among the non-empty
segments of the row. Empty segments (e.g. `mode` when neither
thinking nor fast_mode is set) are dropped *before* the index is
assigned, so palette rotation never leaves a colour gap. This is a
behaviour change from the current code, where `Segment.BG` was the
only per-segment colouring path and indexing did not matter.

### Glyph selection

```go
func pickGlyph(style string) string {
    if style == powerlineStyleSolid {
        return powerlineSolidGlyph
    }
    return powerlineThinGlyph  // default: thin, including "" and unknown values
}
```

Unknown values silently default to thin so a typo in user config
does not break rendering.

### Margin in `Render`

```go
env := renderEnv{
    cwd:          cwd,
    colorEnabled: !opts.NoColor,
    nowUnix:      nowFunc().Unix(),
}
env.ttyCols, env.ttyRows = discoverTermSize(opts.Config)
env.margin = opts.Config.effectiveMargin()
// Degrade gracefully if the terminal is narrower than 2*margin.
if env.ttyCols > 0 && env.ttyCols <= 2*env.margin {
    env.margin = 0
}
```

`renderEnv` gains two new fields:

```go
type renderEnv struct {
    cwd            string
    colorEnabled   bool
    nowUnix        int64
    ttyCols        int
    ttyRows        int
    margin         int    // NEW
    powerlineStyle string // NEW: "thin" | "solid", "" defaults to thin
}
```

`Render` sets `env.powerlineStyle = opts.Config.PowerlineStyle`
once, alongside the existing `env.ttyCols, env.ttyRows = ...` and
the new `env.margin = ...`.

### `renderRowNatural` with margin

```go
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
```

### `renderRowPowerline` with palette + transitions

The current Powerline row builder is rewritten end-to-end. Pseudocode:

```go
func renderRowPowerline(p *payload, row Row, env renderEnv) string {
    if !env.colorEnabled {
        return ""
    }

    // 1. Render visible segments; remember their effective bgs.
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
        bg := effectiveSegmentBg(row, seg, len(visible), true /* powerlineActive */)
        visible = append(visible, renderedSeg{body: s, bg: bg})
    }
    if len(visible) == 0 {
        return ""
    }

    glyph := pickGlyph(env.powerlineStyle)
    var b strings.Builder

    // 2. Leading margin (plain, no bg).
    if env.margin > 0 {
        b.WriteString(strings.Repeat(" ", env.margin))
    }

    // 3. Walk segments, interleaving chevrons.
    for i, seg := range visible {
        if i > 0 {
            // chevron: " " + glyph in (fg=prev.bg, bg=next.bg) + " "
            // Both spaces inherit next.bg.
            prevBg := visible[i-1].bg
            nextBg := visible[i].bg
            chevFg := prevBg
            if prevBg == nextBg {
                chevFg = defaultSameBgChevronFG
            }
            b.WriteString(bg256(nextBg))
            b.WriteString(" ")
            b.WriteString(fg256(chevFg))
            b.WriteString(glyph)
            b.WriteString(reset)
            b.WriteString(bg256(nextBg))
            b.WriteString(" ")
        } else if seg.bg != "" {
            // First segment: open bg.
            b.WriteString(bg256(seg.bg))
        }
        b.WriteString(seg.body)
        if seg.bg != "" {
            b.WriteString(bg256(seg.bg)) // segment body's [0m killed bg; restore
        }
    }

    // 4. Pad to (ttyCols - 2*margin) total visible cols of bg-fill content.
    if env.ttyCols > 0 {
        usableCols := env.ttyCols - 2*env.margin
        used := 0
        for _, seg := range visible {
            used += displayWidth(seg.body)
        }
        used += (len(visible) - 1) * powerlineSeparatorWidth
        if remaining := usableCols - used; remaining > 0 {
            b.WriteString(strings.Repeat(" ", remaining))
        }
    }

    // 5. Close.
    b.WriteString(reset)
    return b.String()
}
```

Notes on the rewrite:

- **`renderRowPowerline` signature unchanged.** The style is
  carried on `renderEnv.powerlineStyle`. Existing tests that
  construct `renderEnv{...}` literals get `""` (default thin) when
  they don't set the new field — no signature-pass-through churn.
- **The "same bg both sides" fallback** preserves the 0.2.0 visual
  for legacy configs that only set `Row.Bg`. Without this, all
  chevrons would become invisible (fg=bg) — a silent visual
  regression.
- **The leading margin is emitted with no SGR**, so it renders in
  Claude Code's default terminal bg (the chrome colour). This is
  intentional and visually correct.
- **Empty `Row.Bg` and empty `Row.Palette`** with `Powerline=true`
  invoke the built-in default palette. Zero-config users get the
  alternating look immediately on first deploy.

### Backwards compatibility

| 0.2.2 config | 0.2.3 behaviour |
|---|---|
| `Row.Bg: "234"` (uniform) | All segs bg=234. Chevron fg falls back to "245" (same-bg case). Identical to 0.2.2 except the 2-col leading margin. |
| `Row.Bg: ""`, no palette, Powerline true | Default palette `[234, 236, 238]` rotates. **NEW visual** (was no bg in 0.2.2). |
| `Segment.BG` explicitly set on every segment | Each segment uses its explicit bg. Chevron transitions properly between distinct bgs. Same as 0.2.2 except chevron now bg-aware. |
| `Margin` not set | Default margin 2 applies. **NEW visual** (2-col indent + 4-col shrink). |
| `PowerlineStyle` not set | Thin chevron (U+E0B1). Identical to 0.2.2. |

The margin default (2) is a deliberate UX choice — it fixes the
truncation issue under Claude Code without requiring config changes.
Users who want flush rendering set `"margin": 0`.

## Tests

### Pure-/Unit-Tests in `render_test.go`

1. `TestConfigJSON_MarginRoundTrip` — `Margin *int`: nil omits,
   set to 0 emits `"margin":0`, set to 5 emits `"margin":5`,
   round-trip preserves value, negative round-trips as-is but
   `effectiveMargin()` clamps to 0.
2. `TestConfig_EffectiveMargin` — table-driven: nil→2, 0→0, 5→5,
   -3→0.
3. `TestConfigJSON_PowerlineStyleRoundTrip` — `""`, `"thin"`,
   `"solid"`, `"invalid"` all marshal/unmarshal without error.
4. `TestPickGlyph` — `""`→thin, `"thin"`→thin, `"solid"`→solid,
   `"invalid"`→thin.
5. `TestRowJSON_PaletteRoundTrip` — empty palette omits, non-empty
   array round-trips, palette + bg coexist in JSON.
6. `TestEffectiveSegmentBg` — table-driven priority ladder:
   `Segment.BG` > `Row.Palette[i%N]` > `Row.Bg` > `defaultPalette`
   (only when `powerlineActive`) > `""`.
7. `TestEffectiveSegmentBg_VisibleIndexRotation` — palette
   `["A","B","C"]` with 5 visible segments yields A, B, C, A, B.
8. `TestEffectiveSegmentBg_DefaultPaletteOnlyWhenPowerline` —
   `powerlineActive=false` and no Row.Bg/Palette → `""`.
9. `TestRenderRowPowerline_PrependsMargin` — env.margin=2, asserts
   first 2 bytes of output are plain spaces (no ANSI).
10. `TestRenderRowPowerline_UsableWidthShrunkByMargin` — env.margin=2,
    ttyCols=80, three 1-col segments. Expected total visible width
    = 2 + (80 - 4) = 78. Trailing absence of content fills the
    last 2 cols (terminal default bg).
11. `TestRenderRowPowerline_AlternatingPaletteEmitsDistinctBgs` —
    palette `["234","236","238"]` with three segments, asserts the
    output contains `\x1b[48;5;234m`, `\x1b[48;5;236m`,
    `\x1b[48;5;238m`.
12. `TestRenderRowPowerline_ChevronTransitionColors` — palette
    `["234","236"]`, two segments. Asserts the chevron bytes are
    framed by `\x1b[38;5;234m` (prev.bg as fg) and `\x1b[48;5;236m`
    (next.bg).
13. `TestRenderRowPowerline_ChevronUniformBgFallback` — Row.Bg=234,
    no palette, no per-segment BG. Two segments → both effective
    bg=234. Asserts chevron fg is the fallback `"245"`.
14. `TestRenderRowPowerline_SolidGlyph` — PowerlineStyle="solid",
    output contains U+E0B0 not U+E0B1.
15. `TestRenderRowPowerline_DefaultPaletteUsedWhenNoConfig` —
    Powerline=true, Row.Bg="", Row.Palette=nil. Three segments → bgs
    234, 236, 238.
16. `TestRenderRowPowerline_EmptySegmentDropsPaletteSlot` — three
    configured segments where the middle renders empty (e.g. `mode`
    with no thinking and no fast_mode). Visible-index rotation
    indexes the remaining two as 0 and 1, not 0 and 2 — both bgs
    come from palette slots 0 and 1.
17. `TestRenderRowNatural_HonorsMargin` — natural mode with
    margin=2 prepends 2 plain spaces before the first segment.
18. `TestRender_MarginDegradesOnNarrowTerminal` — Config.Margin=10,
    ttyCols=15. Margin clamps to 0 to prevent negative usable
    width.

### Existing tests that need updates

All `TestRenderRowPowerline_*` tests construct `renderEnv{...}`
literals directly and bypass `Render`. The new `env.margin` field
defaults to 0 in their zero-valued envs, so these tests do NOT need
margin-related changes. Only tests that exercise the full `Render`
path are affected by the new default.

| Test | Required change |
|---|---|
| `TestRender_PowerlineFalseUsesNaturalPath` | Set `Options{Config: Config{Margin: ptr(0), …}}` to keep string-equality assertions valid against the new default margin. |
| `TestRender_PowerlineTrueEmitsBgAndChevron` | Same — set `Margin: ptr(0)` to keep prefix-byte assertions. |
| `TestRender_PowerlineTTYColsPropagated` | Same — set `Margin: ptr(0)` so the exact-width `displayWidth == 40` assertion stays valid. |
| `TestRender_PopulatesTTYColsAndRowsViaDiscover` | No change — uses `strings.Contains` for `"96×24"`, unaffected by leading margin. |
| `TestConfigJSON_WidthRoundTrip` | No change. |
| All golden fixtures (`testdata/golden/*.txt`) | Each fixture's natural-mode rendering gains a 2-col leading margin on every row. Regenerate with `-update`. Alternatively the test runner can wrap render with `Margin: ptr(0)` to keep the golden bytes stable — implementation chooses which is cleaner. |

A tiny `ptr` helper (`func intPtr(v int) *int { return &v }`) goes
in `render_test.go` next to the affected tests.

### Coverage target

Render-package coverage ≥ 94.5% (current). The rewrite adds the
new effective-bg helper, glyph picker, and transition logic — all
deterministic and easily unit-tested. Margin handling is also fully
testable. Expect coverage to rise slightly.

## Documentation

`docs/configuration.md` changes:

1. Schema table (the one with `rows`, `separator`, `powerline`,
   `width`): append two new rows for `margin` and `powerline_style`.
2. Row shape section: document the new `palette` field. Explain
   the resolution priority (Segment.BG > Row.Palette > Row.Bg >
   default-palette-if-Powerline > none).
3. Powerline section: update the chevron-transition prose. The
   current text describes a uniform-bg row with a "muted-grey
   foreground (245)" chevron. The new prose describes the chevron
   as a transition wedge between adjacent bgs, with the same-bg
   fallback noted explicitly.
4. New example near the existing two-tone example: a three-tone
   palette example demonstrating alternating colours.
5. Migration note: how to convert a uniform `Row.Bg` config to a
   palette config. One short paragraph.

## Rollout

Per project memory `release_workflow_gates`:

1. Implement on `development-0.2.3-work` with TDD commits.
2. Squash → `development-0.2.3-main`.
3. No-ff merge → `main`.
4. Tag `production-0.2.3` from `main`; push origin.

Three explicit user confirmations between phases — do not batch.

The session quick-hack for the margin has been discarded from the
working tree before this branch was created, so the formal
implementation starts from a clean main.

## Risks

- **Visual regression for users with uniform-bg configs**: The
  default margin of 2 visibly indents every row by 2 cols. Users
  who preferred flush rendering must set `"margin": 0`. This is
  documented prominently in the migration note.
- **Same-bg-fallback choice (`"245"`)**: hardcoded. If a future
  user dislikes this colour, they would either configure palette
  variants or wait for a per-row override field (not in scope).
- **Visible-index rotation vs configured-index rotation**: We
  chose visible-index so dropped empty segments don't leave a
  palette gap. The trade-off: if a user toggles `mode` visibility
  rapidly (rare in practice — `mode` is rendered per Claude Code
  refresh tick), the bgs of segments AFTER `mode` flicker between
  palette slots. Acceptable cost for the visually-clean rotation.
- **PowerlineStyle "solid" with paletteless rows**: a solid wedge
  on uniform bg renders as a same-colour rectangle, invisible. The
  fallback fg "245" makes a thin wedge in grey — also acceptable.
  No special handling beyond the existing same-bg fallback.
- **Existing tests need bulk update**: 14+ tests need a
  signature-pass adjustment. The plan task will list each by name
  with the precise change.

## Open Questions

None.

## References

- Project memory: `release_workflow_gates`,
  `release_sizing_one_concept`, `statusline_aesthetic`,
  `02x_terminal_aware_layout`.
- Prior spec: `docs/superpowers/specs/2026-05-13-powerline-design.md`
  (0.2.0 baseline).
- Prior spec: `docs/superpowers/specs/2026-05-14-chevron-spacing-design.md`
  (0.2.1, the spacing rule this release preserves).
- Prior spec: `docs/superpowers/specs/2026-05-14-terminal-size-detection-design.md`
  (0.2.2, the width-detection this release composes with).
