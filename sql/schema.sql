CREATE TABLE workflows (
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

CREATE TABLE tasks (
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

CREATE INDEX idx_tasks_workflow ON tasks(workflow_id);
CREATE INDEX idx_tasks_state ON tasks(state);
CREATE INDEX idx_tasks_claimed ON tasks(state, claimed_at);

CREATE TABLE events (
                        id                  INTEGER PRIMARY KEY AUTOINCREMENT,
                        workflow_id         TEXT REFERENCES workflows(id),
                        task_id             TEXT REFERENCES tasks(id),

                        event_type          TEXT NOT NULL,
                        event_data          TEXT, -- JSON blob

                        created_at          TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_events_workflow ON events(workflow_id);
