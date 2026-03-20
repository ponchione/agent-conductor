import { useEffect, useState, useCallback, useMemo } from "react";
import {
  Outlet,
  useParams,
  useNavigate,
  useSearchParams,
} from "react-router-dom";
import { listWorkflows } from "@/api/client";
import type { ListWorkflowsOptions } from "@/api/client";
import type { WorkflowSummary } from "@/types/api";
import { StatusBadge } from "@/components/StatusBadge";
import { CopyableID } from "@/components/CopyableID";
import { TimeAgo } from "@/components/TimeAgo";
import { FilterBar, type FilterConfig } from "@/components/FilterBar";
import { cn } from "@/lib/utils";

const PAGE_SIZE = 20;
const ALL_SENTINEL = "__all__";

const statusFilterOptions = [
  { value: ALL_SENTINEL, label: "All" },
  { value: "running", label: "Running" },
  { value: "awaiting_review", label: "Awaiting Review" },
  { value: "completed", label: "Completed" },
  { value: "failed", label: "Failed" },
];

function buildProjectFilterOptions(
  workflows: WorkflowSummary[]
): { value: string; label: string }[] {
  const projects = new Set<string>();
  for (const w of workflows) {
    if (w.project) {
      projects.add(w.project);
    }
  }
  const sorted = Array.from(projects).sort();
  return [
    { value: ALL_SENTINEL, label: "All" },
    ...sorted.map((p) => ({ value: p, label: p })),
  ];
}

function formatCost(cost: number): string {
  return `$${cost.toFixed(2)}`;
}

