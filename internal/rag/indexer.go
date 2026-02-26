package rag

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ponchione/agent-conductor/internal/config"
)

// IndexRepo walks the project directory and indexes all matching files into the
// RAG store. Files are filtered by the include/exclude globs in cfg.Index.
// Change detection skips files whose content hash matches what is already stored.
func IndexRepo(
	ctx context.Context,
	cfg *config.ProjectConfig,
	store *Store,
	embedder *Embedder,
	describer *Describer,
) error {
	root := cfg.Project.Path
	if root == "" {
		root = "."
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}

	var filesVisited, filesIndexed, totalChunks int

	walkErr := filepath.WalkDir(absRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(absRoot, path)
		if err != nil {
			return err
		}
		// Normalize to forward slashes for cross-platform glob matching.
		relPath = filepath.ToSlash(relPath)

		filesVisited++

		// Must match at least one include glob.
		included := false
		for _, pat := range cfg.Index.Include {
			if matchesGlob(pat, relPath) {
				included = true
				break
			}
		}
		if !included {
			return nil
		}

		// Must not match any exclude glob.
		for _, pat := range cfg.Index.Exclude {
			if matchesGlob(pat, relPath) {
				return nil
			}
		}

		content, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("failed to read file", "path", relPath, "err", err)
			return nil
		}

		// Change detection: skip if content hash matches stored chunks.
		existing, err := store.ChunksByFilePath(ctx, relPath)
		if err == nil && len(existing) > 0 {
			if existing[0].ContentHash == ContentHashOf(string(content)) {
				slog.Info("skipping unchanged file", "path", relPath)
				return nil
			}
		}

		lang := langFromExt(filepath.Ext(relPath))

		rawChunks, err := ParseFile(relPath, lang, content)
		if err != nil {
			slog.Warn("parse failed", "path", relPath, "err", err)
			return nil
		}
		if len(rawChunks) == 0 {
			return nil
		}

		descriptions, err := describer.DescribeFile(ctx, relPath, string(content))
		if err != nil {
			slog.Warn("describe failed", "path", relPath, "err", err)
			descriptions = make(map[string]string)
		}

		embedTexts := make([]string, len(rawChunks))
		for i, raw := range rawChunks {
			embedTexts[i] = raw.Signature + "\n" + descriptions[raw.Name]
		}

		embeddings, err := embedder.EmbedBatch(ctx, embedTexts)
		if err != nil {
			slog.Warn("embed failed", "path", relPath, "err", err)
			return nil
		}

		chunks := make([]Chunk, len(rawChunks))
		for i, raw := range rawChunks {
			chunks[i] = NewChunk(raw, cfg.Project.Name, relPath, lang, descriptions[raw.Name])
		}

		if err := store.UpsertChunks(ctx, chunks, embeddings); err != nil {
			slog.Warn("upsert failed", "path", relPath, "err", err)
			return nil
		}

		filesIndexed++
		totalChunks += len(chunks)
		slog.Info("indexed file", "path", relPath, "chunks", len(chunks))
		return nil
	})

	slog.Info("indexing complete",
		"files_visited", filesVisited,
		"files_indexed", filesIndexed,
		"total_chunks", totalChunks,
	)

	return walkErr
}

// matchesGlob returns true if relPath matches the given glob pattern.
// Supports "**/" prefix for recursive matching.
func matchesGlob(pattern, relPath string) bool {
	// Direct match.
	if ok, _ := filepath.Match(pattern, relPath); ok {
		return true
	}
	// "**/*.go" style — match basename, and also full relPath with suffix.
	if strings.HasPrefix(pattern, "**/") {
		suffix := pattern[3:]
		if ok, _ := filepath.Match(suffix, filepath.Base(relPath)); ok {
			return true
		}
		if ok, _ := filepath.Match(suffix, relPath); ok {
			return true
		}
		// Also match directory segments: "**/.git/**" should match ".git/config".
		// Walk each path segment.
		parts := strings.SplitN(relPath, "/", -1)
		for i := range parts {
			sub := strings.Join(parts[i:], "/")
			if ok, _ := filepath.Match(suffix, sub); ok {
				return true
			}
		}
	}
	return false
}

// langFromExt maps a file extension to the language identifier used by ParseFile.
func langFromExt(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".md":
		return "markdown"
	default:
		return ""
	}
}
