package main

import (
	"context"
	"fmt"
	"os"

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
		dataDir := cfg.Project.DataDir
		if dataDir == "" {
			dataDir = ".conductor"
		}

		if err := os.MkdirAll(dataDir, 0755); err != nil {
			return fmt.Errorf("create data dir: %w", err)
		}

		ctx := context.Background()

		store, err := rag.NewStore(ctx, dataDir+"/rag.db")
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

