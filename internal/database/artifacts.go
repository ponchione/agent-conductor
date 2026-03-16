package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/google/uuid"
)

const (
	ArtifactTypePlanRawGeneration  = "plan_raw_generation"
	ArtifactTypePlanRawAudit       = "plan_raw_audit"
	ArtifactTypeGeneratedWorkOrder = "generated_work_order"
	ArtifactTypeContextPackage     = "context_package"
	ArtifactTypeVerifyReport       = "verify_report"
	ArtifactTypeBuildStdout        = "build_stdout"
	ArtifactTypeBuildStderr        = "build_stderr"
	ArtifactTypePlanParseFailure   = "plan_parse_failure_raw_output"
)

// Artifact is a DB-backed reference to a file written on disk.
type Artifact struct {
	ID           string
	SessionID    sql.NullString
	WorkflowID   sql.NullString
	TaskID       sql.NullString
	ArtifactType string
	Path         string
	SizeBytes    sql.NullInt64
	MetadataJSON sql.NullString
	CreatedAt    string
}

// RegisterArtifactParams describes one artifact registration request.
type RegisterArtifactParams struct {
	SessionID    string
	WorkflowID   string
	TaskID       string
	ArtifactType string
	Path         string
	Metadata     map[string]any
}

// RegisterArtifact records a file-backed artifact for later UI lookup.
func (db *DB) RegisterArtifact(ctx context.Context, params RegisterArtifactParams) (Artifact, error) {
	if params.ArtifactType == "" {
		return Artifact{}, errors.New("artifact type is required")
	}
	if params.Path == "" {
		return Artifact{}, errors.New("artifact path is required")
	}

	var sizeBytes sql.NullInt64
	if info, err := os.Stat(params.Path); err == nil {
		sizeBytes = sql.NullInt64{Int64: info.Size(), Valid: true}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Artifact{}, fmt.Errorf("stat artifact: %w", err)
	}

	metadataJSON, err := marshalArtifactMetadata(params.Metadata)
	if err != nil {
		return Artifact{}, err
	}

	artifactID := uuid.New().String()
	if _, err := db.conn.ExecContext(ctx, `
		INSERT INTO artifacts (
			id, session_id, workflow_id, task_id, artifact_type, path, size_bytes, metadata_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		artifactID,
		nullableString(params.SessionID),
		nullableString(params.WorkflowID),
		nullableString(params.TaskID),
		params.ArtifactType,
		params.Path,
		sizeBytes,
		metadataJSON,
	); err != nil {
		return Artifact{}, fmt.Errorf("insert artifact: %w", err)
	}

	return db.GetArtifact(ctx, artifactID)
}

// GetArtifact loads one artifact row by ID.
func (db *DB) GetArtifact(ctx context.Context, artifactID string) (Artifact, error) {
	row := db.conn.QueryRowContext(ctx, `
		SELECT
			id, session_id, workflow_id, task_id, artifact_type, path, size_bytes, metadata_json, created_at
		FROM artifacts
		WHERE id = ?
		LIMIT 1
	`, artifactID)

	var artifact Artifact
	err := row.Scan(
		&artifact.ID,
		&artifact.SessionID,
		&artifact.WorkflowID,
		&artifact.TaskID,
		&artifact.ArtifactType,
		&artifact.Path,
		&artifact.SizeBytes,
		&artifact.MetadataJSON,
		&artifact.CreatedAt,
	)
	return artifact, err
}

// ListArtifactsBySession returns artifacts for a session newest-first.
func (db *DB) ListArtifactsBySession(ctx context.Context, sessionID string) ([]Artifact, error) {
	return listArtifacts(ctx, db.conn, `
		SELECT
			id, session_id, workflow_id, task_id, artifact_type, path, size_bytes, metadata_json, created_at
		FROM artifacts
		WHERE session_id = ?
		ORDER BY created_at DESC, id DESC
	`, sessionID)
}

// ListArtifactsByWorkflow returns artifacts for a workflow newest-first.
func (db *DB) ListArtifactsByWorkflow(ctx context.Context, workflowID string) ([]Artifact, error) {
	return listArtifacts(ctx, db.conn, `
		SELECT
			id, session_id, workflow_id, task_id, artifact_type, path, size_bytes, metadata_json, created_at
		FROM artifacts
		WHERE workflow_id = ?
		ORDER BY created_at DESC, id DESC
	`, workflowID)
}

// GetLatestArtifactByType returns the newest matching artifact for a session or workflow.
func (db *DB) GetLatestArtifactByType(ctx context.Context, artifactType, sessionID, workflowID string) (Artifact, error) {
	if artifactType == "" {
		return Artifact{}, errors.New("artifact type is required")
	}
	if sessionID == "" && workflowID == "" {
		return Artifact{}, errors.New("sessionID or workflowID is required")
	}

	query := `
		SELECT
			id, session_id, workflow_id, task_id, artifact_type, path, size_bytes, metadata_json, created_at
		FROM artifacts
		WHERE artifact_type = ?
	`
	args := []any{artifactType}
	if sessionID != "" {
		query += ` AND session_id = ?`
		args = append(args, sessionID)
	}
	if workflowID != "" {
		query += ` AND workflow_id = ?`
		args = append(args, workflowID)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT 1`

	row := db.conn.QueryRowContext(ctx, query, args...)

	var artifact Artifact
	err := row.Scan(
		&artifact.ID,
		&artifact.SessionID,
		&artifact.WorkflowID,
		&artifact.TaskID,
		&artifact.ArtifactType,
		&artifact.Path,
		&artifact.SizeBytes,
		&artifact.MetadataJSON,
		&artifact.CreatedAt,
	)
	return artifact, err
}

func listArtifacts(ctx context.Context, q DBTX, query string, arg string) ([]Artifact, error) {
	rows, err := q.QueryContext(ctx, query, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var artifacts []Artifact
	for rows.Next() {
		var artifact Artifact
		if err := rows.Scan(
			&artifact.ID,
			&artifact.SessionID,
			&artifact.WorkflowID,
			&artifact.TaskID,
			&artifact.ArtifactType,
			&artifact.Path,
			&artifact.SizeBytes,
			&artifact.MetadataJSON,
			&artifact.CreatedAt,
		); err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}

	return artifacts, rows.Err()
}

func marshalArtifactMetadata(metadata map[string]any) (sql.NullString, error) {
	if len(metadata) == 0 {
		return sql.NullString{}, nil
	}

	payload, err := json.Marshal(metadata)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("marshal artifact metadata: %w", err)
	}

	return sql.NullString{String: string(payload), Valid: true}, nil
}
