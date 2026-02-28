package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/llm"
	"github.com/ponchione/agent-conductor/internal/rag"
	"github.com/spf13/cobra"
)

// llmAdapter bridges llm.Client (which returns (string, Usage, error)) to the
// rag.LLMCompleter interface (which expects (string, error)).
type llmAdapter struct{ c *llm.Client }

func (a *llmAdapter) Complete(ctx context.Context, sys, user string) (string, error) {
	text, _, err := a.c.Complete(ctx, sys, user)
	return text, err
}

var indexCmd = &cobra.Command{
	Use:   "index",
	Short: "Index the project repository into the RAG store",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.EnsureDataDirs(cfg); err != nil {
			return fmt.Errorf("create data dirs: %w", err)
		}

		ctx := context.Background()

		store, err := rag.NewStore(ctx, filepath.Join(cfg.Project.DataDir, "rag"))
		if err != nil {
			return fmt.Errorf("open store: %w", err)
		}
		defer store.Close()

		embedder := rag.NewEmbedder(rag.EmbedderConfig{
			Endpoint:       cfg.EmbedModel.Endpoint,
			TimeoutSeconds: cfg.EmbedModel.TimeoutSeconds,
		})

		llmClient := llm.New(cfg.LocalModel)
		describer := rag.NewDescriber(&llmAdapter{llmClient})

		return rag.IndexRepo(ctx, cfg, store, embedder, describer)
	},
}

