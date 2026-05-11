# `ccsb mode` subcommand implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the `ccsb mode <native|proxy>` subcommand for 0.1.4, including a read form, a write form for each mode, validation, status integration, help text, and the eleven test cases listed in the spec.

**Architecture:** Add a single new file `internal/pkg/cli/mode.go` carrying the dispatcher and the default proxy constants. Touch `internal/pkg/cli/cli.go` in three small places: the switch in `Run`, the `UnknownSubcommandError.Error()` string, the new `mode:` line in `runStatus`, and the help text. Mirror the existing `cli_test.go` end-to-end pattern in a new `internal/pkg/cli/mode_test.go`.

**Tech Stack:** Go 1.26, standard library only. The `encoding/json` package's `omitzero` tag (available since Go 1.24) is what makes `mode native` produce a `config.json` free of a `proxy` key.

**Reference spec:** `docs/superpowers/specs/2026-05-11-ccsb-mode-subcommand-design.md`.

## File structure

- Create: `internal/pkg/cli/mode.go` — `runMode` plus `defaultProxyCommand`/`defaultProxyArgs`.
- Create: `internal/pkg/cli/mode_test.go` — all `mode`/`status`/help tests for this feature.
- Modify: `internal/pkg/cli/cli.go` — add `mode` to the switch in `Run`, extend the `UnknownSubcommandError` message, add the `mode:` line in `runStatus`, list `mode` in the help text.

Everything else (`config`, `claudesettings`, `render`, `statusline`) stays untouched.

---

## Task 1: Read form (`ccsb mode` with no args)

**Files:**
- Create: `internal/pkg/cli/mode.go`
- Modify: `internal/pkg/cli/cli.go:75-87` (the `Run` switch)
- Create: `internal/pkg/cli/mode_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/pkg/cli/mode_test.go` with the following content:

```go
package cli_test

import (
	"bytes"
	"context"
	"testing"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/cli"
	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/config"
)

func TestRun_mode_read_native(t *testing.T) {
	e := newEnv(t)
	// no config written -> Proxy.Command is empty -> mode is native

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"mode"}, nil, &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.String() != "native\n" {
		t.Errorf("stdout: got %q, want %q", out.String(), "native\n")
	}
}

func TestRun_mode_read_proxy(t *testing.T) {
	e := newEnv(t)
	e.saveConfig(t, config.Config{Proxy: config.Proxy{
		Command: "npx",
		Args:    []string{"-y", "ccstatusline@latest"},
	}})

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"mode"}, nil, &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.String() != "proxy\n" {
		t.Errorf("stdout: got %q, want %q", out.String(), "proxy\n")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/pkg/cli -run 'TestRun_mode_read' -v`

Expected: both tests FAIL because `cli.Run` returns `*cli.UnknownSubcommandError` for `mode` (the switch in `cli.go` does not know that subcommand yet).

- [ ] **Step 3: Implement `runMode` and wire it into `Run`**

Create `internal/pkg/cli/mode.go`:

```go
package cli

import (
	"fmt"
	"io"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/config"
)

// defaultProxyCommand and defaultProxyArgs are used when `ccsb mode proxy` is
// invoked without an explicit command. They mirror what `install` picks up
// from a stock Claude Code settings.json that points at ccstatusline.
const defaultProxyCommand = "npx"

var defaultProxyArgs = []string{"-y", "ccstatusline@latest"}

// runMode handles the `ccsb mode` subcommand.
//
// With no args, it prints the current mode ("native" or "proxy") followed by
// a newline. Write forms are added in later tasks; for now anything past the
// read form is reported as an invalid mode.
func runMode(p Paths, args []string, stdout io.Writer) error {
	cfg, err := config.Load(p.Config)
	if err != nil {
		return err
	}

	if len(args) == 0 {
		fmt.Fprintln(stdout, currentMode(cfg))
		return nil
	}

	return fmt.Errorf("ccsb: invalid mode %q (valid: native, proxy)", args[0])
}

// currentMode reports "native" or "proxy" based on whether a proxy command
// is configured. This is the single source of truth for the mode label.
func currentMode(cfg config.Config) string {
	if cfg.Proxy.Command == "" {
		return "native"
	}
	return "proxy"
}
```

