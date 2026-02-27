package platform

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// Replaceable for testing error paths.
var (
	osCreateTemp = os.CreateTemp
	osChmod      = os.Chmod
	osRename     = os.Rename
	fileClose    = func(f *os.File) error { return f.Close() }
)

// AtomicWrite writes data to path atomically via temp file + rename.
// The temp file is created in the same directory to ensure same filesystem.
func AtomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)

	tmp, err := osCreateTemp(dir, ".pureclaw-tmp-*")
	if err != nil {
		return fmt.Errorf("atomic write: create temp: %w", err)
	}
	tmpName := tmp.Name()

	// Clean up temp file on any error.
	success := false
	defer func() {
		if !success {
			os.Remove(tmpName)
		}
	}()

	// Write and close: collect both errors so close always runs.
	_, writeErr := tmp.Write(data)
	closeErr := fileClose(tmp)
	if writeErr != nil {
		return fmt.Errorf("atomic write: write: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("atomic write: close: %w", closeErr)
	}

	if err := osChmod(tmpName, perm); err != nil {
		return fmt.Errorf("atomic write: chmod: %w", err)
	}
	if err := osRename(tmpName, path); err != nil {
		return fmt.Errorf("atomic write: rename: %w", err)
	}

	success = true
	slog.Info("file written", "component", "platform", "operation", "atomic_write", "path", path)
	return nil
}
