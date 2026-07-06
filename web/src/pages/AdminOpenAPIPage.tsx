import { useEffect, useMemo, useState } from "react";
import { AdminShell } from "../components/AdminShell";
import {
  adminChannelSites,
  adminOpenAPI,
  buildChannelSitePackage,
  bulkOpenAPISites,
  ChannelSite,
  ChannelSiteInput,
  ChannelSiteRuntimeLog,
  ChannelSitesSummary,
  createChannelSite,
  createOpenAPISite,
  deleteChannelSite,
  deleteOpenAPISite,
  downloadChannelSitePackage,
  OpenAPICallLog,
  OpenAPISite,
  OpenAPISummary,
  revokeOpenAPISite,
  rotateChannelSiteKey,
  updateChannelSite,
  updateOpenAPISite
} from "../lib/api";
import { Button, CheckboxField, CopyButton, DataTable, FilterBar, SelectField, StatusBadge, type DataTableColumn } from "../ui";

const scopes = [
  { id: "overview", label: "总览", path: "/v1/status/overview" },
  { id: "channels", label: "通道", path: "/v1/status/channels" },
  { id: "uptime", label: "可用率", path: "/v1/status/uptime" },
  { id: "incidents", label: "事件", path: "/v1/status/incidents" },
  { id: "channel_sync", label: "通道同步", path: "/v1/status/channel-sync" }
];

const defaultOpenAPIScopes = scopes.filter((scope) => scope.id !== "channel_sync").map((scope) => scope.id);

const moduleOptions: Array<{ key: keyof ChannelSiteInput["modules"]; label: string; hint: string }> = [
  { key: "overview", label: "监控总览", hint: "首页 KPI 和状态摘要" },
  { key: "channelBoard", label: "通道明细", hint: "公开通道实时列表" },
  { key: "recommend", label: "精选推荐", hint: "复用后台推荐配置" },
  { key: "providerRank", label: "供应商排行", hint: "成功率和评分聚合" },
  { key: "strategy", label: "监控策略", hint: "解释 L1/L2/L3 逻辑" }
];

type OpenAPIFilters = {
  q: string;
  status: string;
  scope: string;
};

type OpenAPISection = "open-api" | "channel-sites";

type ChannelSiteImportRow = {
  rowNumber: number;
  input: ChannelSiteInput;
  errors: string[];
};

type ChannelSiteImportResult = {
  rowNumber: number;
  name: string;
  domain: string;
  status: "success" | "failed";
  message: string;
  fileName?: string;
};