Add the `mode` case to the switch in `Run` (`internal/pkg/cli/cli.go`). After the change the switch reads:

```go
	switch args[0] {
	case "-h", "--help", "help":
		printHelp(stdout)
		return nil
	case "install":
		return runInstall(p, stdout)
	case "uninstall":
		return runUninstall(p, stdout)
	case "status":
		return runStatus(p, stdout)
	case "mode":
		return runMode(p, args[1:], stdout)
	default:
		return &UnknownSubcommandError{Name: args[0]}
	}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/pkg/cli -run 'TestRun_mode_read' -v`

Expected: both tests PASS. Then run the full package to confirm nothing else broke:

Run: `go test ./...`

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/mode.go internal/pkg/cli/mode_test.go internal/pkg/cli/cli.go
git commit -s -m "feat(cli): add ccsb mode read form

Wire up the new 'mode' subcommand. Without arguments it prints
'native' or 'proxy' based on whether a proxy command is configured
in ccsb's config.json. Write forms follow in subsequent commits.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: `mode native` clears the proxy block

**Files:**
- Modify: `internal/pkg/cli/mode.go`
- Modify: `internal/pkg/cli/mode_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/pkg/cli/mode_test.go`:

```go
func TestRun_mode_native_clearsProxy(t *testing.T) {
	e := newEnv(t)
	e.saveConfig(t, config.Config{
		Proxy: config.Proxy{Command: "npx", Args: []string{"-y", "ccstatusline@latest"}},
		Backup: config.Backup{
			PreviousStatusLine: json.RawMessage(`{"type":"command","command":"old"}`),
		},
	})

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"mode", "native"}, nil, &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}

	c := e.loadConfig(t)
	if c.Proxy.Command != "" {
		t.Errorf("Proxy.Command: got %q, want empty", c.Proxy.Command)
	}
	if c.Proxy.Args != nil {
		t.Errorf("Proxy.Args: got %#v, want nil", c.Proxy.Args)
	}

	// Backup must be preserved across a mode flip.
	if string(c.Backup.PreviousStatusLine) != `{"type":"command","command":"old"}` {
		t.Errorf("Backup.PreviousStatusLine clobbered: got %s", c.Backup.PreviousStatusLine)
	}

	// Serialised JSON must not carry an empty "proxy" key.
	raw, err := os.ReadFile(e.paths.Config)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(raw), `"proxy"`) {
		t.Errorf("config still mentions \"proxy\":\n%s", raw)
	}
}

func TestRun_mode_native_idempotent(t *testing.T) {
	e := newEnv(t)

	var buf bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"mode", "native"}, nil, &buf, &buf); err != nil {
		t.Fatalf("Run (1): %v", err)
	}
	first, err := os.ReadFile(e.paths.Config)
	if err != nil {
		t.Fatalf("read config (1): %v", err)
	}

	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"mode", "native"}, nil, &buf, &buf); err != nil {
		t.Fatalf("Run (2): %v", err)
	}
	second, err := os.ReadFile(e.paths.Config)
	if err != nil {
		t.Fatalf("read config (2): %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Errorf("mode native is not byte-identical on re-run:\n first=%s\nsecond=%s", first, second)
	}
}
```

Extend the imports at the top of `mode_test.go` so it now reads:

```go
import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/cli"
	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/config"
)
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/pkg/cli -run 'TestRun_mode_native' -v`

Expected: `TestRun_mode_native_clearsProxy` FAILs with the "invalid mode" error from `runMode` (the write form is not implemented yet). `TestRun_mode_native_idempotent` FAILs for the same reason on the first call.

- [ ] **Step 3: Implement the `native` write form**

Replace the body of `runMode` in `internal/pkg/cli/mode.go` with:

