# 0.2.1 — Powerline Chevron Spacing Fix

## Summary

Add a single space on each side of the U+E0B1 chevron when joining
Powerline segments. The 0.2.0 renderer emits `<seg><chev><seg>` flush;
the thin chevron glyph reads as cramped between dense text. This patch
changes the join to `<seg> <chev> <seg>` and updates the full-width
padding math accordingly.

This is a pure rendering fix. No new config fields, no new dependencies,
no behaviour change for the natural-width (non-Powerline) path.

## Motivation

The 0.2.0 release shipped the Powerline renderer with the chevron
sitting flush against neighbouring segment text. With the previous
middle-dot separator (` · `) the spacing felt natural because the dot
itself had visual whitespace around it. The thin chevron is a line
glyph — without explicit padding it reads as a vertical bar glued to
both segments, which makes the row look stuffed.

The 0.2.1 patch addresses exactly this visual complaint. Terminal-size
detection, right-align, truncation and `min_cols` remain out of scope
and ship in later patches (0.2.2+).

## Scope

In:

- Add a literal space before and after the chevron in
  `renderRowPowerline`'s interleave loop.
- Update the full-width padding math to reserve the additional 2 cells
  per separator.
- New constant `powerlineSeparatorWidth = powerlineChevronWidth + 2`
  so the math has a named source of truth.
- One new test asserting the spacing pattern; one new test covering
  the corrected padding math with multiple segments.
- Documentation refresh in `docs/configuration.md` (Powerline section
  bullet about chevron + the one-line example output reference).

Out:

- Terminal-size detection (deferred to 0.2.2).
- `Config.Render.Width` override (deferred to 0.2.2).
- `{"type": "tty_size"}` diagnostic segment (deferred to 0.2.2).
- Right-align, truncation, `min_cols` (deferred to 0.2.3+).
- Making the chevron glyph or its fg configurable (still hardcoded).

## Design

### Single code change in `renderRowPowerline`

The interleave loop currently writes the chevron flush. The fix
inserts ` ` before the chevron and ` ` after it, then re-emits
`bgOpen` as before:

```go
// internal/pkg/render/render.go — renderRowPowerline, step 4
for i, part := range parts {
    if i > 0 {
        b.WriteString(" ")
        b.WriteString(chev)
        b.WriteString(" ")
        b.WriteString(bgOpen) // chev was fg-only; ensure bg before next segment
    }
    b.WriteString(part)
    b.WriteString(bgOpen) // segment's [0m killed bg; restore before chev/padding/reset
}
```

The two padding spaces inherit the row's background because `bgOpen`
is still in effect from the previous segment's tail re-emission. The
chevron itself temporarily overrides fg with `\x1b[38;5;245m` and
closes with `\x1b[39m`, leaving bg intact, so the trailing space
continues painted in row-bg.

### Full-width padding math

The padding loop currently reserves `(N-1) * powerlineChevronWidth`
cells for the separators. With the new ` <chev> ` form it must reserve
`(N-1) * powerlineSeparatorWidth` where the new constant accounts for
the two added spaces:

```go
// internal/pkg/render/render.go — Powerline constants block
const (
    powerlineChevron        = ""
    powerlineChevronWidth   = 1
    powerlineSeparatorWidth = powerlineChevronWidth + 2 // " <chev> "
    powerlineChevronFG      = "245"
)
```

```go
// internal/pkg/render/render.go — renderRowPowerline, step 5
used += (len(parts) - 1) * powerlineSeparatorWidth
```

`powerlineChevronWidth` stays as the bare glyph width so
`displayWidth` and similar low-level callers (none exist today but the
constant is a stable API for future segment-aware math) continue to
mean "width of the chevron rune". `powerlineSeparatorWidth` is the
per-join layout cost — the value used by the padding step.

### Behaviour invariants preserved

- Empty-segment dropping (0.1.11): unchanged. Dropped segments
  participate neither in the joined output nor in the `(N-1) *
  powerlineSeparatorWidth` term, because `N` is `len(parts)` after
  empties are filtered out.
- Bg re-emission: unchanged. The chevron still closes only fg
  (`\x1b[39m`), the surrounding `bgOpen` writes still happen at the
  same logical points.
- NO_COLOR fallback: unchanged. `renderRowPowerline` still returns
  early when `env.colorEnabled` is false; the natural-width path is
  untouched.
- Single-segment rows: unchanged. `len(parts)-1 == 0` means zero
  separators contribute to `used`, no spaces are emitted.

## Tests

