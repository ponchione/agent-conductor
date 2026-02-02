-- name: CreateWorkflow :exec
INSERT INTO workflows (
    id, original_intent, original_file, current_state, target_repo, git_branch,
    max_depth, max_files_changed, max_duration_mins
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetWorkflow :one
SELECT * FROM workflows WHERE id = ? LIMIT 1;

-- name: UpdateWorkflowState :exec
UPDATE workflows
SET current_state = ?, updated_at = datetime('now')
WHERE id = ?;

-- name: UpdateWorkflowBudget :exec
UPDATE workflows
SET current_depth = ?, files_changed = ?, updated_at = datetime('now')
WHERE id = ?;

-- name: CreateTask :exec
INSERT INTO tasks (
    id, workflow_id, sequence_num, task_type, agent_type, target_repo,
    input_artifact, state, max_attempts
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetTask :one
SELECT * FROM tasks WHERE id = ? LIMIT 1;

-- name: GetPendingTask :one
SELECT id FROM tasks
WHERE state = 'pending'
ORDER BY created_at ASC LIMIT 1;

-- name: ClaimTask :exec
UPDATE tasks
SET state = 'claimed', claimed_by = ?, claimed_at = datetime('now')
WHERE id = ?;

-- name: ReleaseTask :exec
UPDATE tasks
SET state = 'pending', claimed_by = NULL, claimed_at = NULL
WHERE id = ?;

-- name: CompleteTask :exec
UPDATE tasks
SET state = 'completed',
    exit_code = ?,
    stdout_log = ?,
    stderr_log = ?,
    files_changed = ?,
    completed_at = datetime('now')
WHERE id = ?;

-- name: FailTask :exec
UPDATE tasks
SET state = 'failed',
    error_message = ?,
    completed_at = datetime('now')
WHERE id = ?;

-- name: RetryTask :exec
UPDATE tasks
SET state = 'pending',
    claimed_by = NULL,
    claimed_at = NULL,
    attempts = attempts + 1
WHERE id = ?;

-- name: CreateEvent :exec
INSERT INTO events (workflow_id, task_id, event_type, event_data)
VALUES (?, ?, ?, ?);

-- name: ListEvents :many
SELECT * FROM events
WHERE workflow_id = ?
ORDER BY created_at ASC;