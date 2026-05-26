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
	// Ignore SIGPIPE so a downstream consumer (Claude Code, a shell pipeline)
	// closing its end of stdout does not terminate the process before the
	// capture files are written. Writes still return EPIPE; we tolerate that.
	signal.Ignore(syscall.SIGPIPE)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	paths, flags, err := cli.NewFromOS()
	if err != nil {
		return err
	}
	return cli.Run(ctx, paths, flags, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
}
