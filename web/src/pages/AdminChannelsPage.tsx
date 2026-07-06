import { FormEvent, useEffect, useMemo, useState } from "react";
import { AdminShell } from "../components/AdminShell";
import {
  AdminChannelImportResult,
  AdminChannelSyncLog,
  AdminPlatformChannel,
  AdminPlatformChannelInput,
  MonitorModelConfig,
  addAdminChannelRecommendation,
  adminSettings,
  adminChannels,
  adminProbeNow,
  bulkAdminChannels,
  createAdminChannel,
  deleteAdminChannel,
  disableAdminChannel,
  exportAdminChannels,
  fetchAdminChannelIntro,
  importAdminChannels,
  PrivateChannelSummary,
  publicChannelPath,
  removeAdminChannelRecommendation,
  rotateAdminChannelCredential,
  syncAdminChannelsFromAPI,
  updateAdminChannel,
  validateAdminChannel
} from "../lib/api";
import { ActionBar, BulkActionBar, Button, DataTable, DataTableColumn, Dialog, FilterBar, PageHeader, SelectField, StatGrid } from "../ui";

const statusClass: Record<string, string> = {
  healthy: "b-green",
  degraded: "b-amber",
  functional_down: "b-magenta",
  connectivity_down: "b-red",
  auth_error: "b-red",
  disabled: "b-gray",
  unknown: "b-gray"
};

const defaultProviderConfig = {
  temperature: 0.2,
  maxTokens: 64,
  timeoutMs: 60000
};

const emptyForm: AdminPlatformChannelInput = {
  name: "",
  provider: "OpenAI",
  type: "openai-compatible",
  model: "",
  upstreamModel: "",
  endpoint: "",
  officialSiteUrl: "",
  introTitle: "",
  introSummary: "",
  introBody: "",
  introHighlights: [],
  logoUrl: "",
  introSourceUrl: "",
  apiKey: "",
  probeDaily: 2880,
  publicVisible: true,
  gatewayEnabled: true,
  enabled: true,
  inputPerMtok: 0,
  outputPerMtok: 0,
  providerConfig: defaultProviderConfig
};

const defaultMonitorModels: MonitorModelConfig[] = [
  {
    key: "claude-sonnet-4-6",
    label: "Claude Sonnet 4.6",
    model: "claude-sonnet-4-6",
    upstreamModel: "claude-sonnet-4-6",
    type: "anthropic",
    enabled: true,
    defaultSelected: true,
    inputPerMtok: 3,
    outputPerMtok: 15,
    aliases: ["claude-sonnet-4-6", "claude-sonnet-4.6", "claude-4-6-sonnet"]
  },
  {
    key: "gpt-5.5",
    label: "GPT-5.5",
    model: "gpt-5.5",
    upstreamModel: "gpt-5.5",
    type: "openai-compatible",
    enabled: true,
    defaultSelected: true,
    inputPerMtok: 5,
    outputPerMtok: 30,
    aliases: ["gpt-5.5", "gpt-5-5", "gpt55"]
  }
];

const CHANNEL_PAGE_SIZE = 10;

