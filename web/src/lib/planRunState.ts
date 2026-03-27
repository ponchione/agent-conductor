import type { PlanRunState } from "@/types/api";
import { TERMINAL_STATES } from "@/types/api";

export interface StateDisplay {
  label: string;
  variant: "progress" | "success" | "error";
}

export const STATE_DISPLAY: Record<PlanRunState, StateDisplay> = {
  pending:          { label: "Preparing...", variant: "progress" },
  generating_epics: { label: "Generating",  variant: "progress" },
  generating_tasks: { label: "Generating",  variant: "progress" },
  auditing:         { label: "Auditing",    variant: "progress" },
  persisting:       { label: "Saving",      variant: "progress" },
  complete:         { label: "Complete",     variant: "success" },
  failed:           { label: "Failed",       variant: "error" },
};

export function isTerminalState(state: PlanRunState): boolean {
  return TERMINAL_STATES.includes(state);
}

export function getProgressText(state: PlanRunState, currentPhase: string | null): string {
  if (currentPhase) {
    return currentPhase;
  }
  return STATE_DISPLAY[state].label;
}
