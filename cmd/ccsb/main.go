// Command ccsb is the Claude Code statusLine provider.
//
// Wired via "statusLine.command" in ~/.claude/settings.json: ccsb reads the
// JSON payload from stdin, captures it under $XDG_STATE_HOME/ccsb/captures,
// and forwards it to the configured proxied statusLine implementation. The
// install/uninstall subcommands manage the settings.json hook.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/cli"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve self: %w", err)
	}

	paths := cli.ResolvePaths(cli.Env{
		Home:          os.Getenv("HOME"),
		XDGConfigHome: os.Getenv("XDG_CONFIG_HOME"),
		XDGStateHome:  os.Getenv("XDG_STATE_HOME"),
		Self:          self,
	})

	return cli.Run(ctx, paths, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
}
