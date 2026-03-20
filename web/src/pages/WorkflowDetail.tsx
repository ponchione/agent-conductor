import { useEffect, useState } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { Loader2, GitBranch } from "lucide-react";
import { getWorkflow } from "@/api/client";
import type { WorkflowDetailResponse } from "@/types/api";
import { StatusBadge } from "@/components/StatusBadge";
import { CopyableID } from "@/components/CopyableID";
import { PhaseProgressStrip } from "@/components/PhaseProgressStrip";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import WorkflowOverview from "@/pages/WorkflowOverview";
import WorkflowEvents from "@/pages/WorkflowEvents";

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
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" disabled>
              Approve
            </Button>
            <Button variant="outline" size="sm" disabled>
              Reject
            </Button>
          </div>
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
            <WorkflowOverview pipelineRun={pipeline_run} subCalls={sub_calls} />
          </TabsContent>
          <TabsContent value="scope">
            <div className="flex items-center justify-center p-6 text-sm text-muted-foreground">
              Scope details coming soon
            </div>
          </TabsContent>
          <TabsContent value="build">
            <div className="flex items-center justify-center p-6 text-sm text-muted-foreground">
              Build output coming soon
            </div>
          </TabsContent>
          <TabsContent value="verify">
            <div className="flex items-center justify-center p-6 text-sm text-muted-foreground">
              Verify results coming soon
            </div>
          </TabsContent>
          <TabsContent value="events">
            <WorkflowEvents
              workflowId={workflow.id}
              workflowState={workflow.current_state}
            />
          </TabsContent>
        </div>
      </Tabs>
    </div>
  );
}
