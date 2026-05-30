import { Route, Routes } from "react-router-dom";
import { AccountBar } from "@/components/AccountBar";
import { CatalogPage } from "@/pages/CatalogPage";
import { InsightPage } from "@/pages/InsightPage";

export default function App() {
  return (
    <div className="min-h-full">
      <AccountBar />
      <main>
        <Routes>
          <Route path="/" element={<CatalogPage />} />
          {/* type puede contener un punto (structural.orphans), no necesita encoding especial */}
          <Route path="/insights/:type" element={<InsightPage />} />
        </Routes>
      </main>
    </div>
  );
}
