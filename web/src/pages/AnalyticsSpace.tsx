import { useState, useEffect, useCallback, useMemo } from "react";
import {
  LineChart,
  Line,
  BarChart,
  Bar,
  PieChart,
  Pie,
  Cell,
  ScatterChart,
  Scatter,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
  ReferenceLine,
  Label,
  LabelList,
} from "recharts";
import { MetricCard } from "@/components/MetricCard";
import { getStatsSummary, getStatsTrends, getStatsModels } from "@/api/client";
import type {
  StatsSummaryResponse,
  StatsTrendsResponse,
  StatsModelsResponse,
  PipelineRunTrend,
  ModelStat,
} from "@/types/api";

const COLORS = {
  pipeline: "#3b82f6",
  plan: "#8b5cf6",
  success: "#22c55e",
  failure: "#ef4444",
  warning: "#f59e0b",
  neutral: "#6b7280",
  scopeBlue: "#93c5fd",
  buildBlue: "#3b82f6",
  verifyBlue: "#1d4ed8",
} as const;

function formatCurrency(v: number): string {
  return `$${v.toFixed(2)}`;
}

function formatDuration(seconds: number): string {
  const m = Math.floor(seconds / 60);
  const s = Math.round(seconds % 60);
  return m > 0 ? `${m}m ${s}s` : `${s}s`;
}

function formatPercent(v: number): string {
  return `${(v * 100).toFixed(1)}%`;
}

function ChartCard({
  title,
  children,
  empty,
  loading,
}: {
  title: string;
  children: React.ReactNode;
  empty?: boolean;
  loading?: boolean;
}) {
  return (
    <div className="rounded-lg border border-border p-4">
      <h3 className="mb-4 text-sm font-medium">{title}</h3>
      {loading ? (
        <div className="flex h-64 items-center justify-center">
          <p className="text-sm text-muted-foreground">Loading...</p>
        </div>
      ) : empty ? (
        <div className="flex h-64 items-center justify-center">
          <p className="text-sm text-muted-foreground">
            No data available for the selected time range
          </p>
        </div>
      ) : (
        children
      )}
    </div>
  );
}

