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
| `powerline` | bool            | `false`       | When true, each row's `bg` fills the terminal width and segments are joined with the U+E0B1 thin chevron. See [Powerline](#powerline). |
| `width`     | int             | `0`           | Explicit terminal-cols override. When `> 0`, ccsb skips terminal-size detection and uses this value. Default `0` runs the detection chain (`/dev/tty` ioctl, then `/proc` parent-process walk). |

When `rows` is omitted or empty, the renderer uses the built-in default:

```json
[
  [{"type": "model", "show_1m_flag": true}, {"type": "context", "style": "bar+pct"}],
  [{"type": "cost"}, {"type": "limit_5h"}, {"type": "limit_7d"}],
  [{"type": "git_branch"}, {"type": "cwd"}]
]
```

A row whose segments all render to empty strings is omitted from the output;
the next row moves up. Empty segments inside a row are dropped before
joining, so adjacent separators are never doubled.

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
- Segments are joined with the U+E0B1 thin chevron in a muted-grey
  foreground (`245`), with a single space on each side for breathing
  room. The chevron has no background of its own, so the row's
  background shows through the spaces and the glyph.
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

The chevron glyph and its foreground are hardcoded in 0.2.0; future
releases may expose them as config knobs.

## Segments

Every segment is an object with at least a `type` field. The renderer
recognises 15 types.

### Common fields

These fields are interpreted by the renderer wrapper, not by individual
segment functions, so they apply to every type:

| Field   | Type    | Notes                                                                       |
| ------- | ------- | --------------------------------------------------------------------------- |
| `type`  | string  | Required. Dispatch key. Unknown types render `?<type>?` so typos are visible. |
| `fg`    | string  | ANSI 256-color foreground as a decimal string `"0"`–`"255"`. Empty = no FG. |
| `bg`    | string  | ANSI 256-color background as a decimal string `"0"`–`"255"`. Empty = no BG. |
| `bold`  | bool    | When true, wraps the segment text in `ESC[1m … ESC[0m`.                     |

Per-segment additional fields are documented under each type below.

### Type-specific fields

| Field           | Used by                              | Notes                                                |
| --------------- | ------------------------------------ | ---------------------------------------------------- |
| `label`         | `text`, `effort`, `output_style`, `git_branch`, `limit_5h`, `limit_7d` | Overrides each segment's default label prefix. |
| `format`        | `cwd`, `cost`                        | Per-type format string or shape selector.            |
| `show_1m_flag`  | `model`                              | Appends `" 1M"` when the payload's `exceeds_200k_tokens` is true. |
| `style`         | `context`, `limit_5h`, `limit_7d`    | Selects between presentation variants.               |
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
when both `used_percentage` and `context_window_size` are zero. Supports
[`thresholds`](#thresholds) (whole-segment or `threshold_target: "pct"`
for just the percentage digits) to switch `fg` based on
`used_percentage`.

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
and `resets_at` are zero (no data). Both segments support
[`thresholds`](#thresholds) (whole-segment or `threshold_target: "pct"`
for just the percentage digits) to switch `fg` based on
`used_percentage`.

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

Equivalent to omitting `render` entirely:

```json
{
  "render": {
    "rows": [
      [{"type": "model", "show_1m_flag": true}, {"type": "context", "style": "bar+pct"}],
      [{"type": "cost"}, {"type": "limit_5h"}, {"type": "limit_7d"}],
      [{"type": "git_branch"}, {"type": "cwd"}]
    ]
  }
}
```

Output:

```
Opus 4.7 1M | [████░░░░░░░░░░░░] 26% 264k/1M
$26.05 | 5h: 18% (2h15m) | 7d: 65% (4d1h)
main | claude-cli-status-bar
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
