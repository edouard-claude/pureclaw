package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFile_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	args, _ := json.Marshal(readFileArgs{Path: path})
	result := handleReadFile(context.Background(), args)

	if !result.Success {
		t.Fatalf("expected success=true, got false, error: %s", result.Error)
	}
	if result.Output != "hello world" {
		t.Errorf("expected output %q, got %q", "hello world", result.Output)
	}
}

func TestReadFile_NotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.txt")

	args, _ := json.Marshal(readFileArgs{Path: path})
	result := handleReadFile(context.Background(), args)

	if result.Success {
		t.Fatal("expected success=false for nonexistent file")
	}
	if !strings.Contains(result.Error, "no such file") {
		t.Errorf("expected error to contain 'no such file', got %q", result.Error)
	}
}

func TestReadFile_IsDirectory(t *testing.T) {
	dir := t.TempDir()

	args, _ := json.Marshal(readFileArgs{Path: dir})
	result := handleReadFile(context.Background(), args)

	if result.Success {
		t.Fatal("expected success=false for directory")
	}
	if !strings.Contains(result.Error, "is a directory") {
		t.Errorf("expected error to contain 'is a directory', got %q", result.Error)
	}
}

func TestReadFile_InvalidArgs(t *testing.T) {
	result := handleReadFile(context.Background(), json.RawMessage(`{invalid`))

	if result.Success {
		t.Fatal("expected success=false for invalid args")
	}
	if !strings.Contains(result.Error, "invalid arguments") {
		t.Errorf("expected error to contain 'invalid arguments', got %q", result.Error)
	}
}

func TestReadFile_EmptyPath(t *testing.T) {
	args, _ := json.Marshal(readFileArgs{Path: ""})
	result := handleReadFile(context.Background(), args)

	if result.Success {
		t.Fatal("expected success=false for empty path")
	}
	if !strings.Contains(result.Error, "path is required") {
		t.Errorf("expected error to contain 'path is required', got %q", result.Error)
	}
}

func TestReadFile_FileTooLarge(t *testing.T) {
	original := osStat
	osStat = func(name string) (os.FileInfo, error) {
		return fakeFileInfo{size: maxReadFileSize + 1}, nil
	}
	defer func() { osStat = original }()

	args, _ := json.Marshal(readFileArgs{Path: "/fake/large.bin"})
	result := handleReadFile(context.Background(), args)

	if result.Success {
		t.Fatal("expected success=false for oversized file")
	}
	if !strings.Contains(result.Error, "file too large") {
		t.Errorf("expected error to contain 'file too large', got %q", result.Error)
	}
}

func TestReadFile_StatError(t *testing.T) {
	original := osStat
	osStat = func(name string) (os.FileInfo, error) {
		return nil, os.ErrPermission
	}
	defer func() { osStat = original }()

	args, _ := json.Marshal(readFileArgs{Path: "/fake/file.txt"})
	result := handleReadFile(context.Background(), args)

	if result.Success {
		t.Fatal("expected success=false on stat error")
	}
	if !strings.Contains(result.Error, "permission denied") {
		t.Errorf("expected error to contain 'permission denied', got %q", result.Error)
	}
}

// fakeFileInfo implements os.FileInfo for testing size checks.
type fakeFileInfo struct {
	size int64
	os.FileInfo
}

func (f fakeFileInfo) Size() int64    { return f.size }
func (f fakeFileInfo) IsDir() bool    { return false }
func (f fakeFileInfo) Mode() os.FileMode { return 0o644 }

func TestReadFile_Definition(t *testing.T) {
	def := NewReadFile()

	if def.Name != "read_file" {
		t.Errorf("expected name %q, got %q", "read_file", def.Name)
	}
	if def.Description == "" {
		t.Error("expected non-empty description")
	}
	if def.Parameters == nil {
		t.Error("expected non-nil parameters")
	}
	if def.Handler == nil {
		t.Error("expected non-nil handler")
	}
}