export function AdminChannelsPage() {
  const [channels, setChannels] = useState<AdminPlatformChannel[]>([]);
  const [privateItems, setPrivateItems] = useState<PrivateChannelSummary[]>([]);
  const [tab, setTab] = useState<"platform" | "private">("platform");
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState("all");
  const [view, setView] = useState<"all" | "public" | "gateway" | "disabled">("all");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [saving, setSaving] = useState(false);
  const [introFetching, setIntroFetching] = useState(false);
  const [actionID, setActionID] = useState("");
  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<AdminPlatformChannel | null>(null);
  const [form, setForm] = useState<AdminPlatformChannelInput>(emptyForm);
  const [providerConfigText, setProviderConfigText] = useState(prettyJSON(defaultProviderConfig));
  const [credentialChannel, setCredentialChannel] = useState<AdminPlatformChannel | null>(null);
  const [credentialKey, setCredentialKey] = useState("");
  const [exportOpen, setExportOpen] = useState(false);
  const [exportPassword, setExportPassword] = useState("");
  const [exporting, setExporting] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  const [importPassword, setImportPassword] = useState("");
  const [importFile, setImportFile] = useState<File | null>(null);
  const [importing, setImporting] = useState(false);
  const [importResults, setImportResults] = useState<AdminChannelImportResult[]>([]);
  const [syncOpen, setSyncOpen] = useState(false);
  const [syncBaseUrl, setSyncBaseUrl] = useState("");
  const [syncSiteKey, setSyncSiteKey] = useState("");
  const [syncPassword, setSyncPassword] = useState("");
  const [syncing, setSyncing] = useState(false);
  const [syncResults, setSyncResults] = useState<AdminChannelImportResult[]>([]);
  const [syncLogs, setSyncLogs] = useState<AdminChannelSyncLog[]>([]);
  const [syncSource, setSyncSource] = useState("");
  const [selectedIDs, setSelectedIDs] = useState<string[]>([]);
  const [monitorModels, setMonitorModels] = useState<MonitorModelConfig[]>(defaultMonitorModels);
  const [selectedMonitorModelKeys, setSelectedMonitorModelKeys] = useState<string[]>(defaultSelectedMonitorModelKeys(defaultMonitorModels));
  const [channelPage, setChannelPage] = useState(1);
  const [privatePage, setPrivatePage] = useState(1);

  useEffect(() => {
    void load();
  }, []);

  async function load() {
    setLoading(true);
    setError("");
    try {
      const [payload, settings] = await Promise.all([adminChannels(), adminSettings()]);
      const nextMonitorModels = normalizeMonitorModels(settings.site.monitorModels);
      setChannels(payload.items ?? []);
      setPrivateItems(payload.private ?? []);
      setMonitorModels(nextMonitorModels);
      setSelectedMonitorModelKeys((current) => reconcileSelectedMonitorKeys(current, nextMonitorModels));
      setSelectedIDs((current) => current.filter((id) => (payload.items ?? []).some((item) => item.id === id)));
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载失败");
    } finally {
      setLoading(false);
    }
  }

  function replaceChannel(channel: AdminPlatformChannel) {
    setChannels((current) => current.map((item) => (item.id === channel.id ? channel : item)));
  }

  function openCreate() {
    setEditing(null);
    setForm(emptyForm);
    setSelectedMonitorModelKeys(defaultSelectedMonitorModelKeys(monitorModels));
    setProviderConfigText(prettyJSON(defaultProviderConfig));
    setEditorOpen(true);
  }

  function openEdit(channel: AdminPlatformChannel) {
    setEditing(channel);
    setForm({
      name: channel.name,
      provider: channel.provider,
      type: channel.type,
      model: channel.model,
      upstreamModel: channel.upstreamModel,
      endpoint: channel.endpoint,
      officialSiteUrl: channel.officialSiteUrl ?? "",
      introTitle: channel.introTitle ?? "",
      introSummary: channel.introSummary ?? "",
      introBody: channel.introBody ?? "",
      introHighlights: channel.introHighlights ?? [],
      logoUrl: channel.logoUrl ?? "",
      introSourceUrl: channel.introSourceUrl ?? channel.officialSiteUrl ?? "",
      apiKey: "",
      probeDaily: channel.probeDaily,
      publicVisible: channel.publicVisible,
      gatewayEnabled: channel.gatewayEnabled,
      enabled: channel.status !== "disabled",
      inputPerMtok: channel.inputPerMtok,
      outputPerMtok: channel.outputPerMtok,
      providerConfig: channel.providerConfig ?? {}
    });
    setProviderConfigText(prettyJSON(channel.providerConfig ?? {}));
    setEditorOpen(true);
  }

  async function submitChannel(event: FormEvent) {
    event.preventDefault();
    setError("");
    const selectedModels = editing ? [] : selectedMonitorModels(monitorModels, selectedMonitorModelKeys);
    if (!editing && selectedModels.length === 0) {
      setError("至少选择一个监控模型");
      return;
    }
    setSaving(true);
    try {
      const providerConfig = parseProviderConfig(providerConfigText);
      const payload = { ...form, providerConfig, monitorModels: editing ? undefined : selectedModels };
      if (editing) {
        const { channel } = await updateAdminChannel(editing.id, { ...payload, apiKey: undefined });
        replaceChannel(channel);
      } else {
        const { channel, channels: createdChannels } = await createAdminChannel(payload);
        const created = createdChannels?.length ? createdChannels : [channel];
        setChannels((current) => [...created, ...current]);
        setChannelPage(1);
      }
      setEditorOpen(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存失败");
    } finally {
      setSaving(false);
    }
  }

  async function fetchIntroDraft() {
    if (!editing) return;
    const sourceUrl = (form.introSourceUrl || form.officialSiteUrl || "").trim();
    if (!sourceUrl) {
      setError("请先填写官网链接或介绍来源 URL");
      return;
    }
    setIntroFetching(true);
    setError("");
    setNotice("");
    try {
      const { draft } = await fetchAdminChannelIntro(editing.id, sourceUrl);
      setForm((current) => ({
        ...current,
        introTitle: draft.introTitle || current.introTitle,
        introSummary: draft.introSummary || current.introSummary,
        introBody: draft.introBody || current.introBody,
        introHighlights: draft.introHighlights ?? current.introHighlights,
        logoUrl: draft.logoUrl || current.logoUrl,
        introSourceUrl: draft.introSourceUrl || sourceUrl,
        officialSiteUrl: current.officialSiteUrl || draft.introSourceUrl || sourceUrl
      }));
      setNotice("已从官网抓取介绍草稿，请检查后保存通道。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "官网介绍抓取失败");
    } finally {
      setIntroFetching(false);
    }
  }

  async function submitCredential(event: FormEvent) {
    event.preventDefault();
    if (!credentialChannel) return;
    setSaving(true);
    setError("");
    try {
      const { channel } = await rotateAdminChannelCredential(credentialChannel.id, credentialKey);
      replaceChannel(channel);
      setCredentialChannel(null);
      setCredentialKey("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "凭据轮换失败");
    } finally {
      setSaving(false);
    }
  }

  async function submitExport(event: FormEvent) {
    event.preventDefault();
    if (!exportPassword.trim()) {
      setError("请输入当前管理员密码");
      return;
    }
    setExporting(true);
    setError("");
    setNotice("");
    try {
      const payload = await exportAdminChannels(exportPassword);
      downloadBlob(payload.blob, payload.filename);
      setExportOpen(false);
      setExportPassword("");
      setNotice("平台通道 CSV 已导出，文件包含明文 API Key，请妥善保存");
    } catch (err) {
      setError(err instanceof Error ? err.message : "导出失败");
    } finally {
      setExporting(false);
    }
  }

  async function submitImport(event: FormEvent) {
    event.preventDefault();
    if (!importFile) {
      setError("请选择 CSV 文件");
      return;
    }
    if (!importPassword.trim()) {
      setError("请输入当前管理员密码");
      return;
    }
    setImporting(true);
    setError("");
    setNotice("");
    try {
      const payload = await importAdminChannels(importPassword, importFile);
      await load();
      setSelectedIDs([]);
      setImportResults(payload.rows ?? []);
      setImportPassword("");
      setImportFile(null);
      setChannelPage(1);
      setNotice(`导入完成：新增 ${payload.created} 个，更新 ${payload.updated} 个`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "导入失败");
    } finally {
      setImporting(false);
    }
  }

  async function submitSync(event: FormEvent) {
    event.preventDefault();
    if (!syncBaseUrl.trim()) {
      setError("请输入源站 Open API Base URL");
      return;
    }
    if (!syncSiteKey.trim()) {
      setError("请输入通道同步 API 的 Site Key");
      return;
    }
    if (!syncPassword.trim()) {
      setError("请输入当前管理员密码");
      return;
    }
    setSyncing(true);
    setError("");
    setNotice("");
    try {
      const payload = await syncAdminChannelsFromAPI({
        baseUrl: syncBaseUrl.trim(),
        siteKey: syncSiteKey.trim(),
        password: syncPassword
      });
      await load();
      setSelectedIDs([]);
      setSyncResults(payload.rows ?? []);
      setSyncLogs(payload.logs ?? []);
      setSyncSource(payload.source || payload.endpoint);
      setSyncPassword("");
      setSyncSiteKey("");
      setChannelPage(1);
      setNotice(`API 调用完成：新增 ${payload.created} 个，更新 ${payload.updated} 个，拉取监控日志 ${payload.logs?.length ?? 0} 条`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "API 调用失败");
    } finally {
      setSyncing(false);
    }
  }

  async function runAction(channel: AdminPlatformChannel, kind: "probe" | "validate" | "enable" | "disable" | "delete" | "recommend" | "unrecommend") {
    if (kind === "enable" && !window.confirm(`确认启用 ${channel.name}？启用后会恢复前台公开和网关候选。`)) return;
    if (kind === "disable" && !window.confirm(`确认禁用 ${channel.name}？禁用后前台和网关候选都会隐藏。`)) return;
    if (kind === "delete" && !window.confirm(`确认删除 ${channel.name}？历史探测和审计会保留，但该通道不会再出现在前台和网关候选。`)) return;
    setActionID(`${kind}:${channel.id}`);
    setError("");
    try {
      if (kind === "probe") {
        await adminProbeNow(channel.id);
        await load();
      }
      if (kind === "validate") {
        const { channel: updated } = await validateAdminChannel(channel.id);
        replaceChannel(updated);
      }
      if (kind === "enable") {
        const { items } = await bulkAdminChannels({ action: "status", status: "active", ids: [channel.id] });
        const updated = items.find((item) => item.id === channel.id);
        if (updated) replaceChannel(updated);
      }
      if (kind === "disable") {
        const { channel: updated } = await disableAdminChannel(channel.id);
        replaceChannel(updated);
      }
      if (kind === "recommend") {
        const { channel: updated } = await addAdminChannelRecommendation(channel.id);
        replaceChannel(updated);
      }
      if (kind === "unrecommend") {
        const { channel: updated } = await removeAdminChannelRecommendation(channel.id);
        replaceChannel(updated);
      }
      if (kind === "delete") {
        await deleteAdminChannel(channel.id);
        setChannels((current) => current.filter((item) => item.id !== channel.id));
        setSelectedIDs((current) => current.filter((id) => id !== channel.id));
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "操作失败");
    } finally {
      setActionID("");
    }
  }

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return channels.filter((channel) => {
      if (status !== "all" && channel.status !== status) return false;
      if (view === "public" && !channel.publicVisible) return false;
      if (view === "gateway" && !channel.gatewayEnabled) return false;
      if (view === "disabled" && channel.status !== "disabled") return false;
      if (!q) return true;
      return `${channel.name} ${channel.provider} ${channel.model} ${channel.upstreamModel} ${channel.endpoint} ${channel.officialSiteUrl}`.toLowerCase().includes(q);
    });
  }, [channels, query, status, view]);

  const privateFiltered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return privateItems.filter((channel) => {
      if (status !== "all" && channel.status !== status) return false;
      if (!q) return true;
      return `${channel.name} ${channel.ownerEmail} ${channel.provider} ${channel.model} ${channel.endpoint}`.toLowerCase().includes(q);
    });
  }, [privateItems, query, status]);

  const filteredIDs = useMemo(() => filtered.map((channel) => channel.id), [filtered]);
  const selectedSet = useMemo(() => new Set(selectedIDs), [selectedIDs]);
  const selectedVisibleIDs = useMemo(() => filteredIDs.filter((id) => selectedSet.has(id)), [filteredIDs, selectedSet]);
  const allVisibleSelected = filteredIDs.length > 0 && selectedVisibleIDs.length === filteredIDs.length;
  const channelTotalPages = Math.max(1, Math.ceil(filtered.length / CHANNEL_PAGE_SIZE));
  const channelCurrentPage = Math.min(channelPage, channelTotalPages);
  const channelPageStart = (channelCurrentPage - 1) * CHANNEL_PAGE_SIZE;
  const pagedChannels = filtered.slice(channelPageStart, channelPageStart + CHANNEL_PAGE_SIZE);
  const privateTotalPages = Math.max(1, Math.ceil(privateFiltered.length / CHANNEL_PAGE_SIZE));
  const privateCurrentPage = Math.min(privatePage, privateTotalPages);
  const privatePageStart = (privateCurrentPage - 1) * CHANNEL_PAGE_SIZE;
  const pagedPrivateChannels = privateFiltered.slice(privatePageStart, privatePageStart + CHANNEL_PAGE_SIZE);

  useEffect(() => {
    setChannelPage(1);
    setPrivatePage(1);
  }, [query, status, view, tab]);

  useEffect(() => {
    setChannelPage((current) => Math.min(Math.max(current, 1), channelTotalPages));
  }, [channelTotalPages]);

  useEffect(() => {
    setPrivatePage((current) => Math.min(Math.max(current, 1), privateTotalPages));
  }, [privateTotalPages]);

  function toggleChannel(channelID: string, checked: boolean) {
    setSelectedIDs((current) => {
      if (checked) return current.includes(channelID) ? current : [...current, channelID];
      return current.filter((id) => id !== channelID);
    });
  }

  function toggleVisible(checked: boolean) {
    setSelectedIDs((current) => {
      if (!checked) return current.filter((id) => !filteredIDs.includes(id));
      const merged = new Set([...current, ...filteredIDs]);
      return Array.from(merged);
    });
  }

  async function runBulkAction(kind: "enable" | "disable" | "delete") {
    const ids = selectedIDs.filter((id) => channels.some((channel) => channel.id === id));
    if (!ids.length) {
      setError("请先选择平台通道");
      return;
    }
    if (kind === "enable" && !window.confirm(`确认批量启用 ${ids.length} 个平台通道？启用后会恢复前台公开和网关候选。`)) return;
    if (kind === "disable" && !window.confirm(`确认批量禁用 ${ids.length} 个平台通道？禁用后会从前台和网关候选隐藏。`)) return;
    if (kind === "delete" && !window.confirm(`确认批量删除 ${ids.length} 个平台通道？历史探测和审计会保留，凭据密文会被清理。`)) return;
    setActionID(`bulk:${kind}`);
    setError("");
    try {
      const payload = kind === "enable"
        ? await bulkAdminChannels({ action: "status", status: "active", ids })
        : kind === "disable"
          ? await bulkAdminChannels({ action: "status", status: "disabled", ids })
          : await bulkAdminChannels({ action: "delete", ids });
      setChannels(payload.items ?? []);
      setSelectedIDs([]);
    } catch (err) {
      setError(err instanceof Error ? err.message : "批量操作失败");
    } finally {
      setActionID("");
    }
  }

  const healthy = channels.filter((item) => item.status === "healthy").length;
  const down = channels.filter((item) => item.status === "connectivity_down" || item.status === "auth_error" || item.status === "functional_down").length;
  const degraded = channels.filter((item) => item.status === "degraded").length;
  const disabled = channels.filter((item) => item.status === "disabled").length;
  const privateHealthy = privateItems.filter((item) => item.status === "healthy").length;
  const privateDown = privateItems.filter((item) => item.status === "connectivity_down" || item.status === "auth_error" || item.status === "functional_down").length;
  const channelColumns: DataTableColumn<AdminPlatformChannel>[] = [
    {
      id: "select",
      header: () => (
        <input
          type="checkbox"
          aria-label="选择当前筛选的平台通道"
          checked={allVisibleSelected}
          disabled={!filtered.length}
          onChange={(event) => toggleVisible(event.target.checked)}
        />
      ),
      cell: ({ row }) => (
        <input
          type="checkbox"
          aria-label={`选择 ${row.original.name}`}
          checked={selectedSet.has(row.original.id)}
          onChange={(event) => toggleChannel(row.original.id, event.target.checked)}
        />
      ),
      meta: { width: "42px", align: "center", className: "channel-select-cell", headerClassName: "channel-select-header" }
    },
    {
      id: "channel",
      header: "通道 / 服务商",
      cell: ({ row }) => {
        const displayName = channelListDisplayName(row.original);
        return (
          <div className="ch-name channel-identity">
            <span className="mark" style={{ background: row.original.mark }}>{row.original.provider[0]}</span>
            <div><b title={row.original.name}>{displayName}</b><small title={row.original.endpoint}>{row.original.provider} · {row.original.endpoint}</small></div>
          </div>
        );
      },
      meta: { width: "22%", className: "channel-identity-cell", headerClassName: "channel-identity-header" }
    },
    {
      id: "model",
      header: "上游模型",
      cell: ({ row }) => <div className="num channel-model-cell"><b title={row.original.upstreamModel || row.original.model}>{row.original.upstreamModel || row.original.model}</b><small>{row.original.type}</small></div>,
      meta: { width: "12%", truncate: true, className: "channel-model-wrap", headerClassName: "channel-model-header" }
    },
    {
      id: "status",
      header: "综合状态",
      cell: ({ row }) => (
        <div className="channel-status-stack">
          <span className={`badge ${statusClass[row.original.status] ?? "b-gray"} dot`}>{statusText(row.original.status)}</span>
          <small className={diagnosisClass(row.original.diagnosis?.severity)} title={row.original.diagnosis?.hint || row.original.diagnosis?.label}>
            {row.original.diagnosis?.label || "等待诊断"}
          </small>
        </div>
      ),
      meta: { width: "9%", className: "channel-status-cell", headerClassName: "channel-status-header" }
    },
    {
      id: "probe",
      header: "分层探测",
      cell: ({ row }) => (
        <div className="channel-probe-grid">
          <span className="channel-probe-step">
            <small>L1</small>
            <Layer status={row.original.l1Status} latency={row.original.l1LatencyMs} />
          </span>
          <span className="channel-probe-step">
            <small>L2</small>
            <Layer status={row.original.l2Status} latency={row.original.l2LatencyMs} />
          </span>
          <span className="channel-probe-step">
            <small>L3</small>
            <Layer status={row.original.l3Status} latency={row.original.l3LatencyMs} />
          </span>
        </div>
      ),
      meta: { width: "20%", className: "channel-probe-cell", headerClassName: "channel-probe-header" }
    },
    {
      id: "credential",
      header: "凭据",
      cell: ({ row }) => row.original.keyMask ? <span className="credential-mask" title={row.original.keyFingerprint}>{row.original.keyMask}</span> : <span className="muted-time">未配置</span>,
      meta: { width: "8%", truncate: true, className: "credential-cell", headerClassName: "credential-header" }
    },
    {
      id: "flags",
      header: "公开/网关",
      cell: ({ row }) => (
        <div className="channel-flags">
          <span className={`badge ${row.original.publicVisible ? "b-blue" : "b-gray"} dot`}>前台</span>
          <span className={`badge ${row.original.gatewayEnabled ? "b-green" : "b-gray"} dot`}>网关</span>
        </div>
      ),
      meta: { width: "8%", className: "channel-flags-cell", headerClassName: "channel-flags-header" }
    },
    {
      id: "actions",
      header: "操作",
      cell: ({ row }) => {
        const channel = row.original;
        const recommendAction = channel.recommended ? "unrecommend" : "recommend";
        const recommendActionID = `${recommendAction}:${channel.id}`;
        const recommendDisabled = actionID === recommendActionID || (!channel.recommended && (!channel.publicVisible || channel.status === "disabled"));
        return (
          <div className="channel-row-actions phase14-channel-actions">
            <Button
              variant="ghost"
              size="sm"
              disabled={recommendDisabled}
              onClick={() => void runAction(channel, recommendAction)}
            >
              {actionID === recommendActionID ? "处理中" : channel.recommended ? "取消推荐" : "加入推荐"}
            </Button>
            <Button variant="ghost" size="sm" onClick={() => openEdit(channel)}>编辑</Button>
            {channel.publicVisible && channel.status !== "disabled" ? <a className="btn btn-ghost btn-sm" href={publicChannelPath(channel)}>详情</a> : null}
            <details className="channel-action-details">
              <summary className="btn btn-ghost btn-sm" role="button" aria-label={`${channel.name} 更多运维操作`}>更多</summary>
              <div className="channel-action-panel" role="group" aria-label={`${channel.name} 运维操作`}>
                <Button variant="ghost" size="sm" disabled={actionID === `validate:${channel.id}`} onClick={() => void runAction(channel, "validate")}>
                  {actionID === `validate:${channel.id}` ? "验证中" : "验证上游"}
                </Button>
                <Button variant="ghost" size="sm" disabled={actionID === `probe:${channel.id}`} onClick={() => void runAction(channel, "probe")}>
                  {actionID === `probe:${channel.id}` ? "探测中" : "立即探测"}
                </Button>
                <Button variant="ghost" size="sm" onClick={() => { setCredentialChannel(channel); setCredentialKey(""); }}>轮换 Key</Button>
                {channel.status === "disabled" ? (
                  <Button variant="ghost" size="sm" disabled={actionID === `enable:${channel.id}`} onClick={() => void runAction(channel, "enable")}>
                    {actionID === `enable:${channel.id}` ? "启用中" : "启用通道"}
                  </Button>
                ) : (
                  <Button variant="danger" size="sm" onClick={() => void runAction(channel, "disable")}>禁用通道</Button>
                )}
                <Button variant="danger" size="sm" onClick={() => void runAction(channel, "delete")}>删除通道</Button>
              </div>
            </details>
          </div>
        );
      },
      meta: { width: "20%", align: "end", className: "actions channel-actions-cell", headerClassName: "channel-actions-header" }
    }
  ];
  const privateColumns: DataTableColumn<PrivateChannelSummary>[] = [
    {
      id: "channel",
      header: "通道名 / 端点",
      cell: ({ row }) => (
        <div className="ch-name private-channel-cell">
          <span className="mark" style={{ background: providerMark(row.original.provider) }}>{row.original.provider[0]}</span>
          <div>
            <b title={row.original.name}>{row.original.name}</b>
            <small title={row.original.endpoint}>{row.original.endpoint}</small>
          </div>
        </div>
      ),
      meta: { width: "38%" }
    },
    { id: "owner", header: "归属用户", cell: ({ row }) => <span title={row.original.ownerEmail}>{row.original.ownerEmail}</span>, meta: { width: "24%", truncate: true } },
    {
      id: "status",
      header: "状态 / 模型",
      cell: ({ row }) => (
        <div className="private-stack private-status-model-cell">
          <div className="private-badge-line">
            <span className="badge b-blue dot">私有</span>
            <span className={`badge ${statusClass[row.original.status] ?? "b-gray"} dot`}>{statusText(row.original.status)}</span>
          </div>
          <small className="private-model" title={row.original.model}>{row.original.model}</small>
        </div>
      ),
      meta: { width: "20%" }
    },
    {
      id: "probe",
      header: "探测",
      cell: ({ row }) => (
        <div className="private-stack private-probe-cell">
          <span className={`quota-tag ${quotaClass(row.original.probesUsedToday, row.original.probeDaily)}`}>{row.original.probesUsedToday} / {row.original.probeDaily}</span>
          <small>最后 {timeLabel(row.original.lastProbeAt)}</small>
        </div>
      ),
      meta: { width: "12%" }
    },
    { id: "readonly", header: "操作", cell: () => <span className="muted-time">只读</span>, meta: { width: "6%", align: "end" } }
  ];

  return (
    <AdminShell title="通道接入" crumb="/ 上游管理">
      <PageHeader
        description={<p className="page-intro">这里接入并管理平台公共通道。平台通道可被各工作区专属中转站引用，每个通道独立配置分层探测，结果实时同步到前台监控看板</p>}
        actions={tab === "platform" ? (
          <>
            <Button variant="ghost" size="sm" onClick={() => { setSyncResults([]); setSyncLogs([]); setSyncSource(""); setSyncOpen(true); }}>API 调用</Button>
            <Button variant="ghost" size="sm" onClick={() => { setImportResults([]); setImportOpen(true); }}>导入 CSV</Button>
            <Button variant="ghost" size="sm" onClick={() => setExportOpen(true)}>导出 CSV</Button>
            <Button variant="primary" size="sm" onClick={openCreate}>＋ 新增平台通道</Button>
          </>
        ) : null}
      />

      {error ? <div className="form-error">{error}</div> : null}
      {notice ? <div className="form-notice">{notice}</div> : null}
      <StatGrid
        className="admin-channel-stats"
        items={[
          { label: "平台通道", value: channels.length, hint: "真实录入，不依赖生产 seed" },
          { label: "健康", value: healthy, hint: "可公开和入网关", tone: "green" },
          { label: "降级", value: degraded, hint: "需要观察", tone: "amber" },
          { label: "故障", value: down, hint: "自动摘除候选", tone: "red" },
          { label: "已禁用", value: disabled, hint: "前台与网关隐藏" }
        ]}
      />

      <ActionBar align="start" className="channel-head-actions">
        <div className="channel-mode-group">
          <div className="tabs">
            <button className={`tab ${tab === "platform" ? "active" : ""}`} onClick={() => setTab("platform")}>平台通道 · {channels.length}</button>
            <button className={`tab ${tab === "private" ? "active" : ""}`} onClick={() => setTab("private")}>全站用户私有通道 · {privateItems.length}</button>
          </div>
          <span className="channel-mode-hint">{tab === "platform" ? "公共通道用于前台监控和工作区网关" : "展示所有用户创建的私有通道汇总，不等同于当前登录用户配置，不回显用户密钥"}</span>
        </div>
      </ActionBar>

      <FilterBar className="channel-filter-toolbar">
        <SelectField value={status} onChange={(event) => setStatus(event.target.value)} aria-label="status">
          <option value="all">状态：全部</option>
          <option value="healthy">状态：健康</option>
          <option value="degraded">状态：降级</option>
          <option value="auth_error">状态：认证异常</option>
          <option value="functional_down">状态：功能故障</option>
          <option value="connectivity_down">状态：连通异常</option>
          <option value="disabled">状态：已禁用</option>
          <option value="unknown">状态：未知</option>
        </SelectField>
        <div className="tb-search">
          <span>⌕</span>
          <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索通道 / 上游模型…" />
        </div>
        {tab === "platform" ? (
          <div className="seg">
            <button className={view === "all" ? "active" : ""} onClick={() => setView("all")}>全部</button>
            <button className={view === "public" ? "active" : ""} onClick={() => setView("public")}>前台</button>
            <button className={view === "gateway" ? "active" : ""} onClick={() => setView("gateway")}>网关</button>
            <button className={view === "disabled" ? "active" : ""} onClick={() => setView("disabled")}>禁用</button>
          </div>
        ) : null}
      </FilterBar>

      {tab === "platform" && selectedIDs.length ? (
        <BulkActionBar className="bulk-toolbar">
          <span>已选择 {selectedIDs.length} 个平台通道</span>
          <Button variant="ghost" size="sm" disabled={actionID === "bulk:enable"} onClick={() => void runBulkAction("enable")}>
            {actionID === "bulk:enable" ? "启用中..." : "批量启用"}
          </Button>
          <Button variant="ghost" size="sm" disabled={actionID === "bulk:disable"} onClick={() => void runBulkAction("disable")}>
            {actionID === "bulk:disable" ? "禁用中..." : "批量禁用"}
          </Button>
          <Button variant="danger" size="sm" disabled={actionID === "bulk:delete"} onClick={() => void runBulkAction("delete")}>
            {actionID === "bulk:delete" ? "删除中..." : "批量删除"}
          </Button>
          <Button variant="ghost" size="sm" onClick={() => setSelectedIDs([])}>取消选择</Button>
        </BulkActionBar>
      ) : null}

      {tab === "platform" ? (
        <DataTable
          data={filtered}
          columns={channelColumns}
          loading={loading}
          pageSize={10}
          cardClassName="admin-channel-card"
          tableClassName="admin-channel-table"
          rowKey={(row) => row.id}
          loadingText="正在加载通道..."
          empty={<div className="empty-state"><div className="ico">＋</div><h4>暂无平台通道</h4><p>生产 seed 不会自动创建示例通道。录入真实上游、保存加密凭据并验证后，前台监控会展示真实状态。</p><Button variant="primary" size="sm" onClick={openCreate}>新增平台通道</Button></div>}
          footerNote={`${healthy} 健康 · ${degraded} 降级 · ${down} 故障 · Key 只保存密文、nonce、fingerprint、mask`}
        />
      ) : (
        <>
          <StatGrid
            className="private-channel-stats"
            items={[
              { label: "全站用户私有通道", value: privateItems.length, hint: "仅查看，不可修改用户配置" },
              { label: "健康", value: privateHealthy, hint: "私有 L3 最近结果", tone: "green" },
              { label: "异常", value: privateDown, hint: "由用户自行处理", tone: "red" },
              { label: "今日探测", value: privateItems.reduce((sum, item) => sum + item.probesUsedToday, 0), hint: "跨用户配额使用", tone: "amber" },
              { label: "Key 明文", value: "0", hint: "后台不可读取用户密钥" }
            ]}
          />
          <DataTable
            data={privateFiltered}
            columns={privateColumns}
            loading={loading}
            pageSize={10}
            cardClassName="admin-private-card"
            tableClassName="admin-private-table"
            rowKey={(row) => row.id}
            loadingText="正在加载私有通道..."
            empty={<div className="empty-state"><div className="ico">＋</div><h4>还没有用户私有通道</h4><p>注册用户在控制台添加私有通道后，会在这里以只读汇总形式展示。</p></div>}
            footerNote={`管理员仅查看汇总，不可读取 API Key · 异常通道：${privateDown} 个`}
          />
        </>
      )}

      <PlatformEditor
        open={editorOpen}
        editing={editing}
        form={form}
        setForm={setForm}
        providerConfigText={providerConfigText}
        setProviderConfigText={setProviderConfigText}
        saving={saving}
        introFetching={introFetching}
        monitorModels={monitorModels}
        selectedMonitorModelKeys={selectedMonitorModelKeys}
        onToggleMonitorModel={(key) => {
          setSelectedMonitorModelKeys((current) => (current.includes(key) ? current.filter((item) => item !== key) : [...current, key]));
        }}
        onSubmit={submitChannel}
        onFetchIntro={fetchIntroDraft}
        onClose={() => setEditorOpen(false)}
      />
      <ChannelExportDialog
        open={exportOpen}
        password={exportPassword}
        setPassword={setExportPassword}
        exporting={exporting}
        onSubmit={submitExport}
        onOpenChange={(open) => {
          setExportOpen(open);
          if (!open) setExportPassword("");
        }}
      />
      <ChannelImportDialog
        open={importOpen}
        password={importPassword}
        setPassword={setImportPassword}
        file={importFile}
        setFile={setImportFile}
        importing={importing}
        results={importResults}
        onSubmit={submitImport}
        onOpenChange={(open) => {
          setImportOpen(open);
          if (!open) {
            setImportPassword("");
            setImportFile(null);
          }
        }}
      />
      <ChannelSyncDialog
        open={syncOpen}
        baseUrl={syncBaseUrl}
        setBaseUrl={setSyncBaseUrl}
        siteKey={syncSiteKey}
        setSiteKey={setSyncSiteKey}
        password={syncPassword}
        setPassword={setSyncPassword}
        syncing={syncing}
        results={syncResults}
        logs={syncLogs}
        source={syncSource}
        onSubmit={submitSync}
        onOpenChange={(open) => {
          setSyncOpen(open);
          if (!open) {
            setSyncPassword("");
            setSyncSiteKey("");
          }
        }}
      />
      <CredentialDrawer
        channel={credentialChannel}
        apiKey={credentialKey}
        setAPIKey={setCredentialKey}
        saving={saving}
        onSubmit={submitCredential}
        onClose={() => setCredentialChannel(null)}
      />
    </AdminShell>
  );
}

