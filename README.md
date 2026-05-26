# claude-cli-status-bar

A native [statusLine](https://docs.claude.com/en/docs/claude-code/statusline)
renderer for Claude Code, written in Go. ccsb reads the JSON payload Claude
Code emits on each status update and prints a Powerline-styled status line
directly — a single fast binary, no Node round-trip per update. Every payload
is captured to disk for later inspection, schema drift in the inbound JSON is
detected and logged automatically, and a transparent proxy mode is available
for compatibility with existing setups (see [Background](#background)).

> **Status:** active development — `0.2.22`. The native renderer is the
> primary mode: a fully configurable Powerline pipeline with an
> out-of-the-box default layout (model + mode + context bar + 5h/7d
> rate-limit bars + git branch + lines diff + cwd + version stamp +
> hidden-by-default schema-health indicator), per-segment right-align
> and configurable bar widths, terminal-size detection with automatic
> row-overflow reflow (segments marked `wrap: true` are lifted onto a
> new row when the parent row would overflow), and a four-rung
> schema-robustness ladder behind the indicator: per-segment
> isolated payload parsing, automatic `.diag` drift logging,
> persistent `schema_version` tracking, and a `ccsb doctor`
> schema-drift diff against the most recent capture. Compatibility-mode
> proxying to an existing `statusLine` command is supported as a
> fallback and is seeded automatically by `ccsb install` when a
> non-trivial existing entry is present.

## What it does

- **Native renderer** — renders the status line directly from the JSON
  payload. Rows and segments (`model`, `context`, `cost`, `duration`,
  `lines`, `cwd`, `git_branch`, `limit_5h`, `limit_7d`, `mode`, `effort`,
  `session_name`, `output_style`, `tty_size`, `version`, `schema_health`,
  `text`) are configurable. The default layout (used when no config file
  exists) is two-row Powerline with round end caps: row 1 carries model +
  mode glyph + context bar + 5h/7d rate-limit bars + a hidden-by-default
  schema-health indicator; row 2 carries git branch + lines diff + cwd
  and a right-aligned version stamp. Percentage segments escalate fg at
  70 % (amber) and 90 % (red).
- **Schema robustness** — per-segment isolated payload parsing contains
  any type error in one upstream field to that field only, so the rest of
  the bar keeps rendering. When the inbound JSON looks broken, a `.diag`
  file is written next to the capture with a stable plain-text dump of
  the issue (top-level parse error, missing critical fields, per-field
  unmarshal errors, additive keys). The last seen `schema_version` is
  persisted under `$XDG_STATE_HOME/ccsb/schema_version` so any upstream
  schema bump appears as an explicit transition in the next `.diag`.
- **Capture** — every invocation writes the raw stdin JSON to
  `$XDG_STATE_HOME/ccsb/captures/<RFC3339Nano>-<session_id>.json`, the
  rendered statusLine bytes to `.out`, any proxy stderr to `.err`, and
  any schema-drift diagnostic to `.diag` — all sharing the same basename
  so input, output, and diagnostic can be paired.
- **Hook management** — `ccsb install` swaps the `statusLine` entry in
  `~/.claude/settings.json` with the path to this binary and saves the
  previous value verbatim; `ccsb uninstall` restores it byte-for-byte;
  `ccsb doctor` auto-installs if the hook drifted, switches to native
  mode if the proxy target is circular or missing, and reports schema
  drift against the latest capture.
- **Mode + config** — `ccsb mode native` clears the proxy block;
  `ccsb mode proxy [cmd args]` reinstates it (defaulting to the
  same command the install heuristic recognises, for symmetry);
  `ccsb mode` prints the active mode; `ccsb config reset` moves the
  user config aside (timestamped backup) so the next run picks up
  the in-code defaults.
- **Proxy mode (compatibility)** — when a proxy command is configured,
  ccsb forwards the stdin payload to it and prints its stdout verbatim.
  Useful for setups that already have a different statusLine renderer in
  place — ccsb sits in front of it for capture and schema-drift logging
  while the existing renderer keeps driving the visible bar.

## Build

The Go module path is `go.muehmer.eu/claude-cli-status-bar`. A vanity
URL pointing at the public mirror on GitHub is planned; until it is
in place, build from source:

```bash
git clone https://github.com/bsmr/claude-cli-status-bar.git
cd claude-cli-status-bar
go install ./cmd/ccsb                          # installs to $GOBIN or ~/go/bin
# or build a local artifact:
go build -o bin/ccsb ./cmd/ccsb
install -m 0755 bin/ccsb ~/.local/bin/ccsb     # or anywhere on $PATH
```

Once the vanity URL is up, `go install go.muehmer.eu/claude-cli-status-bar/cmd/ccsb@vX.Y.Z`
will resolve directly without cloning.

The version segment reads its string from `runtime/debug.ReadBuildInfo()`:
a binary built from a checked-out tag reports that tag (`v0.2.14`), an
untagged commit reports `(devel)`. Check out the tag you want shipped if
you care about the version stamp.

Requires Go 1.26 or newer.

## Use

```bash
ccsb install      # save current statusLine, replace it with ccsb
ccsb mode         # print current mode (native or proxy)
ccsb mode native  # clear proxy block so the native renderer drives ccsb
ccsb mode proxy   # restore proxy mode (default: npx -y ccstatusline@latest)
ccsb config reset # move config.json aside (timestamped backup); defaults apply
ccsb status       # print resolved paths, hook state, mode, proxy/backup
ccsb uninstall    # restore the previous statusLine byte-for-byte
ccsb help         # subcommand summary
```

`install` saves the existing `statusLine` value verbatim into ccsb's config
as the backup and rewrites `statusLine.command` to the absolute path of the
ccsb binary; other top-level keys in `settings.json` are preserved.

If the existing entry matches the canonical
`npx -y ccstatusline@latest` invocation (a common predecessor — see
[Background](#background)), `install` lands directly in native mode:
the backup is preserved for `uninstall`, no proxy command is
configured, and the in-binary renderer drives the status line. For any
other command, the existing argv is seeded as `proxy.command` /
`proxy.args` so ccsb keeps forwarding to it. Switch mode any time with
`ccsb mode native` or `ccsb mode proxy <cmd args>`.

`uninstall` only proceeds when `statusLine` currently points at this binary —
manual edits since install are never overwritten.

## File locations

| Purpose | Path |
| --- | --- |
| Claude Code settings | `~/.claude/settings.json` |
| ccsb config | `${XDG_CONFIG_HOME:-$HOME/.config}/ccsb/config.json` |
| Captures | `${XDG_STATE_HOME:-$HOME/.local/state}/ccsb/captures/` |

The config file holds the proxy command/args plus a verbatim backup of the
previous `statusLine` value so `uninstall` can restore it. The `render`
section and the full segment vocabulary are documented in
[`docs/configuration.md`](docs/configuration.md).

## Roadmap

- **0.1.x** — proxy mode, capture, install/uninstall machinery, the
  configurable native renderer, and the `ccsb mode` subcommand.
- **0.2.x** — Powerline rendering with row-bg fill, chevron transitions,
  opt-in end caps, terminal-size detection, auto-detected version
  segment, row- and per-segment right-alignment, per-segment
  configurable bar widths, a full Powerline default layout out of the
  box, a `ccsb config reset` subcommand, and a four-rung
  schema-robustness ladder (per-segment isolated parsing, `.diag`
  drift logger, `ccsb doctor` schema-check, persistent
  `schema_version` tracking).
- **Next** — two layout primitives still open: `max_width` truncation
  (`cwd` / `git_branch` / `text` shorten with an ellipsis when the
  row would otherwise overflow) and `min_cols` conditional include
  (segment is hidden when the terminal is narrower than its declared
  threshold).
- **Later** — a GoReleaser-based release pipeline producing
  cross-platform binaries on GitHub Releases.

## Background

ccsb started as a thin Go proxy in front of an existing `statusLine`
command, so the JSON payload Claude Code emits on every status update
could be captured and inspected locally before reaching the downstream
renderer. The common predecessor on this hook at the time was
`npx -y ccstatusline@latest`, a Node-based renderer — which is why
ccsb still recognises that specific command on `install` and switches
straight to native mode (its own renderer covers the same ground
without the Node round-trip on every update).

The surrounding code then grew to validate, diagnose, and eventually
render the payload itself, and ccsb became a standalone statusLine
renderer. The proxy path is preserved as a compatibility option so
setups that already have a different renderer wired up keep working
unchanged while ccsb sits in front of it for capture and schema-drift
logging.

## Develop

```bash
go test -race -cover ./...   # all tests
go vet ./...
gofmt -l .                   # must be empty
go build -o bin/ccsb ./cmd/ccsb
```

See [`CLAUDE.md`](CLAUDE.md) for the package layout, data flow, and
contributor conventions.

## License

MIT — see [`LICENSE`](LICENSE).
