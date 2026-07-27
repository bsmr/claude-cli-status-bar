package selfupdate

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/render"
)

// stubBuildInfo installs build info with the given vcs.revision setting
// ("" omits the setting entirely, i.e. a go install build).
func stubBuildInfo(t *testing.T, revision string) {
	t.Helper()
	restore := buildInfoFunc
	info := &debug.BuildInfo{}
	if revision != "" {
		info.Settings = []debug.BuildSetting{{Key: "vcs.revision", Value: revision}}
	}
	buildInfoFunc = func() (*debug.BuildInfo, bool) { return info, true }
	t.Cleanup(func() { buildInfoFunc = restore })
}

// stubBuildSettings installs build info carrying exactly the given settings,
// for the cases stubBuildInfo cannot express — notably a vcs.revision key
// that is present but empty.
func stubBuildSettings(t *testing.T, settings ...debug.BuildSetting) {
	t.Helper()
	restore := buildInfoFunc
	info := &debug.BuildInfo{Settings: settings}
	buildInfoFunc = func() (*debug.BuildInfo, bool) { return info, true }
	t.Cleanup(func() { buildInfoFunc = restore })
}

func stubGOOS(t *testing.T, goos string) {
	t.Helper()
	restore := goosFunc
	goosFunc = func() string { return goos }
	t.Cleanup(func() { goosFunc = restore })
}

func TestGuardWindows(t *testing.T) {
	stubGOOS(t, "windows")
	stubBuildInfo(t, "")
	dir := t.TempDir()
	got := Guard(Options{Self: filepath.Join(dir, "ccsb.exe"), Current: "0.4.7"})
	if got != render.BlockWindows {
		t.Errorf("Guard = %q, want %q", got, render.BlockWindows)
	}
}

func TestGuardLocalBuild(t *testing.T) {
	stubGOOS(t, "linux")
	stubBuildInfo(t, "91a8bdd0deaf81d32e980802fd5af60dae223029")
	dir := t.TempDir()
	got := Guard(Options{Self: filepath.Join(dir, "ccsb"), Current: "0.4.7"})
	if got != render.BlockLocalBuild {
		t.Errorf("Guard = %q, want %q", got, render.BlockLocalBuild)
	}
}

// An empty vcs.revision is not a local build. The release pipeline runs with
// -buildvcs=false and stamps no vcs settings at all, so a present-but-empty
// key can only come from a toolchain that failed to resolve a revision —
// refusing the update there would block release users for nothing.
func TestGuardEmptyRevisionIsNotLocalBuild(t *testing.T) {
	stubGOOS(t, "linux")
	stubBuildSettings(t, debug.BuildSetting{Key: "vcs.revision", Value: ""})
	dir := t.TempDir()
	got := Guard(Options{Self: filepath.Join(dir, "ccsb"), Current: "0.4.7"})
	if got != render.BlockNone {
		t.Errorf("Guard = %q, want %q", got, render.BlockNone)
	}
}

func TestGuardNotWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	stubGOOS(t, "linux")
	stubBuildInfo(t, "")
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	got := Guard(Options{Self: filepath.Join(dir, "ccsb"), Current: "0.4.7"})
	if got != render.BlockNotWritable {
		t.Errorf("Guard = %q, want %q", got, render.BlockNotWritable)
	}
}

func TestGuardClear(t *testing.T) {
	stubGOOS(t, "linux")
	stubBuildInfo(t, "")
	dir := t.TempDir()
	got := Guard(Options{Self: filepath.Join(dir, "ccsb"), Current: "0.4.7"})
	if got != render.BlockNone {
		t.Errorf("Guard = %q, want empty", got)
	}
}

// fakeBinary writes an executable shell script printing out and exiting with
// code.
func fakeBinary(t *testing.T, dir, name, out string, code int) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := fmt.Sprintf("#!/bin/sh\necho '%s'\nexit %d\n", out, code)
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSmokeTestAcceptsMatchingVersion(t *testing.T) {
	bin := fakeBinary(t, t.TempDir(), "ccsb-new", "ccsb version 0.4.8", 0)
	if err := smokeTest(context.Background(), bin, "0.4.8"); err != nil {
		t.Errorf("smokeTest: %v", err)
	}
}

func TestSmokeTestRejectsNonZeroExit(t *testing.T) {
	bin := fakeBinary(t, t.TempDir(), "ccsb-new", "boom", 1)
	if err := smokeTest(context.Background(), bin, "0.4.8"); err == nil {
		t.Error("err = nil, want a failure for a non-zero exit")
	}
}

