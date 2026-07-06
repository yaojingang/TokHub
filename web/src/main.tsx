import { useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter, Navigate, Route, Routes, useLocation } from "react-router-dom";
import { I18nextProvider } from "react-i18next";
import { i18n } from "./i18n";
import "./styles/tokhub.css";
import "./styles/platform.css";
import "./styles/admin.css";
import "./styles/app.css";
import { NotFoundPage } from "./pages/NotFoundPage";
import { modules, moduleElement } from "./modules/registry";
import { DEFAULT_ADMIN_PATH, adminPath, getAdminPath, legacyAdminPathToCurrent, routeWithAdminPath, setAdminPath } from "./lib/adminPath";
import { siteConfig } from "./lib/api";

const isProductionBuild = Boolean((import.meta as ImportMeta & { env?: { PROD?: boolean } }).env?.PROD);

if ("serviceWorker" in navigator && isProductionBuild) {
  window.addEventListener("load", () => {
    navigator.serviceWorker.register("/sw.js").catch(() => undefined);
  });
}

createRoot(document.getElementById("root")!).render(
  <I18nextProvider i18n={i18n}>
    <BrowserRouter>
      <AppRoutes />
    </BrowserRouter>
  </I18nextProvider>
);

function AppRoutes() {
  const location = useLocation();
  const [adminBase, setAdminBase] = useState(getAdminPath());
  const [adminConfigLoaded, setAdminConfigLoaded] = useState(false);

  useEffect(() => {
    let active = true;
    siteConfig()
      .then((site) => {
        if (!active) return;
        setAdminBase(setAdminPath(site.adminPath));
      })
      .catch(() => {
        if (!active) return;
        setAdminBase(setAdminPath(DEFAULT_ADMIN_PATH));
      })
      .finally(() => {
        if (active) setAdminConfigLoaded(true);
      });
    return () => {
      active = false;
    };
  }, []);

  if (!adminConfigLoaded && shouldWaitForAdminPath(location.pathname)) {
    return <AppBootShell />;
  }

  return (
    <>
      <AnalyticsPageviewTracker />
      <Routes>
        {modules.map((module) => {
          const routeKey = moduleDependsOnAdminPath(module.path) ? `${module.id}:${adminBase}` : module.id;
          return <Route path={routeWithAdminPath(module.path)} element={moduleElement(module)} key={routeKey} />;
        })}
        {adminBase !== DEFAULT_ADMIN_PATH ? (
          <Route path="/admin/*" element={<LegacyAdminRedirect />} />
        ) : (
          <Route path="/admin/*" element={<NotFoundPage />} />
        )}
        <Route path={`${adminPath()}/*`} element={<NotFoundPage />} />
        <Route path="/console/*" element={<NotFoundPage />} />
        <Route path="*" element={<NotFoundPage />} />
      </Routes>
    </>
  );
}

type AnalyticsWindow = Window & {
  gtag?: (...args: unknown[]) => void;
  _hmt?: unknown[];
};

function AnalyticsPageviewTracker() {
  const location = useLocation();
  const initialPathRef = useRef(`${location.pathname}${location.search}${location.hash}`);

  useEffect(() => {
    const path = `${location.pathname}${location.search}${location.hash}`;
    if (initialPathRef.current === path) {
      initialPathRef.current = "";
      return;
    }
    if (!isAnalyticsPublicPath(location.pathname)) {
      return;
    }
    const analyticsWindow = window as AnalyticsWindow;
    if (typeof analyticsWindow.gtag === "function") {
      analyticsWindow.gtag("event", "page_view", {
        page_location: window.location.href,
        page_path: `${location.pathname}${location.search}`,
        page_title: document.title
      });
    }
    if (Array.isArray(analyticsWindow._hmt)) {
      analyticsWindow._hmt.push(["_trackPageview", `${location.pathname}${location.search}`]);
    }
  }, [location.hash, location.pathname, location.search]);

  return null;
}

function isAnalyticsPublicPath(pathname: string) {
  const configuredAdminPath = adminPath();
  return (
    pathname === "/" ||
    pathname === "/dashboard" ||
    pathname === "/pricing" ||
    pathname === "/recommend" ||
    pathname.startsWith("/channels/") ||
    (!isPrivateAppPath(pathname, configuredAdminPath) && !pathname.startsWith("/api/"))
  );
}

function isPrivateAppPath(pathname: string, configuredAdminPath: string) {
  return (
    pathname === "/login" ||
    pathname === "/admin" ||
    pathname.startsWith("/admin/") ||
    pathname === configuredAdminPath ||
    pathname.startsWith(`${configuredAdminPath}/`) ||
    pathname === "/console" ||
    pathname.startsWith("/console/")
  );
}

function shouldWaitForAdminPath(pathname: string) {
  return !isStableBeforeAdminConfig(pathname);
}

function isStableBeforeAdminConfig(pathname: string) {
  return (
    pathname === "/" ||
    pathname === "/dashboard" ||
    pathname === "/login" ||
    pathname === "/pricing" ||
    pathname === "/recommend" ||
    pathname.startsWith("/channels/") ||
    pathname === "/console" ||
    pathname.startsWith("/console/")
  );
}

function moduleDependsOnAdminPath(path: string) {
  return path === DEFAULT_ADMIN_PATH || path.startsWith(`${DEFAULT_ADMIN_PATH}/`);
}

function AppBootShell() {
  return (
    <main className="app-boot" aria-busy="true" aria-live="polite">
      <span className="app-boot-mark">T</span>
      <span className="app-boot-copy">
        <b>TokHub</b>
        <small>正在准备站点入口</small>
      </span>
    </main>
  );
}

function LegacyAdminRedirect() {
  const location = useLocation();
  const pathname = legacyAdminPathToCurrent(location.pathname);
  return <Navigate to={`${pathname}${location.search}${location.hash}`} replace />;
}
