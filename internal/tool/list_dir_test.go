package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListDir_Success(t *testing.T) {
	dir := t.TempDir()

	// Create a file.
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create a subdirectory.
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	args, _ := json.Marshal(listDirArgs{Path: dir})
	result := handleListDir(context.Background(), args)

	if !result.Success {
		t.Fatalf("expected success=true, got false, error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "file.txt (file)") {
		t.Errorf("expected output to contain 'file.txt (file)', got %q", result.Output)
	}
	if !strings.Contains(result.Output, "subdir/ (dir)") {
		t.Errorf("expected output to contain 'subdir/ (dir)', got %q", result.Output)
	}
}

func TestListDir_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	args, _ := json.Marshal(listDirArgs{Path: dir})
	result := handleListDir(context.Background(), args)

	if !result.Success {
		t.Fatalf("expected success=true, got false, error: %s", result.Error)
	}
	if result.Output != "" {
		t.Errorf("expected empty output for empty dir, got %q", result.Output)
	}
}

func TestListDir_NotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent")

	args, _ := json.Marshal(listDirArgs{Path: path})
	result := handleListDir(context.Background(), args)

	if result.Success {
		t.Fatal("expected success=false for nonexistent dir")
	}
	if !strings.Contains(result.Error, "no such file") {
		t.Errorf("expected error to contain 'no such file', got %q", result.Error)
	}
}

func TestListDir_IsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	args, _ := json.Marshal(listDirArgs{Path: path})
	result := handleListDir(context.Background(), args)

	if result.Success {
		t.Fatal("expected success=false for file path")
	}
	if !strings.Contains(result.Error, "not a directory") {
		t.Errorf("expected error to contain 'not a directory', got %q", result.Error)
	}
}

func TestListDir_InvalidArgs(t *testing.T) {
	result := handleListDir(context.Background(), json.RawMessage(`{invalid`))

	if result.Success {
		t.Fatal("expected success=false for invalid args")
	}
	if !strings.Contains(result.Error, "invalid arguments") {
		t.Errorf("expected error to contain 'invalid arguments', got %q", result.Error)
	}
}

func TestListDir_EmptyPath(t *testing.T) {
	args, _ := json.Marshal(listDirArgs{Path: ""})
	result := handleListDir(context.Background(), args)

	if result.Success {
		t.Fatal("expected success=false for empty path")
	}
	if !strings.Contains(result.Error, "path is required") {
		t.Errorf("expected error to contain 'path is required', got %q", result.Error)
	}
}

func TestListDir_Definition(t *testing.T) {
	def := NewListDir()

	if def.Name != "list_dir" {
		t.Errorf("expected name %q, got %q", "list_dir", def.Name)
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

func TestListDir_Symlink(t *testing.T) {
	dir := t.TempDir()

	// Create a target file.
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create a symlink.
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	args, _ := json.Marshal(listDirArgs{Path: dir})
	result := handleListDir(context.Background(), args)

	if !result.Success {
		t.Fatalf("expected success=true, got false, error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "link.txt") {
		t.Errorf("expected output to contain 'link.txt', got %q", result.Output)
	}
	if !strings.Contains(result.Output, "symlink") {
		t.Errorf("expected output to contain 'symlink', got %q", result.Output)
	}
}

func TestListDir_SymlinkReadlinkError(t *testing.T) {
	dir := t.TempDir()

	// Create a target file and symlink.
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	// Make Readlink fail.
	original := osReadlink
	osReadlink = func(name string) (string, error) {
		return "", os.ErrPermission
	}
	defer func() { osReadlink = original }()

	args, _ := json.Marshal(listDirArgs{Path: dir})
	result := handleListDir(context.Background(), args)

	if !result.Success {
		t.Fatalf("expected success=true, got false, error: %s", result.Error)
	}
	// Should fall back to "name (symlink)" without target.
	if !strings.Contains(result.Output, "link.txt (symlink)") {
		t.Errorf("expected output to contain 'link.txt (symlink)', got %q", result.Output)
	}
	// Should NOT contain the arrow notation.
	if strings.Contains(result.Output, "->") {
		t.Errorf("expected output to NOT contain '->' on readlink error, got %q", result.Output)
	}
}

func TestListDir_ReadDirError(t *testing.T) {
	original := osReadDir
	osReadDir = func(name string) ([]os.DirEntry, error) {
		return nil, os.ErrPermission
	}
	defer func() { osReadDir = original }()

	args, _ := json.Marshal(listDirArgs{Path: "/some/dir"})
	result := handleListDir(context.Background(), args)

	if result.Success {
		t.Fatal("expected success=false on readdir error")
	}
	if !strings.Contains(result.Error, "permission denied") {
		t.Errorf("expected error to contain 'permission denied', got %q", result.Error)
	}
}