export function AdminOpenAPIPage() {
  const [section, setSection] = useState<OpenAPISection>(() => initialSection());
  const [data, setData] = useState<OpenAPISummary | null>(null);
  const [channelSites, setChannelSites] = useState<ChannelSitesSummary | null>(null);
  const [name, setName] = useState("官网状态页");
  const [qps, setQps] = useState(30);
  const [pickedScopes, setPickedScopes] = useState<Set<string>>(new Set(defaultOpenAPIScopes));
  const [createdSite, setCreatedSite] = useState<OpenAPISite | null>(null);
  const [query, setQuery] = useState(() => initialOpenAPIFilter("q", ""));
  const [status, setStatus] = useState(() => initialOpenAPIFilter("status", "all"));
  const [scopeFilter, setScopeFilter] = useState(() => initialOpenAPIFilter("scope", "all"));
  const [selected, setSelected] = useState<string[]>([]);
  const [bulkStatus, setBulkStatus] = useState("paused");
  const [channelForm, setChannelForm] = useState<ChannelSiteInput>(() => channelSiteDefaults());
  const [editingChannelSiteID, setEditingChannelSiteID] = useState("");
  const [channelSiteKey, setChannelSiteKey] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [savingSiteID, setSavingSiteID] = useState("");
  const [importingChannelSites, setImportingChannelSites] = useState(false);
  const [channelImportFileName, setChannelImportFileName] = useState("");
  const [channelImportRows, setChannelImportRows] = useState<ChannelSiteImportRow[]>([]);
  const [channelImportResults, setChannelImportResults] = useState<ChannelSiteImportResult[]>([]);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  useEffect(() => {
    void loadAll();
  }, []);

  useEffect(() => {
    syncOpenAPIFiltersToURL({ q: query.trim(), status, scope: scopeFilter }, section);
  }, [query, status, scopeFilter, section]);

  async function loadAll() {
    setLoading(true);
    setError("");
    try {
      const [openAPI, sites] = await Promise.all([adminOpenAPI(), adminChannelSites()]);
      setData(openAPI);
      setChannelSites(sites);
      setSelected((current) => current.filter((id) => openAPI.sites.some((site) => site.id === id)));
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载开放能力失败");
    } finally {
      setLoading(false);
    }
  }

  async function reloadOpenAPI() {
    const payload = await adminOpenAPI();
    setData(payload);
    setSelected((current) => current.filter((id) => payload.sites.some((site) => site.id === id)));
  }

  async function reloadChannelSites() {
    const payload = await adminChannelSites();
    setChannelSites(payload);
  }

  function clearFilters() {
    setQuery("");
    setStatus("all");
    setScopeFilter("all");
    setSelected([]);
    syncOpenAPIFiltersToURL({ q: "", status: "all", scope: "all" }, section);
  }

  function toggleScope(scope: string) {
    setPickedScopes((current) => {
      const next = new Set(current);
      next.has(scope) ? next.delete(scope) : next.add(scope);
      return next;
    });
  }

  async function createSite() {
    if (!name.trim()) {
      setError("请输入授权站点名称");
      return;
    }
    if (!pickedScopes.size) {
      setError("至少选择一个可访问端点");
      return;
    }
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const payload = await createOpenAPISite({ name: name.trim(), scopes: Array.from(pickedScopes), qpsLimit: qps });
      setCreatedSite(payload.site);
      setNotice("Site Key 已创建，只会完整展示一次。");
      await reloadOpenAPI();
    } catch (err) {
      setError(err instanceof Error ? err.message : "创建授权站点失败");
    } finally {
      setSaving(false);
    }
  }

  async function patchSite(site: OpenAPISite, patch: Partial<OpenAPISite>) {
    setSavingSiteID(site.id);
    setError("");
    setNotice("");
    try {
      const payload = await updateOpenAPISite(site.id, {
        name: patch.name ?? site.name,
        scopes: patch.scopes ?? site.scopes,
        qpsLimit: patch.qpsLimit ?? site.qpsLimit,
        status: patch.status ?? site.status
      });
      setData((current) => current ? { ...current, sites: current.sites.map((item) => item.id === site.id ? { ...item, ...payload.site, callsToday: item.callsToday } : item) } : current);
      setNotice("授权站点已更新。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "更新授权站点失败");
    } finally {
      setSavingSiteID("");
    }
  }

  function patchSiteLocal(siteID: string, patch: Partial<OpenAPISite>) {
    setData((current) => current ? { ...current, sites: current.sites.map((item) => item.id === siteID ? { ...item, ...patch } : item) } : current);
  }

  async function revokeSite(site: OpenAPISite) {
    if (!window.confirm(`确认吊销 ${site.name}？吊销后该 Site Key 会立即失效。`)) return;
    setSavingSiteID(site.id);
    setError("");
    setNotice("");
    try {
      const payload = await revokeOpenAPISite(site.id);
      setData((current) => current ? { ...current, sites: current.sites.map((item) => item.id === site.id ? { ...item, ...payload.site, callsToday: item.callsToday } : item) } : current);
      setNotice("Site Key 已吊销。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "吊销授权站点失败");
    } finally {
      setSavingSiteID("");
    }
  }

  async function deleteSite(site: OpenAPISite) {
    if (!window.confirm(`确认删除 ${site.name}？Site Key 会立即失效，调用日志和审计上下文会保留。`)) return;
    setSavingSiteID(site.id);
    setError("");
    setNotice("");
    try {
      await deleteOpenAPISite(site.id);
      await reloadOpenAPI();
      setSelected((current) => current.filter((id) => id !== site.id));
      setNotice("授权站点已删除。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "删除授权站点失败");
    } finally {
      setSavingSiteID("");
    }
  }

  async function runBulk(action: "status" | "revoke" | "delete") {
    if (!selected.length) return;
    const label = action === "delete" ? "删除所选授权站点" : action === "revoke" ? "吊销所选授权站点" : `将所选授权站点改为 ${bulkStatus}`;
    if (!window.confirm(`确认${label}？操作会写入审计。`)) return;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const payload = await bulkOpenAPISites({ action, ids: selected, status: bulkStatus });
      setData((current) => current ? { ...current, sites: payload.sites } : current);
      setSelected([]);
      setNotice(action === "delete" ? "批量删除完成。" : action === "revoke" ? "批量吊销完成。" : "批量状态更新完成。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "批量操作失败");
    } finally {
      setSaving(false);
    }
  }

  function toggleSiteScope(site: OpenAPISite, scope: string) {
    const next = new Set(site.scopes);
    next.has(scope) ? next.delete(scope) : next.add(scope);
    const scopesNext = Array.from(next);
    if (!scopesNext.length) {
      setError("授权站点至少保留一个 scope");
      return;
    }
    void patchSite(site, { scopes: scopesNext });
  }

  function patchChannelForm(patch: Partial<ChannelSiteInput>) {
    setChannelForm((current) => ({ ...current, ...patch }));
  }

  function patchChannelModules(key: keyof ChannelSiteInput["modules"], checked: boolean) {
    setChannelForm((current) => ({ ...current, modules: { ...current.modules, [key]: checked } }));
  }

  function patchChannelCopy(patch: Partial<ChannelSiteInput["copy"]>) {
    setChannelForm((current) => ({ ...current, copy: { ...current.copy, ...patch } }));
  }

  function patchChannelSEO(patch: Partial<ChannelSiteInput["seo"]>) {
    setChannelForm((current) => ({ ...current, seo: { ...current.seo, ...patch } }));
  }

  function patchChannelNav(index: number, patch: Partial<ChannelSiteInput["navItems"][number]>) {
    setChannelForm((current) => ({
      ...current,
      navItems: current.navItems.map((item, itemIndex) => itemIndex === index ? { ...item, ...patch } : item)
    }));
  }

  function addChannelNav() {
    setChannelForm((current) => {
      if (current.navItems.length >= 3) return current;
      return {
        ...current,
        navItems: [...current.navItems, { label: "", href: "", position: current.navItems.length }]
      };
    });
  }

  function removeChannelNav(index: number) {
    setChannelForm((current) => ({
      ...current,
      navItems: current.navItems.filter((_, itemIndex) => itemIndex !== index).map((item, itemIndex) => ({ ...item, position: itemIndex }))
    }));
  }

  function editChannelSite(site: ChannelSite) {
    setEditingChannelSiteID(site.id);
    setChannelForm(channelSiteToInput(site));
    setChannelSiteKey("");
    setNotice(`正在编辑渠道站点：${site.name}`);
    window.scrollTo({ top: 0, behavior: "smooth" });
  }

  function resetChannelSiteForm() {
    setEditingChannelSiteID("");
    setChannelForm(channelSiteDefaults());
    setChannelSiteKey("");
  }

  async function saveChannelSite() {
    const input = normalizeChannelSiteInput(channelForm);
    if (!input.name || !input.domain || !input.publicUrl) {
      setError("请填写站点名称、域名和站点地址");
      return;
    }
    setSaving(true);
    setError("");
    setNotice("");
    setChannelSiteKey("");
    try {
      const payload = editingChannelSiteID ? await updateChannelSite(editingChannelSiteID, input) : await createChannelSite(input);
      if (payload.site.plainRuntimeKey) {
        setChannelSiteKey(payload.site.plainRuntimeKey);
      }
      await reloadChannelSites();
      setNotice(editingChannelSiteID ? "渠道站点配置已保存" : "渠道站点已创建，runtime key 只会完整展示一次");
      if (!editingChannelSiteID) resetChannelSiteForm();
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存渠道站点失败");
    } finally {
      setSaving(false);
    }
  }

  async function rotateChannelKey(site: ChannelSite) {
    if (!window.confirm(`确认轮换 ${site.name} 的 runtime key？旧站点包里的 key 会立即失效。`)) return;
    setSavingSiteID(site.id);
    setError("");
    setNotice("");
    try {
      const payload = await rotateChannelSiteKey(site.id);
      setChannelSiteKey(payload.site.plainRuntimeKey || "");
      await reloadChannelSites();
      setNotice("runtime key 已轮换，只会完整展示一次，请重新生成站点包");
    } catch (err) {
      setError(err instanceof Error ? err.message : "轮换 runtime key 失败");
    } finally {
      setSavingSiteID("");
    }
  }

  async function buildPackage(site: ChannelSite) {
    setSavingSiteID(site.id);
    setError("");
    setNotice("");
    try {
      const payload = await buildChannelSitePackage(site.id);
      setChannelSiteKey(payload.site.plainRuntimeKey || "");
      await reloadChannelSites();
      setNotice(`站点包已生成：${payload.export.fileName}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "生成站点包失败");
    } finally {
      setSavingSiteID("");
    }
  }

  async function downloadPackage(site: ChannelSite) {
    setSavingSiteID(site.id);
    setError("");
    setNotice("");
    try {
      const payload = await downloadChannelSitePackage(site.id);
      downloadBlob(payload.blob, payload.filename);
      await reloadChannelSites();
      setNotice("站点包已下载。下载时会写入新的 runtime key，旧包会失效，请部署最新 zip");
    } catch (err) {
      setError(err instanceof Error ? err.message : "下载站点包失败");
    } finally {
      setSavingSiteID("");
    }
  }

  function downloadChannelSiteTemplate() {
    downloadBlob(buildChannelSiteTemplateBlob(), "tokhub-channel-site-import-template.csv");
    setNotice("已下载渠道站点导入模板，Excel 可直接打开编辑，保存为 CSV 或 XLSX 后上传");
  }

  async function handleChannelSiteImportFile(file: File) {
    setError("");
    setNotice("");
    setChannelImportResults([]);
    setChannelImportFileName(file.name);
    try {
      const records = await readChannelSiteImportRecords(file);
      const rows = records.map((record, index) => buildChannelSiteImportRow(record, index + 2));
      setChannelImportRows(rows);
      const validCount = rows.filter((row) => !row.errors.length).length;
      const errorCount = rows.length - validCount;
      setNotice(`已解析 ${rows.length} 行，${validCount} 行可导入${errorCount ? `，${errorCount} 行需要修正` : ""}`);
    } catch (err) {
      setChannelImportRows([]);
      setError(err instanceof Error ? err.message : "解析导入文件失败");
    }
  }

  async function runChannelSiteBatchImport() {
    const validRows = channelImportRows.filter((row) => !row.errors.length);
    if (!validRows.length) {
      setError("没有可导入的有效站点，请先上传并修正模板内容");
      return;
    }
    setImportingChannelSites(true);
    setError("");
    setNotice("");
    setChannelSiteKey("");
    const results: ChannelSiteImportResult[] = [];
    try {
      for (const row of validRows) {
        try {
          const created = await createChannelSite(row.input);
          const downloaded = await downloadChannelSitePackage(created.site.id);
          downloadBlob(downloaded.blob, downloaded.filename);
          results.push({
            rowNumber: row.rowNumber,
            name: row.input.name,
            domain: row.input.domain,
            status: "success",
            message: "已创建、生成并下载",
            fileName: downloaded.filename
          });
        } catch (err) {
          results.push({
            rowNumber: row.rowNumber,
            name: row.input.name,
            domain: row.input.domain,
            status: "failed",
            message: err instanceof Error ? err.message : "导入失败"
          });
        }
      }
      setChannelImportResults(results);
      await reloadChannelSites();
      const successCount = results.filter((item) => item.status === "success").length;
      const failedCount = results.length - successCount;
      const successfulRowNumbers = new Set(results.filter((item) => item.status === "success").map((item) => item.rowNumber));
      setChannelImportRows((rows) => rows.filter((row) => !successfulRowNumbers.has(row.rowNumber)));
      setNotice(`批量导入完成：成功 ${successCount} 个，失败 ${failedCount} 个`);
    } finally {
      setImportingChannelSites(false);
    }
  }

  async function removeChannelSite(site: ChannelSite) {
    if (!window.confirm(`确认删除渠道站点 ${site.name}？已下载的站点包将无法继续读取 runtime API。`)) return;
    setSavingSiteID(site.id);
    setError("");
    setNotice("");
    try {
      await deleteChannelSite(site.id);
      await reloadChannelSites();
      if (editingChannelSiteID === site.id) resetChannelSiteForm();
      setNotice("渠道站点已删除");
    } catch (err) {
      setError(err instanceof Error ? err.message : "删除渠道站点失败");
    } finally {
      setSavingSiteID("");
    }
  }

  const stats = useMemo<Array<[string, string | number, string]>>(() => [
    ["授权站点", data?.stats.sites ?? 0, `${data?.stats.active ?? 0} 个启用`],
    ["开放端点", data?.stats.endpoints ?? 0, "正式路径 /v1/status/*"],
    ["今日调用", data?.stats.callsToday ?? 0, "含限流和越权记录"],
    ["渠道站点", channelSites?.stats.sites ?? 0, `${channelSites?.stats.active ?? 0} 个启用`],
    ["站点包", channelSites?.stats.exports ?? 0, "可下载部署"]
  ], [data, channelSites]);

  const channelSiteStats = useMemo<Array<[string, string | number, string]>>(() => [
    ["渠道站点", channelSites?.stats.sites ?? 0, `${channelSites?.stats.active ?? 0} 个启用`],
    ["今日回源", channelSites?.stats.callsToday ?? 0, "runtime API 请求"],
    ["站点包", channelSites?.stats.exports ?? 0, "已生成记录"],
    ["运行时 Base", channelSites?.baseUrl ? 1 : 0, channelSites?.baseUrl || "/site/v1"],
    ["菜单上限", 3, "每个站点最多 3 个自定义链接"]
  ], [channelSites]);

  const filteredSites = useMemo(() => {
    const keyword = query.trim().toLowerCase();
    return (data?.sites ?? []).filter((site) => {
      if (status !== "all" && site.status !== status) return false;
      if (scopeFilter !== "all" && !site.scopes.includes(scopeFilter)) return false;
      if (!keyword) return true;
      return `${site.name} ${site.siteKeyPrefix} ${site.siteKeyMask} ${site.scopes.join(" ")}`.toLowerCase().includes(keyword);
    });
  }, [data, query, status, scopeFilter]);

  const selectedSet = useMemo(() => new Set(selected), [selected]);
  const allVisibleSelected = filteredSites.length > 0 && filteredSites.every((site) => selectedSet.has(site.id));

  const endpointColumns = useMemo<DataTableColumn<OpenAPISummary["endpoints"][number]>[]>(() => [
    { accessorKey: "path", header: "端点", cell: ({ row }) => <span className="mono">{row.original.method} {row.original.path}</span>, meta: { truncate: true } },
    { accessorKey: "scope", header: "Scope", cell: ({ row }) => <StatusBadge tone="blue">{row.original.scope}</StatusBadge>, meta: { width: "120px", wrap: "nowrap" } },
    { accessorKey: "description", header: "说明", cell: ({ row }) => row.original.description, meta: { truncate: true } },
    { accessorKey: "callsToday", header: "今日调用", cell: ({ row }) => formatInt(row.original.callsToday), meta: { width: "100px", align: "end", className: "mono", wrap: "nowrap" } },
    { accessorKey: "averageMs", header: "平均延迟", cell: ({ row }) => `${row.original.averageMs}ms`, meta: { width: "100px", align: "end", className: "mono", wrap: "nowrap" } },
    { accessorKey: "cache", header: "缓存", cell: ({ row }) => row.original.cache, meta: { width: "92px", wrap: "nowrap" } }
  ], []);

  const logColumns = useMemo<DataTableColumn<OpenAPICallLog>[]>(() => [
    { accessorKey: "createdAt", header: "时间", cell: ({ row }) => <span className="muted-time">{timeLabel(row.original.createdAt)}</span>, meta: { width: "128px", wrap: "nowrap" } },
    { accessorKey: "siteName", header: "站点", cell: ({ row }) => row.original.siteName || "-", meta: { width: "190px", truncate: true } },
    { accessorKey: "endpoint", header: "端点", cell: ({ row }) => row.original.endpoint, meta: { className: "mono", truncate: true } },
    { accessorKey: "statusCode", header: "状态码", cell: ({ row }) => <StatusBadge tone={row.original.statusCode >= 400 ? "red" : "green"}>{row.original.statusCode}</StatusBadge>, meta: { width: "94px", align: "center", wrap: "nowrap" } },
    { accessorKey: "latencyMs", header: "耗时", cell: ({ row }) => `${row.original.latencyMs}ms`, meta: { width: "88px", className: "mono", align: "end", wrap: "nowrap" } },
    { accessorKey: "ip", header: "来源 IP", cell: ({ row }) => row.original.ip || "-", meta: { width: "150px", className: "mono", truncate: true } }
  ], []);

  const channelRuntimeLogColumns = useMemo<DataTableColumn<ChannelSiteRuntimeLog>[]>(() => [
    { accessorKey: "createdAt", header: "时间", cell: ({ row }) => <span className="muted-time">{timeLabel(row.original.createdAt)}</span>, meta: { width: "128px", wrap: "nowrap" } },
    { accessorKey: "siteName", header: "渠道站点", cell: ({ row }) => row.original.siteName || "-", meta: { width: "180px", truncate: true } },
    { accessorKey: "endpoint", header: "Runtime 端点", cell: ({ row }) => <span className="mono">{row.original.endpoint}</span>, meta: { truncate: true } },
    { accessorKey: "statusCode", header: "状态码", cell: ({ row }) => <StatusBadge tone={row.original.statusCode >= 400 ? "red" : "green"}>{row.original.statusCode}</StatusBadge>, meta: { width: "94px", align: "center", wrap: "nowrap" } },
    { accessorKey: "latencyMs", header: "耗时", cell: ({ row }) => `${row.original.latencyMs}ms`, meta: { width: "88px", className: "mono", align: "end", wrap: "nowrap" } },
    { accessorKey: "origin", header: "Origin", cell: ({ row }) => row.original.origin || "-", meta: { width: "220px", truncate: true } },
    { accessorKey: "ip", header: "来源 IP", cell: ({ row }) => row.original.ip || "-", meta: { width: "150px", className: "mono", truncate: true } }
  ], []);

  return (
    <AdminShell title="开放 API" crumb="/ 对外数据接口">
      <div className="page-intro-row">
        <p className="page-intro open-api-intro">把 TokHub 监控数据以只读 Open API 对外开放，也可以生成渠道站点包。站点包部署后通过独立 runtime key 回源读取当前系统数据</p>
      </div>
      {error ? <div className="form-error">{error}</div> : null}
      {notice ? <div className="form-notice">{notice}</div> : null}

      <div className="stat-row">
        {stats.map(([label, value, hint]) => (
          <div className="stat" key={label}>
            <div className="l">{label}</div>
            <div className="v">{formatStat(value)}</div>
            <div className="d">{hint}</div>
          </div>
        ))}
      </div>

      <div className="open-api-tabs" role="tablist" aria-label="开放能力类型">
        <Button className={section === "open-api" ? "open-api-tab active" : "open-api-tab"} onClick={() => setSection("open-api")}>只读 Open API</Button>
        <Button className={section === "channel-sites" ? "open-api-tab active" : "open-api-tab"} onClick={() => setSection("channel-sites")}>渠道站点包</Button>
      </div>

      {section === "open-api" ? (
        <OpenAPISectionView
          allVisibleSelected={allVisibleSelected}
          bulkStatus={bulkStatus}
          clearFilters={clearFilters}
          createdSite={createdSite}
          createSite={createSite}
          data={data}
          deleteSite={deleteSite}
          endpointColumns={endpointColumns}
          filteredSites={filteredSites}
          loading={loading}
          logColumns={logColumns}
          name={name}
          patchSite={patchSite}
          patchSiteLocal={patchSiteLocal}
          pickedScopes={pickedScopes}
          qps={qps}
          query={query}
          revokeSite={revokeSite}
          runBulk={runBulk}
          saving={saving}
          savingSiteID={savingSiteID}
          scopeFilter={scopeFilter}
          selected={selected}
          selectedSet={selectedSet}
          setBulkStatus={setBulkStatus}
          setName={setName}
          setQps={setQps}
          setQuery={setQuery}
          setScopeFilter={setScopeFilter}
          setSelected={setSelected}
          setStatus={setStatus}
          status={status}
          toggleScope={toggleScope}
          toggleSiteScope={toggleSiteScope}
        />
      ) : (
        <ChannelSitesSectionView
          addChannelNav={addChannelNav}
          buildPackage={buildPackage}
          channelForm={channelForm}
          channelSiteKey={channelSiteKey}
          channelSiteStats={channelSiteStats}
          channelSites={channelSites}
          deleteChannelSite={removeChannelSite}
          downloadChannelSiteTemplate={downloadChannelSiteTemplate}
          downloadPackage={downloadPackage}
          editChannelSite={editChannelSite}
          editingChannelSiteID={editingChannelSiteID}
          handleChannelSiteImportFile={handleChannelSiteImportFile}
          importingChannelSites={importingChannelSites}
          channelImportFileName={channelImportFileName}
          channelImportResults={channelImportResults}
          channelImportRows={channelImportRows}
          loading={loading}
          patchChannelCopy={patchChannelCopy}
          patchChannelForm={patchChannelForm}
          patchChannelModules={patchChannelModules}
          patchChannelNav={patchChannelNav}
          patchChannelSEO={patchChannelSEO}
          removeChannelNav={removeChannelNav}
          resetChannelSiteForm={resetChannelSiteForm}
          rotateChannelKey={rotateChannelKey}
          runChannelSiteBatchImport={runChannelSiteBatchImport}
          runtimeLogColumns={channelRuntimeLogColumns}
          saveChannelSite={saveChannelSite}
          saving={saving}
          savingSiteID={savingSiteID}
        />
      )}
    </AdminShell>
  );
}

function OpenAPISectionView({
  allVisibleSelected,
  bulkStatus,
  clearFilters,
  createdSite,
  createSite,
  data,
  deleteSite,
  endpointColumns,
  filteredSites,
  loading,
  logColumns,
  name,
  patchSite,
  patchSiteLocal,
  pickedScopes,
  qps,
  query,
  revokeSite,
  runBulk,
  saving,
  savingSiteID,
  scopeFilter,
  selected,
  selectedSet,
  setBulkStatus,
  setName,
  setQps,
  setQuery,
  setScopeFilter,
  setSelected,
  setStatus,
  status,
  toggleScope,
  toggleSiteScope
}: {
  allVisibleSelected: boolean;
  bulkStatus: string;
  clearFilters: () => void;
  createdSite: OpenAPISite | null;
  createSite: () => Promise<void>;
  data: OpenAPISummary | null;
  deleteSite: (site: OpenAPISite) => Promise<void>;
  endpointColumns: DataTableColumn<OpenAPISummary["endpoints"][number]>[];
  filteredSites: OpenAPISite[];
  loading: boolean;
  logColumns: DataTableColumn<OpenAPICallLog>[];
  name: string;
  patchSite: (site: OpenAPISite, patch: Partial<OpenAPISite>) => Promise<void>;
  patchSiteLocal: (siteID: string, patch: Partial<OpenAPISite>) => void;
  pickedScopes: Set<string>;
  qps: number;
  query: string;
  revokeSite: (site: OpenAPISite) => Promise<void>;
  runBulk: (action: "status" | "revoke" | "delete") => Promise<void>;
  saving: boolean;
  savingSiteID: string;
  scopeFilter: string;
  selected: string[];
  selectedSet: Set<string>;
  setBulkStatus: (value: string) => void;
  setName: (value: string) => void;
  setQps: (value: number) => void;
  setQuery: (value: string) => void;
  setScopeFilter: (value: string) => void;
  setSelected: (value: string[] | ((current: string[]) => string[])) => void;
  setStatus: (value: string) => void;
  status: string;
  toggleScope: (scope: string) => void;
  toggleSiteScope: (site: OpenAPISite, scope: string) => void;
}) {
  return (
    <>
      <div className="grid open-api-grid">
        <div className="card card-pad">
          <div className="section-head compact">
            <h2>授权新站点</h2>
            <span className="sub">Site Key 只展示一次</span>
          </div>
          <div className="field">
            <label>站点名称</label>
            <input className="input" value={name} onChange={(event) => setName(event.target.value)} />
          </div>
          <div className="field">
            <label>QPS 限额 <span className="hint">每个 Site Key 独立计算</span></label>
            <input className="input" type="number" min={1} max={500} value={qps} onChange={(event) => setQps(Number(event.target.value) || 1)} />
          </div>
          <div className="field">
            <label>可访问端点</label>
            <div className="scope-grid">
              {scopes.map((scope) => {
                const active = pickedScopes.has(scope.id);
                return (
                  <button type="button" className={`scope-chip ${active ? "active" : ""}`} onClick={() => toggleScope(scope.id)} key={scope.id}>
                    <b>{scope.label}</b>
                    <small>{scope.path}</small>
                  </button>
                );
              })}
            </div>
          </div>
          <Button variant="primary" onClick={() => void createSite()} disabled={saving}>{saving ? "创建中..." : "＋ 授权新站点"}</Button>
          {createdSite?.plainKey ? (
            <div className="one-time-key">
              <StatusBadge tone="amber">仅展示一次</StatusBadge>
              <div className="endpoint">
                <span className="k">SITE_KEY</span>
                <span className="v">{createdSite.plainKey}</span>
                <CopyButton value={createdSite.plainKey}>复制</CopyButton>
              </div>
            </div>
          ) : null}
        </div>

        <div className="card card-pad">
          <div className="section-head compact">
            <h2>调用示例</h2>
            <span className="sub">正式开放路径</span>
          </div>
          <div className="endpoint">
            <span className="k">BASE</span>
            <span className="v">{data?.baseUrl || "/v1/status"}</span>
            <CopyButton value={data?.baseUrl || "/v1/status"}>复制</CopyButton>
          </div>
          <pre className="api-code">{`curl -H "X-Site-Key: site-th-****" \\
  ${window.location.origin}/v1/status/overview`}</pre>
          <div className="ops-help">Open API 只读，不提供后台管理或网关调用能力。通道同步 scope 会在显式请求时返回平台通道凭据明文，请单独签发并限制 QPS。</div>
        </div>
      </div>

      <div className="section-head">
        <h2>可调用端点</h2>
        <span className="sub">与前台监控看板一致</span>
      </div>
      <DataTable
        data={data?.endpoints ?? []}
        columns={endpointColumns}
        loading={loading}
        pageSize={10}
        rowKey={(endpoint) => endpoint.path}
        loadingText="正在加载端点…"
        footerNote="正式文档只暴露 /v1/status/*"
      />

      <div className="section-head">
        <h2>授权站点</h2>
        <span className="sub">Site Key 创建后只显示 mask</span>
      </div>
      <FilterBar>
        <SelectField aria-label="授权站点状态筛选" value={status} onChange={(event) => setStatus(event.target.value)}>
          <option value="all">状态：全部</option>
          <option value="active">状态：active</option>
          <option value="paused">状态：paused</option>
          <option value="revoked">状态：revoked</option>
        </SelectField>
        <SelectField aria-label="授权站点 Scope 筛选" value={scopeFilter} onChange={(event) => setScopeFilter(event.target.value)}>
          <option value="all">Scope：全部</option>
          {scopes.map((scope) => <option value={scope.id} key={scope.id}>Scope：{scope.id}</option>)}
        </SelectField>
        <input className="input tk-filter-search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索站点名、Key mask、scope..." />
        <Button size="sm" onClick={clearFilters} disabled={loading}>清空筛选</Button>
      </FilterBar>
      <div className="toolbar card tk-bulk-action-bar">
        <span className="muted">已选 {selected.length} 项</span>
        <SelectField className="compact-select" value={bulkStatus} onChange={(event) => setBulkStatus(event.target.value)}>
          <option value="active">active</option>
          <option value="paused">paused</option>
        </SelectField>
        <Button size="sm" disabled={!selected.length || saving} onClick={() => void runBulk("status")}>批量改状态</Button>
        <Button size="sm" disabled={!selected.length || saving} onClick={() => void runBulk("revoke")}>批量吊销</Button>
        <Button size="sm" variant="danger" disabled={!selected.length || saving} onClick={() => void runBulk("delete")}>批量删除</Button>
      </div>
      <div className="card board">
        <div className="dt-wrap">
          <table className="dt open-api-sites-table">
            <colgroup>
              <col className="open-api-site-select-col" />
              <col className="open-api-site-name-col" />
              <col className="open-api-site-key-col" />
              <col className="open-api-site-scope-col" />
              <col className="open-api-site-calls-col" />
              <col className="open-api-site-qps-col" />
              <col className="open-api-site-status-col" />
              <col className="open-api-site-time-col" />
              <col className="open-api-site-actions-col" />
            </colgroup>
            <thead>
              <tr>
                <th><CheckboxField aria-label="选择全部授权站点" checked={allVisibleSelected} onChange={(event) => setSelected(event.target.checked ? filteredSites.map((site) => site.id) : [])} /></th>
                <th>授权站点</th>
                <th>Site Key</th>
                <th>可访问端点</th>
                <th>今日调用</th>
                <th>QPS</th>
                <th>状态</th>
                <th>最近使用</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {loading ? <tr><td colSpan={9}>正在加载授权站点…</td></tr> : filteredSites.length ? filteredSites.map((site) => (
                <tr key={site.id}>
                  <td><CheckboxField aria-label={`选择 ${site.name}`} checked={selectedSet.has(site.id)} onChange={(event) => setSelected((current) => event.target.checked ? [...current, site.id] : current.filter((id) => id !== site.id))} /></td>
                  <td>
                    <div className="stacked-cell">
                      <input
                        className="input compact-name open-api-site-name-input"
                        value={site.name}
                        title={site.name}
                        disabled={savingSiteID === site.id || site.status === "revoked"}
                        onChange={(event) => patchSiteLocal(site.id, { name: event.target.value })}
                        onBlur={(event) => void patchSite(site, { name: event.target.value })}
                      />
                      <small>{site.siteKeyPrefix || site.siteKeyMask}</small>
                    </div>
                  </td>
                  <td>
                    <div className="site-key-mask mono" title={site.siteKeyMask}>{site.siteKeyMask}</div>
                  </td>
                  <td>
                    <div className="mini-scopes">
                      {scopes.map((scope) => <button type="button" className={`scope-dot ${site.scopes.includes(scope.id) ? "on" : ""}`} disabled={savingSiteID === site.id || site.status === "revoked"} onClick={() => toggleSiteScope(site, scope.id)} key={scope.id}>{scope.id}</button>)}
                    </div>
                  </td>
                  <td className="mono">{site.callsToday}</td>
                  <td>
                    <input
                      className="input compact-number open-api-site-qps-input"
                      type="number"
                      min={1}
                      max={500}
                      value={site.qpsLimit}
                      disabled={savingSiteID === site.id || site.status === "revoked"}
                      onChange={(event) => patchSiteLocal(site.id, { qpsLimit: Math.min(500, Math.max(1, Number(event.target.value) || 1)) })}
                      onBlur={(event) => void patchSite(site, { qpsLimit: Math.min(500, Math.max(1, Number(event.target.value) || 1)) })}
                    />
                  </td>
                  <td>
                    <SelectField className="compact-select open-api-site-status-select" value={site.status} disabled={savingSiteID === site.id || site.status === "revoked"} onChange={(event) => void patchSite(site, { status: event.target.value })}>
                      <option value="active">active</option>
                      <option value="paused">paused</option>
                      {site.status === "revoked" ? <option value="revoked">revoked</option> : null}
                    </SelectField>
                  </td>
                  <td className="muted-time">{site.lastUsedAt ? timeLabel(site.lastUsedAt) : "-"}</td>
                  <td className="actions">
                    <div className="table-actions">
                      <Button size="sm" disabled={savingSiteID === site.id || site.status === "revoked"} onClick={() => void revokeSite(site)}>吊销</Button>
                      <Button size="sm" variant="danger" disabled={savingSiteID === site.id} onClick={() => void deleteSite(site)}>删除</Button>
                    </div>
                  </td>
                </tr>
              )) : (
                <tr><td colSpan={9}><div className="empty-state"><h4>没有匹配授权站点</h4><p>创建 Site Key 或调整筛选条件后再试。</p></div></td></tr>
              )}
            </tbody>
          </table>
        </div>
        <div className="tfoot"><span>Site Key 仅在创建时完整展示一次</span><span>越权和限流请求会写入调用日志</span></div>
      </div>

      <div className="section-head">
        <h2>调用记录</h2>
        <span className="sub">真实 Open API 请求 · 默认 10 行</span>
      </div>
      <DataTable
        data={data?.logs ?? []}
        columns={logColumns}
        loading={loading}
        pageSize={10}
        rowKey={(log) => log.id}
        loadingText="正在加载调用记录…"
        footerNote="仅保留真实 /v1/status/* 调用记录"
        empty={<div className="empty-state"><h4>暂无真实调用记录</h4><p>用 Site Key 调用任一 /v1/status/* 端点后会显示在这里。</p></div>}
      />
    </>
  );
}

function ChannelSitesSectionView({
  addChannelNav,
  buildPackage,
  channelForm,
  channelImportFileName,
  channelImportResults,
  channelImportRows,
  channelSiteKey,
  channelSiteStats,
  channelSites,
  deleteChannelSite,
  downloadChannelSiteTemplate,
  downloadPackage,
  editChannelSite,
  editingChannelSiteID,
  handleChannelSiteImportFile,
  importingChannelSites,
  loading,
  patchChannelCopy,
  patchChannelForm,
  patchChannelModules,
  patchChannelNav,
  patchChannelSEO,
  removeChannelNav,
  resetChannelSiteForm,
  rotateChannelKey,
  runChannelSiteBatchImport,
  runtimeLogColumns,
  saveChannelSite,
  saving,
  savingSiteID
}: {
  addChannelNav: () => void;
  buildPackage: (site: ChannelSite) => Promise<void>;
  channelForm: ChannelSiteInput;
  channelImportFileName: string;
  channelImportResults: ChannelSiteImportResult[];
  channelImportRows: ChannelSiteImportRow[];
  channelSiteKey: string;
  channelSiteStats: Array<[string, string | number, string]>;
  channelSites: ChannelSitesSummary | null;
  deleteChannelSite: (site: ChannelSite) => Promise<void>;
  downloadChannelSiteTemplate: () => void;
  downloadPackage: (site: ChannelSite) => Promise<void>;
  editChannelSite: (site: ChannelSite) => void;
  editingChannelSiteID: string;
  handleChannelSiteImportFile: (file: File) => Promise<void>;
  importingChannelSites: boolean;
  loading: boolean;
  patchChannelCopy: (patch: Partial<ChannelSiteInput["copy"]>) => void;
  patchChannelForm: (patch: Partial<ChannelSiteInput>) => void;
  patchChannelModules: (key: keyof ChannelSiteInput["modules"], checked: boolean) => void;
  patchChannelNav: (index: number, patch: Partial<ChannelSiteInput["navItems"][number]>) => void;
  patchChannelSEO: (patch: Partial<ChannelSiteInput["seo"]>) => void;
  removeChannelNav: (index: number) => void;
  resetChannelSiteForm: () => void;
  rotateChannelKey: (site: ChannelSite) => Promise<void>;
  runChannelSiteBatchImport: () => Promise<void>;
  runtimeLogColumns: DataTableColumn<ChannelSiteRuntimeLog>[];
  saveChannelSite: () => Promise<void>;
  saving: boolean;
  savingSiteID: string;
}) {
  const validImportRows = channelImportRows.filter((row) => !row.errors.length);
  const invalidImportRows = channelImportRows.length - validImportRows.length;
  const successfulImports = channelImportResults.filter((result) => result.status === "success").length;

  return (
    <>
      <div className="channel-site-safe-note">
        <div>
          <b>站点包是静态文件，数据来自 runtime API</b>
          <p>下载包内包含首页、精选推荐页、SEO 文件、llms.txt 和部署说明。runtime key 只允许读取公开监控与推荐数据，不具备后台、网关或密钥权限。</p>
        </div>
        <div className="endpoint">
          <span className="k">BASE</span>
          <span className="v">{channelSites?.baseUrl || "/site/v1"}</span>
          <CopyButton value={channelSites?.baseUrl || "/site/v1"}>复制</CopyButton>
        </div>
      </div>

      <div className="stat-row">
        {channelSiteStats.map(([label, value, hint]) => (
          <div className="stat" key={label}>
            <div className="l">{label}</div>
            <div className="v">{formatStat(value)}</div>
            <div className="d">{hint}</div>
          </div>
        ))}
      </div>

      <div className="grid channel-site-grid">
        <div className="card card-pad channel-site-form-card">
          <div className="section-head compact">
            <h2>{editingChannelSiteID ? "编辑渠道站点" : "新增渠道站点"}</h2>
            <span className="sub">配置域名、菜单、模块和站点文案</span>
          </div>

          <div className="channel-site-form-grid">
            <label className="field">
              <span>站点名称</span>
              <input className="input" value={channelForm.name} onChange={(event) => patchChannelForm({ name: event.target.value })} placeholder="例如 TokHub 渠道站" />
            </label>
            <label className="field">
              <span>绑定域名</span>
              <input className="input" value={channelForm.domain} onChange={(event) => patchChannelForm({ domain: event.target.value })} placeholder="status.example.com" />
            </label>
            <label className="field">
              <span>站点地址</span>
              <input className="input" value={channelForm.publicUrl} onChange={(event) => patchChannelForm({ publicUrl: event.target.value })} placeholder="https://status.example.com" />
            </label>
            <label className="field">
              <span>QPS</span>
              <input className="input" type="number" min={1} max={500} value={channelForm.qpsLimit} onChange={(event) => patchChannelForm({ qpsLimit: Number(event.target.value) || 1 })} />
            </label>
            <label className="field">
              <span>状态</span>
              <SelectField value={channelForm.status} onChange={(event) => patchChannelForm({ status: event.target.value })}>
                <option value="active">active</option>
                <option value="paused">paused</option>
              </SelectField>
            </label>
            <label className="field">
              <span>Logo 标识</span>
              <input className="input" value={channelForm.logoMark} maxLength={3} onChange={(event) => patchChannelForm({ logoMark: event.target.value })} />
            </label>
          </div>

          <div className="channel-site-form-grid two">
            <label className="field">
              <span>首页标题</span>
              <input className="input" value={channelForm.title} onChange={(event) => patchChannelForm({ title: event.target.value })} />
            </label>
            <label className="field">
              <span>站点描述</span>
              <input className="input" value={channelForm.description} onChange={(event) => patchChannelForm({ description: event.target.value })} />
            </label>
            <label className="field">
              <span>首页菜单名称</span>
              <input className="input" value={channelForm.overviewLabel} onChange={(event) => patchChannelForm({ overviewLabel: event.target.value })} />
            </label>
            <label className="field">
              <span>推荐菜单名称</span>
              <input className="input" value={channelForm.recommendLabel} onChange={(event) => patchChannelForm({ recommendLabel: event.target.value })} />
            </label>
          </div>

          <div className="field">
            <label>启用模块</label>
            <div className="channel-site-module-grid">
              {moduleOptions.map((module) => (
                <label className="channel-site-toggle" key={module.key}>
                  <CheckboxField checked={channelForm.modules[module.key]} onChange={(event) => patchChannelModules(module.key, event.target.checked)} />
                  <span><b>{module.label}</b><small>{module.hint}</small></span>
                </label>
              ))}
            </div>
          </div>

          <div className="field">
            <label>自定义菜单 <span className="hint">最多 3 个</span></label>
            <div className="channel-site-nav-list">
              {channelForm.navItems.map((item, index) => (
                <div className="channel-site-nav-row" key={index}>
                  <input className="input" value={item.label} onChange={(event) => patchChannelNav(index, { label: event.target.value })} placeholder="菜单文本" />
                  <input className="input" value={item.href} onChange={(event) => patchChannelNav(index, { href: event.target.value })} placeholder="https:// 或 /path" />
                  <Button size="sm" variant="danger" onClick={() => removeChannelNav(index)}>删除</Button>
                </div>
              ))}
              <Button size="sm" disabled={channelForm.navItems.length >= 3} onClick={addChannelNav}>＋ 添加菜单</Button>
            </div>
          </div>

          <div className="channel-site-form-grid two">
            <label className="field">
              <span>首页说明</span>
              <textarea className="input" value={channelForm.copy.homeIntro} onChange={(event) => patchChannelCopy({ homeIntro: event.target.value })} />
            </label>
            <label className="field">
              <span>推荐说明</span>
              <textarea className="input" value={channelForm.copy.recommendIntro} onChange={(event) => patchChannelCopy({ recommendIntro: event.target.value })} />
            </label>
            <label className="field">
              <span>页脚文案</span>
              <input className="input" value={channelForm.copy.footerText} onChange={(event) => patchChannelCopy({ footerText: event.target.value })} />
            </label>
            <label className="field">
              <span>Canonical URL</span>
              <input className="input" value={channelForm.seo.canonicalUrl} onChange={(event) => patchChannelSEO({ canonicalUrl: event.target.value })} placeholder="默认使用站点地址" />
            </label>
          </div>

          <div className="channel-site-actions">
            <Button onClick={resetChannelSiteForm} disabled={saving}>重置</Button>
            <Button variant="primary" onClick={() => void saveChannelSite()} disabled={saving}>{saving ? "保存中..." : editingChannelSiteID ? "保存配置" : "新增渠道站点"}</Button>
          </div>

          {channelSiteKey ? (
            <div className="one-time-key">
              <StatusBadge tone="amber">runtime key 仅展示一次</StatusBadge>
              <div className="endpoint">
                <span className="k">RUNTIME_KEY</span>
                <span className="v">{channelSiteKey}</span>
                <CopyButton value={channelSiteKey}>复制</CopyButton>
              </div>
            </div>
          ) : null}
        </div>

        <div className="channel-site-side-stack">
          <div className="card card-pad channel-site-import-card">
            <div className="section-head compact">
              <h2>批量导入站点包</h2>
              <span className="sub">Excel .xlsx / CSV 批量创建、生成并下载</span>
            </div>
            <div className="channel-site-import-actions">
              <label className={`btn tk-button tk-button-primary tk-button-md btn-primary channel-site-file-label ${importingChannelSites ? "disabled" : ""}`}>
                上传 Excel / CSV
                <input
                  type="file"
                  accept=".csv,.xlsx,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
                  disabled={importingChannelSites}
                  onChange={(event) => {
                    const file = event.target.files?.[0];
                    if (file) void handleChannelSiteImportFile(file);
                    event.target.value = "";
                  }}
                />
              </label>
              <Button onClick={downloadChannelSiteTemplate} disabled={importingChannelSites}>下载导入模板</Button>
            </div>
            <div className="channel-site-template-help">
              <b>模板字段</b>
              <p>必填：site_name、domain、public_url。可选：title、description、qps_limit、status、modules、home_intro、recommend_intro、footer_text、canonical_url、nav1_label/nav1_href 到 nav3_label/nav3_href。</p>
              <p>modules 用分号分隔，例如 overview;channelBoard;recommend；status 填 active 或 paused。</p>
              <div className="channel-site-template-fields" aria-label="导入字段示例">
                <span><b>site_name</b><small>站点名称，例如 Example Status</small></span>
                <span><b>domain</b><small>绑定域名，例如 status.example.com</small></span>
                <span><b>public_url</b><small>公开地址，需要 http:// 或 https://</small></span>
                <span><b>nav*_label / nav*_href</b><small>最多 3 个菜单锚文本和链接</small></span>
              </div>
            </div>
            {channelImportRows.length ? (
              <div className="channel-site-import-preview">
                <div>
                  <b>{channelImportFileName || "导入文件"}</b>
                  <span>{channelImportRows.length} 行 · {validImportRows.length} 行可导入 · {invalidImportRows} 行需修正</span>
                </div>
                <Button variant="primary" disabled={!validImportRows.length || importingChannelSites} onClick={() => void runChannelSiteBatchImport()}>
                  {importingChannelSites ? "导入中..." : "导入并生成下载"}
                </Button>
              </div>
            ) : null}
            {invalidImportRows ? (
              <div className="channel-site-import-errors">
                {channelImportRows.filter((row) => row.errors.length).slice(0, 3).map((row) => (
                  <span key={row.rowNumber}>第 {row.rowNumber} 行：{row.errors.join("、")}</span>
                ))}
                {invalidImportRows > 3 ? <span>还有 {invalidImportRows - 3} 行错误未显示</span> : null}
              </div>
            ) : null}
            {channelImportResults.length ? (
              <div className="channel-site-import-results">
                <div className="channel-site-import-result-summary">
                  <b>批量结果</b>
                  <span>{successfulImports}/{channelImportResults.length} 成功</span>
                </div>
                {channelImportResults.slice(0, 4).map((result) => (
                  <div className="channel-site-import-result" key={`${result.rowNumber}-${result.domain}`}>
                    <StatusBadge tone={result.status === "success" ? "green" : "red"}>{result.status === "success" ? "成功" : "失败"}</StatusBadge>
                    <span>{result.name} · {result.message}</span>
                  </div>
                ))}
              </div>
            ) : null}
          </div>

          <div className="card card-pad">
            <div className="section-head compact">
              <h2>部署说明</h2>
              <span className="sub">上传即用，回源取数</span>
            </div>
            <div className="channel-site-flow">
              <div><b>1. 配置域名</b><span>填写渠道站域名和公开访问地址</span></div>
              <div><b>2. 生成站点包</b><span>下载 zip，包含 index、recommend、SEO 和静态资源</span></div>
              <div><b>3. 上传部署</b><span>解压到渠道站目录，页面自动请求 TokHub runtime API</span></div>
              <div><b>4. 后台治理</b><span>菜单、文案、推荐和监控数据仍由 TokHub 统一管理</span></div>
            </div>
          </div>
        </div>
      </div>

      <div className="section-head">
        <h2>渠道站点</h2>
        <span className="sub">一个站点一个 runtime key，下载包会写入最新 key</span>
      </div>
      <div className="channel-site-card-list">
        {loading ? (
          <div className="card card-pad channel-site-empty">正在加载渠道站点…</div>
        ) : channelSites?.sites.length ? channelSites.sites.map((site) => (
          <div className="card channel-site-card" key={site.id}>
            <div className="channel-site-card-top">
              <div className="avatar">{site.logoMark || site.name.slice(0, 1)}</div>
              <div>
                <h3>{site.name}</h3>
                <p>{site.domain} · {site.publicUrl}</p>
              </div>
              <StatusBadge tone={site.status === "active" ? "green" : site.status === "paused" ? "amber" : "red"}>{site.status}</StatusBadge>
            </div>
            <div className="channel-site-card-meta">
              <span>Runtime Key <b className="mono">{site.runtimeKeyMask}</b></span>
              <span>QPS <b>{site.qpsLimit}</b></span>
              <span>今日回源 <b>{site.callsToday}</b></span>
              <span>最近使用 <b>{site.lastUsedAt ? timeLabel(site.lastUsedAt) : "-"}</b></span>
            </div>
            <div className="channel-site-package-list">
              {(site.packageExports || []).slice(0, 2).map((item) => (
                <span key={item.id}>{item.fileName} · {formatBytes(item.fileSize)} · {timeLabel(item.createdAt)}</span>
              ))}
              {!(site.packageExports || []).length ? <span>暂无站点包导出记录</span> : null}
            </div>
            <div className="channel-site-card-actions">
              <Button size="sm" onClick={() => editChannelSite(site)}>编辑</Button>
              <Button size="sm" onClick={() => void buildPackage(site)} disabled={savingSiteID === site.id}>生成包</Button>
              <Button size="sm" variant="primary" onClick={() => void downloadPackage(site)} disabled={savingSiteID === site.id}>下载 ZIP</Button>
              <Button size="sm" onClick={() => void rotateChannelKey(site)} disabled={savingSiteID === site.id}>轮换 Key</Button>
              <Button size="sm" variant="danger" onClick={() => void deleteChannelSite(site)} disabled={savingSiteID === site.id}>删除</Button>
            </div>
          </div>
        )) : (
          <div className="empty-state card card-pad channel-site-empty">
            <div>
              <h4>暂无渠道站点</h4>
              <p>先配置域名和站点文案，再生成可部署的静态站点包</p>
            </div>
            <div className="channel-site-empty-steps" aria-label="渠道站点创建步骤">
              <span><b>1</b> 填写渠道域名</span>
              <span><b>2</b> 设置菜单和 SEO 文案</span>
              <span><b>3</b> 下载 ZIP 并部署</span>
            </div>
          </div>
        )}
      </div>

      <div className="section-head">
        <h2>渠道站点运行日志</h2>
        <span className="sub">只记录 runtime API 请求，默认 10 行分页</span>
      </div>
      <DataTable
        data={channelSites?.logs ?? []}
        columns={runtimeLogColumns}
        loading={loading}
        pageSize={10}
        rowKey={(log) => log.id}
        loadingText="正在加载运行日志…"
        footerNote="runtime key 只允许读取公开监控和推荐数据"
        empty={<div className="empty-state"><h4>暂无渠道站点调用</h4><p>部署后的站点访问 /site/v1/* 时会写入这里。</p></div>}
      />
    </>
  );
}

function channelSiteDefaults(): ChannelSiteInput {
  const origin = typeof window !== "undefined" ? window.location.origin : "";
  return {
    name: "TokHub 渠道站",
    domain: "",
    publicUrl: "",
    title: "API 中转站监控",
    description: "实时查看中转站状态、精选推荐和供应商表现",
    logoMark: "T",
    overviewLabel: "监控总览",
    recommendLabel: "精选推荐",
    modules: { overview: true, channelBoard: true, recommend: true, providerRank: true, strategy: true },
    copy: {
      homeIntro: "实时追踪中转站健康、延迟、可用率和真实生成表现",
      recommendIntro: "精选可用供应商与接入入口，适合快速选择稳定通道",
      footerText: "Powered by TokHub"
    },
    seo: {
      title: "API 中转站监控",
      description: "实时查看 API 中转站健康状态、真实调用表现和精选推荐",
      canonicalUrl: origin
    },
    navItems: [],
    qpsLimit: 60,
    status: "active"
  };
}

function channelSiteToInput(site: ChannelSite): ChannelSiteInput {
  return {
    name: site.name,
    domain: site.domain,
    publicUrl: site.publicUrl,
    title: site.title,
    description: site.description,
    logoMark: site.logoMark,
    overviewLabel: site.overviewLabel,
    recommendLabel: site.recommendLabel,
    modules: site.modules,
    copy: site.copy,
    seo: site.seo,
    navItems: (site.navItems || []).slice(0, 3),
    qpsLimit: site.qpsLimit,
    status: site.status === "revoked" ? "paused" : site.status
  };
}

function normalizeChannelSiteInput(input: ChannelSiteInput): ChannelSiteInput {
  const publicUrl = input.publicUrl.trim().replace(/\/+$/, "");
  return {
    ...input,
    name: input.name.trim(),
    domain: input.domain.trim().replace(/^https?:\/\//, "").replace(/\/.*$/, ""),
    publicUrl,
    title: input.title.trim() || input.name.trim(),
    description: input.description.trim(),
    logoMark: (input.logoMark.trim() || input.name.trim().slice(0, 1) || "T").slice(0, 3),
    overviewLabel: input.overviewLabel.trim() || "监控总览",
    recommendLabel: input.recommendLabel.trim() || "精选推荐",
    qpsLimit: Math.min(500, Math.max(1, input.qpsLimit || 60)),
    navItems: input.navItems
      .map((item, index) => ({ ...item, label: item.label.trim(), href: item.href.trim(), position: index }))
      .filter((item) => item.label && item.href)
      .slice(0, 3),
    seo: {
      title: input.seo.title.trim() || input.title.trim() || input.name.trim(),
      description: input.seo.description.trim() || input.description.trim(),
      canonicalUrl: input.seo.canonicalUrl.trim() || publicUrl
    }
  };
}

const channelSiteImportHeaders = [
  "site_name",
  "domain",
  "public_url",
  "title",
  "description",
  "overview_label",
  "recommend_label",
  "logo_mark",
  "qps_limit",
  "status",
  "modules",
  "home_intro",
  "recommend_intro",
  "footer_text",
  "canonical_url",
  "nav1_label",
  "nav1_href",
  "nav2_label",
  "nav2_href",
  "nav3_label",
  "nav3_href"
] as const;

const channelSiteImportExample = [
  "Example Status",
  "status.example.com",
  "https://status.example.com",
  "Example API 中转站监控",
  "实时查看 Example 渠道的 API 可用性和精选推荐",
  "监控总览",
  "精选推荐",
  "E",
  "30",
  "active",
  "overview;channelBoard;recommend;providerRank;strategy",
  "查看 API 中转站实时可用性、延迟和真实生成表现",
  "精选稳定中转站与接入入口",
  "Powered by TokHub",
  "https://status.example.com",
  "官网",
  "https://example.com",
  "控制台",
  "https://tokhub.run/console",
  "帮助",
  "https://example.com/docs"
];

const channelSiteImportHeaderAliases: Record<string, string> = {
  site_name: "site_name",
  name: "site_name",
  "站点名称": "site_name",
  domain: "domain",
  "绑定域名": "domain",
  "域名": "domain",
  public_url: "public_url",
  publicurl: "public_url",
  url: "public_url",
  "站点地址": "public_url",
  title: "title",
  "首页标题": "title",
  description: "description",
  "站点描述": "description",
  overview_label: "overview_label",
  "首页菜单名称": "overview_label",
  recommend_label: "recommend_label",
  "推荐菜单名称": "recommend_label",
  logo_mark: "logo_mark",
  "logo标识": "logo_mark",
  "logo 标识": "logo_mark",
  qps: "qps_limit",
  qps_limit: "qps_limit",
  "状态": "status",
  status: "status",
  modules: "modules",
  "启用模块": "modules",
  home_intro: "home_intro",
  "首页说明": "home_intro",
  recommend_intro: "recommend_intro",
  "推荐说明": "recommend_intro",
  footer_text: "footer_text",
  "页脚文案": "footer_text",
  canonical_url: "canonical_url",
  nav1_label: "nav1_label",
  nav1_href: "nav1_href",
  nav2_label: "nav2_label",
  nav2_href: "nav2_href",
  nav3_label: "nav3_label",
  nav3_href: "nav3_href",
  "菜单1文本": "nav1_label",
  "菜单1链接": "nav1_href",
  "菜单2文本": "nav2_label",
  "菜单2链接": "nav2_href",
  "菜单3文本": "nav3_label",
  "菜单3链接": "nav3_href"
};

const channelSiteModuleImportMap: Record<string, keyof ChannelSiteInput["modules"]> = {
  overview: "overview",
  "监控总览": "overview",
  channelboard: "channelBoard",
  channel_board: "channelBoard",
  channels: "channelBoard",
  "通道明细": "channelBoard",
  "通道明细看板": "channelBoard",
  recommend: "recommend",
  "精选推荐": "recommend",
  providerrank: "providerRank",
  provider_rank: "providerRank",
  "供应商排行": "providerRank",
  strategy: "strategy",
  "监控策略": "strategy"
};

function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  document.body.removeChild(anchor);
  URL.revokeObjectURL(url);
}

function buildChannelSiteTemplateBlob() {
  const rows = [
    channelSiteImportHeaders,
    channelSiteImportExample
  ];
  const csv = rows.map((row) => row.map(csvCell).join(",")).join("\n");
  return new Blob(["\ufeff", csv], { type: "text/csv;charset=utf-8" });
}

function csvCell(value: unknown) {
  const text = String(value ?? "");
  const safeText = /^[=+\-@]/.test(text) ? `'${text}` : text;
  return `"${safeText.replace(/"/g, '""')}"`;
}

async function readChannelSiteImportRecords(file: File): Promise<Array<Record<string, string>>> {
  if (/\.xlsx$/i.test(file.name)) {
    const { readSheet } = await import("read-excel-file/browser");
    const rows = await readSheet(file);
    if (rows.length < 2) throw new Error("Excel 至少需要表头和一行数据");
    const headers = rows[0].map((cell) => canonicalImportHeader(String(cell ?? "")));
    return rows.slice(1)
      .filter((row) => row.some((cell) => String(cell ?? "").trim()))
      .map((row) => headers.reduce<Record<string, string>>((record, header, index) => {
        record[header] = String(row[index] ?? "").trim();
        return record;
      }, {}));
  }

  const isCSV = /\.csv$/i.test(file.name) || file.type === "text/csv" || file.type === "application/vnd.ms-excel";
  if (!isCSV) {
    throw new Error("仅支持 CSV 或 Excel .xlsx 文件");
  }

  const text = await file.text();
  const rows = parseCSVRows(text);
  if (rows.length < 2) throw new Error("CSV 至少需要表头和一行数据");
  const headers = rows[0].map(canonicalImportHeader);
  return rows.slice(1)
    .filter((row) => row.some((cell) => cell.trim()))
    .map((row) => headers.reduce<Record<string, string>>((record, header, index) => {
      record[header] = String(row[index] ?? "").trim();
      return record;
    }, {}));
}

function canonicalImportHeader(header: string) {
  const trimmed = header.replace(/^\ufeff/, "").trim();
  const normalized = trimmed.toLowerCase().replace(/[\s-]+/g, "_");
  return channelSiteImportHeaderAliases[normalized] || channelSiteImportHeaderAliases[trimmed] || normalized;
}

function parseCSVRows(text: string) {
  const rows: string[][] = [];
  let row: string[] = [];
  let cell = "";
  let quoted = false;
  for (let index = 0; index < text.length; index += 1) {
    const char = text[index];
    const next = text[index + 1];
    if (char === "\"") {
      if (quoted && next === "\"") {
        cell += "\"";
        index += 1;
      } else {
        quoted = !quoted;
      }
      continue;
    }
    if (char === "," && !quoted) {
      row.push(cell);
      cell = "";
      continue;
    }
    if ((char === "\n" || char === "\r") && !quoted) {
      if (char === "\r" && next === "\n") index += 1;
      row.push(cell);
      rows.push(row);
      row = [];
      cell = "";
      continue;
    }
    cell += char;
  }
  row.push(cell);
  if (row.some((item) => item.trim())) rows.push(row);
  return rows;
}

function buildChannelSiteImportRow(record: Record<string, string>, rowNumber: number): ChannelSiteImportRow {
  const defaults = channelSiteDefaults();
  const name = importValue(record, "site_name");
  const domain = importValue(record, "domain");
  const publicUrl = importValue(record, "public_url");
  const qpsLimit = parseImportNumber(importValue(record, "qps_limit"), defaults.qpsLimit, 1, 500);
  const navItems = [1, 2, 3].flatMap((position) => {
    const label = importValue(record, `nav${position}_label`);
    const href = importValue(record, `nav${position}_href`);
    return label && href ? [{ label, href, position: position - 1 }] : [];
  });
  const input = normalizeChannelSiteInput({
    ...defaults,
    name,
    domain,
    publicUrl,
    title: importValue(record, "title") || name || defaults.title,
    description: importValue(record, "description") || defaults.description,
    overviewLabel: importValue(record, "overview_label") || defaults.overviewLabel,
    recommendLabel: importValue(record, "recommend_label") || defaults.recommendLabel,
    logoMark: importValue(record, "logo_mark") || name.slice(0, 1) || defaults.logoMark,
    qpsLimit,
    status: normalizeImportStatus(importValue(record, "status")),
    modules: parseImportModules(importValue(record, "modules")),
    copy: {
      homeIntro: importValue(record, "home_intro") || defaults.copy.homeIntro,
      recommendIntro: importValue(record, "recommend_intro") || defaults.copy.recommendIntro,
      footerText: importValue(record, "footer_text") || defaults.copy.footerText
    },
    seo: {
      title: importValue(record, "title") || name || defaults.seo.title,
      description: importValue(record, "description") || defaults.seo.description,
      canonicalUrl: importValue(record, "canonical_url") || publicUrl || defaults.seo.canonicalUrl
    },
    navItems
  });
  const errors: string[] = [];
  if (!input.name) errors.push("缺少 site_name");
  if (!input.domain) errors.push("缺少 domain");
  if (!input.publicUrl) errors.push("缺少 public_url");
  if (!/^https?:\/\//i.test(input.publicUrl)) errors.push("public_url 需要以 http:// 或 https:// 开头");
  if (importValue(record, "modules") && !Object.values(input.modules).some(Boolean)) errors.push("modules 没有匹配到可用模块");
  return { rowNumber, input, errors };
}

function importValue(record: Record<string, string>, key: string) {
  return String(record[key] ?? "").trim();
}

function normalizeImportStatus(value: string) {
  const status = value.trim().toLowerCase();
  return status === "paused" ? "paused" : "active";
}

function parseImportNumber(value: string, fallback: number, min: number, max: number) {
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return fallback;
  return Math.min(max, Math.max(min, Math.floor(parsed)));
}

function parseImportModules(value: string): ChannelSiteInput["modules"] {
  const defaults = channelSiteDefaults().modules;
  if (!value.trim()) return { ...defaults };
  const modules: ChannelSiteInput["modules"] = { overview: false, channelBoard: false, recommend: false, providerRank: false, strategy: false };
  value.split(/[;,，、|/\n]+/).map((item) => item.trim()).filter(Boolean).forEach((item) => {
    const key = item.toLowerCase().replace(/[\s-]+/g, "_");
    const moduleKey = channelSiteModuleImportMap[key] || channelSiteModuleImportMap[item];
    if (moduleKey) modules[moduleKey] = true;
  });
  return modules;
}

function formatInt(value: number) {
  if (value >= 1000000) return `${(value / 1000000).toFixed(1)}M`;
  if (value >= 1000) return `${(value / 1000).toFixed(1)}K`;
  return String(value);
}

function formatStat(value: string | number) {
  return typeof value === "number" ? formatInt(value) : value;
}

function formatBytes(value: number) {
  if (value >= 1024 * 1024) return `${(value / 1024 / 1024).toFixed(1)} MB`;
  if (value >= 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${value} B`;
}

function timeLabel(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "-" : date.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

function initialSection(): OpenAPISection {
  const value = new URLSearchParams(window.location.search).get("section");
  return value === "channel-sites" ? "channel-sites" : "open-api";
}

function initialOpenAPIFilter(key: keyof OpenAPIFilters, fallback: string) {
  const value = new URLSearchParams(window.location.search).get(key)?.trim() || "";
  if (!value) return fallback;
  if (key === "status" && !["all", "active", "paused", "revoked"].includes(value)) return fallback;
  if (key === "scope" && !["all", ...scopes.map((scope) => scope.id)].includes(value)) return fallback;
  return value;
}

function syncOpenAPIFiltersToURL(filters: OpenAPIFilters, section: OpenAPISection) {
  const params = new URLSearchParams(window.location.search);
  for (const key of ["q", "status", "scope", "section"]) {
    params.delete(key);
  }
  if (section === "channel-sites") params.set("section", "channel-sites");
  if (filters.q) params.set("q", filters.q);
  if (filters.status !== "all") params.set("status", filters.status);
  if (filters.scope !== "all") params.set("scope", filters.scope);
  const nextSearch = params.toString();
  const nextURL = `${window.location.pathname}${nextSearch ? `?${nextSearch}` : ""}${window.location.hash}`;
  window.history.replaceState(null, "", nextURL);
}
