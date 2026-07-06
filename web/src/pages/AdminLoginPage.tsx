import { FormEvent, useEffect, useState } from "react";
import { currentUser, login, logout, User } from "../lib/api";

function isPlatformRole(user: User | null) {
  return user?.role === "owner" || user?.role === "admin";
}

export function AdminLoginPage() {
  const [identifier, setIdentifier] = useState("");
  const [password, setPassword] = useState("");
  const [existingUser, setExistingUser] = useState<User | null>(null);
  const [checking, setChecking] = useState(true);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  useEffect(() => {
    let active = true;
    currentUser({ force: true })
      .then((user) => {
        if (!active) return;
        if (isPlatformRole(user)) {
          window.location.href = safeAdminNextPath(new URLSearchParams(window.location.search).get("next"));
          return;
        }
        setExistingUser(user);
      })
      .catch(() => {
        if (active) setExistingUser(null);
      })
      .finally(() => {
        if (active) setChecking(false);
      });
    return () => {
      active = false;
    };
  }, []);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setLoading(true);
    setError("");
    setNotice("");
    try {
      const user = await login(identifier.trim(), password);
      if (!isPlatformRole(user)) {
        setExistingUser(user);
        setNotice("当前账号不是平台管理员。可以进入用户控制台，或退出后切换管理员账号。");
        return;
      }
      window.location.href = safeAdminNextPath(new URLSearchParams(window.location.search).get("next"));
    } catch (err) {
      setError(err instanceof Error ? err.message : "管理员登录失败");
    } finally {
      setLoading(false);
    }
  }

  async function switchAccount() {
    setLoading(true);
    setError("");
    setNotice("");
    try {
      await logout();
      setExistingUser(null);
      setIdentifier("");
      setPassword("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "退出当前账号失败");
    } finally {
      setLoading(false);
    }
  }

  return (
    <main className="admin-login-wrap">
      <section className="admin-login-left" aria-label="TokHub 平台管理登录">
        <a className="admin-login-logo" href="/" aria-label="返回 TokHub 前台首页">
          <span className="logo-mark">T</span>
          <span>
            <b>TokHub</b>
            <small>平台管理后台</small>
          </span>
        </a>

        <div className="admin-login-card">
          <div className="admin-login-kicker">Platform Admin</div>
          <h1>进入 TokHub 平台管理</h1>
          <p className="admin-login-lead">仅 owner / admin 账号可访问平台通道、用户、组织、开放 API、推荐运营和审计治理。</p>

          {error ? <div className="form-error">{error}</div> : null}
          {notice ? <div className="form-notice">{notice}</div> : null}

          {checking ? (
            <div className="admin-login-current">正在检查当前会话...</div>
          ) : existingUser && !isPlatformRole(existingUser) ? (
            <div className="admin-login-current">
              <div>
                <span className="avatar">{existingUser.avatar || "U"}</span>
                <div>
                  <b>{existingUser.name || existingUser.email}</b>
                  <small>{existingUser.email}</small>
                </div>
              </div>
              <p>该账号没有平台管理权限。</p>
              <div className="admin-login-current-actions">
                <a className="btn btn-ghost btn-sm" href="/console">进入用户控制台</a>
                <button className="btn btn-primary btn-sm" type="button" disabled={loading} onClick={() => void switchAccount()}>
                  退出并切换管理员
                </button>
              </div>
            </div>
          ) : (
            <form className="admin-login-form" onSubmit={submit}>
              <div className="fld">
                <label htmlFor="admin-login-identifier">管理员用户名</label>
                <input
                  id="admin-login-identifier"
                  type="text"
                  autoComplete="username"
                  value={identifier}
                  onChange={(event) => setIdentifier(event.target.value)}
                  placeholder="admin"
                  required
                />
              </div>
              <div className="fld">
                <label htmlFor="admin-login-password">管理员密码</label>
                <input
                  id="admin-login-password"
                  type="password"
                  autoComplete="current-password"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  placeholder="输入平台管理员密码"
                  required
                />
              </div>
              <button type="submit" className="btn btn-primary btn-block" disabled={loading}>
                {loading ? "验证中..." : "进入平台管理 →"}
              </button>
            </form>
          )}

          <div className="admin-login-links">
            <a href="/login">普通用户登录</a>
            <span />
            <a href="/">前台首页</a>
            <span />
            <a href="/dashboard">返回监控总览</a>
          </div>
        </div>
      </section>

      <section className="admin-login-panel" aria-label="平台管理安全状态">
        <div className="admin-login-panel-head">
          <span className="badge b-blue dot">Admin only</span>
          <span className="muted">RBAC · Audit · CSRF</span>
        </div>
        <div className="admin-login-console">
          <div className="admin-login-console-top">
            <span className="traffic red" />
            <span className="traffic amber" />
            <span className="traffic green" />
            <b>tokhub-admin</b>
          </div>
          <div className="admin-login-rows">
            <div className="admin-login-row">
              <span>权限范围</span>
              <b>owner / admin</b>
            </div>
            <div className="admin-login-row">
              <span>登录审计</span>
              <b>enabled</b>
            </div>
            <div className="admin-login-row">
              <span>会话保护</span>
              <b>httpOnly cookie</b>
            </div>
            <div className="admin-login-row">
              <span>操作边界</span>
              <b>platform scope</b>
            </div>
          </div>
        </div>
        <div className="admin-login-note">
          <b>平台后台入口已与用户控制台分离</b>
          <p>用户注册、私有通道和个人网关继续从普通登录页进入；平台治理只在这里完成管理员身份校验。</p>
        </div>
      </section>
    </main>
  );
}

function safeAdminNextPath(rawNext: string | null) {
  const fallback = "/admin";
  if (!rawNext || !rawNext.startsWith("/") || rawNext.startsWith("//") || rawNext.startsWith("\\")) {
    return fallback;
  }
  try {
    const next = new URL(rawNext, window.location.origin);
    if (
      next.origin !== window.location.origin ||
      next.pathname === "/admin/login" ||
      next.pathname === "/login" ||
      !next.pathname.startsWith("/admin") ||
      isNonPageNextPath(next.pathname)
    ) {
      return fallback;
    }
    return `${next.pathname}${next.search}${next.hash}`;
  } catch {
    return fallback;
  }
}

function isNonPageNextPath(pathname: string) {
  return (
    pathname.startsWith("/api/") ||
    pathname.startsWith("/gateway/") ||
    pathname.startsWith("/v1/") ||
    pathname.startsWith("/ws/") ||
    pathname === "/metrics" ||
    pathname === "/healthz" ||
    pathname === "/readyz"
  );
}
