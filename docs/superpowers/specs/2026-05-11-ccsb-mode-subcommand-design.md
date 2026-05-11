# `ccsb mode` subcommand — 0.1.4 design

Status: approved 2026-05-11.

## Motivation

ccsb 0.1.2 shipped a native renderer, but the install path still keeps users
in proxy mode: `ccsb install` copies the existing `statusLine` into
`config.json` as `proxy.command`, so the native renderer is bypassed until
someone hand-edits the config to clear the `proxy` block. This subcommand
makes the choice explicit, scriptable, and reversible.

`mode` is intentionally orthogonal to `install`/`uninstall`. `install` hooks
ccsb into `~/.claude/settings.json` and seeds the proxy from whatever was
there; `mode` only touches `~/.config/ccsb/config.json` afterwards.

## CLI surface

```text
ccsb mode                                  # read:  prints "native" or "proxy" + "\n"
ccsb mode native                           # write: clears the proxy block
ccsb mode proxy                            # write: defaults to npx -y ccstatusline@latest
ccsb mode proxy <cmd> [arg ...]            # write: sets Proxy.Command/Args from argv
```

- Positional args. `args[0]` is `"mode"`, `args[1]` is `"native"` or `"proxy"`,
  `args[2:]` becomes the proxy command and its arguments. The user's shell
  is responsible for tokenisation; ccsb does not split or unquote.
- No success message on a write — silent rewrite is the script-friendly
  default and is consistent with how `install` only speaks up for the
  re-install corner case.
- The read form prints exactly one word followed by `"\n"`, exit 0.
  Suitable for shell predicates: `if [ "$(ccsb mode)" = native ]; then …`.

## State changes

- `mode` only writes `~/.config/ccsb/config.json`. `~/.claude/settings.json`
  is never touched.
- `mode native` sets `cfg.Proxy.Command = ""` and `cfg.Proxy.Args = nil`.
  Because `Config.Proxy` carries the `omitzero` JSON tag, the `proxy` key
  disappears entirely from the serialised config — the file shape after
  switching to native is indistinguishable from a fresh install with no
  prior proxy.
- `mode proxy` without extra args sets the package-level defaults:

  ```go
  const defaultProxyCommand = "npx"
  var defaultProxyArgs = []string{"-y", "ccstatusline@latest"}
  ```

  Both live in `internal/pkg/cli/mode.go` (see Implementation surface).
- `mode proxy <cmd> [arg ...]` sets `cfg.Proxy.Command = args[2]` and
  `cfg.Proxy.Args = append([]string(nil), args[3:]...)`. Args are stored
  verbatim; no expansion.
- `cfg.Backup.PreviousStatusLine` is unchanged in either direction — it
  exists for `uninstall`, not for mode flips.
- `cfg.Render` is unchanged. The render package treats an empty
  `Config.Rows` as "use defaultRows", so no seeding is required to make
  the native renderer produce useful output.

## Detection rule

`mode == "native"` iff `cfg.Proxy.Command == ""`. Everywhere ccsb needs to
report or branch on the mode (status output, read form, future tests), this
single predicate is the source of truth. `Proxy.Args` without a `Command`
is treated as native — it shouldn't occur in normal use, but the predicate
keeps the rule simple.

## `ccsb status` update

A new line is added directly before the `proxy:` line:

```text
ccsb: hooked:   yes
ccsb: self:     /home/user/.local/bin/ccsb
ccsb: settings: /home/user/.claude/settings.json
ccsb: config:   /home/user/.config/ccsb/config.json
ccsb: capture:  /home/user/.local/state/ccsb/captures
ccsb: mode:     proxy
ccsb: proxy:    npx -y ccstatusline@latest
ccsb: backup:   {"type":"command","command":"npx -y ccstatusline@latest"}
```

In native mode:

```text
ccsb: mode:     native
ccsb: proxy:    (none — built-in fallback)
```

The `proxy:` line is preserved unchanged — `mode:` is a redundant label that
exists for at-a-glance reading and for symmetry with the new subcommand.

## Errors and edge cases

- `ccsb mode foo` (unknown target) → returns `fmt.Errorf("ccsb: invalid mode %q (valid: native, proxy)", target)`. Plain error, no dedicated type — callers do not need to distinguish it from other failures.
- `ccsb mode` with extra args after `native` (e.g. `ccsb mode native extra`) is rejected with the same invalid-mode error pattern: `"ccsb: native mode takes no arguments"`. This catches users who type `ccsb mode native --some-flag` expecting a switch.
- `ccsb mode` works regardless of whether ccsb is installed in
  `settings.json`. It only edits `config.json`; the hook state is a separate
  concern. No `not installed` warning is printed (consistent with `status`,
  which works in either state).
