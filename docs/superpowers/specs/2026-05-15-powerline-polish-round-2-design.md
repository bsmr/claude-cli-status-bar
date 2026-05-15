# 0.2.4 — Powerline Polish Round 2

## Summary

Correct the 0.2.3 chevron-transition specification (which produced a
visible bg-gap between the previous segment and the chevron glyph),
split the chevron geometry by glyph style so thin and solid both
flow correctly, and add opt-in rounded end caps to Powerline rows.

The 0.2.3 release shipped a chevron transition that emitted both
surrounding spaces in the *next* segment's bg. That made the bg jump
abruptly from prev to next *before* the chevron glyph, which broke
the visual illusion of a flowing transition. This release corrects
the geometry: the pre-space now renders in the prev bg, the chevron
cell follows a per-style rule, and the post-space renders in the
next bg.

In parallel, two new optional configuration fields enable end caps
on Powerline rows: `Row.Caps` (per-row bool) and `Config.CapStyle`
(global string with values `"round"`, `"square"`, `"slant"`). When
enabled, the row gains a 1-col cap glyph on each end whose colour
matches the first/last segment's effective bg, producing a rounded
visual silhouette.

The accumulated quick-hacks from this session were discarded; the
formal implementation re-does the changes with tests and docs.

## Motivation

After 0.2.3 shipped, two visual issues surfaced under live use:

1. The thin-chevron geometry produced a 3-col gap of `next.bg`
   before the line glyph appeared. The line looked detached from
   the previous segment because the bg jumped one space too early.
2. Even after the gap was fixed, the line glyph itself sits inside
   a cell whose bg is `next.bg`, so the chevron cell's right half
   still shows `next.bg` while the line is drawn in `prev.bg` —
   "the left half of the chevron has the colour of the following
   block" in the user's words.

The first issue is a spec error in 0.2.3 section 4. The second
issue is a glyph-specific geometry decision that 0.2.3 didn't
distinguish: U+E0B0 (solid wedge) is a fill glyph that benefits
from `chev-cell bg=next, fg=prev` (canonical Powerline wedge),
while U+E0B1 (thin line) only marks a boundary and benefits from
`chev-cell bg=prev, fg=next` (line at trailing edge of prev). The
two glyphs need different SGR pairings.

Separately, the leftmost segment's text starts abruptly against
the row's bg-fill — no breathing room between the row-bg's left
edge and the first segment's content. Powerline's standard
solution is the half-circle cap glyphs U+E0B6 (left) and U+E0B4
(right), which visually round the row's ends and naturally
introduce a 1-col padding.

## Scope

In:

- `Row.Caps bool` field with `json:"caps,omitempty"`. Default
  false — opt-in per row.
- `Config.CapStyle string` field with `json:"cap_style,omitempty"`.
  Values: `"round"` (default), `"square"`, `"slant"`. Unknown
  values fall back to `"round"`.
- Three new cap-style constants and one helper:
  - `capStyleRound = "round"`, `capStyleSquare = "square"`,
    `capStyleSlant = "slant"`.
  - `powerlineLeftCapRound = ""`, `powerlineRightCapRound =
    ""` (half-circles).
  - `powerlineLeftCapSlant = ""`, `powerlineRightCapSlant =
    ""` (filled triangles).
  - `pickCapGlyphs(style string) capGlyphs` helper.
- `renderEnv.capStyle string` populated from `Config.CapStyle` by
  `Render`.
- `renderRowPowerline` rewrite:
  - Pre-chevron space now in `prevBg`, post-chevron space now in
    `nextBg` (correcting the 0.2.3 spec).
  - Per-style asymmetric chev-cell geometry:
    - solid (U+E0B0): chev-cell bg = `nextBg`, fg = `prevBg`.
    - thin (U+E0B1, default): chev-cell bg = `prevBg`, fg = `nextBg`.
    - Same-bg fallback unchanged: chev fg = `defaultSameBgChevronFG`
      ("245").
  - Optional left cap before the first segment, optional right cap
    after the padding. Caps subtract 2 cols from `usableCols` when
    enabled.
- Documentation refresh in `docs/configuration.md`:
  - New `cap_style` row in the schema table.
  - New `caps` field documented in the Row shape section.
  - Powerline section's chevron prose corrected for the
    per-style asymmetry and the pre-/post-space colour rule.
  - New "End caps" subsection demonstrating `caps: true` with the
    three style options.

