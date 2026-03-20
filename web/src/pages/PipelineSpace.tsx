import { Outlet, useParams } from "react-router-dom";

export default function PipelineSpace() {
  const { workflowId } = useParams();

  if (workflowId) {
    return <Outlet />;
  }

  return (
    <div className="grid h-full grid-cols-[350px_1fr]">
      {/* List panel */}
      <div className="border-r border-border p-6">
        <h2 className="mb-4 text-sm font-medium uppercase tracking-wider text-muted-foreground">
          Workflows
        </h2>
        <p className="text-sm text-muted-foreground">Workflows will appear here</p>
      </div>
      {/* Detail panel */}
      <div className="flex items-center justify-center p-6">
        <p className="text-sm text-muted-foreground">Select a workflow to view details</p>
      </div>
    </div>
  );
}