- Config file does not exist yet → `config.Load` returns a zero `Config`
  with no error; the write path creates the file. Same behaviour as
  `install`.
- Missing both `XDG_CONFIG_HOME` and `HOME` → `config.Save` returns
  `"config: empty path"`, propagated unchanged.

## Help text and dispatcher

- `cli.go` switch in `Run` gains a `case "mode": return runMode(p, args[1:], stdout)`.
- `UnknownSubcommandError.Error()` updates its valid-list to
  `"install, uninstall, status, mode, help"`.
- `printHelp` gains a `mode` block:

  ```text
    mode        Print the current mode (native or proxy) when invoked with no
                argument. With "native", clear the proxy block; with "proxy",
                set it to "npx -y ccstatusline@latest" by default or to the
                given command and arguments.
  ```

## Implementation surface

One new file is sufficient: `internal/pkg/cli/mode.go` containing
`runMode(p Paths, args []string, stdout io.Writer) error` and the package-
private default constants. Tests go to `internal/pkg/cli/mode_test.go`,
mirroring the style of the existing `cli_test.go` cases (temp HOME,
temp `XDG_CONFIG_HOME`, full `Run` invocations via `bytes.Buffer`).

The existing `cli.go` only changes in three places:

1. The switch in `Run` gains the `mode` case.
2. `UnknownSubcommandError.Error()` lists the new subcommand.
3. `runStatus` gains the `mode:` line (one extra `fmt.Fprintf` plus the
   detection predicate inlined).

`printHelp` is updated in the same file or extracted later — out of scope
for this design.

## Test plan

All tests drive `cli.Run` end-to-end with injected `Paths`, `Flags`,
`stdin`, `stdout`, `stderr` and temp directories. Names follow the existing
`TestRun_<subcommand>_<scenario>` pattern.

1. `TestRun_mode_read_native` — no proxy in config → stdout `"native\n"`.
2. `TestRun_mode_read_proxy` — config with proxy set → stdout `"proxy\n"`.
3. `TestRun_mode_native_clearsProxy` — start with proxy, run `mode native`,
   reload config, assert `Proxy.Command == ""` and `Proxy.Args == nil` and
   that the serialised JSON does not contain a `"proxy"` key.
4. `TestRun_mode_proxy_defaults` — `mode proxy` on a clean config sets the
   `npx -y ccstatusline@latest` default.
5. `TestRun_mode_proxy_explicit` — `mode proxy /usr/bin/foo --x y` sets
   `Command=/usr/bin/foo`, `Args=[--x, y]`.
6. `TestRun_mode_proxy_overwrite` — proxy already set → `mode proxy
   /usr/bin/bar` replaces it, no Args carry-over.
7. `TestRun_mode_invalid` — `mode unicorn` returns the invalid-mode error;
   `mode native extra` returns the no-arguments error; config file is not
   touched in either case.
8. `TestRun_mode_idempotent` — `mode native` twice on a clean config
   produces byte-identical `config.json` both times.
9. `TestRun_status_mode_line` — `status` output contains the `mode:` line
   in both native and proxy states; ordering is `… capture → mode → proxy → backup`.
10. `TestRun_help_listsMode` — `help` output mentions `mode`.
11. `TestRun_unknownSubcommand_listsMode` — the unknown-subcommand error
    string contains `mode` in its valid list.

`Backup.PreviousStatusLine` is never touched by `mode`; cases 3 and 4
should also assert this explicitly to lock in the orthogonality with
`install`/`uninstall`.

## Out of scope

- Persisting a "previous proxy" so users can restore a custom proxy after
  switching to native and back. The current design uses a hardcoded default
  on `mode proxy` with no argument; users who want a non-default command
  pass it on the command line.
- Any change to how `install` seeds the initial proxy from
  `settings.json`. `install` continues to set proxy mode by default for
  users coming from an existing third-party `statusLine`.
- Render-config seeding or migration. `defaultRows` covers fresh installs.
- A `ccsb mode proxy --from-settings` variant that reads the saved backup.
  Not requested; can be added later without breaking this surface.
