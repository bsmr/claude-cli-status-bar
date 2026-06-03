# Configuration

`ccsb` reads a single JSON file at startup. This document describes the
on-disk shape and the segment vocabulary used by the native renderer.

## File location

```
${XDG_CONFIG_HOME:-$HOME/.config}/ccsb/config.json
```

The file is created with `0600` permissions and may not exist on a fresh
install — `ccsb` treats a missing file as an empty config (zero values for
every field).

## Top-level shape

```json
{
  "proxy":  { "command": "npx", "args": ["-y", "ccstatusline@latest"] },
  "backup": { "previous_status_line": { /* … */ } },
  "render": { "rows": [ /* … */ ], "separator": " | " }
}
```

| Key      | Purpose                                                                                |
| -------- | -------------------------------------------------------------------------------------- |
| `proxy`  | When `proxy.command` is non-empty, ccsb forwards the payload to that child process and prints its stdout. When empty (or the key is absent), the in-binary native renderer drives the status line. Toggle with `ccsb mode native` / `ccsb mode proxy`. |
| `backup` | Holds the verbatim `statusLine` value that was in `~/.claude/settings.json` at install time, so `ccsb uninstall` can restore it. Never written by `ccsb mode`. |
| `render` | Native-renderer config. Ignored while proxy mode is active. |

All three keys are optional. With every key omitted, ccsb runs the native
renderer with the default layout.

## `render`

### Fields

