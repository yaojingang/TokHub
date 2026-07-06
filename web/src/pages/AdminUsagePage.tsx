import { useEffect, useMemo, useState } from "react";
import { AdminShell } from "../components/AdminShell";
import { ConsoleShell } from "../components/ConsoleShell";
import { adminUsage, consoleUsage, GatewayUsageSummary, recomputeUsageRollup, UsageFilter, usageExportURL } from "../lib/api";
import { Button, DataTable, DataTableColumn, FilterBar, SelectField, StatGrid, StatusBadge } from "../ui";

type UsageRollup = GatewayUsageSummary["rollups"][number];
type UsageEvent = GatewayUsageSummary["recent"][number];

export function AdminUsagePage({ scope = "admin" }: { scope?: "admin" | "console" }) {
  const [usage, setUsage] = useState<GatewayUsageSummary | null>(null);
  const [filter, setFilter] = useState<UsageFilter>(() => initialUsageFilter());
  const [draftFilter, setDraftFilter] = useState<UsageFilter>(() => initialUsageFilter());
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  useEffect(() => {
    void load();
  }, [scope, filter]);

  useEffect(() => {
    syncUsageFilterToURL(filter);
  }, [filter]);

  async function load() {
    setLoading(true);
    setError("");
    const load = scope === "console" ? consoleUsage : adminUsage;
    load(filter)
      .then(setUsage)
      .catch((err) => setError(err instanceof Error ? err.message : "加载用量失败"))
      .finally(() => setLoading(false));
  }

  function applyFilter() {
    setFilter(cleanUsageFilter(draftFilter));
  }

  function resetFilter() {
    const next = { days: "30" };
    setDraftFilter(next);
    setFilter(next);
  }

  async function recompute() {
    if (!window.confirm("确认重新聚合当前环境的 Daily Rollup？该操作会重跑用量汇总，可能影响告警判断和报表数值。")) return;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      await recomputeUsageRollup(scope);
      setNotice("Daily rollup 已重算。");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "重算 rollup 失败");
    } finally {
      setSaving(false);
    }
  }

  const stats = useMemo(() => {
    const totals = usage?.totals;
    const usageDays = cleanUsageFilter(filter).days ?? "30";
    return [
      { label: `近 ${usageDays} 天请求`, value: formatInt(totals?.requests ?? 0), hint: "经专属中转站调用" },
      { label: "Token", value: formatInt(totals?.tokens ?? 0), hint: "输入 + 输出" },
      { label: "成本", value: `$${(totals?.costUsd ?? 0).toFixed(4)}`, hint: "按模型价格估算" },
      { label: "错误率", value: `${(totals?.errorRate ?? 0).toFixed(1)}%`, hint: "4xx / 5xx 请求", tone: (totals?.errorRate ?? 0) > 0 ? "red" as const : "gray" as const },
      { label: "最近事件", value: `${usage?.recent.length ?? 0}`, hint: "请求级 usage event" }
    ];
  }, [filter, usage]);

  const gatewayOptions = useMemo(() => uniqueOptions([
    ...(usage?.gateways ?? []).map((item) => [item.id, item.name] as const),
    ...(usage?.rollups ?? []).filter((item) => item.gatewayId).map((item) => [item.gatewayId, item.gatewayId] as const)
  ]), [usage]);
  const channelOptions = useMemo(() => uniqueOptions([
    ...(usage?.channels ?? []).map((item) => [item.id, item.name] as const),
    ...(usage?.rollups ?? []).filter((item) => item.channelId).map((item) => [item.channelId, item.channelId] as const)
  ]), [usage]);
  const modelOptions = useMemo(() => uniqueOptions((usage?.rollups ?? []).filter((item) => item.model && item.model !== "-").map((item) => [item.model, item.model] as const)), [usage]);
  const memberOptions = useMemo(() => uniqueOptions((usage?.rollups ?? []).filter((item) => item.memberUserId).map((item) => [item.memberUserId, item.memberUserId] as const)), [usage]);
  const rollups = usage?.rollups ?? [];
  const recentEvents = usage?.recent ?? [];
  const rollupColumns = useMemo<DataTableColumn<UsageRollup>[]>(() => [
    { id: "day", header: "日期", cell: ({ row }) => <span className="mono">{row.original.day}</span>, meta: { width: "13%", wrap: "nowrap" } },
    { id: "source", header: "来源", cell: ({ row }) => <StatusBadge tone={row.original.source === "gateway" ? "blue" : "green"}>{row.original.source}</StatusBadge>, meta: { width: "14%" } },
    { id: "model", header: "模型", cell: ({ row }) => <span className="mono" title={row.original.model || "-"}>{row.original.model || "-"}</span>, meta: { width: "22%", truncate: true } },
    { id: "requests", header: "请求", cell: ({ row }) => <span className="mono">{formatInt(row.original.requests)}</span>, meta: { align: "end", width: "10%" } },
    { id: "tokens", header: "Token", cell: ({ row }) => <span className="mono">{formatInt(row.original.tokens)}</span>, meta: { align: "end", width: "10%" } },
    { id: "cost", header: "成本", cell: ({ row }) => <span className="mono">${row.original.costUsd.toFixed(5)}</span>, meta: { align: "end", width: "12%" } },
    { id: "errors", header: "错误", cell: ({ row }) => <span className="mono">{row.original.errors}</span>, meta: { align: "end", width: "9%" } },
    { id: "probe", header: "探测", cell: ({ row }) => <span className="mono">{row.original.probeRuns}</span>, meta: { align: "end", width: "10%" } }
  ], []);
  const recentColumns = useMemo<DataTableColumn<UsageEvent>[]>(() => [
    { id: "time", header: "时间", cell: ({ row }) => <span className="muted-time">{timeLabel(row.original.createdAt)}</span>, meta: { width: "12%", wrap: "nowrap" } },
    { id: "gateway", header: "网关", cell: ({ row }) => <span title={row.original.gateway}>{row.original.gateway}</span>, meta: { width: "14%", truncate: true } },
    { id: "key", header: "Key", cell: ({ row }) => <span title={row.original.keyName}>{row.original.keyName}</span>, meta: { width: "12%", truncate: true } },
    { id: "channel", header: "上游", cell: ({ row }) => <span title={row.original.channel}>{row.original.channel}</span>, meta: { width: "14%", truncate: true } },
    { id: "model", header: "模型", cell: ({ row }) => <span className="mono" title={row.original.model || "-"}>{row.original.model || "-"}</span>, meta: { width: "16%", truncate: true } },
    { id: "status", header: "状态", cell: ({ row }) => <StatusBadge tone={row.original.statusCode >= 400 ? "red" : "green"}>{row.original.statusCode}</StatusBadge>, meta: { width: "8%" } },
    { id: "tokens", header: "Token", cell: ({ row }) => <span className="mono">{row.original.tokens}</span>, meta: { align: "end", width: "8%" } },
    { id: "cost", header: "成本", cell: ({ row }) => <span className="mono">${row.original.costUsd.toFixed(5)}</span>, meta: { align: "end", width: "10%" } },
    { id: "metering", header: "计量", cell: ({ row }) => <StatusBadge tone={row.original.estimated ? "amber" : "blue"}>{row.original.estimated ? "估算" : "真实"}</StatusBadge>, meta: { width: "8%" } },
    { id: "latency", header: "延迟", cell: ({ row }) => <span className="mono">{row.original.latencyMs}ms{row.original.stream ? " · SSE" : ""}</span>, meta: { width: "10%", truncate: true } }
  ], []);

  const Shell = scope === "console" ? ConsoleShell : AdminShell;
  const intro = scope === "console"
    ? "当前工作区经专属中转站的调用在此统一计量。成本按上游通道单价估算，daily rollup 可重跑并用于告警判断"
    : "平台全局经专属中转站的调用在此统一计量。这里用于平台 owner 查看跨工作区总量，工作区成员只看 /console/usage";

  return (
    <Shell title="用量数据" crumb={scope === "console" ? "/ 工作区 / 用量" : "/ 平台治理 / 用量"}>
      <p className="page-intro">{intro}</p>
      {error ? <div className="form-error">{error}</div> : null}
      {notice ? <div className="form-notice">{notice}</div> : null}

      <StatGrid items={stats} className="usage-summary-stats" />

      <FilterBar className="usage-filter-toolbar">
        <SelectField value={draftFilter.days ?? "30"} onChange={(event) => setDraftFilter({ ...draftFilter, days: event.target.value })} aria-label="用量时间范围" className="usage-days-filter">
          <option value="7">近 7 天</option>
          <option value="30">近 30 天</option>
          <option value="90">近 90 天</option>
        </SelectField>
        <SelectField value={draftFilter.source ?? ""} onChange={(event) => setDraftFilter({ ...draftFilter, source: event.target.value as UsageFilter["source"] })} aria-label="来源筛选" className="usage-source-filter">
          <option value="">来源：全部</option>
          <option value="gateway">来源：网关调用</option>
          <option value="probe">来源：探测任务</option>
        </SelectField>
        <SelectField value={draftFilter.gatewayId ?? ""} onChange={(event) => setDraftFilter({ ...draftFilter, gatewayId: event.target.value })} aria-label="网关筛选" className="usage-gateway-filter">
          <option value="">网关：全部</option>
          {gatewayOptions.map((item) => <option key={item.id} value={item.id}>{item.label}</option>)}
        </SelectField>
        <SelectField value={draftFilter.channelId ?? ""} onChange={(event) => setDraftFilter({ ...draftFilter, channelId: event.target.value })} aria-label="通道筛选" className="usage-channel-filter">
          <option value="">通道：全部</option>
          {channelOptions.map((item) => <option key={item.id} value={item.id}>{item.label}</option>)}
        </SelectField>
        <SelectField value={draftFilter.model ?? ""} onChange={(event) => setDraftFilter({ ...draftFilter, model: event.target.value })} aria-label="模型筛选" className="usage-model-filter">
          <option value="">模型：全部</option>
          {modelOptions.map((item) => <option key={item.id} value={item.id}>{item.label}</option>)}
        </SelectField>
        <div className="tb-search usage-member-filter">
          <span>⚿</span>
          <input
            value={draftFilter.memberUserId ?? ""}
            onChange={(event) => setDraftFilter({ ...draftFilter, memberUserId: event.target.value })}
            placeholder="成员 User ID"
            list="usage-member-options"
          />
          <datalist id="usage-member-options">
            {memberOptions.map((item) => <option key={item.id} value={item.id}>{item.label}</option>)}
          </datalist>
        </div>
        <div className="usage-filter-actions">
          <Button variant="primary" size="sm" onClick={applyFilter} disabled={loading}>应用筛选</Button>
          <Button variant="ghost" size="sm" onClick={resetFilter} disabled={loading}>重置</Button>
          <a
            className="btn btn-ghost btn-sm"
            href={usageExportURL(scope, filter)}
            onClick={(event) => {
              if (!window.confirm("确认导出当前筛选范围的用量 CSV？导出内容不包含密钥明文，但仍属于运营数据。")) event.preventDefault();
            }}
          >
            导出 CSV
          </a>
        </div>
      </FilterBar>
      <div className="phase14-risk-note usage-risk-note">
        用量页默认按当前筛选读取真实 usage event 和 rollup；重新聚合用于修复统计，不等同于刷新页面。
      </div>

      <div className="grid usage-top-grid">
        <div className="card">
          <div className="chart-head"><span className="ct">用量数据趋势 · 近 30 天</span><span className="badge b-green dot">实时</span></div>
          <div className="chart-wrap">
            {loading ? <div className="empty-state"><h4>正在加载趋势</h4></div> : <UsageChart points={usage?.trend ?? []} />}
          </div>
        </div>
        <div className="card card-pad">
          <div className="section-head" style={{ margin: "0 0 14px" }}><h2 style={{ fontSize: 15 }}>预算额度</h2><span className="sub">当前筛选</span></div>
          <QuotaBox requests={usage?.totals.requests ?? 0} quotaMonth={usage?.quotaMonth ?? 0} />
        </div>
      </div>

      <div className="grid usage-break-grid">
        <Breakdown title="按网关拆分" rows={usage?.gateways ?? []} tone="var(--brand)" />
        <Breakdown title="按模型拆分" rows={usage?.models ?? []} tone="var(--green)" />
        <Breakdown title="按上游通道拆分" rows={usage?.channels ?? []} tone="var(--amber)" />
      </div>

      <div className="section-head">
        <h2>Daily Rollup <span className="tag">{usage?.rollups.length ?? 0}</span></h2>
        <Button variant="ghost" size="sm" onClick={recompute} disabled={saving || loading} title="重新聚合会重跑汇总并影响报表数值">
          {saving ? "聚合中..." : "重新聚合"}
        </Button>
      </div>
      <DataTable
        data={rollups}
        columns={rollupColumns}
        loading={loading}
        pageSize={10}
        tableClassName="usage-rollup-table"
        rowKey={(row) => `${row.day}-${row.source}-${row.gatewayId}-${row.channelId}-${row.model}-${row.memberUserId}`}
        loadingText="正在加载 rollup..."
        empty={<div className="empty-state"><h4>暂无 rollup</h4><p>产生网关调用或探测记录后会自动聚合。</p></div>}
        footerNote="Daily Rollup"
      />

      <div className="section-head"><h2>请求记录</h2><span className="sub">当前筛选下最近 {recentEvents.length} 条 Gateway usage event</span></div>
      <DataTable
        data={recentEvents}
        columns={recentColumns}
        loading={loading}
        pageSize={10}
        tableClassName="usage-event-table"
        rowKey={(row) => row.id}
        loadingText="正在加载请求记录..."
        empty={<div className="empty-state"><div className="ico">$</div><h4>还没有网关用量</h4><p>创建网关和 Key 后，调用 `/gateway/v1/chat/completions` 或 `/gateway/v1/responses` 会在这里出现。</p></div>}
        footerNote="usage event"
      />
    </Shell>
  );
}

