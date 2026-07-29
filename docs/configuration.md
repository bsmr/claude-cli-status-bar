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
  "proxy":  { "command": "npx", "args": ["-y", "ccstatusline@latest"], "timeout": "10s" },
  "backup": { "previous_status_line": { /* … */ } },
  "render": { "rows": [ /* … */ ], "separator": " | " },
  "update": { "auto": "patch" }
}
```

| Key      | Purpose                                                                                |
| -------- | -------------------------------------------------------------------------------------- |
| `proxy`  | When `proxy.command` is non-empty, ccsb forwards the payload to that child process and prints its stdout. When empty (or the key is absent), the in-binary native renderer drives the status line. Toggle with `ccsb mode native` / `ccsb mode proxy`. `proxy.timeout` bounds the child — see [`proxy.timeout`](#proxytimeout). |
| `backup` | Holds the verbatim `statusLine` value that was in `~/.claude/settings.json` at install time, so `ccsb uninstall` can restore it. Never written by `ccsb mode`. |
| `render` | Native-renderer config. Ignored while proxy mode is active. |
| `update` | Opt-in self-update preferences. Absent (the default) means ccsb never installs an update by itself. See [`update`](#update). |

All four keys are optional. With every key omitted, ccsb runs the native
renderer with the default layout.

Any top-level key ccsb does not recognise is left alone. Since `0.4.21` it
survives every rewrite — `install`, `mode`, `doctor` and `update` all persist
the config as a side effect, and before that each of them silently dropped
what it could not model. The realistic case is version skew rather than
hand-editing: an older ccsb still on the system running `ccsb mode native`
over a config a newer one wrote used to delete the newer block outright.
One visible cost: a config carrying such a key comes back with its top-level
keys in alphabetical order, because the merge goes through a map. `ccsb
config reset` is the deliberate exception — it moves the whole file to a
`.bak` and starts fresh, so unrecognised keys stay in the backup and do not
come back.

## `render`

### Fields

| Field       | Type            | Default       | Effect                                                      |
| ----------- | --------------- | ------------- | ----------------------------------------------------------- |
| `rows`      | array of arrays | default rows  | Each inner array is one output line. Segments in a row are joined with `separator`. |
| `separator` | string          | `" \| "`      | Joiner between segments in the same row.                    |
| `powerline` | bool            | `false`       | When true, each row gets a coloured background fill (from `bg`, `palette`, or the built-in default palette) and segments are joined with a Powerline chevron. The glyph is selectable via `powerline_style`. See [Powerline](#powerline). |
| `width`     | int             | `0`           | Explicit terminal-cols override. When `> 0`, ccsb skips terminal-size detection and uses this value. Default `0` runs the detection chain (`/dev/tty` ioctl, then `COLUMNS`/`LINES`, then a `/proc` parent-process walk). |
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
before joining, so adjacent separators are never doubled. Under the
default layout the bar is never blank, not even for a broken payload
(`{}`, a global parse failure, a per-field type error): the layout's
[`schema_health`](#schema_health) segment renders the `☠` marker
whenever the detection fires, and when it does not fire the `model`
and `cwd` segments are guaranteed to carry data. A user-supplied `rows`
that intentionally renders empty stays empty — the guarantee covers the
default layout only.

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
(uniform fill) > the shared `render.palette`, which defaults to nine
monotonic dark→light greys (`232`–`240`) rotated per output row by
`palette_stride` and applied only when Powerline is enabled. If all
four are unset and Powerline is off, the segment has no background.

### Powerline

With `powerline: true`, the renderer switches to a Powerline-style
row layout:

- Each `Row.Bg` is opened at the start of the row and fills the
  line all the way to the terminal's right edge. The width is taken
  from `width` if set, otherwise from `/dev/tty + ioctl(TIOCGWINSZ)`,
  otherwise from the `COLUMNS` environment variable, otherwise from a
  `/proc` parent-process walk. When every source fails, the row falls
  back to natural width and the bg ends after the last segment.
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

## `update`

A top-level block, sibling to `render` and `proxy`:

```json
{ "update": { "auto": "patch" } }
```

`auto` is the largest version jump ccsb may install by itself:

| Value | Installs automatically |
|---|---|
| `patch` | `0.4.8` → `0.4.9` |
| `minor` | also `0.4.8` → `0.5.0` |
| `major` | also `0.4.8` → `1.0.0` and beyond |

Each value is a ceiling, not an exact match — `minor` also covers a patch
jump, `major` also covers a minor or patch jump.

Absent, empty, or any other **string** — including a different
capitalisation such as `"PATCH"` — disables automatic updating entirely;
`ccsb update` remains available by hand. `ccsb doctor` names a value it does
not recognise, since the renderer stays silent about it. A non-string value
(`{"update": {"auto": true}}`) is not "anything else" but a config parse
error, exactly like a wrongly typed value anywhere else in this file: the
config fails to load and the whole status bar goes blank.

The trigger lives in the `version` segment, so it only fires from a layout
that actually renders one with `"check_update": true` — and only when a
state directory is resolvable. The default layout satisfies both. A custom
`rows` block without such a segment (the kind the `ccsb-wizard` skill
produces) makes `update.auto` inert while it still reads as enabled.
Nothing in the rendered bar shows this, but `ccsb doctor` says so:

```text
ccsb: doctor: update.auto: "patch" never runs — no version segment sets
"check_update": true, and the update check only runs from there
```

Like the wizard-skill check it reports and does not repair — the layout is
yours — and it is not counted in doctor's `fixed N issue(s)` tally. The fix
is to add `"check_update": true` to a `version` segment in `rows`, or to
delete the `rows` block entirely and fall back to the default layout.

When enabled, the renderer starts a detached `ccsb update` at most once per
[`update_check_interval`](#version) (24h by default) — the same interval
that gates the version segment's own background release check — and only
when the pending jump is within the configured ceiling and nothing already
known would make the update fail here (Windows, a binary built from a
local checkout, or a target directory that a prior attempt found
unwritable). A failed attempt still resets that clock, so a persistently
failing update is retried at most once per interval, not on every render.

The clock is the timestamp in `$XDG_STATE_HOME/ccsb/update-attempt.json`,
which `ccsb update` stamps on every exit. A single-flight marker beside it
(`update-attempt.json.pending`) carries the same interval as its lifetime,
which covers the case the record cannot: an update killed before it returns
— a reboot, or quitting Claude Code during a from-source build — stamps
nothing, and without the marker would restart on the next render.

## Segments

Every segment is an object with at least a `type` field. The renderer
recognises 18 types.

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
| `max_width` | int | When positive, caps the visible column width of the segment's rendered body. Longer bodies are shortened to `max_width − 1` columns and suffixed with `…` (U+2026); shorter bodies pass through unchanged. Zero (the default) disables truncation. Truncation runs on the segment's **plain** body before the style wrap, so it is safe for text-only segments (`text`, `cwd`, `git_branch`, `model`, …). Segments that self-embed ANSI escapes for sub-styling (`context`, `limit_5h`, `limit_7d` when `threshold_target: "pct"`, or when any of `bar_fg` / `bar_thresholds` / `label_fg` / `label_thresholds` is set) should leave `max_width` at 0 — truncating across an embedded escape can leave the terminal in an unintended SGR state. |
| `min_cols` | int | When positive, suppresses the segment when the detected terminal width is strictly narrower than `min_cols`. The hidden segment behaves like an empty render: no body, no chevron, no palette slot. The gate runs before the segment function fires, so hidden segments cost nothing. When the terminal width is unknown (`ttyCols == 0`, e.g. no `/dev/tty`, no `COLUMNS`, no `/proc` walk hit, no `Config.Width` override) the gate is bypassed — "no info to gate on" defaults to "keep the segment". Zero (the default) disables the gate. Pairs naturally with `max_width` and `wrap` (both above) to build layouts that gracefully shed segments as the terminal narrows. |
| `shrink` | bool | When true, marks the segment as the one that yields display width when its row would overflow. After reflow, a pre-pass measures the row; if the visible content exceeds the usable width (`ttyCols - 2*margin` minus cap columns when active), every `shrink: true` segment has its effective `max_width` lowered — in row order, each yielding down to a 1-column floor (the `…` glyph) — until the overflow is absorbed. The cut reuses the `max_width` path, so `shrink` is the **dynamic** counterpart to that **static** cap and is safe on the same text-only segments (`text`, `cwd`, `git_branch`, `model`, …). A user-set `max_width` is only ever lowered further, never raised. A no-op when `ttyCols` is unknown or the row already fits. In the default layout `cwd` carries this flag so the right-aligned `version` stamp stays intact on narrow terminals. |

Per-segment additional fields are documented under each type below.

### Type-specific fields

| Field           | Used by                              | Notes                                                |
| --------------- | ------------------------------------ | ---------------------------------------------------- |
| `label`         | `text`, `effort`, `output_style`, `git_branch`, `git_dirty`, `limit_5h`, `limit_7d`, `tty_size` | Overrides each segment's default label prefix. |
| `format`        | `cwd`, `cost`, `git_dirty`, `tty_size` | Per-type format string or shape selector.          |
| `show_1m_flag`  | `model`                              | Appends `" 1M"` when the payload's `exceeds_200k_tokens` is true. |
| `style`         | `context`, `limit_5h`, `limit_7d`    | Selects between presentation variants.               |
| `token_position` | `context`                           | In the `bar+pct` style, places the `used/total` token fraction: `"after"` (default) trails the percentage, `"before"` leads the bar, `"hidden"` omits it. Empty or unknown falls back to `"after"`. |
| `bar_width`     | `context`, `limit_5h`, `limit_7d`    | Circle-bar length in cells. Default `16`; zero or negative falls back to the default. See [`bar_width`](#bar_width). |
| `bar_glyphs`    | `context`, `limit_5h`, `limit_7d`    | Overrides the fill ramp: an ordered list from empty (first) to full (last); each cell renders one of `len − 1` sub-steps. At least two entries; fewer falls back to the built-in ramp. Overrides `bar_style`. |
| `bar_style`     | `context`, `limit_5h`, `limit_7d`    | Built-in fill ramp: `"circles"` (default, `○◔◑◕●`) or `"blocks"` (`░▏▎▍▌▋▊▉█`). Unknown falls back to `"circles"`. Ignored when `bar_glyphs` is set. |
| `thresholds`    | `context`, `limit_5h`, `limit_7d`    | Per-segment percentage-driven `fg` overrides. See [Thresholds](#thresholds). |
| `threshold_target` | `context`, `limit_5h`, `limit_7d` | `"all"` (default) wraps the whole segment in the threshold `fg`; `"pct"` wraps only the percentage digits. See [Thresholds](#thresholds). |
| `bar_fg`        | `context`, `limit_5h`, `limit_7d`    | Foreground for the **bar glyphs only**, independent of the segment/number colour. `bar_thresholds` (reactive) overrides it. Lets a dimmed bar sit beside a bright, threshold-reactive number. |
| `bar_thresholds` | `context`, `limit_5h`, `limit_7d`   | Percentage-keyed palette for the bar (same shape as `thresholds`; highest matching `min` wins), overriding `bar_fg`. |
| `label_fg`      | `limit_5h`, `limit_7d`               | Foreground for the **label prefix only** (`5h:` / `7d:`), independent of the bar and number. `label_thresholds` is the reactive variant. |
| `label_thresholds` | `limit_5h`, `limit_7d`            | Percentage-keyed palette for the label prefix (same shape as `thresholds`). |
| `scope`         | `git_branch`                         | `"local"` (default) reports the nearest repository; `"toplevel"` reports the outermost superproject when `cwd` is inside a submodule. See [`git_branch`](#git_branch). |

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

In this configuration the bar `●●○○○○○○○○○○○○○○` stays in `245`
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
the field is empty. Non-printable characters are dropped, in both forms — a
directory name is filesystem content just like the branch name in
[`git_branch`](#git_branch), and extracting an archive is enough to put an
escape sequence into the path this segment prints.

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

Context-window state from `payload.context_window`. Sixteen-cell circle
bar — each cell has five quarter-fill states (`○◔◑◕●`, empty to full), so
the ramp is `bar_width × 4` steps. The fill glyphs are configurable via
[`bar_glyphs` / `bar_style`](#type-specific-fields) (e.g. `"blocks"` for a
`░▏▎▍▌▋▊▉█` ramp). The percent is rounded to the nearest integer; the
"consumed" number is `total_input_tokens` (cumulative session input, not
the current turn's prompt size after caching).

| `style`     | Output                              |
| ----------- | ----------------------------------- |
| `"bar+pct"` | `●●●●●●●●○○○○○○○○ 50% 100k/200k` (default) |
| `"bar"`     | `●●●●●●●●○○○○○○○○`                    |
| `"pct"`     | `50%`                               |

Token counts are compacted: `1234` → `"1k"`, `1_500_000` → `"1.5M"`. Hidden
when both `used_percentage` and `context_window_size` are zero. The bar
length defaults to 16 cells; override it with [`bar_width`](#bar_width).
Supports [`thresholds`](#thresholds) (whole-segment or
`threshold_target: "pct"` for just the percentage digits) to switch `fg`
based on `used_percentage`. In the `bar+pct` style, `token_position`
places the `used/total` fraction: `"after"` (default) trails the
percentage, `"before"` leads the bar, `"hidden"` omits it.

```json
{"type": "context", "style": "bar+pct"}
{"type": "context", "style": "bar+pct", "token_position": "before"}
```

#### `limit_5h`, `limit_7d`

The five-hour and seven-day rate-limit buckets from
`payload.rate_limits.five_hour` and `…seven_day`. Default labels are
`"5h"` / `"7d"`. The countdown is computed against the renderer's wall
clock at the time of the call.

| `style`     | Output                                         |
| ----------- | ---------------------------------------------- |
| `""` / `"pct"` | `5h: 18% · 2h15m` (default)                 |
| `"bar"`     | `5h: ●●●○○○○○○○○○○○○○`                          |
| `"bar+pct"` | `5h: ●●●○○○○○○○○○○○○○ 18% · 2h15m`              |

The countdown is joined with `" · "`, not wrapped in parentheses, and the
bar uses the same circle ramp as `context` (`○◔◑◕●`) — see
[`bar_glyphs` / `bar_style`](#bar_width) to switch it to blocks.

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
shape git emits for worktrees and submodules — is followed once. A
relative `gitdir:` path that escapes the working tree is honored only
when it lands in a `.git/modules/` tree (git's canonical submodule
layout, e.g. `../../.git/modules/<name>`); any other escape (e.g.
`../../../etc`) is rejected. With a non-empty `label`, the output is
prefixed: `"<label>: <branch>"`. Hidden for detached HEAD, malformed
HEAD, no repo, or empty cwd.

Non-printable characters in the ref name are dropped before rendering.
`.git/HEAD` is repository content, and a repository can reach you as an
archive: without this, a crafted HEAD emitted its escape sequences to the
terminal verbatim, and an embedded newline added a whole extra row to the
bar. The remaining text is still shown — `\e]8;;url\a` becomes the visible
`]8;;url` — so nothing disappears silently. Same treatment on
[`cwd`](#cwd), for the same reason.

The `scope` field selects which repository's branch to report when `cwd`
is inside a submodule working tree:

- `"local"` (the default, also `""` and any unknown value) — the nearest
  repository, i.e. the submodule itself.
- `"toplevel"` — the outermost superproject of the submodule chain. The
  top-level git dir is the prefix of the resolved gitdir up to the first
  `.git/modules` component; git flattens nested submodule git dirs there,
  so a submodule-within-a-submodule still resolves to the outermost repo.
  Worktree git dirs carry no `modules` component, so `"toplevel"` leaves a
  worktree's own branch untouched. Outside a submodule, `"toplevel"`
  equals `"local"`.

```json
{"type": "git_branch"}                       // "main" (submodule branch in a submodule)
{"type": "git_branch", "label": "git"}       // "git: main"
{"type": "git_branch", "scope": "toplevel"}  // superproject branch when cwd is in a submodule
```

To show both at once, place two `git_branch` segments with different
scopes — e.g. `{"scope": "local", "label": "sub"}` next to
`{"scope": "toplevel", "label": "top"}`.

#### `git_dirty`

Number of paths `git status --porcelain` reports as changed in the
repository containing `cwd` — modified, staged, deleted, renamed, and
untracked alike. Hidden for a clean tree, outside a repository, and until
the first count is available, so the segment and its separator drop out
entirely.

`format` supports the `{n}` placeholder and defaults to `"*{n}"`; a
non-empty `label` is prefixed as `"<label>: <body>"`.

```json
{"type": "git_dirty"}                       // "*3"
{"type": "git_dirty", "format": "±{n}"}     // "±3"
{"type": "git_dirty", "label": "dirty"}     // "dirty: *3"
```

**The count is refreshed out of band.** Unlike every other segment,
a dirty count cannot be read from a single file — it is a comparison of
the index against the working tree, which means running `git`. Rendering
is therefore never allowed to do it: the segment reads a cached number and
prints it immediately, and when that number is older than 3 seconds it
starts a detached `ccsb refresh-git-dirty <dir>` that updates the cache for
the *next* status update. Consequences worth knowing:

- The render path never runs `git` and never blocks: it pays only a cache
  read plus, at most, the fork of a tiny detached helper. A huge or slow
  repository can therefore never delay the status line — `git` runs only in
  the background refresher, never in a render.
- The number can lag by one status update, and the segment stays hidden
  until the first refresh has completed.
- The cache lives at `$XDG_STATE_HOME/ccsb/git-dirty/<hash>.json`, one
  entry per repository. Deleting it is harmless — the next render
  repopulates it.
- A single-flight marker (`<hash>.json.pending`) beside the cache keeps
  refreshes to one per repository at a time — however many render passes,
  consecutive updates, or parallel Claude sessions hit the same repo. A
  marker orphaned by a crashed refresher is reclaimed after a short timeout;
  that reclaim is best-effort, so a crash can briefly allow a second
  refresher — never a wrong count.
- `git` must be on `PATH` for the refresher. Without it the segment simply
  stays hidden.

This is one of two segments that start a subprocess — a detached helper,
never `git` in the render path itself — and only when a config asks for
it: it is absent from the default layout. (The other is `version`'s
`check_update`, below.)

#### `tty_size`

Detected terminal columns × rows. Format supports `{cols}` and `{rows}`
placeholders; the default format is `"{cols}×{rows}"` using the Unicode
multiplication sign `U+00D7` (not ASCII `x`). Hidden when detection
fails — both dimensions zero — so the surrounding separator or
chevron is dropped naturally. With a non-empty `label`, the output is
prefixed `"<label>: "`.

Detection chain (first non-zero `cols` wins): the `width` config
field > `/dev/tty` ioctl > the `COLUMNS`/`LINES` environment
variables > `/proc` parent-process walk. When ccsb is invoked by
Claude Code, `/dev/tty` is unreachable and `COLUMNS`/`LINES` supply
the size — Claude Code exports both and keeps them current for every
invocation. The `/proc` walk remains the fallback for hosts that
export neither. When ccsb is invoked directly from a shell,
`/dev/tty` satisfies the chain on the first try.

"First non-zero `cols`" is meant literally, and since `0.4.24` the code
matches it: a pty allocated without a size — `script(1)`, and any
`forkpty` caller that never issues `TIOCSWINSZ` — answers the ioctl
successfully with `0` columns. That is not a size, so the chain carries
on to `COLUMNS`/`LINES` and then to the `/proc` walk instead of
reporting "unknown".

On Windows the chain is `width` > `COLUMNS`/`LINES` > unknown; there
is no `/dev/tty` and no `/proc`.

When the `width` config field is set, only `cols` is overridden —
`rows` stays `0` until `/dev/tty`, `LINES`, or the `/proc` walk
supplies a row count. A custom format using `{rows}` therefore renders as
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
`runtime/debug.ReadBuildInfo().Main.Version`, otherwise the literal
`"dev"`. A `"dev"` result renders as `"☠ vdev"` (U+2620 SKULL AND
CROSSBONES) and skips the update check below entirely.

What `ReadBuildInfo` actually yields, measured rather than assumed:

| Build | Reported version |
| --- | --- |
| release archive (`-X` ldflag) | `0.4.15` |
| `go install …@v0.4.15`, or `go build` on a clean tagged checkout | `0.4.15` |
| `go build` at an **untagged** commit in the repo | `0.4.11-0.20260728052304-404e83d2fe2b` |
| any of the above with a dirty worktree | same, plus `+dirty` |
| source tarball, or `-buildvcs=false` | `dev` → `☠ vdev` |

So `dev` does **not** mean "untagged" — since Go 1.24 an untagged in-repo
build gets a VCS pseudo-version instead. This matters because a
pseudo-version and a `+dirty` suffix both fail `ParseSemver`, so the update
check below returns nothing at all: no indicator, no skull, no error. That is
the same failure that shipped in v0.4.7, which reported `0.4.7+dirty` and had
its update indicator dead for every release user.

The segment is hidden entirely when the resolution yields the empty string,
so the surrounding separator or chevron is dropped.

When `check_update` is true, the segment also checks
`github.com/bsmr/claude-cli-status-bar`'s Releases for a newer tag and,
if one exists, appends `" (<glyph> v<latest>)"`. `check_update`'s Go zero
value is `false` — `defaultConfig()` sets it `true` in ccsb's own shipped
layout, but a hand-written config that omits the field gets no background
check, so set it explicitly if you want this. `update_check_interval` (a
Go duration string, e.g. `"6h"`; default `"24h"`) bounds how often the
refresh below re-runs.

**The check is refreshed out of band**, the same way `git_dirty` is
refreshed (above): rendering never makes the network call itself — it
reads a cache and prints whatever it currently holds, and when that
cache is missing, stale, or the running version can't even be parsed as
semver, starts a detached `ccsb refresh-update-check` that updates the
cache for the *next* status update. A slow or unreachable GitHub can
therefore never delay the status line. A failed fetch (offline,
rate-limited) still stamps the cache with a fresh timestamp so it isn't
retried on every render, only once per `update_check_interval`.

- The cache lives at `$XDG_STATE_HOME/ccsb/update-check.json` — global,
  not per-repository, since there is exactly one ccsb release stream.
  Deleting it is harmless — the next render repopulates it.
- A single-flight marker (`update-check.json.pending`) beside the cache
  keeps refreshes to one at a time across however many render passes,
  consecutive updates, or parallel Claude sessions are running.

The glyph and color escalate with how far the latest release is ahead
of the running build:

| Gap | Glyph | Color field | Default |
|---|---|---|---|
| newer patch | `↑` | (segment's own `fg`) | — |
| newer minor | `↑` | `update_minor_fg` | `136` |
| newer major, one ahead | `↑` | `update_major_fg` | `208` |
| newer major, two+ ahead | `⚡` | `update_big_fg` | `160` |

**`⊘` (U+2298 CIRCLED DIVISION SLASH) replaces `↑`/`⚡` when a newer
release exists but `ccsb update` cannot apply it here.** The severity
color is kept — only the glyph changes, from "go get it" to "go get it
by hand". `ccsb update` refuses in three cases: on Windows (replacing a
running `.exe` is not supported), on a binary built from a local
checkout rather than installed from a tagged release, and when the
binary's directory is not writable.

`ccsb doctor` names which of the three applies. It runs the checks
itself, so the answer is available immediately — you never have to run
`ccsb update` first to find out why it would refuse.

The `⊘` glyph is slightly more reserved than `doctor`, because rendering
is a hot path:

- The Windows and local-build reasons show up in `⊘` immediately, on
  the very next render — the renderer decides both in-process (target
  OS, build metadata), the same way it decides `dev` above.
- The not-writable reason needs a filesystem probe, which rendering
  never performs. `⊘` therefore picks it up from the record `ccsb
  update` leaves behind, i.e. only after an update has actually been
  attempted. `ccsb doctor` reports it right away regardless.

Set `"check_update": false` to disable the network check entirely.

Typical placement is the last segment of the last row with
`"align": "right"` so the stamp sits flush right:

```json
{"type": "version", "fg": "245", "align": "right", "check_update": true,
 "update_minor_fg": "136", "update_major_fg": "208", "update_big_fg": "160"}