Five test functions exist in `render_test.go` around
`renderRowPowerline`. The chevron-spacing fix affects them as follows:

| Test | Effect |
|---|---|
| `TestRenderRowPowerline_AllEmptyReturnsEmpty` | Unchanged — no segments rendered. |
| `TestRenderRowPowerline_OpensAndClosesRowBg` | Unchanged — single-segment row. |
| `TestRenderRowPowerline_ChevronBetweenSegments` | Unchanged — counts chevrons (still 2 for 3 segments). |
| `TestRenderRowPowerline_EmptySegmentDropsChevron` | Unchanged — counts chevrons (still 1 after empty drop). |
| `TestRenderRowPowerline_FullWidthPaddingWhenTTYKnown` | Unchanged — single-segment row, `(N-1)*w == 0`. |
| `TestRenderRowPowerline_NoPaddingWhenTTYIsZero` | Unchanged — single-segment row. |
| `TestRenderRowPowerline_NoBgSkipsBgEscape` | Unchanged — does not assert chevron neighbours. |
| `TestRenderRowPowerline_NoColorReturnsEmpty` | Unchanged. |
| `TestRender_PowerlineFalseUsesNaturalPath` | Unchanged. |
| `TestRender_PowerlineTrueEmitsBgAndChevron` | Unchanged — only asserts bg + chevron presence. |

No existing assertions hardcode the byte pattern around the chevron,
so the change is regression-safe for the existing suite. To prevent
silent regression of the new behaviour, two new tests are required:

1. **`TestRenderRowPowerline_ChevronHasSpacingAroundIt`** — three
   text segments `A`, `B`, `C`, no row bg. Strip ANSI from the output
   via `ansiRegexp`. The stripped string must contain
   `" " + powerlineChevron + " "` exactly twice. Assertion failure
   message must include the stripped form for debuggability.

2. **`TestRenderRowPowerline_MultiSegmentPaddingHonoursSpacing`** —
   three text segments `A`, `B`, `C` with `Bg: "234"` and
   `ttyCols: 80`. `displayWidth(got)` must equal 80. The visible
   content before padding is `1 + 1 + 1` segment cols plus
   `2 * (1 + 2)` separator cols = 9 cols; padding fills the remaining
   71 cols. With the old math this test would expect 75 and fail.

Goldens: the five fixtures under `internal/pkg/render/testdata/golden/`
do not enable Powerline, so no `-update` regeneration is needed. A
direct grep against the testdata confirms zero references to
`powerline`.

## Documentation

`docs/configuration.md`, Powerline section bullet about the chevron
(currently around line 97):

> Segments are joined with the U+E0B1 thin chevron in a muted-grey
> foreground (`245`). The chevron has no background of its own, so
> the row's background shows through.

Replace with:

> Segments are joined with the U+E0B1 thin chevron in a muted-grey
> foreground (`245`), with a single space on each side for breathing
> room. The chevron has no background of its own, so the row's
> background shows through the spaces and the glyph.

No other docs change.

## Rollout

Standard release pipeline per project memory `release_workflow_gates`:

1. Create `development-0.2.1-work` from `main`. Implement on -work
   with TDD commits (test fail → impl → test pass → docs).
2. Squash `-work` into `development-0.2.1-main` (rebased against
   current `main` first). One commit on -main.
3. No-ff merge `-main` into `main`.
4. Tag `production-0.2.1` from `main`; push origin.

Three explicit user confirmations between phases per memory rule —
do not batch.

## Risks

- **Width-math drift**: a future contributor changing one of the two
  constants but not the other could desync padding from interleave.
  Mitigation: `powerlineSeparatorWidth` is derived from
  `powerlineChevronWidth + 2` at constant-definition time, so the
  relationship is mechanically tied. A doc comment on the constant
  spells out the `" <chev> "` derivation.
- **User running 0.2.0 + 0.2.1 in alternating terminals**: bg-fill
  width was already non-functional in 0.2.0 under Claude Code spawn
  (see memory `02x_terminal_aware_layout`), so the spacing change is
  the *only* visible difference. No upgrade complications.
- **Subagent regression on existing tests**: implementer must not
  "modernise" the unchanged tests listed in the table above. The
  controller verifies the diff touches only the two new tests plus
  the two production-code edits.

## Open Questions

None.

## References

- Project memory: `release_workflow_gates`, `release_sizing_one_concept`,
  `statusline_aesthetic`, `02x_terminal_aware_layout`.
- Prior spec: `docs/superpowers/specs/2026-05-13-powerline-design.md`
  (the 0.2.0 design this patch refines).
