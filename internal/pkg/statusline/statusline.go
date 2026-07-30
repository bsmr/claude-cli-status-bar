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
	"errors"
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
	// ProxyTimeout bounds how long the proxy child may run. Zero means no
	// limit, which is the pre-0.4.15 behaviour and a deliberate escape hatch
	// for a proxy known to be slow.
	ProxyTimeout time.Duration
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
	// Version is forwarded to the render package for the "version" segment
	// type. Empty string suppresses the segment.
	Version string
	// StateDir is ccsb's state directory, forwarded to the render package
	// as the cache root for the "git_dirty" segment. Empty suppresses that
	// segment.
	StateDir string
	// AutoUpdate is the user's update.auto preference, forwarded to the
	// render package. Empty disables automatic updating.
	AutoUpdate string
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

// maxStdinBytes caps the JSON payload we are willing to read from Claude
// Code. Real payloads are a few kilobytes; the cap is defense-in-depth
// against a buggy or malicious parent that pipes an unbounded stream.
const maxStdinBytes = 10 << 20 // 10 MiB

// Run reads stdin to completion, optionally captures input and rendered
// output, then either runs the configured proxy or renders the built-in
// fallback.
func Run(ctx context.Context, opts Options, stdin io.Reader, stdout, stderr io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	raw, err := io.ReadAll(io.LimitReader(stdin, maxStdinBytes))
	if err != nil {
		return fmt.Errorf("statusline: read stdin: %w", err)
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
		// Schema-drift logger + schema-version acknowledge. A .diag
		// file is written when either the payload trips the
		// schema-health detection, OR the schema_version value the
		// payload reports changed since the last time ccsb saw one.
		// Healthy + same-version captures produce no .diag.
		diag := render.Diagnose(raw)
		statePath := schemaVersionStatePath(opts.CaptureDir)
		prevVer := loadSchemaVersion(statePath)
		curVer := render.SchemaVersionOf(raw)
		versionChanged := prevVer != "" && curVer != "" && prevVer != curVer

		if diag.Issue() || versionChanged {
			content := diag.Format()
			if versionChanged {
				content = fmt.Appendf(content, "schema_version changed: %s -> %s\n", prevVer, curVer)
			}
			if _, cerr := capture.SaveOutput(opts.CaptureDir, p.SessionID, content, now, "diag"); cerr != nil {
				fmt.Fprintf(stderr, "ccsb: capture diag: %s\n", cerr)
			}
		}

		// Persist the current schema_version when it differs from
		// what we stored. The first sighting (prev == "") also
		// initialises the state file so subsequent changes can be
		// detected; a missing schema_version on a later invocation
		// (e.g. a downgrade) does NOT erase the stored value.
		if curVer != "" && curVer != prevVer {
			if err := saveSchemaVersion(statePath, curVer); err != nil {
				fmt.Fprintf(stderr, "ccsb: schema-version state: %s\n", err)
			}
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
		// Bound the child. The only context reaching here otherwise is main's
		// signal.NotifyContext, which never expires, so a proxy that hangs
		// hangs ccsb with it — on every status update, indefinitely. Zero
		// keeps the old unbounded behaviour for a proxy known to be slow.
		pctx := ctx
		if opts.ProxyTimeout > 0 {
			var cancel context.CancelFunc
			pctx, cancel = context.WithTimeout(ctx, opts.ProxyTimeout)
			defer cancel()
		}
		runErr = proxy.Run(pctx, opts.ProxyCommand, opts.ProxyArgs, raw, outW, errW)

		// A proxy that never started wrote nothing, so rendering natively
		// cannot corrupt a partial line — and it is strictly better than
		// the alternative: returning an error here exits non-zero, and a
		// non-zero exit makes Claude Code show no status bar at all. The
		// configuration is left alone; the reason goes to stderr, and
		// `ccsb doctor` reports it too.
		//
		// Deliberately NOT extended to a proxy that ran and then failed or
		// timed out (0.4.15): that child may already have emitted part of a
		// line, and rendering over it would produce a corrupted bar.
		if errors.Is(runErr, proxy.ErrNotStarted) {
			fmt.Fprintf(errW, "ccsb: %s — falling back to the native renderer\n", runErr)
			runErr = renderNative(opts, p, raw, outW)
		}
	} else {
		runErr = renderNative(opts, p, raw, outW)
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

// renderNative renders the built-in layout and writes it to out. It is the
// path taken when no proxy is configured, and the fallback when a configured
// proxy could not be started at all.
func renderNative(opts Options, p payload, raw []byte, out io.Writer) error {
	cwd := p.Workspace.CurrentDir
	if cwd == "" {
		cwd = p.Workspace.ProjectDir
	}
	rendered, err := render.Render(render.Options{
		Config:     opts.Render,
		Cwd:        cwd,
		NoColor:    opts.NoColor,
		Version:    opts.Version,
		StateDir:   opts.StateDir,
		AutoUpdate: opts.AutoUpdate,
	}, raw)
	if err != nil {
		return fmt.Errorf("statusline: render: %w", err)
	}
	if _, err := fmt.Fprintln(out, rendered); err != nil {
		return fmt.Errorf("statusline: write: %w", err)
	}
	return nil
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