func TestSmokeTestRejectsWrongVersion(t *testing.T) {
	bin := fakeBinary(t, t.TempDir(), "ccsb-new", "ccsb version 0.4.1", 0)
	err := smokeTest(context.Background(), bin, "0.4.8")
	if err == nil {
		t.Fatal("err = nil, want a version mismatch")
	}
	if !strings.Contains(err.Error(), "0.4.8") {
		t.Errorf("err = %v, want it to name the expected version", err)
	}
}

func TestActivateReplacesBinaryAndClearsCache(t *testing.T) {
	dir := t.TempDir()
	stateDir := t.TempDir()
	self := fakeBinary(t, dir, "ccsb", "ccsb version 0.4.7", 0)
	staged := fakeBinary(t, dir, ".ccsb-update-x", "ccsb version 0.4.8", 0)

	cache := render.UpdateCachePath(stateDir)
	if err := os.WriteFile(cache, []byte(`{"latest_tag":"v0.4.8","unix":1}`), 0o600); err != nil {
		t.Fatal(err)
	}

	o := Options{StateDir: stateDir, Self: self, Current: "0.4.8"}
	if err := activate(context.Background(), o, staged, "0.4.8"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	got, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "0.4.8") {
		t.Errorf("target still holds the old binary: %q", got)
	}
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Error("update-check cache still present, want it removed")
	}
}

// A ccsb installed 0755 in a shared location must stay 0755. The staged file
// is written 0700 while unverified, and a rename would otherwise carry that
// private mode onto the target, taking the binary away from every other user
// on the machine.
func TestActivatePreservesTargetMode(t *testing.T) {
	dir := t.TempDir()
	self := fakeBinary(t, dir, "ccsb", "ccsb version 0.4.7", 0)
	if err := os.Chmod(self, 0o755); err != nil {
		t.Fatal(err)
	}
	staged := fakeBinary(t, dir, ".ccsb-update-x", "ccsb version 0.4.8", 0)
	if err := os.Chmod(staged, 0o700); err != nil {
		t.Fatal(err)
	}

	o := Options{StateDir: t.TempDir(), Self: self, Current: "0.4.7"}
	if err := activate(context.Background(), o, staged, "0.4.8"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	info, err := os.Stat(self)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Errorf("mode = %04o, want 0755", got)
	}
}

func TestActivateLeavesTargetOnFailedSmokeTest(t *testing.T) {
	dir := t.TempDir()
	self := fakeBinary(t, dir, "ccsb", "ccsb version 0.4.7", 0)
	staged := fakeBinary(t, dir, ".ccsb-update-x", "broken", 1)

	o := Options{StateDir: t.TempDir(), Self: self, Current: "0.4.8"}
	if err := activate(context.Background(), o, staged, "0.4.8"); err == nil {
		t.Fatal("err = nil, want the smoke test to fail")
	}
	got, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "0.4.7") {
		t.Errorf("target was replaced despite a failed smoke test: %q", got)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Error("staged file left behind, want it removed")
	}
}

func TestUpdateStampsAttemptOnGuardRefusal(t *testing.T) {
	stubGOOS(t, "windows")
	stubBuildInfo(t, "")
	stateDir := t.TempDir()

	o := Options{
		StateDir: stateDir,
		Self:     filepath.Join(t.TempDir(), "ccsb.exe"),
		Current:  "0.4.7",
		Stdout:   io.Discard,
		Stderr:   io.Discard,
	}
	err := Update(context.Background(), o)
	if err == nil {
		t.Fatal("err = nil, want a refusal on windows")
	}
	att, ok := render.ReadUpdateAttempt(stateDir)
	if !ok {
		t.Fatal("no attempt record written, want one")
	}
	if att.Blocked != render.BlockWindows {
		t.Errorf("Blocked = %q, want %q", att.Blocked, render.BlockWindows)
	}
}

func TestUpdateReportsAlreadyCurrent(t *testing.T) {
	stubGOOS(t, "linux")
	stubBuildInfo(t, "")
	stateDir := t.TempDir()
	dir := t.TempDir()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v0.4.7"}`))
	}))
	defer srv.Close()
	restore := latestReleaseURL
	latestReleaseURL = srv.URL
	defer func() { latestReleaseURL = restore }()

	var out bytes.Buffer
	o := Options{
		StateDir: stateDir,
		Self:     fakeBinary(t, dir, "ccsb", "ccsb version 0.4.7", 0),
		Current:  "0.4.7",
		Stdout:   &out,
		Stderr:   io.Discard,
	}
	if err := Update(context.Background(), o); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !strings.Contains(out.String(), "already current") {
		t.Errorf("stdout = %q, want an already-current notice", out.String())
	}
	att, ok := render.ReadUpdateAttempt(stateDir)
	if !ok {
		t.Fatal("no attempt record written, want one")
	}
	if att.Blocked != render.BlockNone {
		t.Errorf("Blocked = %q, want empty", att.Blocked)
	}
}
