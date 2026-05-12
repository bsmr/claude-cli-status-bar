# Selective threshold target — 0.1.12 design

Status: approved 2026-05-12.

## Motivation

ccsb 0.1.10 introduced `Segment.Thresholds` so percentage-bearing
segments (`context`, `limit_5h`, `limit_7d`) could escalate their
foreground color as the metric crosses configured breakpoints. The
escalation wraps the *entire* segment string — bar cells, percentage
digits, token counts, and label all switch color together.

In live use this turned out to be visually noisy: the wide
`[██████████████░░] 92% 920k/1M` bar going fully red at 90 % dominates
the line. The user wants the threshold-driven color to escalate **only
the percentage digits**, leaving the rest of the segment in its static
foreground.

## Wider context — phases

This spec covers one phase of a three-phase statusline overhaul the
user requested on 2026-05-12. The phases are intentionally separated by
the project's per-concept release rule:

- **Phase 1 (no release)** — direct user-config edit:
  two-row layout, `mode` segment between `model` and `context`, drop
  `cost`, add `lines`, temporarily disable thresholds so the
  pre-0.1.12 "anstrengende" coloring is not in play. Lands as a
  `~/.config/ccsb/config.json` change after 0.1.12 ships.
- **Phase 2 (0.1.12 `feat`) — this spec.** Selective threshold target
  so the user can re-introduce escalation with `threshold_target: "pct"`.
- **Phase 3 (0.2.0 `feat`)** — Powerline-style segment chevrons +
  terminal-aware layout (also tracked in
  `~/.claude/projects/.../memory/02x_terminal_aware_layout.md`). Out of
  scope here; gets its own brainstorming session.

## Schema

A new optional field on `Segment`:

```go
type Segment struct {
    // … existing fields …
    Thresholds      []Threshold `json:"thresholds,omitempty"`
    ThresholdTarget string      `json:"threshold_target,omitempty"`
}
```

Accepted values:

- `""` (omitted) or `"all"` — current 0.1.10 behaviour: the threshold
  override applies to the segment's full text via `style()` at the
  renderer's outer wrap.
- `"pct"` — the segment renders the percentage digits in the
  threshold-chosen color while the rest of the segment text stays in
  the segment's static `fg`.

