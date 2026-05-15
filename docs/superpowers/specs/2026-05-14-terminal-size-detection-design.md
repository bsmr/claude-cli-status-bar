# 0.2.2 — Terminal-Size Detection

## Summary

Auto-detect the terminal column and row count at the start of every
render, with a manual `Config.Width` escape hatch as the highest-
priority override. Surface the detected size through a new
diagnostic segment type `tty_size`. Powerline's full-width bg fill
becomes functional under Claude Code spawn, which the 0.2.0 design
assumed but the actual deployment did not deliver.

This is a self-contained feat patch in the 0.2.x stream. It does not
touch the chevron spacing (shipped in 0.2.1), and it stops short of
right-align, truncation, and `min_cols` segment suppression — those
ship as separate concepts in 0.2.3+.

## Motivation

The 0.2.0 Powerline renderer reserves a full-width bg fill via a
`/dev/tty + ioctl(TIOCGWINSZ)` probe in `internal/pkg/render/tty.go`.
The 2026-05-13 re-probe established that `/dev/tty` is not reachable
from a Claude-Code-spawned subprocess (`ENXIO: no such device or
address`), so `readTTYCols()` returns 0 in production and the renderer
falls back to natural width. The visible row bg stops at the last
chevron instead of extending to the terminal's right edge.

The 2026-05-14 process-tree probe established that walking the parent
PID chain via `/proc/<pid>/stat` reaches the `claude` process at
depth 2 (`bash → claude → bash → go-process`), and that the
`claude` process has a usable controlling TTY (`tty_nr=34818`,
`cols=128 rows=37`). Opening `/proc/<claude-pid>/fd/0` and running
`TIOCGWINSZ` against that file descriptor returns the correct size.
The proc-walk strategy therefore closes the 0.2.0 detection gap.

The user also wants a way to set the width explicitly (so the
detection chain is debuggable and overridable when /proc is
unavailable) and a way to display the detected size for diagnostics.

## Scope

In:

- New `Config.Width int` field. Default `0` means "rely on
  detection". A non-zero value short-circuits detection and is used
  as `ttyCols` directly.
- New `discoverTermSize(cfg Config) (cols, rows int)` orchestrator
  in `internal/pkg/render/tty.go`. Detection chain:
  1. `cfg.Width` if `> 0` → `(cfg.Width, 0)`.
  2. `/dev/tty` ioctl `TIOCGWINSZ` (preserved from 0.2.0 for direct-
     CLI invocation paths).
  3. `/proc` walk: starting at the current process's PPID, follow
     PPIDs upward (capped at 16 levels, stop at PID 1). For each
     ancestor with `tty_nr != 0` in its `/proc/<pid>/stat` line,
     open `/proc/<pid>/fd/0` and run `TIOCGWINSZ`. First success
     wins.
  4. Fall through → `(0, 0)`.
- New pure helpers in `tty.go`:
  - `parseProcStat(content []byte) (ppid int, ttyNr int, err error)`
    handles `comm` fields with embedded spaces and parens by splitting
    on the **last** `)` in the line.
  - `walkProcForTTY(pid int, maxDepth int, statReader, sizeReader)
    (cols, rows int)` is the testable orchestration logic with the
    file-system layer injected.
- New segment type `tty_size` rendered by `renderTTYSize` in
  `internal/pkg/render/segments.go`.
- `renderEnv.ttyRows int` added next to the existing
  `renderEnv.ttyCols`; both are populated unconditionally at the
  start of every `Render` call.
- Documentation refresh in `docs/configuration.md`: new `width`
  field in the schema table, new `tty_size` entry in the segment
  vocabulary section.

Out:

- Right-align, `max_width` truncation, `min_cols` segment
  suppression — deferred to 0.2.3+ as separate concepts (see memory
  note `02x_terminal_aware_layout`).
- Any change to the chevron rendering shipped in 0.2.1.
- Stderr logging, warnings, debug output. Detection always either
  returns a size or `(0, 0)`. Silence is by design (Claude Code
  suppresses stderr anyway).
- A Windows / macOS port. `/proc`-walk is Linux-only by design; the
  project is already Linux-only.

## Design

### Schema

`render.Config` gains one new field:

```go
type Config struct {
    Rows      []Row  `json:"rows,omitzero"`
    Separator string `json:"separator,omitempty"`
    Powerline bool   `json:"powerline,omitempty"`
    Width     int    `json:"width,omitempty"` // NEW: explicit terminal-cols override
}
```

`Width` is JSON-`omitempty`, so existing configs without the field
still unmarshal cleanly and behave identically to today.