Out:

- A global migration of user configs. Live user config keeps
  `powerline_style: "solid"` (set in the previous session) but
  does not automatically gain `caps: true`. A post-release
  migration is offered separately.
- Per-row `cap_style` (one config-level setting per Render call
  is sufficient; row-level overrides are not requested).
- Changing the `powerline_style` default from `"thin"` to
  `"solid"` — existing thin-chevron users keep their current
  rendering (with the new asymmetric geometry).
- Right-align, `max_width` truncation, `min_cols` segment
  suppression — still 0.2.5+ per memory `02x_terminal_aware_layout`.
- A `caps: true` default for new users — opt-in keeps
  backward-compat for the default-rows path.

## Design

### Chevron geometry correction (the 0.2.3 spec error)

In 0.2.3 the chevron transition between `visible[i-1]` and
`visible[i]` emitted:

```
bg256(nextBg) + " " + fg256(prevBg) + glyph + reset + bg256(nextBg) + " "
```

Both spaces around the glyph rendered in `nextBg`. The result was
a 3-col block of `next.bg` (space + glyph cell + space) immediately
after the previous segment's `prev.bg`, producing a visible bg
boundary one column too early.

The new geometry emits:

```
bg256(prevBg) + " " + bg256(chevCellBg) + fg256(chevFg) + glyph + reset + bg256(nextBg) + " "
```

with `(chevCellBg, chevFg)` selected per style:

| Style | chev-cell bg | chev-cell fg | Visual |
|---|---|---|---|
| `"thin"` (default) | `prevBg` | `nextBg` | Line in next colour sits at trailing edge of prev; bg switches at the post-space. |
| `"solid"` | `nextBg` | `prevBg` | Filled wedge in prev colour flows into next bg via the glyph's curve. |
| same-bg | (matches both) | `defaultSameBgChevronFG` ("245") | Static muted-grey marker on uniform bg, preserves the 0.2.0 visual for legacy configs. |

### `Config.CapStyle` and `Row.Caps`

```go
type Config struct {
    Rows           []Row  `json:"rows,omitzero"`
    Separator      string `json:"separator,omitempty"`
    Powerline      bool   `json:"powerline,omitempty"`
    Width          int    `json:"width,omitempty"`
    Margin         *int   `json:"margin,omitempty"`
    PowerlineStyle string `json:"powerline_style,omitempty"`
    CapStyle       string `json:"cap_style,omitempty"` // NEW
}

type Row struct {
    Segments []Segment `json:"segments"`
    Bg       string    `json:"bg,omitempty"`
    Palette  []string  `json:"palette,omitempty"`
    Caps     bool      `json:"caps,omitempty"` // NEW
}
```

`Segment` is unchanged.

### Cap glyph constants and picker

```go
const (
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

    // "square" emits no glyph — just a 1-col plain bg-painted space.
)

// capGlyphs carries the left/right glyph pair for a cap style.
// Empty strings indicate the "square" sentinel: emit a bg-painted
// space instead of a glyph.
type capGlyphs struct {
    left, right string
}

// pickCapGlyphs returns the cap-glyph pair for the given style.
// Unknown or empty values fall back to round.
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
```

