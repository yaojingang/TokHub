import { ReactNode, useEffect, useState } from "react";
import { Link, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { activeWorkspaceId, cachedCurrentUser, consoleSettings, currentUser, logout, setActiveWorkspaceId, User, WorkspaceOption, WorkspaceSettings } from "../lib/api";
import { groupedNavModules, type ModuleConfig } from "../modules/registry";
import { Footer } from "./Footer";

let workspaceCache: WorkspaceSettings | null = null;
let workspacesCache: WorkspaceOption[] = [];
let lastConsoleAuthValidationAt = 0;

export function ConsoleShell({ children, title = "控制台首页", crumb = "/ 工作区" }: { children: ReactNode; title?: string; crumb?: string }) {
  const location = useLocation();
  const { t } = useTranslation(["common", "console"]);
  const cachedUser = cachedCurrentUser();
  const [user, setUser] = useState<User | null>(cachedUser ?? null);
  const [workspace, setWorkspace] = useState<WorkspaceSettings | null>(workspaceCache);
  const [workspaces, setWorkspaces] = useState<WorkspaceOption[]>(workspacesCache);
  const [loaded, setLoaded] = useState(cachedUser !== undefined);

  useEffect(() => {
    function handleCurrentUserChanged(event: Event) {
      const nextUser = (event as CustomEvent<User | null>).detail;
      setUser(nextUser);
      setLoaded(true);
    }
    window.addEventListener("tokhub:current-user-changed", handleCurrentUserChanged);
    return () => window.removeEventListener("tokhub:current-user-changed", handleCurrentUserChanged);
  }, []);

  useEffect(() => {
    let active = true;
    const nextPath = `${location.pathname}${location.search}${location.hash}`;
    const hasCachedUser = cachedCurrentUser() !== undefined;
    const forceRefresh = !hasCachedUser || Date.now() - lastConsoleAuthValidationAt > 30_000;
    if (forceRefresh) lastConsoleAuthValidationAt = Date.now();

    async function verifyAccess() {
      try {
        const value = await currentUser({ force: forceRefresh });
        if (!active) return;
        setUser(value);
        if (!value) {
          window.location.href = `/login?next=${encodeURIComponent(nextPath)}`;
          return;
        }

        loadWorkspaceSettings()
          .then((payload) => {
            workspaceCache = payload.workspace;
            workspacesCache = payload.workspaces;
            if (!activeWorkspaceId()) setActiveWorkspaceId(payload.workspace.orgId);
            if (active) {
              setWorkspace(payload.workspace);
              setWorkspaces(payload.workspaces);
            }
          })
          .catch(() => {
            if (active && !workspaceCache) setWorkspace(null);
          });
      } catch {
        if (!active) return;
        setUser(null);
        window.location.href = `/login?next=${encodeURIComponent(nextPath)}`;
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
    workspaceCache = null;
    workspacesCache = [];
    await logout();
    window.location.href = "/login";
  }

  function handleWorkspaceChange(orgId: string) {
    if (!orgId || orgId === workspace?.orgId) return;
    workspaceCache = null;
    setActiveWorkspaceId(orgId);
    window.location.reload();
  }

  if (!loaded || !user) {
    return (
      <div className="admin console">
        <div className="admin-main">
          <header className="admin-top">
            <h1>{title}</h1>
            <span className="crumb">{crumb}</span>
          </header>
          <div className="admin-body">
            <div className="card card-pad">正在验证控制台访问权限...</div>
          </div>
        </div>
      </div>
    );
  }

  const groups = groupedNavModules("console");
  const displayName = user.name || user.username || user.email || "用户";
  const userMark = user.avatar || firstMark(displayName, "T");

  return (
    <div className="admin console">
      <aside className="sidebar">
        <div className="sb-logo">
          <span className="logo-mark">T</span>
          <span className="logo-text">
            <b>{t("common:brand.name")}</b>
            <small>{t("common:brand.consoleSubtitle")}</small>
          </span>
        </div>
        <nav className="sb-nav">
          {groups.map((group) => (
            <div className="sb-group" key={group.groupKey}>
              <div className="sb-title">{t(group.groupKey)}</div>
              {group.modules.map((module) => (
                <Link className={`sb-link ${isConsoleNavActive(location.pathname, module.path) ? "active" : ""}`} to={module.path} key={module.id}>
                  <span className="i">{module.icon}</span>
                  {t(module.navKey || module.titleKey)}
                  {module.summaryKey ? <span className="mini">{countMini(readWorkspaceSummary(workspace, module))}</span> : null}
                </Link>
              ))}
            </div>
          ))}
        </nav>
        <Link className="sb-foot sb-foot-link" to="/console/settings#profile" aria-label="编辑个人设置">
          <span className="avatar">{user?.avatar || "TA"}</span>
          <span className="meta">
            <b>{displayName || (loaded ? "未登录" : "加载中")}</b>
            <small>{user?.email || "需要登录访问控制台"}</small>
          </span>
        </Link>
      </aside>
      <div className="admin-main">
        <header className="admin-top">
          <h1>{title}</h1>
          <span className="crumb">{crumb}</span>
          <span className="spacer" />
          <div className="org-pick workspace-switcher" aria-label="当前工作区">
            <span className="dot">{workspace?.name ? firstMark(workspace.name, "T") : userMark}</span>
            <select value={workspace?.orgId ?? ""} onChange={(event) => handleWorkspaceChange(event.target.value)} aria-label="切换工作区">
              {(workspaces.length ? workspaces : workspace ? [{ orgId: workspace.orgId, name: workspace.name, plan: workspace.plan, status: workspace.status, role: workspace.role, members: workspace.members, privateChannels: workspace.privateChannels, gateways: workspace.gateways, activeKeys: workspace.activeKeys }] : []).map((item) => (
                <option value={item.orgId} key={item.orgId}>{item.name} · {roleText(item.role)}</option>
              ))}
            </select>
          </div>
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
          <Footer admin hidePlatformAdmin />
        </div>
      </div>
    </div>
  );
}

async function loadWorkspaceSettings() {
  try {
    return await consoleSettings();
  } catch (err) {
    if (!activeWorkspaceId()) throw err;
    setActiveWorkspaceId("");
    return consoleSettings();
  }
}

function readWorkspaceSummary(workspace: WorkspaceSettings | null, module: ModuleConfig) {
  if (!workspace || !module.summaryKey) return undefined;
  const value = workspace[module.summaryKey as keyof WorkspaceSettings];
  return typeof value === "number" ? value : undefined;
}

function countMini(value: number | undefined) {
  if (value === undefined || value === null) return undefined;
  if (value > 99) return "99+";
  return String(value);
}

function isConsoleNavActive(pathname: string, href: string) {
  if (pathname === href) return true;
  if (href === "/console/keys") return pathname === "/console/members";
  return false;
}

function firstMark(value: string, fallback: string) {
  return (Array.from(value.trim())[0] || fallback || "T").toUpperCase();
}

function roleText(role: string) {
  if (role === "owner") return "Owner";
  if (role === "admin") return "Admin";
  if (role === "operator") return "Operator";
  return "Viewer";
}
