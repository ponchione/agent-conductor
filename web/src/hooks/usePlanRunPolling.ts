import { useCallback, useEffect, useRef, useState } from "react";
import type { PlanRunDetailResponse } from "@/types/api";
import { getPlanRun } from "@/api/client";
import { isTerminalState } from "@/lib/planRunState";

const POLL_INTERVAL_MS = 3000;

export function usePlanRunPolling(planRunId: string | undefined): {
  run: PlanRunDetailResponse | null;
  loading: boolean;
  error: string | null;
} {
  const [run, setRun] = useState<PlanRunDetailResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const mountedRef = useRef(true);
  const inFlightRef = useRef(false);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const clearPolling = useCallback(() => {
    if (intervalRef.current !== null) {
      clearInterval(intervalRef.current);
      intervalRef.current = null;
    }
  }, []);

  const fetchRun = useCallback(async () => {
    if (!planRunId || inFlightRef.current) return;

    inFlightRef.current = true;
    try {
      const data = await getPlanRun(planRunId);
      if (mountedRef.current) {
        setRun(data);
        setError(null);
        setLoading(false);

        // Stop polling on terminal state
        if (isTerminalState(data.state)) {
          clearPolling();
        }
      }
    } catch (err) {
      if (mountedRef.current) {
        setError(err instanceof Error ? err.message : "Failed to fetch plan run");
        setLoading(false);
      }
    } finally {
      inFlightRef.current = false;
    }
  }, [planRunId, clearPolling]);

  // Initial fetch and polling setup
  useEffect(() => {
    mountedRef.current = true;
    inFlightRef.current = false;

    if (!planRunId) {
      setRun(null);
      setLoading(false);
      setError(null);
      return;
    }

    setLoading(true);
    setError(null);
    setRun(null);

    // Initial fetch
    fetchRun();

    // Start polling interval
    intervalRef.current = setInterval(fetchRun, POLL_INTERVAL_MS);

    return () => {
      mountedRef.current = false;
      clearPolling();
    };
  }, [planRunId, fetchRun, clearPolling]);

  return { run, loading, error };
}
