import { FormEvent, useEffect, useMemo, useState } from "react";
import { AdminShell } from "../components/AdminShell";
import { ConsoleShell } from "../components/ConsoleShell";
import { auditExportURL, auditLogDetail, AuditLogItem, auditLogs, AuditLogResult } from "../lib/api";
import { Button, DataTable, DataTableColumn, FilterBar, SelectField, StatGrid, StatusBadge } from "../ui";
import { ProbeLogsPanel } from "./ProbeLogsPanel";

type Scope = "admin" | "console";
type EventClass = "governance" | "system" | "all";

type Filters = {
  eventClass: EventClass;
  action: string;
  actor: string;
  objectType: string;
  objectId: string;
  result: string;
  query: string;
  from: string;
  to: string;
  limit: number;
};

const emptyFilters: Filters = {
  eventClass: "governance",
  action: "",
  actor: "",
  objectType: "",
  objectId: "",
  result: "",
  query: "",
  from: "",
  to: "",
  limit: 100
};

export function AuditPage({ scope = "admin" }: { scope?: Scope }) {
  const [activeTab, setActiveTab] = useState<"audit" | "probe">(() => scope === "admin" && new URLSearchParams(window.location.search).get("tab") === "probe" ? "probe" : "audit");
  const [filters, setFilters] = useState<Filters>(() => initialAuditFilters());
  const [appliedFilters, setAppliedFilters] = useState<Filters>(() => initialAuditFilters());
  const [result, setResult] = useState<AuditLogResult>({ items: [], total: 0 });
  const [selected, setSelected] = useState<AuditLogItem | null>(null);
  const [loading, setLoading] = useState(true);
  const [detailLoading, setDetailLoading] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    void load(appliedFilters);
  }, [scope]);

  async function load(nextFilters = filters) {
    const cleaned = cleanAuditFilters(nextFilters);
    setLoading(true);
    setError("");
    try {
      syncAuditFiltersToURL(cleaned);
      setResult(await auditLogs(scope, cleaned));
      setAppliedFilters(cleaned);
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载审计日志失败");
    } finally {
      setLoading(false);
    }
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    await load();
  }

  async function openDetail(item: AuditLogItem) {
    setSelected(item);
    setDetailLoading(true);
    setError("");
    try {
      setSelected(await auditLogDetail(scope, item.id));
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载审计详情失败");
    } finally {
      setDetailLoading(false);
    }
  }

  function exportCSV() {
    if (!window.confirm("确认导出当前筛选范围内最多 500 条审计日志？导出内容已脱敏，但仍属于安全数据。")) return;
    window.location.href = auditExportURL(scope, { ...appliedFilters, limit: 500 });
  }

  function applyQuickFilter(nextFilters: Partial<Filters>) {
    const next = cleanAuditFilters({ ...emptyFilters, ...nextFilters });
    setFilters(next);
    void load(next);
  }

  const stats = useMemo(() => {
    const failures = result.items.filter((item) => item.result !== "success").length;
    const actors = new Set(result.items.map((item) => item.actorEmail || item.actorId).filter(Boolean)).size;
    return [
      { label: "匹配事件", value: String(result.total), hint: auditScopeHint(appliedFilters.eventClass) },
      { label: "当前加载", value: String(result.items.length), hint: "loaded rows" },
      { label: "当前账号", value: String(actors), hint: "loaded actors" },
      { label: "当前异常", value: String(failures), hint: "loaded non-success", tone: failures ? "red" as const : "gray" as const },
      { label: "导出上限", value: "500", hint: "safe csv rows" }
    ];
  }, [appliedFilters.eventClass, result]);
  const auditColumns = useMemo<DataTableColumn<AuditLogItem>[]>(() => [
    { id: "time", header: "时间", cell: ({ row }) => <span className="mono">{timeLabel(row.original.createdAt)}</span>, meta: { width: "12%", wrap: "nowrap" } },
    { id: "actor", header: "账号", cell: ({ row }) => <span title={row.original.actorEmail || row.original.actorId || row.original.actorType}>{row.original.actorEmail || row.original.actorId || row.original.actorType}</span>, meta: { width: "16%", truncate: true } },
    { id: "action", header: "动作", cell: ({ row }) => <span className="mono" title={row.original.action}>{row.original.action}</span>, meta: { width: "17%", truncate: true } },
    { id: "object", header: "对象", cell: ({ row }) => <><span className="pill">{row.original.objectType}</span> <span className="mono" title={row.original.objectId || "-"}>{row.original.objectId || "-"}</span></>, meta: { width: "25%", truncate: true } },
    { id: "result", header: "结果", cell: ({ row }) => <StatusBadge tone={row.original.result === "success" ? "green" : "red"}>{row.original.result}</StatusBadge>, meta: { width: "10%" } },
    { id: "ip", header: "IP", cell: ({ row }) => <span className="mono">{row.original.ip || "-"}</span>, meta: { width: "8%", truncate: true } },
    { id: "detail", header: "详情", cell: ({ row }) => <Button variant="ghost" size="sm" onClick={() => void openDetail(row.original)}>查看</Button>, meta: { width: "12%", align: "end" } }
  ], []);

  const Shell = scope === "console" ? ConsoleShell : AdminShell;
  return (
    <Shell title={scope === "admin" ? "日志中心" : "审计日志"} crumb={scope === "console" ? "/ 工作区 / 审计" : "/ 平台治理 / 日志中心"}>
      {scope === "admin" ? <div className="phase14-section-switch log-center-tabs" role="tablist" aria-label="日志类型">
        <button type="button" role="tab" aria-selected={activeTab === "audit"} aria-controls="audit-log-panel" className={activeTab === "audit" ? "active" : ""} onClick={() => switchLogTab("audit", setActiveTab)}>操作审计</button>
        <button type="button" role="tab" aria-selected={activeTab === "probe"} aria-controls="probe-log-panel" className={activeTab === "probe" ? "active" : ""} onClick={() => switchLogTab("probe", setActiveTab)}>探测日志</button>
      </div> : null}

      {scope === "admin" && activeTab === "probe" ? <div id="probe-log-panel" role="tabpanel">
        <div className="page-intro">逐次查看平台通道的真实探测请求、HTTP 返回代码、中文状态含义和脱敏后的上游错误摘要</div>
        <ProbeLogsPanel />
      </div> : <div id={scope === "admin" ? "audit-log-panel" : undefined} role={scope === "admin" ? "tabpanel" : undefined}>
      <div className="page-intro">{scope === "console" ? "工作区审计只显示当前工作区相关的网关、密钥、私有通道和成员操作" : "平台审计显示 TokHub 全局管理操作、运营配置变更和安全事件"}</div>
      {error ? <div className="form-error">{error}</div> : null}

      <StatGrid items={stats} className="audit-stat-row" />

      <div className="phase14-section-switch audit-quick-bar" aria-label="审计快捷筛选">
        <button type="button" className={appliedFilters.eventClass === "governance" && !appliedFilters.action && !appliedFilters.objectType ? "active" : ""} onClick={() => applyQuickFilter({})}>治理事件</button>
        <button type="button" className={appliedFilters.eventClass === "all" && !appliedFilters.action && !appliedFilters.objectType ? "active" : ""} onClick={() => applyQuickFilter({ eventClass: "all" })}>全部事件</button>
        <button type="button" className={appliedFilters.eventClass === "governance" && appliedFilters.action === "auth.login" ? "active" : ""} onClick={() => applyQuickFilter({ action: "auth.login" })}>登录事件</button>
        <button type="button" className={appliedFilters.eventClass === "governance" && appliedFilters.objectType === "user" ? "active" : ""} onClick={() => applyQuickFilter({ objectType: "user" })}>用户变更</button>
        <button type="button" className={appliedFilters.eventClass === "governance" && appliedFilters.objectType === "channel" ? "active" : ""} onClick={() => applyQuickFilter({ objectType: "channel" })}>通道变更</button>
        <button type="button" className={appliedFilters.eventClass === "governance" && appliedFilters.objectType === "gateway_key" ? "active" : ""} onClick={() => applyQuickFilter({ objectType: "gateway_key" })}>Key 变更</button>
        <button type="button" className={appliedFilters.eventClass === "system" ? "active" : ""} onClick={() => applyQuickFilter({ eventClass: "system" })}>系统探测</button>
      </div>
      <div className="phase14-risk-note audit-risk-note">
        默认展示人工和治理事件，系统探测状态变化已隐藏；需要排查通道抖动时切换到系统探测或全部事件。CSV 导出最多 500 条，并且不包含 metadata 原文。
      </div>

      <FilterBar as="form" className="audit-filter-toolbar" onSubmit={(event) => void submit(event)}>
        <input className="input audit-query" value={filters.query} onChange={(event) => setFilters({ ...filters, query: event.target.value })} placeholder="搜索 action / object / email" />
        <input className="input audit-actor" value={filters.actor} onChange={(event) => setFilters({ ...filters, actor: event.target.value })} placeholder="操作人邮箱 / ID" />
        <SelectField className="audit-select audit-object-type" value={filters.objectType} onChange={(event) => setFilters({ ...filters, objectType: event.target.value })}>
          <option value="">全部对象</option>
          <option value="user">user</option>
          <option value="org">org</option>
          <option value="gateway">gateway</option>
          <option value="gateway_key">gateway_key</option>
          <option value="channel">channel</option>
          <option value="alert_rule">alert_rule</option>
          <option value="notification_channel">notification_channel</option>
          <option value="open_api_site">open_api_site</option>
          <option value="site_config">site_config</option>
          <option value="recommend_config">recommend_config</option>
        </SelectField>
        <input className="input audit-object-id" value={filters.objectId} onChange={(event) => setFilters({ ...filters, objectId: event.target.value })} placeholder="对象 ID" />
        <SelectField className="audit-select audit-result" value={filters.result} onChange={(event) => setFilters({ ...filters, result: event.target.value })}>
          <option value="">全部结果</option>
          <option value="success">success</option>
          <option value="failed">failed</option>
        </SelectField>
        <input className="input audit-date" type="date" value={filters.from} onChange={(event) => setFilters({ ...filters, from: event.target.value })} />
        <input className="input audit-date" type="date" value={filters.to} onChange={(event) => setFilters({ ...filters, to: event.target.value })} />
        <SelectField className="audit-select audit-limit" value={filters.limit} onChange={(event) => setFilters({ ...filters, limit: Number(event.target.value) })}>
          <option value={50}>50 条</option>
          <option value={100}>100 条</option>
          <option value={200}>200 条</option>
          <option value={500}>500 条</option>
        </SelectField>
        <input className="input audit-action" value={filters.action} onChange={(event) => setFilters({ ...filters, action: event.target.value })} placeholder="精确 action" />
        <Button type="submit" variant="primary" size="sm" disabled={loading}>筛选</Button>
        <Button variant="ghost" size="sm" type="button" onClick={() => { setFilters(emptyFilters); void load(emptyFilters); }}>重置</Button>
        <Button variant="ghost" size="sm" type="button" onClick={exportCSV}>导出 CSV</Button>
      </FilterBar>

      <DataTable
        data={result.items}
        columns={auditColumns}
        loading={loading}
        pageSize={10}
        tableClassName="audit-table"
        rowKey={(row) => row.id}
        loadingText="正在加载审计日志..."
        empty={<div className="empty-state"><h4>没有匹配的审计事件</h4></div>}
        footerNote={`审计日志 · ${auditScopeLabel(appliedFilters.eventClass)}共 ${result.total} 条匹配 · 当前加载 ${result.items.length} 条 · CSV 导出不包含 metadata`}
      />

      <AuditDrawer item={selected} loading={detailLoading} onClose={() => setSelected(null)} />
      </div>}
    </Shell>
  );
}

