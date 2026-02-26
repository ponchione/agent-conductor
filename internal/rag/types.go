package rag

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strconv"
	"time"
)

// RawChunk is the output of the tree-sitter parser before enrichment.
// It contains structural information only — no embedding, no description.
type RawChunk struct {
	Name      string // function/method/type name
	Signature string // full signature extracted by tree-sitter
	Body      string // full function body (truncated at MaxBodyLength)
	ChunkType string // "function", "method", "type", "table", "section"
	LineStart int
	LineEnd   int
}

// MaxBodyLength is the maximum character length for a chunk body.
// Bodies exceeding this are truncated before storage.
const MaxBodyLength = 2000

// Chunk is the fully enriched unit of indexing stored in LanceDB.
type Chunk struct {
	// Identity
	ID          string
	ProjectName string

	// Location
	FilePath  string // repo-relative: "internal/features/auth/service.go"
	Language  string // "go", "typescript", "dart", "python", "sql", "markdown"
	ChunkType string // "function", "method", "type", "table", "section"

	// Content
	Name        string
	Signature   string
	Body        string
	Description string // 1-2 sentence semantic summary from local LLM

	// Metadata
	LineStart   int
	LineEnd     int
	ContentHash string // sha256 of Body for change detection
	IndexedAt   time.Time
}

// ChunkID computes the deterministic ID for a chunk.
// Formula: sha256(filePath + chunkType + name + lineStart)
// This prevents collisions when the same name appears multiple times
// in a file (e.g., methods on different receiver types).
func ChunkID(filePath, chunkType, name string, lineStart int) string {
	raw := filePath + chunkType + name + strconv.Itoa(lineStart)
	h := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", h)
}

// ContentHash computes the sha256 hash of chunk body content.
// Used for change detection during incremental re-indexing.
func ContentHashOf(body string) string {
	h := sha256.Sum256([]byte(body))
	return fmt.Sprintf("%x", h)
}

// NewChunk creates a fully populated Chunk from a RawChunk and enrichment data.
func NewChunk(raw RawChunk, projectName, filePath, language, description string) Chunk {
	body := raw.Body
	if len(body) > MaxBodyLength {
		body = body[:MaxBodyLength]
	}

	return Chunk{
		ID:          ChunkID(filePath, raw.ChunkType, raw.Name, raw.LineStart),
		ProjectName: projectName,
		FilePath:    filePath,
		Language:    language,
		ChunkType:   raw.ChunkType,
		Name:        raw.Name,
		Signature:   raw.Signature,
		Body:        body,
		Description: description,
		LineStart:   raw.LineStart,
		LineEnd:     raw.LineEnd,
		ContentHash: ContentHashOf(body),
		IndexedAt:   time.Now(),
	}
}

// SearchResult pairs a retrieved chunk with its cosine similarity score.
type SearchResult struct {
	Chunk Chunk
	Score float32 // cosine similarity, 0.0–1.0
}

// Filter constrains vector search results. All fields are optional;
// zero-value fields are ignored.
type Filter struct {
	Language  string // restrict to "go", "typescript", etc.
	ChunkType string // restrict to "function", "table", etc.
	FilePath  string // restrict to a specific file path prefix
}

// Searcher is the query interface consumed by context assembly.
// Implementations handle query embedding and vector search internally.
type Searcher interface {
	Search(ctx context.Context, query string, topK int, filters ...Filter) ([]SearchResult, error)
}

// FormatForContext renders a SearchResult as a text block suitable for
// injection into the scope phase's assembled context.
func (r SearchResult) FormatForContext() string {
	return fmt.Sprintf("[Score: %.2f] %s — %s (lines %d-%d)\n  %s\n  %s",
		r.Score,
		r.Chunk.FilePath,
		r.Chunk.Name,
		r.Chunk.LineStart,
		r.Chunk.LineEnd,
		r.Chunk.Description,
		r.Chunk.Signature,
	)
}

// FormatResultsBlock formats a slice of SearchResults as the full
// context assembly section.
func FormatResultsBlock(results []SearchResult) string {
	if len(results) == 0 {
		return ""
	}

	block := "=== SEMANTICALLY RELEVANT CODE (via RAG) ===\n"
	for _, r := range results {
		block += r.FormatForContext() + "\n\n"
	}
	return block
}
