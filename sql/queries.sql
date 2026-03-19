-- name: ClaimTask :execrows
UPDATE tasks
SET state = 'claimed', claimed_by = ?, claimed_at = datetime('now')
WHERE id = ? AND state = 'pending';

-- name: CompleteTask :exec
UPDATE tasks
SET state = 'completed',
    exit_code = ?,
    stdout_log = ?,
    stderr_log = ?,
    files_changed = ?,
    completed_at = datetime('now')
WHERE id = ?;

-- name: CreateEvent :exec
INSERT INTO events (workflow_id, task_id, event_type, event_data)
VALUES (?, ?, ?, ?);

-- name: CreateTask :exec
INSERT INTO tasks (
    id, workflow_id, sequence_num, task_type, agent_type, target_repo,
    phase, input_artifact, state, max_attempts
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: CreateWorkflow :exec
INSERT INTO workflows (
    id, original_intent, original_file, current_state, target_repo, git_branch,
    context_package_path, verification_report_path,
    max_depth, max_files_changed, max_duration_mins
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: FailTask :exec
UPDATE tasks
SET state = 'failed',
    error_message = ?,
    completed_at = datetime('now')
WHERE id = ?;

-- name: GetPendingTask :one
SELECT id FROM tasks
WHERE state = 'pending'
ORDER BY created_at ASC LIMIT 1;

-- name: GetTask :one
SELECT id, workflow_id, sequence_num, task_type, agent_type, target_repo, phase, input_artifact, output_artifact, state, claimed_by, claimed_at, attempts, max_attempts, exit_code, stdout_log, stderr_log, files_changed, created_at, started_at, completed_at, error_message FROM tasks WHERE id = ? LIMIT 1;

-- name: GetWorkflow :one
SELECT id, original_intent, original_file, current_state, target_repo, git_branch, context_package_path, verification_report_path, max_depth, max_files_changed, max_duration_mins, current_depth, files_changed, started_at, created_at, updated_at, completed_at, error_message FROM workflows WHERE id = ? LIMIT 1;

-- name: ListEvents :many
SELECT id, workflow_id, task_id, event_type, event_data, created_at FROM events
WHERE workflow_id = ?
ORDER BY created_at ASC;

-- name: ReleaseTask :exec
UPDATE tasks
SET state = 'pending', claimed_by = NULL, claimed_at = NULL
WHERE id = ?;

-- name: RetryTask :exec
UPDATE tasks
SET state = 'pending',
    claimed_by = NULL,
    claimed_at = NULL,
    attempts = attempts + 1
WHERE id = ?;

-- name: UpdateWorkflowBudget :exec
UPDATE workflows
SET current_depth = ?, files_changed = ?, updated_at = datetime('now')
WHERE id = ?;

-- name: UpdateWorkflowState :exec
UPDATE workflows
SET current_state = ?, updated_at = datetime('now')
WHERE id = ?;

-- name: UpdateWorkflowContext :exec
UPDATE workflows
SET context_package_path = ?, updated_at = datetime('now')
WHERE id = ?;

-- name: UpdateWorkflowVerification :exec
UPDATE workflows
SET verification_report_path = ?, current_state = ?, updated_at = datetime('now')
WHERE id = ?;

-- name: CreatePipelineRun :exec
INSERT INTO pipeline_runs (
    id, workflow_id, project, work_order_type,
    plan_file, plan_task_id, plan_epic_id
) VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: UpdatePipelineRunScope :exec
UPDATE pipeline_runs
SET scope_started_at = ?, scope_completed_at = ?,
    scope_tokens_in = ?, scope_tokens_out = ?,
    scope_model = ?,
    scope_files_suggested = ?,
    scope_estimated_complexity = ?,
    scope_rag_direct = ?, scope_rag_hops = ?,
    scope_paths_stripped = ?, scope_paths_reclassified = ?,
    updated_at = datetime('now')
WHERE workflow_id = ?;

-- name: UpdatePipelineRunBuild :exec
UPDATE pipeline_runs
SET build_started_at = ?, build_completed_at = ?,
    build_files_changed = ?,
    build_tokens_in = ?, build_tokens_out = ?,
    build_model = ?,
    build_cost_usd = ?, build_session_id = ?,
    build_tool_calls = ?,
    build_claude_md_content = ?,
    updated_at = datetime('now')
WHERE workflow_id = ?;

-- name: UpdatePipelineRunVerify :exec
UPDATE pipeline_runs
SET verify_started_at = ?, verify_completed_at = ?,
    verify_tokens_in = ?, verify_tokens_out = ?,
    verify_model = ?, verify_result = ?,
    build_scope_drift = ?,
    updated_at = datetime('now')
WHERE workflow_id = ?;

-- name: UpdatePipelineRunHumanResult :exec
UPDATE pipeline_runs
SET human_result = ?, updated_at = datetime('now')
WHERE workflow_id = ?;

-- name: GetPipelineStats :one
SELECT
    COUNT(*)                                                                AS total_runs,
    SUM(CASE WHEN verify_result = 'PASS' THEN 1 ELSE 0 END)               AS verify_pass,
    SUM(CASE WHEN verify_result = 'WARN' THEN 1 ELSE 0 END)               AS verify_warn,
    SUM(CASE WHEN verify_result = 'FAIL' THEN 1 ELSE 0 END)               AS verify_fail,
    SUM(CASE WHEN human_result = 'approved' THEN 1 ELSE 0 END)            AS human_approved,
    SUM(CASE WHEN human_result = 'rejected' THEN 1 ELSE 0 END)            AS human_rejected,
    SUM(CASE WHEN human_result IS NULL THEN 1 ELSE 0 END)                 AS human_pending,
    AVG(CASE
        WHEN scope_started_at IS NOT NULL AND scope_completed_at IS NOT NULL
        THEN CAST(strftime('%s', scope_completed_at) AS INTEGER) - CAST(strftime('%s', scope_started_at) AS INTEGER)
    END)                                                                    AS avg_scope_secs,
    AVG(CASE
        WHEN build_started_at IS NOT NULL AND build_completed_at IS NOT NULL
        THEN CAST(strftime('%s', build_completed_at) AS INTEGER) - CAST(strftime('%s', build_started_at) AS INTEGER)
    END)                                                                    AS avg_build_secs,
    AVG(CASE
        WHEN verify_started_at IS NOT NULL AND verify_completed_at IS NOT NULL
        THEN CAST(strftime('%s', verify_completed_at) AS INTEGER) - CAST(strftime('%s', verify_started_at) AS INTEGER)
    END)                                                                    AS avg_verify_secs,
    SUM(COALESCE(scope_tokens_in, 0))                                      AS total_scope_tokens_in,
    SUM(COALESCE(scope_tokens_out, 0))                                     AS total_scope_tokens_out,
    SUM(COALESCE(build_tokens_in, 0))                                      AS total_build_tokens_in,
    SUM(COALESCE(build_tokens_out, 0))                                     AS total_build_tokens_out,
    SUM(COALESCE(verify_tokens_in, 0))                                     AS total_verify_tokens_in,
    SUM(COALESCE(verify_tokens_out, 0))                                    AS total_verify_tokens_out,
    SUM(COALESCE(build_cost_usd, 0))                                       AS total_build_cost_usd
FROM pipeline_runs;

-- name: GetRecentPipelineRuns :many
SELECT
    workflow_id,
    work_order_type,
    verify_result,
    human_result,
    scope_estimated_complexity,
    CASE
        WHEN verify_result IS NULL OR human_result IS NULL THEN ''
        WHEN verify_result = 'PASS' AND human_result = 'approved' THEN 'match'
        WHEN verify_result = 'WARN' THEN 'match'
        WHEN verify_result = 'FAIL' AND human_result = 'rejected' THEN 'match'
        ELSE 'mismatch'
    END AS agreement,
    COALESCE(scope_tokens_in, 0) + COALESCE(scope_tokens_out, 0) +
    COALESCE(build_tokens_in, 0) + COALESCE(build_tokens_out, 0) +
    COALESCE(verify_tokens_in, 0) + COALESCE(verify_tokens_out, 0) AS total_tokens
FROM pipeline_runs
ORDER BY created_at DESC
LIMIT 5;

-- name: GetVerifyHumanAgreement :many
SELECT
    verify_result,
    human_result,
    COUNT(*) AS count
FROM pipeline_runs
WHERE verify_result IS NOT NULL AND human_result IS NOT NULL
GROUP BY verify_result, human_result;

-- name: GetStatsByWorkOrderType :many
SELECT
    work_order_type,
    COUNT(*) AS total,
    SUM(CASE WHEN verify_result = 'PASS' THEN 1 ELSE 0 END) AS verify_pass,
    SUM(CASE WHEN verify_result = 'WARN' THEN 1 ELSE 0 END) AS verify_warn,
    SUM(CASE WHEN verify_result = 'FAIL' THEN 1 ELSE 0 END) AS verify_fail,
    SUM(CASE WHEN human_result = 'approved' THEN 1 ELSE 0 END) AS human_approved,
    SUM(CASE WHEN human_result = 'rejected' THEN 1 ELSE 0 END) AS human_rejected,
    AVG(CASE
        WHEN scope_started_at IS NOT NULL AND scope_completed_at IS NOT NULL
        THEN CAST(strftime('%s', scope_completed_at) AS INTEGER) - CAST(strftime('%s', scope_started_at) AS INTEGER)
    END) AS avg_scope_secs
FROM pipeline_runs
WHERE work_order_type IS NOT NULL
GROUP BY work_order_type;

-- name: UpdatePipelineRunWorkOrderContent :exec
UPDATE pipeline_runs
SET work_order_content = ?, updated_at = datetime('now')
WHERE workflow_id = ?;

-- name: GetScopeQualityStats :one
SELECT
    AVG(scope_paths_stripped) AS avg_paths_stripped,
    AVG(scope_paths_reclassified) AS avg_paths_reclassified,
    SUM(CASE WHEN scope_estimated_complexity = 'low' THEN 1 ELSE 0 END) AS complexity_low,
    SUM(CASE WHEN scope_estimated_complexity = 'medium' THEN 1 ELSE 0 END) AS complexity_medium,
    SUM(CASE WHEN scope_estimated_complexity = 'high' THEN 1 ELSE 0 END) AS complexity_high
FROM pipeline_runs;

-- name: InsertSubCall :exec
INSERT INTO sub_calls (
    pipeline_run_id, phase, step, target_path,
    provider, model, tokens_in, tokens_out,
    latency_ms, estimated_cost_usd, success, error_message
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetSubCallsByPipelineRun :many
SELECT id, pipeline_run_id, phase, step, target_path,
       provider, model, tokens_in, tokens_out,
       latency_ms, estimated_cost_usd, success, error_message, created_at
FROM sub_calls
WHERE pipeline_run_id = ?
ORDER BY created_at;

-- name: GetPipelineRunIDByWorkflowID :one
SELECT id FROM pipeline_runs WHERE workflow_id = ? LIMIT 1;

-- name: GetPipelineRunByWorkflowID :one
SELECT id, build_claude_md_content, plan_file, plan_task_id, plan_epic_id
FROM pipeline_runs
WHERE workflow_id = ?
LIMIT 1;

-- name: CountSuccessfulApprovedRunsByPlanTaskID :one
SELECT COUNT(*)
FROM pipeline_runs
WHERE plan_file = ?
  AND plan_task_id = ?
  AND verify_result = 'PASS'
  AND human_result = 'approved';

-- name: ListLatestPipelineRunsByPlanFile :many
SELECT
    latest.plan_task_id,
    latest.workflow_id,
    latest.verify_result,
    latest.human_result,
    latest.current_state
FROM (
    SELECT
        pr.plan_task_id,
        pr.workflow_id,
        pr.verify_result,
        pr.human_result,
        wf.current_state,
        ROW_NUMBER() OVER (
            PARTITION BY pr.plan_task_id
            ORDER BY pr.created_at DESC, pr.id DESC
        ) AS row_num
    FROM pipeline_runs pr
    JOIN workflows wf ON wf.id = pr.workflow_id
    WHERE pr.plan_file = ?
      AND pr.plan_task_id IS NOT NULL
) AS latest
WHERE latest.row_num = 1
ORDER BY latest.plan_task_id ASC;

-- name: GetSubCallAggregatesByProvider :many
SELECT
    provider,
    SUM(tokens_in) AS total_tokens_in,
    SUM(tokens_out) AS total_tokens_out,
    SUM(estimated_cost_usd) AS total_estimated_cost_usd,
    COUNT(*) AS call_count
FROM sub_calls
WHERE pipeline_run_id = ?
GROUP BY provider;

-- name: GetSubCallGlobalStatsByProvider :many
SELECT
    provider,
    SUM(tokens_in) AS total_tokens_in,
    SUM(tokens_out) AS total_tokens_out,
    SUM(estimated_cost_usd) AS total_estimated_cost_usd,
    COUNT(*) AS call_count
FROM sub_calls
GROUP BY provider;

-- name: GetSubCallPhaseAverages :many
SELECT
    phase,
    COUNT(*) AS total_calls,
    COUNT(DISTINCT pipeline_run_id) AS run_count
FROM sub_calls
GROUP BY phase;

-- name: InsertPlanRun :exec
INSERT INTO plan_runs (
    id, spec_file, project, spec_fingerprint,
    generation_model, epic_generation_model, task_generation_model, audit_model,
    generation_session_id, audit_session_id,
    work_orders_generated, epic_count, task_count,
    pre_audit_work_order_count, post_audit_work_order_count, audit_change_text,
    audit_work_orders_added, audit_work_orders_modified, audit_work_orders_unchanged,
    generation_cost_usd, epic_generation_cost_usd, task_generation_cost_usd, audit_cost_usd,
    generation_duration_ms, epic_generation_duration_ms, task_generation_duration_ms, audit_duration_ms,
    generation_retry_count,
    generation_tokens_in, generation_tokens_out,
    epic_generation_tokens_in, epic_generation_tokens_out,
    task_generation_call_count, task_generation_tokens_in, task_generation_tokens_out,
    audit_tokens_in, audit_tokens_out
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
