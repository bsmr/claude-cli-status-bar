package selfupdate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// modulePath is the import path `go install` resolves. It goes through the
// project's vanity URL, which redirects to the GitHub repository. Note that
// with the default GOPROXY the vanity host is not contacted at all — the
// module proxy answers from its cache — so a vanity outage mostly does not
// reach this path, and when it does, the caller falls back to the asset
// download, which never resolves a module path.
const modulePath = "go.muehmer.eu/claude-cli-status-bar/cmd/ccsb"

// goInstallTimeout bounds the toolchain run. A cold module cache plus
// compilation is slow, but this stays well below the single-flight lock TTL
// the 0.4.10 auto-update trigger uses — the update-check interval, 24h by
// default — so a hung build cannot outlive its own marker. That holds at the
// default only: the interval is user-tunable, and any value below this
// timeout lets the marker expire mid-build, allowing a second `go install`
// to start alongside the first.
const goInstallTimeout = 5 * time.Minute

// goBinDir reports where `go install` places binaries. The environment is
// read directly rather than shelling out to `go env`, which would cost a
// process for a value that is wrong only in the rare `go env -w GOBIN=...`
// case — and being wrong there merely routes the update through the asset
// path, which works regardless.
func goBinDir() string {
	if v := os.Getenv("GOBIN"); v != "" {
		return v
	}
	if v := os.Getenv("GOPATH"); v != "" {
		return filepath.Join(v, "bin")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "go", "bin")
}

// useGoInstall reports whether the go install path applies: a toolchain must
// be available and the running binary must live where go install would put
// it. Otherwise go install would write a second copy elsewhere and leave the
// binary actually in use untouched.
func useGoInstall(self string) bool {
	if _, err := exec.LookPath("go"); err != nil {
		return false
	}
	dir := goBinDir()
	if dir == "" {
		return false
	}
	return filepath.Dir(self) == filepath.Clean(dir)
}

// goInstall runs the toolchain against the pinned release tag. The version is
// pinned rather than @latest so the outcome matches what the update check
// reported. The toolchain performs download, checksum-database verification,
// build and atomic replacement itself.
func goInstall(ctx context.Context, version string) error {
	ctx, cancel := context.WithTimeout(ctx, goInstallTimeout)
	defer cancel()

	target := fmt.Sprintf("%s@v%s", modulePath, version)
	out, err := exec.CommandContext(ctx, "go", "install", target).CombinedOutput()
	if err != nil {
		return fmt.Errorf("go install %s: %w (output: %s)", target, err, out)
	}
	return nil
}
