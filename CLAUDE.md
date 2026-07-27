# claude-cli-status-bar

## Language & Communication

- **Responses to the user**: Always in German — short, concise, technically precise.
- **Everything else** (code, commits, code comments, file contents): Always in English.
- **Prompt correction**: Check the user's German prompt for style, grammar, and spelling before answering. If errors are found: prepend brief corrections, then the actual answer.
- **Style (all languages)**: Precise, concise, technical. No filler text, no platitudes.

## Project Overview

A `statusLine` provider for [Claude Code](https://docs.claude.com/en/docs/claude-code/statusline).
Claude Code invokes the binary, sends a JSON payload on stdin (session id, model, workspace,
cost, etc.), and renders the first line of stdout as the status bar.

- **Module**: `go.muehmer.eu/claude-cli-status-bar`
- **Binary**: `ccsb` (short form — used in users' `~/.claude/settings.json` `statusLine.command`).
- **Runtime contract**: read JSON from stdin, write the rendered status bar to stdout, exit 0. Claude Code renders every line it receives, and the default layout emits two rows — the "single line" of the original 0.1.x contract no longer holds. Errors go to stderr; non-zero exit makes Claude Code fall back silently.
- **Performance**: Claude Code calls the binary on every status update — startup must be fast (< ~100 ms). Avoid heavy init, network calls in the hot path, or large dependencies. The one intentional exception is the `version` segment's update check: it never runs on the render path itself, only in a detached `ccsb refresh-update-check` the renderer spawns out-of-band (see `internal/pkg/render/updatecheck.go`).

### Operating modes

- **Proxy mode** (no args, the path Claude Code invokes): read stdin once, capture the raw bytes to `$XDG_STATE_HOME/ccsb/captures/<RFC3339Nano>-<session_id>.json`, then either forward the payload to a configured proxied statusLine command (its stdout becomes ours) or render a built-in fallback. The rendered output and any proxy stderr are captured alongside the input as `.out` and `.err` files sharing the same basename, so input and result can be paired by readers.
- **`ccsb install`**: on the first install — while ccsb is not yet hooked and its config still holds no backup — read the current `statusLine` from `~/.claude/settings.json`, save it verbatim to the ccsb config (`$XDG_CONFIG_HOME/ccsb/config.json`) under `backup.previous_status_line`, and seed `proxy.command`/`proxy.args` from it via whitespace split. The proxy block stays empty when the entry is the canonical `npx -y ccstatusline@latest` or another ccsb binary, so install lands in native mode; the backup is preserved either way. Every install replaces `statusLine` with this binary's path. Idempotent; never overwrites an existing backup, and once a backup exists the proxy block is left alone and managed via `ccsb mode`.
- **`ccsb uninstall`**: strict inverse — only proceeds if `statusLine` currently points at this binary. Restores the backup byte-for-byte (or removes the key if there was no prior).
- **`ccsb status`**: prints `hooked: yes/no`, resolved paths, current proxy command, and current backup.
- **`ccsb captures clean [--older-than <duration>]`**: removes capture files. Without arguments the cutoff is `time.Now()`, so everything goes — "remove all" is not a separate code path. `--older-than` accepts a day count (`7d`) or any Go duration (`24h`); `time.ParseDuration` has no day unit, so a trailing `d` is converted to hours in `parseRetention`. Age comes from the RFC3339Nano basename, never from `stat`. Files without a parsable timestamp and all subdirectories are left alone.

The capture path exists primarily to inspect what Claude Code actually sends, so a future built-in renderer can be validated against the real input rather than guessed at. Nothing prunes it automatically — `ccsb captures clean` is the manual escape hatch, deliberately kept out of the render hot path.

## Build & Test Commands

```bash
go build -o bin/ccsb ./cmd/ccsb        # build (output MUST go to bin/; version comes from ReadBuildInfo — "dev" only off-tag)
go build -o bin/ccsb -ldflags "-X go.muehmer.eu/claude-cli-status-bar/internal/pkg/cli.Version=X.Y.Z" ./cmd/ccsb  # versioned build
# Release builds (GoReleaser) add -buildvcs=false and always inject the version
# via that -X ldflag: a stamped vcs.revision means "local build" to the
# self-update guard, so release binaries must carry none.
go test ./...                           # all tests
go test -race -cover ./...              # with race detector and coverage
go vet ./...                            # static checks
go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...  # staticcheck (version pinned, same as CI)
gofmt -l .                              # list mis-formatted files (must be empty)
```

Smoke test against a Claude-Code-style payload (fallback render — no proxy configured):

```bash
echo '{"session_id":"x","model":{"display_name":"Sonnet"},"workspace":{"current_dir":"/tmp"}}' \
  | ./bin/ccsb
```

End-to-end against a throwaway HOME so the real `~/.claude/settings.json` is untouched:

```bash
TMPHOME=$(mktemp -d) TMPSTATE=$(mktemp -d) TMPCFG=$(mktemp -d)
mkdir -p "$TMPHOME/.claude"
cat > "$TMPHOME/.claude/settings.json" <<'EOF'
{"statusLine":{"type":"command","command":"npx -y ccstatusline@latest"},"theme":"dark"}
EOF
ccsb() { HOME=$TMPHOME XDG_CONFIG_HOME=$TMPCFG XDG_STATE_HOME=$TMPSTATE ./bin/ccsb "$@"; }
ccsb install && ccsb status && ccsb uninstall
```

## Architecture

```text
claude-cli-status-bar/
├── cmd/
│   └── ccsb/
│       └── main.go                 # entry point — wiring only; resolves env paths, calls cli.Run
├── internal/
│   └── pkg/
│       ├── cli/                    # subcommand dispatcher (install / uninstall / status / mode /
│       │                             config / captures / doctor / install-skill / uninstall-skill /
│       │                             version / help, plus the internal refresh-git-dirty and
│       │                             refresh-update-check helpers) and the no-args proxy-mode
│       │                             entry point
│       ├── render/                 # the native renderer: payload parsing, the segment registry,
│       │                             Powerline rows, palettes, reflow/shrink, terminal-size
│       │                             detection, schema diagnostics, git branch/dirty lookups,
│       │                             the GitHub release check (updatecheck.go), and the
│       │                             single-flight lock primitives shared by both out-of-band
│       │                             refreshers (singleflight.go)
│       ├── statusline/             # stdin → memory → capture (best effort) → proxy or native render
│       ├── proxy/                  # exec.CommandContext wrapper: pipes payload to child stdin,
│       │                             streams stdout/stderr through, propagates *exec.ExitError
│       ├── capture/                # one-file-per-invocation writer with atomic temp+rename,
│       │                             DefaultDir resolution from XDG_STATE_HOME, and Prune
│       ├── fileutil/               # WriteAtomic — the temp+rename writer every persistent
│       │                             write in the project goes through (0o700 dir / 0o600 file)
│       ├── config/                 # JSON config at $XDG_CONFIG_HOME/ccsb/config.json
│       │                             (proxy command/args + render config + statusLine backup)
│       └── claudesettings/         # ~/.claude/settings.json read/write preserving unknown keys
│                                     via map[string]json.RawMessage
├── bin/                            # build output (gitignored)
├── go.mod
└── CLAUDE.md
```

- All application logic lives in `internal/pkg/`. `cmd/ccsb/main.go` only resolves environment paths (`HOME`, `XDG_CONFIG_HOME`, `XDG_STATE_HOME`, `os.Executable()`) and calls `cli.Run`.
- I/O is injected via parameters (`context.Context`, `args []string`, `stdin io.Reader`, `stdout io.Writer`, `stderr io.Writer`) so packages are fully testable without touching the real process I/O.
- Filesystem locations are passed in as a `cli.Paths` struct, never read from the environment inside packages — this is what lets the test suite drop a temp HOME under each scenario.

### Data flow (proxy mode)

```text
stdin (Claude Code JSON)
   │
   ▼
cli.Run ──► statusline.Run
              │
              ├──► capture.Save(raw, sessionID, now)               .json (input)
              │
              ├──► proxy.Run via tee writers (errIgnoringWriter)
              │      │                                              ┌────────────────►  Claude Code stdout
              │      └─► child stdout ──► outBuf ──► .out (output) ─┘
              │           child stderr ──► errBuf ──► .err (only if non-empty)
              │
              └── (if ProxyCommand == "") fallback render is teed identically
```

Notes on the tee path:

- `errIgnoringWriter` swallows write errors from real stdout/stderr so a downstream consumer truncating its end of the pipe (`head -c N`, Claude Code closing early) cannot abort the MultiWriter chain mid-stream. The buffer must always reach `SaveOutput` with the full bytes the proxy emitted.
- For the same reason, `cmd/ccsb/main.go` ignores `SIGPIPE`. Without that, Go's default handler terminates the process on a broken-pipe write to fd 1/2 *before* `SaveOutput` runs, even though the user-level write returned an error we wanted to swallow.
- All capture files for one invocation share a basename — `.json` always, plus `.out`, `.err` and `.diag` when non-empty. Pass the same `time.Time` to `capture.Save` and `capture.SaveOutput` so the prefix matches; `capture.TimeFromName` parses it back, which is what `ccsb doctor` and `ccsb captures clean` sort and prune on.

## Style Guide

Follow the [Google Go Style Guide](https://google.github.io/styleguide/go/) for naming,
package design, error handling, and documentation conventions.

## Go Conventions

### Mandatory: `main()` → `run()` pattern

`main()` is a thin wrapper; `run()` is wiring only. `main()` calls `os.Exit()` which cannot
be intercepted in tests — keep all logic out of it.

```go
func main() {
    if err := run(); err != nil {
        fmt.Fprintf(os.Stderr, "error: %s\n", err)
        os.Exit(1)
    }
}

func run() error {
    // Ignore SIGPIPE so a consumer closing stdout cannot kill the process
    // before the capture files are written.
    signal.Ignore(syscall.SIGPIPE)

    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()

    paths, flags, err := cli.NewFromOS()
    if err != nil {
        return err
    }
    return cli.Run(ctx, paths, flags, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
}
```

- `main()` only calls `run()` and handles `os.Exit` — never in `run()`.
- `run()` is wiring only: `cli.NewFromOS()` resolves `HOME`/`XDG_*`/`NO_COLOR`/`os.Executable()` into `Paths` + `Flags`, then delegates to `internal/pkg/cli`.
- Application logic lives in `internal/pkg/<name>/`.
- All I/O is injected: `context.Context`, `args []string`, `stdin io.Reader`, `stdout io.Writer`, `stderr io.Writer`.
- Filesystem locations travel as explicit values (`cli.Paths`), not via env reads inside library packages.
- Errors are returned, not logged-and-exited.

### Tests and documentation

- Every package MUST have accompanying `_test.go` files with meaningful coverage.
- Tests are the primary development tool — write tests first, then implement.
- Document non-obvious logic; keep public API doc comments per Google Go Style Guide.

### `internal/cmd/` vs `internal/pkg/`

- `internal/pkg/` — reusable library packages, fully testable.
- `internal/cmd/` — build-time tools/generators (same `main()` → `run()` pattern). Not present yet; add only when needed.

### Build output

- Always build into `bin/`: `go build -o bin/ccsb ./cmd/ccsb`. Never run bare `go build` — it pollutes the project root.
- `bin/` is gitignored.

## Git Workflow

### Branch model

- `main` — current production commit, always deployable.
- `production-X.Y.Z` — versioned production branch, created from `main` after merge.
- `development-X.Y.Z-{main,work}` — version-tracked development. `-main` holds the base state from `main`; `-work` is the workspace.
- `feature-<name>-{main,work}`, `fix-<name>-{main,work}`, `hotfix-<name>-{main,work}` — same `-main` / `-work` split.

The active development branch tracks the current patch version
(`git branch --show-current`); release history lives in `git log
--first-parent main`.

### Merge rules

**Step 0 — sync local `main` with `origin/main` first**:

```bash
git fetch --all
git checkout main && git pull --ff-only
```

If `git branch -vv` shows `main` as `[origin/main: N behind]`, stop and fix this first — a stale local `main` makes the later push to `main` non-FF, which is forbidden.

1. **`-work` → `-main`** (squash):

   ```bash
   git checkout development-X.Y.Z-main
   git merge main
   git merge --squash development-X.Y.Z-work
   git commit -s -m "feat(X.Y.Z): <summary>"
   ```

2. **`-main` → `main`** (no-ff merge commit):

   ```bash
   git checkout main
   git merge --no-ff development-X.Y.Z-main
   git log origin/main..HEAD --oneline   # verify push-FF before pushing
   ```

3. **Tag production**:

   ```bash
   git branch production-X.Y.Z main
   git tag vX.Y.Z main
   git push origin production-X.Y.Z vX.Y.Z
   ```

   The `vX.Y.Z` tag is what the Go module proxy and `go install module@vX.Y.Z`
   use to resolve the version. `runtime/debug.ReadBuildInfo()` inside ccsb reads
   it back at startup, so `go install`-built binaries show the correct version
   without any `-ldflags` required.

   GoReleaser builds are the exception: they run `-buildvcs=false` and inject
   the version via `-X …/cli.Version={{ .Version }}`. Without that, the release
   binaries would carry `vcs.revision`, which the self-update guard reads as
   "local build" and refuses to overwrite. See `.goreleaser.yaml`.

### Remotes

- `origin` → personal/private fork (read-write).
- `upstream` → shared team repo (read-only; read-write for maintainers).
- External sources prefixed (`github-upstream`, `github-origin`, …).

### Commit messages

- AI-generated commits use conventional commits (`feat:`, `fix:`, `refactor:`, `docs:`, `chore:`, `test:`).
- Branches remain in `origin` as archive after merge (no deletion). Tags are optional.
