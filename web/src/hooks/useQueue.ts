import { useCallback, useEffect, useRef, useState } from "react";
import type { QueueAddItem, QueueState } from "../types/api";
import {
  addQueueItems,
  continueQueue,
  getQueue,
  pauseQueue,
  reorderQueue,
  removeQueueItem,
  startQueue,
} from "../api/client";

const EMPTY_STATE: QueueState = { state: "idle", items: [], current: undefined };
const POLL_INTERVAL_MS = 3000;

export function useQueue() {
  const [state, setState] = useState<QueueState>(EMPTY_STATE);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const mountedRef = useRef(true);

  const fetchState = useCallback(async () => {
    try {
      const data = await getQueue();
      if (mountedRef.current) {
        setState(data);
        setError(null);
      }
    } catch (err) {
      if (mountedRef.current) {
        setError(err instanceof Error ? err.message : "Failed to fetch queue");
      }
    }
  }, []);

  const refresh = useCallback(() => {
    fetchState();
  }, [fetchState]);

  // Initial fetch
  useEffect(() => {
    mountedRef.current = true;
    fetchState().finally(() => {
      if (mountedRef.current) setLoading(false);
    });
    return () => {
      mountedRef.current = false;
    };
  }, [fetchState]);

  // Polling
  useEffect(() => {
    const id = setInterval(fetchState, POLL_INTERVAL_MS);
    return () => clearInterval(id);
  }, [fetchState]);

  const addItems = useCallback(async (items: QueueAddItem[]) => {
    try {
      const data = await addQueueItems(items);
      if (mountedRef.current) {
        setState(data);
        setError(null);
      }
    } catch (err) {
      if (mountedRef.current) {
        setError(err instanceof Error ? err.message : "Failed to add items");
      }
    }
  }, []);

  const removeItem = useCallback(async (id: string) => {
    try {
      const data = await removeQueueItem(id);
      if (mountedRef.current) {
        setState(data);
        setError(null);
      }
    } catch (err) {
      if (mountedRef.current) {
        setError(err instanceof Error ? err.message : "Failed to remove item");
      }
    }
  }, []);

  const reorder = useCallback(async (order: string[]) => {
    try {
      const data = await reorderQueue(order);
      if (mountedRef.current) {
        setState(data);
        setError(null);
      }
    } catch (err) {
      if (mountedRef.current) {
        setError(err instanceof Error ? err.message : "Failed to reorder queue");
      }
    }
  }, []);

  const start = useCallback(async () => {
    try {
      const data = await startQueue();
      if (mountedRef.current) {
        setState(data);
        setError(null);
      }
    } catch (err) {
      if (mountedRef.current) {
        setError(err instanceof Error ? err.message : "Failed to start queue");
      }
    }
  }, []);

  const pause = useCallback(async () => {
    try {
      const data = await pauseQueue();
      if (mountedRef.current) {
        setState(data);
        setError(null);
      }
    } catch (err) {
      if (mountedRef.current) {
        setError(err instanceof Error ? err.message : "Failed to pause queue");
      }
    }
  }, []);

  const continue_ = useCallback(async () => {
    try {
      const data = await continueQueue();
      if (mountedRef.current) {
        setState(data);
        setError(null);
      }
    } catch (err) {
      if (mountedRef.current) {
        setError(err instanceof Error ? err.message : "Failed to continue queue");
      }
    }
  }, []);

  return {
    state,
    loading,
    error,
    refresh,
    addItems,
    removeItem,
    reorder,
    start,
    pause,
    continue_,
  };
}