function cleanUsageFilter(filter: UsageFilter): UsageFilter {
  return {
    days: normalizeUsageDays(filter.days),
    source: filter.source || undefined,
    gatewayId: filter.gatewayId?.trim() || undefined,
    channelId: filter.channelId?.trim() || undefined,
    model: filter.model?.trim() || undefined,
    memberUserId: filter.memberUserId?.trim() || undefined
  };
}

function initialUsageFilter(): UsageFilter {
  const params = new URLSearchParams(window.location.search);
  return cleanUsageFilter({
    days: params.get("days") || "30",
    source: normalizeUsageSource(params.get("source")),
    gatewayId: params.get("gatewayId") || undefined,
    channelId: params.get("channelId") || undefined,
    model: params.get("model") || undefined,
    memberUserId: params.get("memberUserId") || undefined
  });
}

function syncUsageFilterToURL(filter: UsageFilter) {
  const cleaned = cleanUsageFilter(filter);
  const params = new URLSearchParams(window.location.search);
  for (const key of ["days", "source", "gatewayId", "channelId", "model", "memberUserId"]) {
    params.delete(key);
  }
  if (cleaned.days && cleaned.days !== "30") params.set("days", cleaned.days);
  if (cleaned.source) params.set("source", cleaned.source);
  if (cleaned.gatewayId) params.set("gatewayId", cleaned.gatewayId);
  if (cleaned.channelId) params.set("channelId", cleaned.channelId);
  if (cleaned.model) params.set("model", cleaned.model);
  if (cleaned.memberUserId) params.set("memberUserId", cleaned.memberUserId);
  const nextSearch = params.toString();
  const nextURL = `${window.location.pathname}${nextSearch ? `?${nextSearch}` : ""}${window.location.hash}`;
  window.history.replaceState(null, "", nextURL);
}

