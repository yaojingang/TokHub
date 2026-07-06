import { ReactNode, useEffect, useState } from "react";
import { Link, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { AdminSettingsSummary, adminSettings, cachedCurrentUser, currentUser, logout, User } from "../lib/api";
import { groupedNavModules, type ModuleConfig } from "../modules/registry";
import { Footer } from "./Footer";

let adminSummaryCache: AdminSettingsSummary | null = null;
let lastAdminAuthValidationAt = 0;

function isPlatformRole(user: User | null) {
  return user?.role === "owner" || user?.role === "admin";
}

export function AdminShell({ children, title = "控制台首页", crumb = "/ 概览" }: { children: ReactNode; title?: string; crumb?: string }) {
  const location = useLocation();
  const { t } = useTranslation(["common", "admin"]);
  const cachedUser = cachedCurrentUser();
  const [user, setUser] = useState<User | null>(cachedUser ?? null);
  const [summary, setSummary] = useState<AdminSettingsSummary | null>(adminSummaryCache);
  const [loaded, setLoaded] = useState(cachedUser !== undefined);

  useEffect(() => {
    let active = true;
    const nextPath = `${location.pathname}${location.search}${location.hash}`;
    const hasCachedUser = cachedCurrentUser() !== undefined;
    const forceRefresh = !hasCachedUser || Date.now() - lastAdminAuthValidationAt > 30_000;
    if (forceRefresh) lastAdminAuthValidationAt = Date.now();

    async function verifyAccess() {
      try {
        const value = await currentUser({ force: forceRefresh });
        if (!active) return;
        setUser(value);
        if (!value) {
          window.location.href = `/admin/login?next=${encodeURIComponent(nextPath)}`;
          return;
        }
        if (!isPlatformRole(value)) {
          window.location.href = "/console";
          return;
        }

        adminSettings()
          .then((payload) => {
            adminSummaryCache = payload.summary;
            if (active) setSummary(payload.summary);
          })
          .catch(() => {
            if (active && !adminSummaryCache) setSummary(null);
          });
      } catch {
        if (!active) return;
        setUser(null);
        window.location.href = `/admin/login?next=${encodeURIComponent(nextPath)}`;
      } finally {
        if (active) setLoaded(true);
      }
    }

    verifyAccess();
    return () => {
      active = false;
    };
  }, [location.hash, location.pathname, location.search]);

  async function handleLogout() {
    adminSummaryCache = null;
    await logout();
    window.location.href = "/admin/login";
  }

  if (!loaded || !user || !isPlatformRole(user)) {
    return (
      <div className="admin">
        <div className="admin-main">
          <header className="admin-top">
            <h1>{title}</h1>
            <span className="crumb">{crumb}</span>
          </header>
          <div className="admin-body">
            <div className="card card-pad">正在验证平台管理权限...</div>
          </div>
        </div>
      </div>
    );
  }

  const groups = groupedNavModules("admin");
  const platformOrgName = summary?.platformOrg?.name || "平台组织";
  const platformOrgMark = firstMark(platformOrgName);
  const platformOrgHref = `/admin/orgs${summary?.platformOrg?.slug ? `?q=${encodeURIComponent(summary.platformOrg.slug)}` : ""}`;

  return (
    <div className="admin">
      <aside className="sidebar">
        <div className="sb-logo">
          <span className="logo-mark">T</span>
          <span className="logo-text">
            <b>{t("common:brand.name")}</b>
            <small>{t("common:brand.adminSubtitle")}</small>
          </span>
        </div>
        <nav className="sb-nav">
          {groups.map((group) => (
            <div className="sb-group" key={group.groupKey}>
              <div className="sb-title">{t(group.groupKey)}</div>
              {group.modules.map((module) => {
                const active = location.pathname === module.path || (module.path !== "/admin" && location.pathname.startsWith(`${module.path}/`));
                return (
                  <Link className={`sb-link ${active ? "active" : ""}`} to={module.path} key={module.id}>
                    <span className="i">{module.icon}</span>
                    {t(module.navKey || module.titleKey)}
                    {module.summaryKey ? <span className="mini">{countMini(readSummary(summary, module))}</span> : null}
                  </Link>
                );
              })}
            </div>
          ))}
        </nav>
        <div className="sb-foot">
          <span className="avatar">{user?.avatar || "TA"}</span>
          <span className="meta">
            <b>{user?.name || (loaded ? "未登录" : "加载中")}</b>
            <small>{user?.email || "需要登录访问后台"}</small>
          </span>
        </div>
      </aside>
      <div className="admin-main">
        <header className="admin-top">
          <h1>{title}</h1>
          <span className="crumb">{crumb}</span>
          <span className="spacer" />
          <Link className="org-pick" to={platformOrgHref} aria-label="编辑平台组织">
            <span className="dot">{platformOrgMark}</span>{platformOrgName}<span className="ar">▾</span>
          </Link>
          <a className="btn btn-ghost btn-sm" href="/console">
            {t("common:actions.userConsole")}
          </a>
          <a className="btn btn-ghost btn-sm" href="/">
            {t("common:actions.publicHome")}
          </a>
          <a className="btn btn-ghost btn-sm" href="/dashboard">
            {t("common:actions.publicBoard")}
          </a>
          <button className="btn btn-ghost btn-sm" onClick={handleLogout}>
            {t("common:actions.logout")}
          </button>
        </header>
        <div className="admin-body">
          {children}
          <Footer admin />
        </div>
      </div>
    </div>
  );
}

function readSummary(summary: AdminSettingsSummary | null, module: ModuleConfig) {
  if (!summary || !module.summaryKey) return undefined;
  const value = summary[module.summaryKey as keyof AdminSettingsSummary];
  return typeof value === "number" ? value : undefined;
}

function countMini(value: number | undefined) {
  if (value === undefined || value === null) return undefined;
  if (value > 99) return "99+";
  return String(value);
}

function firstMark(value: string) {
  return (Array.from(value.trim())[0] || "T").toUpperCase();
}
