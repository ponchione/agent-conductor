package worker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponchione/agent-conductor/internal/models"
)

func TestValidateScopeOutputRejectsSymlinkEscape(t *testing.T) {
	repoRoot := t.TempDir()
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	if err := os.Symlink(outsideDir, filepath.Join(repoRoot, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	pkg := &models.ContextPackage{
		FilesToModify: []models.FileRef{{
			Path:   filepath.Join("escape", "secret.txt"),
			Reason: "test symlink escape",
		}},
	}

	result := validateScopeOutput(repoRoot, pkg)
	if result.err == nil {
		t.Fatal("validateScopeOutput() error = nil, want symlink escape error")
	}
	if !strings.Contains(result.err.Error(), "path traversal") && !strings.Contains(result.err.Error(), "escapes root") {
		t.Fatalf("validateScopeOutput() error = %q, want path traversal/escape error", result.err)
	}
}