```go
func runMode(p Paths, args []string, stdout io.Writer) error {
	cfg, err := config.Load(p.Config)
	if err != nil {
		return err
	}

	if len(args) == 0 {
		fmt.Fprintln(stdout, currentMode(cfg))
		return nil
	}

	switch args[0] {
	case "native":
		cfg.Proxy = config.Proxy{}
		return config.Save(p.Config, cfg)
	}

	return fmt.Errorf("ccsb: invalid mode %q (valid: native, proxy)", args[0])
}
```

`config.Proxy{}` zeros both `Command` and `Args`; combined with the `omitzero` JSON tag on `Config.Proxy`, the saved file no longer contains a `proxy` key.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/pkg/cli -run 'TestRun_mode' -v`

Expected: all `TestRun_mode_*` tests PASS, including the read tests from Task 1.

Run: `go test ./...`

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/mode.go internal/pkg/cli/mode_test.go
git commit -s -m "feat(cli): mode native clears the proxy block

ccsb mode native zeros cfg.Proxy so the omitzero JSON tag drops the
proxy key from config.json. Backup is intentionally untouched so the
mode flip stays orthogonal to install/uninstall.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: `mode proxy` with default and explicit command

**Files:**
- Modify: `internal/pkg/cli/mode.go`
- Modify: `internal/pkg/cli/mode_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/pkg/cli/mode_test.go`:

```go
func TestRun_mode_proxy_defaults(t *testing.T) {
	e := newEnv(t)

	var buf bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"mode", "proxy"}, nil, &buf, &buf); err != nil {
		t.Fatalf("Run: %v", err)
	}

	c := e.loadConfig(t)
	if c.Proxy.Command != "npx" {
		t.Errorf("Proxy.Command: got %q, want %q", c.Proxy.Command, "npx")
	}
	want := []string{"-y", "ccstatusline@latest"}
	if !reflect.DeepEqual(c.Proxy.Args, want) {
		t.Errorf("Proxy.Args: got %#v, want %#v", c.Proxy.Args, want)
	}
}

func TestRun_mode_proxy_explicit(t *testing.T) {
	e := newEnv(t)

	var buf bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"mode", "proxy", "/usr/bin/foo", "--x", "y"}, nil, &buf, &buf); err != nil {
		t.Fatalf("Run: %v", err)
	}

	c := e.loadConfig(t)
	if c.Proxy.Command != "/usr/bin/foo" {
		t.Errorf("Proxy.Command: got %q", c.Proxy.Command)
	}
	want := []string{"--x", "y"}
	if !reflect.DeepEqual(c.Proxy.Args, want) {
		t.Errorf("Proxy.Args: got %#v, want %#v", c.Proxy.Args, want)
	}
}

func TestRun_mode_proxy_overwrite(t *testing.T) {
	e := newEnv(t)
	e.saveConfig(t, config.Config{Proxy: config.Proxy{
		Command: "/old/cmd",
		Args:    []string{"--legacy", "--flag"},
	}})

	var buf bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"mode", "proxy", "/usr/bin/bar"}, nil, &buf, &buf); err != nil {
		t.Fatalf("Run: %v", err)
	}

	c := e.loadConfig(t)
	if c.Proxy.Command != "/usr/bin/bar" {
		t.Errorf("Proxy.Command: got %q, want %q", c.Proxy.Command, "/usr/bin/bar")
	}
	if c.Proxy.Args != nil {
		t.Errorf("Proxy.Args should be nil after overwrite without trailing args, got %#v", c.Proxy.Args)
	}
}

