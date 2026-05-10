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
)

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

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("proxy: %s: %w", command, err)
	}
	return nil
}