Implementation note for the slant codepoints: the chosen pair
(`` left, `` right) reflects the conventional Nerd
Font Powerline-Extras mapping. If a visual smoke verification at
implementation time finds the pair produces wrong-looking caps,
the implementer may substitute (e.g. `` / ``) and
note the substitution in the test source. The selected codepoints
must produce a left-cap that visually opens like `/` and a
right-cap that closes like `\` on a typical patched Powerline
font.

### `renderEnv.capStyle`

```go
type renderEnv struct {
    cwd            string
    colorEnabled   bool
    nowUnix        int64
    ttyCols        int
    ttyRows        int
    margin         int
    powerlineStyle string
    capStyle       string // NEW: "round" | "square" | "slant" (empty → round)
}
```

`Render` sets `env.capStyle = opts.Config.CapStyle` once per call,
alongside the existing `powerlineStyle` and `margin` plumbing.

### `renderRowPowerline` with caps

The function gains a left-cap block before the first segment and a
right-cap block after the padding. The width-math step subtracts 1
column per active cap from `usableCols`.

```go
func renderRowPowerline(p *payload, row Row, env renderEnv) string {
    if !env.colorEnabled {
        return ""
    }

    // 1. Render visible segments and capture each one's effective bg.
    // [unchanged from 0.2.3]
    var visible []renderedSeg
    for _, seg := range row.Segments { /* ... */ }
    if len(visible) == 0 {
        return ""
    }

    glyph := pickGlyph(env.powerlineStyle)
    caps := pickCapGlyphs(env.capStyle)
    hasLeftCap := row.Caps && visible[0].bg != ""
    hasRightCap := row.Caps && visible[len(visible)-1].bg != ""

    var b strings.Builder

    // 2. Leading margin: plain spaces with no bg.
    if env.margin > 0 {
        b.WriteString(strings.Repeat(" ", env.margin))
    }

    // 2.5. Left cap: depends on cap style.
    if hasLeftCap {
        firstBg := visible[0].bg
        if caps.left == "" {
            // Square: 1 col of plain first.bg-painted space.
            b.WriteString(bg256(firstBg))
            b.WriteString(" ")
        } else {
            // Round/slant: glyph in fg=first.bg on default bg.
            b.WriteString("\x1b[49m") // default bg
            b.WriteString(fg256(firstBg))
            b.WriteString(caps.left)
            b.WriteString(reset)
        }
    }

    // 3. Walk segments, interleaving chevrons (corrected geometry).
    for i, seg := range visible {
        if i == 0 {
            if seg.bg != "" {
                b.WriteString(bg256(seg.bg))
            }
        } else {
            prevBg := visible[i-1].bg
            nextBg := seg.bg

            // Per-style asymmetric chev-cell geometry.
            var chevCellBg, chevFg string
            if env.powerlineStyle == powerlineStyleSolid {
                chevCellBg, chevFg = nextBg, prevBg
            } else {
                chevCellBg, chevFg = prevBg, nextBg
            }
            if prevBg == nextBg {
                chevFg = defaultSameBgChevronFG
            }

            // Pre-space in prevBg, chev-cell in (chevCellBg, chevFg),
            // post-space in nextBg.
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
            b.WriteString(bg256(seg.bg))
        }
    }

    // 4. Pad to (ttyCols - 2*margin - 2*cap_cols) total visible cols
    //    of bg-fill so the left and right caps each occupy 1 col
    //    within the usable region.
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

    // 4.5. Right cap: depends on cap style.
    if hasRightCap {
        lastBg := visible[len(visible)-1].bg
        if caps.right == "" {
            // Square: 1 col of plain last.bg-painted space (bg
            // already set to lastBg from the previous step).
            b.WriteString(" ")
        } else {
            b.WriteString(reset)
            b.WriteString(fg256(lastBg))
            b.WriteString(caps.right)
        }
    }

    // 5. Close.
    b.WriteString(reset)
    return b.String()
}
```

### Backwards compatibility

| 0.2.3 config | 0.2.4 behaviour |
|---|---|
| `Row.Caps` not set | No caps. Identical visible width to 0.2.3 with the chevron geometry corrected. |
| `Row.Caps: true`, no `cap_style` | Round caps (U+E0B6 / U+E0B4). |
| `Row.Caps: true`, `cap_style: "square"` | 1-col bg-painted space on each side. |
| `Row.Caps: true`, `cap_style: "slant"` | Slant glyphs. |
| Chevron transition with `powerline_style` thin | Asymmetric geometry: chev-cell bg = prev, fg = next. Visual differs from 0.2.3 (which had bg = next, fg = prev for both styles). |
| Chevron transition with `powerline_style` solid | Asymmetric geometry: chev-cell bg = next, fg = prev. Same as 0.2.3's wedge intent. |
| Uniform `Row.Bg` (legacy, no palette) | Same-bg fallback unchanged: chev fg = "245". |

The chevron-geometry change is a visible diff for any 0.2.3 user
who set up alternating bgs. Documented in the docs migration note.

## Tests

All tests live in `package render` in `internal/pkg/render/`.

### JSON round-trip

1. `TestConfigJSON_CapStyleRoundTrip` — empty omits, `"round"`,
   `"square"`, `"slant"`, `"invalid"` all marshal and round-trip.
2. `TestRowJSON_CapsRoundTrip` — `caps:false` omitted; `caps:true`
   emits and round-trips; coexists with `bg` and `palette`.

### Cap-glyph picker

3. `TestPickCapGlyphs` — table:
   - `""` → round pair.
   - `"round"` → round pair.
   - `"square"` → empty pair (sentinel).
   - `"slant"` → slant pair.
   - `"invalid"` → round pair.

### `renderRowPowerline` with caps

4. `TestRenderRowPowerline_NoCapsByDefault` — `row.Caps=false`. No
   cap glyphs in output (``, ``, ``, ``
   all absent).
5. `TestRenderRowPowerline_RoundCapsEmitGlyphs` — `row.Caps=true`,
   default cap_style. Output contains `` in fg = first.bg
   and `` in fg = last.bg.
6. `TestRenderRowPowerline_SlantCapsEmitGlyphs` —
   `cap_style="slant"`. Output contains `` and ``,
   does NOT contain `` or ``.
7. `TestRenderRowPowerline_SquareCapsEmitBgSpaces` —
   `cap_style="square"`. Output contains NO cap glyphs (no
   `\ue0bX` codepoints in the cap range). Visible width of the
   row is 2 cols larger than the no-caps baseline (margin
   accounted separately).
8. `TestRenderRowPowerline_UnknownCapStyleFallsBackToRound` —
   `cap_style="garbage"`. Output contains round glyphs.
9. `TestRenderRowPowerline_CapsWidthMath` — `row.Caps=true`,
   `ttyCols=80`, `margin=2`. 3 single-col segments + 2 chev
   separators of width 3 = 9 used. Caps reduce usableCols by 2.
   Padding fills `74 - 9 = 65` cols. Total visible width =
   `2 margin + 1 left cap + 9 content + 65 pad + 1 right cap =
   78` cols (which equals `ttyCols - margin` as in 0.2.3).
10. `TestRenderRowPowerline_LeftCapWithoutBgSkipped` — `row.Caps`
    true but `visible[0].bg == ""` (constructed via a row with
    `Bg=""`, `Palette=nil`, and Powerline disabled in env — edge
    case). No cap emitted.

### Asymmetric chevron geometry (correction)

11. `TestRenderRowPowerline_ChevronTransitionColors` — **update**:
    thin geometry, asserts chev-cell bg = prev (234) and fg = next
    (236) — opposite of the 0.2.3 assertion.
12. `TestRenderRowPowerline_SolidChevronTransitionColors` —
    solid geometry, asserts chev-cell bg = next (236) and fg =
    prev (234).
13. `TestRenderRowPowerline_ChevronPreSpaceInPrevBg` — new test
    that explicitly asserts the byte sequence right after the
    first segment's body contains `bg256(prevBg)` followed by a
    space, NOT `bg256(nextBg)` followed by a space. Prevents
    regression of the 0.2.3 bug.

### Regression test list

The following tests were modified in the session's quick-hacks and
will revert to their original assertions (because `caps` is now
opt-in and defaults to false):

- `TestRenderRowPowerline_NoPaddingWhenTTYIsZero` — back to
  `want 2` (no caps in default scenario).
- `TestRenderRowPowerline_OpensAndClosesRowBg` — back to
  `HasPrefix` assertion (no cap before the first bg-open).
- `TestRenderRowPowerline_ChevronTransitionColors` — update per
  item 11 above (asymmetric thin geometry).

### Coverage target

Render-package coverage ≥ 94.5% (current baseline). New code paths
(cap rendering, cap-style picker, asymmetric chev geometry) are
fully test-covered. Expect coverage to hold or rise slightly.

## Documentation

`docs/configuration.md` changes:

1. **Schema table** (`### Fields`) — append a `cap_style` row:

   ```
   | `cap_style` | string | `"round"` | End-cap glyph style applied when a row's `caps` is true. `"round"` (default) renders U+E0B6 / U+E0B4 half-circles; `"square"` extends the bg with a 1-col plain space on each side; `"slant"` renders U+E0BC / U+E0BA filled triangles. Unknown values fall back to `"round"`. |
   ```

