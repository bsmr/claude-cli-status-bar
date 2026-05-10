# Native Renderer Design — ccsb 0.1.x

Status: approved
Date: 2026-05-10
Owner: bsmr

## Context

ccsb today proxies the Claude Code statusLine payload through `npx -y
ccstatusline@latest` and renders a single-line `<model> · <dir>` fallback when
no proxy is configured. Captures collected with `cd1c66a` show that
ccstatusline ignores several useful payload fields (`cost.total_cost_usd`,
`rate_limits.seven_day.*`, `effort.level`, `session_name`,
`exceeds_200k_tokens`) and mislabels the 5-hour rate-limit bucket as
"Session". It also pulls in Node.js/npx on every status update.

The native renderer replaces the trivial fallback with a configurable Go
implementation that consumes the full payload, handles git branch lookup
without spawning a subprocess, and is meant to eventually let users drop the
`proxy` configuration entirely.

## Goals

- Render every payload field that materially helps the user (cost, both rate
  limits, context window, model with 1M flag, effort level, session name).
- Configurable layout: rows of segments with optional per-segment overrides
  (color, label, format string).
- No external runtime dependency: no `git` subprocess, no `npx`, no Node.
- Drop-in: when the user clears `proxy.command`, the native renderer takes
  over with a sensible default.

## Non-goals (v1)

- Dirty-state, ahead/behind, stash count, tags in the git segment.
- Themes / preset color schemes shipped in the box.
- Configurable ANSI attributes beyond `bold` (no italic/underline/dim/blink).
- Migration tooling — `Render` is `omitzero` so old configs continue to load.
- A subcommand to flip between proxy and native (user edits config directly).

## Architecture

### Package layout

```
internal/pkg/render/
├── render.go         // Render(opts, raw) (string, error) — public entry
├── render_test.go
├── segments.go       // map[string]segmentFunc + per-type renderers
├── segments_test.go
├── ansi.go           // ANSI 256-color helper, NO_COLOR-aware style()
├── ansi_test.go
├── git.go            // .git/HEAD parser, walk-up, worktree gitdir: support
└── git_test.go
```

Segments are plain functions registered in `map[string]segmentFunc`, not
behind an interface. The interface-based plugin architecture is deliberately
deferred until the segment count makes it worthwhile (8+).

### Public API

```go
package render

type Config struct {
    Rows      [][]Segment `json:"rows,omitzero"`
    Separator string      `json:"separator,omitempty"` // default " | "
}

type Segment struct {
    Type   string `json:"type"`             // "model", "cost", "context", ...
    Format string `json:"format,omitempty"` // optional per-segment format override
    Label  string `json:"label,omitempty"`  // optional prefix label
    FG     string `json:"fg,omitempty"`     // ANSI 256-color number, e.g. "131"
    BG     string `json:"bg,omitempty"`
    Bold   bool   `json:"bold,omitempty"`

    // Type-specific knobs (zero-valued when unused):
    Show1MFlag bool   `json:"show_1m_flag,omitempty"` // type=model
    Style      string `json:"style,omitempty"`        // type=context|limit_*: "bar+pct"|"pct"|"bar"
}

type Options struct {
    Config  Config
    Cwd     string // for git_branch; usually payload.workspace.current_dir
    NoColor bool   // resolved outside the package
}

func Render(opts Options, raw []byte) (string, error)
```

`Render` parses `raw` once into a private payload struct, walks
`opts.Config.Rows`, dispatches each segment to its renderer, joins segments
with `Separator` (default `" | "`) and rows with `\n`. Empty `Config.Rows`
triggers the built-in default layout below.

The "Beides" config rule from brainstorming: `Format` overrides the
type-specific default formatting; absent `Format` means use the segment's
default. `Label` overrides the segment's default label prefix.

### Built-in default layout

```go
var defaultRows = [][]Segment{
    {{Type: "model", Show1MFlag: true}, {Type: "context", Style: "bar+pct"}},
    {{Type: "cost"}, {Type: "limit_5h"}, {Type: "limit_7d"}},
    {{Type: "git_branch"}, {Type: "cwd"}},
}
```

