# claude-cli-status-bar

A small native [statusLine](https://docs.claude.com/en/docs/claude-code/statusline)
provider for Claude Code, written in Go. It can stand in front of an existing
statusLine implementation as a transparent proxy, captures every payload Claude
Code sends so it can be inspected later, and is meant to grow into a
self-contained renderer that drops the `npx` / Node round-trip on every update.

> **Status:** active development — `0.2.9`. Proxy mode, capture, hook
> management, a configurable Powerline-aware native renderer, terminal-size
> detection, row- and per-segment right-alignment, and a `ccsb mode`
> subcommand to switch between proxy and native rendering are all
> functional. `ccsb install` defaults to native mode when the previous
> `statusLine` was the canonical `npx -y ccstatusline@latest`; otherwise
> it seeds proxy mode so the prior renderer keeps working.

## What it does

- **Proxy mode** — invoked by Claude Code on stdin, forwards the payload to a
  configured child command and prints its stdout verbatim.
- **Native renderer** — when no proxy is configured, renders the status line
  directly from the JSON payload. Rows and segments (`model`, `context`,
  `cost`, `duration`, `lines`, `cwd`, `git_branch`, `limit_5h`, `limit_7d`,
  `mode`, `effort`, `session_name`, `output_style`, `tty_size`, `version`,
  `text`) are configurable; the default layout shows model + context bar,
  cost + rate-limit countdowns, git branch + working directory, and a
  right-aligned version row.
- **Mode switching** — `ccsb mode native` clears the proxy block in ccsb's
  config so the native renderer takes over; `ccsb mode proxy` reinstates it
  (defaulting to `npx -y ccstatusline@latest` or a given command and args).
  `ccsb mode` with no argument prints the active mode.
- **Capture** — every invocation writes the raw stdin JSON to
  `$XDG_STATE_HOME/ccsb/captures/<RFC3339Nano>-<session_id>.json`, the rendered
  statusLine bytes to `.out` and any proxy stderr to `.err`, all sharing the
  same basename so input and result can be paired.
- **Hook management** — `ccsb install` swaps the `statusLine` entry in
  `~/.claude/settings.json` with the path to this binary and saves the previous
  value verbatim; `ccsb uninstall` restores it byte-for-byte.

## Build

```bash
git clone ssh://git@git.nebula.muehmer.eu/bsmr/claude-cli-status-bar.git
cd claude-cli-status-bar
go build -o bin/ccsb ./cmd/ccsb
install -m 0755 bin/ccsb ~/.local/bin/ccsb   # or anywhere on $PATH
```

Requires Go 1.26 or newer.

## Use

```bash
ccsb install     # save current statusLine, replace it with ccsb
ccsb mode        # print current mode (native or proxy)
ccsb mode native # clear proxy block so the native renderer drives ccsb
ccsb mode proxy  # restore proxy mode (default: npx -y ccstatusline@latest)
ccsb status      # print resolved paths, hook state, mode, proxy/backup
ccsb uninstall   # restore the previous statusLine byte-for-byte
ccsb help        # subcommand summary
```

`install` saves the existing `statusLine` value verbatim into ccsb's config
as the backup and rewrites `statusLine.command` to the absolute path of the
ccsb binary; other top-level keys in `settings.json` are preserved. If the
previous entry was the canonical

```json
{ "statusLine": { "type": "command", "command": "npx -y ccstatusline@latest" } }
```

ccsb defaults to native mode — the backup stays for `uninstall`, but no
proxy command is configured and the in-binary renderer drives the status
line. For any other command, the existing argv is seeded as `proxy.command`
/ `proxy.args` so ccsb keeps forwarding to it. Switch any time with
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
  opt-in end caps, terminal-size detection, version segment with
  auto-detect via `runtime/debug.ReadBuildInfo()`, and right-alignment
  at both row and per-segment level.
- **Next** — `max_width` truncation and `min_cols` conditional include
  for narrower terminals. Otherwise the 0.2.x surface is stable.

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
