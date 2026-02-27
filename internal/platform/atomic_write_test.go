package platform

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtomicWrite_success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	data := []byte("hello world")

	if err := AtomicWrite(path, data, 0644); err != nil {
		t.Fatalf("AtomicWrite failed: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("got %q, want %q", got, data)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Mode().Perm() != 0644 {
		t.Fatalf("permissions = %o, want 0644", info.Mode().Perm())
	}
}

func TestAtomicWrite_overwriteExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	if err := AtomicWrite(path, []byte("first"), 0644); err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	if err := AtomicWrite(path, []byte("second"), 0600); err != nil {
		t.Fatalf("second write failed: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "second" {
		t.Fatalf("got %q, want %q", got, "second")
	}
}

func TestAtomicWrite_emptyData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")

	if err := AtomicWrite(path, []byte{}, 0644); err != nil {
		t.Fatalf("AtomicWrite with empty data failed: %v", err)
	}

	got, _ := os.ReadFile(path)
	if len(got) != 0 {
		t.Fatalf("expected empty file, got %d bytes", len(got))
	}
}

func TestAtomicWrite_createTempError(t *testing.T) {
	err := AtomicWrite("/nonexistent-dir-xyz/file.txt", []byte("data"), 0644)
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
	if !strings.Contains(err.Error(), "create temp") {
		t.Fatalf("expected 'create temp' error, got: %v", err)
	}
}

func TestAtomicWrite_writeError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	// Return a file with a closed fd so Write fails.
	orig := osCreateTemp
	osCreateTemp = func(d, pattern string) (*os.File, error) {
		f, err := orig(d, pattern)
		if err != nil {
			return nil, err
		}
		f.Close() // Close fd; Write will fail.
		return f, nil
	}
	t.Cleanup(func() { osCreateTemp = orig })

	err := AtomicWrite(path, []byte("data"), 0644)
	if err == nil {
		t.Fatal("expected write error")
	}
	if !strings.Contains(err.Error(), "write") {
		t.Fatalf("expected 'write' in error, got: %v", err)
	}
}

func TestAtomicWrite_closeError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	// Inject fileClose to return an error while the real close succeeds.
	orig := fileClose
	fileClose = func(f *os.File) error {
		f.Close()
		return errors.New("close injected error")
	}
	t.Cleanup(func() { fileClose = orig })

	err := AtomicWrite(path, []byte("data"), 0644)
	if err == nil {
		t.Fatal("expected close error")
	}
	if !strings.Contains(err.Error(), "close") {
		t.Fatalf("expected 'close' in error, got: %v", err)
	}
}

func TestAtomicWrite_chmodError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	orig := osChmod
	osChmod = func(string, os.FileMode) error {
		return errors.New("chmod injected error")
	}
	t.Cleanup(func() { osChmod = orig })

	err := AtomicWrite(path, []byte("data"), 0644)
	if err == nil {
		t.Fatal("expected chmod error")
	}
	if !strings.Contains(err.Error(), "chmod") {
		t.Fatalf("expected 'chmod' in error, got: %v", err)
	}

	// Temp file should be cleaned up.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".pureclaw-tmp-") {
			t.Errorf("temp file not cleaned up: %s", e.Name())
		}
	}
}

func TestAtomicWrite_renameError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	orig := osRename
	osRename = func(string, string) error {
		return errors.New("rename injected error")
	}
	t.Cleanup(func() { osRename = orig })

	err := AtomicWrite(path, []byte("data"), 0644)
	if err == nil {
		t.Fatal("expected rename error")
	}
	if !strings.Contains(err.Error(), "rename") {
		t.Fatalf("expected 'rename' in error, got: %v", err)
	}

	// Temp file should be cleaned up.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".pureclaw-tmp-") {
			t.Errorf("temp file not cleaned up: %s", e.Name())
		}
	}
}

func TestAtomicWrite_noPartialWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	if err := AtomicWrite(path, []byte("original"), 0644); err != nil {
		t.Fatalf("initial write failed: %v", err)
	}

	orig := osRename
	osRename = func(string, string) error {
		return errors.New("rename fail")
	}
	t.Cleanup(func() { osRename = orig })

	_ = AtomicWrite(path, []byte("corrupted"), 0644)

	got, _ := os.ReadFile(path)
	if string(got) != "original" {
		t.Fatalf("original file corrupted: got %q", got)
	}
}
