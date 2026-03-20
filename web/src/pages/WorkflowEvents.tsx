import { useEffect, useRef, useState, useCallback } from "react";
import { openEventStream } from "@/api/client";
import { StatusBadge } from "@/components/StatusBadge";
import { TimeAgo } from "@/components/TimeAgo";
import { cn } from "@/lib/utils";
import type { EventStreamEnvelope, PipelineEventType } from "@/types/api";

interface WorkflowEventsProps {
  workflowId: string;
  workflowState: string;
}

type ConnectionStatus = "connected" | "reconnecting" | "disconnected";

const terminalStates = new Set(["completed", "failed"]);

function eventTypeToStatus(eventType: PipelineEventType): string {
  switch (eventType) {
    case "phase_start":
    case "build_stdout":
      return "running";
    case "phase_complete":
    case "run_complete":
      return "completed";
    case "phase_error":
      return "failed";
    case "run_awaiting_review":
      return "awaiting_review";
    default:
      return "pending";
  }
}

function formatPayloadValue(value: unknown): string {
  if (value === null || value === undefined) {
    return "null";
  }
  if (typeof value === "string") {
    return value;
  }
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  return JSON.stringify(value, null, 2);
}

export default function WorkflowEvents({ workflowId, workflowState }: WorkflowEventsProps) {
  const [events, setEvents] = useState<EventStreamEnvelope[]>([]);
  const [status, setStatus] = useState<ConnectionStatus>("disconnected");
  const eventMapRef = useRef<Map<number, EventStreamEnvelope>>(new Map());
  const sourceRef = useRef<EventSource | null>(null);

  const handleEvent = useCallback((event: EventStreamEnvelope) => {
    if (eventMapRef.current.has(event.id)) {
      return;
    }
    eventMapRef.current.set(event.id, event);
    setEvents((prev) => [event, ...prev]);
  }, []);

  useEffect(() => {
    // Reset state when workflowId changes.
    eventMapRef.current.clear();
    setEvents([]);
    setStatus("disconnected");

    const isTerminal = terminalStates.has(workflowState);

    const source = openEventStream({
      workflowId,
      cursor: isTerminal ? 0 : undefined,
      onEvent: handleEvent,
      onOpen: () => {
        setStatus("connected");
      },
      onError: () => {
        // EventSource auto-reconnects on error, so mark as reconnecting.
        // If the workflow is terminal, we just disconnect since replay is done.
        if (isTerminal) {
          setStatus("disconnected");
        } else {
          setStatus("reconnecting");
        }
      },
    });

    sourceRef.current = source;

    return () => {
      source.close();
      sourceRef.current = null;
    };
  }, [workflowId, workflowState, handleEvent]);

  return (
    <div className="space-y-4 p-4">
      {/* Connection status indicator */}
      <div className="flex items-center gap-2 mb-4 text-sm">
        <span
          className={cn(
            "h-2 w-2 rounded-full",
            status === "connected" && "bg-green-500",
            status === "reconnecting" && "bg-amber-500 animate-pulse",
            status === "disconnected" && "bg-zinc-500"
          )}
        />
        <span className="text-muted-foreground">
          {status === "connected"
            ? "Connected"
            : status === "reconnecting"
              ? "Reconnecting..."
              : "Disconnected"}
        </span>
      </div>

      {/* Event list or empty state */}
      {events.length === 0 ? (
        <div className="flex items-center justify-center p-6 text-sm text-muted-foreground">
          {status === "disconnected" ? "No events recorded" : "Waiting for events..."}
        </div>
      ) : (
        <div className="space-y-3">
          {events.map((event) => (
            <div
              key={event.id}
              className="rounded-lg border border-border bg-card p-4 space-y-2"
            >
              {/* Header row: ID, badge, timestamp */}
              <div className="flex items-center gap-3 flex-wrap">
                <span className="text-xs text-muted-foreground font-mono">
                  #{event.id}
                </span>
                <StatusBadge status={eventTypeToStatus(event.event_type)} size="sm" />
                <span className="text-xs font-medium text-muted-foreground">
                  {event.event_type}
                </span>
                {event.task_id && (
                  <span className="text-xs text-muted-foreground font-mono">
                    task: {event.task_id}
                  </span>
                )}
                <span className="ml-auto text-xs text-muted-foreground">
                  <TimeAgo timestamp={event.created_at} />
                </span>
              </div>

              {/* Payload */}
              {event.event_data != null && (
                <div className="text-sm font-mono bg-muted/50 rounded-md p-3 space-y-1 overflow-x-auto">
                  {typeof event.event_data === "string" ? (
                    <p className="text-muted-foreground whitespace-pre-wrap break-all">
                      {event.event_data}
                    </p>
                  ) : (
                    Object.entries(event.event_data).map(([key, value]) => (
                      <div key={key} className="flex gap-2">
                        <span className="text-muted-foreground shrink-0">{key}:</span>
                        <span className="text-foreground whitespace-pre-wrap break-all">
                          {formatPayloadValue(value)}
                        </span>
                      </div>
                    ))
                  )}
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