function PlatformEditor({
  open,
  editing,
  form,
  setForm,
  providerConfigText,
  setProviderConfigText,
  saving,
  introFetching,
  monitorModels,
  selectedMonitorModelKeys,
  onToggleMonitorModel,
  onSubmit,
  onFetchIntro,
  onClose
}: {
  open: boolean;
  editing: AdminPlatformChannel | null;
  form: AdminPlatformChannelInput;
  setForm: (value: AdminPlatformChannelInput) => void;
  providerConfigText: string;
  setProviderConfigText: (value: string) => void;
  saving: boolean;
  introFetching: boolean;
  monitorModels: MonitorModelConfig[];
  selectedMonitorModelKeys: string[];
  onToggleMonitorModel: (key: string) => void;
  onSubmit: (event: FormEvent) => void;
  onFetchIntro: () => void;
  onClose: () => void;
}) {
  return (
    <div className={`drawer-mask ${open ? "open" : ""}`} onClick={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <form className={`drawer ${open ? "open" : ""} private-editor platform-editor`} onSubmit={onSubmit}>
        <div className="dh">
          <div>
            <h3>{editing ? "编辑平台通道" : "新增平台通道"}</h3>
            <div>录入真实上游 endpoint、探测模型和公开策略</div>
          </div>
          <button type="button" className="icon-btn" onClick={onClose}>×</button>
        </div>
        <div className="db">
          <div className="tk-form">
            <div className="row">
              <label htmlFor="pc-name">通道名称</label>
              <input id="pc-name" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} placeholder="例如：OpenAI 主线路" />
            </div>
            <div className="row">
              <label htmlFor="pc-provider">服务商</label>
              <input id="pc-provider" value={form.provider} onChange={(event) => setForm({ ...form, provider: event.target.value })} placeholder="OpenAI / Anthropic / Gemini" />
            </div>
            <div className="row">
              <label htmlFor="pc-type">Adapter</label>
              <SelectField id="pc-type" value={form.type} onChange={(event) => setForm({ ...form, type: event.target.value })}>
                <option value="openai-compatible">OpenAI 兼容</option>
                <option value="anthropic">Anthropic Messages</option>
                <option value="gemini">Gemini generateContent</option>
                <option value="mixed">混合通道</option>
              </SelectField>
            </div>
            <div className="row">
              <label htmlFor="pc-endpoint">Endpoint URL</label>
              <input id="pc-endpoint" value={form.endpoint} onChange={(event) => setForm({ ...form, endpoint: event.target.value })} placeholder="https://api.vendor.com/v1" />
            </div>
            <div className="row">
              <label htmlFor="pc-official-site-url">官网链接</label>
              <input id="pc-official-site-url" value={form.officialSiteUrl} onChange={(event) => setForm({ ...form, officialSiteUrl: event.target.value })} placeholder="https://www.vendor.com" />
            </div>
            <div className="row col channel-intro-editor">
              <label>前台介绍与官网素材</label>
              <div className="channel-intro-admin-grid">
                <div className="channel-intro-admin-fields">
                  <div className="intro-source-row">
                    <input
                      value={form.introSourceUrl}
                      onChange={(event) => setForm({ ...form, introSourceUrl: event.target.value })}
                      placeholder="介绍来源 URL，默认使用官网链接"
                      aria-label="介绍来源 URL"
                    />
                    <button
                      type="button"
                      className="btn btn-ghost btn-sm"
                      disabled={!editing || introFetching}
                      onClick={onFetchIntro}
                    >
                      {introFetching ? "抓取中..." : "从官网抓取草稿"}
                    </button>
                  </div>
                  <input
                    value={form.logoUrl}
                    onChange={(event) => setForm({ ...form, logoUrl: event.target.value })}
                    placeholder="Logo URL，可留空"
                    aria-label="Logo URL"
                  />
                  <input
                    value={form.introTitle}
                    onChange={(event) => setForm({ ...form, introTitle: event.target.value })}
                    placeholder="介绍标题，例如：AIGoCode 官方介绍"
                    aria-label="介绍标题"
                  />
                  <textarea
                    className="intro-summary-textarea"
                    value={form.introSummary}
                    onChange={(event) => setForm({ ...form, introSummary: event.target.value })}
                    placeholder="一句话摘要，会展示在介绍标题下方"
                    aria-label="介绍摘要"
                  />
                  <textarea
                    className="intro-body-textarea"
                    value={form.introBody}
                    onChange={(event) => setForm({ ...form, introBody: event.target.value })}
                    placeholder={"正文支持段落、空行、- 列表和 **加粗**。建议 300 到 500 字。"}
                    aria-label="介绍正文"
                  />
                  <textarea
                    className="intro-highlights-textarea"
                    value={form.introHighlights.join("\n")}
                    onChange={(event) => setForm({ ...form, introHighlights: splitIntroHighlights(event.target.value) })}
                    placeholder={"亮点列表，每行一条，例如：\n服务商：AIGoCode\n模型：claude-sonnet-4-6"}
                    aria-label="介绍亮点"
                  />
                </div>
                <div className="channel-intro-preview">
                  <div className="intro-preview-head">
                    {form.logoUrl ? <img src={form.logoUrl} alt="" /> : <span>{(form.provider || "T").slice(0, 1)}</span>}
                    <div>
                      <b>{form.introTitle || `${form.name || "通道"} 官方介绍`}</b>
                      <small>{form.introSummary || "保存后会展示在前台通道详情页和 SEO 静态 HTML 中。"}</small>
                    </div>
                  </div>
                  <p>{form.introBody ? form.introBody.split(/\n\s*\n/)[0] : "可从官网抓取草稿，也可以手动编写对人友好的结构化介绍。"}</p>
                  {form.introHighlights.length ? (
                    <ul>
                      {form.introHighlights.slice(0, 4).map((item, index) => <li key={`${item}-${index}`}>{item}</li>)}
                    </ul>
                  ) : null}
                </div>
              </div>
              <div className="hint">抓取结果只会填入当前表单，不会自动保存；正文不会作为 HTML 执行，前台只渲染安全的段落、加粗和列表。</div>
            </div>
            {!editing ? (
              <div className="row">
                <label htmlFor="pc-key">API Key</label>
                <input id="pc-key" type="password" value={form.apiKey ?? ""} onChange={(event) => setForm({ ...form, apiKey: event.target.value })} placeholder="sk-..." />
              </div>
            ) : null}
            {!editing ? (
              <div className="row col">
                <label>监控模型</label>
                <div className="monitor-pick-list">
                  {monitorModels.map((model, index) => {
                    const key = monitorModelKey(model, index);
                    const selected = selectedMonitorModelKeys.includes(key);
                    return (
                      <label className={`monitor-pick ${selected ? "selected" : ""} ${model.enabled ? "" : "disabled"}`} key={`${key}:${index}`}>
                        <input
                          type="checkbox"
                          checked={selected}
                          disabled={!model.enabled}
                          onChange={() => onToggleMonitorModel(key)}
                        />
                        <span>
                          <b>{model.label || model.model}</b>
                          <small className="mono">{model.model} · ${model.inputPerMtok}/{model.outputPerMtok}</small>
                        </span>
                      </label>
                    );
                  })}
                </div>
                <div className="hint">已勾选的模型会按顺序创建为独立平台通道，并使用上方选择的 Adapter；后续通道测试会由后台串行错开执行。</div>
              </div>
            ) : (
              <>
                <div className="row">
                  <label htmlFor="pc-model">展示模型</label>
                  <input id="pc-model" value={form.model} onChange={(event) => setForm({ ...form, model: event.target.value, upstreamModel: form.upstreamModel || event.target.value })} placeholder="gpt-4o-mini" />
                </div>
                <div className="row">
                  <label htmlFor="pc-upstream-model">上游模型</label>
                  <input id="pc-upstream-model" value={form.upstreamModel} onChange={(event) => setForm({ ...form, upstreamModel: event.target.value })} placeholder="真实传给供应商的模型名" />
                </div>
              </>
            )}
            <div className="row">
              <label htmlFor="pc-quota">每日探测上限</label>
              <input id="pc-quota" type="number" min={1} max={10000} value={form.probeDaily} onChange={(event) => setForm({ ...form, probeDaily: Number(event.target.value) || 2880 })} />
            </div>
            {editing ? (
              <>
                <div className="row">
                  <label htmlFor="pc-input-price">输入价格 / MTok</label>
                  <input id="pc-input-price" type="number" min={0} step="0.0001" value={form.inputPerMtok} onChange={(event) => setForm({ ...form, inputPerMtok: Number(event.target.value) || 0 })} />
                </div>
                <div className="row">
                  <label htmlFor="pc-output-price">输出价格 / MTok</label>
                  <input id="pc-output-price" type="number" min={0} step="0.0001" value={form.outputPerMtok} onChange={(event) => setForm({ ...form, outputPerMtok: Number(event.target.value) || 0 })} />
                </div>
              </>
            ) : null}
            <div className="row col">
              <label htmlFor="pc-provider-config">供应商高级参数 JSON</label>
              <textarea
                id="pc-provider-config"
                className="provider-config-textarea"
                value={providerConfigText}
                onChange={(event) => setProviderConfigText(event.target.value)}
                spellCheck={false}
              />
              <div className="hint">支持 temperature、topP、topK、maxTokens、timeoutMs、stop、authHeader、clientProfile、clientVersion、modelsProbeMode、l2ProbeMode、l3ProbeMode、l3ProbeMaxTokens、l3ContentPolicy、l3ExpectedContent、l3WarnMs；这些值会作为真实网关和探测 profile 默认参数。</div>
            </div>
            <div className="row col">
              <label>通道策略</label>
              <div className="switch-row">
                <label><input type="checkbox" checked={form.enabled} onChange={(event) => setForm({ ...form, enabled: event.target.checked })} /> 启用探测</label>
                <label><input type="checkbox" checked={form.publicVisible} onChange={(event) => setForm({ ...form, publicVisible: event.target.checked })} /> 前台公开</label>
                <label><input type="checkbox" checked={form.gatewayEnabled} onChange={(event) => setForm({ ...form, gatewayEnabled: event.target.checked })} /> 允许网关引用</label>
              </div>
              <div className="hint">禁用或删除后会自动从前台和网关候选中隐藏；历史探测、用量和审计保留。</div>
            </div>
          </div>
        </div>
        <div className="df">
          <span>{editing ? "如需轮换 Key，请使用列表中的 Key 操作。" : "API Key 只加密入库，不会在 API 或页面回显明文。"}</span>
          <div className="drawer-actions">
            <button type="button" className="btn btn-ghost btn-sm" onClick={onClose}>取消</button>
            <button type="submit" className="btn btn-primary btn-sm" disabled={saving}>{saving ? "保存中..." : "保存通道"}</button>
          </div>
        </div>
      </form>
    </div>
  );
}