func TestRun_mode_proxy_preservesBackup(t *testing.T) {
	e := newEnv(t)
	e.saveConfig(t, config.Config{
		Backup: config.Backup{
			PreviousStatusLine: json.RawMessage(`{"type":"command","command":"old"}`),
		},
	})

	var buf bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"mode", "proxy"}, nil, &buf, &buf); err != nil {
		t.Fatalf("Run: %v", err)
	}

	c := e.loadConfig(t)
	if string(c.Backup.PreviousStatusLine) != `{"type":"command","command":"old"}` {
		t.Errorf("Backup.PreviousStatusLine clobbered: got %s", c.Backup.PreviousStatusLine)
	}
}
```

Extend the imports at the top of `mode_test.go` to include `"reflect"`:

```go
import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/cli"
	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/config"
)
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/pkg/cli -run 'TestRun_mode_proxy' -v`

Expected: all four `TestRun_mode_proxy_*` tests FAIL with the "invalid mode" error from `runMode` because the `proxy` arm is not implemented yet.

- [ ] **Step 3: Implement the `proxy` write form**

Update `runMode` in `internal/pkg/cli/mode.go` so the switch covers both modes:

```go
func runMode(p Paths, args []string, stdout io.Writer) error {
	cfg, err := config.Load(p.Config)
	if err != nil {
		return err
	}

	if len(args) == 0 {
		fmt.Fprintln(stdout, currentMode(cfg))
		return nil
	}

	switch args[0] {
	case "native":
		cfg.Proxy = config.Proxy{}
		return config.Save(p.Config, cfg)
	case "proxy":
		cfg.Proxy = proxyFromArgs(args[1:])
		return config.Save(p.Config, cfg)
	}

	return fmt.Errorf("ccsb: invalid mode %q (valid: native, proxy)", args[0])
}

// proxyFromArgs builds a Proxy from the tokens after `proxy`. Empty input
// produces the npx/ccstatusline default; otherwise the first token becomes
// the command and the rest the arguments. Args are copied so the saved
// config never aliases the caller's argv.
func proxyFromArgs(tokens []string) config.Proxy {
	if len(tokens) == 0 {
		return config.Proxy{
			Command: defaultProxyCommand,
			Args:    append([]string(nil), defaultProxyArgs...),
		}
	}
	if len(tokens) == 1 {
		return config.Proxy{Command: tokens[0]}
	}
	return config.Proxy{
		Command: tokens[0],
		Args:    append([]string(nil), tokens[1:]...),
	}
}
```

The `len(tokens) == 1` branch is what gives the overwrite test its `Args == nil` expectation: a single command token must not carry over legacy args.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/pkg/cli -run 'TestRun_mode' -v`

Expected: every `TestRun_mode_*` test PASSes.

Run: `go test ./...`

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/mode.go internal/pkg/cli/mode_test.go
git commit -s -m "feat(cli): mode proxy with default and explicit command

ccsb mode proxy with no extra args picks npx -y ccstatusline@latest;
with cmd+args it writes them verbatim, defensively copying so the
config never aliases argv. A single trailing token clears any
previously stored args so the overwrite stays predictable.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Validation — invalid mode and extra args after `native`

**Files:**
- Modify: `internal/pkg/cli/mode.go`
- Modify: `internal/pkg/cli/mode_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/pkg/cli/mode_test.go`:

```go
func TestRun_mode_unknown_target(t *testing.T) {
	e := newEnv(t)

	var out, errOut bytes.Buffer
	err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"mode", "unicorn"}, nil, &out, &errOut)
	if err == nil {
		t.Fatal("expected error for unknown mode")
	}
	if !strings.Contains(err.Error(), "invalid mode") || !strings.Contains(err.Error(), "unicorn") {
		t.Errorf("error should name the invalid mode, got: %v", err)
	}

	// Config must not exist after a rejected mode call.
	if _, statErr := os.Stat(e.paths.Config); !os.IsNotExist(statErr) {
		t.Errorf("config.json must not be created on error, got stat err=%v", statErr)
	}
}

func TestRun_mode_native_rejectsExtraArgs(t *testing.T) {
	e := newEnv(t)

	var out, errOut bytes.Buffer
	err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"mode", "native", "extra"}, nil, &out, &errOut)
	if err == nil {
		t.Fatal("expected error for trailing args after native")
	}
	if !strings.Contains(err.Error(), "native") {
		t.Errorf("error should reference the offending mode, got: %v", err)
	}

	if _, statErr := os.Stat(e.paths.Config); !os.IsNotExist(statErr) {
		t.Errorf("config.json must not be created on error, got stat err=%v", statErr)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/pkg/cli -run 'TestRun_mode_unknown_target|TestRun_mode_native_rejectsExtraArgs' -v`

