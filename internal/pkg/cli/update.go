package cli

import (
	"context"
	"errors"
	"io"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/selfupdate"
)

// runUpdate backs the `ccsb update` subcommand: it replaces the running
// binary with the newest GitHub release. Unlike the hidden refresh-*
// helpers this one is loud — a user typed it and wants to know what
// happened.
func runUpdate(ctx context.Context, p Paths, stdout, stderr io.Writer) error {
	if p.State == "" || p.Self == "" {
		return errors.New("ccsb: update: state directory or binary path unknown")
	}
	return selfupdate.Update(ctx, selfupdate.Options{
		StateDir: p.State,
		Self:     p.Self,
		Current:  Version,
		Stdout:   stdout,
		Stderr:   stderr,
	})
}
