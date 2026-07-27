package cli

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestUpdateSubcommandIsDispatched(t *testing.T) {
	// A local build (the test binary always is one) must refuse, which
	// proves the subcommand reached selfupdate rather than the unknown
	// subcommand branch.
	var out, errOut strings.Builder
	err := Run(context.Background(), Paths{State: t.TempDir(), Self: "/nonexistent/ccsb"},
		Flags{}, []string{"update"}, strings.NewReader(""), &out, &errOut)
	if err == nil {
		t.Fatal("err = nil, want a refusal")
	}
	var unknown *UnknownSubcommandError
	if errors.As(err, &unknown) {
		t.Fatalf("update was not dispatched: %v", err)
	}
}

func TestHelpMentionsUpdate(t *testing.T) {
	var out strings.Builder
	if err := Run(context.Background(), Paths{}, Flags{}, []string{"help"},
		strings.NewReader(""), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "update") {
		t.Error("help output does not mention the update subcommand")
	}
}
