import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import RootLayout from "@/layouts/RootLayout";
import PlanSpace from "@/pages/PlanSpace";
import PipelineSpace from "@/pages/PipelineSpace";
import WorkflowDetail from "@/pages/WorkflowDetail";
import AnalyticsSpace from "@/pages/AnalyticsSpace";

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route element={<RootLayout />}>
          <Route index element={<Navigate to="/pipeline" replace />} />
          <Route path="plan" element={<PlanSpace />} />
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