function normalizeUsageDays(value?: string) {
  if (value === "7" || value === "90") return value;
  return "30";
}

function normalizeUsageSource(value: string | null): UsageFilter["source"] | undefined {
  if (value === "gateway" || value === "probe") return value;
  return undefined;
}

function uniqueOptions(items: ReadonlyArray<readonly [string, string]>) {
  const seen = new Set<string>();
  return items.filter(([id]) => {
    if (!id || seen.has(id)) return false;
    seen.add(id);
    return true;
  }).map(([id, label]) => ({ id, label: label || id }));
}

function UsageChart({ points }: { points: Array<{ date: string; requests: number; costUsd: number }> }) {
  const values = points.map((point) => point.requests);
  const max = Math.max(...values, 1);
  const min = Math.min(...values, 0);
  const width = 1000;
  const height = 200;
  const path = values.map((value, index) => {
    const x = 8 + (984 * index) / Math.max(values.length - 1, 1);
    const y = 170 - ((value - min) / Math.max(max - min, 1)) * 140;
    return `${index ? "L" : "M"}${x.toFixed(1)},${y.toFixed(1)}`;
  }).join(" ");
  if (!values.some(Boolean)) {
    return <div className="empty-state"><h4>暂无趋势数据</h4><p>完成一次网关调用后趋势图会自动更新。</p></div>;
  }
  return (
    <svg viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none" style={{ height: 200, width: "100%" }}>
      <g className="chart-grid">
        <line x1="8" y1="30" x2="992" y2="30" />
        <line x1="8" y1="100" x2="992" y2="100" />
        <line x1="8" y1="170" x2="992" y2="170" />
      </g>
      <path d={`${path} L992,170 L8,170 Z`} fill="rgba(37,99,235,.12)" />
      <path d={path} fill="none" stroke="var(--brand)" strokeWidth="2.2" strokeLinejoin="round" />
    </svg>
  );
}

