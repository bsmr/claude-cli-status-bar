// Package proxy executes an external statusLine provider as a child process.
//
// It pipes a payload (typically the JSON received from Claude Code on stdin)
// to the child's stdin and streams the child's stdout and stderr to the
// supplied writers. The child's exit error - including non-zero exit codes
// reachable via *exec.ExitError - is returned wrapped.
package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"
)

// cancelGracePeriod bounds how long cmd.Wait may keep blocking after
// the context is cancelled. exec.CommandContext's default Cancel
// sends SIGKILL to the child, but cmd.Wait can still block on the
// runtime's stdin/stdout/stderr copy goroutines until the OS finishes
// tearing the pipes down. Setting cmd.WaitDelay forces the runtime
// to close those pipes after the grace window, so Run always
// returns promptly after a cancel. Half a second is plenty for a
// child that exec.CommandContext just SIGKILL'd.
const cancelGracePeriod = 500 * time.Millisecond

// ErrNotStarted marks a failure that happened BEFORE the child ran: the
// command could not be resolved, or the fork/exec itself failed. It is
// worth distinguishing because such a child cannot have written anything,
// so a caller may safely fall back to its own rendering. A child that ran
// and then failed carries no such guarantee — it may have emitted a
// partial line already.
var ErrNotStarted = errors.New("child never started")

// Run spawns command with args, sends payload on its stdin and copies its
// stdout/stderr to the supplied writers. ctx cancels the child.
//
// A failure to start is wrapped with ErrNotStarted; every other failure
// (non-zero exit, timeout, cancellation) is not.
func Run(ctx context.Context, command string, args []string, payload []byte, stdout, stderr io.Writer) error {
	if command == "" {
		return errors.New("proxy: empty command")
	}

	// Captured before the run so the error can state the limit rather than the
	// (by then zero) time remaining.
	var limit time.Duration
	if d, ok := ctx.Deadline(); ok {
		limit = time.Until(d).Round(time.Millisecond)
	}

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = cancelGracePeriod

	// Start and Wait are split rather than using cmd.Run so the two
	// outcomes stay distinguishable: a child that never started wrote
	// nothing, a child that started may have written a partial line.
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("proxy: %s: %w: %w", command, ErrNotStarted, err)
	}

	if err := cmd.Wait(); err != nil {
		// exec.CommandContext SIGKILLs on expiry, so cmd.Wait reports only
		// "signal: killed" — true but useless to someone whose bar went blank.
		// Distinguish the deadline from a plain cancel (SIGINT/SIGTERM, which
		// is not the proxy's fault) and say which limit was hit.
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("proxy: %s: timed out after %s", command, limit)
		}
		return fmt.Errorf("proxy: %s: %w", command, err)
	}
	return nil
}
