package worker

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"github.com/ponchione/agent-conductor/internal/database"
)

func (w *Worker) registerWorkflowArtifact(ctx context.Context, workflowID, taskID, artifactType, path string, metadata map[string]any) {
	if path == "" {
		return
	}

	params := database.RegisterArtifactParams{
		WorkflowID:   workflowID,
		TaskID:       taskID,
		ArtifactType: artifactType,
		Path:         path,
		Metadata:     metadata,
	}

	sessionID, err := w.db.GetSessionIDForWorkflow(ctx, workflowID)
	if err == nil {
		params.SessionID = sessionID
	} else if !errors.Is(err, sql.ErrNoRows) {
		slog.Warn("failed to resolve session for artifact registration", "workflow_id", workflowID, "artifact_type", artifactType, "error", err)
	}

	if _, err := w.db.RegisterArtifact(ctx, params); err != nil {
		slog.Warn("failed to register artifact", "workflow_id", workflowID, "task_id", taskID, "artifact_type", artifactType, "path", path, "error", err)
	}
}
