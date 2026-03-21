import { useEffect, useState } from "react";
import { ChevronDown, ChevronRight, Loader2 } from "lucide-react";
import { getWorkflowScope } from "@/api/client";

interface WorkflowScopeProps {
  workflowId: string;
}

// Preferred key order for the context_package
const PREFERRED_KEY_ORDER = [
  "work_order",
  "file_tree",
  "conventions",
  "rag_context",
  "build_instructions",
  "constraints",
  "files_to_modify",
  "new_files",
];

function orderKeys(obj: Record<string, unknown>): string[] {
  const keys = Object.keys(obj);
  const ordered: string[] = [];

  for (const key of PREFERRED_KEY_ORDER) {
    if (keys.includes(key)) {
      ordered.push(key);
    }
  }

  for (const key of keys) {
    if (!ordered.includes(key)) {
      ordered.push(key);
    }
  }

  return ordered;
}

interface CollapsibleTreeNodeProps {
  label: string;
  value: unknown;
  depth: number;
  defaultOpen?: boolean;
}

function isMultiline(s: string): boolean {
  return s.includes("\n");
}

function CollapsibleTreeNode({ label, value, depth, defaultOpen = false }: CollapsibleTreeNodeProps) {
  const [open, setOpen] = useState(defaultOpen);

  const indent = depth * 16;

  // Null / undefined
  if (value === null || value === undefined) {
    return (
      <div className="flex items-baseline gap-2 py-1" style={{ paddingLeft: indent }}>
        <span className="text-sm font-medium text-muted-foreground">{label}:</span>
        <span className="text-sm text-zinc-500 italic">null</span>
      </div>
    );
  }

  // Number / boolean
  if (typeof value === "number" || typeof value === "boolean") {
    return (
      <div className="flex items-baseline gap-2 py-1" style={{ paddingLeft: indent }}>
        <span className="text-sm font-medium text-muted-foreground">{label}:</span>
        <span className="text-sm text-foreground">{String(value)}</span>
      </div>
    );
  }

  // String
  if (typeof value === "string") {
    if (isMultiline(value)) {
      return (
        <div className="py-1" style={{ paddingLeft: indent }}>
          <button
            type="button"
            onClick={() => setOpen(!open)}
            className="flex items-center gap-1 text-sm font-medium text-muted-foreground hover:text-foreground"
          >
            {open ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
            {label}
          </button>
          {open && (
            <pre className="mt-1 ml-5 whitespace-pre-wrap break-words rounded bg-muted/50 p-2 text-xs font-mono text-foreground">
              {value}
            </pre>
          )}
        </div>
      );
    }

    return (
      <div className="flex items-baseline gap-2 py-1" style={{ paddingLeft: indent }}>
        <span className="text-sm font-medium text-muted-foreground">{label}:</span>
        <span className="text-sm text-foreground">{value}</span>
      </div>
    );
  }

  // Array
  if (Array.isArray(value)) {
    return (
      <div className="py-1" style={{ paddingLeft: indent }}>
        <button
          type="button"
          onClick={() => setOpen(!open)}
          className="flex items-center gap-2 text-sm font-medium text-muted-foreground hover:text-foreground"
        >
          {open ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
          <span>{label}</span>
          <span className="rounded-full bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">
            {value.length}
          </span>
        </button>
        {open && (
          <div className="mt-1">
            {value.map((item, i) => (
              <CollapsibleTreeNode
                key={i}
                label={String(i)}
                value={item}
                depth={depth + 1}
              />
            ))}
          </div>
        )}
      </div>
    );
  }

  // Object
  if (typeof value === "object") {
    const obj = value as Record<string, unknown>;
    const keys = Object.keys(obj);
    return (
      <div className="py-1" style={{ paddingLeft: indent }}>
        <button
          type="button"
          onClick={() => setOpen(!open)}
          className="flex items-center gap-2 text-sm font-medium text-muted-foreground hover:text-foreground"
        >
          {open ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
          <span>{label}</span>
          <span className="rounded-full bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">
            {keys.length} key{keys.length !== 1 ? "s" : ""}
          </span>
        </button>
        {open && (
          <div className="mt-1">
            {keys.map((key) => (
              <CollapsibleTreeNode
                key={key}
                label={key}
                value={obj[key]}
                depth={depth + 1}
              />
            ))}
          </div>
        )}
      </div>
    );
  }

  // Fallback
  return (
    <div className="flex items-baseline gap-2 py-1" style={{ paddingLeft: indent }}>
      <span className="text-sm font-medium text-muted-foreground">{label}:</span>
      <span className="text-sm text-foreground">{String(value)}</span>
    </div>
  );
}

export default function WorkflowScope({ workflowId }: WorkflowScopeProps) {
  const [contextPackage, setContextPackage] = useState<Record<string, unknown> | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);

    getWorkflowScope(workflowId)
      .then((resp) => {
        if (!cancelled) setContextPackage(resp.context_package);
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          const message = err instanceof Error ? err.message : "Failed to load scope";
          if (message.includes("404") || message.toLowerCase().includes("not found")) {
            setError("No scope context package available for this workflow");
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
  }, [workflowId]);

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center p-6">
        <Loader2 className="size-6 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex items-center justify-center p-6 text-sm text-muted-foreground">
        {error}
      </div>
    );
  }

  if (!contextPackage) {
    return (
      <div className="flex items-center justify-center p-6 text-sm text-muted-foreground">
        No scope context package available for this workflow
      </div>
    );
  }

  const orderedKeys = orderKeys(contextPackage);

  return (
    <div className="space-y-1 p-4">
      {orderedKeys.map((key) => (
        <CollapsibleTreeNode
          key={key}
          label={key}
          value={contextPackage[key]}
          depth={0}
          defaultOpen={key === "work_order"}
        />
      ))}
    </div>
  );
}
