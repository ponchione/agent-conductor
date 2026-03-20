import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import RootLayout from "@/layouts/RootLayout";
import PlanSpace from "@/pages/PlanSpace";
import PipelineSpace from "@/pages/PipelineSpace";
import AnalyticsSpace from "@/pages/AnalyticsSpace";

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route element={<RootLayout />}>
          <Route index element={<Navigate to="/pipeline" replace />} />
          <Route path="plan" element={<PlanSpace />} />
          <Route path="pipeline">
            <Route index element={<PipelineSpace />} />
            <Route path=":workflowId" element={<PipelineSpace />} />
            <Route path=":workflowId/:tab" element={<PipelineSpace />} />
          </Route>
          <Route path="analytics" element={<AnalyticsSpace />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
