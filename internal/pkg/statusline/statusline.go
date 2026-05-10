// Package statusline implements the Claude Code statusLine provider.
//
// Run reads a JSON payload from stdin once into memory, optionally captures
// the raw bytes to disk for later inspection, and either forwards the payload
// to a configured proxy command (whose stdout becomes our stdout) or, if no
// proxy is configured, renders a minimal built-in fallback line.
//
// See https://docs.claude.com/en/docs/claude-code/statusline for the upstream
// payload schema.
package statusline

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/capture"
	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/proxy"
)

// Options configures a single Run invocation. All fields are optional; an
// empty Options value triggers fallback rendering with no capture.
type Options struct {
	// ProxyCommand, when non-empty, is executed with ProxyArgs and receives
	// the raw stdin payload on its own stdin. Its stdout becomes Run's
	// stdout. When empty, Run renders a minimal built-in fallback line.
	ProxyCommand string
	ProxyArgs    []string
	// CaptureDir, when non-empty, receives a copy of the raw stdin payload
	// per capture.Save. Capture failures are reported on stderr but do not
	// fail Run.
	CaptureDir string
}

type payload struct {
	SessionID string    `json:"session_id"`
	Model     model     `json:"model"`
	Workspace workspace `json:"workspace"`
}

type model struct {
	DisplayName string `json:"display_name"`
}

type workspace struct {
	CurrentDir string `json:"current_dir"`
	ProjectDir string `json:"project_dir"`
}

const placeholder = "claude-cli-status-bar"

// Run reads stdin to completion, optionally captures it, then either runs the
// configured proxy or renders the built-in fallback.
func Run(ctx context.Context, opts Options, stdin io.Reader, stdout, stderr io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	raw, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}

	// Parse best-effort: a malformed payload is still captured and proxied.
	var p payload
	_ = json.Unmarshal(raw, &p)

	if opts.CaptureDir != "" && len(raw) > 0 {
		if _, cerr := capture.Save(opts.CaptureDir, p.SessionID, raw, time.Now()); cerr != nil {
			fmt.Fprintf(stderr, "ccsb: capture: %s\n", cerr)
		}
	}

	if opts.ProxyCommand != "" {
		return proxy.Run(ctx, opts.ProxyCommand, opts.ProxyArgs, raw, stdout, stderr)
	}

	if _, err := fmt.Fprintln(stdout, render(p)); err != nil {
		return fmt.Errorf("write statusLine: %w", err)
	}
	return nil
}

func render(p payload) string {
	var parts []string
	if p.Model.DisplayName != "" {
		parts = append(parts, p.Model.DisplayName)
	}
	switch {
	case p.Workspace.CurrentDir != "":
		parts = append(parts, p.Workspace.CurrentDir)
	case p.Workspace.ProjectDir != "":
		parts = append(parts, p.Workspace.ProjectDir)
	}
	if len(parts) == 0 {
		return placeholder
	}
	return strings.Join(parts, " · ")
}