`render.Segment` is unchanged structurally. The new `Type: "tty_size"`
value uses the existing `Format` and `Label` fields. No new segment-
specific knobs.

### Detection orchestration

```go
// discoverTermSize returns the detected terminal cols×rows. Detection
// order, first non-zero cols wins: Config.Width > /dev/tty > /proc
// walk from PPID upward > (0, 0). The rows component may be 0 even
// when cols is non-zero (the Config.Width branch only sets cols).
func discoverTermSize(cfg Config) (cols, rows int) {
    if cfg.Width > 0 {
        return cfg.Width, 0
    }
    if c, r, ok := devTTYWinsizeReader(); ok {
        return c, r
    }
    return walkProcForTTY(os.Getppid(), procWalkDepth, procStatReader, procFDWinsizeReader)
}

const procWalkDepth = 16
```

The three file-system-touching helpers are package-level function-
pointer variables so tests can swap them. They are declared with the
same indirection pattern as the existing `nowFunc`:

```go
var procStatReader = func(pid int) ([]byte, error) {
    return os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
}

var procFDWinsizeReader = func(path string) (cols, rows int, err error) {
    f, err := os.Open(path)
    if err != nil {
        return 0, 0, err
    }
    defer f.Close()
    ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
    if err != nil {
        return 0, 0, err
    }
    return int(ws.Col), int(ws.Row), nil
}

var devTTYWinsizeReader = func() (cols, rows int, ok bool) {
    f, err := os.Open("/dev/tty")
    if err != nil {
        return 0, 0, false
    }
    defer f.Close()
    ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
    if err != nil {
        return 0, 0, false
    }
    return int(ws.Col), int(ws.Row), true
}
```

`devTTYWinsizeReader` replaces the 0.2.0 `readTTYCols()` /
`ttyColsFunc` pair. The 0.2.0 `tty.go` file is rewritten end-to-end
in this patch — none of its current contents survive verbatim
because both the API shape (one return → three returns including
rows and ok) and the indirection target name change.

### Pure helpers

`parseProcStat` returns `(ppid, ttyNr, err)`. Implementation sketch:

```go
func parseProcStat(content []byte) (ppid int, ttyNr int, err error) {
    last := bytes.LastIndexByte(content, ')')
    if last < 0 {
        return 0, 0, errors.New("parseProcStat: no closing paren")
    }
    fields := bytes.Fields(content[last+1:])
    if len(fields) < 5 {
        return 0, 0, fmt.Errorf("parseProcStat: only %d fields after comm", len(fields))
    }
    // fields after ')' are: state ppid pgrp session tty_nr ...
    ppid, err = strconv.Atoi(string(fields[1]))
    if err != nil {
        return 0, 0, fmt.Errorf("parseProcStat: ppid: %w", err)
    }
    ttyNr, err = strconv.Atoi(string(fields[4]))
    if err != nil {
        return 0, 0, fmt.Errorf("parseProcStat: tty_nr: %w", err)
    }
    return ppid, ttyNr, nil
}
```

The `LastIndexByte(content, ')')` split handles `comm` fields that
themselves contain `)` (e.g. `(foo(bar)baz)`) — the kernel guarantees
the `comm` is the parenthesised field whose closing `)` is followed
by exactly one space and the state character.

`walkProcForTTY` walks the chain:

```go
func walkProcForTTY(
    pid int,
    maxDepth int,
    statReader func(pid int) ([]byte, error),
    sizeReader func(path string) (cols, rows int, err error),
) (cols, rows int) {
    for depth := 0; depth < maxDepth && pid > 1; depth++ {
        content, err := statReader(pid)
        if err != nil {
            return 0, 0
        }
        ppid, ttyNr, err := parseProcStat(content)
        if err != nil {
            return 0, 0
        }
        if ttyNr != 0 {
            if c, r, err := sizeReader(fmt.Sprintf("/proc/%d/fd/0", pid)); err == nil {
                return c, r
            }
        }
        pid = ppid
    }
    return 0, 0
}
```

Stop conditions: depth cap, PID == 1, any `statReader` failure, any
`parseProcStat` failure. A `sizeReader` failure for one ancestor does
**not** stop the walk — the next ancestor still gets a chance.

### `renderEnv.ttyRows`

Field added next to `ttyCols`:

```go
type renderEnv struct {
    cwd          string
    colorEnabled bool
    nowUnix      int64
    ttyCols      int
    ttyRows      int // NEW
}
```

`Render` populates both fields unconditionally at the start of each
call:

