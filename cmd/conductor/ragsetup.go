package main

import (
	"context"
	"path/filepath"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/llm"
	"github.com/ponchione/agent-conductor/internal/rag"
)

// llmAdapter bridges llm.Client (which returns (string, Usage, error)) to the
// rag.LLMCompleter interface (which expects (string, error)).
type llmAdapter struct{ c *llm.Client }

func (a *llmAdapter) Complete(ctx context.Context, sys, user string) (string, error) {
	text, _, err := a.c.Complete(ctx, sys, user)
	return text, err
}

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

	embedder := rag.NewEmbedder(rag.EmbedderConfig{
		Endpoint:       cfg.EmbedModel.Endpoint,
		TimeoutSeconds: cfg.EmbedModel.TimeoutSeconds,
	})

	llmClient := llm.New(cfg.LocalModel)
	describer := rag.NewDescriber(&llmAdapter{llmClient})

	return store, embedder, describer, nil
}
