// Package selfupdate replaces the running ccsb binary with the latest
// GitHub release.
//
// It never runs on the render path. `ccsb update` invokes it directly; from
// 0.4.9 the renderer may start that subcommand detached, but always as a
// separate process — this package is never imported by internal/pkg/render,
// which is what keeps the dependency on render one-way.
package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/render"
)

// Options bundles what Update needs. Filesystem locations are passed in
// rather than read from the environment here, matching the rest of the
// project.
type Options struct {
	StateDir string    // $XDG_STATE_HOME/ccsb
	Self     string    // absolute path of the running binary (cli.Paths.Self)
	Current  string    // running version without the leading "v", e.g. "0.4.7"
	Stdout   io.Writer // progress output; never nil in production
	Stderr   io.Writer // diagnostics, e.g. the path-A fallback notice
}

// buildInfoFunc reads this binary's build info. A package variable purely so
// tests can inject build info: inside a test binary the real call describes
// the test binary, so the local-build guard would be untestable otherwise.
// Production code never reassigns it.
var buildInfoFunc = debug.ReadBuildInfo

// goosFunc reports the target operating system. A package variable for the
// same testing reason as buildInfoFunc.
var goosFunc = func() string { return runtime.GOOS }

// isLocalBuild reports whether this binary came from a local `go build`
// rather than `go install module@version`. The Go toolchain stamps vcs.*
// settings only for builds made inside the VCS tree; a module-proxy build
// carries none.
//
// This is the guard that matters most: a local build made on a tagged,
// unmodified commit stamps a real version, so the intuitive "version == dev"
// test does not catch it.
func isLocalBuild() bool {
	info, ok := buildInfoFunc()
	if !ok || info == nil {
		return false
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && s.Value != "" {
			return true
		}
	}
	return false
}

// targetWritable reports whether new files can be created in dir. It probes
// by creating and removing a file rather than inspecting permission bits,
// which say nothing about ACLs, read-only mounts or effective privileges.
//
// Only ever called from the updater — never from a render.
func targetWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".ccsb-probe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

// Guard runs the pre-flight checks and returns the reason an update cannot
// proceed, or render.BlockNone when it may. Ordered cheapest first: two
// in-process checks before the filesystem probe.
func Guard(o Options) render.BlockReason {
	if goosFunc() == "windows" {
		return render.BlockWindows
	}
	if isLocalBuild() {
		return render.BlockLocalBuild
	}
	if !targetWritable(filepath.Dir(o.Self)) {
		return render.BlockNotWritable
	}
	return render.BlockNone
}

// smokeTestTimeout bounds the staged binary's self-report. It only has to
// print its version; anything slower is a hang.
const smokeTestTimeout = 10 * time.Second

// smokeTest runs the staged binary's `version` subcommand and requires a
// clean exit reporting wantVersion. This is what keeps a corrupted or
// mismatched download from ever going live — the failure mode that actually
// occurs, and the reason no backup or rollback command is needed.
func smokeTest(ctx context.Context, binary, wantVersion string) error {
	ctx, cancel := context.WithTimeout(ctx, smokeTestTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, binary, "version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("smoke test: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	want := "ccsb version " + wantVersion
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == want {
			return nil
		}
	}
	return fmt.Errorf("smoke test: %q does not report version %s", strings.TrimSpace(string(out)), wantVersion)
}

// activate smoke-tests the staged binary against wantVersion and, only on
// success, renames it over the running one. The staged file is removed on
// every failure path, leaving the target untouched.
//
// wantVersion is the version the staged binary is expected to report — not
// o.Current, which names the running binary's version and stays that way for
// the guard checks that read it.
//
// The replaced binary's permission bits are carried over before the rename.
// The staged file is deliberately created 0700 — it must not be readable by
// anyone else while it is still unverified — but a rename discards the
// target's mode, so a ccsb installed 0755 in a shared location would silently
// become user-only and vanish for every other user on the machine.
//
// The stale update-check cache is dropped afterwards: it still names the
// version just installed as "latest available", which would render a phantom
// update indicator until the cache ages out.
func activate(ctx context.Context, o Options, staged, wantVersion string) error {
	if err := smokeTest(ctx, staged, wantVersion); err != nil {
		_ = os.Remove(staged)
		return err
	}
	if err := os.Chmod(staged, targetMode(o.Self)); err != nil {
		_ = os.Remove(staged)
		return fmt.Errorf("prepare %s: %w", staged, err)
	}
	if err := os.Rename(staged, o.Self); err != nil {
		_ = os.Remove(staged)
		return fmt.Errorf("replace %s: %w", o.Self, err)
	}
	_ = os.Remove(render.UpdateCachePath(o.StateDir))
	return nil
}

// targetMode reports the permission bits the replacement should carry: those
// of the binary being replaced, or 0755 when it cannot be stated. 0755 is the
// right fallback because it matches what `go install` writes, which is the
// only other way a ccsb binary gets onto disk.
func targetMode(self string) os.FileMode {
	info, err := os.Stat(self)
	if err != nil {
		return 0o755
	}
	return info.Mode().Perm()
}

// latestReleaseURL is the endpoint Update queries for the newest release. A
// package variable so tests can redirect it; production code never
// reassigns it.
var latestReleaseURL = render.LatestReleaseURL

