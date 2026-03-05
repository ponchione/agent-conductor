package worker

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/scope"
)

// persistSubCalls looks up the pipeline_run_id for a workflow and inserts each
// SubCallRecord into the sub_calls table. Errors are logged but not fatal.
func (w *Worker) persistSubCalls(ctx context.Context, workflowID string, records []scope.SubCallRecord) {
	if len(records) == 0 {
		return
	}

	pipelineRunID, err := w.db.GetPipelineRunIDByWorkflowID(ctx, workflowID)
	if err != nil {
		slog.Warn("Could not resolve pipeline_run_id for sub-call persistence",
			"workflow", workflowID, "error", err)
		return
	}

	for _, r := range records {
		if err := w.db.InsertSubCall(ctx, database.InsertSubCallParams{
			PipelineRunID:    pipelineRunID,
			Phase:            r.Phase,
			Step:             r.Step,
			TargetPath:       sql.NullString{String: r.TargetPath, Valid: r.TargetPath != ""},
			Provider:         r.Provider,
			Model:            r.Model,
			TokensIn:         int64(r.TokensIn),
			TokensOut:        int64(r.TokensOut),
			LatencyMs:        r.LatencyMs,
			EstimatedCostUsd: r.EstimatedCostUSD,
			Success:          boolToInt(r.Success),
			ErrorMessage:     sql.NullString{String: r.ErrorMessage, Valid: r.ErrorMessage != ""},
		}); err != nil {
			slog.Warn("Failed to insert sub-call record",
				"phase", r.Phase, "step", r.Step, "error", err)
		}
	}
}

// tokenAggregate holds summed token counts and the model name from the last
// successful call for a given synthesis step.
type tokenAggregate struct {
	tokensIn  int
	tokensOut int
	model     string
}

// aggregateTokens sums TokensIn/TokensOut across all records and returns the
// model name from the last successful call matching synthStep.
func aggregateTokens(records []scope.SubCallRecord, synthStep string) tokenAggregate {
	var agg tokenAggregate
	for _, r := range records {
		agg.tokensIn += r.TokensIn
		agg.tokensOut += r.TokensOut
		if r.Success && r.Step == synthStep {
			agg.model = r.Model
		}
	}
	return agg
}
