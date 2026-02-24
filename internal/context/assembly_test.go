package context

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/git"
	"github.com/ponchione/agent-conductor/internal/models"
)

func TestAssembler_Assemble(t *testing.T) {
	// Setup temporary directory structure
	tmpDir, err := os.MkdirTemp("", "conductor-context-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create project structure
	modulePath := filepath.Join(tmpDir, "internal", "features")
	targetModule := filepath.Join(modulePath, "auth")
	sqlPath := filepath.Join(tmpDir, "sql")

	os.MkdirAll(targetModule, 0755)
	os.MkdirAll(sqlPath, 0755)

	// Create dummy files
	os.WriteFile(filepath.Join(targetModule, "handler.go"), []byte("package auth\nfunc Login() {}\n"), 0644)
	os.WriteFile(filepath.Join(sqlPath, "schema.sql"), []byte("CREATE TABLE users (id INT);\n"), 0644)

	// Init Git Repo
	gitMgr := git.New(&config.ProjectConfig{Project: config.Project{Path: tmpDir}})
	// We need a real git repo for GetRecentCommits to work without erroring,
	// or we mock it. Since GitManager uses go-git on disk, we should init a repo.
	// But let's just test file listing and structure first, maybe mock git if possible
	// or accept "no commits found".
	// The Assembler uses GitManager directly.

	// Create Config
	cfg := &config.ProjectConfig{
		Project: config.Project{
			Path:    tmpDir,
			DataDir: filepath.Join(tmpDir, "data"),
		},
		Conventions: config.Conventions{
			ModulePath: "internal/features",
			SQLPath:    "sql",
		},
	}

	// Re-init git mgr with the full config if needed, but it only needs Path for some ops?
	// Actually New() takes the config pointer.
	gitMgr = git.New(cfg)

	assembler := NewAssembler(cfg, gitMgr)

	wo := &models.WorkOrder{
		Title:        "Test Feature",
		TargetModule: "auth",
		Type:         "feature",
		KnownFiles:   []string{"README.md"},
	}

	ctx := context.Background()
	result, err := assembler.Assemble(ctx, wo)
	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}

	// Verify Output Content
	if !strings.Contains(result, "Title: Test Feature") {
		t.Error("Missing title")
	}
	if !strings.Contains(result, "Target module: auth") {
		t.Error("Missing target module")
	}
	// The dummy file 'handler.go' is in internal/features/auth
	if !strings.Contains(result, "internal/features/auth/handler.go") {
		t.Error("Missing repo-relative path for handler.go")
	}
	// The dummy file 'schema.sql' is in sql/
	if !strings.Contains(result, "schema.sql") {
		t.Error("Missing schema.sql listing")
	}
	// Git section might be empty or error message, that's fine for this unit test
}
