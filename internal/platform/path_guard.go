package platform

import (
	"errors"
	"path/filepath"
	"strings"
)

// ErrPathOutOfBounds is returned when a target path resolves outside the allowed root.
var ErrPathOutOfBounds = errors.New("path outside allowed root")

// Replaceable for testing.
var (
	filepathAbs          = filepath.Abs
	filepathEvalSymlinks = filepath.EvalSymlinks
)

// ValidatePath ensures target resolves within root. Returns ErrPathOutOfBounds otherwise.
func ValidatePath(root, target string) error {
	resolvedRoot, err := resolve(root)
	if err != nil {
		return err
	}

	resolvedTarget, err := resolve(target)
	if err != nil {
		// Target doesn't exist. Walk up to find the deepest existing ancestor,
		// resolve it, then re-append the remaining path segments.
		resolvedTarget, err = resolveNonexistent(target)
		if err != nil {
			return err
		}
	}

	// Add trailing separator to avoid "/root-other" matching "/root".
	prefix := resolvedRoot + string(filepath.Separator)
	if resolvedTarget == resolvedRoot || strings.HasPrefix(resolvedTarget, prefix) {
		return nil
	}
	return ErrPathOutOfBounds
}

// resolve returns the absolute, symlink-resolved, cleaned path.
func resolve(path string) (string, error) {
	abs, err := filepathAbs(path)
	if err != nil {
		return "", err
	}
	real, err := filepathEvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return filepath.Clean(real), nil
}

// resolveNonexistent resolves a path that may not exist by walking up to
// find the deepest existing ancestor, resolving it, then re-appending
// the remaining segments.
func resolveNonexistent(target string) (string, error) {
	abs, err := filepathAbs(target)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)

	// Walk up until we find an existing directory.
	current := abs
	var tail []string
	for {
		resolved, err := filepathEvalSymlinks(current)
		if err == nil {
			// Found existing ancestor — append remaining segments.
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return filepath.Clean(resolved), nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			// Reached filesystem root without finding existing path.
			return abs, nil
		}
		tail = append(tail, filepath.Base(current))
		current = parent
	}
}
