import type { PipelineRunDetail, SubCall } from "@/types/api";
import { MetricCard } from "@/components/MetricCard";
import { StatusBadge } from "@/components/StatusBadge";
import { TimeAgo } from "@/components/TimeAgo";

interface WorkflowOverviewProps {
  pipelineRun: PipelineRunDetail | null;
  subCalls: SubCall[];
}

function formatDuration(startedAt?: string, completedAt?: string): string {
  if (!startedAt) return "\u2014";
  if (!completedAt) return "In progress";

  const start = new Date(startedAt).getTime();
  const end = new Date(completedAt).getTime();
  const totalSeconds = Math.floor((end - start) / 1000);

  if (totalSeconds < 0) return "\u2014";

  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;

  if (hours > 0) {
    return minutes > 0 ? `${hours}h ${minutes}m` : `${hours}h`;
  }
  if (minutes > 0) {
    return seconds > 0 ? `${minutes}m ${seconds}s` : `${minutes}m`;
  }
  return `${seconds}s`;
}

function derivePhaseStatus(startedAt?: string, completedAt?: string): string {
  if (completedAt) return "completed";
  if (startedAt) return "running";
  return "pending";
}

function formatCost(value: number): string {
  return `$${value.toFixed(2)}`;
}

function formatTokenCount(n: number): string {
  if (n >= 1_000_000) {
    return `${(n / 1_000_000).toFixed(1)}M`;
  }
  if (n >= 1_000) {
    return `${(n / 1_000).toFixed(1)}K`;
  }
  return String(n);
}

interface PhaseRow {
  phase: string;
  model?: string;
  startedAt?: string;
  completedAt?: string;
}

export default function WorkflowOverview({ pipelineRun, subCalls }: WorkflowOverviewProps) {
  if (!pipelineRun) {
    return (
      <div className="flex items-center justify-center p-6 text-sm text-muted-foreground">
        No pipeline run data available
      </div>
    );
  }

  const phases: PhaseRow[] = [
    {
      phase: "Scope",
      model: pipelineRun.scope_model,
      startedAt: pipelineRun.scope_started_at,
      completedAt: pipelineRun.scope_completed_at,
    },
    {
      phase: "Build",
      model: pipelineRun.build_model,
      startedAt: pipelineRun.build_started_at,
      completedAt: pipelineRun.build_completed_at,
    },
    {
      phase: "Verify",
      model: pipelineRun.verify_model,
      startedAt: pipelineRun.verify_started_at,
      completedAt: pipelineRun.verify_completed_at,
    },
  ];

  // Cost & token aggregations
  const totalSubCallCost = subCalls.reduce((sum, sc) => sum + sc.estimated_cost_usd, 0);
  const buildCost = pipelineRun.build_cost_usd ?? 0;
  const totalCost = totalSubCallCost + buildCost;

  const scopeCalls = subCalls.filter((sc) => sc.phase === "scope");
  const scopeTokensIn = scopeCalls.reduce((sum, sc) => sum + sc.tokens_in, 0);
  const scopeTokensOut = scopeCalls.reduce((sum, sc) => sum + sc.tokens_out, 0);

  const verifyCalls = subCalls.filter((sc) => sc.phase === "verify");
  const verifyTokensIn = verifyCalls.reduce((sum, sc) => sum + sc.tokens_in, 0);
  const verifyTokensOut = verifyCalls.reduce((sum, sc) => sum + sc.tokens_out, 0);

  return (
    <div className="space-y-8 p-4">
      {/* Section 1: Phase Timing Breakdown */}
      <section>
        <h3 className="mb-3 text-sm font-medium uppercase tracking-wider text-muted-foreground">
          Phase Timing
        </h3>
        <div className="overflow-x-auto rounded-lg border border-border">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-muted/50">
                <th className="px-4 py-2 text-left font-medium text-muted-foreground">Phase</th>
                <th className="px-4 py-2 text-left font-medium text-muted-foreground">Model</th>
                <th className="px-4 py-2 text-left font-medium text-muted-foreground">Started</th>
                <th className="px-4 py-2 text-left font-medium text-muted-foreground">Duration</th>
                <th className="px-4 py-2 text-left font-medium text-muted-foreground">Status</th>
              </tr>
            </thead>
            <tbody>
              {phases.map((row) => (
                <tr key={row.phase} className="border-b border-border last:border-b-0">
                  <td className="px-4 py-2 font-medium">{row.phase}</td>
                  <td className="px-4 py-2 text-muted-foreground">{row.model ?? "\u2014"}</td>
                  <td className="px-4 py-2">
                    {row.startedAt ? <TimeAgo timestamp={row.startedAt} /> : "\u2014"}
                  </td>
                  <td className="px-4 py-2">{formatDuration(row.startedAt, row.completedAt)}</td>
                  <td className="px-4 py-2">
                    <StatusBadge
                      status={derivePhaseStatus(row.startedAt, row.completedAt)}
                      size="sm"
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      {/* Section 2: Cost & Token Summary */}
      <section>
        <h3 className="mb-3 text-sm font-medium uppercase tracking-wider text-muted-foreground">
          Cost &amp; Token Summary
        </h3>
        <div className="grid grid-cols-2 gap-4 md:grid-cols-3 lg:grid-cols-5">
          <MetricCard label="Total Cost" value={formatCost(totalCost)} />
          <MetricCard
            label="Scope Tokens"
            value={formatTokenCount(scopeTokensIn + scopeTokensOut)}
            subtitle={`${formatTokenCount(scopeTokensIn)} in / ${formatTokenCount(scopeTokensOut)} out`}
          />
          <MetricCard label="Build Cost" value={formatCost(buildCost)} />
          <MetricCard
            label="Verify Tokens"
            value={formatTokenCount(verifyTokensIn + verifyTokensOut)}
            subtitle={`${formatTokenCount(verifyTokensIn)} in / ${formatTokenCount(verifyTokensOut)} out`}
          />
          <MetricCard label="Total Sub-Calls" value={subCalls.length} />
        </div>
      </section>

      {/* Section 3: Scope Quality Metrics */}
      <section>
        <h3 className="mb-3 text-sm font-medium uppercase tracking-wider text-muted-foreground">
          Scope Quality Metrics
        </h3>
        <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
          <MetricCard label="Files Suggested" value={pipelineRun.scope_files_suggested ?? 0} />
          <MetricCard label="Files Changed" value={pipelineRun.build_files_changed ?? 0} />
          <MetricCard label="Scope Drift" value={pipelineRun.build_scope_drift ?? 0} />
          <MetricCard label="Paths Stripped" value={pipelineRun.scope_paths_stripped ?? 0} />
          <MetricCard
            label="Paths Reclassified"
            value={pipelineRun.scope_paths_reclassified ?? 0}
          />
          <MetricCard label="RAG Direct Hits" value={pipelineRun.scope_rag_direct ?? 0} />
          <MetricCard label="RAG Hops" value={pipelineRun.scope_rag_hops ?? 0} />
          <MetricCard
            label="Estimated Complexity"
            value={pipelineRun.scope_estimated_complexity ?? "\u2014"}
          />
        </div>
      </section>
    </div>
  );
}
