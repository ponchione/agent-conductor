package main

import (
	"context"
	"fmt"
	"os"
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

	embedder := rag.NewEmbedder(rag.EmbedderConfig{
		Endpoint:       cfg.EmbedModel.Endpoint,
		TimeoutSeconds: cfg.EmbedModel.TimeoutSeconds,
	})

	prompts, err := templates.LoadPrompts(cfg)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load prompts: %w", err)
	}

	var describeClient llm.Client
	if provName, ok := cfg.Models.Roles["describe"]; ok {
		if pc, ok := cfg.Models.Providers[provName]; ok {
			describeClient = llm.NewProviderClient(llm.Provider{
				Endpoint:         pc.Endpoint,
				Model:            pc.Model,
				APIKey:           os.ExpandEnv(pc.APIKey),
				TimeoutSeconds:   pc.TimeoutSeconds,
				MaxContextTokens: pc.MaxContextTokens,
				Temperature:      pc.Temperature,
			})
		}
	}
	if describeClient == nil {
		describeClient = llm.New(cfg.LocalModel)
	}
	describer := rag.NewDescriber(&llm.RAGCompleterAdapter{Client: describeClient}, prompts.Describe)

	return store, embedder, describer, nil
}
