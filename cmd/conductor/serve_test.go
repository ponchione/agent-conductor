package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveServeDBPath_UsesExplicitDB(t *testing.T) {
	restore := snapshotServeGlobals(t)
	defer restore()

	dbPath := filepath.Join(t.TempDir(), "conductor.db")
	if err := os.WriteFile(dbPath, []byte("test"), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	serveDBPath = dbPath

	got, err := resolveServeDBPath(serveCmd)
	if err != nil {
		t.Fatalf("resolveServeDBPath() error: %v", err)
	}
	if got != dbPath {
		t.Fatalf("dbPath = %q, want %q", got, dbPath)
	}
}

func TestResolveServeDBPath_UsesDataDir(t *testing.T) {
	restore := snapshotServeGlobals(t)
	defer restore()

	dataDir := t.TempDir()
	dbDir := filepath.Join(dataDir, "db")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}
	dbPath := filepath.Join(dbDir, "conductor.db")
	if err := os.WriteFile(dbPath, []byte("test"), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	serveDataDir = dataDir

	got, err := resolveServeDBPath(serveCmd)
	if err != nil {
		t.Fatalf("resolveServeDBPath() error: %v", err)
	}
	if got != dbPath {
		t.Fatalf("dbPath = %q, want %q", got, dbPath)
	}
}

func TestResolveServeDBPath_UsesProjectWhenPresent(t *testing.T) {
	restore := snapshotServeGlobals(t)
	defer restore()

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	projectDir := t.TempDir()
	projectPath = filepath.Join(projectDir, "project.yaml")
	projectFlag := rootCmd.PersistentFlags().Lookup("project")
	projectFlag.Changed = false

	projectConfig := "project:\n  name: serve-test\n  path: /tmp/repo\n  language: go\n"
	if err := os.WriteFile(projectPath, []byte(projectConfig), 0644); err != nil {
		t.Fatalf("WriteFile(project.yaml) error: %v", err)
	}

	dbDir := filepath.Join(homeDir, "source", ".conductor", "projects", "serve-test", "db")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		t.Fatalf("MkdirAll(dbDir) error: %v", err)
	}
	expectedDBPath := filepath.Join(dbDir, "conductor.db")
	if err := os.WriteFile(expectedDBPath, []byte("test"), 0644); err != nil {
		t.Fatalf("WriteFile(conductor.db) error: %v", err)
	}

	got, err := resolveServeDBPath(serveCmd)
	if err != nil {
		t.Fatalf("resolveServeDBPath() error: %v", err)
	}
	if got != expectedDBPath {
		t.Fatalf("dbPath = %q, want %q", got, expectedDBPath)
	}
}

func TestResolveServeDBPath_RequiresSource(t *testing.T) {
	restore := snapshotServeGlobals(t)
	defer restore()

	projectPath = filepath.Join(t.TempDir(), "missing-project.yaml")
	projectFlag := rootCmd.PersistentFlags().Lookup("project")
	projectFlag.Changed = false

	_, err := resolveServeDBPath(serveCmd)
	if err == nil {
		t.Fatal("resolveServeDBPath() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "--db") {
		t.Fatalf("error = %q, want hint about --db", err)
	}
}

func snapshotServeGlobals(t *testing.T) func() {
	t.Helper()

	oldServeDBPath := serveDBPath
	oldServeDataDir := serveDataDir
	oldProjectPath := projectPath
	oldCfg := cfg
	projectFlag := rootCmd.PersistentFlags().Lookup("project")
	oldProjectChanged := false
	if projectFlag != nil {
		oldProjectChanged = projectFlag.Changed
	}

	serveDBPath = ""
	serveDataDir = ""
	projectPath = "project.yaml"
	cfg = nil

	return func() {
		serveDBPath = oldServeDBPath
		serveDataDir = oldServeDataDir
		projectPath = oldProjectPath
		cfg = oldCfg
		if projectFlag != nil {
			projectFlag.Changed = oldProjectChanged
		}
	}
}