function QuotaBox({ requests, quotaMonth }: { requests: number; quotaMonth: number }) {
  const quota = Math.max(0, quotaMonth);
  if (!quota) {
    return (
      <div>
        <div style={{ display: "flex", alignItems: "baseline", gap: 8 }}>
          <b style={{ fontSize: 30 }}>--</b>
          <span className="muted-time">{formatInt(requests)} 请求 · 未返回额度</span>
        </div>
        <div className="prog" style={{ marginTop: 14, width: "100%" }}><i style={{ width: "0%" }} /></div>
        <div className="module-sub" style={{ marginTop: 14 }}>当前筛选范围没有可计算的 gateway/key 月度额度，不再使用固定额度估算。</div>
      </div>
    );
  }
  const pct = Math.min(Math.round((requests / quota) * 100), 100);
  return (
    <div>
      <div style={{ display: "flex", alignItems: "baseline", gap: 8 }}>
        <b style={{ fontSize: 30 }}>{pct}%</b>
        <span className="muted-time">{formatInt(requests)} / {formatInt(quota)} 请求</span>
      </div>
      <div className={`prog ${pct >= 90 ? "red" : pct >= 70 ? "warn" : ""}`} style={{ marginTop: 14, width: "100%" }}><i style={{ width: `${pct}%` }} /></div>
      <div className="module-sub" style={{ marginTop: 14 }}>额度来自当前筛选范围内的 gateway/key 月度限额，成本阈值和告警恢复通知已接入治理中心。</div>
    </div>
  );
}

