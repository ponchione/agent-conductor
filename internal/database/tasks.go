package database

import (
	"database/sql"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/ponchione/agent-conductor/internal/util"
)

func (db *DB) CreateTask(t *Task) error {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	query := `INSERT INTO tasks (
		id, workflow_id, sequence_num, task_type, agent_type, target_repo,
		input_artifact, state, max_attempts
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := db.conn.Exec(query,
		t.ID, t.WorkflowID, t.SequenceNum, t.TaskType, t.AgentType, t.TargetRepo,
		t.InputArtifact, t.State, t.MaxAttempts,
	)
	return err
}

// ClaimTask atomically claims the oldest pending task
func (db *DB) ClaimTask(workerID string) (*Task, error) {
	tx, err := db.conn.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// 1. Find candidate
	querySelect := `SELECT id FROM tasks 
		WHERE state = 'pending' 
		ORDER BY created_at ASC LIMIT 1`

	var taskID string
	err = tx.QueryRow(querySelect).Scan(&taskID)
	if err == sql.ErrNoRows {
		return nil, nil // No tasks
	}
	if err != nil {
		return nil, err
	}

	// 2. Claim it
	queryUpdate := `UPDATE tasks 
		SET state = 'claimed', claimed_by = ?, claimed_at = datetime('now')
		WHERE id = ?`

	_, err = tx.Exec(queryUpdate, workerID, taskID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// 3. Return full task
	return db.GetTask(taskID)
}

func (db *DB) GetTask(id string) (*Task, error) {
	query := `SELECT id, workflow_id, sequence_num, task_type, agent_type, target_repo,
		input_artifact, output_artifact, state, claimed_by, claimed_at,
		attempts, max_attempts, exit_code, stdout_log, stderr_log, files_changed,
		created_at, started_at, completed_at, error_message
		FROM tasks WHERE id = ?`

	t := &Task{}
	var outArtifact, claimedBy, stdout, stderr, filesChangedStr, errMsg sql.NullString
	var claimedAt, createdAt, startedAt, completedAt sql.NullString
	var exitCode sql.NullInt64

	err := db.conn.QueryRow(query, id).Scan(
		&t.ID, &t.WorkflowID, &t.SequenceNum, &t.TaskType, &t.AgentType, &t.TargetRepo,
		&t.InputArtifact, &outArtifact, &t.State, &claimedBy, &claimedAt,
		&t.Attempts, &t.MaxAttempts, &exitCode, &stdout, &stderr, &filesChangedStr,
		&createdAt, &startedAt, &completedAt, &errMsg,
	)
	if err != nil {
		return nil, err
	}

	t.OutputArtifact = outArtifact.String
	t.ClaimedBy = claimedBy.String
	t.StdoutLog = stdout.String
	t.StderrLog = stderr.String
	t.ErrorMessage = errMsg.String

	t.ClaimedAt = util.ParseSQLiteTime(claimedAt)
	t.StartedAt = util.ParseSQLiteTime(startedAt)
	t.CompletedAt = util.ParseSQLiteTime(completedAt)

	if tVal := util.ParseSQLiteTime(createdAt); tVal != nil {
		t.CreatedAt = *tVal
	}

	if exitCode.Valid {
		val := int(exitCode.Int64)
		t.ExitCode = &val
	}

	if filesChangedStr.Valid && filesChangedStr.String != "" {
		_ = json.Unmarshal([]byte(filesChangedStr.String), &t.FilesChanged)
	}

	return t, nil
}

func (db *DB) ReleaseTask(taskID string) error {
	query := `UPDATE tasks 
		SET state = 'pending', claimed_by = NULL, claimed_at = NULL 
		WHERE id = ?`
	_, err := db.conn.Exec(query, taskID)
	return err
}

func (db *DB) FailTask(taskID, errMsg string) error {
	query := `UPDATE tasks 
		SET state = 'failed', error_message = ?, completed_at = datetime('now') 
		WHERE id = ?`
	_, err := db.conn.Exec(query, errMsg, taskID)
	return err
}