| Field       | Type            | Default       | Effect                                                      |
| ----------- | --------------- | ------------- | ----------------------------------------------------------- |
| `rows`      | array of arrays | default rows  | Each inner array is one output line. Segments in a row are joined with `separator`. |
| `separator` | string          | `" \| "`      | Joiner between segments in the same row.                    |
| `powerline` | bool            | `false`       | When true, each row gets a coloured background fill (from `bg`, `palette`, or the built-in default palette) and segments are joined with a Powerline chevron. The glyph is selectable via `powerline_style`. See [Powerline](#powerline). |
| `width`     | int             | `0`           | Explicit terminal-cols override. When `> 0`, ccsb skips terminal-size detection and uses this value. Default `0` runs the detection chain (`/dev/tty` ioctl, then `/proc` parent-process walk). |
| `margin`    | int             | `2`           | Plain (no-bg) leading spaces per row; usable bg-fill width is shrunk by `2*margin`. Leaves room for Claude Code's built-in statusLine chrome on each side. Set to `0` to disable. Defaults to `2` when omitted; negative values clamp to `0`. |
| `powerline_style` | string    | `"thin"`      | Chevron glyph in Powerline mode. `"thin"` (default) renders U+E0B1; `"solid"` renders U+E0B0 as a filled wedge. Unknown values silently fall back to `"thin"`. |
| `cap_style` | string | `"round"` | End-cap glyph style applied when a row's `caps` is true. `"round"` (default) renders U+E0B6 / U+E0B4 half-circles; `"square"` extends the bg with a 1-col plain space on each side; `"slant"` renders U+E0BC / U+E0BA filled triangles. Unknown values fall back to `"round"`. |
| `palette` | array of strings | `232`…`240` | Shared global background palette for Powerline mode. Each output row starts at index `rowIndex * palette_stride` and rotates through the palette per segment. A per-row `palette` (see [Powerline](#powerline)) overrides this for that one row. When omitted, the built-in nine-grey monotonic dark→light default (`232`…`240`) is used. |
| `palette_stride` | int | `2` | Per-output-row offset into the global `palette`: row N's first segment starts at `N * palette_stride`, so every row (including reflow-inserted rows) begins a couple of shades brighter than the one above and the dark→light gradient never jumps or repeats. `0` uses the default (`2`); negative clamps to `0`. |

When `rows` is omitted or empty, the renderer uses the built-in
default. The default is the full Powerline layout — two rows with
round end caps, a solid chevron, a shared monotonic grey palette with
a per-output-row stride, threshold-coloured percentages, and a
right-aligned version stamp.
`powerline`, `powerline_style`, `cap_style`, `palette`, and
`palette_stride` are also filled in from the default when (and only
when) `rows` is empty, so the
out-of-the-box look does not depend on a config file. See
[Default layout](#default-layout) for the exact equivalent JSON.

A row whose segments all render to empty strings is omitted from the
output; the next row moves up. Empty segments inside a row are dropped
before joining, so adjacent separators are never doubled. When the
default layout produces no segments at all (e.g. a `{}` payload), the
renderer falls through to the [last-resort fallback](#last-resort-fallback)
so the bar is never blank. A user-supplied `rows` that intentionally
renders empty stays empty — the fallback is reserved for the default
layout.

### Row shape

Each entry in `rows` is either:

- **Legacy array form** (0.1.x compatibility) — a bare array of
  segments. The row has no background and is rendered with the
  configured `separator`:

  ```json
  [{"type": "model"}, {"type": "context", "style": "bar+pct"}]
  ```

- **Object form** (0.2.0 native) — `{"segments": [...], "bg": "234"}`.
  The `bg` field is meaningful only when `powerline` is true; it
  fills the row from column 0 to the terminal's right edge in
  Powerline mode and is ignored otherwise.

Both shapes can be mixed within the same `rows` array. `ccsb`
writes back the object form whenever it persists the config (e.g.,
on `ccsb install` or `ccsb mode`), so a legacy config is migrated
automatically the next time ccsb modifies the file.

Within the object form, an optional `palette` field accepts an array
of ANSI 256-color strings:

```json
{"palette": ["234", "236", "238"], "segments": [...]}
```

When set, the palette rotates across the row's **visible** segments —
empty segments (e.g. `mode` when neither thinking nor fast_mode is
set) do not consume a palette slot. Index N takes
`palette[N % len(palette)]`.

The object form also accepts an optional boolean `caps` field. When
`caps: true` and the first or last visible segment has an effective
bg, a 1-col cap glyph is emitted on the corresponding side. The
glyph variant is selected globally via [`cap_style`](#fields). Each
enabled cap consumes 1 column from the row's usable bg-fill width.

```json
{"caps": true, "palette": ["234", "236", "238"], "segments": [...]}
```

The object form also accepts an optional `align` field. With
`"align": "right"` the entire row is rendered as plain text flush to
the right edge of the usable width, bypassing Powerline (no row-bg,
no chevrons) — useful for a discreet trailing line such as a version
stamp. Any other value (including the omitted default) renders the
row left-aligned in whatever mode the rest of the config selects.
Per-segment right alignment within an otherwise normal row is
documented as the [`align` common field](#common-fields).

The effective background of each segment is resolved in priority
order: explicit `Segment.bg` > `Row.palette` rotation > `Row.bg`
(uniform fill) > built-in `defaultPalette` of three subtle dark
greys (`["234", "236", "238"]`, applied only when Powerline is
enabled). If all four are unset and Powerline is off, the segment
has no background.

### Last-resort fallback

If the JSON payload from Claude Code is unparsable, ccsb emits a single
`<model> · <cwd>` line (or just one of the two, or the literal
`claude-cli-status-bar`) so the status bar is never blank. Configuration
does not affect this path.

### Powerline

With `powerline: true`, the renderer switches to a Powerline-style
row layout:

- Each `Row.Bg` is opened at the start of the row and fills the
  line all the way to the terminal's right edge. The width is taken
  from `width` if set, otherwise from `/dev/tty + ioctl(TIOCGWINSZ)`,
  otherwise from a `/proc` parent-process walk. When every source
  fails, the row falls back to natural width and the bg ends after
  the last segment.
- Segments are joined with a Powerline chevron whose colours depend
  on the glyph style. `"solid"` (U+E0B0, filled wedge) renders with
  fg = previous segment's bg and bg = next segment's bg, so the
  wedge shape flows the prev colour into the next region. `"thin"`
  (U+E0B1, line; the default) renders with fg = next bg and
  bg = prev bg, so the line marks the trailing edge of prev with a
  hint of next. The space before the chevron renders in the prev
  segment's bg, the space after in the next segment's bg. When
  adjacent backgrounds are equal (legacy uniform-bg configs without
  a palette), the chevron foreground falls back to `245` so the
  glyph stays visible. Select the glyph style via `powerline_style`.
- Per-segment `fg` / `bg` / `bold` continue to apply *inside* the
  row-bg. A segment with its own `bg` overrides the row-bg for
  that segment's text.
- Empty segments (e.g. `mode` when neither thinking nor fast_mode
  is set) are dropped together with the surrounding chevron, so
  the chain stays seamless.
- With `NO_COLOR`, Powerline degrades to the natural-separator
  path: no bg, no chevron, segments joined with the configured
  `separator`. The output is identical to a plain 0.1.x render.

Example two-row config with a two-tone palette:

```json
{
  "render": {
    "powerline": true,
    "rows": [
      {"bg": "234", "segments": [
        {"type": "model", "fg": "33", "bold": true, "show_1m_flag": true},
        {"type": "mode"},
        {"type": "context", "style": "bar+pct", "fg": "245",
         "threshold_target": "pct",
         "thresholds": [{"min": 70, "fg": "136"}, {"min": 90, "fg": "160"}]},
        {"type": "limit_5h", "fg": "245", "threshold_target": "pct",
         "thresholds": [{"min": 70, "fg": "136"}, {"min": 90, "fg": "160"}]},
        {"type": "limit_7d", "fg": "245", "threshold_target": "pct",
         "thresholds": [{"min": 70, "fg": "136"}, {"min": 90, "fg": "160"}]}
      ]},
      {"bg": "237", "segments": [
        {"type": "git_branch", "fg": "33"},
        {"type": "lines", "fg": "245"},
        {"type": "cwd", "fg": "245"}
      ]}
    ]
  }
}
```

For a classic alternating-Powerline look within a single row, use
`palette` instead of a single `bg`:

```json
{
  "render": {
    "powerline": true,
    "rows": [
      {"palette": ["234", "236", "238"], "segments": [
        {"type": "model", "fg": "33", "bold": true},
        {"type": "context", "style": "bar+pct", "fg": "245"},
        {"type": "limit_5h", "fg": "245"},
        {"type": "limit_7d", "fg": "245"}
      ]},
      {"palette": ["237", "238", "237"], "segments": [
        {"type": "git_branch", "fg": "33"},
        {"type": "cwd", "fg": "245"}
      ]}
    ]
  }
}
```

Each visible segment gets a bg from its row's palette (modulo
rotation), and the chevron between segments transitions between
those bgs. When `powerline` is `true` and neither `bg` nor
`palette` is set on a row, the built-in default palette
`["234", "236", "238"]` is applied automatically.

**Migration from uniform `bg` to `palette`** (0.2.2 → 0.2.3): if
your existing config uses a single `bg` per row, the visual is
preserved unchanged in 0.2.3 — chevrons fall back to the muted-grey
`245` foreground on the uniform background. To opt into the
alternating-Powerline look, replace `bg` with `palette` on each
row:

```diff
-{"bg": "234", "segments": [...]}
+{"palette": ["234", "236", "238"], "segments": [...]}
```

You can keep `bg` alongside `palette` — `palette` wins for segment
fills, and `bg` becomes the row-level fallback that the renderer
ignores when the palette is non-empty.

The chevron glyph is selectable via `powerline_style`; its colours
are derived from adjacent backgrounds.

#### End caps

Setting `caps: true` on a row adds a 1-col cap glyph to each end
whose colour matches the first/last visible segment's effective
background. The visual silhouette of the row gains a rounded,
squared, or slanted edge depending on the global `cap_style`.

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

Each cap consumes 1 column from the row's usable bg-fill width. The
three styles:

- `"round"` (default) — U+E0B6 / U+E0B4 half-circles. Filled glyph
  in the segment's bg colour on the terminal's default bg, producing
  a rounded edge.
- `"square"` — no glyph; a 1-col plain bg-painted space on each
  side. Useful when the patched Powerline font is unavailable. The
  visual is a flat bg extension without a curved edge.
- `"slant"` — U+E0BC / U+E0BA filled triangles. Diagonal edge in
  the segment's bg colour.

Unknown `cap_style` values fall back to `"round"`.

## Segments

Every segment is an object with at least a `type` field. The renderer
recognises 17 types.

### Common fields

These fields are interpreted by the renderer wrapper, not by individual
segment functions, so they apply to every type:

| Field   | Type    | Notes                                                                       |
| ------- | ------- | --------------------------------------------------------------------------- |
| `type`  | string  | Required. Dispatch key. Unknown types render `?<type>?` so typos are visible. |
| `fg`    | string  | ANSI 256-color foreground as a decimal string `"0"`–`"255"`. Empty = no FG. |
| `bg`    | string  | ANSI 256-color background as a decimal string `"0"`–`"255"`. Empty = no BG. |
| `bold`  | bool    | When true, wraps the segment text in `ESC[1m … ESC[0m`.                     |
| `align` | string  | `"right"` anchors this segment (and every later segment in the same row) to the right edge of the usable width. The slack between the preceding left group and the first right-aligned segment becomes padding; in Powerline mode that padding inherits the bg of the last left-aligned visible segment so the streak stays continuous. Degrades to inline (no padding) when terminal width is unknown or the row already overflows. Unknown values are treated as left (the default). Distinct from `Row.align="right"`, which forces the whole row right and bypasses Powerline. |
| `wrap`  | bool    | When true, marks the segment as eligible for row-overflow reflow. If the containing row's visible content exceeds the usable width (`ttyCols - 2*margin` minus cap columns when active), every `wrap: true` segment is pulled out and joined into a new row inserted directly after the original. The new row inherits the parent row's `bg` / `palette` / `caps` / Powerline mode; palette rotation restarts at index 0. Reflow degrades to a no-op (segments stay inline) when `ttyCols` is unknown or the row already fits. In the default layout `limit_5h` and `limit_7d` carry this flag so a narrow terminal automatically lifts them onto their own line. |
| `max_width` | int | When positive, caps the visible column width of the segment's rendered body. Longer bodies are shortened to `max_width − 1` columns and suffixed with `…` (U+2026); shorter bodies pass through unchanged. Zero (the default) disables truncation. Truncation runs on the segment's **plain** body before the style wrap, so it is safe for text-only segments (`text`, `cwd`, `git_branch`, `model`, …). Segments that self-embed ANSI escapes for sub-styling (`context`, `limit_5h`, `limit_7d` when `threshold_target: "pct"`) should leave `max_width` at 0 — truncating across an embedded escape can leave the terminal in an unintended SGR state. |
| `min_cols` | int | When positive, suppresses the segment when the detected terminal width is strictly narrower than `min_cols`. The hidden segment behaves like an empty render: no body, no chevron, no palette slot. The gate runs before the segment function fires, so hidden segments cost nothing. When the terminal width is unknown (`ttyCols == 0`, e.g. no `/dev/tty`, no `/proc` walk hit, no `Config.Width` override) the gate is bypassed — "no info to gate on" defaults to "keep the segment". Zero (the default) disables the gate. Pairs naturally with [`max_width`](#) and [`wrap`](#) to build layouts that gracefully shed segments as the terminal narrows. |
| `shrink` | bool | When true, marks the segment as the one that yields display width when its row would overflow. After reflow, a pre-pass measures the row; if the visible content exceeds the usable width (`ttyCols - 2*margin` minus cap columns when active), every `shrink: true` segment has its effective `max_width` lowered — in row order, each yielding down to a 1-column floor (the `…` glyph) — until the overflow is absorbed. The cut reuses the `max_width` path, so `shrink` is the **dynamic** counterpart to that **static** cap and is safe on the same text-only segments (`text`, `cwd`, `git_branch`, `model`, …). A user-set `max_width` is only ever lowered further, never raised. A no-op when `ttyCols` is unknown or the row already fits. In the default layout `cwd` carries this flag so the right-aligned `version` stamp stays intact on narrow terminals. |

Per-segment additional fields are documented under each type below.

### Type-specific fields

| Field           | Used by                              | Notes                                                |
| --------------- | ------------------------------------ | ---------------------------------------------------- |
| `label`         | `text`, `effort`, `output_style`, `git_branch`, `limit_5h`, `limit_7d` | Overrides each segment's default label prefix. |
| `format`        | `cwd`, `cost`                        | Per-type format string or shape selector.            |
| `show_1m_flag`  | `model`                              | Appends `" 1M"` when the payload's `exceeds_200k_tokens` is true. |
| `style`         | `context`, `limit_5h`, `limit_7d`    | Selects between presentation variants.               |
| `bar_width`     | `context`, `limit_5h`, `limit_7d`    | Circle-bar length in cells. Default `16`; zero or negative falls back to the default. See [`bar_width`](#bar_width). |
| `thresholds`    | `context`, `limit_5h`, `limit_7d`    | Per-segment percentage-driven `fg` overrides. See [Thresholds](#thresholds). |
| `threshold_target` | `context`, `limit_5h`, `limit_7d` | `"all"` (default) wraps the whole segment in the threshold `fg`; `"pct"` wraps only the percentage digits. See [Thresholds](#thresholds). |

### Thresholds

For percentage-bearing segments (`context`, `limit_5h`, `limit_7d`), the
`thresholds` field lets the foreground color react to the current fill
state. Each entry is a `{min, fg}` pair; when the segment's percentage
metric reaches `min`, that entry's `fg` is a candidate override. The
highest-`min` matching entry wins. Entries with `fg: ""` are skipped (as
if absent), and segments without a percentage metric (`model`, `cost`,
`cwd`, …) ignore the field entirely.

```json
{"type": "context", "style": "bar+pct", "fg": "245",
 "thresholds": [
   {"min": 70, "fg": "136"},
   {"min": 90, "fg": "160"}
 ]}
```

Reading this configuration: below 70% used, the segment is rendered in
the static `fg` (`245`, neutral grey); from 70% it switches to `136`
(amber), and from 90% to `160` (red). The bar, percentage, and token
counts all wrap in the chosen color — thresholds drive the segment's
overall `fg`, not just the percentage number.

Threshold ordering inside the array does not matter; the renderer picks
by `min`, not by position. `bg` and `bold` are not part of the
threshold schema — pick those statically on the segment.

By default, when a threshold matches, its `fg` is applied to the
entire segment via the renderer's outer style wrap. That includes
the bar cells, the percentage digits, the token counts, and the
label — every glyph in the segment switches color together. For
narrow displays (single line, modest contrast) this is the desired
behaviour; for wider layouts the full-segment color shift can
overwhelm.

The optional `threshold_target` field scopes the override:

- `"all"` (default, omitted): the existing whole-segment behaviour.
- `"pct"`: only the percentage digits switch to the threshold `fg`.
  The bar cells, token counts, and label keep the segment's static
  `fg`.

```json
{"type": "context", "style": "bar+pct", "fg": "245",
 "threshold_target": "pct",
 "thresholds": [
   {"min": 70, "fg": "136"},
   {"min": 90, "fg": "160"}
 ]}
```

In this configuration the bar `[██░░░░░░░░░░░░░░]` stays in `245`
(grey) regardless of fill; only the `95%` flicks to `160` (red)
above the 90 threshold.

### `bar_width`

The `bar` and `bar+pct` styles draw a circle bar whose cells each have
five quarter-fill states (`○ ◔ ◑ ◕ ●`). `bar_width` sets the cell count
for a single segment; it applies to `context`, `limit_5h`, and
`limit_7d`. The default is `16`; zero or negative values fall back to
the default, so an omitted or nonsensical value never collapses the bar.

A wide `context` bar plus full-width `limit_5h` / `limit_7d` bars can
overflow narrow terminals — give the rate-limit buckets a compact width
while leaving `context` at the default:

```json
{"type": "context",  "style": "bar+pct"},
{"type": "limit_5h", "style": "bar+pct", "bar_width": 8},
{"type": "limit_7d", "style": "bar+pct", "bar_width": 8}
```

### Segment reference

#### `text`

Static literal. Returns `Label` verbatim. Useful as an inline separator or
prefix.

```json
{"type": "text", "label": "ccsb · "}
```

#### `model`

Returns `payload.model.display_name`. Strips any trailing ` (...)` clause
so `"Opus 4.7 (1M context)"` becomes `"Opus 4.7"`. With `show_1m_flag: true`
and `payload.exceeds_200k_tokens` set, appends `" 1M"` →
`"Opus 4.7 1M"`.

```json
{"type": "model", "show_1m_flag": true}
```

#### `effort`

Returns `<label>:<level>` from `payload.effort.level`. Default label is
`"effort"`. Hidden when level is empty.

```json
{"type": "effort"}                     // "effort: xhigh"
{"type": "effort", "label": "eff"}     // "eff: xhigh"
```

#### `session_name`

Returns `payload.session_name` verbatim. Hidden when empty.

```json
{"type": "session_name"}
```

#### `output_style`

Returns `<label>:<name>` from `payload.output_style.name`. Default label is
`"style"`. Hidden when name is empty.

```json
{"type": "output_style"}               // "style: concise"
```

#### `cwd`

Returns the working directory from `payload.workspace.current_dir`. Default
is the basename; `format: "full"` returns the absolute path. Hidden when
the field is empty.

```json
{"type": "cwd"}                        // "claude-cli-status-bar"
{"type": "cwd", "format": "full"}      // "/home/u/repos/claude-cli-status-bar"
```

#### `cost`

Returns `payload.cost.total_cost_usd` formatted via `format` (a Go
`fmt.Sprintf` verb). Default format is `"$%.2f"`. Always rendered — `$0.00`
at the start of a session is meaningful information.

```json
{"type": "cost"}                       // "$26.05"
{"type": "cost", "format": "€%.3f"}    // "€26.047"
```

#### `duration`

Compact `h/m/s` representation of `payload.cost.total_duration_ms`. Higher
zero units are dropped (`"2h10m"`, `"5m"`, `"12s"`). Hidden when the field
is zero or negative.

```json
{"type": "duration"}                   // "2h10m"
```

#### `lines`

Lines added / removed during the session. Format is `+<added> −<removed>`
(uses `U+2212 MINUS SIGN`, not ASCII `-`). Hidden when both counters are
zero.

```json
{"type": "lines"}                      // "+120 −45"
```

#### `context`

Context-window state from `payload.context_window`. Sixteen-cell unicode
block-element bar (`█` for filled cells, `░` for empty). The percent is
rounded to the nearest integer; the "consumed" number is
`total_input_tokens` (cumulative session input, not the current turn's
prompt size after caching).

| `style`     | Output                                  |
| ----------- | --------------------------------------- |
| `"bar+pct"` | `[████░░░░░░░░░░░░] 26% 264k/1M` (default) |
| `"bar"`     | `[████░░░░░░░░░░░░]`                    |
| `"pct"`     | `26%`                                   |

Token counts are compacted: `1234` → `"1k"`, `1_500_000` → `"1.5M"`. Hidden
when both `used_percentage` and `context_window_size` are zero. The bar
length defaults to 16 cells; override it with [`bar_width`](#bar_width).
Supports [`thresholds`](#thresholds) (whole-segment or
`threshold_target: "pct"` for just the percentage digits) to switch `fg`
based on `used_percentage`.

```json
{"type": "context", "style": "bar+pct"}
```

#### `limit_5h`, `limit_7d`

The five-hour and seven-day rate-limit buckets from
`payload.rate_limits.five_hour` and `…seven_day`. Default labels are
`"5h"` / `"7d"`. The countdown is computed against the renderer's wall
clock at the time of the call.

| `style`     | Output                                         |
| ----------- | ---------------------------------------------- |
| `""` / `"pct"` | `5h: 18% (2h15m)` (default)                 |
| `"bar"`     | `5h: [███░░░░░░░░░░░░░]`                        |
| `"bar+pct"` | `5h: [███░░░░░░░░░░░░░] 18% (2h15m)`            |

Countdown format mirrors `duration`: drop zero higher units, keep at most
two adjacent units (`"4d1h"`, `"2h15m"`, `"45m"`). Reaches `"now"` once the
reset time has passed. Percentages with fractional parts < 0.0005 render
as integers (`"100%"` not `"100.0%"`). Hidden when both `used_percentage`
and `resets_at` are zero (no data). The `bar` / `bar+pct` styles default
to a 16-cell bar; override it with [`bar_width`](#bar_width). Both
segments support [`thresholds`](#thresholds) (whole-segment or
`threshold_target: "pct"` for just the percentage digits) to switch `fg`
based on `used_percentage`.

```json
{"type": "limit_5h"}
{"type": "limit_7d", "style": "bar+pct"}
```

#### `mode`

Single glyph indicating the inference mode:

| Condition                  | Glyph        |
| -------------------------- | ------------ |
| `payload.thinking.enabled` | 🧠 (`U+1F9E0` BRAIN) |
| `payload.fast_mode`        | ⚡ (`U+26A1` HIGH VOLTAGE) |
| neither                    | empty (hidden) |

Thinking wins when both flags are set — it is the slower, more noteworthy
state.

```json
{"type": "mode"}
```

#### `git_branch`

Branch name read from `.git/HEAD`, walking up the directory tree from
`cwd` until a `.git` is found (depth-capped at 30 parents; no `git`
subprocess). A `.git` pointer file with a `gitdir: <path>` line — the
shape git emits for worktrees — is followed once; relative `gitdir:`
paths that resolve outside the current directory are rejected to prevent
escape. With a non-empty `label`, the output is prefixed:
`"<label>:<branch>"`. Hidden for detached HEAD, malformed HEAD, no repo,
or empty cwd.

```json
{"type": "git_branch"}                 // "main"
{"type": "git_branch", "label": "git"} // "git: main"
```

#### `tty_size`

Detected terminal columns × rows. Format supports `{cols}` and `{rows}`
placeholders; the default format is `"{cols}×{rows}"` using the Unicode
multiplication sign `U+00D7` (not ASCII `x`). Hidden when detection
fails — both dimensions zero — so the surrounding separator or
chevron is dropped naturally. With a non-empty `label`, the output is
prefixed `"<label>: "`.

Detection chain (first non-zero `cols` wins): the `width` config
field > `/dev/tty` ioctl > `/proc` parent-process walk. When ccsb
is invoked by Claude Code, `/dev/tty` is unreachable and the `/proc`
walk supplies the size. When ccsb is invoked directly from a shell,
`/dev/tty` satisfies the chain on the first try.

When the `width` config field is set, only `cols` is overridden —
`rows` stays `0` until `/dev/tty` or the `/proc` walk supplies a
row count. A custom format using `{rows}` therefore renders as
`"…×0"` while `width` is the active source.

```json
{"type": "tty_size"}                            // "128×37"
{"type": "tty_size", "format": "{cols}c"}       // "128c"
{"type": "tty_size", "label": "term"}           // "term: 128×37"
```

#### `version`

Emits the running ccsb version as `"v<x.y.z>"`. The version is
resolved at startup with the first non-empty source winning:
`-ldflags "-X .../cli.Version=…"` injected at build time, then
`runtime/debug.ReadBuildInfo().Main.Version` (set by `go install`
on a tagged module), otherwise the literal `"dev"`. A `"dev"`
result renders as `"☠ v dev"` (U+2620 SKULL AND CROSSBONES) to
flag untagged builds. Hidden when the resolution yields the empty
string, so the surrounding separator or chevron is dropped.

Typical placement is the last segment of the last row with
`"align": "right"` so the stamp sits flush right:

```json
{"type": "version", "fg": "245", "align": "right"}
```

#### `schema_health`

Visible-when-broken indicator for the inbound Claude Code JSON payload.
Emits a single skull glyph (`☠` U+2620) when the renderer detects a
schema issue, otherwise renders empty so the segment costs no palette
slot and no chevron — completely invisible in the happy path.

The detection is intentionally narrow to avoid false positives. It
fires when:

- the top-level JSON parse fails outright (not an object), or
- any per-field unmarshal returns a type error (a real schema
  regression — see *per-segment isolation* below), or
- one of the three critical fields ccsb always expects is empty
  (`session_id`, `model.display_name`, `workspace.current_dir`).

Optional fields like `rate_limits` or `cost` arriving empty during
the first updates of a session do NOT trigger the indicator — only
a type mismatch on those fields does.

The payload is parsed in **per-segment-isolated** mode (introduced in
0.2.17): the raw bytes are first decoded into a top-level
`map[string]json.RawMessage`, then each known key is unmarshalled
into its specific destination field individually. A type error in
one field stops at that field — the rest of the payload still
populates normally, and only the broken segment loses its data while
`schema_health` surfaces the issue. This is the mechanism that lets
e.g. a broken `cost` field hide just the cost segment while
`context_window` and `rate_limits` continue to render.

Default placement (in the built-in `defaultConfig`) is the right edge
of row 1 with a dark-red alarm block:

```json
{"type": "schema_health", "fg": "160", "bg": "52", "bold": true, "align": "right"}
```

Override the colours or alignment freely. Placing the segment in a user
config also enables the indicator there; omitting it disables the
indicator entirely.

To investigate **why** the indicator fired, run `ccsb doctor`: among
its other checks it diffs the most recent capture's top-level keys
against the set ccsb's renderer expects, printing any missing keys
(removed/renamed on Claude Code's side) and any additive keys (new
fields ccsb does not yet handle). The schema-check is purely
informational — ccsb cannot fix the upstream payload — but surfaces
the drift explicitly so an unexpected ☠ has a concrete root cause.

Whenever the indicator fires, ccsb also persists a `.diag` file next
to the matching capture (same basename as the `.json`/`.out`/`.err`
siblings) containing a stable plain-text dump of the detected issue:
the top-level parse error if any, missing critical fields, per-field
unmarshal errors, and the list of additive keys spotted on the side.
Healthy captures produce no `.diag` file, so the capture dir stays
uncluttered.

ccsb also remembers the last `schema_version` value the upstream
payload reported in `$XDG_STATE_HOME/ccsb/schema_version` (sibling
of the `captures/` directory, 0o600 user-private). A change in that
value triggers a `.diag` entry even when the payload is otherwise
healthy — useful to spot the moment Claude Code rolls out a payload
schema bump. The state file is initialised silently on first sight
of a `schema_version` value; payloads that omit the field do not
erase the stored value.

## Colors

Foreground and background use ANSI 256-color codes as decimal strings in
the range `"0"`–`"255"`. Invalid values are silently dropped (no escape
emitted) to prevent escape-sequence injection from a malicious config.

```json
{"type": "model", "fg": "39"}                    // bright cyan
{"type": "cost",  "fg": "220", "bold": true}     // bold yellow
{"type": "model", "fg": "15", "bg": "236"}       // white on dark grey
```

When `NO_COLOR` is set in the environment, the renderer suppresses all
escape emission (`no-color.org` convention) — segments still produce text,
but `fg`/`bg`/`bold` have no on-wire effect. Segments without any style
attribute set never emit escapes, including the trailing reset.

## Examples

### Default layout

Equivalent to omitting `render` entirely (`ccsb` fills these values
in from the built-in `defaultConfig` when `rows` is absent):

```json
{
  "render": {
    "powerline": true,
    "powerline_style": "solid",
    "cap_style": "round",
    "palette": ["232", "233", "234", "235", "236", "237", "238", "239", "240"],
    "palette_stride": 2,
    "rows": [
      {
        "caps": true,
        "segments": [
          {"type": "model", "fg": "33", "bold": true, "show_1m_flag": true},
          {"type": "mode"},
          {"type": "context", "fg": "245", "style": "bar+pct",
           "threshold_target": "pct",
           "thresholds": [{"min": 70, "fg": "136"}, {"min": 90, "fg": "160"}]},
          {"type": "limit_5h", "fg": "245", "style": "bar+pct", "bar_width": 8, "wrap": true,
           "threshold_target": "pct",
           "thresholds": [{"min": 70, "fg": "136"}, {"min": 90, "fg": "160"}]},
          {"type": "limit_7d", "fg": "245", "style": "bar+pct", "bar_width": 8, "wrap": true,
           "threshold_target": "pct",
           "thresholds": [{"min": 70, "fg": "136"}, {"min": 90, "fg": "160"}]},
          {"type": "schema_health", "fg": "160", "bg": "52", "bold": true, "align": "right"}
        ]
      },
      {
        "caps": true,
        "segments": [
          {"type": "git_branch", "fg": "33"},
          {"type": "lines", "fg": "245"},
          {"type": "cwd", "fg": "245", "shrink": true},
          {"type": "version", "fg": "245", "align": "right"}
        ]
      }
    ]
  }
}
```

Output (ANSI stripped, valid payload — `schema_health` is hidden):

```
  Opus 4.7 1M  🧠  ●●●◑○○○○○○○○○○○○ 22% 217k/1M  5h: ◔○○○○○○○ 5% · 4h25m  7d: ◔○○○○○○○ 5% · 6d2h
  main  +546 −107  claude-cli-status-bar                                                                          v0.2.16
```

If the inbound JSON payload from Claude Code looks broken (top-level
parse failure, or one of the critical fields `session_id`,
`model.display_name`, `workspace.current_dir` is empty), the
`schema_health` segment fires and a dark-red block with a bright-red
`☠` glyph appears at the right edge of row 1:

```
  …                                                                                                    ☠
```

### Single-line, compact

```json
{
  "render": {
    "separator": " · ",
    "rows": [
      [
        {"type": "model"},
        {"type": "context", "style": "pct"},
        {"type": "cost"},
        {"type": "git_branch"}
      ]
    ]
  }
}
```

Output:

```
Opus 4.7 · 26% · $26.05 · main
```

### Coloured emphasis

```json
{
  "render": {
    "rows": [
      [
        {"type": "model", "fg": "39", "bold": true},
        {"type": "mode"},
        {"type": "cost", "fg": "220"},
        {"type": "limit_5h"}
      ],
      [
        {"type": "git_branch", "fg": "108"},
        {"type": "cwd"}
      ]
    ]
  }
}
```

`mode` renders 🧠/⚡ when the corresponding flag is set and disappears
otherwise — handy for keeping the row width stable while still surfacing
thinking mode.

### Percentage thresholds

Context and rate-limit segments switch color as the metric crosses
breakpoints — neutral grey under 70%, amber at 70-90%, red above 90%:

```json
{
  "render": {
    "rows": [
      [
        {"type": "model", "fg": "33", "bold": true, "show_1m_flag": true},
        {"type": "context", "style": "bar+pct", "fg": "245",
         "thresholds": [{"min": 70, "fg": "136"}, {"min": 90, "fg": "160"}]}
      ],
      [
        {"type": "cost", "fg": "136", "bold": true},
        {"type": "limit_5h", "fg": "245",
         "thresholds": [{"min": 70, "fg": "136"}, {"min": 90, "fg": "160"}]},
        {"type": "limit_7d", "fg": "245",
         "thresholds": [{"min": 70, "fg": "136"}, {"min": 90, "fg": "160"}]}
      ],
      [
        {"type": "git_branch", "fg": "33"},
        {"type": "cwd", "fg": "245"}
      ]
    ]
  }
}
```

### Bar-only context, bar+pct limits

```json
{
  "render": {
    "rows": [
      [{"type": "model"}, {"type": "context", "style": "bar"}],
      [{"type": "limit_5h", "style": "bar+pct"}, {"type": "limit_7d", "style": "bar+pct"}],
      [{"type": "git_branch"}, {"type": "cwd", "format": "full"}]
    ]
  }
}
```

### Literal separators and labels

```json
{
  "render": {
    "rows": [
      [
        {"type": "text", "label": "› "},
        {"type": "session_name"},
        {"type": "text", "label": " // "},
        {"type": "effort", "label": "eff"}
      ]
    ]
  }
}
```