// "v0.4.6" or, with an update pending: "v0.4.6 (↑ v0.4.9)"
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

Nothing prunes the capture directory automatically. Every status update
writes one to four files (`.json` always, plus `.out`, `.err` and `.diag`
when non-empty) into
`${XDG_STATE_HOME:-$HOME/.local/state}/ccsb/captures/`, so it grows until
you clear it. Only `ccsb doctor` ever reads a capture back, and only the
newest one — removing the rest is safe at any time:

```bash
ccsb captures clean                     # remove all captures
ccsb captures clean --older-than 7d     # keep anything newer than 7 days
```

`--older-than` accepts a day count (`7d`, `90d`, up to `106751d`) or any Go
duration (`24h`, `90m`, `1h30m`). The age comes from the timestamp in each
filename, not from the filesystem, so touching a file does not make it look
fresh.

That timestamp is RFC3339Nano with `-` in place of `:`
(`2026-07-29T08-01-15.720561317Z`) — `:` cannot appear in an NTFS filename,
and until `0.4.20` that made every capture write fail on Windows without
saying so. Both spellings are parsed, so an existing capture directory stays
prunable across the upgrade.

Only names that *start* with such a timestamp are candidates; everything
else — `notes.txt`, a `README`, any subdirectory — is left alone. Note the
converse: a file you name with a leading timestamp is indistinguishable from
a capture and will be removed, so park your own files under a different name
or elsewhere. Symlinks are unlinked, never followed, so a link pointing out
of the directory cannot delete its target.

