export default function PlanSpace() {
  return (
    <div className="grid h-full grid-cols-[350px_1fr]">
      {/* List panel */}
      <div className="border-r border-border p-6">
        <h2 className="mb-4 text-sm font-medium uppercase tracking-wider text-muted-foreground">
          Plan Runs
        </h2>
        <p className="text-sm text-muted-foreground">Plan runs will appear here</p>
      </div>
      {/* Detail panel */}
      <div className="flex items-center justify-center p-6">
        <p className="text-sm text-muted-foreground">Select a plan run to view details</p>
      </div>
    </div>
  );
}
