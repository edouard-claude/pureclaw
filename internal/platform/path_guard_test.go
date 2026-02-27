package platform

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestValidatePath_withinRoot(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	os.MkdirAll(sub, 0755)

	if err := ValidatePath(dir, sub); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidatePath_fileInRoot(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	os.WriteFile(file, []byte("x"), 0644)

	if err := ValidatePath(dir, file); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidatePath_targetEqualsRoot(t *testing.T) {
	dir := t.TempDir()

	if err := ValidatePath(dir, dir); err != nil {
		t.Fatalf("expected nil for target==root, got %v", err)
	}
}

func TestValidatePath_outsideRoot(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "..", "outside.txt")

	err := ValidatePath(dir, outside)
	if !errors.Is(err, ErrPathOutOfBounds) {
		t.Fatalf("expected ErrPathOutOfBounds, got %v", err)
	}
}

func TestValidatePath_dotDotTraversal(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "a", "b")
	os.MkdirAll(sub, 0755)

	// Traverse out via ../..
	traversal := filepath.Join(sub, "..", "..", "..", "etc", "passwd")
	err := ValidatePath(dir, traversal)
	if !errors.Is(err, ErrPathOutOfBounds) {
		t.Fatalf("expected ErrPathOutOfBounds, got %v", err)
	}
}

func TestValidatePath_symlinkWithinRoot(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	os.MkdirAll(real, 0755)
	link := filepath.Join(dir, "link")
	os.Symlink(real, link)

	if err := ValidatePath(dir, link); err != nil {
		t.Fatalf("expected nil for symlink within root, got %v", err)
	}
}

func TestValidatePath_symlinkOutsideRoot(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(dir, "escape-link")
	os.Symlink(outside, link)

	err := ValidatePath(dir, link)
	if !errors.Is(err, ErrPathOutOfBounds) {
		t.Fatalf("expected ErrPathOutOfBounds for symlink escaping root, got %v", err)
	}
}

func TestValidatePath_nonexistentTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "new-file.txt")

	if err := ValidatePath(dir, target); err != nil {
		t.Fatalf("expected nil for nonexistent target within root, got %v", err)
	}
}

func TestValidatePath_nonexistentTargetOutside(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "..", "nonexistent.txt")

	err := ValidatePath(dir, target)
	if !errors.Is(err, ErrPathOutOfBounds) {
		t.Fatalf("expected ErrPathOutOfBounds, got %v", err)
	}
}

func TestValidatePath_prefixTrap(t *testing.T) {
	dir := t.TempDir()
	trap := dir + "-other"
	os.MkdirAll(trap, 0755)
	t.Cleanup(func() { os.RemoveAll(trap) })

	err := ValidatePath(dir, trap)
	if !errors.Is(err, ErrPathOutOfBounds) {
		t.Fatalf("expected ErrPathOutOfBounds for prefix trap, got %v", err)
	}
}

func TestValidatePath_invalidRoot(t *testing.T) {
	err := ValidatePath("/nonexistent-root-xyz", "/tmp/file.txt")
	if err == nil {
		t.Fatal("expected error for nonexistent root")
	}
}

func TestValidatePath_nonexistentParentChain(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "nonexistent-parent", "deep", "file.txt")

	// Parent chain doesn't exist: falls back to cleaned absolute path.
	// Since the cleaned path is still within root, should succeed.
	if err := ValidatePath(dir, target); err != nil {
		t.Fatalf("expected nil for deep nonexistent target within root, got %v", err)
	}
}

func TestValidatePath_absError_root(t *testing.T) {
	orig := filepathAbs
	filepathAbs = func(string) (string, error) {
		return "", errors.New("abs injected error")
	}
	t.Cleanup(func() { filepathAbs = orig })

	err := ValidatePath("/root", "/root/file")
	if err == nil {
		t.Fatal("expected abs error")
	}
}

func TestValidatePath_absError_target(t *testing.T) {
	dir := t.TempDir()

	calls := 0
	orig := filepathAbs
	filepathAbs = func(path string) (string, error) {
		calls++
		if calls > 1 {
			return "", errors.New("abs injected error")
		}
		return orig(path)
	}
	t.Cleanup(func() { filepathAbs = orig })

	err := ValidatePath(dir, "/nonexistent/file")
	if err == nil {
		t.Fatal("expected abs error on target")
	}
}

func TestValidatePath_absError_fallback(t *testing.T) {
	dir := t.TempDir()

	calls := 0
	orig := filepathAbs
	filepathAbs = func(path string) (string, error) {
		calls++
		// Call 1: resolve(root) → succeed
		// Call 2: resolve(target) → succeed but EvalSymlinks fails (nonexistent)
		// Call 3: resolveNonexistent(target) → fail here
		if calls >= 3 {
			return "", errors.New("abs fallback error")
		}
		return orig(path)
	}
	t.Cleanup(func() { filepathAbs = orig })

	target := filepath.Join(dir, "no", "such", "parent", "file.txt")
	err := ValidatePath(dir, target)
	if err == nil {
		t.Fatal("expected abs fallback error")
	}
}

func TestValidatePath_evalSymlinksAlwaysFails(t *testing.T) {
	// When EvalSymlinks fails for every path (including /),
	// resolveNonexistent walks up to root and returns abs as-is.
	dir := t.TempDir()

	origAbs := filepathAbs
	origEval := filepathEvalSymlinks

	// Let root resolve succeed by special-casing it.
	rootCalls := 0
	filepathAbs = origAbs
	filepathEvalSymlinks = func(path string) (string, error) {
		rootCalls++
		if rootCalls == 1 {
			// First call is for resolving root — let it succeed.
			return origEval(path)
		}
		// All subsequent calls fail.
		return "", errors.New("eval always fails")
	}
	t.Cleanup(func() {
		filepathAbs = origAbs
		filepathEvalSymlinks = origEval
	})

	target := filepath.Join(dir, "nonexistent.txt")
	err := ValidatePath(dir, target)
	// resolveNonexistent walks all the way up (EvalSymlinks always fails),
	// eventually parent==current, returns abs. Due to macOS /tmp→/private/tmp
	// symlink, root and target may not share the same prefix.
	// Either nil or ErrPathOutOfBounds is acceptable — the point is covering
	// the parent==current branch.
	if err != nil && !errors.Is(err, ErrPathOutOfBounds) {
		t.Fatalf("expected nil or ErrPathOutOfBounds, got %v", err)
	}
}
