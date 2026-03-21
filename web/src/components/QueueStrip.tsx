import type { QueueState } from "@/types/api";
import { Button } from "@/components/ui/button";

interface QueueStripProps {
  state: QueueState;
  onManageClick: () => void;
}

function getStateLabel(state: QueueState): string {
  switch (state.state) {
    case "idle":
      return "Idle";
    case "ready": {
      const pending = state.items.filter((i) => i.status === "pending").length;
      return `${pending} Queued`;
    }
    case "running": {
      const completed = state.items.filter((i) => i.status === "completed").length;
      const total = state.items.length;
      return `Running (${completed + 1}/${total})`;
    }
    case "paused":
      return "Paused";
    case "completed":
      return "Complete";
    default:
      return "Unknown";
  }
}

function getPauseLabel(reason?: string): string {
  switch (reason) {
    case "awaiting_review":
      return "Paused \u2014 awaiting review";
    case "error":
      return "Paused \u2014 error";
    default:
      return "Paused \u2014 manual";
  }
}

export function QueueStrip({ state, onManageClick }: QueueStripProps) {
  return (
    <div>
      <p className="mb-2 text-xs font-medium uppercase text-muted-foreground">
        Queue
      </p>

      <p className="mb-1 text-sm font-medium">{getStateLabel(state)}</p>

      {state.state === "running" && state.current && (
        <div className="mb-2 flex items-center gap-2 text-sm text-muted-foreground">
          <span className="inline-block h-2 w-2 animate-pulse rounded-full bg-blue-500" />
          <span className="truncate">{state.current.title}</span>
        </div>
      )}

      {state.state === "paused" && (
        <p className="mb-2 text-sm text-muted-foreground">
          {getPauseLabel(state.pause_reason)}
        </p>
      )}

      <Button variant="outline" size="sm" onClick={onManageClick}>
        Manage Queue
      </Button>
    </div>
  );
}
