import { useState } from "react";
import { NavLink } from "react-router-dom";
import { ClipboardList, GitBranch, BarChart3, Settings } from "lucide-react";
import { cn } from "@/lib/utils";
import { Separator } from "@/components/ui/separator";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { useQueue } from "@/hooks/useQueue";
import { QueueStrip } from "@/components/QueueStrip";
import { QueueDrawer } from "@/components/QueueDrawer";
import { ConfigIndicator } from "@/components/ConfigPanel";
import { SettingsPopover } from "@/components/SettingsPopover";

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

      {/* Config indicator — pushed to bottom */}
      <div className="mt-auto">
        <Separator className="mb-4" />
        <Popover>
          <ConfigIndicator
            gearButton={
              <PopoverTrigger asChild>
                <button
                  type="button"
                  className="shrink-0 rounded-md p-1 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
                  aria-label="Settings"
                >
                  <Settings className="h-4 w-4" />
                </button>
              </PopoverTrigger>
            }
          />
          <PopoverContent side="top" align="start" className="w-80">
            <SettingsPopover />
          </PopoverContent>
        </Popover>
      </div>
    </div>
  );
}