```go
env := renderEnv{
    cwd:          cwd,
    colorEnabled: !opts.NoColor,
    nowUnix:      nowFunc().Unix(),
}
env.ttyCols, env.ttyRows = discoverTermSize(opts.Config)
```

The 0.2.0 guard "only call detection when Powerline is on" is
dropped. Detection costs at most a /dev/tty open (fast ENXIO) plus
up to 16 `/proc/<pid>/stat` reads (each is a single small file
read). Total budget is well under 1 ms on any modern Linux. This
keeps the data flow uniform and lets the `tty_size` segment work in
natural mode too.

### `renderTTYSize` segment

```go
// renderTTYSize formats env.ttyCols × env.ttyRows. Returns "" when
// detection failed (both dimensions 0), so the empty-segment-drop
// path in both renderers omits the segment plus its surrounding
// separator / chevron. seg.Format supports the placeholders {cols}
// and {rows}; default is "{cols}×{rows}" (U+00D7 multiplication
// sign, not ASCII 'x'). seg.Label, when set, prefixes "label: ".
func renderTTYSize(_ *payload, s Segment, env renderEnv) string {
    if env.ttyCols == 0 && env.ttyRows == 0 {
        return ""
    }
    format := s.Format
    if format == "" {
        format = "{cols}×{rows}"
    }
    out := strings.ReplaceAll(format, "{cols}", strconv.Itoa(env.ttyCols))
    out = strings.ReplaceAll(out, "{rows}", strconv.Itoa(env.ttyRows))
    if s.Label != "" {
        out = s.Label + ": " + out
    }
    return out
}
```

Dispatch registration in `renderSegment` (one new case) makes
`Type: "tty_size"` route to `renderTTYSize`.

### Detection-chain priority rationale

`Config.Width` is highest because it is the only branch the user can
control. If detection picks a wrong value, the user must be able to
override it without recompiling. The /dev/tty branch is next because
it is the cheapest and works for direct-CLI invocation (no /proc
walk needed). The /proc walk is the slowest path but the only one
that works under Claude Code spawn. Final `(0, 0)` keeps Powerline's
existing graceful fallback.

### Behaviour invariants preserved

- 0.1.x empty-segment drop: `tty_size` returns `""` when detection
  failed, so the surrounding separator / chevron is also dropped.
- 0.2.0 Powerline pad: still skipped when `env.ttyCols == 0`.
- 0.2.1 chevron spacing: unchanged.
- Existing tests: every test that constructs `renderEnv{...}` keeps
  the same form — the new `ttyRows` field is zero-valued by default.

## Tests

Five test categories. All tests live in `internal/pkg/render/`
in-package.

### `parseProcStat` (table-driven)

| Case | Input | Want |
|---|---|---|
| simple | `51437 (claude) S 50912 51437 50912 34818 …` | ppid=50912, ttyNr=34818, err=nil |
| comm with embedded colon | `50911 (tmux: server) S 1 50911 50911 0 …` | ppid=1, ttyNr=0, err=nil |
| comm with embedded parens | `99 (foo(bar)baz) S 1 99 99 0 …` | ppid=1, ttyNr=0, err=nil |
| empty input | `` | err non-nil |
| no closing paren | `51437 (claude S 50912 …` | err non-nil |
| fewer than 5 fields after comm | `51437 (claude) S 50912` | err non-nil |
| non-numeric ppid | `51437 (claude) S X 51437 …` | err non-nil |
| non-numeric tty_nr | `51437 (claude) S 1 51437 50912 X …` | err non-nil |

### `walkProcForTTY` (injection-based)

| Test | statReader stub | sizeReader stub | Want |
|---|---|---|---|
| immediate hit | depth 0 → tty_nr=42 | matches path → (128, 37, nil) | 128, 37 |
| depth-2 hit | depths 0,1 → tty_nr=0, depth 2 → tty_nr=42 | matches only depth 2 | 128, 37 |
| stops at PID 1 | depth 0 → tty_nr=0, ppid=1 | never called | 0, 0 |
| depth cap | always tty_nr=0, ppid+=10 each step | never called | 0, 0; readers seen ≤ 16 |
| sizeReader fails, walk continues | depth 0 tty_nr=42 size err; depth 1 tty_nr=42 size ok | both called | 128, 37 |
| /proc unavailable | readers return ENOENT | never called | 0, 0 |
| statReader error mid-walk | depth 0 tty_nr=0 → next; depth 1 err | depth-1 sizeReader never called | 0, 0 |

### `discoverTermSize` (orchestration)

Three tests via the function-pointer indirection:

1. `cfg.Width = 128` → returns `(128, 0)`; neither
   `procStatReader` nor `procFDWinsizeReader` is called (stubs set
   to panic if invoked).
