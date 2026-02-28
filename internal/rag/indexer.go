package rag

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ponchione/agent-conductor/internal/config"
)

// parsedFile holds the results of parsing a single file during pass 1.
type parsedFile struct {
	relPath  string
	absPath  string
	language string
	content  []byte
	fileHash string
	chunks   []Chunk
}

// IndexRepo walks the project directory and indexes all matching files into the
// RAG store using a three-pass pipeline:
//
//  1. Walk + parse: collect all chunks with forward call metadata
//  2. Reverse call graph: populate CalledBy on target chunks
//  3. Describe + embed + store: enrich with descriptions and embeddings, then upsert
//
// Files are filtered by include/exclude globs. Change detection skips unchanged files.
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

	hashFile := filepath.Join(cfg.Project.DataDir, "rag_file_hashes.json")
	fileHashes, err := loadFileHashes(hashFile)
	if err != nil {
		slog.Warn("could not load file hashes, re-indexing all", "error", err)
		fileHashes = make(map[string]string)
	}

	// Schema migration: force full re-index when schema version changes.
	if fileHashes["__schema_version"] != SchemaVersion {
		slog.Info("schema version changed, forcing full re-index",
			"old", fileHashes["__schema_version"],
			"new", SchemaVersion,
		)
		fileHashes = map[string]string{"__schema_version": SchemaVersion}
		if err := store.DropAndRecreateTable(ctx); err != nil {
			slog.Warn("failed to drop and recreate table, continuing", "error", err)
		}
	}

	// Select parser based on project language.
	var parser Parser
	if cfg.Project.Language == "go" {
		goParser, err := NewGoASTParser(absRoot)
		if err != nil {
			slog.Warn("GoASTParser init failed, falling back to tree-sitter", "error", err)
			parser = NewTreeSitterParser()
		} else {
			parser = goParser
		}
	} else {
		parser = NewTreeSitterParser()
	}

	// ── Pass 1: Walk all files, parse, collect chunks ──

	var filesVisited int
	var parsed []parsedFile

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
		relPath = filepath.ToSlash(relPath)
		filesVisited++

		if !matchesAnyGlob(cfg.Index.Include, relPath) {
			return nil
		}
		if matchesAnyGlob(cfg.Index.Exclude, relPath) {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("failed to read file", "path", relPath, "err", err)
			return nil
		}

		fHash := ContentHashOf(string(content))
		if fileHashes[relPath] == fHash {
			return nil
		}

		lang := langFromExt(filepath.Ext(relPath))

		// GoASTParser needs absolute paths for pkgsByFile lookup.
		rawChunks, err := parser.ParseFile(path, lang, content)
		if err != nil {
			slog.Warn("parse failed", "path", relPath, "err", err)
			return nil
		}
		if len(rawChunks) == 0 {
			return nil
		}

		chunks := make([]Chunk, len(rawChunks))
		for i, raw := range rawChunks {
			chunks[i] = NewChunk(raw, cfg.Project.Name, relPath, lang, "")
		}

		parsed = append(parsed, parsedFile{
			relPath:  relPath,
			absPath:  path,
			language: lang,
			content:  content,
			fileHash: fHash,
			chunks:   chunks,
		})

		return nil
	})
	if walkErr != nil {
		return walkErr
	}

	// ── Pass 2: Reverse call graph — populate CalledBy ──

	// Build index: "package.FuncName" → indices into a flat chunk slice.
	type chunkRef struct {
		fileIdx  int
		chunkIdx int
	}
	callIndex := make(map[string][]chunkRef)

	for fi, pf := range parsed {
		for ci, chunk := range pf.chunks {
			if chunk.Name == "" {
				continue
			}
			// Key by qualified name for Go chunks, plain name otherwise.
			var key string
			if pf.language == "go" && len(chunk.Imports) > 0 {
				// Use the chunk's package from the file path as a rough key.
				// For reverse lookups within the same project this is sufficient.
				key = pf.relPath + "." + chunk.Name
			} else {
				key = chunk.Name
			}
			callIndex[key] = append(callIndex[key], chunkRef{fi, ci})
		}
	}

	// Also build a package-qualified index for Go functions.
	pkgIndex := make(map[string][]chunkRef)
	for fi, pf := range parsed {
		for ci, chunk := range pf.chunks {
			if chunk.Name == "" {
				continue
			}
			for _, call := range chunk.Calls {
				// Register the caller's identity for looking up later.
				_ = call // processed below
			}
			// Build package-based key from calls targets.
			// We index by the chunk's own identity.
			if pf.language == "go" {
				// Derive package path from relPath: "internal/rag/types.go" → "internal/rag"
				dir := filepath.Dir(pf.relPath)
				key := dir + "." + chunk.Name
				pkgIndex[key] = append(pkgIndex[key], chunkRef{fi, ci})
			}
		}
	}

	// For each chunk's Calls, look up the target and append CalledBy.
	for fi, pf := range parsed {
		for ci, chunk := range pf.chunks {
			for _, call := range chunk.Calls {
				// Try to find the target chunk by package path.
				var targets []chunkRef

				// Match by package directory + name.
				for _, pf2 := range parsed {
					dir2 := filepath.Dir(pf2.relPath)
					if strings.HasSuffix(call.Package, filepath.ToSlash(dir2)) || call.Package == dir2 {
						key := dir2 + "." + call.Name
						targets = append(targets, pkgIndex[key]...)
					}
				}

				// Also try direct name match for same-package calls.
				if call.Package == "" || strings.Contains(pf.relPath, call.Package) {
					for _, pf2 := range parsed {
						dir2 := filepath.Dir(pf2.relPath)
						key := dir2 + "." + call.Name
						targets = append(targets, pkgIndex[key]...)
					}
				}

				callerRef := FuncRef{
					Name:    chunk.Name,
					Package: call.Package, // reuse the package context
					File:    pf.relPath,
				}

				seen := make(map[chunkRef]bool)
				for _, t := range targets {
					if seen[t] {
						continue
					}
					seen[t] = true
					// Don't add self-references.
					if t.fileIdx == fi && t.chunkIdx == ci {
						continue
					}
					parsed[t.fileIdx].chunks[t.chunkIdx].CalledBy = append(
						parsed[t.fileIdx].chunks[t.chunkIdx].CalledBy,
						callerRef,
					)
				}
			}
		}
	}

	// ── Pass 3: Describe, embed, store ──

	var filesIndexed, totalChunks int

	for i := range parsed {
		pf := &parsed[i]

		descContent := string(pf.content)
		if relCtx := FormatRelationshipContext(pf.chunks); relCtx != "" {
			descContent = descContent + "\n\n" + relCtx
		}
		descriptions, err := describer.DescribeFile(ctx, pf.relPath, descContent)
		if err != nil {
			slog.Warn("describe failed", "path", pf.relPath, "err", err)
			descriptions = make(map[string]string)
		}

		// Apply descriptions to chunks.
		embedTexts := make([]string, len(pf.chunks))
		for j := range pf.chunks {
			desc := descriptions[pf.chunks[j].Name]
			pf.chunks[j].Description = desc
			embedTexts[j] = pf.chunks[j].Signature + "\n" + desc
		}

		embeddings, err := embedder.EmbedBatch(ctx, embedTexts)
		if err != nil {
			slog.Warn("embed failed", "path", pf.relPath, "err", err)
			continue
		}

		if err := store.UpsertChunks(ctx, pf.chunks, embeddings); err != nil {
			slog.Warn("upsert failed", "path", pf.relPath, "err", err)
			continue
		}

		fileHashes[pf.relPath] = pf.fileHash
		filesIndexed++
		totalChunks += len(pf.chunks)
		slog.Info("indexed file", "path", pf.relPath, "chunks", len(pf.chunks))
	}

	slog.Info("indexing complete",
		"files_visited", filesVisited,
		"files_indexed", filesIndexed,
		"total_chunks", totalChunks,
	)

	if saveErr := saveFileHashes(hashFile, fileHashes); saveErr != nil {
		slog.Warn("could not save file hashes", "error", saveErr)
	}

	return nil
}

// matchesAnyGlob returns true if relPath matches at least one of the patterns.
func matchesAnyGlob(patterns []string, relPath string) bool {
	for _, pat := range patterns {
		if matchesGlob(pat, relPath) {
			return true
		}
	}
	return false
}

// matchesGlob returns true if relPath matches the given glob pattern.
// Supports "**/" prefix for recursive matching.
func matchesGlob(pattern, relPath string) bool {
	if ok, _ := filepath.Match(pattern, relPath); ok {
		return true
	}
	if strings.HasPrefix(pattern, "**/") {
		suffix := pattern[3:]
		if ok, _ := filepath.Match(suffix, filepath.Base(relPath)); ok {
			return true
		}
		if ok, _ := filepath.Match(suffix, relPath); ok {
			return true
		}
		parts := strings.Split(relPath, "/")
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
	case ".ts":
		return "typescript"
	case ".tsx":
		return "tsx"
	default:
		return ""
	}
}