Expected:
- `TestRun_mode_unknown_target`: the error part PASSes (Task 1 already produced this error), but the config-must-not-exist check might PASS too since we never reached `config.Save` on the error path — confirm this is the case. The point of the test is regression protection.
- `TestRun_mode_native_rejectsExtraArgs`: FAILs because `mode native extra` is currently accepted (`args[0] == "native"` is the only thing checked) and writes the config.

If the unknown-target test already passes after Task 3, that is expected; the test exists as regression coverage. If it fails, the implementation in Step 3 will make it pass.

- [ ] **Step 3: Add the extra-args check**

In `internal/pkg/cli/mode.go`, expand the `native` arm so it rejects trailing tokens:

```go
	switch args[0] {
	case "native":
		if len(args) > 1 {
			return fmt.Errorf("ccsb: native mode takes no arguments, got %d", len(args)-1)
		}
		cfg.Proxy = config.Proxy{}
		return config.Save(p.Config, cfg)
	case "proxy":
		cfg.Proxy = proxyFromArgs(args[1:])
		return config.Save(p.Config, cfg)
	}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/pkg/cli -run 'TestRun_mode' -v`

Expected: every `TestRun_mode_*` test PASSes.

Run: `go test ./...`

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/mode.go internal/pkg/cli/mode_test.go
git commit -s -m "feat(cli): reject invalid mode targets and extra args after native

Both errors propagate before any config write so a typo never leaves
config.json in a half-edited state. Adds regression tests that assert
the file does not appear when the call is rejected.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: `ccsb status` exposes the current mode

**Files:**
- Modify: `internal/pkg/cli/cli.go:183-211` (the `runStatus` function)
- Modify: `internal/pkg/cli/mode_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/pkg/cli/mode_test.go`:

```go
func TestRun_status_modeLine_native(t *testing.T) {
	e := newEnv(t)
	e.writeSettings(t, `{}`)

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"status"}, nil, &out, &errOut); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out.String(), "mode:     native") {
		t.Errorf("status should report native mode, got:\n%s", out.String())
	}
}

func TestRun_status_modeLine_proxy(t *testing.T) {
	e := newEnv(t)
	e.writeSettings(t, `{}`)
	e.saveConfig(t, config.Config{Proxy: config.Proxy{
		Command: "npx",
		Args:    []string{"-y", "ccstatusline@latest"},
	}})

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"status"}, nil, &out, &errOut); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out.String(), "mode:     proxy") {
		t.Errorf("status should report proxy mode, got:\n%s", out.String())
	}
}

func TestRun_status_modeLine_ordering(t *testing.T) {
	e := newEnv(t)
	e.writeSettings(t, `{}`)

	var out, errOut bytes.Buffer
	if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"status"}, nil, &out, &errOut); err != nil {
		t.Fatalf("status: %v", err)
	}

	got := out.String()
	captureIdx := strings.Index(got, "capture:")
	modeIdx := strings.Index(got, "mode:")
	proxyIdx := strings.Index(got, "proxy:")
	backupIdx := strings.Index(got, "backup:")
	if captureIdx < 0 || modeIdx < 0 || proxyIdx < 0 || backupIdx < 0 {
		t.Fatalf("status output missing one of capture/mode/proxy/backup:\n%s", got)
	}
	if !(captureIdx < modeIdx && modeIdx < proxyIdx && proxyIdx < backupIdx) {
		t.Errorf("status lines must appear in order capture -> mode -> proxy -> backup, got order indices c=%d m=%d p=%d b=%d in:\n%s",
			captureIdx, modeIdx, proxyIdx, backupIdx, got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/pkg/cli -run 'TestRun_status_modeLine' -v`

