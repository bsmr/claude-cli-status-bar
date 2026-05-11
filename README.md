# claude-cli-status-bar

A small native [statusLine](https://docs.claude.com/en/docs/claude-code/statusline)
provider for Claude Code, written in Go. It can stand in front of an existing
statusLine implementation as a transparent proxy, captures every payload Claude
Code sends so it can be inspected later, and is meant to grow into a
self-contained renderer that drops the `npx` / Node round-trip on every update.

> **Status:** active development — `0.1.2`. Proxy mode, capture, hook
> management, and a configurable native renderer are functional. When no
> proxy is configured, the native renderer drives the status line directly
> from the Claude Code payload.

## What it does

- **Proxy mode** — invoked by Claude Code on stdin, forwards the payload to a
  configured child command and prints its stdout verbatim.
- **Native renderer** — when no proxy is configured, renders the status line
  directly from the JSON payload. Rows and segments (`model`, `context`,
  `cost`, `duration`, `lines`, `cwd`, `git_branch`, `limit_5h`, `limit_7d`,
  `mode`, `effort`, `session_name`, `output_style`, `text`) are configurable;
  the default layout shows model + context bar, cost + rate-limit countdowns,
  and git branch + working directory.
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
ccsb status      # print resolved paths, hook state, current proxy/backup
ccsb uninstall   # restore the previous statusLine byte-for-byte
ccsb help        # subcommand summary
```

When `install` finds an existing entry like

```json
{ "statusLine": { "type": "command", "command": "npx -y ccstatusline@latest" } }
```

it copies the value into ccsb's config as the backup, derives
`proxy.command = "npx"` / `proxy.args = ["-y", "ccstatusline@latest"]` for the
forwarding path, and rewrites `statusLine.command` to the absolute path of the
ccsb binary. Other top-level keys in `settings.json` are preserved.

`uninstall` only proceeds when `statusLine` currently points at this binary —
manual edits since install are never overwritten.

## File locations

| Purpose | Path |
| --- | --- |
| Claude Code settings | `~/.claude/settings.json` |
| ccsb config | `${XDG_CONFIG_HOME:-$HOME/.config}/ccsb/config.json` |
| Captures | `${XDG_STATE_HOME:-$HOME/.local/state}/ccsb/captures/` |

The config file holds the proxy command/args plus a verbatim backup of the
previous `statusLine` value so `uninstall` can restore it.

## Roadmap

- **0.1.x** — proxy mode, capture, install/uninstall machinery, and a
  configurable native renderer covering the Claude Code payload (current).
- **Next** — drop the `npx`/Node round-trip on the install path: make the
  native renderer the default when no proxy is already present, document the
  segment vocabulary and config schema, and validate the renderer against the
  growing corpus of captured payloads.

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
