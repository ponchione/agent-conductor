-- ALTER TABLE fallbacks for existing databases that already have the plan_runs table
-- without the new state tracking columns. SQLite errors with "duplicate column name"
-- if the column already exists, which applyDDL tolerates.
ALTER TABLE plan_runs ADD COLUMN state TEXT NOT NULL DEFAULT 'pending';
ALTER TABLE plan_runs ADD COLUMN error_message TEXT;
ALTER TABLE plan_runs ADD COLUMN current_phase TEXT;

-- Backfill: mark historical rows so they don't appear as in-progress.
-- Rows with work_orders_generated are successful completions.
UPDATE plan_runs SET state = 'complete' WHERE state = 'pending' AND work_orders_generated IS NOT NULL;
-- Remaining 'pending' rows that have a created_at older than 2 hours are stale failures.
UPDATE plan_runs SET state = 'failed', error_message = 'legacy run — no completion recorded' WHERE state = 'pending' AND created_at < datetime('now', '-2 hours');
