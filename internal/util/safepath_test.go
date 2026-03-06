package util

import (
	"path/filepath"
	"testing"
)

func TestSafePath(t *testing.T) {
	root := "/project/repo"

	tests := []struct {
		name    string
		rel     string
		wantErr bool
	}{
		{"simple relative", "src/main.go", false},
		{"nested path", "internal/pkg/foo.go", false},
		{"dot path", "./src/main.go", false},
		{"traversal escape", "../../../etc/passwd", true},
		{"hidden traversal", "src/../../etc/passwd", true},
		{"root itself", ".", false},
		{"empty rel", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafePath(root, tt.rel)
			if tt.wantErr {
				if err == nil {
					t.Errorf("SafePath(%q, %q) = %q, want error", root, tt.rel, got)
				}
				return
			}
			if err != nil {
				t.Errorf("SafePath(%q, %q) unexpected error: %v", root, tt.rel, err)
				return
			}
			abs, _ := filepath.Abs(root)
			if got != abs && got != filepath.Join(abs, tt.rel) {
				// Just verify it stays under root
				if got[:len(abs)] != abs {
					t.Errorf("SafePath(%q, %q) = %q, not under root %q", root, tt.rel, got, abs)
				}
			}
		})
	}
}
