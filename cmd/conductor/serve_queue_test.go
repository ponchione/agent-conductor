package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/git"
)

func TestBuildServeQueueCallbacksWithoutConfig(t *testing.T) {
	t.Parallel()

	executeFn, monitorFn := buildServeQueueCallbacks(nil, nil, git.New(nil), "")
	if executeFn != nil || monitorFn != nil {
		t.Fatalf("buildServeQueueCallbacks() = (%v, %v), want (nil, nil)", executeFn, monitorFn)
	}
}

func TestBuildServeQueueCallbacksWithConfig(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "db", "conductor.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		t.Fatalf("MkdirAll(db dir) error: %v", err)
	}
	db, err := database.NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := &config.ProjectConfig{
		Project: config.Project{
			Name:    "serve-test",
			Path:    t.TempDir(),
			DataDir: dataDir,
		},
	}

	executeFn, monitorFn := buildServeQueueCallbacks(db, cfg, git.New(nil), filepath.Join(dataDir, "work-orders"))
	if executeFn == nil || monitorFn == nil {
		t.Fatalf("buildServeQueueCallbacks() returned nil callbacks: execute=%v monitor=%v", executeFn, monitorFn)
	}
}