## Segment types (v1)

| Type           | Source                                                              | Default output           |
| -------------- | ------------------------------------------------------------------- | ------------------------ |
| `model`        | `model.display_name`, `exceeds_200k_tokens`                         | `Opus 4.7 1M`            |
| `cost`         | `cost.total_cost_usd`                                               | `$17.70`                 |
| `duration`     | `cost.total_duration_ms`                                            | `2h10m`                  |
| `lines`        | `cost.total_lines_added/removed`                                    | `+2813 −136`             |
| `context`      | `context_window.{used_percentage, current_usage*, context_window_size}` | `[████░░░░░░░░] 27% 273k/1M` |
| `limit_5h`     | `rate_limits.five_hour.{used_percentage, resets_at}`                | `5h:7% (4h23m)`          |
| `limit_7d`     | `rate_limits.seven_day.{used_percentage, resets_at}`                | `7d:30% (5d2h)`          |
| `effort`       | `effort.level`                                                      | `effort:xhigh`           |
| `session_name` | `session_name`                                                      | `Skeleton implementieren`|
| `output_style` | `output_style.name`                                                 | `style:default`          |
| `mode`         | `thinking.enabled`, `fast_mode`                                     | `🧠` / `⚡` / empty       |
| `git_branch`   | walk-up from `cwd` into `.git/HEAD`                                 | `main`                   |
| `cwd`          | `workspace.current_dir`                                             | `claude-cli-status-bar`  |
| `text`         | (uses `Label`)                                                      | `<Label>`                |

Excluded from v1: `transcript_path`, `version`, `workspace.added_dirs`.

`context.style`:
- `bar+pct` — `[████░░░░░░░░] 27% 273k/1M`
- `pct` — `27%`
- `bar` — `[████░░░░░░░░]`

`limit_5h`/`limit_7d.style` accepts the same values; `bar+pct` includes the
reset countdown, `pct` is just the percent + countdown, `bar` is the bar
alone.

## Color, ANSI, NO_COLOR

Colors are ANSI 256-color codes (matches ccstatusline, broadly supported).
The `Segment.FG`/`BG` fields hold the numeric code as a string. `Bold` is the
only attribute besides color that's configurable in v1.

`ansi.go` exposes:

```go
const reset = "\x1b[0m"
func fg256(n string) string  // "" if n is empty or not 0-255
func bg256(n string) string
func style(s, fg, bg string, bold, colorEnabled bool) string
```

`fg256`/`bg256` validate `n` against `^[0-9]{1,3}$` and the 0-255 range,
returning `""` otherwise — this prevents injection of arbitrary ANSI
sequences via the config file.