Expected: all three FAIL because `runStatus` does not yet emit a `mode:` line.

- [ ] **Step 3: Add the `mode:` line to `runStatus`**

In `internal/pkg/cli/cli.go`, locate `runStatus` and add a line between the `capture:` and `proxy:` blocks. After the change the function body reads:

```go
func runStatus(p Paths, stdout io.Writer) error {
	cfg, err := config.Load(p.Config)
	if err != nil {
		return err
	}
	s, err := claudesettings.Load(p.Settings)
	if err != nil {
		return err
	}

	hooked := pointsToSelf(s, p.Self)
	fmt.Fprintf(stdout, "ccsb: hooked: %s\n", yesNo(hooked))
	fmt.Fprintf(stdout, "ccsb: self:     %s\n", p.Self)
	fmt.Fprintf(stdout, "ccsb: settings: %s\n", p.Settings)
	fmt.Fprintf(stdout, "ccsb: config:   %s\n", p.Config)
	fmt.Fprintf(stdout, "ccsb: capture:  %s\n", p.Capture)
	fmt.Fprintf(stdout, "ccsb: mode:     %s\n", currentMode(cfg))
	if cfg.Proxy.Command != "" {
		args := strings.Join(cfg.Proxy.Args, " ")
		fmt.Fprintf(stdout, "ccsb: proxy:    %s %s\n", cfg.Proxy.Command, args)
	} else {
		fmt.Fprintln(stdout, "ccsb: proxy:    (none — built-in fallback)")
	}
	if len(cfg.Backup.PreviousStatusLine) > 0 {
		fmt.Fprintf(stdout, "ccsb: backup:   %s\n", string(cfg.Backup.PreviousStatusLine))
	} else {
		fmt.Fprintln(stdout, "ccsb: backup:   (none)")
	}
	return nil
}
```

`currentMode` already lives in `mode.go` and is package-visible, so no extra import or helper is needed.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/pkg/cli -run 'TestRun_status' -v`

Expected: all three new tests PASS, and the existing `TestStatus_*` tests still PASS.

Run: `go test ./...`

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/cli.go internal/pkg/cli/mode_test.go
git commit -s -m "feat(cli): expose current mode in ccsb status

Adds a 'mode:' line between 'capture:' and 'proxy:' so the active
renderer (native or proxied) is visible at a glance. The proxy:
line is unchanged; mode: is a redundant-on-purpose label that
matches the new subcommand vocabulary.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Help text and unknown-subcommand listing mention `mode`

**Files:**
- Modify: `internal/pkg/cli/cli.go:66` (the `UnknownSubcommandError.Error` string)
- Modify: `internal/pkg/cli/cli.go:220-236` (the `printHelp` constant)
- Modify: `internal/pkg/cli/mode_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/pkg/cli/mode_test.go`:

```go
func TestRun_help_listsMode(t *testing.T) {
	e := newEnv(t)
	for _, flag := range []string{"-h", "--help", "help"} {
		var out, errOut bytes.Buffer
		if err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{flag}, nil, &out, &errOut); err != nil {
			t.Fatalf("%s: %v", flag, err)
		}
		if !strings.Contains(out.String(), "mode") {
			t.Errorf("%s: help should mention the mode subcommand, got:\n%s", flag, out.String())
		}
	}
}

