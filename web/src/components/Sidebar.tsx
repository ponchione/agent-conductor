import { useState } from "react";
import { NavLink } from "react-router-dom";
import { ClipboardList, GitBranch, BarChart3 } from "lucide-react";
import { cn } from "@/lib/utils";
import { Separator } from "@/components/ui/separator";
import { useQueue } from "@/hooks/useQueue";
import { QueueStrip } from "@/components/QueueStrip";
import { QueueDrawer } from "@/components/QueueDrawer";

const navItems = [
  { to: "/plan", label: "Plan", icon: ClipboardList },
  { to: "/pipeline", label: "Pipeline", icon: GitBranch },
  { to: "/analytics", label: "Analytics", icon: BarChart3 },
] as const;

export function Sidebar() {
  const [drawerOpen, setDrawerOpen] = useState(false);
  const queue = useQueue();

  return (
    <div className="flex h-full flex-col p-4">
      {/* Navigation section */}
      <div className="space-y-1">
        <h1 className="mb-4 text-lg font-bold">topham</h1>
        {navItems.map(({ to, label, icon: Icon }) => (
          <NavLink
            key={to}
            to={to}
            className={({ isActive }) =>
              cn(
                "flex items-center gap-2 rounded-md px-3 py-2",
                isActive
                  ? "bg-accent text-accent-foreground"
                  : "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground",
              )
            }
          >
            <Icon className="h-4 w-4" />
            <span>{label}</span>
            <span className="ml-auto" />
          </NavLink>
        ))}
      </div>

      {/* Queue strip */}
      <div className="mt-6">
        <Separator className="mb-4" />
        <QueueStrip state={queue.state} onManageClick={() => setDrawerOpen(true)} />
      </div>
      <QueueDrawer open={drawerOpen} onClose={() => setDrawerOpen(false)} queue={queue} />

      {/* Config panel — pushed to bottom */}
      <div className="mt-auto">
        <Separator className="mb-4" />
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
      </div>
    </div>
  );
}
