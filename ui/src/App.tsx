import { lazy, Suspense } from "react";
import { Navigate, Route, Routes } from "react-router-dom";
import AppLayout from "./layout/AppLayout";

const GraphExplorerPage = lazy(() => import("./screens/graph"));
const RelationshipInventoryPage = lazy(() => import("./screens/inventory"));
const CatalogPage = lazy(() => import("./screens/catalog"));
const PolicyCreatePage = lazy(() => import("./screens/catalog/PolicyCreatePage"));

export default function App() {
  return (
    <Suspense fallback={<div style={{ padding: 24 }}>Loading…</div>}>
      <Routes>
        <Route element={<AppLayout />}>
          <Route path="/" element={<Navigate to="/graph" replace />} />
          <Route path="/graph" element={<GraphExplorerPage />} />
          <Route path="/relationships" element={<RelationshipInventoryPage />} />
          <Route path="/catalog" element={<CatalogPage />} />
          <Route path="/catalog/policies/new" element={<PolicyCreatePage />} />
        </Route>
      </Routes>
    </Suspense>
  );
}
