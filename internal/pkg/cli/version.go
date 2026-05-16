package cli

import (
	"runtime/debug"
	"strings"
)

// Version is the ccsb release version. Resolution order:
//
//  1. -ldflags "-X go.muehmer.eu/claude-cli-status-bar/internal/pkg/cli.Version=<tag>"
//     (explicit build-time injection — takes precedence over everything else)
//  2. runtime/debug.ReadBuildInfo() — populated automatically when the
//     binary is installed from a tagged module release via go install
//     module@vX.Y.Z; the leading "v" is stripped so the render layer adds
//     it back uniformly.
//  3. "dev" — fallback for untagged local builds (go build without ldflags).
var Version = "dev"

func init() {
	if Version != "dev" {
		return // ldflags override takes precedence
	}
	info, _ := debug.ReadBuildInfo()
	if v := versionFromBuildInfo(info); v != "" {
		Version = v
	}
}

// versionFromBuildInfo extracts a clean version string from build info.
// Returns "" when the build info is absent, empty, or marks an untagged
// local build ("(devel)"). The leading "v" is stripped so callers can
// re-add it uniformly (e.g. renderVersion prepends "v").
func versionFromBuildInfo(info *debug.BuildInfo) string {
	if info == nil || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return ""
	}
	return strings.TrimPrefix(info.Main.Version, "v")
}
