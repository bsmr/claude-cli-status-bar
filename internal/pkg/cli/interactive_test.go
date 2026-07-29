package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStdinIsTTY_PipeIsNotATerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()

	if stdinIsTTY(r) {
		t.Error("a pipe must not be reported as a terminal — this is the path Claude Code uses")
	}
}

func TestStdinIsTTY_RegularFileIsNotATerminal(t *testing.T) {
	name := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(name, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	f, err := os.Open(name)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	if stdinIsTTY(f) {
		t.Error("a regular file must not be reported as a terminal — `ccsb < capture.json` has to render")
	}
}

// TestStdinIsTTY_CharDeviceIsATerminal pins the documented imprecision: the
// check is "character device", so os.DevNull counts as a terminal. Nothing
// invokes ccsb that way, and distinguishing the two would need a real ioctl
// probe plus a Windows stub.
func TestStdinIsTTY_CharDeviceIsATerminal(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Skipf("cannot open %s: %v", os.DevNull, err)
	}
	defer f.Close()

	if !stdinIsTTY(f) {
		t.Errorf("%s is a character device and must be reported as a terminal", os.DevNull)
	}
}

// TestStdinIsTTY_UnstattableIsNotATerminal pins the safe direction of the
// error case: when the descriptor cannot be inspected, ccsb renders rather
// than printing usage. The opposite default would risk writing help text into
// the status bar.
func TestStdinIsTTY_UnstattableIsNotATerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	w.Close()
	r.Close() // Stat on a closed descriptor fails.

	if stdinIsTTY(r) {
		t.Error("a descriptor that cannot be stat'ed must not be reported as a terminal")
	}
}

// TestNewFromOS_StdinIsTTYReflectsSwappedStdin pins the NewFromOS wiring:
// the StdinIsTTY field must actually come from stdinIsTTY(os.Stdin), not
// just exist on the struct with its zero value. That distinction is why
// this checks BOTH directions rather than only the pipe: dropping
// `StdinIsTTY: stdinIsTTY(os.Stdin)` from the Flags literal leaves the
// field at its zero value, false — which happens to equal the pipe
// expectation, so a pipe-only assertion would pass even with the wiring
// deleted. Swapping in an explicit character device (never relying on
// whatever os.Stdin happens to be inherited as under `go test`, which is
// not guaranteed across environments) and asserting true is what actually
// catches that mutation. os.Stdin is a package-level var and this
// package's tests never call t.Parallel, so a temporary swap is safe.
func TestNewFromOS_StdinIsTTYReflectsSwappedStdin(t *testing.T) {
	orig := os.Stdin
	defer func() { os.Stdin = orig }()

	t.Run("pipe is not a terminal", func(t *testing.T) {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("os.Pipe: %v", err)
		}
		defer r.Close()
		defer w.Close()

		os.Stdin = r
		_, flags, err := NewFromOS()
		if err != nil {
			t.Fatalf("NewFromOS: %v", err)
		}
		if flags.StdinIsTTY {
			t.Error("NewFromOS must resolve StdinIsTTY from os.Stdin — a pipe must not be reported as a terminal")
		}
	})

	t.Run("character device is a terminal", func(t *testing.T) {
		f, err := os.Open(os.DevNull)
		if err != nil {
			t.Skipf("cannot open %s: %v", os.DevNull, err)
		}
		defer f.Close()

		os.Stdin = f
		_, flags, err := NewFromOS()
		if err != nil {
			t.Fatalf("NewFromOS: %v", err)
		}
		if !flags.StdinIsTTY {
			t.Error("NewFromOS must resolve StdinIsTTY from os.Stdin — a character device must be reported as a terminal")
		}
	})
}

func TestRun_SubcommandOnTerminalStillDispatches(t *testing.T) {
	var out, errOut bytes.Buffer

	// StdinIsTTY: true mimics a person's shell — Flags carries it regardless
	// of args. The dispatch must still run "version" rather than printing
	// usage: the interactive short-circuit is scoped to len(args) == 0 only.
	if err := Run(context.Background(), Paths{}, Flags{StdinIsTTY: true}, []string{"version"}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "ccsb version") {
		t.Errorf("expected the version subcommand to run, got:\n%s", got)
	}
	if strings.Contains(got, "Subcommands:") {
		t.Errorf("a subcommand call must dispatch, not fall back to usage, got:\n%s", got)
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr must stay empty, got %q", errOut.String())
	}
}

func TestRun_NoArgsOnTerminalPrintsHintAndUsage(t *testing.T) {
	var out, errOut bytes.Buffer

	// Paths is deliberately zero: the interactive path must answer without a
	// config or a state directory. stdin is an empty reader rather than nil
	// because a nil stdin would panic inside io.LimitReader on the RED run,
	// and a panic is a useless RED — it aborts the rest of the package's tests.
	if err := Run(context.Background(), Paths{}, Flags{StdinIsTTY: true}, nil, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"stdin is a terminal",
		"ccsb - Claude Code statusLine provider",
		"Subcommands:",
		"install",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("stdout is missing %q\ngot:\n%s", want, got)
		}
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr must stay empty, got %q", errOut.String())
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("no status bar must be rendered, but stdout carries ANSI:\n%q", got)
	}
}

func TestRun_NoArgsWithPipedPayloadStillRenders(t *testing.T) {
	dir := t.TempDir()
	p := Paths{
		Config:  filepath.Join(dir, "config.json"),
		Capture: filepath.Join(dir, "captures"),
		State:   dir,
	}
	const payload = `{"session_id":"x","model":{"display_name":"Sonnet"},"workspace":{"current_dir":"/tmp"}}`

	var out, errOut bytes.Buffer
	if err := Run(context.Background(), p, Flags{}, nil, strings.NewReader(payload), &out, &errOut); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Sonnet") {
		t.Errorf("expected a rendered bar naming the model, got:\n%s", got)
	}
	if strings.Contains(got, "Subcommands:") {
		t.Errorf("usage must not be printed when stdin carries a payload, got:\n%s", got)
	}
}
