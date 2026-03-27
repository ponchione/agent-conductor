import { Check, X, Circle, Loader2 } from "lucide-react";
import type { PipelineRunDetail } from "@/types/api";
import { cn } from "@/lib/utils";

interface PhaseProgressStripProps {
  pipelineRun: PipelineRunDetail | null;
  workflowState: string;
  errorMessage?: string;
}

type PhaseStatus = "completed" | "active" | "failed" | "pending";

interface PhaseInfo {
  label: string;
  status: PhaseStatus;
  duration?: string;
}

function formatDuration(ms: number): string {
  const totalSeconds = Math.floor(ms / 1000);
  if (totalSeconds < 60) {
    return `${totalSeconds}s`;
  }
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes < 60) {
    return seconds > 0 ? `${minutes}m ${seconds}s` : `${minutes}m`;
  }
  const hours = Math.floor(minutes / 60);
  const remainingMinutes = minutes % 60;
  return remainingMinutes > 0 ? `${hours}h ${remainingMinutes}m` : `${hours}h`;
}

function computeElapsed(startedAt: string): string {
  const start = new Date(startedAt).getTime();
  const now = Date.now();
  return formatDuration(now - start);
}

function computeDuration(startedAt: string, completedAt: string): string {
  const start = new Date(startedAt).getTime();
  const end = new Date(completedAt).getTime();
  return formatDuration(end - start);
}

function derivePhaseStatus(
  startedAt: string | undefined,
  completedAt: string | undefined,
  errorMessage: string | undefined,
): { status: PhaseStatus; duration?: string } {
  if (completedAt && startedAt) {
    return { status: "completed", duration: computeDuration(startedAt, completedAt) };
  }
  if (startedAt) {
    if (errorMessage) {
      return { status: "failed", duration: computeElapsed(startedAt) };
    }
    return { status: "active", duration: computeElapsed(startedAt) };
  }
  return { status: "pending" };
}

function derivePhases(
  pipelineRun: PipelineRunDetail | null,
  workflowState: string,
  errorMessage?: string,
): PhaseInfo[] {
  if (!pipelineRun) {
    return [
      { label: "Scope", status: "pending" },
      { label: "Build", status: "pending" },
      { label: "Verify", status: "pending" },
      { label: "Review", status: "pending" },
    ];
  }

  // Determine which phase is the last active one (the one that would show the error)
  const phases: { key: string; label: string; startedAt?: string; completedAt?: string }[] = [
    { key: "scope", label: "Scope", startedAt: pipelineRun.scope_started_at, completedAt: pipelineRun.scope_completed_at },
    { key: "build", label: "Build", startedAt: pipelineRun.build_started_at, completedAt: pipelineRun.build_completed_at },
    { key: "verify", label: "Verify", startedAt: pipelineRun.verify_started_at, completedAt: pipelineRun.verify_completed_at },
  ];

  // Find the last phase that has started but not completed (candidate for error)
  let activePhaseKey: string | null = null;
  for (const p of phases) {
    if (p.startedAt && !p.completedAt) {
      activePhaseKey = p.key;
    }
  }

  const result: PhaseInfo[] = phases.map((p) => {
    const isActivePhaseWithError = errorMessage && p.key === activePhaseKey;
    const { status, duration } = derivePhaseStatus(
      p.startedAt,
      p.completedAt,
      isActivePhaseWithError ? errorMessage : undefined,
    );
    return { label: p.label, status, duration };
  });

  // Review phase: special logic
  let reviewStatus: PhaseStatus = "pending";
  let reviewDuration: string | undefined;
  if (pipelineRun.human_result) {
    reviewStatus = "completed";
  } else if (workflowState === "awaiting_review" || workflowState === "human_review") {
    reviewStatus = "active";
  }
  result.push({ label: "Review", status: reviewStatus, duration: reviewDuration });

  return result;
}

function PhaseIcon({ status }: { status: PhaseStatus }) {
  switch (status) {
    case "completed":
      return <Check className="size-4 text-green-400" />;
    case "active":
      return <Loader2 className="size-4 animate-spin text-blue-400" />;
    case "failed":
      return <X className="size-4 text-red-400" />;
    case "pending":
      return <Circle className="size-4 text-zinc-500" />;
  }
}

const statusRing: Record<PhaseStatus, string> = {
  completed: "border-green-500/50 bg-green-500/10",
  active: "border-blue-500/50 bg-blue-500/10",
  failed: "border-red-500/50 bg-red-500/10",
  pending: "border-zinc-600 bg-zinc-800/50",
};

const statusLabel: Record<PhaseStatus, string> = {
  completed: "text-green-400",
  active: "text-blue-400",
  failed: "text-red-400",
  pending: "text-zinc-500",
};

const connectorColor: Record<PhaseStatus, string> = {
  completed: "bg-green-500/40",
  active: "bg-blue-500/40",
  failed: "bg-red-500/40",
  pending: "bg-zinc-700",
};

export function PhaseProgressStrip({ pipelineRun, workflowState, errorMessage }: PhaseProgressStripProps) {
  const phases = derivePhases(pipelineRun, workflowState, errorMessage);

  return (
    <div className="flex flex-col gap-2">
      <span className="text-xs text-muted-foreground">Phases</span>
      <div className="flex items-center gap-0">
      {phases.map((phase, i) => (
        <div key={phase.label} className="flex items-center">
          {/* Phase node */}
          <div className="flex flex-col items-center gap-1">
            <div
              className={cn(
                "flex size-8 items-center justify-center rounded-full border-2",
                statusRing[phase.status],
              )}
            >
              <PhaseIcon status={phase.status} />
            </div>
            <span className={cn("text-xs font-medium", statusLabel[phase.status])}>
              {phase.label}
            </span>
            {phase.duration && (
              <span className="text-[10px] text-muted-foreground">{phase.duration}</span>
            )}
          </div>
          {/* Connector line */}
          {i < phases.length - 1 && (
            <div
              className={cn(
                "mx-2 h-0.5 w-8",
                // Connector takes the color of the completed side
                phase.status === "completed" ? connectorColor.completed : connectorColor.pending,
              )}
            />
          )}
        </div>
      ))}
    </div>
    </div>
  );
}
