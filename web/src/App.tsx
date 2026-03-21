import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import RootLayout from "@/layouts/RootLayout";
import PlanSpace from "@/pages/PlanSpace";
import NewPlan from "@/pages/NewPlan";
import PlanRunDetail from "@/pages/PlanRunDetail";
import PipelineSpace from "@/pages/PipelineSpace";
import WorkflowDetail from "@/pages/WorkflowDetail";
import AnalyticsSpace from "@/pages/AnalyticsSpace";

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route element={<RootLayout />}>
          <Route index element={<Navigate to="/pipeline" replace />} />
          <Route path="plan" element={<PlanSpace />}>
            <Route path="new" element={<NewPlan />} />
            <Route path=":planRunId" element={<PlanRunDetail />} />
          </Route>
          <Route path="pipeline" element={<PipelineSpace />}>
            <Route path=":workflowId" element={<WorkflowDetail />} />
            <Route path=":workflowId/:tab" element={<WorkflowDetail />} />
          </Route>
          <Route path="analytics" element={<AnalyticsSpace />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
