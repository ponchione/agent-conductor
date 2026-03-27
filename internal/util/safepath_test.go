package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafePath_AllowsExistingFileUnderRoot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "src", "main.go")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}
	if err := os.WriteFile(path, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	got, err := SafePath(root, "src/main.go")
	if err != nil {
		t.Fatalf("SafePath() error: %v", err)
	}
	if got != path {
		t.Fatalf("SafePath() = %q, want %q", got, path)
	}
}

func TestSafePath_RejectsLexicalTraversalEscape(t *testing.T) {
	root := t.TempDir()

	if _, err := SafePath(root, "../outside.txt"); err == nil {
		t.Fatal("SafePath() error = nil, want traversal error")
	}
}

func TestSafePath_RejectsSymlinkToOutsideFile(t *testing.T) {
	root := t.TempDir()
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	mustSymlink(t, outsideFile, filepath.Join(root, "secret-link.txt"))

	if _, err := SafePath(root, "secret-link.txt"); err == nil {
		t.Fatal("SafePath() error = nil, want symlink escape error")
	}
}

func TestSafePath_RejectsSymlinkedDirectoryToOutside(t *testing.T) {
	root := t.TempDir()
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	mustSymlink(t, outsideDir, filepath.Join(root, "escape-dir"))

	if _, err := SafePath(root, filepath.Join("escape-dir", "secret.txt")); err == nil {
		t.Fatal("SafePath() error = nil, want symlink directory escape error")
	}
}

func TestSafePath_AllowsNonExistentNewFileUnderRoot(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, "new", "file.txt")

	got, err := SafePath(root, filepath.Join("new", "file.txt"))
	if err != nil {
		t.Fatalf("SafePath() error: %v", err)
	}
	if got != want {
		t.Fatalf("SafePath() = %q, want %q", got, want)
	}
}

func TestSafePath_RejectsNonExistentPathUnderSymlinkedOutsideParent(t *testing.T) {
	root := t.TempDir()
	outsideDir := t.TempDir()
	mustSymlink(t, outsideDir, filepath.Join(root, "escape-dir"))

	if _, err := SafePath(root, filepath.Join("escape-dir", "new.txt")); err == nil {
		t.Fatal("SafePath() error = nil, want symlink parent escape error")
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}
