package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFile_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.txt")

	args, _ := json.Marshal(writeFileArgs{Path: path, Content: "hello world"})
	result := handleWriteFile(context.Background(), args)

	if !result.Success {
		t.Fatalf("expected success=true, got false, error: %s", result.Error)
	}
	if !strings.Contains(result.Output, path) {
		t.Errorf("expected output to contain path %q, got %q", path, result.Output)
	}

	// Verify file content.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("expected file content %q, got %q", "hello world", string(data))
	}
}

func TestWriteFile_CreatesDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "deep.txt")

	args, _ := json.Marshal(writeFileArgs{Path: path, Content: "nested"})
	result := handleWriteFile(context.Background(), args)

	if !result.Success {
		t.Fatalf("expected success=true, got false, error: %s", result.Error)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(data) != "nested" {
		t.Errorf("expected file content %q, got %q", "nested", string(data))
	}
}

func TestWriteFile_OverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")

	// First write.
	args1, _ := json.Marshal(writeFileArgs{Path: path, Content: "first"})
	result1 := handleWriteFile(context.Background(), args1)
	if !result1.Success {
		t.Fatalf("first write failed: %s", result1.Error)
	}

	// Second write (overwrite).
	args2, _ := json.Marshal(writeFileArgs{Path: path, Content: "second"})
	result2 := handleWriteFile(context.Background(), args2)
	if !result2.Success {
		t.Fatalf("second write failed: %s", result2.Error)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(data) != "second" {
		t.Errorf("expected %q, got %q", "second", string(data))
	}
}

func TestWriteFile_InvalidArgs(t *testing.T) {
	result := handleWriteFile(context.Background(), json.RawMessage(`{invalid`))

	if result.Success {
		t.Fatal("expected success=false for invalid args")
	}
	if !strings.Contains(result.Error, "invalid arguments") {
		t.Errorf("expected error to contain 'invalid arguments', got %q", result.Error)
	}
}

func TestWriteFile_EmptyContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")

	args, _ := json.Marshal(writeFileArgs{Path: path, Content: ""})
	result := handleWriteFile(context.Background(), args)

	if !result.Success {
		t.Fatalf("expected success=true for empty content, got false, error: %s", result.Error)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty file, got %d bytes", len(data))
	}
}

func TestWriteFile_EmptyPath(t *testing.T) {
	args, _ := json.Marshal(writeFileArgs{Path: "", Content: "data"})
	result := handleWriteFile(context.Background(), args)

	if result.Success {
		t.Fatal("expected success=false for empty path")
	}
	if !strings.Contains(result.Error, "path is required") {
		t.Errorf("expected error to contain 'path is required', got %q", result.Error)
	}
}

func TestWriteFile_Definition(t *testing.T) {
	def := NewWriteFile()

	if def.Name != "write_file" {
		t.Errorf("expected name %q, got %q", "write_file", def.Name)
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

func TestWriteFile_MkdirAllError(t *testing.T) {
	original := osMkdirAll
	osMkdirAll = func(path string, perm os.FileMode) error {
		return os.ErrPermission
	}
	defer func() { osMkdirAll = original }()

	args, _ := json.Marshal(writeFileArgs{Path: "/fake/path/file.txt", Content: "data"})
	result := handleWriteFile(context.Background(), args)

	if result.Success {
		t.Fatal("expected success=false on mkdir error")
	}
	if !strings.Contains(result.Error, "permission denied") {
		t.Errorf("expected error to contain 'permission denied', got %q", result.Error)
	}
}

func TestWriteFile_AtomicWriteError(t *testing.T) {
	original := atomicWrite
	atomicWrite = func(path string, data []byte, perm os.FileMode) error {
		return os.ErrPermission
	}
	defer func() { atomicWrite = original }()

	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	args, _ := json.Marshal(writeFileArgs{Path: path, Content: "data"})
	result := handleWriteFile(context.Background(), args)

	if result.Success {
		t.Fatal("expected success=false on atomic write error")
	}
	if !strings.Contains(result.Error, "permission denied") {
		t.Errorf("expected error to contain 'permission denied', got %q", result.Error)
	}
}
