package main

import (
	"context"

	"github.com/ponchione/agent-conductor/internal/rag"
	"github.com/spf13/cobra"
)

var indexForce bool

var indexCmd = &cobra.Command{
	Use:   "index",
	Short: "Index the project repository into the RAG store",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		store, embedder, describer, err := buildRAGStack(ctx, cfg)
		if err != nil {
			return err
		}
		defer store.Close()

		return rag.IndexRepo(ctx, cfg, store, embedder, describer, rag.IndexOpts{
			Force: indexForce,
		})
	},
}

func init() {
	indexCmd.Flags().BoolVar(&indexForce, "force", false, "Drop and fully re-index, ignoring file hash cache")
}
