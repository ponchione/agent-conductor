package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/llm"
	"github.com/ponchione/agent-conductor/internal/rag"
	"github.com/ponchione/agent-conductor/internal/templates"
)

// buildRAGStack initialises the RAG store, embedder, and describer from the
// project config. The caller owns store.Close().
func buildRAGStack(ctx context.Context, cfg *config.ProjectConfig) (*rag.Store, *rag.Embedder, *rag.Describer, error) {
	if err := config.EnsureDataDirs(cfg); err != nil {
		return nil, nil, nil, err
	}

	store, err := rag.NewStore(ctx, filepath.Join(cfg.Project.DataDir, "rag"))
	if err != nil {
		return nil, nil, nil, err
	}
	success := false
	defer func() {
		if !success {
			store.Close()
		}
	}()

	embedder := rag.NewEmbedder(rag.EmbedderConfig{
		Endpoint:       cfg.EmbedModel.Endpoint,
		TimeoutSeconds: cfg.EmbedModel.TimeoutSeconds,
	})

	prompts, err := templates.LoadPrompts(cfg)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load prompts: %w", err)
	}

	describeClient, err := buildClientForRole(cfg, "describe")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("invalid model config: %w\n  Hint: run \"conductor init-global\" or define models.providers and models.roles.describe explicitly", err)
	}
	describer := rag.NewDescriber(&llm.RAGCompleterAdapter{Client: describeClient}, prompts.Describe)

	success = true
	return store, embedder, describer, nil
}
