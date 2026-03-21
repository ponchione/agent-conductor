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
  current?: QueueItem;
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
  session_id: string;
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
