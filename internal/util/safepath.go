package util

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SafePath joins root and rel, resolves symlinks where possible, and verifies
// that the final path remains under root. Non-existent leaf paths are allowed
// as long as their nearest existing parent resolves under root.
func SafePath(root, rel string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf("resolve root symlinks: %w", err)
	}
	resolvedRoot = filepath.Clean(resolvedRoot)

	candidate, err := filepath.Abs(filepath.Join(absRoot, rel))
	if err != nil {
		return "", fmt.Errorf("resolve candidate: %w", err)
	}
	resolvedCandidate, err := resolvePathWithMissingSuffix(candidate)
	if err != nil {
		return "", err
	}
	if !pathWithinRoot(resolvedRoot, resolvedCandidate) {
		return "", fmt.Errorf("path %q escapes root %q", rel, root)
	}
	return resolvedCandidate, nil
}

func resolvePathWithMissingSuffix(path string) (string, error) {
	current := filepath.Clean(path)
	var suffix []string

	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved), nil
		}

		if info, statErr := os.Lstat(current); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("resolve path %q: %w", path, err)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("resolve path %q: %w", path, err)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("resolve path %q: %w", path, err)
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func pathWithinRoot(root, path string) bool {
	if path == root {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}
