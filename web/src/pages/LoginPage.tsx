import { FormEvent, useEffect, useState } from "react";
import { forgotPassword, login, register, resetPassword, siteConfig, verifyEmail } from "../lib/api";

type AuthTab = "login" | "reg";

export function LoginPage() {
  const [tab, setTab] = useState<AuthTab>("login");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [regEmail, setRegEmail] = useState("");
  const [regPassword, setRegPassword] = useState("");
  const [registrationOpen, setRegistrationOpen] = useState(true);
  const [showRegisterCta, setShowRegisterCta] = useState(true);
  const [emailVerificationRequired, setEmailVerificationRequired] = useState(false);
  const [showForgot, setShowForgot] = useState(false);
  const [forgotEmail, setForgotEmail] = useState("");
  const [resetTokenValue, setResetTokenValue] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [verificationToken, setVerificationToken] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const verifyToken = params.get("verify")?.trim();
    const resetToken = params.get("reset")?.trim();

    if (resetToken) {
      setTab("login");
      setShowForgot(true);
      setResetTokenValue(resetToken);
      setNewPassword("");
      setNotice("请输入新密码完成重置。");
      replaceAuthQueryParams(["reset"]);
    }

    if (verifyToken) {
      setLoading(true);
      setError("");
      verifyEmail(verifyToken)
        .then(() => {
          setTab("login");
          setNotice("邮箱验证已完成，请登录。");
          replaceAuthQueryParams(["verify"]);
        })
        .catch((err) => {
          setError(err instanceof Error ? err.message : "邮箱验证失败");
          replaceAuthQueryParams(["verify"]);
        })
        .finally(() => setLoading(false));
    }

    siteConfig()
      .then((cfg) => {
        setRegistrationOpen(cfg.registrationOpen);
        setShowRegisterCta(cfg.showRegisterCta);
        setEmailVerificationRequired(Boolean(cfg.emailVerificationRequired));
        if (!cfg.registrationOpen || !cfg.showRegisterCta) {
          setTab("login");
        }
      })
      .catch(() => {
        setRegistrationOpen(true);
        setShowRegisterCta(true);
      });
  }, []);

  const canShowRegister = registrationOpen && showRegisterCta;

  async function submit(event: FormEvent) {
    event.preventDefault();
    setLoading(true);
    setError("");
    setNotice("");
    try {
      if (tab === "login") {
        await login(email, password);
        redirectAfterAuth();
        return;
      }
      if (!registrationOpen) {
        throw new Error("当前实例已关闭公共注册");
      }
      const payload = await register(regEmail, regPassword);
      if (!payload.verificationRequired) {
        setNotice("账号已创建，正在进入控制台。");
        redirectAfterAuth();
        return;
      }
      setVerificationToken(payload.devVerificationToken || "");
      setEmail(payload.user.email);
      setPassword(regPassword);
      setNotice(
        payload.devVerificationToken
          ? "账号已创建。当前本地开发环境会返回邮箱验证令牌，可直接点击下方按钮完成验证。"
          : "账号已创建，请查看邮箱完成验证后再登录。"
      );
    } catch (err) {
      setError(err instanceof Error ? err.message : "请求失败");
    } finally {
      setLoading(false);
    }
  }

  async function verifyCreatedEmail() {
    if (!verificationToken) return;
    setLoading(true);
    setError("");
    try {
      await verifyEmail(verificationToken);
      await login(regEmail || email, regPassword || password);
      redirectAfterAuth();
    } catch (err) {
      setError(err instanceof Error ? err.message : "邮箱验证失败");
    } finally {
      setLoading(false);
    }
  }

  async function requestReset(event: FormEvent) {
    event.preventDefault();
    setLoading(true);
    setError("");
    setNotice("");
    try {
      const payload = await forgotPassword(forgotEmail || email);
      setResetTokenValue(payload.devResetToken || "");
      setNotice(
        payload.devResetToken
          ? "如果邮箱存在，重置邮件已经生成。本地开发环境会在这里展示重置令牌。"
          : "如果邮箱存在，重置邮件已经发送，请按邮件提示继续。"
      );
    } catch (err) {
      setError(err instanceof Error ? err.message : "重置请求失败");
    } finally {
      setLoading(false);
    }
  }

  async function finishReset() {
    setLoading(true);
    setError("");
    try {
      await resetPassword(resetTokenValue, newPassword);
      setPassword("");
      setResetTokenValue("");
      setNewPassword("");
      setShowForgot(false);
      setTab("login");
      setNotice("密码已更新，请重新登录。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "密码重置失败");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="auth-wrap">
      <div className="auth-brand">
        <a className="ab-logo" href="/" aria-label="返回 TokHub 前台首页">
          <span className="logo-mark">T</span>
          <span>
            <b>TokHub</b>
            <small>API 中转站监控与企业网关</small>
          </span>
        </a>
        <h2>
          把 14 个中转站
          <br />
          收敛成一个可信入口
        </h2>
        <p>分层探测实时判断入口是否健康与模型是否真的可用，一键生成企业专属网关，故障自动转移。</p>
        <div className="ab-feats">
          <div className="f">
            <span>L1</span>基础监控 L1/L2，高频不烧 Token
          </div>
          <div className="f">
            <span>L3</span>真实监控 L3，校验内容真实可用
          </div>
          <div className="f">
            <span>GW</span>专属中转站，统一入口智能路由
          </div>
        </div>
      </div>

      <div className="auth-form">
        <form className="auth-card" onSubmit={submit}>
          <div className="auth-tabs">
            <button type="button" className={tab === "login" ? "active" : ""} onClick={() => { setTab("login"); setShowForgot(false); }}>
              登录
            </button>
            {canShowRegister ? (
              <button type="button" className={tab === "reg" ? "active" : ""} onClick={() => { setTab("reg"); setShowForgot(false); }}>
                注册新账号
              </button>
            ) : null}
          </div>

          {error ? <div className="form-error">{error}</div> : null}
          {notice ? <div className="form-notice">{notice}</div> : null}

          {showForgot ? (
            <ForgotPanel
              email={forgotEmail || email}
              setEmail={setForgotEmail}
              resetToken={resetTokenValue}
              setResetToken={setResetTokenValue}
              newPassword={newPassword}
              setNewPassword={setNewPassword}
              loading={loading}
              onRequest={requestReset}
              onReset={() => void finishReset()}
              onBack={() => setShowForgot(false)}
            />
          ) : tab === "login" ? (
            <>
              <h1>欢迎回到 TokHub</h1>
              <div className="lead">登录后可订阅平台通道、管理私有通道</div>
              <div className="fld">
                <label htmlFor="login-email">邮箱</label>
                <input id="login-email" type="email" value={email} onChange={(event) => setEmail(event.target.value)} placeholder="you@company.com" />
              </div>
              <div className="fld">
                <label className="row" htmlFor="login-password">
                  <span>密码</span>
                  <a href="/login" onClick={(event) => { event.preventDefault(); setShowForgot(true); setForgotEmail(email); }}>
                    忘记密码？
                  </a>
                </label>
                <input id="login-password" type="password" value={password} onChange={(event) => setPassword(event.target.value)} placeholder="请输入密码" />
              </div>
              <button type="submit" className="btn btn-primary btn-block" disabled={loading}>
                {loading ? "登录中..." : "登录 →"}
              </button>
            </>
          ) : (
            <>
              <h1>免费注册 TokHub</h1>
              <div className="lead">注册即可订阅中转站监控、添加你自己的私有通道</div>
              {emailVerificationRequired ? <div className="form-note">当前实例已开启邮箱验证，注册后需要先完成验证再登录。</div> : null}
              <div className="fld">
                <label htmlFor="reg-email">邮箱</label>
                <input id="reg-email" type="email" value={regEmail} onChange={(event) => setRegEmail(event.target.value)} placeholder="you@company.com" />
              </div>
              <div className="fld">
                <label htmlFor="reg-password">设置密码</label>
                <input id="reg-password" type="password" value={regPassword} onChange={(event) => setRegPassword(event.target.value)} placeholder="至少 8 位" />
              </div>
              <button type="submit" className="btn btn-primary btn-block" disabled={loading}>
                {loading ? "创建中..." : "创建账号并进入控制台 →"}
              </button>
              {verificationToken ? (
                <div className="reg-notice">
                  <b>邮箱验证令牌</b>
                  <code>{verificationToken}</code>
                  <button type="button" className="btn btn-ghost btn-sm" disabled={loading} onClick={() => void verifyCreatedEmail()}>
                    完成邮箱验证
                  </button>
                </div>
              ) : null}
              <div className="auth-foot" style={{ marginTop: 16, fontSize: 11.5 }}>
                注册即代表同意服务条款与隐私政策
              </div>
            </>
          )}

          {!registrationOpen && !showForgot ? (
            <div className="reg-notice">
              当前 TokHub 实例的管理员已关闭公共注册。如需账号，请联系管理员。
            </div>
          ) : null}

          <div className="or">或直接进入</div>
          <div className="entry">
            <a href="/">
              <span className="ei">⌂</span>
              <b>前台首页</b>
              <small>产品亮点与入口</small>
            </a>
            <a href="/dashboard">
              <span className="ei">▦</span>
              <b>监控总览</b>
              <small>实时通道监控</small>
            </a>
            <a href="/console">
              <span className="ei">⌂</span>
              <b>用户控制台</b>
              <small>私有通道与专属网关</small>
            </a>
          </div>
          {canShowRegister && !showForgot ? (
            <div className="auth-foot">
              {tab === "login" ? (
                <>
                  还没有账号？
                  <a href="/login" onClick={(event) => { event.preventDefault(); setTab("reg"); }}>
                    免费注册
                  </a>
                </>
              ) : (
                <>
                  已有账号？
                  <a href="/login" onClick={(event) => { event.preventDefault(); setTab("login"); }}>
                    直接登录
                  </a>
                </>
              )}
            </div>
          ) : null}
        </form>
      </div>
    </div>
  );
}

function redirectAfterAuth() {
  window.location.href = safeNextPath(new URLSearchParams(window.location.search).get("next"));
}

function replaceAuthQueryParams(keys: string[]) {
  const url = new URL(window.location.href);
  for (const key of keys) {
    url.searchParams.delete(key);
  }
  const next = `${url.pathname}${url.search}${url.hash}`;
  window.history.replaceState({}, "", next);
}

function safeNextPath(rawNext: string | null) {
  const fallback = "/console";
  if (!rawNext || !rawNext.startsWith("/") || rawNext.startsWith("//") || rawNext.startsWith("\\")) {
    return fallback;
  }
  try {
    const next = new URL(rawNext, window.location.origin);
    if (next.origin !== window.location.origin || next.pathname === "/login" || next.pathname === "/admin/login" || isNonPageNextPath(next.pathname)) {
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

function ForgotPanel({
  email,
  setEmail,
  resetToken,
  setResetToken,
  newPassword,
  setNewPassword,
  loading,
  onRequest,
  onReset,
  onBack
}: {
  email: string;
  setEmail: (value: string) => void;
  resetToken: string;
  setResetToken: (value: string) => void;
  newPassword: string;
  setNewPassword: (value: string) => void;
  loading: boolean;
  onRequest: (event: FormEvent) => void;
  onReset: () => void;
  onBack: () => void;
}) {
  return (
    <>
      <h1>重置登录密码</h1>
      <div className="lead">输入邮箱后生成重置邮件；允许开发令牌的环境会直接展示令牌。</div>
      <div className="fld">
        <label htmlFor="forgot-email">邮箱</label>
        <input id="forgot-email" type="email" value={email} onChange={(event) => setEmail(event.target.value)} placeholder="you@company.com" />
      </div>
      <button type="button" className="btn btn-primary btn-block" disabled={loading} onClick={onRequest}>
        {loading ? "发送中..." : "发送重置邮件"}
      </button>
      {resetToken ? (
        <>
          <div className="fld reset-token-field">
            <label htmlFor="reset-token">重置令牌</label>
            <input id="reset-token" className="mono" value={resetToken} onChange={(event) => setResetToken(event.target.value)} />
          </div>
          <div className="fld">
            <label htmlFor="new-password">新密码</label>
            <input id="new-password" type="password" value={newPassword} onChange={(event) => setNewPassword(event.target.value)} />
          </div>
          <button type="button" className="btn btn-primary btn-block" disabled={loading || !newPassword.trim()} onClick={onReset}>
            更新密码
          </button>
        </>
      ) : null}
      <button type="button" className="btn btn-ghost btn-block" onClick={onBack}>
        返回登录
      </button>
    </>
  );
}
