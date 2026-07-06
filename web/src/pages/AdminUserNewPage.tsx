import { FormEvent, useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { AdminShell } from "../components/AdminShell";
import { AdminUserInput, createAdminUser } from "../lib/api";
import { FormField, PageHeader, SelectField } from "../ui";

type UserCreateDraft = {
  email: string;
  username: string;
  password: string;
  name: string;
  role: string;
  plan: string;
  status: string;
  emailVerified: boolean;
};

const emptyDraft: UserCreateDraft = {
  email: "",
  username: "",
  password: "",
  name: "",
  role: "user",
  plan: "free",
  status: "active",
  emailVerified: true
};

export function AdminUserNewPage() {
  const { t } = useTranslation(["common", "admin"]);
  const navigate = useNavigate();
  const [draft, setDraft] = useState<UserCreateDraft>(emptyDraft);
  const [showPassword, setShowPassword] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const emailName = useMemo(() => {
    const value = draft.email.trim();
    if (!value.includes("@")) return "";
    return value.split("@")[0] || "";
  }, [draft.email]);

  function patch<K extends keyof UserCreateDraft>(key: K, value: UserCreateDraft[K]) {
    setDraft((current) => ({ ...current, [key]: value }));
  }

  function generatePassword() {
    const chars = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#$%";
    const bytes = new Uint32Array(18);
    window.crypto.getRandomValues(bytes);
    const next = Array.from(bytes, (value) => chars[value % chars.length]).join("");
    setDraft((current) => ({ ...current, password: next }));
    setShowPassword(true);
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError("");
    try {
      const input: AdminUserInput = {
        email: draft.email.trim(),
        username: draft.username.trim(),
        password: draft.password,
        name: draft.name.trim(),
        role: draft.role,
        plan: draft.plan,
        status: draft.status,
        emailVerified: draft.emailVerified,
        dataOrigin: "runtime"
      };
      const payload = await createAdminUser(input);
      navigate(`/admin/users?q=${encodeURIComponent(payload.user.email)}`, { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "创建用户失败");
    } finally {
      setSaving(false);
    }
  }

  return (
    <AdminShell title="新增用户" crumb="/ 平台资源 / 用户 / 新增">
      <PageHeader
        description={(
          <div className="admin-user-create-copy">
            <p className="page-intro">创建平台级用户账号。提交后系统会生成默认工作区，并把账号、权限、状态写入审计日志</p>
          </div>
        )}
        actions={(
          <>
            <Link className="btn btn-ghost btn-sm" to="/admin/users">返回用户列表</Link>
            <button className="btn btn-primary btn-sm" type="submit" form="admin-user-create-form" disabled={saving}>
              {saving ? "保存中..." : "创建用户"}
            </button>
          </>
        )}
      />

      {error ? <div className="form-error">{error}</div> : null}

      <div className="admin-user-create-layout">
        <form id="admin-user-create-form" className="card admin-user-create-form" onSubmit={(event) => void submit(event)}>
          <section className="admin-user-form-section">
            <div className="admin-user-form-section-head">
              <div>
                <h2>账号信息</h2>
                <p>只要求填写登录必要信息；姓名为空时会使用邮箱前缀。</p>
              </div>
              <span className="admin-user-step">01</span>
            </div>
            <div className="admin-user-form-grid">
              <FormField label="邮箱">
                <input className="input" type="email" value={draft.email} onChange={(event) => patch("email", event.target.value)} placeholder="name@company.com" autoComplete="email" required />
              </FormField>
              <FormField label="登录用户名（可选）">
                <input className="input" value={draft.username} onChange={(event) => patch("username", event.target.value)} placeholder={emailName ? `例如 ${emailName}` : "例如 admin 或 ops"} autoComplete="username" />
              </FormField>
              <FormField label="姓名（可选）">
                <input className="input" value={draft.name} onChange={(event) => patch("name", event.target.value)} placeholder={emailName || "默认取邮箱前缀"} autoComplete="name" />
              </FormField>
              <FormField label="初始密码">
                <div className="admin-user-password-row">
                  <input className="input" type={showPassword ? "text" : "password"} value={draft.password} onChange={(event) => patch("password", event.target.value)} placeholder="至少 8 位" autoComplete="new-password" minLength={8} required />
                  <button className="btn btn-ghost btn-sm" type="button" onClick={() => setShowPassword((value) => !value)}>{showPassword ? "隐藏" : "显示"}</button>
                  <button className="btn btn-ghost btn-sm" type="button" onClick={generatePassword}>生成</button>
                </div>
              </FormField>
              <div className="admin-user-inline-note">
                初始密码只用于首次创建。正式发放时建议通过安全渠道交付，并要求用户首次登录后自行修改。
              </div>
            </div>
          </section>

          <section className="admin-user-form-section">
            <div className="admin-user-form-section-head">
              <div>
                <h2>权限与状态</h2>
                <p>角色决定后台权限，用户计划决定是否可使用平台通道一键创建专属网关。</p>
              </div>
              <span className="admin-user-step">02</span>
            </div>
            <div className="admin-user-form-grid">
              <SelectField label="角色" value={draft.role} onChange={(event) => patch("role", event.target.value)}>
                <option value="user">User</option>
                <option value="admin">Admin</option>
                <option value="owner">Owner</option>
              </SelectField>
              <SelectField label="用户计划" value={draft.plan} onChange={(event) => patch("plan", event.target.value)}>
                <option value="free">Free</option>
                <option value="super_vip">Super VIP</option>
              </SelectField>
              <SelectField label="账号状态" value={draft.status} onChange={(event) => patch("status", event.target.value)}>
                <option value="active">Active</option>
                <option value="suspended">Suspended</option>
                <option value="disabled">Disabled</option>
              </SelectField>
              <div className="admin-user-verified-card">
                <label className="check-line">
                  <input type="checkbox" checked={draft.emailVerified} onChange={(event) => patch("emailVerified", event.target.checked)} />
                  邮箱已验证
                </label>
                <p>取消后用户需要完成邮箱验证，才能进入完整工作区流程。</p>
              </div>
            </div>
          </section>

          <section className="admin-user-form-section">
            <div className="admin-user-form-section-head">
              <div>
                <h2>来源与审计</h2>
                <p>后台手动新增的真实用户统一按 Runtime 来源写入，不再生成 demo/test 数据。</p>
              </div>
              <span className="admin-user-step">03</span>
            </div>
            <div className="admin-user-origin-row">
              <span className="badge b-green">runtime</span>
              <span>创建用户、默认工作区和审计事件都会落库；列表页可通过邮箱快速定位新账号。</span>
            </div>
          </section>

          <div className="admin-user-form-footer">
            <Link className="btn btn-ghost btn-sm" to="/admin/users">取消</Link>
            <button className="btn btn-primary btn-sm" type="submit" disabled={saving}>{saving ? "保存中..." : "创建用户"}</button>
          </div>
        </form>

        <aside className="admin-user-create-side">
          <section className="card admin-user-help-card">
            <h3>创建后会发生什么</h3>
            <ul>
              <li>生成一个用户账号和默认工作区。</li>
              <li>账号角色、计划、状态写入审计。</li>
              <li>返回列表后自动按邮箱筛选新用户。</li>
            </ul>
          </section>
          <section className="card admin-user-help-card">
            <h3>权限提示</h3>
            <p><b>User</b> 只能进入用户控制台；<b>Admin</b> 和 <b>Owner</b> 可以进入平台管理后台。</p>
            <p><b>Super VIP</b> 可以使用平台通道能力创建专属网关；Free 用户只能管理自己的私有通道和网关。</p>
          </section>
          <section className="card admin-user-help-card muted-card">
            <h3>数据治理</h3>
            <p>这里不提供 demo/test 来源选项，避免后台操作重新污染真实试运行数据。</p>
          </section>
        </aside>
      </div>
    </AdminShell>
  );
}
