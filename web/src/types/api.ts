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

export type PlanRunState =
  | "pending"
  | "generating_epics"
  | "generating_tasks"
  | "auditing"
  | "persisting"
  | "complete"
  | "failed";

export const TERMINAL_STATES: PlanRunState[] = ["complete", "failed"];

export interface PlanRunDetailResponse {
  id: string;
  state: PlanRunState;
  current_phase: string | null;
  error_message: string | null;
  session_id?: string;
  project?: string;
  spec_file: string;
  spec_fingerprint?: string;
  epic_count?: number;
  task_count?: number;
  work_orders_generated?: number;
  pre_audit_work_order_count?: number;
  post_audit_work_order_count?: number;
  work_order_delta?: number;
  audit_changes?: string[];
  audit_change_text?: string;
  audit_work_orders_added?: number;
  audit_work_orders_modified?: number;
  audit_work_orders_unchanged?: number;
  generation_cost_usd?: number;
  generation_duration_ms?: number;
  epic_generation_cost_usd?: number;
  epic_generation_duration_ms?: number;
  task_generation_cost_usd?: number;
  task_generation_duration_ms?: number;
  audit_cost_usd?: number;
  audit_duration_ms?: number;
  generation_model?: string;
  epic_generation_model?: string;
  task_generation_model?: string;
  audit_model?: string;
  generation_retry_count?: number;
  created_at: string;
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
  state?: PlanRunState;
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

export interface WorkflowSummary {
  id: string;
  current_state: string;
  work_order_title: string;
  work_order_type?: string;
  project: string;
  session_id?: string;
  git_branch: string;
  verify_result?: string;
  human_result?: string;
  total_cost_usd?: number;
  total_duration_seconds?: number;
  created_at: string;
  updated_at: string;
}

export interface WorkflowListResponse {
  workflows: WorkflowSummary[];
  total: number;
}

export interface WorkflowRecord {
  id: string;
  current_state: string;
  original_intent: string;
  original_file: string;
  git_branch: string;
  target_repo: string;
  created_at: string;
  updated_at: string;
  error_message?: string;
}

export interface PipelineRunDetail {
  id: string;
  work_order_type?: string;
  work_order_content?: string;
  verify_result?: string;
  human_result?: string;
  scope_started_at?: string;
  scope_completed_at?: string;
  build_started_at?: string;
  build_completed_at?: string;
  verify_started_at?: string;
  verify_completed_at?: string;
  scope_model?: string;
  build_model?: string;
  verify_model?: string;
  scope_files_suggested?: number;
  build_files_changed?: number;
  build_scope_drift?: number;
  scope_estimated_complexity?: string;
  scope_rag_direct?: number;
  scope_rag_hops?: number;
  scope_paths_stripped?: number;
  scope_paths_reclassified?: number;
  build_cost_usd?: number;
  build_tool_calls?: string;
}

export interface SubCall {
  phase: string;
  step: string;
  provider: string;
  model: string;
  tokens_in: number;
  tokens_out: number;
  latency_ms: number;
  estimated_cost_usd: number;
  success: boolean;
  error_message?: string;
}

export interface WorkflowDetailResponse {
  workflow: WorkflowRecord;
  pipeline_run: PipelineRunDetail | null;
  sub_calls: SubCall[];
}

// --- Spec 03: Interactive Pipeline types ---

export interface DiffStats {
  additions: number;
  deletions: number;
  files: number;
}

export interface WorkflowDiffResponse {
  diff: string;
  base_branch: string;
  workflow_branch: string;
  files_changed: string[];
  stats: DiffStats;
}

export interface WorkflowScopeResponse {
  context_package: Record<string, unknown>;
  source: string;
}

export interface ApproveResponse {
  status: string;
  workflow_id: string;
  merged: boolean;
  reindexed: boolean;
}

export interface RejectResponse {
  status: string;
  workflow_id: string;
}

// --- Spec 04: Queue System types ---

export type QueueItemStatus = 'pending' | 'executing' | 'completed' | 'failed' | 'skipped';
export type QueueStateValue = 'idle' | 'ready' | 'running' | 'paused' | 'completed';

export interface QueueItem {
  id: string;
  work_order_file: string;
  title: string;
  type: string;
  target_module: string;
  overrides: Record<string, string>;
  status: QueueItemStatus;
  workflow_id?: string;
  error?: string;
  added_at: string;
}

export interface QueueState {
  state: QueueStateValue;
  items: QueueItem[];
  current?: string;
  pause_reason?: string;
}

export interface QueueAddItem {
  work_order_file: string;
  overrides?: Record<string, string>;
}

// --- Spec 05: Plan Space types ---

export interface WorkOrderFile {
  filename: string;
  title: string;
  type: string;
  target_module: string;
  size_bytes: number;
  modified_at: string;
}

export interface WorkOrderListResponse {
  work_orders: WorkOrderFile[];
  directory: string;
}

export interface WorkOrderContentResponse {
  filename: string;
  content: string;
}

export interface WorkOrderUpdateResponse {
  filename: string;
  content: string;
  valid: boolean;
}

export interface PlanSubmitResponse {
  plan_run_id?: string;
  session_id: string;
  state?: PlanRunState;
  status: string;
}

// --- Spec 06: Config & Override types ---

export interface RoleConfig {
  name: string;
  current_provider: string;
  current_model: string;
  description: string;
}

export interface ProviderModels {
  provider: string;
  models: string[];
}

export interface ProjectInfo {
  name: string;
  path: string;
  data_dir: string;
}

export interface ModelOverride {
  provider: string;
  model: string;
}

export interface ConfigRolesResponse {
  roles: RoleConfig[];
  available_models: ProviderModels[];
  project: ProjectInfo;
}

export interface ConfigOverridesResponse {
  overrides: Record<string, ModelOverride>;
}

// --- Spec 07: Analytics types ---

export interface StatsSummaryResponse {
  range: string;
  pipeline: {
    total_runs: number;
    pass_count: number;
    fail_count: number;
    error_count: number;
    pass_rate: number;
    avg_cost_usd: number;
    avg_duration_seconds: number;
    total_cost_usd: number;
    verify_human_agreement_rate: number;
  };
  plan: {
    total_runs: number;
    avg_generation_cost_usd: number;
    avg_audit_cost_usd: number;
    total_cost_usd: number;
    avg_work_orders_per_plan: number;
    avg_audit_delta: number;
  };
  combined_total_cost_usd: number;
}

export interface PipelineRunTrend {
  id: string;
  date: string;
  pipeline_cost_usd: number;
  scope_duration_seconds: number;
  build_duration_seconds: number;
  verify_duration_seconds: number;
  total_duration_seconds: number;
  verify_result: string;
  human_result?: string;
  human_agreed?: boolean;
  scope_model?: string;
  build_model?: string;
  verify_model?: string;
  scope_files_suggested?: number;
  scope_paths_stripped?: number;
  scope_paths_reclassified?: number;
  build_files_changed?: number;
  build_scope_drift?: number;
}

export interface PlanRunTrend {
  id: string;
  date: string;
  generation_cost_usd: number;
  audit_cost_usd: number;
  total_cost_usd: number;
  work_orders_generated: number;
  pre_audit_count: number;
  post_audit_count: number;
  audit_delta: number;
  generation_model?: string;
  audit_model?: string;
}

export interface StatsTrendsResponse {
  pipeline_runs: PipelineRunTrend[];
  plan_runs: PlanRunTrend[];
}

export interface ModelStat {
  model: string;
  provider: string;
  role: string;
  run_count: number;
  avg_cost_usd: number;
  avg_duration_seconds: number;
  avg_tokens_in: number;
  avg_tokens_out: number;
  pass_rate: number | null;
}

export interface StatsModelsResponse {
  models: ModelStat[];
}
