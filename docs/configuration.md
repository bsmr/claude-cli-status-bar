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

### Last-resort fallback

If the JSON payload from Claude Code is unparsable, ccsb emits a single
`<model> · <cwd>` line (or just one of the two, or the literal
`claude-cli-status-bar`) so the status bar is never blank. Configuration
does not affect this path.

## Segments

Every segment is an object with at least a `type` field. The renderer
recognises 14 types.

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
{"type": "effort"}                     // "effort:xhigh"
{"type": "effort", "label": "eff"}     // "eff:xhigh"
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
{"type": "output_style"}               // "style:concise"
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
when both `used_percentage` and `context_window_size` are zero.

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
| `""` / `"pct"` | `5h:18% (2h15m)` (default)                  |
| `"bar"`     | `5h:[███░░░░░░░░░░░░░]`                        |
| `"bar+pct"` | `5h:[███░░░░░░░░░░░░░] 18% (2h15m)`            |

Countdown format mirrors `duration`: drop zero higher units, keep at most
two adjacent units (`"4d1h"`, `"2h15m"`, `"45m"`). Reaches `"now"` once the
reset time has passed. Percentages with fractional parts < 0.0005 render
as integers (`"100%"` not `"100.0%"`). Hidden when both `used_percentage`
and `resets_at` are zero (no data).

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
{"type": "git_branch", "label": "git"} // "git:main"
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
$26.05 | 5h:18% (2h15m) | 7d:65% (4d1h)
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