function CredentialDrawer({
  channel,
  apiKey,
  setAPIKey,
  saving,
  onSubmit,
  onClose
}: {
  channel: AdminPlatformChannel | null;
  apiKey: string;
  setAPIKey: (value: string) => void;
  saving: boolean;
  onSubmit: (event: FormEvent) => void;
  onClose: () => void;
}) {
  return (
    <div className={`drawer-mask ${channel ? "open" : ""}`} onClick={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <form className={`drawer ${channel ? "open" : ""} private-editor credential-editor`} onSubmit={onSubmit}>
        <div className="dh">
          <div>
            <h3>轮换平台通道 Key</h3>
            <div>{channel?.name ?? "平台通道"} · 当前 {channel?.keyMask || "未配置"}</div>
          </div>
          <button type="button" className="icon-btn" onClick={onClose}>×</button>
        </div>
        <div className="db">
          <div className="tk-form">
            <div className="row">
              <label htmlFor="pc-rotate-key">新 API Key</label>
              <input id="pc-rotate-key" type="password" value={apiKey} onChange={(event) => setAPIKey(event.target.value)} placeholder="sk-..." />
            </div>
            <div className="hint">保存后只展示新的 mask 和 fingerprint。旧明文不会保留，也不会出现在审计或日志中。</div>
          </div>
        </div>
        <div className="df">
          <span>轮换完成后建议立即执行验证。</span>
          <div className="drawer-actions">
            <button type="button" className="btn btn-ghost btn-sm" onClick={onClose}>取消</button>
            <button type="submit" className="btn btn-primary btn-sm" disabled={saving}>{saving ? "保存中..." : "确认轮换"}</button>
          </div>
        </div>
      </form>
    </div>
  );
}

function ChannelExportDialog({
  open,
  password,
  setPassword,
  exporting,
  onSubmit,
  onOpenChange
}: {
  open: boolean;
  password: string;
  setPassword: (value: string) => void;
  exporting: boolean;
  onSubmit: (event: FormEvent) => void;
  onOpenChange: (open: boolean) => void;
}) {
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="导出平台通道 CSV"
      description="导出的文件包含明文 API Key。"
      footer={(
        <>
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)}>取消</Button>
          <Button variant="primary" size="sm" type="submit" form="platform-channel-export-form" disabled={exporting}>{exporting ? "导出中..." : "导出 CSV"}</Button>
        </>
      )}
    >
      <form id="platform-channel-export-form" className="tk-form channel-csv-form" onSubmit={onSubmit}>
        <div className="row">
          <label htmlFor="channel-export-password">管理员密码</label>
          <input
            id="channel-export-password"
            type="password"
            value={password}
            autoComplete="current-password"
            onChange={(event) => setPassword(event.target.value)}
            autoFocus
          />
        </div>
      </form>
    </Dialog>
  );
}