2. **Row shape section** — add a paragraph about the `caps` field:

   > Within the object form, an optional boolean `caps` field enables
   > rounded end caps on the row. When `caps: true` and the first
   > and/or last visible segment has an effective `bg`, a 1-col cap
   > glyph is emitted on the corresponding side. The glyph variant
   > is selected globally via `Config.cap_style`. Each enabled cap
   > consumes 1 column from the row's usable bg-fill width.

3. **Powerline chevron prose** — correct the 0.2.3 description.
   Current text says fg = prev.bg, bg = next.bg for both glyphs.
   New text:

   > Segments are joined with a Powerline chevron whose colours
   > depend on the glyph style: `"solid"` (U+E0B0, filled wedge)
   > renders with fg = previous segment's bg and bg = next
   > segment's bg, so the wedge shape flows the prev colour into
   > the next region. `"thin"` (U+E0B1, line, the default) renders
   > with fg = next bg and bg = prev bg, so the line marks the
   > trailing edge of prev with a hint of next. The space before
   > the chevron renders in the prev bg, the space after in the
   > next bg. When adjacent backgrounds are equal (legacy uniform-
   > bg configs), the chevron fg falls back to `245` so the glyph
   > stays visible.

4. **New "End caps" subsection** inside `### Powerline`:

   ````markdown
   #### End caps

   Setting `caps: true` on a row adds a 1-col cap glyph to each end
   whose colour matches the first/last visible segment's effective
   background. The visual silhouette of the row gains a rounded,
   squared, or slanted edge depending on `Config.cap_style`.

   ```json
   {
     "render": {
       "powerline": true,
       "cap_style": "round",
       "rows": [
         {"caps": true, "palette": ["234", "236", "238"], "segments": [
           {"type": "model", "fg": "33", "bold": true},
           {"type": "cwd", "fg": "245"}
         ]}
       ]
     }
   }
   ```

   Each cap consumes 1 column from the usable bg-fill width — a
   row with `caps: true` and `ttyCols=80, margin=2` has
   `80 - 2*margin - 2*1 = 74` usable cols instead of `76`. The
   `"square"` style emits no glyph at all; it just paints a 1-col
   bg-extending space, useful when the patched Powerline font is
   not available.
   ````

