import { useEffect, useState, useCallback } from "react";
import { Outlet, useParams, useNavigate, useLocation } from "react-router-dom";
import { getPlanAuditStats } from "@/api/client";
import type { PlanAuditRun } from "@/types/api";
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

export default function PlanSpace() {
  const { planRunId } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const hasChildRoute = planRunId != null || location.pathname.endsWith("/new");
  const [runs, setRuns] = useState<PlanAuditRun[]>([]);
  const [loading, setLoading] = useState(true);

  const fetchRuns = useCallback(async () => {
    setLoading(true);
    try {
      const response = await getPlanAuditStats(50);
      setRuns(response.recent_runs ?? []);
    } catch {
      setRuns([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchRuns();
  }, [fetchRuns]);

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
