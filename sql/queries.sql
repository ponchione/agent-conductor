-- name: UpdateWorkflowContext :exec
UPDATE workflows
SET context_package_path = ?, updated_at = datetime('now')
WHERE id = ?;

-- name: UpdateWorkflowVerification :exec
UPDATE workflows
SET verification_report_path = ?, current_state = ?, updated_at = datetime('now')
WHERE id = ?;