function ChannelImportDialog({
  open,
  password,
  setPassword,
  file,
  setFile,
  importing,
  results,
  onSubmit,
  onOpenChange
}: {
  open: boolean;
  password: string;
  setPassword: (value: string) => void;
  file: File | null;
  setFile: (value: File | null) => void;
  importing: boolean;
  results: AdminChannelImportResult[];
  onSubmit: (event: FormEvent) => void;
  onOpenChange: (open: boolean) => void;
}) {
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="导入平台通道 CSV"
      description="按 CSV 中的 id 更新已有平台通道；没有 id 的行会创建新通道。"
      footer={(
        <>
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)}>关闭</Button>
          <Button variant="primary" size="sm" type="submit" form="platform-channel-import-form" disabled={importing}>{importing ? "导入中..." : "导入 CSV"}</Button>
        </>
      )}
    >
      <form id="platform-channel-import-form" className="tk-form channel-csv-form" onSubmit={onSubmit}>
        <div className="row">
          <label htmlFor="channel-import-file">CSV 文件</label>
          <input key={file?.name ?? "empty-file"} id="channel-import-file" type="file" accept=".csv,text/csv" onChange={(event) => setFile(event.target.files?.[0] ?? null)} />
        </div>
        <div className="row">
          <label htmlFor="channel-import-password">管理员密码</label>
          <input
            id="channel-import-password"
            type="password"
            value={password}
            autoComplete="current-password"
            onChange={(event) => setPassword(event.target.value)}
          />
        </div>
        <div className="hint">{file ? `${file.name} · ${Math.ceil(file.size / 1024)} KB` : "请选择从平台通道导出的 CSV 文件"}</div>
      </form>
      {results.length ? (
        <div className="channel-import-results">
          {results.slice(0, 8).map((result) => (
            <div key={`${result.rowNumber}:${result.id}`} className="channel-import-result">
              <span>第 {result.rowNumber} 行</span>
              <b>{result.name}</b>
              <span>{result.action === "created" ? "已创建" : "已更新"} · {result.keyMask}</span>
            </div>
          ))}
          {results.length > 8 ? <div className="hint">还有 {results.length - 8} 行已处理</div> : null}
        </div>
      ) : null}
    </Dialog>
  );
}

