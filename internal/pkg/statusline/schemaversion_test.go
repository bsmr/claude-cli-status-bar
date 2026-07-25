package statusline

import (
	"strings"
	"testing"
)

// The schema-version helpers are unexported, so this file is an in-package
// test — statusline_test.go covers the rest of the package from the outside.

func TestSaveSchemaVersion_EmptyPathReturnsError(t *testing.T) {
	err := saveSchemaVersion("", "1.2.3")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("unexpected error: %v", err)
	}
}
