package fileutil

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// TestWriteAtomic_ConcurrentWritersLeaveValidFile exercises the atomicity
// guarantee under contention — run with `go test -race`. Many goroutines write
// distinct payloads to the SAME path at once (the git_dirty cache is written
// this way by refreshers from multiple Claude sessions). Each payload is a
// multi-KB run (> one page) of a single per-writer byte and a distinct length,
// so a non-atomic in-place write would betray itself as a mixed (torn) or
// short file. The temp+rename design must instead leave the final file
// HOMOGENEOUS and equal to exactly one writer's COMPLETE payload, and leak no
// .tmp files.
func TestWriteAtomic_ConcurrentWritersLeaveValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	const writers = 40
	payloads := make([][]byte, writers)
	lenOf := make(map[byte]int, writers)
	for i := range writers {
		b := byte('A' + i)
		p := bytes.Repeat([]byte{b}, 4096+i*97) // > page size, variable length
		payloads[i] = p
		lenOf[b] = len(p)
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range writers {
		wg.Go(func() {
			<-start // maximise contention
			if err := WriteAtomic(path, payloads[i]); err != nil {
				t.Errorf("writer %d: %v", i, err)
			}
		})
	}
	close(start)
	wg.Wait()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("final file is empty")
	}
	// Homogeneous: every byte equals the first (no interleaving of two writers).
	first := got[0]
	for i, b := range got {
		if b != first {
			t.Fatalf("torn write: byte %d is %q but the file starts with %q", i, b, first)
		}
	}
	// Complete: the length matches that writer's payload exactly (no short write).
	if want := lenOf[first]; len(got) != want {
		t.Errorf("short/long write: %d bytes of %q, want %d", len(got), first, want)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file left behind under contention: %s", e.Name())
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