function switchLogTab(tab: "audit" | "probe", setActiveTab: (tab: "audit" | "probe") => void) {
  const params = new URLSearchParams(window.location.search);
  if (tab === "probe") params.set("tab", "probe");
  else params.delete("tab");
  const search = params.toString();
  window.history.replaceState(null, "", `${window.location.pathname}${search ? `?${search}` : ""}${window.location.hash}`);
  setActiveTab(tab);
}

function AuditDrawer({ item, loading, onClose }: { item: AuditLogItem | null; loading: boolean; onClose: () => void }) {
  return (
    <div className={`drawer-mask ${item ? "open" : ""}`} onClick={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <section className={`drawer ${item ? "open" : ""}`} role="dialog" aria-modal="true" aria-labelledby="audit-detail-title" onClick={(event) => event.stopPropagation()}>
        <div className="dh">
          <div>
            <h3 id="audit-detail-title">{item?.action || "审计详情"}</h3>
            <small className="muted-time">{item?.id}</small>
          </div>
          <button className="btn btn-ghost btn-sm" onClick={onClose}>关闭</button>
        </div>
        <div className="db">
          {loading ? <div className="empty-state"><h4>正在加载详情</h4></div> : item ? (
            <div className="card set-card">
              <DetailRow label="时间" value={timeLabel(item.createdAt)} />
              <DetailRow label="账号" value={item.actorEmail || item.actorId || item.actorType} />
              <DetailRow label="动作" value={item.action} />
              <DetailRow label="对象" value={`${item.objectType} / ${item.objectId || "-"}`} />
              <DetailRow label="结果" value={item.result} />
              <DetailRow label="IP" value={item.ip || "-"} />
              <div className="set-row">
                <div className="lbl"><b>Metadata</b><small>已脱敏显示</small></div>
                <pre className="audit-json">{safeJSON(item.metadata)}</pre>
              </div>
            </div>
          ) : null}
        </div>
      </section>
    </div>
  );
}

function DetailRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="set-row">
      <div className="lbl"><b>{label}</b></div>
      <div className="ctl"><span className="mono">{value}</span></div>
    </div>
  );
}

