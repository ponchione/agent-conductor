CREATE TABLE IF NOT EXISTS workflows (
    id                  TEXT PRIMARY KEY,
    original_intent     TEXT NOT NULL,
    original_file       TEXT NOT NULL,
    current_state       TEXT NOT NULL,
    target_repo         TEXT NOT NULL,
    git_branch          TEXT NOT NULL,

    -- New fields for 3-phase execution
    context_package_path     TEXT,
    verification_report_path TEXT,

    -- Budget Limits
    max_depth           INTEGER NOT NULL DEFAULT 5,
    max_files_changed   INTEGER NOT NULL DEFAULT 50,
    max_duration_mins   INTEGER NOT NULL DEFAULT 60,

    -- Budget Consumed
    current_depth       INTEGER NOT NULL DEFAULT 0,
    files_changed       INTEGER NOT NULL DEFAULT 0,
    started_at          TEXT,

    -- Metadata
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at          TEXT NOT NULL DEFAULT (datetime('now')),
    completed_at        TEXT,
    error_message       TEXT
);

CREATE TABLE IF NOT EXISTS tasks (
    id                  TEXT PRIMARY KEY,
    workflow_id         TEXT NOT NULL REFERENCES workflows(id),
    sequence_num        INTEGER NOT NULL,

    task_type           TEXT NOT NULL,
    agent_type          TEXT NOT NULL,
    target_repo         TEXT NOT NULL,

    -- New field for 3-phase execution
    phase               TEXT NOT NULL DEFAULT 'scope', -- scope, build, verify, human_review, complete, failed

    input_artifact      TEXT NOT NULL,
    output_artifact     TEXT,

    state               TEXT NOT NULL DEFAULT 'pending',
    claimed_by          TEXT,
    claimed_at          TEXT,

    attempts            INTEGER NOT NULL DEFAULT 0,
    max_attempts        INTEGER NOT NULL DEFAULT 2,

    -- Results
    exit_code           INTEGER,
    stdout_log          TEXT,
    stderr_log          TEXT,
    files_changed       TEXT, -- JSON array

    -- Timing
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    started_at          TEXT,
    completed_at        TEXT,

    error_message       TEXT
);

CREATE INDEX IF NOT EXISTS idx_tasks_workflow ON tasks(workflow_id);
CREATE INDEX IF NOT EXISTS idx_tasks_state ON tasks(state);
CREATE INDEX IF NOT EXISTS idx_tasks_claimed ON tasks(state, claimed_at);

CREATE TABLE IF NOT EXISTS events (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    workflow_id         TEXT REFERENCES workflows(id),
    task_id             TEXT REFERENCES tasks(id),

    event_type          TEXT NOT NULL,
    event_data          TEXT, -- JSON blob

    created_at          TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_events_workflow ON events(workflow_id);

CREATE TABLE IF NOT EXISTS pipeline_runs (
    id                      TEXT PRIMARY KEY,
    workflow_id             TEXT NOT NULL REFERENCES workflows(id),
    project                 TEXT NOT NULL,
    work_order_type         TEXT,

    -- Phase timing
    scope_started_at        TEXT,
    scope_completed_at      TEXT,
    build_started_at        TEXT,
    build_completed_at      TEXT,
    verify_started_at       TEXT,
    verify_completed_at     TEXT,

    -- Model tracking
    scope_model             TEXT,
    build_model             TEXT,
    verify_model            TEXT,

    -- Outcome
    scope_files_suggested   INTEGER,
    build_files_changed     INTEGER,
    build_scope_drift       INTEGER NOT NULL DEFAULT 0,
    scope_estimated_complexity TEXT,
    scope_rag_direct           INTEGER,
    scope_rag_hops             INTEGER,
    scope_paths_stripped        INTEGER,
    scope_paths_reclassified   INTEGER,
    verify_result           TEXT,
    human_result            TEXT,
    work_order_content      TEXT,

    -- Token tracking (NULL if unavailable)
    scope_tokens_in         INTEGER,
    scope_tokens_out        INTEGER,
    build_tokens_in         INTEGER,
    build_tokens_out        INTEGER,
    verify_tokens_in        INTEGER,
    verify_tokens_out       INTEGER,

    created_at              TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at              TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_pipeline_runs_workflow ON pipeline_runs(workflow_id);

CREATE TABLE IF NOT EXISTS sub_calls (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    pipeline_run_id     TEXT NOT NULL REFERENCES pipeline_runs(id),
    phase               TEXT NOT NULL,
    step                TEXT NOT NULL,
    target_path         TEXT,
    provider            TEXT NOT NULL,
    model               TEXT NOT NULL,
    tokens_in           INTEGER NOT NULL DEFAULT 0,
    tokens_out          INTEGER NOT NULL DEFAULT 0,
    latency_ms          INTEGER NOT NULL DEFAULT 0,
    estimated_cost_usd  REAL NOT NULL DEFAULT 0.0,
    success             INTEGER NOT NULL DEFAULT 1,
    error_message       TEXT,
    created_at          TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_sub_calls_pipeline_run ON sub_calls(pipeline_run_id);
CREATE INDEX IF NOT EXISTS idx_sub_calls_phase_step ON sub_calls(phase, step);
