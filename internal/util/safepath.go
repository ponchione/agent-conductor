package util

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SafePath joins root and rel, cleans the result, and verifies that it remains
// under root. Returns an error if the resolved path escapes root.
func SafePath(root, rel string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	joined := filepath.Join(absRoot, rel)
	cleaned := filepath.Clean(joined)
	if !strings.HasPrefix(cleaned, absRoot+string(filepath.Separator)) && cleaned != absRoot {
		return "", fmt.Errorf("path %q escapes root %q", rel, root)
	}
	return cleaned, nil
}
