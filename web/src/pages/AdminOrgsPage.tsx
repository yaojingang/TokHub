import { FormEvent, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { AdminShell } from "../components/AdminShell";
import {
  AdminOrgInput,
  AdminOrgItem,
  AdminOrgsResult,
  adminOrgs,
  bulkAdminOrgs,
  createAdminOrg,
  deleteAdminOrg,
  updateAdminOrg,
  updateAdminOrgStatus
} from "../lib/api";
import { BulkActionBar, DataTable, DataTableColumn, FilterBar, PageHeader, SelectField, StatGrid } from "../ui";

type OrgDraft = Required<AdminOrgInput>;
type OrgFilters = {
  q: string;
  status: string;
  plan: string;
  origin: string;
};

const emptyDraft: OrgDraft = {
  name: "",
  slug: "",
  plan: "starter",
  status: "active",
  timezone: "Asia/Shanghai",
  dataOrigin: "runtime"
};

const ORG_PAGE_SIZE = 10;

export function AdminOrgsPage() {
  const { t } = useTranslation(["common", "admin"]);
  const [data, setData] = useState<AdminOrgsResult | null>(null);
  const [query, setQuery] = useState(() => initialOrgFilter("q", ""));
  const [status, setStatus] = useState(() => initialOrgFilter("status", "all"));
  const [plan, setPlan] = useState(() => initialOrgFilter("plan", "all"));
  const [origin, setOrigin] = useState(() => initialOrgFilter("origin", "all"));
  const [bulkStatus, setBulkStatus] = useState("suspended");
  const [bulkPlan, setBulkPlan] = useState("team");
  const [selected, setSelected] = useState<string[]>([]);
  const [savingID, setSavingID] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [selectionMode, setSelectionMode] = useState<"browse" | "bulk">("browse");
  const [editor, setEditor] = useState<{ mode: "create" | "edit"; org?: AdminOrgItem } | null>(null);

  useEffect(() => {
    void load();
  }, []);

  function filters(): OrgFilters {
    return { q: query.trim(), status, plan, origin };
  }

  function clearFilters() {
    const next = { q: "", status: "all", plan: "all", origin: "all" };
    setQuery(next.q);
    setStatus(next.status);
    setPlan(next.plan);
    setOrigin(next.origin);
    setSelected([]);
    void load(next);
  }

  async function load(nextFilters = filters()) {
    setLoading(true);
    setError("");
    try {
      syncOrgFiltersToURL(nextFilters);
      const result = await adminOrgs(nextFilters);
      setData(result);
      setSelected((current) => current.filter((id) => result.items.some((item) => item.id === id && item.id !== "org_default" && item.status !== "deleted")));
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载组织失败");
    } finally {
      setLoading(false);
    }
  }

  async function changeStatus(org: AdminOrgItem, nextStatus: string) {
    if (org.status === nextStatus) return;
    if (nextStatus !== "active" && !window.confirm(`确认将 ${org.name} 标记为 ${statusLabel(nextStatus)}？该组织的工作区治理会进入受限状态。`)) return;
    setSavingID(org.id);
    setError("");
    setNotice("");
    try {
      const payload = await updateAdminOrgStatus(org.id, nextStatus);
      setData((current) => current ? { ...current, items: current.items.map((item) => item.id === org.id ? payload.org : item) } : current);
      setNotice("组织状态已更新，并写入审计。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "更新组织状态失败");
    } finally {
      setSavingID("");
    }
  }

  async function removeOrg(org: AdminOrgItem) {
    if (!window.confirm(`确认删除 ${org.name}？系统会软删除组织并保留审计记录。`)) return;
    setSavingID(org.id);
    setError("");
    setNotice("");
    try {
      await deleteAdminOrg(org.id);
      setData((current) => current ? { ...current, items: current.items.map((item) => item.id === org.id ? { ...item, status: "deleted", deletedAt: new Date().toISOString() } : item) } : current);
      setSelected((current) => current.filter((id) => id !== org.id));
      setNotice("组织已删除，并写入审计。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "删除组织失败");
    } finally {
      setSavingID("");
    }
  }

  async function submitOrg(input: AdminOrgInput) {
    setSaving(true);
    setError("");
    setNotice("");
    try {
      if (editor?.mode === "edit" && editor.org) {
        const payload = await updateAdminOrg(editor.org.id, input);
        setData((current) => current ? { ...current, items: current.items.map((item) => item.id === editor.org?.id ? payload.org : item) } : current);
        setNotice("组织资料已保存，并写入审计。");
      } else {
        const payload = await createAdminOrg(input);
        setData((current) => current ? { ...current, items: [payload.org, ...current.items] } : current);
        setNotice("组织已创建。");
      }
      setEditor(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存组织失败");
    } finally {
      setSaving(false);
    }
  }

  async function runBulk(action: "status" | "plan" | "delete") {
    const actionableIDs = selected.filter((id) => rows.some((item) => item.id === id && item.id !== "org_default" && item.status !== "deleted"));
    if (!actionableIDs.length) {
      setError("请先选择可治理组织。默认组织和已删除组织不能再次批量编辑。");
      return;
    }
    const text = action === "delete" ? "删除所选组织" : action === "plan" ? `将所选组织套餐改为 ${planLabel(bulkPlan)}` : `将所选组织改为 ${statusLabel(bulkStatus)}`;
    if (!window.confirm(`确认${text}？默认平台组织受保护，操作会写入审计。`)) return;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const result = await bulkAdminOrgs({ action, ids: actionableIDs, status: bulkStatus, plan: bulkPlan });
      setData(result);
      setSelected([]);
      setNotice(action === "delete" ? "批量删除完成。" : action === "plan" ? "批量套餐更新完成。" : "批量状态更新完成。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "批量操作失败");
    } finally {
      setSaving(false);
    }
  }

  const rows = data?.items ?? [];
  const bulkMode = selectionMode === "bulk";
  const selectedSet = useMemo(() => new Set(selected), [selected]);
  const selectableRows = rows.filter((item) => item.id !== "org_default" && item.status !== "deleted");
  const allVisibleSelected = selectableRows.length > 0 && selectableRows.every((item) => selectedSet.has(item.id));

  const stats = [
    { label: t("admin:orgs.stats.total"), value: data?.stats.total ?? 0, hint: `${data?.stats.active ?? 0} active` },
    { label: t("admin:orgs.stats.system"), value: data?.stats.system ?? 0, hint: "平台默认" },
    { label: t("admin:orgs.stats.runtime"), value: data?.stats.runtime ?? 0, hint: "真实用户/团队" },
    { label: t("admin:orgs.stats.suspended"), value: data?.stats.suspended ?? 0, hint: "受限状态" },
    { label: t("admin:orgs.stats.deleted"), value: data?.stats.deleted ?? 0, hint: "软删除保留审计" }
  ];

  const columns = useMemo<DataTableColumn<AdminOrgItem>[]>(() => {
    const base: DataTableColumn<AdminOrgItem>[] = [
      {
        id: "org",
        header: "组织",
        cell: ({ row }) => (
          <div className="u-cell">
            <span className="av">{initials(row.original.name)}</span>
            <span className="nm">{row.original.name}<small>{row.original.slug} · {row.original.timezone}</small></span>
          </div>
        )
      },
      { id: "plan", header: "计划", cell: ({ row }) => <span className="badge b-blue">{row.original.plan}</span> },
      { id: "origin", header: "来源", cell: ({ row }) => <span className={`badge ${originClass(row.original.dataOrigin)}`}>{row.original.dataOrigin}</span> },
      { accessorKey: "members", header: "成员", meta: { className: "mono" } },
      { accessorKey: "gateways", header: "网关", meta: { className: "mono" } },
      { accessorKey: "privateChannels", header: "私有通道", meta: { className: "mono" } },
      { accessorKey: "activeKeys", header: "有效 Key", meta: { className: "mono" } },
      { accessorKey: "auditEvents", header: "审计", meta: { className: "mono" } },
      { id: "updatedAt", header: "更新时间", meta: { className: "muted-time" }, cell: ({ row }) => timeLabel(row.original.updatedAt || row.original.createdAt) },
      {
        id: "status",
        header: "状态",
        meta: { className: "org-status-cell", headerClassName: "org-status-col" },
        cell: ({ row }) => {
          const protectedOrg = row.original.id === "org_default";
          return (
            <SelectField className="compact-select org-status-select" value={row.original.status} disabled={savingID === row.original.id || protectedOrg || row.original.status === "deleted"} onChange={(event) => void changeStatus(row.original, event.target.value)}>
              {row.original.status === "deleted" ? <option value="deleted" disabled>Deleted</option> : null}
              <option value="active">Active</option>
              <option value="suspended">Suspended</option>
              <option value="disabled">Disabled</option>
            </SelectField>
          );
        }
      },
      {
        id: "actions",
        header: "操作",
        cell: ({ row }) => {
          const protectedOrg = row.original.id === "org_default";
          return (
            <div className="table-actions">
              <button className="btn btn-ghost btn-sm" disabled={row.original.status === "deleted"} onClick={() => setEditor({ mode: "edit", org: row.original })}>编辑</button>
              <button className="btn btn-ghost btn-sm" disabled={protectedOrg || savingID === row.original.id || row.original.status === "deleted"} onClick={() => void removeOrg(row.original)}>删除</button>
            </div>
          );
        }
      }
    ];
    if (!bulkMode) return base;
    return [
      {
        id: "select",
        header: () => <input type="checkbox" aria-label="选择全部组织" checked={allVisibleSelected} onChange={(event) => setSelected((current) => event.target.checked ? Array.from(new Set([...current, ...selectableRows.map((item) => item.id)])) : current.filter((id) => !selectableRows.some((item) => item.id === id)))} />,
        cell: ({ row }) => <input type="checkbox" aria-label={`选择 ${row.original.name}`} checked={selectedSet.has(row.original.id)} disabled={row.original.id === "org_default" || row.original.status === "deleted"} onChange={(event) => setSelected((current) => event.target.checked ? [...current, row.original.id] : current.filter((id) => id !== row.original.id))} />
      },
      ...base
    ];
  }, [allVisibleSelected, bulkMode, savingID, selectableRows, selectedSet]);

  return (
    <AdminShell title={t("admin:orgs.title")} crumb={t("admin:crumbs.orgs")}>
      <PageHeader
        description={<p className="page-intro">{t("admin:orgs.intro")}</p>}
        actions={(
          <>
            <button className="btn btn-ghost btn-sm" onClick={() => void load()} disabled={loading}>{t("common:actions.refresh")}</button>
            <button className="btn btn-primary btn-sm" onClick={() => setEditor({ mode: "create" })}>{t("admin:orgs.add")}</button>
          </>
        )}
      />
      {error ? <div className="form-error">{error}</div> : null}
      {notice ? <div className="form-notice">{notice}</div> : null}

      <StatGrid items={stats} />

      <div className="org-control-strip card">
        <FilterBar className="org-filter-toolbar">
          <SelectField className="org-filter-select" value={status} onChange={(event) => setStatus(event.target.value)}>
            <option value="all">所有状态</option>
            <option value="active">Active</option>
            <option value="suspended">Suspended</option>
            <option value="disabled">Disabled</option>
            <option value="deleted">Deleted</option>
          </SelectField>
          <SelectField className="org-filter-select" value={plan} onChange={(event) => setPlan(event.target.value)}>
            <option value="all">所有计划</option>
            <option value="starter">Starter</option>
            <option value="team">Team</option>
            <option value="business">Business</option>
            <option value="enterprise">Enterprise</option>
          </SelectField>
          <SelectField className="org-filter-select" value={origin} onChange={(event) => setOrigin(event.target.value)}>
            <option value="all">所有来源</option>
            <option value="runtime">Runtime</option>
            <option value="system">System</option>
            <option value="test">Test</option>
            <option value="demo">Demo</option>
          </SelectField>
          <SelectField aria-label="组织列表模式" className="org-filter-select org-mode-select mode-select" value={selectionMode} onChange={(event) => {
            const nextMode = event.target.value === "bulk" ? "bulk" : "browse";
            setSelectionMode(nextMode);
            if (nextMode === "browse") setSelected([]);
          }}>
            <option value="browse">浏览</option>
            <option value="bulk">多选</option>
          </SelectField>
          <input className="input org-filter-search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索组织名、slug、套餐、来源..." />
          <div className="org-filter-actions">
            <button className="btn btn-ghost btn-sm" onClick={() => void load()} disabled={loading}>{t("common:actions.filter")}</button>
            <button className="btn btn-ghost btn-sm" onClick={clearFilters} disabled={loading}>{t("common:actions.clearFilters")}</button>
          </div>
        </FilterBar>

        {bulkMode ? (
          <>
            <span className="org-toolbar-divider" aria-hidden="true" />

            <BulkActionBar className="org-bulk-toolbar">
              <span className="muted">已选 {selected.length} 项</span>
              <SelectField className="compact-select" value={bulkStatus} onChange={(event) => setBulkStatus(event.target.value)}>
                <option value="active">启用</option>
                <option value="suspended">暂停</option>
                <option value="disabled">禁用</option>
              </SelectField>
              <SelectField className="compact-select" value={bulkPlan} onChange={(event) => setBulkPlan(event.target.value)}>
                <option value="starter">Starter</option>
                <option value="team">Team</option>
                <option value="business">Business</option>
                <option value="enterprise">Enterprise</option>
              </SelectField>
              <button className="btn btn-ghost btn-sm" disabled={!selected.length || saving} onClick={() => void runBulk("status")} title="批量改状态">批量改状态</button>
              <button className="btn btn-ghost btn-sm" disabled={!selected.length || saving} onClick={() => void runBulk("plan")} title="批量改套餐">批量改套餐</button>
              <button className="btn btn-ghost btn-sm" disabled={!selected.length || saving} onClick={() => void runBulk("delete")} title="批量删除">批量删除</button>
            </BulkActionBar>
          </>
        ) : null}
      </div>

      <DataTable
        data={rows}
        columns={columns}
        loading={loading}
        pageSize={ORG_PAGE_SIZE}
        tableClassName={`org-table ${bulkMode ? "select-mode" : "browse-mode"}`}
        wrapClassName="org-table-wrap"
        loadingText="正在加载组织..."
        footerNote="默认平台组织受保护，不能暂停或删除"
        empty={<div className="empty-state"><h4>没有匹配组织</h4><p>调整筛选条件后再试。</p></div>}
      />

      <OrgEditor editor={editor} saving={saving} onClose={() => setEditor(null)} onSubmit={submitOrg} />
    </AdminShell>
  );
}

function OrgEditor({ editor, saving, onClose, onSubmit }: { editor: { mode: "create" | "edit"; org?: AdminOrgItem } | null; saving: boolean; onClose: () => void; onSubmit: (input: AdminOrgInput) => Promise<void> }) {
  const [draft, setDraft] = useState<OrgDraft>(emptyDraft);
  const open = Boolean(editor);
  const protectedOrg = editor?.org?.id === "org_default";

  useEffect(() => {
    if (!editor) return;
    if (editor.mode === "edit" && editor.org) {
      setDraft({
        name: editor.org.name,
        slug: editor.org.slug,
        plan: editor.org.plan,
        status: editor.org.status,
        timezone: editor.org.timezone,
        dataOrigin: editor.org.dataOrigin
      });
    } else {
      setDraft(emptyDraft);
    }
  }, [editor]);

  function patch<K extends keyof OrgDraft>(key: K, value: OrgDraft[K]) {
    setDraft((current) => ({ ...current, [key]: value }));
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    await onSubmit({
      name: draft.name,
      slug: draft.slug,
      plan: draft.plan,
      status: protectedOrg ? "active" : draft.status,
      timezone: draft.timezone,
      dataOrigin: draft.dataOrigin
    });
  }

  return (
    <div className={`drawer-mask ${open ? "open" : ""}`} onClick={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <form className={`drawer ${open ? "open" : ""} private-editor`} onSubmit={(event) => void submit(event)}>
        <div className="dh">
          <div><h3>{editor?.mode === "edit" ? "编辑组织" : "新增组织"}</h3><p>组织名、计划、状态和来源都会写入审计。</p></div>
          <button type="button" className="icon-btn" onClick={onClose}>×</button>
        </div>
        <div className="db form-grid">
          <label><span>组织名</span><input className="input" value={draft.name} onChange={(event) => patch("name", event.target.value)} required /></label>
          <label><span>Slug</span><input className="input" value={draft.slug} onChange={(event) => patch("slug", event.target.value)} placeholder="留空自动生成" /></label>
          <label><span>计划</span><SelectField value={draft.plan} onChange={(event) => patch("plan", event.target.value)}><option value="starter">Starter</option><option value="team">Team</option><option value="business">Business</option><option value="enterprise">Enterprise</option></SelectField></label>
          <label><span>状态</span><SelectField value={protectedOrg ? "active" : draft.status} disabled={protectedOrg} onChange={(event) => patch("status", event.target.value)}><option value="active">Active</option><option value="suspended">Suspended</option><option value="disabled">Disabled</option></SelectField></label>
          <label><span>时区</span><input className="input" value={draft.timezone} onChange={(event) => patch("timezone", event.target.value)} /></label>
          <label><span>来源</span><SelectField value={draft.dataOrigin} onChange={(event) => patch("dataOrigin", event.target.value)}><option value="runtime">Runtime</option><option value="system">System</option><option value="test">Test</option><option value="demo">Demo</option></SelectField></label>
        </div>
        <div className="df">
          <button type="button" className="btn btn-ghost btn-sm" onClick={onClose}>取消</button>
          <button type="submit" className="btn btn-primary btn-sm" disabled={saving}>{saving ? "保存中..." : "保存组织"}</button>
        </div>
      </form>
    </div>
  );
}

function initials(value: string) {
  return value.split(/\s+/).filter(Boolean).slice(0, 2).map((part) => part[0]).join("").toUpperCase().slice(0, 2) || "T";
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

function planLabel(plan: string) {
  if (plan === "enterprise") return "Enterprise";
  if (plan === "business") return "Business";
  if (plan === "team") return "Team";
  return "Starter";
}

function timeLabel(value?: string) {
  if (!value) return "-";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "-" : date.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

function initialOrgFilter(key: keyof OrgFilters, fallback: string) {
  const value = new URLSearchParams(window.location.search).get(key);
  return value?.trim() || fallback;
}

function syncOrgFiltersToURL(filters: OrgFilters) {
  const params = new URLSearchParams(window.location.search);
  for (const key of ["q", "status", "plan", "origin"]) {
    params.delete(key);
  }
  if (filters.q) params.set("q", filters.q);
  if (filters.status !== "all") params.set("status", filters.status);
  if (filters.plan !== "all") params.set("plan", filters.plan);
  if (filters.origin !== "all") params.set("origin", filters.origin);
  const nextSearch = params.toString();
  const nextURL = `${window.location.pathname}${nextSearch ? `?${nextSearch}` : ""}${window.location.hash}`;
  window.history.replaceState(null, "", nextURL);
}
