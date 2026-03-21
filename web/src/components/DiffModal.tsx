import { useEffect, useState } from "react";
import { Loader2, X } from "lucide-react";
import { getWorkflowDiff } from "@/api/client";
import type { WorkflowDiffResponse } from "@/types/api";
import { Button } from "@/components/ui/button";

interface DiffModalProps {
  open: boolean;
  onClose: () => void;
  workflowId: string;
}

export function DiffModal({ open, onClose, workflowId }: DiffModalProps) {
  const [data, setData] = useState<WorkflowDiffResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) {
      setData(null);
      setError(null);
      return;
    }

    let cancelled = false;
    setLoading(true);
    setError(null);

    getWorkflowDiff(workflowId)
      .then((resp) => {
        if (!cancelled) setData(resp);
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          const message = err instanceof Error ? err.message : "Failed to load diff";
          // Handle 404 gracefully
          if (message.includes("404") || message.toLowerCase().includes("not found")) {
            setError("Branch no longer exists. Diff is unavailable.");
          } else {
            setError(message);
          }
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [open, workflowId]);

  // Close on escape key
  useEffect(() => {
    if (!open) return;
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [open, onClose]);

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center"
      style={{ backgroundColor: "rgba(0,0,0,0.8)" }}
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        className="flex flex-col rounded-lg border border-border bg-card"
        style={{ width: "95vw", height: "95vh" }}
      >
        {/* Header */}
        <div className="flex items-center justify-between border-b border-border px-4 py-3">
          <div className="flex items-center gap-4 text-sm">
            {data && (
              <>
                <span className="text-muted-foreground">
                  {data.stats.files} file{data.stats.files !== 1 ? "s" : ""} changed
                </span>
                <span className="text-green-400">+{data.stats.additions}</span>
                <span className="text-red-400">-{data.stats.deletions}</span>
              </>
            )}
          </div>
          <Button variant="ghost" size="icon-sm" onClick={onClose}>
            <X className="size-4" />
          </Button>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-auto p-4">
          {loading && (
            <div className="flex h-full items-center justify-center">
              <Loader2 className="size-6 animate-spin text-muted-foreground" />
            </div>
          )}

          {error && (
            <div className="flex h-full items-center justify-center">
              <p className="text-sm text-muted-foreground">{error}</p>
            </div>
          )}

          {data && !loading && !error && (
            <pre
              className="whitespace-pre-wrap break-words rounded-lg p-4 text-xs leading-5 font-mono"
              style={{
                backgroundColor: "#1a1a2e",
                color: "#d4d4d8",
              }}
            >
              {data.diff}
            </pre>
          )}
        </div>

        {/* Footer */}
        {data && !loading && !error && (
          <div className="border-t border-border px-4 py-3">
            <details className="text-sm">
              <summary className="cursor-pointer text-muted-foreground hover:text-foreground">
                Changed files ({data.files_changed.length})
              </summary>
              <ul className="mt-2 space-y-1 pl-4">
                {data.files_changed.map((file) => (
                  <li key={file} className="font-mono text-xs text-muted-foreground">
                    {file}
                  </li>
                ))}
              </ul>
            </details>
          </div>
        )}
      </div>
    </div>
  );
}