func TestRun_unknownSubcommand_listsMode(t *testing.T) {
	e := newEnv(t)
	var buf bytes.Buffer
	err := cli.Run(context.Background(), e.paths, cli.Flags{}, []string{"frobnicate"}, nil, &buf, &buf)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "mode") {
		t.Errorf("unknown-subcommand error should list 'mode' as valid, got: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/pkg/cli -run 'TestRun_help_listsMode|TestRun_unknownSubcommand_listsMode' -v`

Expected: both FAIL — the current help text and `UnknownSubcommandError` do not mention `mode`.

- [ ] **Step 3: Update the help text and the unknown-subcommand error**

In `internal/pkg/cli/cli.go`, change the `UnknownSubcommandError.Error` body from:

```go
func (e *UnknownSubcommandError) Error() string {
	return fmt.Sprintf("ccsb: unknown subcommand %q (valid: install, uninstall, status, help)", e.Name)
}
```

to:

```go
func (e *UnknownSubcommandError) Error() string {
	return fmt.Sprintf("ccsb: unknown subcommand %q (valid: install, uninstall, status, mode, help)", e.Name)
}
```

Then update `printHelp` so the subcommand block reads:

```go
	const help = `ccsb - Claude Code statusLine provider

Without arguments, ccsb reads the JSON payload from stdin and renders the
configured statusLine (proxy or built-in fallback). Claude Code invokes it
via the "statusLine.command" entry in ~/.claude/settings.json.

Subcommands:
  install     Save the current statusLine into ccsb's config and replace it
              in settings.json with this binary so Claude Code calls ccsb.
  uninstall   Restore the previous statusLine from the saved backup.
  status      Print whether settings.json points at ccsb and show the
              current proxy/backup state.
  mode        Print the current mode (native or proxy) when invoked with no
              argument. With "native", clear the proxy block; with "proxy",
              set it to "npx -y ccstatusline@latest" by default or to the
              given command and arguments.
  help        Print this message.
`
```

(`mode` slots between `status` and `help` so `help` stays last.)

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/pkg/cli -v`

Expected: every test in the package PASSes.

Run: `go test -race -cover ./...`

Expected: all tests PASS under the race detector. The line coverage of `internal/pkg/cli` should not regress; the new `mode.go` and its tests should bring it up.

Run: `go vet ./...`

Expected: no output.

Run: `gofmt -l .`

Expected: empty output. If anything is listed, run `gofmt -w .` and re-run.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/cli.go internal/pkg/cli/mode_test.go
git commit -s -m "docs(cli): list mode in help text and unknown-subcommand error

Brings the discoverability surface (help, error string) in line with
the new subcommand. mode slots between status and help so help stays
last in the listing.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Final verification

After Task 6, the branch is feature-complete. Before handing it off for the squash-merge into `development-0.1.4-main`:

- [ ] Run the full suite once more: `go test -race -cover ./...` — every package PASSes.
- [ ] Lint: `go vet ./...` produces no output; `gofmt -l .` is empty.
- [ ] Manual smoke test (build, exercise both modes against a throwaway HOME):

```bash
go build -o bin/ccsb ./cmd/ccsb
TMPHOME=$(mktemp -d) TMPSTATE=$(mktemp -d) TMPCFG=$(mktemp -d)
mkdir -p "$TMPHOME/.claude"
echo '{}' > "$TMPHOME/.claude/settings.json"
ccsb() { HOME=$TMPHOME XDG_CONFIG_HOME=$TMPCFG XDG_STATE_HOME=$TMPSTATE ./bin/ccsb "$@"; }
ccsb install
ccsb mode                # expect: native
ccsb mode proxy          # writes npx -y ccstatusline@latest
ccsb mode                # expect: proxy
ccsb status              # expect mode: proxy line; proxy: npx -y ccstatusline@latest
ccsb mode proxy /usr/bin/foo --bar baz
ccsb status              # expect proxy: /usr/bin/foo --bar baz
ccsb mode native         # clears proxy
ccsb status              # expect mode: native; proxy: (none — built-in fallback)
cat "$TMPCFG/ccsb/config.json"  # confirm there is no "proxy" key
ccsb uninstall           # statusLine is restored (empty in this throwaway case)
```

Each expectation lines up with one of the eleven spec test cases plus the existing install/uninstall smoke.

## Out of scope (reminder)

The spec explicitly excludes these — do not add them:

- Persisting a "previous proxy" backup so a custom proxy can be restored without retyping. Use the explicit-cmd form instead.
- Any change to how `install` seeds the initial proxy from `settings.json`.
- Seeding `cfg.Render` defaults; `render.defaultRows` already covers fresh configs.
- A `ccsb mode proxy --from-settings` variant.