export default function PipelineSpace() {
  const { workflowId } = useParams();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();

  // Filter state initialized from URL search params
  const [statusFilter, setStatusFilter] = useState<string>(
    () => searchParams.get("status") || ALL_SENTINEL
  );
  const [projectFilter, setProjectFilter] = useState<string>(
    () => searchParams.get("project") || ALL_SENTINEL
  );

  const [workflows, setWorkflows] = useState<WorkflowSummary[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [offset, setOffset] = useState(0);
  const [loadingMore, setLoadingMore] = useState(false);

  // Track all projects seen (across all fetches) for the project filter dropdown
  const [allProjects, setAllProjects] = useState<WorkflowSummary[]>([]);

  const projectFilterOptions = useMemo(
    () => buildProjectFilterOptions(allProjects),
    [allProjects]
  );

  const filterConfigs: FilterConfig[] = useMemo(
    () => [
      {
        key: "status",
        label: "Status",
        options: statusFilterOptions,
      },
      {
        key: "project",
        label: "Project",
        options: projectFilterOptions,
      },
    ],
    [projectFilterOptions]
  );

  const filterValues = useMemo(
    () => ({
      status: statusFilter,
      project: projectFilter,
    }),
    [statusFilter, projectFilter]
  );

  // Fetch workflows
  const fetchWorkflows = useCallback(
    async (currentOffset: number, append: boolean) => {
      if (!append) {
        setLoading(true);
      } else {
        setLoadingMore(true);
      }

      try {
        const options: ListWorkflowsOptions = {
          limit: PAGE_SIZE,
          offset: currentOffset,
        };
        if (statusFilter !== ALL_SENTINEL) {
          options.status = statusFilter;
        }
        if (projectFilter !== ALL_SENTINEL) {
          options.project = projectFilter;
        }

        const response = await listWorkflows(options);
        const fetched = response.workflows ?? [];

        if (append) {
          setWorkflows((prev) => [...prev, ...fetched]);
        } else {
          setWorkflows(fetched);
        }
        setTotal(response.total);

        // Always accumulate projects for filter dropdown (deduped in buildProjectFilterOptions)
        setAllProjects((prev) => [...prev, ...fetched]);
      } catch {
        // On error, show empty state rather than crashing
        if (!append) {
          setWorkflows([]);
          setTotal(0);
        }
      } finally {
        setLoading(false);
        setLoadingMore(false);
      }
    },
    [statusFilter, projectFilter]
  );

  // Initial fetch and refetch when filters change
  useEffect(() => {
    setOffset(0);
    fetchWorkflows(0, false);
  }, [fetchWorkflows]);

  // Sync filters to URL search params
  useEffect(() => {
    const params = new URLSearchParams();
    if (statusFilter !== ALL_SENTINEL) {
      params.set("status", statusFilter);
    }
    if (projectFilter !== ALL_SENTINEL) {
      params.set("project", projectFilter);
    }
    setSearchParams(params, { replace: true });
  }, [statusFilter, projectFilter, setSearchParams]);

  const handleFilterChange = useCallback((key: string, value: string) => {
    if (key === "status") {
      setStatusFilter(value);
    } else if (key === "project") {
      setProjectFilter(value);
    }
  }, []);

  const handleLoadMore = useCallback(() => {
    const nextOffset = offset + PAGE_SIZE;
    setOffset(nextOffset);
    fetchWorkflows(nextOffset, true);
  }, [offset, fetchWorkflows]);

  const handleCardClick = useCallback(
    (id: string) => {
      const params = new URLSearchParams();
      if (statusFilter !== ALL_SENTINEL) {
        params.set("status", statusFilter);
      }
      if (projectFilter !== ALL_SENTINEL) {
        params.set("project", projectFilter);
      }
      const query = params.toString();
      navigate(`/pipeline/${encodeURIComponent(id)}${query ? `?${query}` : ""}`);
    },
    [navigate, statusFilter, projectFilter]
  );

  const hasMore = workflows.length < total;

  return (
    <div className="grid h-full grid-cols-[350px_1fr]">
      {/* List panel */}
      <div className="flex flex-col border-r border-border">
        <div className="border-b border-border p-4">
          <h2 className="mb-3 text-sm font-medium uppercase tracking-wider text-muted-foreground">
            Workflows
          </h2>
          <FilterBar
            filters={filterConfigs}
            values={filterValues}
            onChange={handleFilterChange}
          />
        </div>

        <div className="flex-1 overflow-y-auto p-3">
          {loading ? (
            <p className="py-8 text-center text-sm text-muted-foreground">
              Loading...
            </p>
          ) : workflows.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">
              No workflows found
            </p>
          ) : (
            <div className="flex flex-col gap-2">
              {workflows.map((workflow) => {
                const isSelected = workflow.id === workflowId;
                return (
                  <div
                    key={workflow.id}
                    role="button"
                    tabIndex={0}
                    className={cn(
                      "border rounded-lg p-3 cursor-pointer transition-colors",
                      workflow.current_state === "running" &&
                        "border-blue-500/50 animate-pulse",
                      workflow.current_state === "awaiting_review" &&
                        "border-l-4 border-l-amber-500",
                      isSelected && "bg-accent",
                      !isSelected && "hover:bg-accent/50"
                    )}
                    onClick={() => handleCardClick(workflow.id)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        handleCardClick(workflow.id);
                      }
                    }}
                  >
                    <div className="mb-1 flex items-start justify-between gap-2">
                      <span className="text-sm font-medium leading-tight">
                        {workflow.work_order_title ||
                          workflow.work_order_type ||
                          "Untitled"}
                      </span>
                      <StatusBadge
                        status={workflow.current_state}
                        size="sm"
                      />
                    </div>
                    <div className="flex items-center gap-3 text-xs text-muted-foreground">
                      <CopyableID id={workflow.id} truncate={8} />
                      <TimeAgo timestamp={workflow.updated_at} />
                      {workflow.total_cost_usd != null && (
                        <span>{formatCost(workflow.total_cost_usd)}</span>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>

        {/* Pagination footer */}
        {!loading && workflows.length > 0 && (
          <div className="border-t border-border px-4 py-3">
            <p className="mb-2 text-xs text-muted-foreground">
              Showing {workflows.length} of {total} workflows
            </p>
            {hasMore && (
              <button
                type="button"
                className="w-full rounded-md border border-border px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-accent disabled:opacity-50"
                onClick={handleLoadMore}
                disabled={loadingMore}
              >
                {loadingMore ? "Loading..." : "Load more"}
              </button>
            )}
          </div>
        )}
      </div>

      {/* Detail panel */}
      <div className="overflow-y-auto">
        {workflowId ? (
          <Outlet />
        ) : (
          <div className="flex h-full items-center justify-center p-6">
            <p className="text-sm text-muted-foreground">
              Select a workflow to view details
            </p>
          </div>
        )}
      </div>
    </div>
  );
}
