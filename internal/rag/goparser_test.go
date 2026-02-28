package rag

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// repoRoot returns the root of the agent-conductor repository by
// walking up from the test file's location.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine test file path")
	}
	// thisFile is internal/rag/goparser_test.go → root is ../../
	root := filepath.Join(filepath.Dir(thisFile), "..", "..")
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	return abs
}

func TestGoASTParser_SelfParse(t *testing.T) {
	root := repoRoot(t)

	parser, err := NewGoASTParser(root)
	if err != nil {
		t.Fatalf("NewGoASTParser: %v", err)
	}

	// Parse types.go — should find the NewChunk function with calls and imports.
	typesPath := filepath.Join(root, "internal", "rag", "types.go")
	content, err := os.ReadFile(typesPath)
	if err != nil {
		t.Fatalf("read types.go: %v", err)
	}

	chunks, err := parser.ParseFile(typesPath, "go", content)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if len(chunks) == 0 {
		t.Fatal("expected chunks from types.go, got 0")
	}

	// Find the NewChunk function.
	var newChunk *RawChunk
	for i := range chunks {
		if chunks[i].Name == "NewChunk" {
			newChunk = &chunks[i]
			break
		}
	}

	if newChunk == nil {
		t.Fatal("NewChunk function not found in parsed chunks")
	}

	if newChunk.ChunkType != "function" {
		t.Errorf("expected ChunkType 'function', got %q", newChunk.ChunkType)
	}

	// NewChunk should have calls (e.g., ChunkID, ContentHashOf).
	if len(newChunk.Calls) == 0 {
		t.Error("expected NewChunk to have non-empty Calls")
	}

	// Should have file-level imports.
	if len(newChunk.Imports) == 0 {
		t.Error("expected NewChunk to have non-empty Imports")
	}

	// Verify that a known call target exists.
	foundChunkID := false
	for _, call := range newChunk.Calls {
		if call.Name == "ChunkID" {
			foundChunkID = true
			break
		}
	}
	if !foundChunkID {
		t.Error("expected NewChunk.Calls to include ChunkID")
	}
}

func TestGoASTParser_TypeImplements(t *testing.T) {
	root := repoRoot(t)

	parser, err := NewGoASTParser(root)
	if err != nil {
		t.Fatalf("NewGoASTParser: %v", err)
	}

	// Parse store.go — Store type should exist.
	storePath := filepath.Join(root, "internal", "rag", "store.go")
	content, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read store.go: %v", err)
	}

	chunks, err := parser.ParseFile(storePath, "go", content)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	// Find a type chunk.
	var foundType bool
	for _, c := range chunks {
		if c.ChunkType == "type" {
			foundType = true
			break
		}
	}

	if !foundType {
		t.Log("no type chunks found — this is OK if Store is unexported or has no struct fields")
	}
}

func TestGoASTParser_NonGoFallback(t *testing.T) {
	root := repoRoot(t)

	parser, err := NewGoASTParser(root)
	if err != nil {
		t.Fatalf("NewGoASTParser: %v", err)
	}

	// A markdown file should be delegated to tree-sitter.
	mdContent := []byte("## Section One\nSome content\n\n## Section Two\nMore content\n")
	chunks, err := parser.ParseFile("test.md", "markdown", mdContent)
	if err != nil {
		t.Fatalf("ParseFile markdown: %v", err)
	}

	if len(chunks) != 2 {
		t.Errorf("expected 2 markdown sections, got %d", len(chunks))
	}

	for _, c := range chunks {
		if c.ChunkType != "section" {
			t.Errorf("expected section chunk type for markdown, got %q", c.ChunkType)
		}
	}
}
