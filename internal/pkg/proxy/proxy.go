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

// Run spawns command with args, sends payload on its stdin and copies its
// stdout/stderr to the supplied writers. ctx cancels the child.
func Run(ctx context.Context, command string, args []string, payload []byte, stdout, stderr io.Writer) error {
	if command == "" {
		return errors.New("proxy: empty command")
	}

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = cancelGracePeriod

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("proxy: %s: %w", command, err)
	}
	return nil
}