export default function AnalyticsSpace() {
  const [range_, setRange] = useState("30d");
  const [project, setProject] = useState("");
  const [summary, setSummary] = useState<StatsSummaryResponse | null>(null);
  const [trends, setTrends] = useState<StatsTrendsResponse | null>(null);
  const [models, setModels] = useState<StatsModelsResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [sortCol, setSortCol] = useState("run_count");
  const [sortDir, setSortDir] = useState<"asc" | "desc">("desc");

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    const proj = project || undefined;
    Promise.all([
      getStatsSummary(range_, proj),
      getStatsTrends(range_, proj),
      getStatsModels(range_),
    ])
      .then(([s, t, m]) => {
        if (!cancelled) {
          setSummary(s);
          setTrends(t);
          setModels(m);
        }
      })
      .catch(() => {
        // leave state as-is on error
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [range_, project]);

  // --- Derived data for charts ---

  const costOverTimeData = useMemo(() => {
    if (!trends) return [];
    const byDate = new Map<
      string,
      { date: string; pipeline_cost: number; plan_cost: number }
    >();
    for (const r of trends.pipeline_runs) {
      const entry = byDate.get(r.date) ?? {
        date: r.date,
        pipeline_cost: 0,
        plan_cost: 0,
      };
      entry.pipeline_cost += r.pipeline_cost_usd;
      byDate.set(r.date, entry);
    }
    for (const r of trends.plan_runs) {
      const entry = byDate.get(r.date) ?? {
        date: r.date,
        pipeline_cost: 0,
        plan_cost: 0,
      };
      entry.plan_cost += r.total_cost_usd;
      byDate.set(r.date, entry);
    }
    return Array.from(byDate.values()).sort((a, b) =>
      a.date.localeCompare(b.date),
    );
  }, [trends]);

  const verifyDistribution = useMemo(() => {
    if (!trends) return [];
    let pass = 0;
    let fail = 0;
    let error = 0;
    for (const r of trends.pipeline_runs) {
      if (r.verify_result === "PASS") pass++;
      else if (r.verify_result === "FAIL") fail++;
      else error++;
    }
    return [
      { name: "PASS", value: pass, color: COLORS.success },
      { name: "FAIL", value: fail, color: COLORS.failure },
      { name: "ERROR", value: error, color: COLORS.warning },
    ].filter((d) => d.value > 0);
  }, [trends]);

  const scatterData = useMemo(() => {
    if (!trends) return { pass: [] as PipelineRunTrend[], fail: [] as PipelineRunTrend[] };
    const withScope = trends.pipeline_runs.filter(
      (r) => r.scope_files_suggested !== undefined,
    );
    return {
      pass: withScope.filter((r) => r.verify_result === "PASS"),
      fail: withScope.filter((r) => r.verify_result !== "PASS"),
    };
  }, [trends]);

  const agreementData = useMemo(() => {
    if (!trends) return [];
    const withBoth = trends.pipeline_runs.filter(
      (r) => r.human_agreed !== undefined,
    );
    return withBoth.map((run, idx) => {
      const start = Math.max(0, idx - 9);
      const window = withBoth.slice(start, idx + 1);
      const agreed = window.filter((r) => r.human_agreed === true).length;
      return { date: run.date, rate: (agreed / window.length) * 100 };
    });
  }, [trends]);

  const auditChartData = useMemo(() => {
    if (!trends) return [];
    return trends.plan_runs.map((r) => ({
      date: r.date,
      pre_audit: r.pre_audit_count,
      audit_added: Math.max(0, r.post_audit_count - r.pre_audit_count),
      delta: r.audit_delta,
      post_audit_count: r.post_audit_count,
    }));
  }, [trends]);

  const sortedModels = useMemo(() => {
    if (!models) return [];
    const rows = [...models.models];
    rows.sort((a, b) => {
      const key = sortCol as keyof ModelStat;
      const av = a[key];
      const bv = b[key];
      if (av === null || av === undefined) return 1;
      if (bv === null || bv === undefined) return -1;
      if (typeof av === "number" && typeof bv === "number") {
        return sortDir === "asc" ? av - bv : bv - av;
      }
      const sa = String(av);
      const sb = String(bv);
      return sortDir === "asc"
        ? sa.localeCompare(sb)
        : sb.localeCompare(sa);
    });
    return rows;
  }, [models, sortCol, sortDir]);

  const handleSort = useCallback(
    (col: string) => {
      if (sortCol === col) {
        setSortDir((d) => (d === "asc" ? "desc" : "asc"));
      } else {
        setSortCol(col);
        setSortDir("desc");
      }
    },
    [sortCol],
  );

  const sortIndicator = useCallback(
    (col: string) => {
      if (sortCol !== col) return "";
      return sortDir === "asc" ? " \u25B2" : " \u25BC";
    },
    [sortCol, sortDir],
  );

  return (
    <div className="h-full overflow-y-auto p-6">
      {/* Header + Controls */}
      <div className="mb-6 flex items-center justify-between">
        <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground">
          Analytics
        </h2>
        <div className="flex items-center gap-3">
          <select
            className="rounded-md border border-input bg-background px-2 py-1 text-sm"
            value={project}
            onChange={(e) => setProject(e.target.value)}
          >
            <option value="">All Projects</option>
          </select>
          <div className="flex gap-1 rounded-lg border border-border p-1">
            {["7d", "30d", "90d", "all"].map((r) => (
              <button
                key={r}
                onClick={() => setRange(r)}
                className={`rounded-md px-3 py-1 text-sm ${
                  range_ === r
                    ? "bg-accent text-accent-foreground"
                    : "text-muted-foreground hover:text-foreground"
                }`}
              >
                {r === "all" ? "All" : r}
              </button>
            ))}
          </div>
        </div>
      </div>

      {/* Pipeline Summary Cards */}
      {loading || !summary ? (
        <div className="grid grid-cols-5 gap-4">
          {Array.from({ length: 5 }).map((_, i) => (
            <div
              key={i}
              className="h-24 animate-pulse rounded-lg border border-border bg-muted"
            />
          ))}
        </div>
      ) : (
        <div className="grid grid-cols-5 gap-4">
          <MetricCard
            label="Total Runs"
            value={summary.pipeline.total_runs}
            subtitle="pipeline executions"
          />
          <MetricCard
            label="Pass Rate"
            value={formatPercent(summary.pipeline.pass_rate)}
            subtitle={`${summary.pipeline.pass_count} / ${summary.pipeline.total_runs}`}
          />
          <MetricCard
            label="Avg Cost"
            value={formatCurrency(summary.pipeline.avg_cost_usd)}
            subtitle="per pipeline run"
          />
          <MetricCard
            label="Avg Duration"
            value={formatDuration(summary.pipeline.avg_duration_seconds)}
            subtitle="per pipeline run"
          />
          <MetricCard
            label="Verify Agreement"
            value={formatPercent(summary.pipeline.verify_human_agreement_rate)}
            subtitle="verify matches human"
          />
        </div>
      )}

      {/* Plan Summary Cards */}
      {loading || !summary ? (
        <div className="mt-4 grid grid-cols-3 gap-4">
          {Array.from({ length: 3 }).map((_, i) => (
            <div
              key={i}
              className="h-24 animate-pulse rounded-lg border border-border bg-muted"
            />
          ))}
        </div>
      ) : (
        <div className="mt-4 grid grid-cols-3 gap-4">
          <MetricCard label="Plan Runs" value={summary.plan.total_runs} />
          <MetricCard
            label="Avg Plan Cost"
            value={formatCurrency(
              summary.plan.avg_generation_cost_usd +
                summary.plan.avg_audit_cost_usd,
            )}
            subtitle="gen + audit per plan"
          />
          <MetricCard
            label="Combined Total"
            value={formatCurrency(summary.combined_total_cost_usd)}
            subtitle="all runs"
          />
        </div>
      )}

      {/* Charts */}
      <div className="mt-6 space-y-6">
        {/* Chart 1: Cost Over Time */}
        <ChartCard
          title="Cost Over Time"
          loading={loading}
          empty={costOverTimeData.length === 0}
        >
          <ResponsiveContainer width="100%" height={300}>
            <LineChart data={costOverTimeData}>
              <CartesianGrid strokeDasharray="3 3" />
              <XAxis dataKey="date" />
              <YAxis />
              <Tooltip />
              <Legend />
              <Line
                type="monotone"
                dataKey="pipeline_cost"
                name="Pipeline Cost"
                stroke={COLORS.pipeline}
                dot={false}
              />
              <Line
                type="monotone"
                dataKey="plan_cost"
                name="Plan Cost"
                stroke={COLORS.plan}
                dot={false}
              />
            </LineChart>
          </ResponsiveContainer>
        </ChartCard>

        {/* Chart 2: Duration by Phase */}
        <ChartCard
          title="Duration by Phase"
          loading={loading}
          empty={!trends || trends.pipeline_runs.length === 0}
        >
          <ResponsiveContainer width="100%" height={300}>
            <BarChart data={trends?.pipeline_runs ?? []}>
              <CartesianGrid strokeDasharray="3 3" />
              <XAxis dataKey="date" />
              <YAxis />
              <Tooltip />
              <Legend />
              <Bar
                dataKey="scope_duration_seconds"
                name="Scope"
                stackId="duration"
                fill={COLORS.scopeBlue}
              />
              <Bar
                dataKey="build_duration_seconds"
                name="Build"
                stackId="duration"
                fill={COLORS.buildBlue}
              />
              <Bar
                dataKey="verify_duration_seconds"
                name="Verify"
                stackId="duration"
                fill={COLORS.verifyBlue}
              />
            </BarChart>
          </ResponsiveContainer>
        </ChartCard>

        {/* Chart 3 & 4: Two-column grid */}
        <div className="grid grid-cols-2 gap-6">
          {/* Verify Result Distribution */}
          <ChartCard
            title="Verify Result Distribution"
            loading={loading}
            empty={verifyDistribution.length === 0}
          >
            <ResponsiveContainer width="100%" height={300}>
              <PieChart>
                <Pie
                  data={verifyDistribution}
                  dataKey="value"
                  nameKey="name"
                  cx="50%"
                  cy="50%"
                  innerRadius={55}
                  outerRadius={100}
                  label={({
                    name,
                    value,
                  }: {
                    name?: string;
                    value: number;
                  }) => `${name ?? ""}: ${value}`}
                >
                  {verifyDistribution.map((entry, idx) => (
                    <Cell key={idx} fill={entry.color} />
                  ))}
                  <Label
                    value={verifyDistribution.reduce((s, d) => s + d.value, 0)}
                    position="center"
                    className="fill-foreground text-xl font-bold"
                  />
                </Pie>
                <Tooltip />
                <Legend />
              </PieChart>
            </ResponsiveContainer>
          </ChartCard>

          {/* Scope Quality Scatter */}
          <ChartCard
            title="Scope Quality Scatter"
            loading={loading}
            empty={
              scatterData.pass.length === 0 && scatterData.fail.length === 0
            }
          >
            <ResponsiveContainer width="100%" height={300}>
              <ScatterChart>
                <CartesianGrid strokeDasharray="3 3" />
                <XAxis
                  dataKey="scope_files_suggested"
                  name="Files Suggested"
                  type="number"
                />
                <YAxis
                  dataKey="scope_paths_stripped"
                  name="Paths Stripped"
                  type="number"
                />
                <Tooltip
                  content={({ payload }) => {
                    if (!payload || payload.length === 0) return null;
                    const d = payload[0].payload as PipelineRunTrend;
                    return (
                      <div className="rounded border border-border bg-background p-2 text-xs shadow">
                        <p>ID: {d.id}</p>
                        <p>Files: {d.scope_files_suggested}</p>
                        <p>Paths Stripped: {d.scope_paths_stripped}</p>
                        <p>Result: {d.verify_result}</p>
                      </div>
                    );
                  }}
                />
                <Legend />
                <Scatter
                  name="PASS"
                  data={scatterData.pass}
                  fill={COLORS.success}
                />
                <Scatter
                  name="FAIL"
                  data={scatterData.fail}
                  fill={COLORS.failure}
                />
              </ScatterChart>
            </ResponsiveContainer>
          </ChartCard>
        </div>

        {/* Chart 5: Model Comparison Table */}
        <ChartCard
          title="Model Comparison"
          loading={loading}
          empty={sortedModels.length === 0}
        >
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead>
                <tr className="border-b border-border text-xs uppercase tracking-wider text-muted-foreground">
                  {[
                    { key: "model", label: "Model" },
                    { key: "provider", label: "Provider" },
                    { key: "role", label: "Role" },
                    { key: "run_count", label: "Runs" },
                    { key: "avg_cost_usd", label: "Avg Cost" },
                    { key: "avg_duration_seconds", label: "Avg Duration" },
                    { key: "avg_tokens_in", label: "Tokens In" },
                    { key: "avg_tokens_out", label: "Tokens Out" },
                    { key: "pass_rate", label: "Pass Rate" },
                  ].map((col) => (
                    <th
                      key={col.key}
                      className="cursor-pointer whitespace-nowrap px-3 py-2 hover:text-foreground"
                      onClick={() => handleSort(col.key)}
                    >
                      {col.label}
                      {sortIndicator(col.key)}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {sortedModels.map((m, idx) => (
                  <tr key={idx} className="border-b border-border">
                    <td className="whitespace-nowrap px-3 py-2 font-medium">
                      {m.model}
                    </td>
                    <td className="whitespace-nowrap px-3 py-2">
                      {m.provider}
                    </td>
                    <td className="whitespace-nowrap px-3 py-2">{m.role}</td>
                    <td className="whitespace-nowrap px-3 py-2">
                      {m.run_count}
                    </td>
                    <td className="whitespace-nowrap px-3 py-2">
                      {formatCurrency(m.avg_cost_usd)}
                    </td>
                    <td className="whitespace-nowrap px-3 py-2">
                      {formatDuration(m.avg_duration_seconds)}
                    </td>
                    <td className="whitespace-nowrap px-3 py-2">
                      {Math.round(m.avg_tokens_in).toLocaleString()}
                    </td>
                    <td className="whitespace-nowrap px-3 py-2">
                      {Math.round(m.avg_tokens_out).toLocaleString()}
                    </td>
                    <td className="whitespace-nowrap px-3 py-2">
                      {m.pass_rate !== null
                        ? formatPercent(m.pass_rate)
                        : "N/A"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </ChartCard>

        {/* Chart 6: Verify-Human Agreement Trend */}
        <ChartCard
          title="Verify-Human Agreement Trend (Rolling 10-run)"
          loading={loading}
          empty={agreementData.length === 0}
        >
          <ResponsiveContainer width="100%" height={300}>
            <LineChart data={agreementData}>
              <CartesianGrid strokeDasharray="3 3" />
              <XAxis dataKey="date" />
              <YAxis domain={[0, 100]} />
              <Tooltip />
              <Legend />
              <Line
                type="monotone"
                dataKey="rate"
                name="Agreement %"
                stroke={COLORS.pipeline}
                dot={false}
              />
              {summary && (
                <ReferenceLine
                  y={summary.pipeline.verify_human_agreement_rate * 100}
                  stroke={COLORS.neutral}
                  strokeDasharray="3 3"
                  label="Overall"
                />
              )}
            </LineChart>
          </ResponsiveContainer>
        </ChartCard>

        {/* Chart 7: Plan Audit Effectiveness */}
        <ChartCard
          title="Plan Audit Effectiveness"
          loading={loading}
          empty={!trends || trends.plan_runs.length === 0}
        >
          <ResponsiveContainer width="100%" height={300}>
            <BarChart data={auditChartData}>
              <CartesianGrid strokeDasharray="3 3" />
              <XAxis dataKey="date" />
              <YAxis />
              <Tooltip
                content={({ payload }) => {
                  if (!payload || payload.length === 0) return null;
                  const d = payload[0].payload as (typeof auditChartData)[0];
                  return (
                    <div className="rounded border border-border bg-background p-2 text-xs shadow">
                      <p>Pre-audit: {d.pre_audit}</p>
                      <p>Post-audit: {d.post_audit_count}</p>
                      <p>Delta: {d.delta >= 0 ? `+${d.delta}` : d.delta}</p>
                    </div>
                  );
                }}
              />
              <Legend />
              <Bar
                dataKey="pre_audit"
                name="Pre-Audit"
                stackId="audit"
                fill={COLORS.neutral}
              />
              <Bar
                dataKey="audit_added"
                name="Audit Added"
                stackId="audit"
                fill={COLORS.plan}
              >
                <LabelList
                  dataKey="delta"
                  position="top"
                  fontSize={11}
                  fill={COLORS.plan}
                  formatter={(v: unknown) => { const n = Number(v); return n === 0 ? "" : n > 0 ? `+${n}` : `${n}`; }}
                />
              </Bar>
            </BarChart>
          </ResponsiveContainer>
        </ChartCard>
      </div>
    </div>
  );
}