## Rollout

Per project memory `release_workflow_gates`:

1. Implement on `development-0.2.4-work` with TDD commits via
   subagent-driven-development.
2. Squash → `development-0.2.4-main`.
3. No-ff merge → `main`.
4. Tag `production-0.2.4` from `main`; push origin.

Three explicit user confirmations between phases — do not batch.

The session quick-hacks were discarded from the working tree
before this branch was created.

## Risks

- **Chevron-geometry visible diff for 0.2.3 users with alternating
  bgs.** The asymmetric thin/solid pairing changes the chev-cell
  bg/fg. Users who already adopted the alternating-Powerline look
  in 0.2.3 will see the chevron region's colours flip. This is
  intentional — it corrects a visual error — but documented in
  the migration note.
- **Slant-cap codepoints**: U+E0BC and U+E0BA may render
  differently across Powerline-patched fonts. The spec includes
  an explicit fallback path in the implementation note. The
  implementer must smoke-verify visually that the chosen pair
  produces the intended slant.
- **Square caps look like the regular bg-fill** without padding.
  Users who configure `cap_style: "square"` without expecting an
  end-padding effect may not see what they wanted. Mitigated by
  the docs example showing the three styles side by side.
- **Caps + Width=0 (no terminal-size detection)**: the padding
  step is skipped, so caps appear directly adjacent to segments
  with no padding between content and right-cap. Visually a
  squeezed row, but no padding budget to absorb. Documented as
  expected behaviour.
- **Same-bg fallback bypasses the asymmetric rule.** When the
  fallback fires (legacy uniform-bg row), chev-cell bg is still
  determined by style, but fg is the static `245`. This keeps
  the 0.2.0/0.2.2 visual for legacy configs unchanged.

## Open Questions

None.

## References

- Project memory: `release_workflow_gates`,
  `release_sizing_one_concept`, `statusline_aesthetic`,
  `02x_terminal_aware_layout`.
- Prior spec: `docs/superpowers/specs/2026-05-15-powerline-polish-design.md`
  (0.2.3 baseline, the spec this release corrects in section 4).
- Powerline glyph reference: Nerd Font Powerline-Extras
  (codepoints U+E0B0–U+E0BF).
