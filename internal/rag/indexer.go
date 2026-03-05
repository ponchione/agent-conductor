package rag

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/util"
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

// chunkRef identifies a chunk by its position within the parsed file list.
type chunkRef struct {
	fileIdx  int
	chunkIdx int
}

// IndexOpts configures optional behavior for IndexRepo.
type IndexOpts struct {
	// Force drops the existing index and re-indexes all files,
	// ignoring the file hash cache.
	Force bool
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
	opts IndexOpts,
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

	if opts.Force {
		slog.Info("force flag set, dropping index and re-indexing all files")
		fileHashes = map[string]string{"__schema_version": SchemaVersion}
		if err := store.DropAndRecreateTable(ctx); err != nil {
			slog.Warn("failed to drop and recreate table, continuing", "error", err)
		}
	}

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

	parsed, filesVisited, walkErr := walkAndParse(absRoot, cfg, parser, fileHashes)
	if walkErr != nil {
		return walkErr
	}

	buildReverseCallGraph(parsed)

	filesIndexed, totalChunks := describeEmbedStore(ctx, parsed, store, embedder, describer, fileHashes)

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

// walkAndParse walks the project directory and parses all matching files into
// chunks (Pass 1). Returns the parsed files, total files visited, and any error.
func walkAndParse(absRoot string, cfg *config.ProjectConfig, parser Parser, fileHashes map[string]string) ([]parsedFile, int, error) {
	var filesVisited int
	var parsed []parsedFile

	err := filepath.WalkDir(absRoot, func(path string, d os.DirEntry, err error) error {
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

		if !util.MatchesAnyGlob(cfg.Index.Include, relPath) {
			return nil
		}
		if util.MatchesAnyGlob(cfg.Index.Exclude, relPath) {
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

	return parsed, filesVisited, err
}

// buildReverseCallGraph populates CalledBy on target chunks by resolving
// forward Calls references against pre-computed indexes (Pass 2).
//
// Uses O(n) lookups via suffixToDir and pkgIndex instead of iterating
// all parsed files for each call reference.
func buildReverseCallGraph(parsed []parsedFile) {
	// pkgIndex maps "dir.FuncName" → chunk references for O(1) lookup.
	pkgIndex := make(map[string][]chunkRef)
	dirSet := make(map[string]bool)
	for fi, pf := range parsed {
		for ci, chunk := range pf.chunks {
			if chunk.Name == "" {
				continue
			}
			if pf.language == "go" {
				dir := filepath.Dir(pf.relPath)
				key := dir + "." + chunk.Name
				pkgIndex[key] = append(pkgIndex[key], chunkRef{fi, ci})
				dirSet[dir] = true
			}
		}
	}

	// allDirs is the deduplicated list of directories for fallback resolution.
	allDirs := make([]string, 0, len(dirSet))
	for d := range dirSet {
		allDirs = append(allDirs, d)
	}

	// suffixToDir maps each directory path (and its slash-separated suffixes)
	// to the full directories that end with that suffix. This replaces the
	// inner O(n) loop over all parsed files with an O(1) map lookup.
	suffixToDir := make(map[string][]string)
	for d := range dirSet {
		slashDir := filepath.ToSlash(d)
		suffixToDir[slashDir] = append(suffixToDir[slashDir], d)
		suffixToDir[d] = append(suffixToDir[d], d)
		parts := strings.Split(slashDir, "/")
		for i := 1; i < len(parts); i++ {
			suffix := strings.Join(parts[i:], "/")
			suffixToDir[suffix] = append(suffixToDir[suffix], d)
		}
	}

	for fi, pf := range parsed {
		for ci, chunk := range pf.chunks {
			for _, call := range chunk.Calls {
				var targets []chunkRef

				// Resolve by package suffix match.
				if call.Package != "" {
					if dirs, ok := suffixToDir[call.Package]; ok {
						for _, d := range dirs {
							targets = append(targets, pkgIndex[d+"."+call.Name]...)
						}
					}
				}

				// Fallback: empty package or caller path contains the package.
				if call.Package == "" || strings.Contains(pf.relPath, call.Package) {
					for _, d := range allDirs {
						targets = append(targets, pkgIndex[d+"."+call.Name]...)
					}
				}

				callerRef := FuncRef{
					Name:    chunk.Name,
					Package: call.Package,
					File:    pf.relPath,
				}

				seen := make(map[chunkRef]bool)
				for _, t := range targets {
					if seen[t] {
						continue
					}
					seen[t] = true
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
}

// describeEmbedStore enriches parsed chunks with descriptions and embeddings,
// then upserts them into the store (Pass 3). Returns files indexed and total chunks.
func describeEmbedStore(
	ctx context.Context,
	parsed []parsedFile,
	store *Store,
	embedder *Embedder,
	describer *Describer,
	fileHashes map[string]string,
) (int, int) {
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

	return filesIndexed, totalChunks
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
	case ".py":
		return "python"
	default:
		return ""
	}
}