function safeJSON(value: Record<string, unknown>) {
  return JSON.stringify(redact(value), null, 2);
}

function redact(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map(redact);
  }
  if (!value || typeof value !== "object") {
    return value;
  }
  return Object.fromEntries(Object.entries(value as Record<string, unknown>).map(([key, child]) => {
    const lower = key.toLowerCase();
    if (lower.includes("secret") || lower.includes("token") || lower.includes("password") || lower.includes("plain") || lower === "api_key") {
      return [key, "[redacted]"];
    }
    return [key, redact(child)];
  }));
}

function timeLabel(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "-" : date.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function initialAuditFilters(): Filters {
  const params = new URLSearchParams(window.location.search);
  const action = params.get("action") || "";
  return cleanAuditFilters({
    eventClass: initialEventClass(params, action),
    action,
    actor: params.get("actor") || "",
    objectType: params.get("objectType") || "",
    objectId: params.get("objectId") || "",
    result: params.get("result") || "",
    query: params.get("query") || "",
    from: params.get("from") || "",
    to: params.get("to") || "",
    limit: Number(params.get("limit") || emptyFilters.limit)
  });
}

function cleanAuditFilters(filters: Filters): Filters {
  const limit = [50, 100, 200, 500].includes(filters.limit) ? filters.limit : emptyFilters.limit;
  return {
    eventClass: cleanEventClass(filters.eventClass),
    action: filters.action.trim(),
    actor: filters.actor.trim(),
    objectType: filters.objectType.trim(),
    objectId: filters.objectId.trim(),
    result: filters.result === "success" || filters.result === "failed" ? filters.result : "",
    query: filters.query.trim(),
    from: filters.from,
    to: filters.to,
    limit
  };
}

function syncAuditFiltersToURL(filters: Filters) {
  const params = new URLSearchParams(window.location.search);
  for (const key of ["eventClass", "query", "actor", "objectType", "objectId", "result", "from", "to", "limit", "action"]) {
    params.delete(key);
  }
  if (filters.eventClass !== emptyFilters.eventClass) params.set("eventClass", filters.eventClass);
  if (filters.query) params.set("query", filters.query);
  if (filters.actor) params.set("actor", filters.actor);
  if (filters.objectType) params.set("objectType", filters.objectType);
  if (filters.objectId) params.set("objectId", filters.objectId);
  if (filters.result) params.set("result", filters.result);
  if (filters.from) params.set("from", filters.from);
  if (filters.to) params.set("to", filters.to);
  if (filters.limit !== emptyFilters.limit) params.set("limit", String(filters.limit));
  if (filters.action) params.set("action", filters.action);
  const nextSearch = params.toString();
  const nextURL = `${window.location.pathname}${nextSearch ? `?${nextSearch}` : ""}${window.location.hash}`;
  window.history.replaceState(null, "", nextURL);
}

function initialEventClass(params: URLSearchParams, action: string): EventClass {
  const explicit = cleanEventClass(params.get("eventClass") || "");
  if (explicit !== emptyFilters.eventClass || params.get("eventClass") === emptyFilters.eventClass) return explicit;
  return action === "probe.status.changed" ? "system" : emptyFilters.eventClass;
}

function cleanEventClass(value: string): EventClass {
  return value === "system" || value === "all" ? value : "governance";
}

function auditScopeHint(value: EventClass) {
  if (value === "system") return "system probe";
  if (value === "all") return "all events";
  return "governance";
}

function auditScopeLabel(value: EventClass) {
  if (value === "system") return "系统探测口径 · ";
  if (value === "all") return "全部事件口径 · ";
  return "治理事件口径 · ";
}
