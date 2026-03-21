import { useEffect, useState, useCallback, useRef } from "react";
import { useParams, Link } from "react-router-dom";
import {
  getPlanAuditStats,
  listWorkOrders,
  getWorkOrder,
  updateWorkOrder,
  addQueueItems,
} from "@/api/client";
import type { PlanAuditRun, WorkOrderFile } from "@/types/api";
import { MetricCard } from "@/components/MetricCard";
import { CopyableID } from "@/components/CopyableID";
import { TimeAgo } from "@/components/TimeAgo";
import { CodeViewer } from "@/components/CodeViewer";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";

interface WorkOrderCardState {
  expanded: boolean;
  editing: boolean;
  content: string | null;
  loading: boolean;
  saving: boolean;
  saveError: string | null;
  saveSuccess: boolean;
  editContent: string;
}

export default function PlanRunDetail() {
  const { planRunId } = useParams<{ planRunId: string }>();
  const [run, setRun] = useState<PlanAuditRun | null>(null);
  const [workOrders, setWorkOrders] = useState<WorkOrderFile[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Per-card state keyed by filename
  const [cardStates, setCardStates] = useState<Record<string, WorkOrderCardState>>({});

  // Queue selection
  const [selectedFiles, setSelectedFiles] = useState<Set<string>>(new Set());
  const [queueing, setQueueing] = useState(false);
  const [queued, setQueued] = useState(false);

  // Cache fetched WO content so we don't refetch
  const contentCacheRef = useRef<Record<string, string>>({});

  useEffect(() => {
    if (!planRunId) return;

    setLoading(true);
    setError(null);

    Promise.all([getPlanAuditStats(100), listWorkOrders()])
      .then(([statsResponse, woResponse]) => {
        const found = statsResponse.recent_runs?.find((r) => r.id === planRunId);
        setRun(found ?? null);
        setWorkOrders(woResponse.work_orders ?? []);
        if (!found) {
          setError("Plan run not found");
        }
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : "Failed to load plan run");
      })
      .finally(() => {
        setLoading(false);
      });
  }, [planRunId]);

  const getCardState = useCallback(
    (filename: string): WorkOrderCardState => {
      return (
        cardStates[filename] ?? {
          expanded: false,
          editing: false,
          content: null,
          loading: false,
          saving: false,
          saveError: null,
          saveSuccess: false,
          editContent: "",
        }
      );
    },
    [cardStates]
  );

  const updateCardState = useCallback(
    (filename: string, patch: Partial<WorkOrderCardState>) => {
      setCardStates((prev) => ({
        ...prev,
        [filename]: { ...( prev[filename] ?? {
          expanded: false,
          editing: false,
          content: null,
          loading: false,
          saving: false,
          saveError: null,
          saveSuccess: false,
          editContent: "",
        }), ...patch },
      }));
    },
    []
  );

  const handleToggleExpand = useCallback(
    async (filename: string) => {
      const current = getCardState(filename);
      if (current.expanded) {
        updateCardState(filename, { expanded: false, editing: false });
        return;
      }

      // Expand - fetch content if not cached
      if (contentCacheRef.current[filename]) {
        const cached = contentCacheRef.current[filename];
        updateCardState(filename, {
          expanded: true,
          content: cached,
          editContent: cached,
        });
        return;
      }

      updateCardState(filename, { expanded: true, loading: true });
      try {
        const response = await getWorkOrder(filename);
        contentCacheRef.current[filename] = response.content;
        updateCardState(filename, {
          loading: false,
          content: response.content,
          editContent: response.content,
        });
      } catch {
        updateCardState(filename, {
          loading: false,
          content: "// Failed to load content",
          editContent: "",
        });
      }
    },
    [getCardState, updateCardState]
  );

  const handleToggleEdit = useCallback(
    (filename: string) => {
      const current = getCardState(filename);
      if (current.editing) {
        // Cancel edit - revert to cached content
        updateCardState(filename, {
          editing: false,
          editContent: current.content ?? "",
          saveError: null,
          saveSuccess: false,
        });
      } else {
        updateCardState(filename, {
          editing: true,
          editContent: current.content ?? "",
          saveError: null,
          saveSuccess: false,
        });
      }
    },
    [getCardState, updateCardState]
  );

  const handleEditChange = useCallback(
    (filename: string, newContent: string) => {
      updateCardState(filename, { editContent: newContent });
    },
    [updateCardState]
  );

  const handleSave = useCallback(
    async (filename: string) => {
      const current = getCardState(filename);
      updateCardState(filename, { saving: true, saveError: null, saveSuccess: false });
      try {
        const result = await updateWorkOrder(filename, current.editContent);
        contentCacheRef.current[filename] = result.content;
        updateCardState(filename, {
          saving: false,
          editing: false,
          content: result.content,
          editContent: result.content,
          saveSuccess: true,
          saveError: result.valid ? null : "Saved but YAML validation had warnings",
        });
      } catch (err) {
        updateCardState(filename, {
          saving: false,
          saveError: err instanceof Error ? err.message : "Save failed",
        });
      }
    },
    [getCardState, updateCardState]
  );

  const handleToggleSelect = useCallback((filename: string) => {
    setSelectedFiles((prev) => {
      const next = new Set(prev);
      if (next.has(filename)) {
        next.delete(filename);
      } else {
        next.add(filename);
      }
      return next;
    });
  }, []);

  const handleQueueSelected = useCallback(async () => {
    if (selectedFiles.size === 0) return;
    setQueueing(true);
    try {
      await addQueueItems(Array.from(selectedFiles).map((f) => ({ work_order_file: f })));
      setQueued(true);
      setSelectedFiles(new Set());
    } catch {
      // Silently fail for now
    } finally {
      setQueueing(false);
    }
  }, [selectedFiles]);

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center p-6">
        <p className="text-sm text-muted-foreground">Loading...</p>
      </div>
    );
  }

  if (error || !run) {
    return (
      <div className="flex h-full items-center justify-center p-6">
        <p className="text-sm text-red-400">{error ?? "Plan run not found"}</p>
      </div>
    );
  }

  const auditChanges = run.audit_changes ?? [];

  return (
    <div className="flex flex-col gap-6 p-6">
      {/* Header */}
      <div>
        <h2 className="text-lg font-semibold">{run.spec_file}</h2>
        <div className="mt-1 flex items-center gap-3 text-sm text-muted-foreground">
          <CopyableID id={run.id} />
          <TimeAgo timestamp={run.created_at} />
        </div>
      </div>

      {/* Audit summary metrics */}
      <div className="grid grid-cols-4 gap-4">
        <MetricCard
          label="Pre-Audit"
          value={run.pre_audit_work_order_count ?? "-"}
        />
        <MetricCard
          label="Post-Audit"
          value={run.post_audit_work_order_count ?? "-"}
        />
        <MetricCard
          label="Delta"
          value={
            run.work_order_delta != null
              ? run.work_order_delta > 0
                ? `+${run.work_order_delta}`
                : String(run.work_order_delta)
              : "-"
          }
        />
        <MetricCard
          label="WOs Generated"
          value={run.work_orders_generated ?? "-"}
        />
      </div>

      {/* Audit changes */}
      {auditChanges.length > 0 && (
        <div>
          <h3 className="mb-2 text-sm font-medium text-muted-foreground uppercase tracking-wider">
            Audit Changes
          </h3>
          <div className="flex flex-wrap gap-2">
            {auditChanges.map((change, i) => {
              const isAdded = change.toLowerCase().includes("added");
              const isRemoved = change.toLowerCase().includes("removed");
              return (
                <Badge
                  key={i}
                  variant="outline"
                  className={
                    isAdded
                      ? "bg-green-500/20 text-green-400 border-green-500/30"
                      : isRemoved
                        ? "bg-red-500/20 text-red-400 border-red-500/30"
                        : "bg-amber-500/20 text-amber-400 border-amber-500/30"
                  }
                >
                  {change}
                </Badge>
              );
            })}
          </div>
        </div>
      )}

      {/* Work order cards */}
      <div>
        <h3 className="mb-3 text-sm font-medium text-muted-foreground uppercase tracking-wider">
          Work Orders
        </h3>
        {workOrders.length === 0 ? (
          <p className="text-sm text-muted-foreground">No work orders found</p>
        ) : (
          <div className="flex flex-col gap-3">
            {workOrders.map((wo) => {
              const state = getCardState(wo.filename);
              const isSelected = selectedFiles.has(wo.filename);

              // Determine audit annotation
              const matchedChange = auditChanges.find((c) =>
                c.toLowerCase().includes(wo.filename.toLowerCase().replace(".yaml", "").replace(".yml", ""))
              );
              const isAddedByAudit = matchedChange?.toLowerCase().includes("added");
              const isModifiedByAudit = matchedChange && !isAddedByAudit;

              return (
                <div
                  key={wo.filename}
                  className="rounded-lg border border-border"
                >
                  {/* Card header */}
                  <div className="flex items-center gap-3 p-3">
                    <input
                      type="checkbox"
                      checked={isSelected}
                      onChange={() => handleToggleSelect(wo.filename)}
                      className="h-4 w-4 rounded border-border"
                    />
                    <div
                      role="button"
                      tabIndex={0}
                      className="flex flex-1 cursor-pointer items-center justify-between"
                      onClick={() => handleToggleExpand(wo.filename)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter" || e.key === " ") {
                          e.preventDefault();
                          handleToggleExpand(wo.filename);
                        }
                      }}
                    >
                      <div className="flex items-center gap-2">
                        <span className="text-sm font-medium">{wo.title || wo.filename}</span>
                        <Badge variant="outline" className="text-xs">
                          {wo.type}
                        </Badge>
                        {wo.target_module && (
                          <span className="text-xs text-muted-foreground">
                            {wo.target_module}
                          </span>
                        )}
                        {isAddedByAudit && (
                          <Badge
                            variant="outline"
                            className="bg-green-500/20 text-green-400 border-green-500/30 text-xs"
                          >
                            added by audit
                          </Badge>
                        )}
                        {isModifiedByAudit && (
                          <Badge
                            variant="outline"
                            className="bg-amber-500/20 text-amber-400 border-amber-500/30 text-xs"
                          >
                            modified by audit
                          </Badge>
                        )}
                      </div>
                      <span className="text-xs text-muted-foreground">
                        {state.expanded ? "Collapse" : "Expand"}
                      </span>
                    </div>
                  </div>

                  {/* Expanded content */}
                  {state.expanded && (
                    <div className="border-t border-border p-3">
                      {state.loading ? (
                        <p className="py-4 text-center text-sm text-muted-foreground">
                          Loading...
                        </p>
                      ) : (
                        <>
                          <div className="mb-2 flex items-center justify-end gap-2">
                            <Button
                              variant="outline"
                              size="xs"
                              onClick={() => handleToggleEdit(wo.filename)}
                            >
                              {state.editing ? "Cancel" : "Edit"}
                            </Button>
                            {state.editing && (
                              <Button
                                variant="default"
                                size="xs"
                                onClick={() => handleSave(wo.filename)}
                                disabled={state.saving}
                              >
                                {state.saving ? "Saving..." : "Save"}
                              </Button>
                            )}
                          </div>
                          <CodeViewer
                            value={state.editing ? state.editContent : (state.content ?? "")}
                            onChange={
                              state.editing
                                ? (val) => handleEditChange(wo.filename, val)
                                : undefined
                            }
                            language="yaml"
                            readOnly={!state.editing}
                            height="300px"
                          />
                          {state.saveError && (
                            <p className="mt-2 text-xs text-red-400">{state.saveError}</p>
                          )}
                          {state.saveSuccess && !state.saveError && (
                            <p className="mt-2 text-xs text-green-400">Saved successfully</p>
                          )}
                        </>
                      )}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* Queue actions */}
      <div className="flex items-center gap-3 border-t border-border pt-4">
        {queued ? (
          <div className="flex items-center gap-2">
            <span className="text-sm text-green-400">Queued!</span>
            <Link
              to="/pipeline"
              className="text-sm text-primary underline underline-offset-4 hover:text-primary/80"
            >
              Open Queue
            </Link>
          </div>
        ) : (
          <Button
            variant="default"
            size="sm"
            onClick={handleQueueSelected}
            disabled={selectedFiles.size === 0 || queueing}
          >
            {queueing
              ? "Queueing..."
              : `Queue Selected (${selectedFiles.size})`}
          </Button>
        )}
      </div>
    </div>
  );
}
