// Package statusline implements the Claude Code statusLine provider.
//
// Run reads a JSON payload from stdin once into memory, optionally captures
// the raw bytes plus the rendered output to disk, and either forwards the
// payload to a configured proxy command (whose stdout becomes our stdout)
// or, if no proxy is configured, renders a minimal built-in fallback line.
//
// See https://docs.claude.com/en/docs/claude-code/statusline for the upstream
// payload schema.
package statusline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/capture"
	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/proxy"
	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/render"
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
	// (.json) plus the rendered statusLine bytes (.out) and any stderr
	// (.err). All three files share a basename so they can be paired.
	// Capture failures are reported on stderr but do not fail Run.
	CaptureDir string
	// Render configures the built-in native renderer used when ProxyCommand
	// is empty. A zero value triggers the render package's default layout.
	Render render.Config
	// NoColor disables ANSI emission in the native renderer. Caller resolves
	// it from the NO_COLOR environment variable.
	NoColor bool
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

// Run reads stdin to completion, optionally captures input and rendered
// output, then either runs the configured proxy or renders the built-in
// fallback.
func Run(ctx context.Context, opts Options, stdin io.Reader, stdout, stderr io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	raw, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}

	// Best-effort parse: a malformed payload is still captured and proxied.
	var p payload
	_ = json.Unmarshal(raw, &p)

	now := time.Now()
	capturing := opts.CaptureDir != "" && len(raw) > 0

	if capturing {
		if _, cerr := capture.Save(opts.CaptureDir, p.SessionID, raw, now); cerr != nil {
			fmt.Fprintf(stderr, "ccsb: capture: %s\n", cerr)
		}
	}

	// Tee stdout/stderr through buffers so the rendered output can be saved
	// alongside the input. The downstream writers are wrapped so that a
	// closed-pipe error (the consumer stopped reading) does not abort the
	// MultiWriter chain - the buffer must end up with the full output even
	// if the real stdout/stderr drops bytes.
	var outBuf, errBuf bytes.Buffer
	outW, errW := stdout, stderr
	if capturing {
		outW = io.MultiWriter(errIgnoringWriter{stdout}, &outBuf)
		errW = io.MultiWriter(errIgnoringWriter{stderr}, &errBuf)
	}

	var runErr error
	if opts.ProxyCommand != "" {
		runErr = proxy.Run(ctx, opts.ProxyCommand, opts.ProxyArgs, raw, outW, errW)
	} else {
		cwd := p.Workspace.CurrentDir
		if cwd == "" {
			cwd = p.Workspace.ProjectDir
		}
		rendered, rerr := render.Render(render.Options{
			Config:  opts.Render,
			Cwd:     cwd,
			NoColor: opts.NoColor,
		}, raw)
		if rerr != nil {
			runErr = fmt.Errorf("render: %w", rerr)
		} else if _, werr := fmt.Fprintln(outW, rendered); werr != nil {
			runErr = fmt.Errorf("write statusLine: %w", werr)
		}
	}

	if capturing {
		if outBuf.Len() > 0 {
			if _, cerr := capture.SaveOutput(opts.CaptureDir, p.SessionID, outBuf.Bytes(), now, "out"); cerr != nil {
				fmt.Fprintf(stderr, "ccsb: capture out: %s\n", cerr)
			}
		}
		if errBuf.Len() > 0 {
			if _, cerr := capture.SaveOutput(opts.CaptureDir, p.SessionID, errBuf.Bytes(), now, "err"); cerr != nil {
				fmt.Fprintf(stderr, "ccsb: capture err: %s\n", cerr)
			}
		}
	}

	return runErr
}

// errIgnoringWriter wraps an io.Writer and reports every write as fully
// successful, even when the underlying writer returns an error (e.g. EPIPE
// because the consumer closed its end). It is paired with a buffer in a
// MultiWriter so partial reads downstream cannot abort the capture chain.
type errIgnoringWriter struct{ w io.Writer }

func (e errIgnoringWriter) Write(p []byte) (int, error) {
	if e.w != nil {
		_, _ = e.w.Write(p)
	}
	return len(p), nil
}
