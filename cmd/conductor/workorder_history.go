package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/models"
)

func runInputWorkOrderPath(dataDir, workflowID string) string {
	return filepath.Join(dataDir, "artifacts", "work-orders", workflowID+".yaml")
}

func persistRunInputWorkOrder(ctx context.Context, db *database.DB, sessionID, workflowID, managedPath, sourcePath string, sourceContent []byte, wo models.WorkOrder) error {
	if err := os.MkdirAll(filepath.Dir(managedPath), 0755); err != nil {
		return fmt.Errorf("create input work order dir: %w", err)
	}
	if err := os.WriteFile(managedPath, sourceContent, 0644); err != nil {
		return fmt.Errorf("write input work order artifact: %w", err)
	}

	if err := db.UpdatePipelineRunWorkOrderContent(ctx, database.UpdatePipelineRunWorkOrderContentParams{
		WorkOrderContent: database.String(string(sourceContent)),
		WorkflowID:       workflowID,
	}); err != nil {
		return fmt.Errorf("store work order content in pipeline_runs: %w", err)
	}

	metadata := map[string]any{
		"title":             wo.Title,
		"type":              wo.Type,
		"target_module":     wo.TargetModule,
		"reference_module":  wo.ReferenceModule,
		"schema_version":    wo.SchemaVersion,
		"acceptance_count":  len(wo.TypedAcceptanceCriteria),
		"requirement_count": len(wo.Requirements),
		"source_path":       sourcePath,
		"storage":           "raw_source_yaml",
	}
	if wo.AuditSource != "" {
		metadata["audit_source"] = wo.AuditSource
	}

	if _, err := db.RegisterArtifact(ctx, database.RegisterArtifactParams{
		SessionID:    sessionID,
		WorkflowID:   workflowID,
		ArtifactType: database.ArtifactTypeInputWorkOrder,
		Path:         managedPath,
		Metadata:     metadata,
	}); err != nil {
		return fmt.Errorf("register input work order artifact: %w", err)
	}

	return nil
}
