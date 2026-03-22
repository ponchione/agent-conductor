import { Check } from "lucide-react";
import { useTheme } from "@/hooks/useTheme";
import { useConfig } from "@/hooks/useConfig";
import { Separator } from "@/components/ui/separator";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { RoleOverrideDropdown } from "@/components/ConfigPanel";

export function SettingsPopover() {
  const { theme, setTheme, themes } = useTheme();
  const {
    roles,
    availableModels,
    project,
    overrides,
    error,
    setOverride,
    clearOverride,
    clearAllOverrides,
  } = useConfig();

  const hasOverrides = Object.keys(overrides).length > 0;

  const maxPathLen = 36;
  const truncatedPath =
    project.path.length > maxPathLen
      ? "..." + project.path.slice(project.path.length - maxPathLen)
      : project.path;

  return (
    <div className="space-y-3">
      {/* Theme Picker */}
      <div>
        <p className="mb-2 text-xs font-medium uppercase text-muted-foreground">
          Theme
        </p>
        <TooltipProvider>
          <div className="grid grid-cols-6 gap-2">
            {themes.map((t) => {
              const isActive = theme === t.id;
              return (
                <Tooltip key={t.id}>
                  <TooltipTrigger asChild>
                    <button
                      type="button"
                      className="flex h-7 w-7 items-center justify-center rounded-full border-2 transition-transform hover:scale-110"
                      style={{
                        backgroundColor: t.accent,
                        borderColor: isActive ? t.accent : t.background,
                      }}
                      onClick={() => setTheme(t.id)}
                      aria-label={t.label}
                    >
                      {isActive && (
                        <Check
                          className="h-3.5 w-3.5"
                          style={{
                            color: t.background,
                          }}
                          strokeWidth={3}
                        />
                      )}
                    </button>
                  </TooltipTrigger>
                  <TooltipContent side="top">
                    <span>{t.label}</span>
                  </TooltipContent>
                </Tooltip>
              );
            })}
          </div>
        </TooltipProvider>
      </div>

      <Separator />

      {/* Model Overrides */}
      <div>
        <p className="mb-2 text-xs font-medium uppercase text-muted-foreground">
          Model Overrides
        </p>
        {roles.map((role) => (
          <RoleOverrideDropdown
            key={role.name}
            role={role}
            availableModels={availableModels}
            activeOverride={overrides[role.name]}
            onChange={(provider, model) =>
              setOverride(role.name, provider, model)
            }
            onClear={() => clearOverride(role.name)}
          />
        ))}
        {hasOverrides && (
          <button
            type="button"
            className="mt-1 text-xs text-blue-500 hover:underline"
            onClick={clearAllOverrides}
          >
            Reset All
          </button>
        )}
        {error && <p className="mt-2 text-xs text-red-500">{error}</p>}
      </div>

      <Separator />

      {/* Project Info */}
      <div>
        <p className="mb-1 text-xs font-medium uppercase text-muted-foreground">
          Project
        </p>
        <p className="text-sm font-medium">{project.name}</p>
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <p className="cursor-default truncate text-xs text-muted-foreground">
                {truncatedPath}
              </p>
            </TooltipTrigger>
            <TooltipContent side="top">
              <span>{project.path}</span>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>
      </div>
    </div>
  );
}