Any other value is treated as `"all"` (silent fallback — same posture
as unknown segment types staying out of the user's way).

The field is meaningful only on segments that have a percentage metric
(`context`, `limit_5h`, `limit_7d`). Other segments parse it without
error but ignore it.

## Renderer contract change

`internal/pkg/render/segments.go` currently states (line 10-12):

> segmentFunc renders one segment. It MUST return "" to suppress the
> segment (the row joiner skips empty results), and MUST NOT return
> ANSI escape codes — colour wrapping happens in renderSegment via
> style().

The doc comment is loosened. After 0.1.12 it reads:

> segmentFunc renders one segment. It MUST return "" to suppress the
> segment (the row joiner skips empty results). It MAY emit ANSI
> escape sequences for sub-region styling (e.g. threshold coloring of
> a percentage substring); when it does, it MUST close every opened
> sequence before returning so the renderer's outer `style()` wrap is
> not broken.

This relaxation is what unlocks selective styling without restructuring
the segment-function signature to return parts.

## Renderer dispatch change

`renderSegment` (`internal/pkg/render/render.go`) needs to know when to
suppress the outer `style()` color wrap because the segment is doing
sub-region coloring itself.

```go
func renderSegment(p *payload, s Segment, env renderEnv) string {
    fn, ok := segmentFuncs[s.Type]
    if !ok {
        return "?" + s.Type + "?"
    }
    out := fn(p, s, env)
    if out == "" {
        return ""
    }
    // For threshold_target=="pct", the segment function has already
    // wrapped the percentage digits in the threshold color. The outer
    // style() must use the static FG (not the threshold override) so
    // the non-pct regions render in the segment's neutral color.
    fg := s.FG
    if s.ThresholdTarget != "pct" {
        fg = chooseFG(s, p)
    }
    return style(out, fg, s.BG, s.Bold, env.colorEnabled)
}
```

`chooseFG` stays unchanged.

## Per-segment changes

Each of `renderContext`, `renderLimit5h`, `renderLimit7d` gains the
ability to wrap its percentage digits in the threshold-chosen color
when `Segment.ThresholdTarget == "pct"`.

The helper that does the wrapping lives in `internal/pkg/render/render.go`
alongside `chooseFG` so segment files stay free of `style()`-level
knowledge:

```go
// wrapPct returns the percentage substring wrapped in the threshold
// FG if Segment.ThresholdTarget=="pct" and a matching threshold
// exists; otherwise it returns the input unchanged.
//
// The wrap is self-contained: after the inner threshold FG is open
// and applied to pctText, it closes back to the segment's static FG
// (or to "\x1b[39m" terminal-default if the segment has no static FG)
// so that segment text after the wrap continues in the surrounding
// color. The outer style() wrap is still in effect — wrapPct nests
// inside it.
func wrapPct(pctText string, s Segment, p *payload, colorEnabled bool) string {
    if !colorEnabled || s.ThresholdTarget != "pct" {
        return pctText
    }
    fg := chooseFG(s, p)
    if fg == "" || fg == s.FG {
        // No threshold matched (or threshold equals static) —
        // nothing to override, leave as-is.
        return pctText
    }
    openInner := fg256(fg)
    if openInner == "" {
        return pctText
    }
    // Close back to the segment's static FG so surrounding text
    // continues in that color, not in the terminal default.
    closeInner := "\x1b[39m"
    if reopen := fg256(s.FG); reopen != "" {
        closeInner = reopen
    }
    return openInner + pctText + closeInner
}
```

Two SGR closer cases:

- Segment has a static `FG` set (the typical case) → close by reopening
  that color so the remaining segment text stays in it.
- Segment has empty `FG` → close with `\x1b[39m` (terminal-default
  foreground) which cancels the inner fg without touching bg or bold.

Both leave the outer `style()` `\x1b[0m` reset to do the final cleanup.

### `renderContext`

The bar/pct/tokens assembly already lives in one `fmt.Sprintf`. With
`ThresholdTarget == "pct"`, only the `%d%%` group needs the wrap. The
restructuring is small:

```go
pctText := fmt.Sprintf("%d%%", pct)
pctStyled := wrapPct(pctText, s, p, colorEnabled)
// …
return fmt.Sprintf("%s %s %s/%s", bar, pctStyled, formatTokens(used), formatTokens(p.Context.ContextWindowSize))
```

The `colorEnabled` flag must propagate into the segment. Currently
`renderEnv` carries it; the segment functions receive `renderEnv` as
their third argument, so this is a non-breaking thread-through.

For `style: "bar"`, no percentage digits are emitted — wrapPct is not
called.

For `style: "pct"` (no bar), only the percentage is emitted — wrapPct
wraps it. Same code path.

### `renderLimit5h` / `renderLimit7d`

The shared helper `renderLimit` produces the percentage text via
`formatPct(rl.UsedPercentage)`. The same pattern:

```go
pctStyled := wrapPct(formatPct(rl.UsedPercentage), s, p, env.colorEnabled)
// …
return fmt.Sprintf("%s:%s (%s)", label, pctStyled, formatCountdown(...))
```

All three `style` variants of `renderLimit` (`""`/`"pct"`, `"bar"`,
`"bar+pct"`) emit the percentage text exactly once, so the wrap site
is unambiguous.

## Backward compatibility

- Segments without `threshold_target` (i.e. every config that exists
  today) parse to `ThresholdTarget == ""` and follow the existing
  `"all"` semantics in `renderSegment`. No behaviour change.
- A segment that sets `threshold_target: "pct"` without configuring
  `thresholds` gets `chooseFG == s.FG`, so `wrapPct` returns the input
  unchanged — visually identical to a no-threshold segment. Safe.
- A segment that sets `threshold_target: "pct"` on a non-percentage
  type (e.g. `model`) gets `segmentMetric == false` in `chooseFG`, so
  the threshold lookup falls through to `s.FG` and `wrapPct` is a
  no-op. Safe.

## Tests

Add to `internal/pkg/render/render_test.go`:

1. `TestWrapPct_AllTargetIsNoOp` — `ThresholdTarget == ""` returns the
   pct text unchanged regardless of thresholds.
2. `TestWrapPct_PctTargetWithoutMatchIsNoOp` — `ThresholdTarget == "pct"`
   but percentage below all thresholds → returns input unchanged.
3. `TestWrapPct_PctTargetMatchWithStaticFG` — segment has `FG: "245"`,
   `ThresholdTarget: "pct"`, matching threshold `{min:90, fg:"160"}` at
   95 % → returns `"\x1b[38;5;160m95%\x1b[38;5;245m"`. Closer reopens
   the static FG, not the terminal default.
4. `TestWrapPct_PctTargetMatchWithEmptyFG` — segment has `FG: ""`,
   `ThresholdTarget: "pct"`, matching threshold → returns
   `"\x1b[38;5;160m95%\x1b[39m"`. Closer is the terminal-default SGR.
5. `TestWrapPct_NoColorReturnsRawText` — `colorEnabled == false`
   returns input regardless of target.
6. `TestRender_ContextPctTargetColorsOnlyDigits` — full Render() pass:
   payload with `used_percentage: 95`, segment with `FG: "245"`,
   `thresholds: [{min:90, fg:"160"}]`, `threshold_target: "pct"`.
   Assert the output contains `"\x1b[38;5;160m95%\x1b[38;5;245m"` and
   does *not* contain `"\x1b[38;5;160m["` (the bar should not start
   in the threshold color).
7. `TestRender_Limit5hPctTargetColorsOnlyDigits` — same shape for
   `limit_5h`. Output contains the inner-wrap; countdown is unstyled.
8. `TestRender_AllTargetStillWorksAsBefore` — regression: with
   `threshold_target` omitted (or `"all"`), the 0.1.10 behaviour holds
   — output wraps the *full* segment in the threshold FG via the
   outer `style()` call.

## Documentation

`docs/configuration.md` updates:

- Add `threshold_target` to the per-segment field table.
- Update the "Thresholds" subsection: split into two paragraphs —
  the current `"all"` behaviour (default) and the new `"pct"`
  behaviour. Include an example showing `threshold_target: "pct"`.
- Update each of context / limit_5h / limit_7d entries to mention
  that `threshold_target: "pct"` localises the threshold escalation
  to the percentage digits.

## Out of scope

- Sub-region targeting other than the percentage digits (e.g.
  threshold-color the bar fill cells, or the label). The user
  explicitly said "nur die Prozent-Ziffern" and rejected wider
  coverage on 2026-05-12.
- Background-color thresholds (`bg`). The 0.1.10 schema also limits
  thresholds to `fg`; carry the same constraint forward.
- A `Threshold.Bold` field. Same reason — would require segment-aware
  re-application of bold after the wrap, which is not worth the
  complexity for one use case the user has not asked for.
- Sub-region thresholds for non-percentage segments (`cost` digits,
  `lines` counters). The trigger condition for percentages
  (`UsedPercentage`) does not generalise to absolute counters; another
  spec round when there is concrete demand.
- Powerline separators with fg/bg transitions, and terminal-width-aware
  layout. Both are Phase 3 / 0.2.0 territory; see
  `~/.claude/projects/.../memory/02x_terminal_aware_layout.md` for the
  TTY-probe results that informed that decision.
