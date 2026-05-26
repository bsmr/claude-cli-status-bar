// Package fileutil provides shared filesystem helpers used across ccsb's
// persistent state writers. Today: WriteAtomic — temp-file + rename with
// uniform 0o700 dir / 0o600 file permissions, matching the user-private
// state convention used for config, settings backup, schema-version, and
// capture files.
package fileutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	dirPerm  os.FileMode = 0o700
	filePerm os.FileMode = 0o600
)

// WriteAtomic writes data to path via temp-file + rename so readers never
// see a partial file. The parent directory is created with 0o700 if it
// does not exist; the final file lands as 0o600. The temp file is placed
// in the same directory as path so the rename stays inside one filesystem.
func WriteAtomic(path string, data []byte) error {
	if path == "" {
		return errors.New("fileutil: empty path")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("fileutil: mkdir %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("fileutil: create temp: %w", err)
	}
	tmpPath := tmp.Name()

	cleanup := func() { _ = os.Remove(tmpPath) }

	if err := os.Chmod(tmpPath, filePerm); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("fileutil: chmod: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("fileutil: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("fileutil: close temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("fileutil: rename: %w", err)
	}
	return nil
}
