import { useEffect, useState } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { Loader2, GitBranch } from "lucide-react";
import { getWorkflow, approveWorkflow, rejectWorkflow } from "@/api/client";
import type { WorkflowDetailResponse } from "@/types/api";
import { StatusBadge } from "@/components/StatusBadge";
import { CopyableID } from "@/components/CopyableID";
import { PhaseProgressStrip } from "@/components/PhaseProgressStrip";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import WorkflowOverview from "@/pages/WorkflowOverview";
import WorkflowEvents from "@/pages/WorkflowEvents";
import WorkflowBuild from "@/pages/WorkflowBuild";
import WorkflowVerify from "@/pages/WorkflowVerify";
import WorkflowScope from "@/pages/WorkflowScope";

const TAB_VALUES = ["overview", "scope", "build", "verify", "events"] as const;
type TabValue = (typeof TAB_VALUES)[number];

function isValidTab(value: string | undefined): value is TabValue {
  return TAB_VALUES.includes(value as TabValue);
}

function parseWorkOrderTitle(pipelineRun: WorkflowDetailResponse["pipeline_run"]): string {
  if (!pipelineRun) return "Untitled";

  if (pipelineRun.work_order_content) {
    const match = pipelineRun.work_order_content.match(/^title:\s*(.+)$/m);
    if (match) {
      // Strip surrounding quotes if present
      return match[1].replace(/^["']|["']$/g, "").trim();
    }
  }

  if (pipelineRun.work_order_type) {
    return pipelineRun.work_order_type;
  }

  return "Untitled";
}

export default function WorkflowDetail() {
  const { workflowId, tab } = useParams();
  const navigate = useNavigate();

  const [data, setData] = useState<WorkflowDetailResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [approveOpen, setApproveOpen] = useState(false);
  const [rejectOpen, setRejectOpen] = useState(false);
  const [rejectReason, setRejectReason] = useState("");
  const [actionLoading, setActionLoading] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const [reindexAfterMerge, setReindexAfterMerge] = useState(true);

  const activeTab: TabValue = isValidTab(tab) ? tab : "overview";

  useEffect(() => {
    if (!workflowId) return;

    let cancelled = false;
    setLoading(true);
    setError(null);

    getWorkflow(workflowId)
      .then((response) => {
        if (!cancelled) {
          setData(response);
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : "Failed to load workflow");
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [workflowId]);

  function refetchWorkflow() {
    if (!workflowId) return;
    getWorkflow(workflowId)
      .then((response) => setData(response))
      .catch(() => {});
  }

  async function handleApprove() {
    if (!workflowId) return;
    setActionLoading(true);
    setActionError(null);
    try {
      await approveWorkflow(workflowId, { reindex: reindexAfterMerge });
      setApproveOpen(false);
      refetchWorkflow();
    } catch (err: unknown) {
      setActionError(err instanceof Error ? err.message : "Approve failed");
    } finally {
      setActionLoading(false);
    }
  }

  async function handleReject() {
    if (!workflowId) return;
    setActionLoading(true);
    setActionError(null);
    try {
      await rejectWorkflow(workflowId, { reason: rejectReason || undefined });
      setRejectOpen(false);
      setRejectReason("");
      refetchWorkflow();
    } catch (err: unknown) {
      setActionError(err instanceof Error ? err.message : "Reject failed");
    } finally {
      setActionLoading(false);
    }
  }

  function handleTabChange(value: string) {
    if (value === "overview") {
      navigate(`/pipeline/${workflowId}`);
    } else {
      navigate(`/pipeline/${workflowId}/${value}`);
    }
  }

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Loader2 className="size-6 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (error || !data) {
    return (
      <div className="flex h-full items-center justify-center">
        <p className="text-sm text-destructive">{error ?? "Workflow not found"}</p>
      </div>
    );
  }

  const { workflow, pipeline_run, sub_calls } = data;
  const title = parseWorkOrderTitle(pipeline_run);

  return (
    <div className="flex h-full flex-col overflow-hidden">
      {/* Header bar */}
      <div className="flex flex-col gap-3 border-b border-border px-6 py-4">
        <div className="flex items-center justify-between gap-4">
          <div className="flex items-center gap-3 min-w-0">
            <h1 className="truncate text-lg font-semibold">{title}</h1>
            <StatusBadge status={workflow.current_state} size="md" />
          </div>
          {workflow.current_state === "human_review" && (
            <div className="flex items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                className="border-green-500/50 text-green-400 hover:bg-green-500/10"
                onClick={() => setApproveOpen(true)}
              >
                Approve
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="border-red-500/50 text-red-400 hover:bg-red-500/10"
                onClick={() => setRejectOpen(true)}
              >
                Reject
              </Button>
            </div>
          )}
        </div>

        <div className="flex items-center gap-4 text-xs text-muted-foreground">
          <span className="font-mono">
            <CopyableID id={workflow.id} truncate={workflow.id.length} />
          </span>
          {workflow.git_branch && (
            <span className="flex items-center gap-1">
              <GitBranch className="size-3" />
              {workflow.git_branch}
            </span>
          )}
        </div>
      </div>

      {/* Phase progress strip */}
      <div className="border-b border-border px-6 py-3">
        <PhaseProgressStrip
          pipelineRun={pipeline_run}
          workflowState={workflow.current_state}
          errorMessage={workflow.error_message}
        />
      </div>

      {/* Tabbed content */}
      <Tabs
        value={activeTab}
        onValueChange={handleTabChange}
        className="flex min-h-0 flex-1 flex-col"
      >
        <div className="border-b border-border px-6">
          <TabsList variant="line">
            <TabsTrigger value="overview">Overview</TabsTrigger>
            <TabsTrigger value="scope">Scope</TabsTrigger>
            <TabsTrigger value="build">Build</TabsTrigger>
            <TabsTrigger value="verify">Verify</TabsTrigger>
            <TabsTrigger value="events">Events</TabsTrigger>
          </TabsList>
        </div>

        <div className="min-h-0 flex-1 overflow-auto">
          <TabsContent value="overview">
            <WorkflowOverview
              pipelineRun={pipeline_run}
              subCalls={sub_calls}
              workflowState={workflow.current_state}
              workflowId={workflow.id}
              onApprove={() => setApproveOpen(true)}
              onReject={() => setRejectOpen(true)}
            />
          </TabsContent>
          <TabsContent value="scope">
            <WorkflowScope workflowId={workflow.id} />
          </TabsContent>
          <TabsContent value="build">
            <WorkflowBuild
              workflowId={workflow.id}
              workflowState={workflow.current_state}
              pipelineRun={pipeline_run}
            />
          </TabsContent>
          <TabsContent value="verify">
            <WorkflowVerify
              workflowId={workflow.id}
              workflowState={workflow.current_state}
              pipelineRun={pipeline_run}
            />
          </TabsContent>
          <TabsContent value="events">
            <WorkflowEvents
              workflowId={workflow.id}
              workflowState={workflow.current_state}
            />
          </TabsContent>
        </div>
      </Tabs>

      {/* Approve confirmation dialog */}
      <ConfirmDialog
        open={approveOpen}
        title="Approve Workflow"
        description={`This will merge the branch "${workflow.git_branch}" into the base branch.`}
        confirmLabel="Approve & Merge"
        onConfirm={handleApprove}
        onCancel={() => {
          setApproveOpen(false);
          setActionError(null);
        }}
      >
        <div className="space-y-3 py-2">
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={reindexAfterMerge}
              onChange={(e) => setReindexAfterMerge(e.target.checked)}
              className="rounded border-border"
            />
            Re-index codebase after merge
          </label>
          {actionError && (
            <p className="text-sm text-destructive">{actionError}</p>
          )}
          {actionLoading && (
            <p className="text-sm text-muted-foreground">Processing...</p>
          )}
        </div>
      </ConfirmDialog>

      {/* Reject confirmation dialog */}
      <ConfirmDialog
        open={rejectOpen}
        title="Reject Workflow"
        description="This workflow will be marked as rejected. The branch will not be merged."
        confirmLabel="Reject"
        onConfirm={handleReject}
        onCancel={() => {
          setRejectOpen(false);
          setRejectReason("");
          setActionError(null);
        }}
      >
        <div className="space-y-3 py-2">
          <textarea
            placeholder="Reason for rejection (optional)"
            value={rejectReason}
            onChange={(e) => setRejectReason(e.target.value)}
            className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
            rows={3}
          />
          {actionError && (
            <p className="text-sm text-destructive">{actionError}</p>
          )}
          {actionLoading && (
            <p className="text-sm text-muted-foreground">Processing...</p>
          )}
        </div>
      </ConfirmDialog>
    </div>
  );
}
