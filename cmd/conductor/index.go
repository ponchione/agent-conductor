package main

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/ponchione/agent-conductor/internal/graph"
	"github.com/ponchione/agent-conductor/internal/rag"
	"github.com/spf13/cobra"
)

var indexForce bool

var indexCmd = &cobra.Command{
	Use:   "index",
	Short: "Index the project repository into the RAG store and graph database",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		// Phase 1: RAG Pipeline (existing)
		store, embedder, describer, err := buildRAGStack(ctx, cfg)
		if err != nil {
			return err
		}
		defer store.Close()

		if err := rag.IndexRepo(ctx, cfg, store, embedder, describer, rag.IndexOpts{
			Force: indexForce,
		}); err != nil {
			return fmt.Errorf("RAG indexing failed: %w", err)
		}

		// Phase 2: Graph Pipeline
		if cfg.Graph.Enabled {
			if err := runGraphPipeline(); err != nil {
				slog.Warn("graph indexing failed, continuing", "error", err)
			}
		}

		return nil
	},
}

func runGraphPipeline() error {
	dbPath := cfg.Graph.DBPath
	if dbPath == "" {
		dbPath = filepath.Join(cfg.Project.DataDir, "graph.db")
	}

	graphStore, err := graph.NewGraphStore(dbPath)
	if err != nil {
		return fmt.Errorf("open graph store: %w", err)
	}
	defer graphStore.Close()

	// Full re-index: drop and recreate
	if err := graphStore.DropAndRecreate(); err != nil {
		return fmt.Errorf("reset graph store: %w", err)
	}

	start := time.Now()

	resolver := graph.NewResolver(cfg.Project.Path, &cfg.Graph)
	result, err := resolver.Analyze()
	if err != nil {
		return fmt.Errorf("graph analysis: %w", err)
	}

	if err := graphStore.StoreAnalysisResult(result); err != nil {
		return fmt.Errorf("store graph: %w", err)
	}

	// Set metadata
	graphStore.SetMeta("indexed_at", time.Now().UTC().Format(time.RFC3339))
	graphStore.SetMeta("project_root", cfg.Project.Path)

	// Phase 3: Link — populate chunk_mapping
	// This correlates graph symbols with LanceDB chunks by file+name.
	// The ChunkMatcher adapter will be implemented when integrating with the RAG store.
	slog.Info("graph: chunk mapping phase skipped (requires RAG store adapter)")

	slog.Info("graph indexing complete",
		"symbols", len(result.Symbols),
		"edges", len(result.Edges),
		"duration", time.Since(start),
	)

	return nil
}

func init() {
	indexCmd.Flags().BoolVar(&indexForce, "force", false, "Drop and fully re-index, ignoring file hash cache")
}
