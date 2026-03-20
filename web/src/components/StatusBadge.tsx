import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

const statusStyles: Record<string, string> = {
  running: "bg-blue-500/20 text-blue-400 border-blue-500/30",
  awaiting_review: "bg-amber-500/20 text-amber-400 border-amber-500/30",
  completed: "bg-green-500/20 text-green-400 border-green-500/30",
  failed: "bg-red-500/20 text-red-400 border-red-500/30",
  pending: "bg-zinc-500/20 text-zinc-400 border-zinc-500/30",
  partial: "bg-orange-500/20 text-orange-400 border-orange-500/30",
};

interface StatusBadgeProps {
  status: string;
  size?: "sm" | "md";
}

export function StatusBadge({ status, size = "md" }: StatusBadgeProps) {
  const style = statusStyles[status] ?? statusStyles.pending;
  return (
    <Badge
      variant="outline"
      className={cn(
        style,
        size === "sm" && "px-1.5 py-0 text-xs",
        size === "md" && "px-2 py-0.5 text-xs"
      )}
    >
      {status.replace(/_/g, " ")}
    </Badge>
  );
}
