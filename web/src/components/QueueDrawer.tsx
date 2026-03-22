import { useState, useCallback, useEffect, useRef } from "react";
import { useNavigate } from "react-router-dom";
import {
  X,
  Play,
  Pause,
  SkipForward,
  GripVertical,
  Trash2,
  ChevronDown,
  ChevronRight,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { StatusBadge } from "@/components/StatusBadge";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { useConfig } from "@/hooks/useConfig";
import { RoleOverrideDropdown, formatRoleName } from "@/components/ConfigPanel";
import { cn } from "@/lib/utils";
import type { QueueItem, ModelOverride, RoleConfig, ProviderModels } from "@/types/api";
import type { useQueue } from "@/hooks/useQueue";

interface QueueDrawerProps {
  open: boolean;
  onClose: () => void;
  queue: ReturnType<typeof useQueue>;
}

export function QueueDrawer({ open, onClose, queue }: QueueDrawerProps) {
  const navigate = useNavigate();
  const config = useConfig();
  const [clearConfirmOpen, setClearConfirmOpen] = useState(false);
  const [dragOverId, setDragOverId] = useState<string | null>(null);
  const [draggedId, setDraggedId] = useState<string | null>(null);
  const [expandedOverrides, setExpandedOverrides] = useState<Set<string>>(
    new Set(),
  );

  const { state } = queue;
  const pendingItems = state.items.filter((i) => i.status === "pending");
  const completedItems = state.items.filter(
    (i) => i.status === "completed" || i.status === "failed" || i.status === "skipped",
  );
  const executingItem = state.items.find((i) => i.status === "executing");
  const hasPending = pendingItems.length > 0;

  const toggleOverrides = useCallback((id: string) => {
    setExpandedOverrides((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  }, []);

  const handleDragStart = useCallback(
    (e: React.DragEvent<HTMLDivElement>, id: string) => {
      e.dataTransfer.effectAllowed = "move";
      e.dataTransfer.setData("text/plain", id);
      setDraggedId(id);
    },
    [],
  );

  const handleDragOver = useCallback(
    (e: React.DragEvent<HTMLDivElement>, id: string) => {
      e.preventDefault();
      e.dataTransfer.dropEffect = "move";
      setDragOverId(id);
    },
    [],
  );

  const handleDragLeave = useCallback(() => {
    setDragOverId(null);
  }, []);

  const handleDrop = useCallback(
    (e: React.DragEvent<HTMLDivElement>, targetId: string) => {
      e.preventDefault();
      setDragOverId(null);
      setDraggedId(null);

      const sourceId = e.dataTransfer.getData("text/plain");
      if (!sourceId || sourceId === targetId) return;

      const ids = pendingItems.map((i) => i.id);
      const sourceIdx = ids.indexOf(sourceId);
      const targetIdx = ids.indexOf(targetId);
      if (sourceIdx === -1 || targetIdx === -1) return;

      ids.splice(sourceIdx, 1);
      ids.splice(targetIdx, 0, sourceId);
      queue.reorder(ids);
    },
    [pendingItems, queue],
  );

  const handleDragEnd = useCallback(() => {
    setDragOverId(null);
    setDraggedId(null);
  }, []);

  const handleClearConfirm = useCallback(() => {
    setClearConfirmOpen(false);
    for (const item of pendingItems) {
      queue.removeItem(item.id);
    }
  }, [pendingItems, queue]);

  const drawerRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<Element | null>(null);

  useEffect(() => {
    if (open) {
      // Remember the element that had focus before opening
      triggerRef.current = document.activeElement;
      // Focus the first focusable element inside the drawer
      const timer = requestAnimationFrame(() => {
        const focusable = drawerRef.current?.querySelector<HTMLElement>(
          'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
        );
        focusable?.focus();
      });
      return () => cancelAnimationFrame(timer);
    } else {
      // Return focus to the trigger that opened the drawer
      if (triggerRef.current instanceof HTMLElement) {
        triggerRef.current.focus();
      }
      triggerRef.current = null;
    }
  }, [open]);

  return (
    <>
      {/* Backdrop */}
      <div
        className={cn(
          "fixed inset-0 z-40 bg-black/50 transition-opacity",
          open ? "opacity-100 duration-200" : "opacity-0 pointer-events-none duration-150",
        )}
        aria-hidden={!open}
        onClick={onClose}
      />

      {/* Drawer */}
      <div
        ref={drawerRef}
        role="dialog"
        aria-label="Run Queue"
        aria-hidden={!open}
        className={cn(
          "fixed inset-y-0 right-0 z-50 flex w-full max-w-lg flex-col border-l bg-background shadow-xl",
          "transition-transform",
          open ? "translate-x-0 duration-250 ease-out" : "translate-x-full duration-200 ease-in",
        )}
      >
        {/* Header */}
        <div className="flex items-center justify-between border-b px-4 py-3">
          <div className="flex items-center gap-3">
            <h2 className="text-lg font-semibold">Run Queue</h2>
            <StatusBadge status={state.state} size="sm" />
          </div>
          <div className="flex items-center gap-2">
            {state.state === "ready" && (
              <Button size="sm" onClick={queue.start}>
                <Play className="h-4 w-4" />
                Start
              </Button>
            )}
            {state.state === "paused" && (
              <Button size="sm" onClick={queue.continue_}>
                <SkipForward className="h-4 w-4" />
                Continue
              </Button>
            )}
            {state.state === "running" && (
              <Button size="sm" variant="outline" onClick={queue.pause}>
                <Pause className="h-4 w-4" />
                Pause
              </Button>
            )}
            {hasPending && (
              <Button
                size="sm"
                variant="ghost"
                onClick={() => setClearConfirmOpen(true)}
              >
                Clear
              </Button>
            )}
            <Button size="icon-sm" variant="ghost" onClick={onClose}>
              <X className="h-4 w-4" />
            </Button>
          </div>
        </div>

        {/* Item list */}
        <div className="flex-1 overflow-y-auto p-4 space-y-4">
          {/* Completed items */}
          {completedItems.length > 0 && (
            <div className="space-y-2">
              <p className="text-xs font-medium uppercase text-muted-foreground">
                Completed
              </p>
              {completedItems.map((item) => (
                <CompletedItemRow
                  key={item.id}
                  item={item}
                  onView={
                    item.workflow_id
                      ? () => navigate(`/pipeline/${item.workflow_id}`)
                      : undefined
                  }
                />
              ))}
            </div>
          )}

          {/* Executing item */}
          {executingItem && (
            <div className="space-y-2">
              <p className="text-xs font-medium uppercase text-muted-foreground">
                Executing
              </p>
              <ExecutingItemRow
                item={executingItem}
                onView={
                  executingItem.workflow_id
                    ? () => navigate(`/pipeline/${executingItem.workflow_id}`)
                    : undefined
                }
              />
            </div>
          )}

          {/* Pending items */}
          {pendingItems.length > 0 && (
            <div className="space-y-2">
              <p className="text-xs font-medium uppercase text-muted-foreground">
                Pending
              </p>
              {pendingItems.map((item) => (
                <PendingItemRow
                  key={item.id}
                  item={item}
                  roles={config.roles}
                  availableModels={config.availableModels}
                  sessionOverrides={config.overrides}
                  isDragOver={dragOverId === item.id}
                  isDragged={draggedId === item.id}
                  isExpanded={expandedOverrides.has(item.id)}
                  onToggleOverrides={() => toggleOverrides(item.id)}
                  onRemove={() => queue.removeItem(item.id)}
                  onDragStart={(e) => handleDragStart(e, item.id)}
                  onDragOver={(e) => handleDragOver(e, item.id)}
                  onDragLeave={handleDragLeave}
                  onDrop={(e) => handleDrop(e, item.id)}
                  onDragEnd={handleDragEnd}
                />
              ))}
            </div>
          )}

          {state.items.length === 0 && (
            <p className="py-8 text-center text-sm text-muted-foreground">
              No items in queue
            </p>
          )}
        </div>
      </div>

      {/* Clear confirm dialog */}
      <ConfirmDialog
        open={clearConfirmOpen}
        title="Clear pending items"
        description={`Remove all ${pendingItems.length} pending item${pendingItems.length === 1 ? "" : "s"} from the queue?`}
        confirmLabel="Clear"
        onConfirm={handleClearConfirm}
        onCancel={() => setClearConfirmOpen(false)}
      />
    </>
  );
}

// --- Sub-components ---

function CompletedItemRow({
  item,
  onView,
}: {
  item: QueueItem;
  onView?: () => void;
}) {
  return (
    <div className="flex items-center gap-2 rounded-md border px-3 py-2 opacity-60">
      <StatusBadge status={item.status} size="sm" />
      <span className="flex-1 truncate text-sm">{item.title}</span>
      {onView && (
        <Button size="xs" variant="ghost" onClick={onView}>
          View
        </Button>
      )}
    </div>
  );
}

function ExecutingItemRow({
  item,
  onView,
}: {
  item: QueueItem;
  onView?: () => void;
}) {
  return (
    <div className="flex items-center gap-2 rounded-md border border-blue-500/50 bg-blue-500/5 px-3 py-2">
      <span className="inline-block h-2 w-2 animate-pulse rounded-full bg-blue-500" />
      <span className="flex-1 truncate text-sm font-medium">{item.title}</span>
      {onView && (
        <Button size="xs" variant="ghost" onClick={onView}>
          View
        </Button>
      )}
    </div>
  );
}

function PendingItemRow({
  item,
  roles,
  availableModels,
  sessionOverrides,
  isDragOver,
  isDragged,
  isExpanded,
  onToggleOverrides,
  onRemove,
  onDragStart,
  onDragOver,
  onDragLeave,
  onDrop,
  onDragEnd,
}: {
  item: QueueItem;
  roles: RoleConfig[];
  availableModels: ProviderModels[];
  sessionOverrides: Record<string, ModelOverride>;
  isDragOver: boolean;
  isDragged: boolean;
  isExpanded: boolean;
  onToggleOverrides: () => void;
  onRemove: () => void;
  onDragStart: (e: React.DragEvent<HTMLDivElement>) => void;
  onDragOver: (e: React.DragEvent<HTMLDivElement>) => void;
  onDragLeave: () => void;
  onDrop: (e: React.DragEvent<HTMLDivElement>) => void;
  onDragEnd: () => void;
}) {
  const hasOverrides =
    item.overrides && Object.keys(item.overrides).length > 0;

  return (
    <div
      draggable
      onDragStart={onDragStart}
      onDragOver={onDragOver}
      onDragLeave={onDragLeave}
      onDrop={onDrop}
      onDragEnd={onDragEnd}
      className={`rounded-md border px-3 py-2 transition-colors ${
        isDragOver ? "border-blue-400 bg-blue-500/10" : ""
      } ${isDragged ? "opacity-50" : ""}`}
    >
      <div className="flex items-center gap-2">
        <GripVertical className="h-4 w-4 shrink-0 cursor-grab text-muted-foreground" />
        <div className="flex-1 min-w-0">
          <p className="truncate text-sm font-medium">{item.title}</p>
          <p className="text-xs text-muted-foreground">
            {item.type}
            {item.target_module ? ` \u00B7 ${item.target_module}` : ""}
          </p>
        </div>
        <div className="flex items-center gap-1">
          <Button
            size="xs"
            variant="ghost"
            onClick={onToggleOverrides}
            className="text-xs text-muted-foreground"
          >
            {isExpanded ? (
              <ChevronDown className="h-3 w-3" />
            ) : (
              <ChevronRight className="h-3 w-3" />
            )}
            Overrides
          </Button>
          <Button size="icon-xs" variant="ghost" onClick={onRemove}>
            <Trash2 className="h-3 w-3" />
          </Button>
        </div>
      </div>
      {isExpanded && (
        <div className="mt-2 space-y-2 rounded bg-muted/50 p-2">
          <p className="text-xs font-medium text-muted-foreground">Override for this run</p>
          {roles.map((role) => {
            const itemOverrideRaw = item.overrides?.[role.name];
            let activeOverride: ModelOverride | undefined;
            if (itemOverrideRaw) {
              const parts = itemOverrideRaw.split("::");
              if (parts.length === 2) {
                activeOverride = { provider: parts[0], model: parts[1] };
              }
            } else if (sessionOverrides[role.name]) {
              activeOverride = sessionOverrides[role.name];
            }

            return (
              <RoleOverrideDropdown
                key={role.name}
                role={role}
                availableModels={availableModels}
                activeOverride={activeOverride}
                onChange={() => {}}
                onClear={() => {}}
                disabled
              />
            );
          })}
        </div>
      )}
    </div>
  );
}