// Update replaces the running binary with the newest release.
//
// Every exit path stamps the attempt record — success, guard refusal and
// mid-run failure alike. Without a stamp on failure, a permanently failing
// update would leave the version segment's severity raised and, from 0.4.9
// on, spawn a fresh attempt on every render past the lock TTL.
func Update(ctx context.Context, o Options) error {
	err := update(ctx, o)

	blocked := render.BlockNone
	var b blockedError
	if errors.As(err, &b) {
		blocked = b.reason
	}
	// A successful update whose bookkeeping failed is still a successful
	// update, so the write error must not become the function's return
	// value — but it must not vanish silently either, since it undermines
	// the retry-storm guarantee above. o.Stderr can be nil once a future
	// release starts this command detached.
	if writeErr := render.WriteUpdateAttempt(o.StateDir, blocked); writeErr != nil && o.Stderr != nil {
		fmt.Fprintf(o.Stderr, "ccsb: update: could not record the update attempt: %v\n", writeErr)
	}
	return err
}

// blockedError carries a guard's refusal reason so Update can persist it.
type blockedError struct {
	reason render.BlockReason
	msg    string
}

func (e blockedError) Error() string { return e.msg }

// update is Update's body, without the attempt bookkeeping.
func update(ctx context.Context, o Options) error {
	// Resolve symlinks once, before anything decides on o.Self. A symlink
	// that appears to sit in GOBIN but resolves elsewhere would route to
	// `go install`, which writes a fresh binary into GOBIN while the binary
	// actually being executed stays stale — an update that silently does
	// nothing. Everything downstream (the writability probe, path
	// selection, staging directory, rename target) must see the real path.
	if resolved, err := filepath.EvalSymlinks(o.Self); err == nil {
		o.Self = resolved
	}

	switch reason := Guard(o); reason {
	case render.BlockWindows:
		return blockedError{reason, "ccsb: update: replacing a running .exe is not supported on Windows — download the release archive and swap the binary manually"}
	case render.BlockLocalBuild:
		return blockedError{reason, fmt.Sprintf("ccsb: update: %s is a local build (built from a checkout) — rebuild it with: go build -o bin/ccsb ./cmd/ccsb", o.Self)}
	case render.BlockNotWritable:
		return blockedError{reason, fmt.Sprintf("ccsb: update: %s is not writable — reinstall with the privileges that own it; ccsb never elevates by itself", filepath.Dir(o.Self))}
	}

	current, ok := render.ParseSemver(o.Current)
	if !ok {
		return fmt.Errorf("ccsb: update: running version %q is not a release version — nothing to compare against", o.Current)
	}

	tag, err := render.FetchLatestTag(ctx, latestReleaseURL)
	if err != nil {
		return fmt.Errorf("ccsb: update: %w", err)
	}
	latest, ok := render.ParseSemver(tag)
	if !ok {
		return fmt.Errorf("ccsb: update: latest release tag %q is not a release version", tag)
	}
	if render.CompareSeverity(current, latest) == render.SeverityNone {
		fmt.Fprintf(o.Stdout, "ccsb: update: already current (v%s)\n", o.Current)
		return nil
	}

	// target is the version being installed; o.Current stays the running one.
	// Keep them distinct — activate needs the target to smoke-test against,
	// and the progress line needs both.
	target := fmt.Sprintf("%d.%d.%d", latest.Major, latest.Minor, latest.Patch)

	if useGoInstall(o.Self) {
		err := goInstall(ctx, target)
		if err == nil {
			fmt.Fprintf(o.Stdout, "ccsb: updated %s → %s (go install)\n", o.Current, target)
			_ = os.Remove(render.UpdateCachePath(o.StateDir))
			return nil
		}
		// Falling back is deliberate and blanket. Right after a tag is
		// pushed the module proxy serves 404 for 30-60 minutes while the
		// GitHub asset is already published; DNS failures, a broken
		// toolchain and compile errors are indistinguishable here and
		// share this one correct response.
		fmt.Fprintf(o.Stderr, "ccsb: update: go install failed (%v) — falling back to the release asset\n", err)
	}
	return updateFromAsset(ctx, o, target)
}

// updateFromAsset performs the path-B update: verified download, extraction
// into the target directory, smoke test, atomic rename. target is the version
// to install; o.Current remains the running version.
//
// Only fetchAsset runs under the download deadline. Letting it cover the rest
// would make a legitimately slow download eat the smoke test's budget and
// destroy a perfectly good staged binary; smokeTest brings its own timeout.
func updateFromAsset(ctx context.Context, o Options, target string) error {
	archive, err := fetchAsset(ctx, target)
	if err != nil {
		return fmt.Errorf("ccsb: update: %w", err)
	}
	staged, err := extractBinary(archive, filepath.Dir(o.Self))
	if err != nil {
		return fmt.Errorf("ccsb: update: %w", err)
	}
	if err := activate(ctx, o, staged, target); err != nil {
		return fmt.Errorf("ccsb: update: %w", err)
	}
	fmt.Fprintf(o.Stdout, "ccsb: updated %s → %s\n", o.Current, target)
	return nil
}