function Breakdown({ title, rows, tone }: { title: string; rows: Array<{ id: string; name: string; requests: number; costUsd: number; errorRate: number }>; tone: string }) {
  const max = Math.max(...rows.map((row) => row.requests), 1);
  return (
    <div className="card card-pad">
      <div className="section-head" style={{ margin: "0 0 16px" }}><h2 style={{ fontSize: 15 }}>{title}</h2></div>
      <div className="break">
        {rows.length ? rows.map((row) => (
          <div className="br" key={row.id || row.name}>
            <span className="bn"><span className="d" style={{ background: tone }} />{row.name || "-"}</span>
            <span className="bb"><i style={{ width: `${Math.round((row.requests / max) * 100)}%`, background: tone }} /></span>
            <span className="bv">{formatInt(row.requests)}<small>${row.costUsd.toFixed(4)} · err {row.errorRate.toFixed(1)}%</small></span>
          </div>
        )) : <div className="empty-state"><h4>暂无数据</h4><p>网关调用后展示拆分。</p></div>}
      </div>
    </div>
  );
}

function formatInt(value: number) {
  if (value >= 1000000) return `${(value / 1000000).toFixed(1)}M`;
  if (value >= 1000) return `${(value / 1000).toFixed(1)}K`;
  return String(value);
}

function timeLabel(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "-" : date.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}