2. `cfg.Width = 0`, `/dev/tty` stub returns `(0, 0, false)`, proc
   walk stubs land on depth-2 hit → returns `(128, 37)`.
3. All three stubs fail/return zero → returns `(0, 0)`.

The /dev/tty branch's indirection (`devTTYWinsizeReader`) is part of
the production code defined above — tests just swap the var.

### `renderTTYSize` (segment)

| Test | env | seg | want |
|---|---|---|---|
| default format | ttyCols=128, ttyRows=37 | `{Type:"tty_size"}` | `"128×37"` |
| cols-only format | ttyCols=128, ttyRows=37 | `{Type:"tty_size", Format:"{cols}c"}` | `"128c"` |
| label prefix | ttyCols=128, ttyRows=37 | `{Type:"tty_size", Label:"term"}` | `"term: 128×37"` |
| hidden when both 0 | ttyCols=0, ttyRows=0 | `{Type:"tty_size"}` | `""` |
| label honours hide | ttyCols=0, ttyRows=0 | `{Type:"tty_size", Label:"term"}` | `""` |
| rows-zero is not hidden | ttyCols=128, ttyRows=0 | `{Type:"tty_size"}` | `"128×0"` |

### Integration: `Render` populates ttyRows

One test that calls `Render` with stubbed detection (the function-
pointer vars from above) and confirms `env.ttyCols` / `env.ttyRows`
propagate to a `tty_size` segment. The simplest form: a row with a
single `tty_size` segment, stubs that return `(128, 37)`, expect
output to contain `"128×37"`.

### Coverage target

Render-package coverage must stay ≥ 94.3% (current baseline). The
unfakeable bodies — the production `readDevTTYWinsize`, the
production `procStatReader` / `procFDWinsizeReader` defaults — stay
best-effort and unreachable in tests, identical to the existing
treatment of `readTTYCols`.

## Documentation

`docs/configuration.md` changes:

1. Schema table (the one that currently lists `rows`, `separator`,
   `powerline`): append a row for `width` with type `int`, default
   `0`, description "Explicit terminal-column count. When > 0,
   overrides auto-detection; when 0, ccsb walks /proc to find the
   terminal size.".
2. Segment vocabulary section: add a 15th entry `tty_size`, document
   the two placeholders `{cols}` and `{rows}`, mention the default
   format `"{cols}×{rows}"`, and call out the hide-on-zero behaviour.

No other docs change.

## Rollout

Per project memory `release_workflow_gates`:

1. Implement on `development-0.2.2-work` with TDD commits.
2. Squash → `development-0.2.2-main`.
3. No-ff merge → `main`.
4. Tag `production-0.2.2` from `main`; push origin.

Three explicit user confirmations between phases — do not batch.

## Risks

- **PPID inheritance pre-walk**: starting at PPID skips the running
  process. The probe established the running `go` process has
  `tty_nr=0`, so starting at PPID is correct in the Claude Code
  case. For direct-CLI invocation, the running process's PPID is
  the shell, which itself has the TTY — also correct.
- **Race between stat read and fd open**: the ancestor process
  could exit between `statReader` and `sizeReader`. The walk
  handles this gracefully (`sizeReader` errors → continue walking).
- **Long `comm` fields with embedded `)`**: `LastIndexByte` handles
  this. The kernel's `proc/<pid>/stat` formatter never escapes
  parens inside `comm`, which is why we cannot use a naive
  `Index(content, ')')` — that would misparse `(foo(bar)baz)`.
- **`renderEnv.ttyRows` zero-value compatibility**: every existing
  test that constructs `renderEnv{...}` will now have an implicit
  `ttyRows: 0`. This matches what those tests previously did (the
  field didn't exist) and the new field's zero value means
  "unknown rows", which `renderTTYSize` treats as hidden when paired
  with `ttyCols=0`.
- **Width-override with 0 rows**: `tty_size` rendering shows
  `"128×0"` when Width is set but rows are unknown. This is honest
  (we don't know rows) but visually odd. Out-of-scope to "fix"
  (the user can supply `Format: "{cols}c"` if rows are irrelevant).

## Open Questions

None.

## References

- Project memory: `release_workflow_gates`,
  `release_sizing_one_concept`, `02x_terminal_aware_layout`.
- Prior spec: `docs/superpowers/specs/2026-05-13-powerline-design.md`
  (0.2.0 baseline this work extends).
- Prior spec: `docs/superpowers/specs/2026-05-14-chevron-spacing-design.md`
  (0.2.1 sibling, no overlap).
