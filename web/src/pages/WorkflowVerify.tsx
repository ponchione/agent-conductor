import { useCallback, useEffect, useRef, useState } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";
import { openEventStream } from "@/api/client";
import type { EventStreamEnvelope, PipelineRunDetail } from "@/types/api";
import { StatusBadge } from "@/components/StatusBadge";

interface WorkflowVerifyProps {
  workflowId: string;
  workflowState: string;
  pipelineRun: PipelineRunDetail | null;
}

interface PrecheckItem {
  name: string;
  passed: boolean;
  output: string;
}

const terminalStates = new Set(["completed", "failed"]);

export default function WorkflowVerify({ workflowId, workflowState, pipelineRun }: WorkflowVerifyProps) {
  const [prechecks, setPrechecks] = useState<PrecheckItem[]>([]);
  const [analysis, setAnalysis] = useState<string | null>(null);
  const [expandedChecks, setExpandedChecks] = useState<Set<string>>(new Set());
  const sourceRef = useRef<EventSource | null>(null);
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const precheckMapRef = useRef<Map<string, PrecheckItem>>(new Map());

  const isTerminal = terminalStates.has(workflowState);

  const handleEvent = useCallback((event: EventStreamEnvelope) => {
    if (event.event_type === "verify_precheck") {
      const data = event.event_data as Record<string, unknown> | null;
      if (!data) return;

      const name = String(data.name ?? data.check ?? "unknown");
      const passed = Boolean(data.passed ?? data.pass ?? data.success);
      const output = String(data.output ?? data.message ?? data.text ?? "");

      // Deduplicate by name
      precheckMapRef.current.set(name, { name, passed, output });
      setPrechecks(Array.from(precheckMapRef.current.values()));
    }

    if (event.event_type === "verify_result") {
      const data = event.event_data as Record<string, unknown> | null;
      if (!data) return;

      const text = String(data.analysis ?? data.reasoning ?? data.result ?? "");
      if (text) setAnalysis(text);
    }
  }, []);

  useEffect(() => {
    setPrechecks([]);
    setAnalysis(null);
    precheckMapRef.current.clear();

    const source = openEventStream({
      workflowId,
      cursor: 0,
      onEvent: handleEvent,
    });

    sourceRef.current = source;

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

  function toggleCheck(name: string) {
    setExpandedChecks((prev) => {
      const next = new Set(prev);
      if (next.has(name)) {
        next.delete(name);
      } else {
        next.add(name);
      }
      return next;
    });
  }

  const verifyResult = pipelineRun?.verify_result;
  const verifyModel = pipelineRun?.verify_model;

  return (
    <div className="space-y-6 p-4">
      {/* Verdict section */}
      <section className="rounded-lg border border-border bg-card p-4">
        <div className="flex items-center gap-3">
          <span className="text-sm font-medium text-muted-foreground">Verdict:</span>
          {verifyResult ? (
            <StatusBadge
              status={verifyResult.toLowerCase() === "pass" ? "completed" : "failed"}
              size="md"
            />
          ) : (
            <span className="text-sm text-muted-foreground">Pending</span>
          )}
          {verifyResult && (
            <span className="text-sm font-medium">
              {verifyResult.toUpperCase()}
            </span>
          )}
          {verifyModel && (
            <span className="ml-auto text-xs text-muted-foreground">
              Model: {verifyModel}
            </span>
          )}
        </div>
      </section>

      {/* Precheck cards */}
      {prechecks.length > 0 && (
        <section>
          <h3 className="mb-3 text-sm font-medium uppercase tracking-wider text-muted-foreground">
            Prechecks
          </h3>
          <div className="space-y-2">
            {prechecks.map((check) => {
              const isExpanded = expandedChecks.has(check.name);
              return (
                <div
                  key={check.name}
                  className="rounded-lg border border-border bg-card"
                >
                  <button
                    type="button"
                    onClick={() => toggleCheck(check.name)}
                    className="flex w-full items-center gap-3 px-4 py-3 text-left text-sm hover:bg-muted/50"
                  >
                    {isExpanded ? (
                      <ChevronDown className="size-4 shrink-0 text-muted-foreground" />
                    ) : (
                      <ChevronRight className="size-4 shrink-0 text-muted-foreground" />
                    )}
                    <span className="flex-1 font-medium">{check.name}</span>
                    <StatusBadge
                      status={check.passed ? "completed" : "failed"}
                      size="sm"
                    />
                  </button>
                  {isExpanded && check.output && (
                    <div className="border-t border-border px-4 py-3">
                      <pre className="whitespace-pre-wrap break-words text-xs text-muted-foreground font-mono">
                        {check.output}
                      </pre>
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        </section>
      )}

      {/* LLM Analysis */}
      {analysis && (
        <section>
          <h3 className="mb-3 text-sm font-medium uppercase tracking-wider text-muted-foreground">
            LLM Analysis
          </h3>
          <div className="rounded-lg border border-border bg-card p-4">
            <pre className="whitespace-pre-wrap break-words text-sm text-foreground font-mono leading-relaxed">
              {analysis}
            </pre>
          </div>
        </section>
      )}

      {/* Empty state */}
      {prechecks.length === 0 && !analysis && !verifyResult && (
        <div className="flex items-center justify-center p-6 text-sm text-muted-foreground">
          No verify data available yet
        </div>
      )}
    </div>
  );
}
