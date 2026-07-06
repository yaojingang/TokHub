import type { FormEvent } from "react";
import { useState } from "react";
import { login, register, verifyEmail, type User } from "../lib/api";
import { Dialog } from "../ui";

type PublicAuthDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  registrationOpen: boolean;
  showRegisterCta?: boolean;
  emailVerificationRequired: boolean;
  onAuthenticated: (user: User) => void;
};

export function PublicAuthDialog({
  open,
  onOpenChange,
  registrationOpen,
  showRegisterCta = true,
  emailVerificationRequired,
  onAuthenticated
}: PublicAuthDialogProps) {
  const [tab, setTab] = useState<"login" | "reg">("login");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [regEmail, setRegEmail] = useState("");
  const [regPassword, setRegPassword] = useState("");
  const [verificationToken, setVerificationToken] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const canShowRegister = registrationOpen && showRegisterCta;

  async function submit(event: FormEvent) {
    event.preventDefault();
    setLoading(true);
    setError("");
    setNotice("");
    try {
      if (tab === "login") {
        const nextUser = await login(email, password);
        onAuthenticated(nextUser);
        return;
      }
      if (!registrationOpen) {
        throw new Error("当前实例已关闭公共注册，请使用已有账号登录。");
      }
      const payload = await register(regEmail, regPassword);
      if (!payload.verificationRequired) {
        onAuthenticated(payload.user);
        return;
      }
      setVerificationToken(payload.devVerificationToken || "");
      setEmail(payload.user.email);
      setPassword(regPassword);
      setNotice(payload.devVerificationToken ? "账号已创建，可直接完成本地验证后继续。" : "账号已创建，请先完成邮箱验证后再登录。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "认证失败");
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
      const nextUser = await login(regEmail || email, regPassword || password);
      onAuthenticated(nextUser);
    } catch (err) {
      setError(err instanceof Error ? err.message : "邮箱验证失败");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="登录 TokHub"
      description="登录后可在当前页面查看我的关注、管理私有通道，并继续刚才的操作。"
    >
      <form className="public-auth-card" onSubmit={submit}>
        <div className="auth-tabs">
          <button type="button" className={tab === "login" ? "active" : ""} onClick={() => { setTab("login"); setError(""); setNotice(""); }}>
            登录
          </button>
          {canShowRegister ? (
            <button type="button" className={tab === "reg" ? "active" : ""} onClick={() => { setTab("reg"); setError(""); setNotice(""); }}>
              注册新账号
            </button>
          ) : null}
        </div>
        {error ? <div className="form-error">{error}</div> : null}
        {notice ? <div className="form-notice">{notice}</div> : null}
        {tab === "login" ? (
          <>
            <div className="fld">
              <label htmlFor="public-login-email">邮箱</label>
              <input id="public-login-email" type="email" value={email} onChange={(event) => setEmail(event.target.value)} placeholder="you@company.com" />
            </div>
            <div className="fld">
              <label htmlFor="public-login-password">密码</label>
              <input id="public-login-password" type="password" value={password} onChange={(event) => setPassword(event.target.value)} placeholder="请输入密码" />
            </div>
            <button type="submit" className="btn btn-primary btn-block" disabled={loading}>
              {loading ? "登录中..." : "登录并继续 →"}
            </button>
          </>
        ) : (
          <>
            {emailVerificationRequired ? <div className="form-note">当前实例已开启邮箱验证，注册后需要先完成验证。</div> : null}
            <div className="fld">
              <label htmlFor="public-reg-email">邮箱</label>
              <input id="public-reg-email" type="email" value={regEmail} onChange={(event) => setRegEmail(event.target.value)} placeholder="you@company.com" />
            </div>
            <div className="fld">
              <label htmlFor="public-reg-password">设置密码</label>
              <input id="public-reg-password" type="password" value={regPassword} onChange={(event) => setRegPassword(event.target.value)} placeholder="至少 8 位" />
            </div>
            <button type="submit" className="btn btn-primary btn-block" disabled={loading}>
              {loading ? "创建中..." : "创建账号并继续 →"}
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
          </>
        )}
      </form>
    </Dialog>
  );
}
