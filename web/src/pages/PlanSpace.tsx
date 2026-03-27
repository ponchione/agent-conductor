import { useEffect, useState, useCallback, useRef } from "react";
import { Outlet, useParams, useNavigate, useLocation } from "react-router-dom";
import { getPlanAuditStats } from "@/api/client";
import type { PlanAuditRun } from "@/types/api";
import { STATE_DISPLAY, isTerminalState } from "@/lib/planRunState";
import { TimeAgo } from "@/components/TimeAgo";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

function DeltaBadge({ delta }: { delta: number | undefined }) {
  if (delta == null) {
    return null;
  }
  if (delta > 0) {
    return (
      <Badge variant="outline" className="bg-green-500/20 text-green-400 border-green-500/30 text-xs">
        +{delta}
      </Badge>
    );
  }
  if (delta < 0) {
    return (
      <Badge variant="outline" className="bg-red-500/20 text-red-400 border-red-500/30 text-xs">
        {delta}
      </Badge>
    );
  }
  return (
    <Badge variant="outline" className="bg-zinc-500/20 text-zinc-400 border-zinc-500/30 text-xs">
      0
    </Badge>
  );
}

function PlanRunStateBadge({ state }: { state: PlanAuditRun["state"] }) {
  // No badge for complete or undefined (legacy data)
  if (!state || state === "complete") return null;

  const display = STATE_DISPLAY[state];
  if (!display) return null;

  if (display.variant === "error") {
    return (
      <Badge variant="outline" className="bg-red-500/20 text-red-400 border-red-500/30 text-xs">
        {display.label}
      </Badge>
    );
  }

  // Progress states: blue badge with spinner
  return (
    <Badge variant="outline" className="bg-blue-500/20 text-blue-400 border-blue-500/30 text-xs">
      <span className="mr-1 inline-block h-2.5 w-2.5 animate-spin rounded-full border border-blue-400 border-t-transparent" />
      {display.label}
    </Badge>
  );
}

const SIDEBAR_POLL_INTERVAL_MS = 10000;

export default function PlanSpace() {
  const { planRunId } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const hasChildRoute = planRunId != null || location.pathname.endsWith("/new");
  const [runs, setRuns] = useState<PlanAuditRun[]>([]);
  const [loading, setLoading] = useState(true);
  const pollIntervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const mountedRef = useRef(true);

  const fetchRuns = useCallback(async (showLoading = false) => {
    if (showLoading) setLoading(true);
    try {
      const response = await getPlanAuditStats(50);
      if (mountedRef.current) {
        setRuns(response.recent_runs ?? []);
      }
    } catch {
      if (mountedRef.current) {
        setRuns([]);
      }
    } finally {
      if (mountedRef.current && showLoading) {
        setLoading(false);
      }
    }
  }, []);

  // Initial fetch
  useEffect(() => {
    mountedRef.current = true;
    fetchRuns(true).finally(() => {
      if (mountedRef.current) setLoading(false);
    });
    return () => {
      mountedRef.current = false;
    };
  }, [fetchRuns]);

  // Slow polling for sidebar: poll every 10s if any run is in-progress
  useEffect(() => {
    const hasInProgress = runs.some(
      (r) => r.state != null && !isTerminalState(r.state)
    );

    // Clear any existing interval
    if (pollIntervalRef.current !== null) {
      clearInterval(pollIntervalRef.current);
      pollIntervalRef.current = null;
    }

    if (hasInProgress) {
      pollIntervalRef.current = setInterval(() => {
        fetchRuns(false);
      }, SIDEBAR_POLL_INTERVAL_MS);
    }

    return () => {
      if (pollIntervalRef.current !== null) {
        clearInterval(pollIntervalRef.current);
        pollIntervalRef.current = null;
      }
    };
  }, [runs, fetchRuns]);

  const handleRunClick = useCallback(
    (id: string) => {
      navigate(`/plan/${encodeURIComponent(id)}`);
    },
    [navigate]
  );

  const handleNewPlan = useCallback(() => {
    navigate("/plan/new");
  }, [navigate]);

  return (
    <div className="grid h-full grid-cols-[320px_1fr]">
      {/* Left panel */}
      <div className="flex flex-col border-r border-border">
        <div className="border-b border-border p-4">
          <h2 className="mb-3 text-sm font-medium uppercase tracking-wider text-muted-foreground">
            Plan Runs
          </h2>
          <Button variant="default" size="sm" className="w-full" onClick={handleNewPlan}>
            New Plan
          </Button>
        </div>

        <div className="flex-1 overflow-y-auto p-3">
          {loading ? (
            <p className="py-8 text-center text-sm text-muted-foreground">Loading...</p>
          ) : runs.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">
              No plan runs found
            </p>
          ) : (
            <div className="flex flex-col gap-2">
              {runs.map((run) => {
                const isSelected = run.id === planRunId;
                return (
                  <div
                    key={run.id}
                    role="button"
                    tabIndex={0}
                    className={cn(
                      "border rounded-lg p-3 cursor-pointer transition-colors",
                      isSelected && "bg-accent",
                      !isSelected && "hover:bg-accent/50"
                    )}
                    onClick={() => handleRunClick(run.id)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        handleRunClick(run.id);
                      }
                    }}
                  >
                    <div className="mb-1 flex items-start justify-between gap-2">
                      <span className="text-sm font-medium leading-tight truncate">
                        {run.spec_file}
                      </span>
                      <DeltaBadge delta={run.work_order_delta} />
                    </div>
                    <div className="flex items-center gap-3 text-xs text-muted-foreground">
                      <PlanRunStateBadge state={run.state} />
                      {run.work_orders_generated != null && (
                        <span>{run.work_orders_generated} WOs</span>
                      )}
                      <TimeAgo timestamp={run.created_at} />
                      {run.audit_changed && (
                        <Badge
                          variant="outline"
                          className="bg-amber-500/20 text-amber-400 border-amber-500/30 text-xs"
                        >
                          audited
                        </Badge>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </div>

      {/* Right panel */}
      <div className="overflow-y-auto">
        {hasChildRoute ? (
          <Outlet />
        ) : (
          <div className="flex h-full items-center justify-center p-6">
            <p className="text-sm text-muted-foreground">
              Select a plan run to view details
            </p>
          </div>
        )}
      </div>
    </div>
  );
}
