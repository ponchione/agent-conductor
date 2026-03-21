import { useCallback, useEffect, useRef, useState } from "react";
import { FileCode2 } from "lucide-react";
import { openEventStream } from "@/api/client";
import type { EventStreamEnvelope, PipelineRunDetail } from "@/types/api";
import { TerminalViewer } from "@/components/TerminalViewer";
import { DiffModal } from "@/components/DiffModal";
import { Button } from "@/components/ui/button";

interface WorkflowBuildProps {
  workflowId: string;
  workflowState: string;
  pipelineRun: PipelineRunDetail | null;
}

const terminalStates = new Set(["completed", "failed"]);

export default function WorkflowBuild({ workflowId, workflowState, pipelineRun }: WorkflowBuildProps) {
  const [lines, setLines] = useState<string[]>([]);
  const [diffOpen, setDiffOpen] = useState(false);
  const sourceRef = useRef<EventSource | null>(null);
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const isTerminal = terminalStates.has(workflowState);

  const handleEvent = useCallback((event: EventStreamEnvelope) => {
    if (event.event_type !== "build_stdout") return;

    const text =
      typeof event.event_data === "string"
        ? event.event_data
        : event.event_data && typeof event.event_data === "object" && "line" in event.event_data
          ? String(event.event_data.line)
          : event.event_data && typeof event.event_data === "object" && "text" in event.event_data
            ? String(event.event_data.text)
            : JSON.stringify(event.event_data);

    // Split on newlines so each line is separate
    const newLines = text.split("\n");
    setLines((prev) => [...prev, ...newLines]);
  }, []);

  useEffect(() => {
    setLines([]);

    const source = openEventStream({
      workflowId,
      cursor: isTerminal ? 0 : undefined,
      onEvent: handleEvent,
    });

    sourceRef.current = source;

    // For completed workflows, close SSE after 5s timeout
    if (isTerminal) {
      timeoutRef.current = setTimeout(() => {
        source.close();
        sourceRef.current = null;
      }, 5000);
    }

    return () => {
      source.close();
      sourceRef.current = null;
      if (timeoutRef.current) {
        clearTimeout(timeoutRef.current);
        timeoutRef.current = null;
      }
    };
  }, [workflowId, isTerminal, handleEvent]);

  const filesChanged = pipelineRun?.build_files_changed ?? 0;

  return (
    <div className="flex flex-col gap-4 p-4">
      {/* Terminal output */}
      <div style={{ height: "400px" }}>
        <TerminalViewer lines={lines} streaming={!isTerminal} />
      </div>

      {/* Build stats bar */}
      <div className="flex items-center justify-between rounded-lg border border-border bg-card p-3">
        <span className="text-sm text-muted-foreground">
          {filesChanged} file{filesChanged !== 1 ? "s" : ""} changed
        </span>
        <Button
          variant="outline"
          size="sm"
          onClick={() => setDiffOpen(true)}
          className="gap-1.5"
        >
          <FileCode2 className="size-3.5" />
          View Diff
        </Button>
      </div>

      {/* Diff modal */}
      <DiffModal
        open={diffOpen}
        onClose={() => setDiffOpen(false)}
        workflowId={workflowId}
      />
    </div>
  );
}
