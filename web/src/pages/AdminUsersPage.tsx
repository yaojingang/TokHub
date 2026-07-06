import { FormEvent, useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { AdminShell } from "../components/AdminShell";
import {
  AdminUserInput,
  AdminUserItem,
  AdminUsersResult,
  adminUsers,
  bulkAdminUsers,
  createAdminUser,
  deleteAdminUser,
  updateAdminUser,
  updateAdminUserStatus
} from "../lib/api";
import { BulkActionBar, DataTable, DataTableColumn, FilterBar, PageHeader, SelectField, StatGrid } from "../ui";

type UserDraft = Required<Omit<AdminUserInput, "password">> & { password: string };
type UserFilters = {
  q: string;
  status: string;
  role: string;
  plan: string;
  origin: string;
};

const emptyDraft: UserDraft = {
  email: "",
  username: "",
  password: "",
  name: "",
  role: "user",
  plan: "free",
  status: "active",
  emailVerified: true,
  dataOrigin: "runtime"
};

const USER_PAGE_SIZE = 10;

export function AdminUsersPage() {
  const { t } = useTranslation(["common", "admin"]);
  const [data, setData] = useState<AdminUsersResult | null>(null);
  const [query, setQuery] = useState(() => initialUserFilter("q", ""));
  const [status, setStatus] = useState(() => initialUserFilter("status", "all"));
  const [role, setRole] = useState(() => initialUserFilter("role", "all"));
  const [plan, setPlan] = useState(() => initialUserFilter("plan", "all"));
  const [origin, setOrigin] = useState(() => initialUserFilter("origin", "all"));
  const [bulkStatus, setBulkStatus] = useState("suspended");
  const [bulkRole, setBulkRole] = useState("user");
  const [selected, setSelected] = useState<string[]>([]);
  const [savingID, setSavingID] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [selectionMode, setSelectionMode] = useState<"browse" | "bulk">("browse");
  const [editor, setEditor] = useState<{ mode: "create" | "edit"; user?: AdminUserItem } | null>(null);

  useEffect(() => {
    void load();
  }, []);

  function filters(): UserFilters {
    return { q: query.trim(), status, role, plan, origin };
  }

  function clearFilters() {
    const next = { q: "", status: "all", role: "all", plan: "all", origin: "all" };
    setQuery(next.q);
    setStatus(next.status);
    setRole(next.role);
    setPlan(next.plan);
    setOrigin(next.origin);
    setSelected([]);
    void load(next);
  }

  async function load(nextFilters = filters()) {
    setLoading(true);
    setError("");
    try {
      syncUserFiltersToURL(nextFilters);
      const result = await adminUsers(nextFilters);
      setData(result);
      setSelected((current) => current.filter((id) => result.items.some((item) => item.id === id && item.status !== "deleted")));
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载用户失败");
    } finally {
      setLoading(false);
    }
  }

  async function changeStatus(user: AdminUserItem, nextStatus: string) {
    if (user.status === nextStatus) return;
    if (nextStatus !== "active" && !window.confirm(`确认将 ${user.email} 标记为 ${statusLabel(nextStatus)}？该用户现有会话会被撤销。`)) return;
    setSavingID(user.id);
    setError("");
    setNotice("");
    try {
      const payload = await updateAdminUserStatus(user.id, nextStatus);
      setData((current) => current ? { ...current, items: current.items.map((item) => item.id === user.id ? payload.user : item) } : current);
      setNotice("用户状态已更新，并写入审计。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "更新用户状态失败");
    } finally {
      setSavingID("");
    }
  }

  async function removeUser(user: AdminUserItem) {
    if (!window.confirm(`确认删除 ${user.email}？系统会软删除账号、撤销会话并保留审计记录。`)) return;
    setSavingID(user.id);
    setError("");
    setNotice("");
    try {
      await deleteAdminUser(user.id);
      setData((current) => current ? { ...current, items: current.items.map((item) => item.id === user.id ? { ...item, status: "deleted", deletedAt: new Date().toISOString() } : item) } : current);
      setSelected((current) => current.filter((id) => id !== user.id));
      setNotice("用户已删除，并写入审计。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "删除用户失败");
    } finally {
      setSavingID("");
    }
  }

  async function submitUser(input: AdminUserInput) {
    setSaving(true);
    setError("");
    setNotice("");
    try {
      if (editor?.mode === "edit" && editor.user) {
        const payload = await updateAdminUser(editor.user.id, input);
        setData((current) => current ? { ...current, items: current.items.map((item) => item.id === editor.user?.id ? payload.user : item) } : current);
        setNotice("用户资料已保存，并写入审计。");
      } else {
        const payload = await createAdminUser(input);
        setData((current) => current ? { ...current, items: [payload.user, ...current.items] } : current);
        setNotice("用户已创建，并生成默认工作区。");
      }
      setEditor(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存用户失败");
    } finally {
      setSaving(false);
    }
  }

  async function runBulk(action: "status" | "role" | "delete") {
    const actionableIDs = selected.filter((id) => rows.some((item) => item.id === id && item.status !== "deleted"));
    if (!actionableIDs.length) {
      setError("请先选择可治理用户。已删除用户只能保留审计记录，不能再次批量编辑。");
      return;
    }
    const text = action === "delete" ? "删除所选用户" : action === "role" ? `将所选用户角色改为 ${roleLabel(bulkRole)}` : `将所选用户改为 ${statusLabel(bulkStatus)}`;
    if (!window.confirm(`确认${text}？该操作会写入审计。`)) return;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const result = await bulkAdminUsers({ action, ids: actionableIDs, status: bulkStatus, role: bulkRole });
      setData(result);
      setSelected([]);
      setNotice(action === "delete" ? "批量删除完成。" : action === "role" ? "批量角色更新完成。" : "批量状态更新完成。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "批量操作失败");
    } finally {
      setSaving(false);
    }
  }

  const rows = data?.items ?? [];
  const bulkMode = selectionMode === "bulk";
  const selectedSet = useMemo(() => new Set(selected), [selected]);
  const selectableRows = rows.filter((item) => item.status !== "deleted");
  const allVisibleSelected = selectableRows.length > 0 && selectableRows.every((item) => selectedSet.has(item.id));

  const stats = [
    { label: t("admin:users.stats.total"), value: data?.stats.total ?? 0, hint: `${data?.stats.active ?? 0} active` },
    { label: t("admin:users.stats.verified"), value: data?.stats.verified ?? 0, hint: "email_verified_at" },
    { label: t("admin:users.stats.admin"), value: (data?.stats.owners ?? 0) + (data?.stats.admins ?? 0), hint: "平台后台权限" },
    { label: t("admin:users.stats.superVip"), value: data?.stats.superVip ?? 0, hint: "可使用平台通道创建专属网关" },
    { label: t("admin:users.stats.deleted"), value: data?.stats.deleted ?? 0, hint: "软删除保留审计" }
  ];

  const columns = useMemo<DataTableColumn<AdminUserItem>[]>(() => {
    const base: DataTableColumn<AdminUserItem>[] = [
      {
        id: "user",
        header: "用户",
        meta: { className: "user-name-cell", headerClassName: "user-name-header" },
        cell: ({ row }) => <UserCell user={row.original} />
      },
      {
        id: "access",
        header: "权限与来源",
        meta: { className: "user-access-cell", headerClassName: "user-access-header" },
        cell: ({ row }) => <UserAccessCell user={row.original} />
      },
      {
        id: "resources",
        header: "资源",
        meta: { className: "user-resources-cell", headerClassName: "user-resources-header" },
        cell: ({ row }) => <UserResourcesCell user={row.original} />
      },
      {
        id: "lastActive",
        header: "最近活跃",
        meta: { className: "user-time-cell muted-time", headerClassName: "user-time-header" },
        cell: ({ row }) => timeLabel(row.original.lastActiveAt || row.original.createdAt)
      },
      {
        id: "status",
        header: "状态",
        meta: { className: "user-status-cell", headerClassName: "user-status-header" },
        cell: ({ row }) => row.original.status === "deleted" ? (
          <span className="badge b-gray user-deleted-status">Deleted</span>
        ) : (
          <SelectField className="compact-select user-status-select" value={row.original.status} disabled={savingID === row.original.id} onChange={(event) => void changeStatus(row.original, event.target.value)}>
            <option value="active">Active</option>
            <option value="suspended">Suspended</option>
            <option value="disabled">Disabled</option>
          </SelectField>
        )
      },
      {
        id: "actions",
        header: "操作",
        meta: { className: "actions user-actions-cell", headerClassName: "user-actions-header" },
        cell: ({ row }) => row.original.status === "deleted" ? (
          <span className="muted user-row-archived">已删除</span>
        ) : (
          <div className="table-actions user-row-actions">
            <button type="button" className="btn btn-ghost btn-sm" onClick={() => setEditor({ mode: "edit", user: row.original })}>编辑</button>
            <button type="button" className="btn btn-ghost btn-sm danger-text" disabled={savingID === row.original.id} onClick={() => void removeUser(row.original)}>删除</button>
          </div>
        )
      }
    ];
    if (!bulkMode) return base;
    return [
      {
        id: "select",
        meta: { className: "user-select-cell", headerClassName: "user-select-header" },
        header: () => <input type="checkbox" aria-label="选择全部用户" checked={allVisibleSelected} onChange={(event) => setSelected((current) => event.target.checked ? Array.from(new Set([...current, ...selectableRows.map((item) => item.id)])) : current.filter((id) => !selectableRows.some((item) => item.id === id)))} />,
        cell: ({ row }) => <input type="checkbox" aria-label={`选择 ${row.original.email}`} checked={selectedSet.has(row.original.id)} disabled={row.original.status === "deleted"} onChange={(event) => setSelected((current) => event.target.checked ? [...current, row.original.id] : current.filter((id) => id !== row.original.id))} />
      },
      ...base
    ];
  }, [allVisibleSelected, bulkMode, savingID, selectableRows, selectedSet]);

  return (
    <AdminShell title={t("admin:users.title")} crumb={t("admin:crumbs.users")}>
      <PageHeader
        description={<p className="page-intro">{t("admin:users.intro")}</p>}
        actions={(
          <>
            <button className="btn btn-ghost btn-sm" onClick={() => void load()} disabled={loading}>{t("common:actions.refresh")}</button>
            <Link className="btn btn-primary btn-sm" to="/admin/users/new">{t("admin:users.add")}</Link>
          </>
        )}
      />
      {error ? <div className="form-error">{error}</div> : null}
      {notice ? <div className="form-notice">{notice}</div> : null}

      <StatGrid items={stats} />

      <div className="user-control-strip card">
        <FilterBar className="user-filter-toolbar">
          <SelectField value={status} onChange={(event) => setStatus(event.target.value)}>
            <option value="all">状态：全部</option>
            <option value="active">状态：Active</option>
            <option value="suspended">状态：Suspended</option>
            <option value="disabled">状态：Disabled</option>
            <option value="deleted">状态：Deleted</option>
          </SelectField>
          <SelectField value={role} onChange={(event) => setRole(event.target.value)}>
            <option value="all">角色：全部</option>
            <option value="owner">角色：Owner</option>
            <option value="admin">角色：Admin</option>
            <option value="user">角色：User</option>
          </SelectField>
          <SelectField value={plan} onChange={(event) => setPlan(event.target.value)}>
            <option value="all">计划：全部</option>
            <option value="free">计划：Free</option>
            <option value="super_vip">计划：Super VIP</option>
          </SelectField>
          <SelectField value={origin} onChange={(event) => setOrigin(event.target.value)}>
            <option value="all">来源：全部</option>
            <option value="runtime">来源：Runtime</option>
            <option value="system">来源：System</option>
            <option value="test">来源：Test</option>
            <option value="demo">来源：Demo</option>
          </SelectField>
          <SelectField aria-label="用户列表模式" className="mode-select" value={selectionMode} onChange={(event) => {
            const nextMode = event.target.value === "bulk" ? "bulk" : "browse";
            setSelectionMode(nextMode);
            if (nextMode === "browse") setSelected([]);
          }}>
            <option value="browse">模式：浏览</option>
            <option value="bulk">模式：多选</option>
          </SelectField>
          <input className="input" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索邮箱、姓名、角色、来源" />
          <button className="btn btn-ghost btn-sm" onClick={() => void load()} disabled={loading}>{t("common:actions.filter")}</button>
          <button className="btn btn-ghost btn-sm" onClick={clearFilters} disabled={loading}>{t("common:actions.clearFilters")}</button>
        </FilterBar>

        {bulkMode ? (
          <>
            <span className="user-toolbar-divider" aria-hidden="true" />

            <BulkActionBar className="user-bulk-toolbar">
              <span className="muted">已选 {selected.length} 项</span>
              <SelectField className="compact-select" value={bulkStatus} onChange={(event) => setBulkStatus(event.target.value)}>
                <option value="active">启用</option>
                <option value="suspended">暂停</option>
                <option value="disabled">禁用</option>
              </SelectField>
              <SelectField className="compact-select" value={bulkRole} onChange={(event) => setBulkRole(event.target.value)}>
                <option value="user">用户</option>
                <option value="admin">管理</option>
                <option value="owner">Owner</option>
              </SelectField>
              <button className="btn btn-ghost btn-sm" disabled={!selected.length || saving} onClick={() => void runBulk("status")} title="批量改状态">批量改状态</button>
              <button className="btn btn-ghost btn-sm" disabled={!selected.length || saving} onClick={() => void runBulk("role")} title="批量改角色">批量改角色</button>
              <button className="btn btn-ghost btn-sm" disabled={!selected.length || saving} onClick={() => void runBulk("delete")} title="批量删除">批量删除</button>
            </BulkActionBar>
          </>
        ) : null}
      </div>

      <DataTable
        data={rows}
        columns={columns}
        loading={loading}
        pageSize={USER_PAGE_SIZE}
        tableClassName={`admin-users-table ${bulkMode ? "select-mode" : "browse-mode"}`}
        wrapClassName="admin-users-table-wrap"
        loadingText="正在加载用户..."
        footerNote="删除为软删除，会撤销 active session 并保留审计"
        empty={<div className="empty-state"><h4>没有匹配用户</h4><p>调整筛选条件后再试。</p></div>}
      />

      <UserEditor editor={editor} saving={saving} onClose={() => setEditor(null)} onSubmit={submitUser} />
    </AdminShell>
  );
}

function UserEditor({ editor, saving, onClose, onSubmit }: { editor: { mode: "create" | "edit"; user?: AdminUserItem } | null; saving: boolean; onClose: () => void; onSubmit: (input: AdminUserInput) => Promise<void> }) {
  const [draft, setDraft] = useState<UserDraft>(emptyDraft);
  const open = Boolean(editor);

  useEffect(() => {
    if (!editor) return;
    if (editor.mode === "edit" && editor.user) {
      setDraft({
        email: editor.user.email,
        username: editor.user.username || "",
        password: "",
        name: editor.user.name,
        role: editor.user.role,
        plan: editor.user.plan || "free",
        status: editor.user.status,
        emailVerified: editor.user.emailVerified,
        dataOrigin: editor.user.dataOrigin
      });
    } else {
      setDraft(emptyDraft);
    }
  }, [editor]);

  function patch<K extends keyof UserDraft>(key: K, value: UserDraft[K]) {
    setDraft((current) => ({ ...current, [key]: value }));
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    const input: AdminUserInput = {
      email: draft.email,
      username: draft.username,
      name: draft.name,
      role: draft.role,
      plan: draft.plan,
      status: draft.status,
      emailVerified: draft.emailVerified,
      dataOrigin: draft.dataOrigin
    };
    if (draft.password.trim()) input.password = draft.password;
    await onSubmit(input);
  }

  return (
    <div className={`drawer-mask ${open ? "open" : ""}`} onClick={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <form className={`drawer ${open ? "open" : ""} private-editor`} onSubmit={(event) => void submit(event)}>
        <div className="dh">
          <div><h3>{editor?.mode === "edit" ? "编辑用户" : "新增用户"}</h3><p>账号、角色、状态和来源都会写入审计。</p></div>
          <button type="button" className="icon-btn" onClick={onClose}>×</button>
        </div>
        <div className="db form-grid">
          <label><span>邮箱</span><input className="input" type="email" value={draft.email} onChange={(event) => patch("email", event.target.value)} required /></label>
          <label><span>登录用户名</span><input className="input" value={draft.username} onChange={(event) => patch("username", event.target.value)} placeholder="例如 admin 或 ops" autoComplete="username" /></label>
          <label><span>姓名</span><input className="input" value={draft.name} onChange={(event) => patch("name", event.target.value)} placeholder="默认取邮箱前缀" /></label>
          <label><span>{editor?.mode === "edit" ? "新密码（可选）" : "初始密码"}</span><input className="input" type="password" value={draft.password} onChange={(event) => patch("password", event.target.value)} required={editor?.mode !== "edit"} minLength={8} /></label>
          <label><span>角色</span><SelectField value={draft.role} onChange={(event) => patch("role", event.target.value)}><option value="user">User</option><option value="admin">Admin</option><option value="owner">Owner</option></SelectField></label>
          <label><span>用户计划</span><SelectField value={draft.plan} onChange={(event) => patch("plan", event.target.value)}><option value="free">Free</option><option value="super_vip">Super VIP</option></SelectField></label>
          <label><span>状态</span><SelectField value={draft.status} onChange={(event) => patch("status", event.target.value)}><option value="active">Active</option><option value="suspended">Suspended</option><option value="disabled">Disabled</option></SelectField></label>
          <label><span>来源</span><SelectField value={draft.dataOrigin} onChange={(event) => patch("dataOrigin", event.target.value)}><option value="runtime">Runtime</option><option value="system">System</option></SelectField></label>
          <label className="check-line"><input type="checkbox" checked={draft.emailVerified} onChange={(event) => patch("emailVerified", event.target.checked)} /> 邮箱已验证</label>
        </div>
        <div className="df">
          <button type="button" className="btn btn-ghost btn-sm" onClick={onClose}>取消</button>
          <button type="submit" className="btn btn-primary btn-sm" disabled={saving}>{saving ? "保存中..." : "保存用户"}</button>
        </div>
      </form>
    </div>
  );
}

function UserCell({ user }: { user: AdminUserItem }) {
  const displayName = user.name?.trim() || user.username || user.email;
  const avatarText = user.avatar || displayName[0] || "U";
  const loginName = user.username ? `@${user.username}` : "未设置用户名";
  return (
    <div className="u-cell">
      <span className="av">{avatarText}</span>
      <span className="nm" aria-label={`${displayName} ${loginName} ${user.email}`}>
        <span className="user-name-line">{displayName}</span>
        {" "}
        <small>{loginName} · {user.email}</small>
      </span>
    </div>
  );
}

function UserAccessCell({ user }: { user: AdminUserItem }) {
  return (
    <div className="user-access-stack">
      <span className="user-access-row">
        <span className={`badge ${roleClass(user.role)} dot`}>{roleLabel(user.role)}</span>
        <span className={`badge ${planClass(user.plan)} dot`}>{planLabel(user.plan)}</span>
      </span>
      <span className="user-access-row user-access-meta">
        {user.emailVerified ? <span className="badge b-green dot">已验证</span> : <span className="badge b-amber dot">未验证</span>}
        <span className={`badge ${originClass(user.dataOrigin)} dot`}>{user.dataOrigin}</span>
      </span>
    </div>
  );
}

function UserResourcesCell({ user }: { user: AdminUserItem }) {
  return (
    <div className="user-resource-metrics" aria-label={`工作区 ${user.orgs}，网关 ${user.gateways}，私有通道 ${user.privateChannels}，审计 ${user.auditEvents}`}>
      <span><b>{user.orgs}</b><small>工作区</small></span>
      <span><b>{user.gateways}</b><small>网关</small></span>
      <span><b>{user.privateChannels}</b><small>私有</small></span>
      <span><b>{user.auditEvents}</b><small>审计</small></span>
    </div>
  );
}

function roleLabel(role: string) {
  if (role === "owner") return "Owner";
  if (role === "admin") return "Admin";
  return "User";
}

function roleClass(role: string) {
  if (role === "owner") return "b-magenta";
  if (role === "admin") return "b-blue";
  return "b-gray";
}

function planLabel(plan: string) {
  return plan === "super_vip" ? "Super VIP" : "Free";
}

function planClass(plan: string) {
  return plan === "super_vip" ? "b-magenta" : "b-gray";
}

function originClass(origin: string) {
  if (origin === "runtime") return "b-green";
  if (origin === "system") return "b-blue";
  if (origin === "test") return "b-amber";
  return "b-gray";
}

function statusLabel(status: string) {
  if (status === "suspended") return "Suspended";
  if (status === "disabled") return "Disabled";
  if (status === "deleted") return "Deleted";
  return "Active";
}

function timeLabel(value?: string) {
  if (!value) return "-";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "-" : date.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

function initialUserFilter(key: keyof UserFilters, fallback: string) {
  const value = new URLSearchParams(window.location.search).get(key);
  return value?.trim() || fallback;
}

function syncUserFiltersToURL(filters: UserFilters) {
  const params = new URLSearchParams(window.location.search);
  for (const key of ["q", "status", "role", "plan", "origin"]) {
    params.delete(key);
  }
  if (filters.q) params.set("q", filters.q);
  if (filters.status !== "all") params.set("status", filters.status);
  if (filters.role !== "all") params.set("role", filters.role);
  if (filters.plan !== "all") params.set("plan", filters.plan);
  if (filters.origin !== "all") params.set("origin", filters.origin);
  const nextSearch = params.toString();
  const nextURL = `${window.location.pathname}${nextSearch ? `?${nextSearch}` : ""}${window.location.hash}`;
  window.history.replaceState(null, "", nextURL);
}