function ChannelSyncDialog({
  open,
  baseUrl,
  setBaseUrl,
  siteKey,
  setSiteKey,
  password,
  setPassword,
  syncing,
  results,
  logs,
  source,
  onSubmit,
  onOpenChange
}: {
  open: boolean;
  baseUrl: string;
  setBaseUrl: (value: string) => void;
  siteKey: string;
  setSiteKey: (value: string) => void;
  password: string;
  setPassword: (value: string) => void;
  syncing: boolean;
  results: AdminChannelImportResult[];
  logs: AdminChannelSyncLog[];
  source: string;
  onSubmit: (event: FormEvent) => void;
  onOpenChange: (open: boolean) => void;
}) {
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="通过通道 API 同步"
      description="从另一个 TokHub 站点拉取平台通道、凭据和最新监控快照。"
      footer={(
        <>
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)}>关闭</Button>
          <Button variant="primary" size="sm" type="submit" form="platform-channel-sync-form" disabled={syncing}>{syncing ? "调用中..." : "开始 API 调用"}</Button>
        </>
      )}
    >
      <form id="platform-channel-sync-form" className="tk-form channel-csv-form" onSubmit={onSubmit}>
        <div className="row">
          <label htmlFor="channel-sync-base">源站 Base URL</label>
          <input
            id="channel-sync-base"
            type="url"
            value={baseUrl}
            placeholder="https://source.example.com 或 https://source.example.com/v1/status"
            onChange={(event) => setBaseUrl(event.target.value)}
            autoFocus
          />
        </div>
        <div className="row">
          <label htmlFor="channel-sync-key">通道同步 Site Key</label>
          <input
            id="channel-sync-key"
            type="password"
            value={siteKey}
            autoComplete="off"
            onChange={(event) => setSiteKey(event.target.value)}
          />
        </div>
        <div className="row">
          <label htmlFor="channel-sync-password">管理员密码</label>
          <input
            id="channel-sync-password"
            type="password"
            value={password}
            autoComplete="current-password"
            onChange={(event) => setPassword(event.target.value)}
          />
        </div>
        <div className="hint">源站 Site Key 需要在 /admin/open-api 勾选 channel_sync scope。同步会覆盖同 id 的平台通道并写入本地审计。</div>
      </form>
      {results.length ? (
        <div className="channel-import-results">
          {source ? <div className="hint">源站：{source}</div> : null}
          {results.slice(0, 8).map((result) => (
            <div key={`${result.rowNumber}:${result.id}`} className="channel-import-result">
              <span>第 {result.rowNumber} 个</span>
              <b>{result.name}</b>
              <span>{result.action === "created" ? "已创建" : "已更新"} · {result.keyMask}</span>
            </div>
          ))}
          {results.length > 8 ? <div className="hint">还有 {results.length - 8} 个通道已处理</div> : null}
        </div>
      ) : null}
      {logs.length ? (
        <div className="channel-import-results channel-sync-logs">
          <div className="hint">已拉取最近 {logs.length} 条监控日志</div>
          {logs.slice(0, 5).map((log) => (
            <div key={log.id} className="channel-import-result">
              <span>{timeLabel(log.sampledAt)}</span>
              <b>{log.channelName}</b>
              <span>{statusText(log.status)} · {log.latencyP95Ms}ms</span>
            </div>
          ))}
        </div>
      ) : null}
    </Dialog>
  );
}

