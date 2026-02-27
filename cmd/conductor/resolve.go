package main

import (
	"context"
	"fmt"

	"github.com/ponchione/agent-conductor/internal/database"
)

func resolveWorkflowID(ctx context.Context, db *database.DB, input string) (string, error) {
	ids, err := db.FindWorkflowsByPrefix(ctx, input)
	if err != nil {
		return "", fmt.Errorf("failed to resolve workflow ID: %w", err)
	}
	switch len(ids) {
	case 0:
		return "", fmt.Errorf("no workflow found matching %q", input)
	case 1:
		return ids[0], nil
	default:
		return "", fmt.Errorf("ambiguous prefix %q matches %d workflows: %v", input, len(ids), ids)
	}
}
