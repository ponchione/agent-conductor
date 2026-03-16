package database

import "github.com/ponchione/agent-conductor/internal/database/dbgen"

// Core types
type Queries = dbgen.Queries
type DBTX = dbgen.DBTX
type Querier = dbgen.Querier

// Model types
type Event = dbgen.Event
type PipelineRun = dbgen.PipelineRun
type SubCall = dbgen.SubCall
type Task = dbgen.Task
type Workflow = dbgen.Workflow

// Params types
type ClaimTaskParams = dbgen.ClaimTaskParams
type CompleteTaskParams = dbgen.CompleteTaskParams
type CreateEventParams = dbgen.CreateEventParams
type CreatePipelineRunParams = dbgen.CreatePipelineRunParams
type CreateTaskParams = dbgen.CreateTaskParams
type CreateWorkflowParams = dbgen.CreateWorkflowParams
type FailTaskParams = dbgen.FailTaskParams
type InsertPlanRunParams = dbgen.InsertPlanRunParams
type InsertSubCallParams = dbgen.InsertSubCallParams
type UpdatePipelineRunBuildParams = dbgen.UpdatePipelineRunBuildParams
type UpdatePipelineRunHumanResultParams = dbgen.UpdatePipelineRunHumanResultParams
type UpdatePipelineRunScopeParams = dbgen.UpdatePipelineRunScopeParams
type UpdatePipelineRunVerifyParams = dbgen.UpdatePipelineRunVerifyParams
type UpdatePipelineRunWorkOrderContentParams = dbgen.UpdatePipelineRunWorkOrderContentParams
type UpdateWorkflowBudgetParams = dbgen.UpdateWorkflowBudgetParams
type UpdateWorkflowContextParams = dbgen.UpdateWorkflowContextParams
type UpdateWorkflowStateParams = dbgen.UpdateWorkflowStateParams
type UpdateWorkflowVerificationParams = dbgen.UpdateWorkflowVerificationParams

// Row types
type GetPipelineRunByWorkflowIDRow = dbgen.GetPipelineRunByWorkflowIDRow
type GetPipelineStatsRow = dbgen.GetPipelineStatsRow
type GetRecentPipelineRunsRow = dbgen.GetRecentPipelineRunsRow
type GetScopeQualityStatsRow = dbgen.GetScopeQualityStatsRow
type GetStatsByWorkOrderTypeRow = dbgen.GetStatsByWorkOrderTypeRow
type GetSubCallAggregatesByProviderRow = dbgen.GetSubCallAggregatesByProviderRow
type GetSubCallGlobalStatsByProviderRow = dbgen.GetSubCallGlobalStatsByProviderRow
type GetSubCallPhaseAveragesRow = dbgen.GetSubCallPhaseAveragesRow
type GetVerifyHumanAgreementRow = dbgen.GetVerifyHumanAgreementRow

// New creates a new Queries instance.
var New = dbgen.New