function Layer({ status, latency }: { status: string; latency: number }) {
  const text = status === "auth_error" ? "auth" : status;
  return (
    <span className="probe-layer">
      <i className={`sdot ${status === "ok" ? "s-ok" : status === "down" || status === "auth_error" ? "s-down" : "s-na"}`} />
      {text}
      {latency ? <em>{latency}ms</em> : null}
    </span>
  );
}

function statusText(status: string) {
  switch (status) {
    case "healthy":
      return "健康";
    case "degraded":
      return "降级";
    case "auth_error":
      return "认证错误";
    case "connectivity_down":
      return "连接故障";
    case "functional_down":
      return "功能故障";
    case "disabled":
      return "已禁用";
    default:
      return "未知";
  }
}

function diagnosisClass(severity?: string) {
  if (severity === "error") return "is-error";
  if (severity === "warn") return "is-warn";
  if (severity === "ok") return "is-ok";
  return "is-info";
}

function timeLabel(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function quotaClass(used: number, quota: number) {
  if (!quota) return "";
  const pct = (used / quota) * 100;
  if (pct >= 90) return "full";
  if (pct >= 70) return "warn";
  return "";
}

function providerMark(provider: string) {
  const palette = ["#0ea5e9", "#f59e0b", "#22c55e", "#ec4899", "#14b8a6", "#f97316"];
  let sum = 0;
  for (const char of provider) sum += char.charCodeAt(0);
  return palette[sum % palette.length];
}

function channelListDisplayName(channel: Pick<AdminPlatformChannel, "name" | "provider">) {
  const name = channel.name.trim();
  if (!name) return channel.provider.trim() || "未命名通道";
  const separator = /\s+·\s+/.exec(name);
  if (!separator || separator.index === 0) return name;
  return name.slice(0, separator.index).trim() || name;
}

function normalizeMonitorModels(items?: MonitorModelConfig[]) {
  const source = items?.length ? items : defaultMonitorModels;
  const normalized = source
    .map((item, index) => {
      const model = (item.model || "").trim();
      const upstreamModel = (item.upstreamModel || model).trim();
      return {
        key: (item.key || model || `monitor-model-${index + 1}`).trim(),
        label: (item.label || model || `监控模型 ${index + 1}`).trim(),
        model,
        upstreamModel,
        type: (item.type || "openai-compatible").trim(),
        enabled: Boolean(item.enabled),
        defaultSelected: Boolean(item.defaultSelected),
        inputPerMtok: Number.isFinite(item.inputPerMtok) ? item.inputPerMtok : 0,
        outputPerMtok: Number.isFinite(item.outputPerMtok) ? item.outputPerMtok : 0,
        aliases: Array.isArray(item.aliases) ? item.aliases : []
      } satisfies MonitorModelConfig;
    })
    .filter((item) => item.model);
  return normalized.length ? normalized : defaultMonitorModels.map((item) => ({ ...item, aliases: [...item.aliases] }));
}

function monitorModelKey(model: MonitorModelConfig, index: number) {
  return (model.key || model.model || `monitor-model-${index + 1}`).trim();
}

function defaultSelectedMonitorModelKeys(models: MonitorModelConfig[]) {
  const enabled = models.filter((item) => item.enabled);
  const selected = enabled.filter((item) => item.defaultSelected);
  return (selected.length ? selected : enabled).map((item, index) => monitorModelKey(item, index));
}

function reconcileSelectedMonitorKeys(current: string[], models: MonitorModelConfig[]) {
  const enabledKeys = new Set(models.filter((item) => item.enabled).map((item, index) => monitorModelKey(item, index)));
  const next = current.filter((key) => enabledKeys.has(key));
  return next.length ? next : defaultSelectedMonitorModelKeys(models);
}

function selectedMonitorModels(models: MonitorModelConfig[], keys: string[]) {
  const selected = new Set(keys);
  return models
    .filter((item, index) => item.enabled && selected.has(monitorModelKey(item, index)))
    .map((item, index) => ({
      ...item,
      key: monitorModelKey(item, index),
      upstreamModel: item.upstreamModel || item.model,
      label: item.label || item.model,
      aliases: [...(item.aliases ?? [])]
    }));
}

function splitIntroHighlights(value: string) {
  return value
    .split(/\n+/)
    .map((item) => item.trim())
    .filter(Boolean)
    .slice(0, 8);
}

function prettyJSON(value: Record<string, unknown>) {
  return JSON.stringify(value, null, 2);
}

function parseProviderConfig(text: string): Record<string, unknown> {
  const trimmed = text.trim();
  if (!trimmed) return {};
  const parsed = JSON.parse(trimmed) as unknown;
  if (!parsed || Array.isArray(parsed) || typeof parsed !== "object") {
    throw new Error("供应商高级参数必须是 JSON object");
  }
  return parsed as Record<string, unknown>;
}

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
