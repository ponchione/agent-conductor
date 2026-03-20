export type SessionState =
  | "running"
  | "awaiting_review"
  | "completed"
  | "failed"
  | "partial";

export type PipelineEventType =
  | "phase_start"
  | "phase_complete"
  | "phase_error"
  | "build_stdout"
  | "scope_step"
  | "verify_precheck"
  | "verify_result"
  | "run_complete"
  | "run_awaiting_review";

export interface SessionSummary {
  id: string;
  kind: string;
  project: string;
  source_spec_path?: string;
  state: SessionState;
  error_message?: string;
  started_at?: string;
  completed_at?: string;
  created_at: string;
  updated_at: string;
  plan_runs_count: number;
  pipeline_runs_count: number;
  latest_workflow_state?: string;
}

export interface SessionListResponse {
  sessions: SessionSummary[];
}

export interface SessionRecord {
  id: string;
  kind: string;
  project: string;
  source_spec_path?: string;
  state: SessionState;
  error_message?: string;
  started_at?: string;
  completed_at?: string;
  created_at: string;
  updated_at: string;
}

export interface PlanRun {
  id: string;
  spec_file: string;
  project?: string;
  spec_fingerprint?: string;
  work_orders_generated?: number;
  pre_audit_work_order_count?: number;
  post_audit_work_order_count?: number;
  work_order_delta?: number;
  audit_changes?: string[];
  created_at: string;
}

export interface PipelineRun {
  id: string;
  workflow_id: string;
  project: string;
  work_order_type?: string;
  verify_result?: string;
  human_result?: string;
  created_at: string;
}

export interface Artifact {
  id: string;
  workflow_id?: string;
  task_id?: string;
  artifact_type: string;
  path: string;
  size_bytes?: number;
  metadata?: unknown;
  created_at: string;
}

export interface SessionDetailResponse {
  session: SessionRecord;
  plan_runs: PlanRun[];
  pipeline_runs: PipelineRun[];
  artifacts: Artifact[];
}

export interface PlanAuditSummary {
  changed_runs: number;
  unchanged_runs: number;
  total_runs: number;
}

export interface PlanAuditRun {
  id: string;
  session_id?: string;
  project?: string;
  spec_file: string;
  audit_changed: boolean;
  work_orders_generated?: number;
  pre_audit_work_order_count?: number;
  post_audit_work_order_count?: number;
  work_order_delta?: number;
  audit_changes?: string[];
  created_at: string;
}

export interface PlanAuditStatsResponse {
  summary: PlanAuditSummary;
  recent_runs: PlanAuditRun[];
}

export type EventPayload = Record<string, unknown> | string | null;

export interface EventStreamEnvelope {
  id: number;
  workflow_id?: string;
  task_id?: string;
  event_type: PipelineEventType;
  event_data: EventPayload;
  created_at: string;
}

export type ShellView = "sessions" | "audit" | "live";
