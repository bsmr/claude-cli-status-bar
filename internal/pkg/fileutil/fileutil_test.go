package fileutil

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAtomic_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	data := []byte("hello world\n")

	if err := WriteAtomic(path, data); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("content: got %q, want %q", got, data)
	}
}

func TestWriteAtomic_EmptyPath(t *testing.T) {
	err := WriteAtomic("", []byte("x"))
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	if !strings.Contains(err.Error(), "empty path") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWriteAtomic_CreatesMissingParent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing", "nested", "out.txt")
	data := []byte("payload")

	if err := WriteAtomic(path, data); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not present: %v", err)
	}

	// Parent dir should be 0o700.
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat parent: %v", err)
	}
	if got := info.Mode().Perm(); got != dirPerm {
		t.Errorf("parent dir perm: got %o, want %o", got, dirPerm)
	}
}

func TestWriteAtomic_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	if err := WriteAtomic(path, []byte("x")); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != filePerm {
		t.Errorf("file perm: got %o, want %o", got, filePerm)
	}
}

func TestWriteAtomic_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := WriteAtomic(path, []byte("new")); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "new" {
		t.Errorf("content: got %q, want %q", got, "new")
	}
}

func TestWriteAtomic_NoTempLeftBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	if err := WriteAtomic(path, []byte("x")); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func TestWriteAtomic_DirCreationFails(t *testing.T) {
	// Create a regular file where the parent dir would need to be created.
	// MkdirAll will fail because a non-dir already occupies the path.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("not-a-dir"), 0o600); err != nil {
		t.Fatalf("seed blocker: %v", err)
	}
	path := filepath.Join(blocker, "nested", "out.txt")

	err := WriteAtomic(path, []byte("x"))
	if err == nil {
		t.Fatal("expected error when parent path is a file")
	}
	if !errors.Is(err, os.ErrExist) && !strings.Contains(err.Error(), "mkdir") {
		t.Errorf("unexpected error shape: %v", err)
	}
}
