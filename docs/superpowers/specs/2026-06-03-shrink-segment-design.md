# Design: dynamic `Shrink` segment (0.2.33)

## Problem

In a narrow terminal the second default row (`git_branch | lines | cwd |
version`) overflows when the branch and directory names are long. The
`version` segment is right-aligned, so when the row exceeds the usable
width the right-align gap collapses to zero, the row renders inline past
`ttyCols`, and the terminal clips the rightmost cells — truncating the
version string (e.g. `v0.2.3…` losing its trailing digits).

Existing layout primitives do not solve this:

- `MaxWidth` is a **static** per-segment cap; it cannot react to the
  actual terminal width.
- `Wrap` reflows segments onto a new row, but the right-aligned version
  row is explicitly excluded from reflow.
- `MinCols` hides a segment entirely below a width threshold — too
  coarse; we want to *keep* `cwd`, just shorter.

## Goal

When a row would overflow, dynamically shorten a designated segment
(`cwd` in the default config) so the remaining segments — notably the
right-aligned `version` — render in full. Truncation must be dynamic
(driven by the live terminal width), reuse the existing `…` ellipsis,
and degrade gracefully when even maximal shrinking is not enough.

## Design

### 1. New field `Segment.Shrink bool` (JSON `shrink`)

Marks a segment as "yields width when its row overflows". Sits alongside
the existing `Wrap`, `MinCols`, and `MaxWidth` fields in the `Segment`
struct. The default config sets `Shrink: true` on the `cwd` segment in
row 2.

### 2. New function `applyShrink`

```go
func applyShrink(p *payload, row Row, env renderEnv, powerlineActive bool, sep string) []Segment
```

A pre-pass that returns a (possibly modified) copy of `row.Segments`
with the effective `MaxWidth` of the `Shrink` segment(s) reduced so the
row fits. Behaviour:

1. `env.ttyCols == 0` (width unknown) → return segments unchanged.
2. No segment has `Shrink == true` → return unchanged.
3. Measure `used` and `usable` exactly as `rowOverflows` does — sum each
   visible segment's `displayWidth`, add the per-style separator cost
   (`powerlineSeparatorWidth` per join in Powerline mode, else
   `displayWidth(sep)`), and subtract cap columns when
   `powerlineActive && row.Caps`. `displayWidth` strips ANSI, so styled
   bodies measure to their visible width — no separate plain-body path
   is needed.
4. `used <= usable` → return unchanged.
5. `deficit = used - usable`. Walk the `Shrink` segments in row order;
   each yields up to `(its current visible width − floor)` columns until
   the deficit is covered. For each segment that yields, set its
   effective `MaxWidth = currentWidth − taken`.
6. **Floor = 1** (the width of the `…` glyph). A segment never shrinks
   below 1 column.
7. If the total available yield is smaller than the deficit (branch +
   version alone exceed `usable`), every `Shrink` segment ends at its
   floor and the row still overflows — the existing inline-overflow
   behaviour applies (best-effort; the user accepted this fallback).
8. A user-supplied `MaxWidth` is never increased — `applyShrink` only
   lowers it (the measured `currentWidth` already reflects any static
   cap, and `currentWidth − taken < currentWidth`).

### 3. Reuse of existing truncation

`applyShrink` only adjusts `MaxWidth`. The actual cut happens in the
existing `renderSegment` → `truncateToWidth` path, which appends `…`
(U+2026) and is runewidth-correct (emoji, CJK fullwidth, ZWJ). No new
ANSI-aware truncation code is introduced.

### 4. Placement in `Render`

`applyShrink` runs per output row in `Render`, **after**
`expandWrappedRows` (so it sees the final, already-reflowed rows) and
**before** the per-row dispatch (`renderRowRight` /
`renderRowPowerline` / `renderRowNatural`). Because all three renderers
call `renderSegment`, the adjusted `MaxWidth` is honoured in every mode.

```text
rows = expandWrappedRows(...)
for each output row:
    row.Segments = applyShrink(p, row, rowEnv, powerlineActive, sep)
    dispatch(row)
```

### 5. Right-align interaction

Correct by construction. When `used > usable`, the Powerline
`rightAlignGap` is already `max(usable-used, 0) == 0`, so the
gap-free `used`/`usable` measurement matches the real overflow
condition. After shrinking, the row fits and the gap re-expands, pulling
`version` flush to the right edge again.

## Testing (TDD — tests first)

`applyShrink` unit tests:

- `ttyCols == 0` → segments returned unchanged.
- No `Shrink` segment → unchanged even when overflowing.
- Row fits (`used <= usable`) → unchanged.
- Overflow → `Shrink` segment's `MaxWidth` set so the row's measured
  width equals `usable` (version/other segments full).
- Overflow beyond available yield → `Shrink` segment at floor (1),
  row still overflows (documents best-effort fallback).
- User-set `MaxWidth` is only lowered, never raised.
- Multiple `Shrink` segments → deficit absorbed in row order, each
  flooring at 1.

Integration test via `Render`:

- Narrow `Width`, default config, long branch + long `cwd`: assert
  `version` string appears in full and `cwd` body contains `…`.

## Non-goals

- Shrinking `git_branch` (user scoped this to `cwd`; only segments with
  `Shrink: true` participate).
- Proportional/weighted distribution beyond simple row-order yield.
- Any change to the static `MaxWidth`, `Wrap`, or `MinCols` semantics.