A run that hits an unremovable file stops, reports how many it removed
before stopping, and exits non-zero; re-running after fixing the cause
resumes where it left off.

ccsb also remembers the last `schema_version` value the upstream
payload reported in `$XDG_STATE_HOME/ccsb/schema_version` (sibling
of the `captures/` directory, 0o600 user-private). A change in that
value triggers a `.diag` entry even when the payload is otherwise
healthy — useful to spot the moment Claude Code rolls out a payload
schema bump. The state file is initialised silently on first sight
of a `schema_version` value; payloads that omit the field do not
erase the stored value.

## `proxy.timeout`

Bounds how long the proxy child may run, as a Go duration string:

```json
{"proxy": {"command": "npx", "args": ["-y", "ccstatusline@latest"], "timeout": "10s"}}
```

| Value | Effect |
| --- | --- |
| absent, empty, or unparsable | `10s` (the default) |
| a duration, e.g. `"3s"`, `"2m"` | that limit |
| `"0"` (or any negative value) | **no limit** — the pre-0.4.15 behaviour |

Why it exists: nothing in ccsb used to bound the child. The only context on
that path is the process-wide signal context, which never expires, so a proxy
that stalled — `npx` against a dead network is the realistic case — stalled
ccsb with it, on every status update, indefinitely. The default is deliberately
generous rather than tight: a cold `npx` run fetches from the registry before
printing anything, and the goal is to make a hang finite, not to police a slow
proxy.

On expiry the child is killed and ccsb exits non-zero, so Claude Code shows no
status bar for that update, with `proxy: <cmd>: timed out after 10s` on stderr.
That is the same outcome as any other proxy failure; it does **not** fall back
to the native renderer, because the proxy may already have written part of a
line and a second one would corrupt the output.

An unparsable value falls back to the default rather than failing the config:
losing the whole status bar over a typo in a timeout would be the worse
outcome. `"0"` is honoured exactly as written — it is an explicit opt-out, and
the only way back to unbounded behaviour for a proxy you know to be slow.

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
  main  +546 −107  claude-cli-status-bar                                                                          v0.4.4
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
