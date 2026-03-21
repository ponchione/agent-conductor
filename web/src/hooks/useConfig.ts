import { useCallback, useEffect, useRef, useState } from "react";
import type {
  ConfigRolesResponse,
  ConfigOverridesResponse,
  ModelOverride,
  ProjectInfo,
  ProviderModels,
  RoleConfig,
} from "../types/api";
import {
  getConfigRoles,
  getConfigOverrides,
  putConfigOverrides,
} from "../api/client";

const EMPTY_PROJECT: ProjectInfo = { name: "", path: "", data_dir: "" };

export function useConfig() {
  const [roles, setRoles] = useState<RoleConfig[]>([]);
  const [availableModels, setAvailableModels] = useState<ProviderModels[]>([]);
  const [project, setProject] = useState<ProjectInfo>(EMPTY_PROJECT);
  const [overrides, setOverrides] = useState<Record<string, ModelOverride>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const mountedRef = useRef(true);

  const fetchAll = useCallback(async () => {
    try {
      const [rolesResp, overridesResp] = await Promise.all([
        getConfigRoles(),
        getConfigOverrides(),
      ]);
      if (mountedRef.current) {
        setRoles(rolesResp.roles ?? []);
        setAvailableModels(rolesResp.available_models ?? []);
        setProject(rolesResp.project);
        setOverrides(overridesResp.overrides ?? {});
        setError(null);
      }
    } catch (err) {
      if (mountedRef.current) {
        setError(err instanceof Error ? err.message : "Failed to fetch config");
      }
    }
  }, []);

  const refresh = useCallback(() => {
    fetchAll();
  }, [fetchAll]);

  useEffect(() => {
    mountedRef.current = true;
    fetchAll().finally(() => {
      if (mountedRef.current) setLoading(false);
    });
    return () => {
      mountedRef.current = false;
    };
  }, [fetchAll]);

  const setOverride = useCallback(
    async (role: string, provider: string, model: string) => {
      try {
        const next = { ...overrides, [role]: { provider, model } };
        const resp = await putConfigOverrides(next);
        if (mountedRef.current) {
          setOverrides(resp.overrides ?? {});
          setError(null);
        }
      } catch (err) {
        if (mountedRef.current) {
          setError(err instanceof Error ? err.message : "Failed to set override");
        }
      }
    },
    [overrides],
  );

  const clearOverride = useCallback(
    async (role: string) => {
      try {
        const next = { ...overrides };
        delete next[role];
        const resp = await putConfigOverrides(next);
        if (mountedRef.current) {
          setOverrides(resp.overrides ?? {});
          setError(null);
        }
      } catch (err) {
        if (mountedRef.current) {
          setError(
            err instanceof Error ? err.message : "Failed to clear override",
          );
        }
      }
    },
    [overrides],
  );

  const clearAllOverrides = useCallback(async () => {
    try {
      const resp = await putConfigOverrides({});
      if (mountedRef.current) {
        setOverrides(resp.overrides ?? {});
        setError(null);
      }
    } catch (err) {
      if (mountedRef.current) {
        setError(
          err instanceof Error ? err.message : "Failed to clear overrides",
        );
      }
    }
  }, []);

  return {
    roles,
    availableModels,
    project,
    overrides,
    loading,
    error,
    setOverride,
    clearOverride,
    clearAllOverrides,
    refresh,
  };
}
