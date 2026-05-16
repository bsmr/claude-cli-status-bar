package cli

// Version is the ccsb release version. Set at build time via
//
//	-ldflags "-X go.muehmer.eu/claude-cli-status-bar/internal/pkg/cli.Version=<tag>"
//
// Defaults to "dev" for unversioned local builds.
var Version = "dev"
