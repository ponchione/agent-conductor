import { Settings } from "lucide-react";
import { useConfig } from "@/hooks/useConfig";
import type { ModelOverride, ProviderModels, RoleConfig } from "@/types/api";

/** Converts a snake_case role name to Title Case (e.g. "scope_decompose" → "Scope Decompose"). */
export function formatRoleName(name: string): string {
  return name
    .split("_")
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(" ");
}

interface RoleOverrideDropdownProps {
  role: RoleConfig;
  availableModels: ProviderModels[];
  activeOverride?: ModelOverride;
  onChange: (provider: string, model: string) => void;
  onClear: () => void;
  label?: string;
  disabled?: boolean;
}

/** Dropdown for selecting a model override for a single pipeline role. */
export function RoleOverrideDropdown({
  role,
  availableModels,
  activeOverride,
  onChange,
  onClear,
  label,
  disabled,
}: RoleOverrideDropdownProps) {
  const currentValue = activeOverride
    ? `${activeOverride.provider}::${activeOverride.model}`
    : "";

  function handleChange(e: React.ChangeEvent<HTMLSelectElement>) {
    const val = e.target.value;
    if (val === "") {
      onClear();
      return;
    }
    const [provider, model] = val.split("::");
    onChange(provider, model);
  }

  return (
    <div className="mb-2">
      <label className="mb-1 flex items-center gap-1.5 text-xs text-muted-foreground">
        {activeOverride && (
          <span className="inline-block h-1.5 w-1.5 rounded-full bg-blue-500" />
        )}
        {label ?? formatRoleName(role.name)}
      </label>
      <select
        className="w-full rounded-md border border-input bg-background px-2 py-1 text-sm text-foreground disabled:cursor-not-allowed disabled:opacity-50"
        value={currentValue}
        onChange={handleChange}
        disabled={disabled}
      >
        <option value="">Default ({role.current_model})</option>
        {availableModels.map((pm) => (
          <optgroup key={pm.provider} label={pm.provider}>
            {pm.models.map((model) => (
              <option key={`${pm.provider}::${model}`} value={`${pm.provider}::${model}`}>
                {model}
              </option>
            ))}
          </optgroup>
        ))}
      </select>
    </div>
  );
}

interface ConfigIndicatorProps {
  /** Render prop for the gear icon button. The caller wraps this with a PopoverTrigger. */
  gearButton?: React.ReactNode;
}

/** Compact config indicator for the sidebar bottom. The gear button is injected by the parent to avoid circular imports. */
export function ConfigIndicator({ gearButton }: ConfigIndicatorProps) {
  const { project, overrides, loading } = useConfig();

  const settingsButton = gearButton ?? (
    <button
      type="button"
      className="shrink-0 rounded-md p-1 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
      aria-label="Settings"
    >
      <Settings className="h-4 w-4" />
    </button>
  );

  if (loading) {
    return <p className="text-sm text-muted-foreground">Loading...</p>;
  }

  if (!project.name) {
    return (
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <span className="inline-block h-2 w-2 rounded-full bg-zinc-500" />
          <span>Disconnected</span>
        </div>
        {settingsButton}
      </div>
    );
  }

  const overrideCount = Object.keys(overrides).length;

  return (
    <div className="flex items-center justify-between gap-2">
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium">{project.name}</p>
        <div className="flex items-center gap-1.5">
          <span className="inline-block h-2 w-2 shrink-0 rounded-full bg-green-500" />
          <span className="text-xs text-muted-foreground">Connected</span>
          {overrideCount > 0 && (
            <span className="ml-1 rounded-full bg-blue-500/15 px-1.5 py-0.5 text-[10px] font-medium text-blue-500">
              {overrideCount} {overrideCount === 1 ? "override" : "overrides"}
            </span>
          )}
        </div>
      </div>
      {settingsButton}
    </div>
  );
}
