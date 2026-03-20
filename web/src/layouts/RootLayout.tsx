import { Outlet } from "react-router-dom";
import { Sidebar } from "@/components/Sidebar";

export default function RootLayout() {
  return (
    <div className="flex h-screen bg-background text-foreground">
      <aside className="w-64 shrink-0 border-r border-border bg-card">
        <Sidebar />
      </aside>
      <main className="flex-1 overflow-auto">
        <Outlet />
      </main>
    </div>
  );
}
