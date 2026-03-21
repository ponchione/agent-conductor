import { useConfig } from "@/hooks/useConfig";
import type { ModelOverride, ProviderModels, RoleConfig } from "@/types/api";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";

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

/** Sidebar config panel showing project info, connection status, and role override dropdowns. */
export function ConfigPanel() {
  const {
    roles,
    availableModels,
    project,
    overrides,
    loading,
    error,
    setOverride,
    clearOverride,
    clearAllOverrides,
  } = useConfig();

  if (loading) {
    return (
      <p className="text-sm text-muted-foreground">Loading...</p>
    );
  }

  if (!project.name) {
    return (
      <>
        <p className="mb-2 text-xs font-medium uppercase text-muted-foreground">
          Config
        </p>
        <p className="mb-3 text-sm text-muted-foreground">
          No project loaded
        </p>
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <span className="inline-block h-2 w-2 rounded-full bg-zinc-500" />
          <span>Disconnected</span>
        </div>
      </>
    );
  }

  const hasOverrides = Object.keys(overrides).length > 0;

  // Truncate path for display; show full path in tooltip.
  const maxPathLen = 30;
  const truncatedPath =
    project.path.length > maxPathLen
      ? "..." + project.path.slice(project.path.length - maxPathLen)
      : project.path;

  return (
    <div>
      <p className="mb-2 text-xs font-medium uppercase text-muted-foreground">
        Config
      </p>

      {/* Project name */}
      <p className="mb-0.5 text-sm font-medium">{project.name}</p>

      {/* Truncated path with tooltip */}
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            <p className="mb-2 cursor-default truncate text-xs text-muted-foreground">
              {truncatedPath}
            </p>
          </TooltipTrigger>
          <TooltipContent side="top">
            <span>{project.path}</span>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>

      {/* Connection status */}
      <div className="mb-3 flex items-center gap-2 text-sm text-muted-foreground">
        <span className="inline-block h-2 w-2 rounded-full bg-green-500" />
        <span>Connected</span>
      </div>

      {/* Role override dropdowns */}
      {roles.map((role) => (
        <RoleOverrideDropdown
          key={role.name}
          role={role}
          availableModels={availableModels}
          activeOverride={overrides[role.name]}
          onChange={(provider, model) => setOverride(role.name, provider, model)}
          onClear={() => clearOverride(role.name)}
        />
      ))}

      {/* Reset All link */}
      {hasOverrides && (
        <button
          type="button"
          className="mt-1 text-xs text-blue-500 hover:underline"
          onClick={clearAllOverrides}
        >
          Reset All
        </button>
      )}

      {/* Error message */}
      {error && (
        <p className="mt-2 text-xs text-red-500">{error}</p>
      )}
    </div>
  );
}