`Options.NoColor` is the single source of truth inside Render. The CLI layer
sets it via `os.Getenv("NO_COLOR") != ""` (per https://no-color.org). When
true, `style` skips all escape sequences and returns `s` verbatim.

The default layout ships with **no colors set** — segments render plain. The
user opts in by adding `fg`/`bg`/`bold` per segment. Rationale: ccsb has no
way to know the user's terminal background, and a wrong default looks worse
than no default.

## Git state

```go
package render

// branch returns the current branch name, walking up from start until a .git
// is found or the filesystem root is reached. Returns "" for: not in a repo,
// detached HEAD, malformed HEAD, I/O error.
func branch(start string) string
```

Resolution order:

1. **Walk-up** from `start` through parent directories until `.git` is found
   or root is reached. Maximum 30 ascents (cycle/symlink guard).
2. If `.git` is a directory: read `<gitDir>/HEAD`.
3. If `.git` is a file: parse `gitdir: <path>` (relative to the `.git`
   file's parent), then read `<resolved>/HEAD`. This handles worktrees and
   submodules.
4. Parse `HEAD`: if it starts with `ref: refs/heads/`, return the trailing
   branch name. Anything else (a 40-hex-char SHA = detached) returns `""`.

The `git_branch` segment renders `""` when `start` is empty (no
`workspace.current_dir` in the payload).

Performance target: a single `Stat` plus a single short `ReadFile`,
< 200 µs typical.

## Integration

### config package

Extend `config.Config`:

```go
type Config struct {
    Proxy  Proxy         `json:"proxy,omitzero"`
    Backup Backup        `json:"backup,omitzero"`
    Render render.Config `json:"render,omitzero"`
}
```

`config` imports `render` for the type. `render` does not import `config` —
no cycle. `omitzero` means existing config files without a `render` section
load unchanged with `Config.Render` empty.

### statusline package

Extend `statusline.Options`:

```go
type Options struct {
    ProxyCommand string
    ProxyArgs    []string
    CaptureDir   string
    Render       render.Config
    NoColor      bool
}
```

`statusline.Run` behaviour:

1. Read stdin once into memory and capture (unchanged).
2. If `ProxyCommand != ""`: spawn proxy, tee stdout/stderr to .out/.err
   (unchanged).
3. Else: call
   ```go
   render.Render(render.Options{
       Config:  opts.Render,
       Cwd:     parsedPayload.Workspace.CurrentDir,
       NoColor: opts.NoColor,
   }, raw)
   ```
   write the result through the same tee chain so .out is also captured for
   the native path.

The current `<model> · <dir>` fallback is removed; if `Render.Rows` is empty,
the render package supplies its built-in default.

### cli package

Add a `Flags` struct alongside `Paths` so env-derived booleans don't bloat
`Paths`:

```go
type Flags struct {
    NoColor bool
}

func Run(ctx context.Context, p Paths, f Flags, args []string,
    stdin io.Reader, stdout, stderr io.Writer) error
```

`main.go` resolves `NoColor` from `os.Getenv("NO_COLOR") != ""` and passes
both structs through.

`install`/`uninstall`/`status`/`help` ignore `cfg.Render` in v1.

## Error handling

- **Per-segment recovery**: a segment that fails to render (parse error, bad
  field, malformed `Format`) returns `?` (or empty for hidden segments). The
  rest of the row continues. A typo'd `type` renders `?<type>?` so it's
  immediately visible.
- **Global parse failure**: if the payload cannot be JSON-parsed at all,
  Render falls back to a hardcoded `<model> · <cwd>` single-line so Claude
  Code never gets an empty statusLine.
- The `error` return is reserved for non-recoverable I/O — currently never
  returned, since Render works entirely in memory.

## Testing

| Layer       | What                                                                 |
| ----------- | -------------------------------------------------------------------- |
| Per segment | Table-driven tests on payload snapshots, `format`/`label`/`fg`/`bg`. |
| ansi        | Reset, combined FG+BG+Bold, NO_COLOR path, invalid color rejection.  |
| git         | Repo dir, `.git`-file worktree, detached HEAD, no repo, walk-up,     |
|             | symlink-loop guard.                                                  |
| Render      | Golden tests against anonymised real captures in `testdata/payloads/`,|
|             | expected output in `testdata/golden/<name>.txt`. Update flag.        |
| statusline  | Extend existing tests: `Options.Render` with rows, with/without      |
|             | capture, multi-line output assertions.                               |
| benchmark   | `BenchmarkRender_Default` — informational target < 1 ms.             |

Anonymisation: replace user paths with `/repo/path`, replace transcript_path
with `/transcript`, round token counts to nearest 1k. The five payload
scenarios committed as fixtures:

1. low-cost early session
2. high-cost session with `exceeds_200k_tokens=true`
3. near 5h limit (`five_hour.used_percentage > 90`)
4. 5h-limit reset (`used_percentage` jumped to 0)
5. detached HEAD (no git_branch)

## Out of scope (explicit follow-ups)

- Dirty-state in git segment (index parsing or `git status --porcelain`).
- Theme presets shipped in-tree.
- ANSI true-color (24-bit) support.
- A `ccsb preview` subcommand to render a sample payload offline.
- Localisation (German labels for v1 stay English: `5h`, `7d`, `cost`).
