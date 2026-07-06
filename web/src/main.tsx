import { createRoot } from "react-dom/client";
import { BrowserRouter, Route, Routes } from "react-router-dom";
import { I18nextProvider } from "react-i18next";
import { i18n } from "./i18n";
import "./styles/tokhub.css";
import "./styles/platform.css";
import "./styles/admin.css";
import "./styles/app.css";
import { NotFoundPage } from "./pages/NotFoundPage";
import { modules, moduleElement } from "./modules/registry";

const isProductionBuild = Boolean((import.meta as ImportMeta & { env?: { PROD?: boolean } }).env?.PROD);

if ("serviceWorker" in navigator && isProductionBuild) {
  window.addEventListener("load", () => {
    navigator.serviceWorker.register("/sw.js").catch(() => undefined);
  });
}

createRoot(document.getElementById("root")!).render(
  <I18nextProvider i18n={i18n}>
    <BrowserRouter>
      <Routes>
        {modules.map((module) => (
          <Route path={module.path} element={moduleElement(module)} key={module.id} />
        ))}
        <Route path="/admin/*" element={<NotFoundPage />} />
        <Route path="/console/*" element={<NotFoundPage />} />
        <Route path="*" element={<NotFoundPage />} />
      </Routes>
    </BrowserRouter>
  </I18nextProvider>
);
