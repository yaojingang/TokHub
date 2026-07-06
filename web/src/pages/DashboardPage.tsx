import type { CSSProperties, Dispatch, FormEvent, ReactNode, SetStateAction } from "react";
import { useEffect, useMemo, useState } from "react";
import { useLocation } from "react-router-dom";
import { ConsoleShell } from "../components/ConsoleShell";
import {
  bulkPrivateChannels,
  bulkRemoveFavorites,
  ConnectionValidationResult,
  consoleMembers,
  consoleSettings,
  consoleUsage,
  createPrivateChannel,
  currentUser,
  deletePrivateChannel,
  favoriteChannels,
  GatewayKey,
  GatewayMember,
  GatewayUsageSummary,
  GovernanceSummary,
  governanceSummary,
  PrivateChannel,
  PrivateChannelInput,
  probePrivateChannel,
  PublicChannel,
  publicChannelPath,
  removeFavorite,
  updatePrivateChannel,
  validatePrivateChannelDraft,
  validatePrivateChannel,
  workspaceCanOperate,
  User,
  privateChannels
} from "../lib/api";
import { Dialog } from "../ui";

const statusClass: Record<string, string> = {
  healthy: "b-green",
  degraded: "b-amber",
  functional_down: "b-magenta",
  connectivity_down: "b-red",
  auth_error: "b-red",
  unknown: "b-gray"
};

const emptyForm: PrivateChannelInput = {
  name: "",
  provider: "OpenAI",
  type: "openai-compatible",
  model: "gpt-4o-mini",
  endpoint: "",
  apiKey: "",
  probeDaily: 50
};

type ActionResultDialogState = {
  kind: "validate" | "probe";
  ok: boolean;
  title: string;
  message: string;
  channel?: PrivateChannel;
  result?: ConnectionValidationResult;
};

type DashboardKPI = {
  label: string;
  value: string;
  foot: string;
  color: string;
  soft: string;
  icon?: string;
};

type UsageRankRow = {
  id: string;
  name: string;
  value: string;
  meta: string;
  pct: number;
  tone: string;
};

type PrivateHealthStats = {
  avgUptime: number;
  used: number;
  quota: number;
  healthy: number;
  abnormal: number;
};

type DashboardWorkspaceData = {
  user: User | null;
  favorites: PublicChannel[];
  setFavorites: Dispatch<SetStateAction<PublicChannel[]>>;
  privateItems: PrivateChannel[];
  setPrivateItems: Dispatch<SetStateAction<PrivateChannel[]>>;
  usageToday: GatewayUsageSummary | null;
  usageMonth: GatewayUsageSummary | null;
  members: GatewayMember[];
  keys: GatewayKey[];
  governance: GovernanceSummary | null;
  workspaceRole: string;
  loading: boolean;
  error: string;
  setError: Dispatch<SetStateAction<string>>;
  reload: () => Promise<void>;
};

function useDashboardWorkspaceData(): DashboardWorkspaceData {
  const location = useLocation();
  const [user, setUser] = useState<User | null>(null);
  const [favorites, setFavorites] = useState<PublicChannel[]>([]);
  const [privateItems, setPrivateItems] = useState<PrivateChannel[]>([]);
  const [usageToday, setUsageToday] = useState<GatewayUsageSummary | null>(null);
  const [usageMonth, setUsageMonth] = useState<GatewayUsageSummary | null>(null);
  const [members, setMembers] = useState<GatewayMember[]>([]);
  const [keys, setKeys] = useState<GatewayKey[]>([]);
  const [governance, setGovernance] = useState<GovernanceSummary | null>(null);
  const [workspaceRole, setWorkspaceRole] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    void reload();
  }, []);

  async function reload() {
    setLoading(true);
    setError("");
    try {
      const me = await currentUser({ force: true });
      if (!me) {
        window.location.href = `/login?next=${encodeURIComponent(`${location.pathname}${location.search}${location.hash}`)}`;
        return;
      }
      setUser(me);
      const [fav, mine, gov, today, month, access, settings] = await Promise.all([
        favoriteChannels(),
        privateChannels(),
        governanceSummary("console"),
        consoleUsage({ days: "1", recompute: "0" }),
        consoleUsage({ days: "30" }),
        consoleMembers(),
        consoleSettings()
      ]);
      setFavorites(fav.items ?? []);
      setPrivateItems(mine.items ?? []);
      setGovernance(gov);
      setUsageToday(today);
      setUsageMonth(month);
      setMembers(access.members ?? []);
      setKeys(access.keys ?? []);
      setWorkspaceRole(settings.workspace.role);
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载用户控制台失败");
    } finally {
      setLoading(false);
    }
  }

  return {
    user,
    favorites,
    setFavorites,
    privateItems,
    setPrivateItems,
    usageToday,
    usageMonth,
    members,
    keys,
    governance,
    workspaceRole,
    loading,
    error,
    setError,
    reload
  };
}

function useDashboardMetrics({
  favorites,
  privateItems,
  usageToday,
  usageMonth,
  members,
  keys
}: {
  favorites: PublicChannel[];
  privateItems: PrivateChannel[];
  usageToday: GatewayUsageSummary | null;
  usageMonth: GatewayUsageSummary | null;
  members: GatewayMember[];
  keys: GatewayKey[];
}) {
  const privateStats = useMemo<PrivateHealthStats>(() => {
    const avgUptime = privateItems.length ? privateItems.reduce((sum, item) => sum + item.uptime24h, 0) / privateItems.length : 0;
    const used = privateItems.reduce((sum, item) => sum + item.probesUsedToday, 0);
    const quota = privateItems.reduce((sum, item) => sum + item.probeDaily, 0);
    const healthy = privateItems.filter((item) => item.status === "healthy").length;
    const abnormal = privateItems.filter((item) => item.status !== "healthy").length;
    return { avgUptime, used, quota, healthy, abnormal };
  }, [privateItems]);

  const legacyKpis = useMemo<DashboardKPI[]>(() => [
    ["关注的平台通道", String(favorites.length), "来自全部平台通道", "#f59e0b", "#fff8ec"],
    ["我的私有通道", String(privateItems.length), "自配 Key · 自定义配额", "var(--brand)", "var(--brand-soft)"],
    ["私有通道平均可用率", privateItems.length ? `${privateStats.avgUptime.toFixed(1)}%` : "—", "最近 24 小时", "var(--green)", "var(--green-soft)"],
    ["今日探测使用", `${privateStats.used} / ${privateStats.quota}`, "私有通道累计探测次数", "var(--blue)", "var(--blue-soft)"],
    ["异常 / 降级", String(privateStats.abnormal), "私有通道中需关注", "var(--magenta)", "var(--magenta-soft)"]
  ].map(([label, value, foot, color, soft]) => ({ label, value, foot, color, soft })), [favorites.length, privateItems.length, privateStats]);

  const cockpitKpis = useMemo<DashboardKPI[]>(() => {
    const activeKeys = keys.filter((item) => item.status === "active").length;
    const requestsToday = usageToday?.totals.requests ?? 0;
    const tokensToday = usageToday?.totals.tokens ?? 0;
    const costMonth = usageMonth?.totals.costUsd ?? 0;
    const errorRate = usageToday?.totals.errorRate ?? usageMonth?.totals.errorRate ?? 0;
    return [
      { label: "今日 Token", value: formatToken(tokensToday), foot: "经专属中转站调用", color: "var(--brand)", soft: "var(--brand-soft)", icon: "Σ" },
      { label: "今日请求", value: formatCompactNumber(requestsToday), foot: `${usageToday?.recent.length ?? 0} 条最近事件`, color: "var(--blue)", soft: "var(--blue-soft)", icon: "↗" },
      { label: "近 30 天成本", value: formatMoney(costMonth), foot: "按上游模型价格估算", color: "var(--amber)", soft: "var(--amber-soft)", icon: "$" },
      { label: "错误率", value: `${errorRate.toFixed(1)}%`, foot: errorRate > 0 ? "今日存在失败请求" : "今日网关请求稳定", color: errorRate > 0 ? "var(--red)" : "var(--green)", soft: errorRate > 0 ? "var(--red-soft)" : "var(--green-soft)", icon: errorRate > 0 ? "!" : "✓" },
      { label: "活跃 Key", value: String(activeKeys), foot: `${members.length || 1} 个工作区成员`, color: "var(--magenta)", soft: "var(--magenta-soft)", icon: "⚿" }
    ];
  }, [keys, members.length, usageMonth, usageToday]);

  const modelRows = useMemo(() => buildUsageRows(usageMonth?.models ?? [], usageMonth?.totals.tokens ?? 0, ["var(--brand)", "var(--green)", "var(--blue)", "var(--amber)"]), [usageMonth]);
  const gatewayRows = useMemo(() => buildUsageRows(usageMonth?.gateways ?? [], usageMonth?.totals.tokens ?? 0, ["var(--blue)", "var(--green)", "var(--amber)", "var(--magenta)"]), [usageMonth]);
  const memberRows = useMemo(() => buildMemberRows(usageMonth, members), [usageMonth, members]);

  return { privateStats, legacyKpis, cockpitKpis, modelRows, gatewayRows, memberRows };
}

export function DashboardPage({ initialView = "overview" }: { initialView?: "overview" | "channels" }) {
  const channelFocused = initialView === "channels";
  const {
    user,
    favorites,
    setFavorites,
    privateItems,
    setPrivateItems,
    usageToday,
    usageMonth,
    members,
    keys,
    governance,
    workspaceRole,
    loading,
    error,
    setError,
    reload
  } = useDashboardWorkspaceData();
  const [saving, setSaving] = useState(false);
  const [probing, setProbing] = useState("");
  const [validating, setValidating] = useState("");
  const [draftValidating, setDraftValidating] = useState(false);
  const [notice, setNotice] = useState("");
  const [actionResult, setActionResult] = useState<ActionResultDialogState | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<PrivateChannel | null>(null);
  const [form, setForm] = useState<PrivateChannelInput>(emptyForm);
  const [favoriteQuery, setFavoriteQuery] = useState("");
  const [favoriteStatus, setFavoriteStatus] = useState("all");
  const [selectedFavoriteIDs, setSelectedFavoriteIDs] = useState<string[]>([]);
  const [privateQuery, setPrivateQuery] = useState("");
  const [privateStatus, setPrivateStatus] = useState("all");
  const [selectedPrivateIDs, setSelectedPrivateIDs] = useState<string[]>([]);
  const canOperateWorkspace = workspaceCanOperate(workspaceRole);

  const {
    privateStats,
    legacyKpis,
    cockpitKpis,
    modelRows,
    gatewayRows,
    memberRows
  } = useDashboardMetrics({ favorites, privateItems, usageToday, usageMonth, members, keys });

  function openCreate() {
    if (!canOperateWorkspace) {
      setError("当前工作区角色不能添加私有通道。");
      return;
    }
    setEditing(null);
    setForm(emptyForm);
    setEditorOpen(true);
    setError("");
    setNotice("");
    setActionResult(null);
  }

  function openEdit(item: PrivateChannel) {
    if (!canOperateWorkspace) {
      setError("当前工作区角色不能编辑私有通道。");
      return;
    }
    setEditing(item);
    setForm({
      name: item.name,
      provider: item.provider,
      type: item.type,
      model: item.model,
      endpoint: item.endpoint,
      apiKey: "",
      probeDaily: item.probeDaily
    });
    setEditorOpen(true);
    setError("");
    setNotice("");
    setActionResult(null);
  }

  function closeEditor() {
    if (saving) return;
    setEditorOpen(false);
  }

  async function savePrivateChannel(event: FormEvent) {
    event.preventDefault();
    if (!canOperateWorkspace) {
      setError("当前工作区角色不能保存私有通道。");
      return;
    }
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const payload = editing ? await updatePrivateChannel(editing.id, form) : await createPrivateChannel(form);
      setPrivateItems((items) => {
        if (!editing) return [payload.channel, ...items];
        return items.map((item) => (item.id === payload.channel.id ? payload.channel : item));
      });
      setNotice(editing ? "私有通道已更新，API Key 未回显。" : "私有通道已创建，API Key 已加密保存。");
      setEditorOpen(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存私有通道失败");
    } finally {
      setSaving(false);
    }
  }

  async function removeFavoriteChannel(channelID: string) {
    setError("");
    try {
      await removeFavorite(channelID);
      setFavorites((items) => items.filter((item) => item.id !== channelID));
      setSelectedFavoriteIDs((ids) => ids.filter((id) => id !== channelID));
    } catch (err) {
      setError(err instanceof Error ? err.message : "取消关注失败");
    }
  }

  async function runBulkRemoveFavorites() {
    const ids = selectedFavoriteIDs.filter((id) => favorites.some((item) => item.id === id));
    if (!ids.length) {
      setError("请先选择至少一个关注通道。");
      return;
    }
    if (!window.confirm(`确认批量取消关注 ${ids.length} 个通道？`)) return;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const payload = await bulkRemoveFavorites(ids);
      setFavorites(payload.items ?? []);
      setSelectedFavoriteIDs([]);
      setNotice("已批量取消关注。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "批量取消关注失败");
    } finally {
      setSaving(false);
    }
  }

  async function removePrivateChannel(channelID: string) {
    if (!canOperateWorkspace) {
      setError("当前工作区角色不能删除私有通道。");
      return;
    }
    if (!window.confirm("确认移除这个私有通道？历史探测记录会保留在审计与探测表中。")) return;
    setError("");
    try {
      await deletePrivateChannel(channelID);
      setPrivateItems((items) => items.filter((item) => item.id !== channelID));
      setSelectedPrivateIDs((ids) => ids.filter((id) => id !== channelID));
      setNotice("私有通道已移除。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "删除私有通道失败");
    }
  }

  async function runBulkPrivate(action: "disable" | "enable" | "delete") {
    if (!canOperateWorkspace) {
      setError("当前工作区角色不能批量操作私有通道。");
      return;
    }
    const ids = selectedPrivateIDs.filter((id) => privateItems.some((item) => item.id === id));
    if (!ids.length) {
      setError("请先选择至少一个私有通道。");
      return;
    }
    const verb = action === "delete" ? "删除" : action === "disable" ? "禁用" : "启用";
    if (!window.confirm(`确认批量${verb} ${ids.length} 个私有通道？该操作会写入审计。`)) return;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const payload = await bulkPrivateChannels({ action, ids });
      setPrivateItems(payload.items ?? []);
      setSelectedPrivateIDs([]);
      setNotice(`私有通道已批量${verb}。`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "私有通道批量操作失败");
    } finally {
      setSaving(false);
    }
  }

  async function probeNow(channelID: string) {
    if (!canOperateWorkspace) {
      setError("当前工作区角色不能发起私有通道探测。");
      return;
    }
    setProbing(channelID);
    setError("");
    setNotice("");
    setActionResult(null);
    try {
      const payload = await probePrivateChannel(channelID);
      setPrivateItems((items) => items.map((item) => (item.id === channelID ? payload.channel : item)));
      setActionResult({
        kind: "probe",
        ok: payload.channel.status === "healthy" || payload.channel.l3Status === "ok",
        title: "L3 真实探测完成",
        message: payload.channel.status === "healthy"
          ? "真实生成探测通过，当前通道可继续纳入专属中转站。"
          : `探测已完成，当前状态为 ${payload.channel.statusLabel}，建议检查上游地址、模型 ID 或 API Key。`,
        channel: payload.channel
      });
    } catch (err) {
      setActionResult({
        kind: "probe",
        ok: false,
        title: "L3 真实探测失败",
        message: err instanceof Error ? err.message : "探测失败"
      });
      await reload();
    } finally {
      setProbing("");
    }
  }

  async function validateNow(channelID: string) {
    if (!canOperateWorkspace) {
      setError("当前工作区角色不能测试私有通道连接。");
      return;
    }
    setValidating(channelID);
    setError("");
    setNotice("");
    setActionResult(null);
    try {
      const payload = await validatePrivateChannel(channelID);
      const channel = privateItems.find((item) => item.id === channelID);
      setActionResult({
        kind: "validate",
        ok: payload.result.ok,
        title: payload.result.ok ? "连接测试通过" : "连接测试失败",
        message: payload.result.ok
          ? `已连通上游，发现 ${payload.result.modelCount} 个模型，耗时 ${payload.result.latencyMs}ms。`
          : payload.result.message || `失败阶段：${payload.result.stage}`,
        channel,
        result: payload.result
      });
    } catch (err) {
      setActionResult({
        kind: "validate",
        ok: false,
        title: "连接测试失败",
        message: err instanceof Error ? err.message : "连接测试失败"
      });
    } finally {
      setValidating("");
    }
  }

  async function validateDraft() {
    if (!canOperateWorkspace) {
      setError("当前工作区角色不能测试私有通道连接。");
      return;
    }
    setDraftValidating(true);
    setError("");
    setNotice("");
    setActionResult(null);
    try {
      const payload = await validatePrivateChannelDraft(form);
      setActionResult({
        kind: "validate",
        ok: payload.result.ok,
        title: payload.result.ok ? "草稿连接测试通过" : "草稿连接测试失败",
        message: payload.result.ok
          ? `已连通上游，发现 ${payload.result.modelCount} 个模型，耗时 ${payload.result.latencyMs}ms。`
          : payload.result.message || `失败阶段：${payload.result.stage}`,
        result: payload.result
      });
    } catch (err) {
      setActionResult({
        kind: "validate",
        ok: false,
        title: "草稿连接测试失败",
        message: err instanceof Error ? err.message : "连接测试失败"
      });
    } finally {
      setDraftValidating(false);
    }
  }

  const filteredFavorites = useMemo(() => {
    const query = favoriteQuery.trim().toLowerCase();
    return favorites.filter((item) => {
      if (favoriteStatus !== "all") {
        if (favoriteStatus === "down") {
          if (!["connectivity_down", "functional_down", "auth_error"].includes(item.status)) return false;
        } else if (item.status !== favoriteStatus) {
          return false;
        }
      }
      if (!query) return true;
      return [item.name, item.provider, item.model, item.upstreamModel, item.endpoint, item.statusLabel]
        .some((value) => String(value ?? "").toLowerCase().includes(query));
    });
  }, [favorites, favoriteQuery, favoriteStatus]);

  const selectedFavoriteSet = useMemo(() => new Set(selectedFavoriteIDs), [selectedFavoriteIDs]);
  const filteredFavoriteIDs = useMemo(() => filteredFavorites.map((item) => item.id), [filteredFavorites]);
  const allFilteredFavoritesSelected = filteredFavoriteIDs.length > 0 && filteredFavoriteIDs.every((id) => selectedFavoriteSet.has(id));

  function toggleFavoriteSelection(id: string, checked: boolean) {
    setSelectedFavoriteIDs((ids) => {
      const next = new Set(ids);
      if (checked) next.add(id);
      else next.delete(id);
      return Array.from(next);
    });
  }

  function toggleAllFilteredFavorites(checked: boolean) {
    setSelectedFavoriteIDs((ids) => {
      const next = new Set(ids);
      for (const id of filteredFavoriteIDs) {
        if (checked) next.add(id);
        else next.delete(id);
      }
      return Array.from(next);
    });
  }

  const filteredPrivateItems = useMemo(() => {
    const query = privateQuery.trim().toLowerCase();
    return privateItems.filter((item) => {
      if (privateStatus !== "all") {
        if (privateStatus === "down") {
          if (!["connectivity_down", "functional_down", "auth_error"].includes(item.status)) return false;
        } else if (item.status !== privateStatus) {
          return false;
        }
      }
      if (!query) return true;
      return [item.name, item.provider, item.model, item.upstreamModel, item.endpoint, item.statusLabel]
        .some((value) => String(value ?? "").toLowerCase().includes(query));
    });
  }, [privateItems, privateQuery, privateStatus]);

  const selectedPrivateSet = useMemo(() => new Set(selectedPrivateIDs), [selectedPrivateIDs]);
  const filteredPrivateIDs = useMemo(() => filteredPrivateItems.map((item) => item.id), [filteredPrivateItems]);
  const allFilteredPrivateSelected = filteredPrivateIDs.length > 0 && filteredPrivateIDs.every((id) => selectedPrivateSet.has(id));

  function togglePrivateSelection(id: string, checked: boolean) {
    setSelectedPrivateIDs((ids) => {
      const next = new Set(ids);
      if (checked) next.add(id);
      else next.delete(id);
      return Array.from(next);
    });
  }

  function toggleAllFilteredPrivate(checked: boolean) {
    setSelectedPrivateIDs((ids) => {
      const next = new Set(ids);
      for (const id of filteredPrivateIDs) {
        if (checked) next.add(id);
        else next.delete(id);
      }
      return Array.from(next);
    });
  }

  return (
    <ConsoleShell title={channelFocused ? "我的通道" : "控制台首页"} crumb={channelFocused ? "/ 工作区 / 通道" : "/ 工作区"}>
      <main className="console-page">
        <div className="dash-hero">
          <div className="dash-user">
            <span className="me-av">{user?.avatar || "U"}</span>
            <div>
              <h1>{channelFocused ? "我的通道" : `${user?.name || "用户"} 的用户控制台`}</h1>
              <p>{channelFocused ? "管理你自己添加的 API 通道，测试通过后可纳入专属中转站" : "管理你的关注通道、私有通道和专属中转站"}</p>
            </div>
          </div>
          <div className="dash-actions">
            {channelFocused ? (
              <a className="btn btn-ghost btn-sm" href="/console">
                回控制台首页
              </a>
            ) : (
              <a className="btn btn-ghost btn-sm" href="/dashboard">
                查看全部通道
              </a>
            )}
            <a className="btn btn-ghost btn-sm" href="/console/gateways">
              专属中转站
            </a>
            <button className="btn btn-primary btn-sm" onClick={openCreate} disabled={!canOperateWorkspace}>
              ＋ 添加私有通道
            </button>
          </div>
        </div>

        {error ? <div className="form-error">{error}</div> : null}
        {notice ? <div className="form-notice">{notice}</div> : null}

        {channelFocused ? (
          <section className="kpis" id="dashKpis">
            {legacyKpis.map((item) => (
              <KPI key={item.label} {...item} />
            ))}
          </section>
        ) : (
          <ConsoleCockpit
            loading={loading}
            kpis={cockpitKpis}
            usageToday={usageToday}
            usageMonth={usageMonth}
            privateStats={privateStats}
            privateItems={privateItems}
            favorites={favorites}
            governance={governance}
            modelRows={modelRows}
            gatewayRows={gatewayRows}
            memberRows={memberRows}
            fullscreenHref="/console/fullscreen"
          />
        )}

        {!channelFocused ? (
          <>
            <div className="section-head" id="fav">
              <h2>
                我的关注 <span className="tag">{favorites.length} 个</span>
              </h2>
              <span className="sub">从监控总览加入关注的平台通道，状态变化会优先在这里提醒</span>
            </div>
            <div className="card board">
              <div className="toolbar dashboard-filter-toolbar">
                <input className="input" style={{ flex: "1 1 320px" }} value={favoriteQuery} onChange={(event) => setFavoriteQuery(event.target.value)} placeholder="搜索关注通道 / 服务商 / 模型..." />
                <select className="input" style={{ width: 150 }} value={favoriteStatus} onChange={(event) => setFavoriteStatus(event.target.value)}>
                  <option value="all">全部状态</option>
                  <option value="healthy">Healthy</option>
                  <option value="degraded">Degraded</option>
                  <option value="down">异常</option>
                  <option value="unknown">Unknown</option>
                </select>
                <button className="btn btn-ghost btn-sm" onClick={() => { setFavoriteQuery(""); setFavoriteStatus("all"); }}>清空筛选</button>
              </div>
              <div className="toolbar bulk-toolbar">
                <label className="bulk-select">
                  <input type="checkbox" aria-label="选择当前筛选的关注通道" checked={allFilteredFavoritesSelected} disabled={!filteredFavoriteIDs.length} onChange={(event) => toggleAllFilteredFavorites(event.target.checked)} />
                  已选 {selectedFavoriteIDs.length} 个
                </label>
                <button className="btn btn-ghost btn-sm danger-lite" disabled={!selectedFavoriteIDs.length || saving} onClick={() => void runBulkRemoveFavorites()}>批量取消关注</button>
              </div>
              <div className="scroll">
                <table className="tk">
                  <thead>
                    <tr>
                      <th style={{ width: 44 }}></th>
                      <th>服务商 / 通道</th>
                      <th>综合状态</th>
                      <th>基础监控 · L1/L2</th>
                      <th>真实监控 · L3</th>
                      <th>真实延迟</th>
                      <th>24h 可用率</th>
                      <th>质量评分</th>
                      <th>取消关注</th>
                    </tr>
                  </thead>
                  <tbody>
                    {loading ? (
                      <tr><td colSpan={9}>正在加载关注通道…</td></tr>
                    ) : filteredFavorites.length ? (
                      filteredFavorites.map((item) => (
                        <FavoriteRow
                          key={item.id}
                          item={item}
                          selected={selectedFavoriteSet.has(item.id)}
                          onSelect={(checked) => toggleFavoriteSelection(item.id, checked)}
                          onRemove={() => void removeFavoriteChannel(item.id)}
                        />
                      ))
                    ) : favorites.length ? (
                      <tr>
                        <td colSpan={9}>
                          <div className="empty-state">
                            <div className="ico">⌕</div>
                            <h4>没有匹配的关注通道</h4>
                            <p>调整关键词或状态筛选后再试。</p>
                            <button className="btn btn-ghost btn-sm" onClick={() => { setFavoriteQuery(""); setFavoriteStatus("all"); }}>清空筛选</button>
                          </div>
                        </td>
                      </tr>
                    ) : (
                      <tr>
                        <td colSpan={9}>
                          <div className="empty-state">
                            <div className="ico">★</div>
                            <h4>还没有关注的平台通道</h4>
                            <p>去监控总览中点击行首的星标加入关注，它们就会出现在这里。</p>
                            <a className="btn btn-primary btn-sm" href="/dashboard#board">去监控总览</a>
                          </div>
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          </>
        ) : null}

        <div className="section-head" id="mine">
          <h2>
            我的私有通道 <span className="tag">{privateItems.length} 个</span>
          </h2>
          <span className="sub">由你自己提供 API Key，TokHub 按你设定的额度做 L3 真实探测</span>
        </div>
        <div className="card board">
          <div className="toolbar dashboard-filter-toolbar">
            <input className="input" style={{ flex: "1 1 320px" }} value={privateQuery} onChange={(event) => setPrivateQuery(event.target.value)} placeholder="搜索私有通道 / 端点 / 模型..." />
            <select className="input" style={{ width: 150 }} value={privateStatus} onChange={(event) => setPrivateStatus(event.target.value)}>
              <option value="all">全部状态</option>
              <option value="healthy">Healthy</option>
              <option value="degraded">Degraded</option>
              <option value="down">异常</option>
              <option value="disabled">Disabled</option>
              <option value="unknown">Unknown</option>
            </select>
            <button className="btn btn-ghost btn-sm" onClick={() => { setPrivateQuery(""); setPrivateStatus("all"); }}>清空筛选</button>
          </div>
          <div className="toolbar bulk-toolbar">
            <label className="bulk-select">
              <input type="checkbox" aria-label="选择当前筛选的私有通道" checked={allFilteredPrivateSelected} disabled={!filteredPrivateIDs.length || !canOperateWorkspace} onChange={(event) => toggleAllFilteredPrivate(event.target.checked)} />
              已选 {selectedPrivateIDs.length} 个
            </label>
            <button className="btn btn-ghost btn-sm" disabled={!selectedPrivateIDs.length || saving || !canOperateWorkspace} onClick={() => void runBulkPrivate("disable")}>批量禁用</button>
            <button className="btn btn-ghost btn-sm" disabled={!selectedPrivateIDs.length || saving || !canOperateWorkspace} onClick={() => void runBulkPrivate("enable")}>批量启用</button>
            <button className="btn btn-ghost btn-sm danger-lite" disabled={!selectedPrivateIDs.length || saving || !canOperateWorkspace} onClick={() => void runBulkPrivate("delete")}>批量删除</button>
          </div>
          <div className="scroll">
            <table className="tk dashboard-private-table">
              <thead>
                <tr>
                  <th style={{ width: 42 }}></th>
                  <th>通道信息</th>
                  <th>状态 / 质量</th>
                  <th>探测与配额</th>
                  <th>API Key</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {loading ? (
                  <tr><td colSpan={6}>正在加载私有通道…</td></tr>
                ) : filteredPrivateItems.length ? (
                  filteredPrivateItems.map((item) => (
                    <PrivateRow
                      key={item.id}
                      item={item}
                      selected={selectedPrivateSet.has(item.id)}
                      probing={probing === item.id}
                      validating={validating === item.id}
                      canOperate={canOperateWorkspace}
                      onSelect={(checked) => togglePrivateSelection(item.id, checked)}
                      onProbe={() => void probeNow(item.id)}
                      onValidate={() => void validateNow(item.id)}
                      onEdit={() => openEdit(item)}
                      onDelete={() => void removePrivateChannel(item.id)}
                    />
                  ))
                ) : privateItems.length ? (
                  <tr>
                    <td colSpan={6}>
                      <div className="empty-state">
                        <div className="ico">⌕</div>
                        <h4>没有匹配的私有通道</h4>
                        <p>调整关键词或状态筛选后再试。</p>
                        <button className="btn btn-ghost btn-sm" onClick={() => { setPrivateQuery(""); setPrivateStatus("all"); }}>清空筛选</button>
                      </div>
                    </td>
                  </tr>
                ) : (
                  <tr>
                    <td colSpan={6}>
                      <div className="empty-state">
                        <div className="ico">＋</div>
                        <h4>还没有添加私有通道</h4>
                        <p>把你公司或个人在用的中转站填进来，TokHub 会按你设定的频率帮你跑探测，结果只有你能看见。</p>
                        <button className="btn btn-primary btn-sm" onClick={openCreate} disabled={!canOperateWorkspace}>＋ 立即添加</button>
                      </div>
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </div>
      </main>

      <PrivateChannelEditor
        open={editorOpen}
        editing={editing}
        form={form}
        setForm={setForm}
        saving={saving}
        validating={draftValidating}
        canOperate={canOperateWorkspace}
        onSubmit={savePrivateChannel}
        onValidate={() => void validateDraft()}
        onClose={closeEditor}
        />
      <ActionResultDialog result={actionResult} onClose={() => setActionResult(null)} />
    </ConsoleShell>
  );
}

export function DashboardFullscreenPage() {
  const {
    favorites,
    privateItems,
    usageToday,
    usageMonth,
    members,
    keys,
    governance,
    loading,
    error,
    reload
  } = useDashboardWorkspaceData();
  const {
    privateStats,
    cockpitKpis,
    modelRows,
    gatewayRows,
    memberRows
  } = useDashboardMetrics({ favorites, privateItems, usageToday, usageMonth, members, keys });

  return (
    <main className="console-fullscreen-page">
      {error ? <div className="form-error fullscreen-error">{error}</div> : null}

      <ConsoleCockpit
        loading={loading}
        kpis={cockpitKpis}
        usageToday={usageToday}
        usageMonth={usageMonth}
        privateStats={privateStats}
        privateItems={privateItems}
        favorites={favorites}
        governance={governance}
        modelRows={modelRows}
        gatewayRows={gatewayRows}
        memberRows={memberRows}
        showActions={false}
        variant="fullscreen"
        headerActions={
          <>
            <button className="btn btn-ghost btn-sm" disabled={loading} onClick={() => void reload()}>
              {loading ? "刷新中" : "刷新数据"}
            </button>
            <a className="btn btn-ghost btn-sm" href="/console">
              返回控制台
            </a>
          </>
        }
      />

      <FullscreenDetailPanels
        loading={loading}
        usageToday={usageToday}
        usageMonth={usageMonth}
        privateStats={privateStats}
        privateItems={privateItems}
        keys={keys}
        members={members}
        governance={governance}
      />
    </main>
  );
}

function ConsoleCockpit({
  loading,
  kpis,
  usageToday,
  usageMonth,
  privateStats,
  privateItems,
  favorites,
  governance,
  modelRows,
  gatewayRows,
  memberRows,
  fullscreenHref,
  showActions = true,
  variant = "default",
  headerActions
}: {
  loading: boolean;
  kpis: DashboardKPI[];
  usageToday: GatewayUsageSummary | null;
  usageMonth: GatewayUsageSummary | null;
  privateStats: PrivateHealthStats;
  privateItems: PrivateChannel[];
  favorites: PublicChannel[];
  governance: GovernanceSummary | null;
  modelRows: UsageRankRow[];
  gatewayRows: UsageRankRow[];
  memberRows: UsageRankRow[];
  fullscreenHref?: string;
  showActions?: boolean;
  variant?: "default" | "fullscreen";
  headerActions?: ReactNode;
}) {
  const topPrivateItems = [...privateItems]
    .sort((a, b) => statusPriority(a.status) - statusPriority(b.status) || b.score - a.score)
    .slice(0, 4);
  return (
    <section className={`console-cockpit ${variant === "fullscreen" ? "fullscreen-cockpit" : ""}`} aria-label="工作区 AI 用量驾驶舱">
      <div className="cockpit-head">
        <div>
          <span className="cockpit-eyebrow">工作区 · AI 算力实况</span>
          <h2>今日用量、模型分布和私有监控</h2>
        </div>
        {showActions || fullscreenHref || headerActions ? (
          <div className="cockpit-head-actions">
            {showActions ? <a className="btn btn-ghost btn-sm" href="/console/usage">查看用量</a> : null}
            {showActions ? <a className="btn btn-ghost btn-sm" href="/console/alerts">告警规则</a> : null}
            {fullscreenHref ? (
              <a className="btn btn-primary btn-sm cockpit-fullscreen-action" href={fullscreenHref} target="_blank" rel="noreferrer">
                全屏监控
              </a>
            ) : null}
            {headerActions}
          </div>
        ) : null}
      </div>

      <section className="kpis cockpit-kpis" id="dashKpis">
        {kpis.map((item) => (
          <KPI key={item.label} {...item} />
        ))}
      </section>

      <div className="cockpit-grid">
        <div className="cockpit-panel cockpit-usage-panel">
          <div className="cockpit-panel-head">
            <div>
              <span>工作区用量</span>
              <h3>专属中转站消耗</h3>
            </div>
            <span className="cockpit-period">今日</span>
          </div>
          <div className="cockpit-primary-number">
            <b>{formatToken(usageToday?.totals.tokens ?? 0)}</b>
            <span>token</span>
          </div>
          <div className="cockpit-usage-meta">
            <span><b>{formatCompactNumber(usageToday?.totals.requests ?? 0)}</b> 请求</span>
            <span><b>{formatMoney(usageToday?.totals.costUsd ?? 0)}</b> 成本</span>
            <span><b>{(usageToday?.totals.errorRate ?? 0).toFixed(1)}%</b> 错误率</span>
          </div>
          <UsageSparkline points={usageMonth?.trend ?? []} loading={loading} />
        </div>

        <RankPanel
          title="模型消耗分布"
          subtitle="近 30 天 · 按 Token"
          emptyTitle="暂无模型用量"
          rows={modelRows}
          actionHref="/console/usage"
          actionLabel="筛选模型"
        />
      </div>

      <div className="cockpit-secondary-grid">
        <RankPanel
          title="专属中转站"
          subtitle="近 30 天 · 按网关"
          emptyTitle="暂无网关调用"
          rows={gatewayRows}
          actionHref="/console/gateways"
          actionLabel="管理网关"
        />
        <RankPanel
          title="成员用量"
          subtitle="近 30 天 · 按 Key 创建人"
          emptyTitle="暂无成员用量"
          rows={memberRows}
          actionHref="/console/members"
          actionLabel="成员与密钥"
        />
        <div className="cockpit-panel cockpit-monitor-panel">
          <div className="cockpit-panel-head">
            <div>
              <span>私有监控</span>
              <h3>通道健康与配额</h3>
            </div>
            <a className="cockpit-link" href="/console/channels">我的通道</a>
          </div>
          <div className="monitor-metrics">
            <MiniMetric label="健康通道" value={`${privateStats.healthy}/${privateItems.length}`} />
            <MiniMetric label="今日探测" value={`${privateStats.used}/${privateStats.quota}`} />
            <MiniMetric label="24h 可用率" value={privateItems.length ? `${privateStats.avgUptime.toFixed(1)}%` : "—"} />
            <MiniMetric label="关注通道" value={String(favorites.length)} />
          </div>
          <div className="monitor-strip">
            <span>打开事件 <b>{governance?.openIncidents ?? 0}</b></span>
            <span>今日告警 <b>{governance?.alertsToday ?? 0}</b></span>
            <span>人工审计 <b>{governance?.auditToday ?? 0}</b></span>
          </div>
          <div className="monitor-list">
            {topPrivateItems.length ? topPrivateItems.map((item) => (
              <div className="monitor-row" key={item.id}>
                <span className={`sdot ${dotClass(item.status === "healthy" ? "ok" : item.status)}`} />
                <b title={item.name}>{item.name}</b>
                <em>{item.uptime24h.toFixed(1)}%</em>
              </div>
            )) : (
              <div className="cockpit-empty compact">还没有私有通道</div>
            )}
          </div>
        </div>
      </div>
    </section>
  );
}

function FullscreenDetailPanels({
  loading,
  usageToday,
  usageMonth,
  privateStats,
  privateItems,
  keys,
  members,
  governance
}: {
  loading: boolean;
  usageToday: GatewayUsageSummary | null;
  usageMonth: GatewayUsageSummary | null;
  privateStats: PrivateHealthStats;
  privateItems: PrivateChannel[];
  keys: GatewayKey[];
  members: GatewayMember[];
  governance: GovernanceSummary | null;
}) {
  const activeKeys = keys.filter((item) => item.status === "active").length;
  const quotaPct = privateStats.quota ? Math.round((privateStats.used / privateStats.quota) * 100) : 0;
  const recent = usageToday?.recent ?? [];
  const abnormalPrivate = privateItems.filter((item) => item.status !== "healthy");

  return (
    <>
      <section className="fullscreen-section">
        <div className="fullscreen-section-head">
          <div>
            <span className="cockpit-eyebrow">明细</span>
            <h2>今日工作区明细</h2>
          </div>
          <span>{loading ? "同步中" : `${recent.length} 条最近请求`}</span>
        </div>
        <div className="fullscreen-detail-grid">
          <FullscreenDetailStat label="请求量" value={formatCompactNumber(usageToday?.totals.requests ?? 0)} helper="今日专属中转站请求" />
          <FullscreenDetailStat label="Token" value={formatToken(usageToday?.totals.tokens ?? 0)} helper="今日网关 Token 消耗" />
          <FullscreenDetailStat label="成本" value={formatMoney(usageToday?.totals.costUsd ?? 0)} helper="今日按模型价格估算" />
          <FullscreenDetailStat label="错误率" value={`${(usageToday?.totals.errorRate ?? 0).toFixed(1)}%`} helper="今日网关失败占比" />
          <FullscreenDetailStat label="探测额度" value={`${quotaPct}%`} helper={`${privateStats.used}/${privateStats.quota} 次已用`} />
          <FullscreenDetailStat label="活跃 Key" value={String(activeKeys)} helper={`${members.length || 1} 个工作区成员`} />
          <FullscreenDetailStat label="打开事件" value={String(governance?.openIncidents ?? 0)} helper={`今日告警 ${governance?.alertsToday ?? 0}`} />
          <FullscreenDetailStat label="异常通道" value={String(abnormalPrivate.length)} helper={`${privateItems.length} 个私有通道`} />
        </div>
      </section>

      <section className="fullscreen-lists-grid" aria-label="监控列表">
        <FullscreenUsageMonitorList
          title="网关用量监测"
          subtitle="近 30 天 · 按 Token 排序"
          items={usageMonth?.gateways ?? []}
          emptyTitle="暂无网关调用"
        />
        <FullscreenUsageMonitorList
          title="模型消耗监测"
          subtitle="近 30 天 · 模型分布"
          items={usageMonth?.models ?? []}
          emptyTitle="暂无模型用量"
        />
        <FullscreenPrivateMonitorList items={privateItems} loading={loading} />
      </section>

      <section className="fullscreen-section">
        <div className="fullscreen-section-head">
          <div>
            <span className="cockpit-eyebrow">最近请求</span>
            <h2>专属中转站事件流</h2>
          </div>
          <span>今日</span>
        </div>
        <FullscreenRecentList items={recent} loading={loading} />
      </section>
    </>
  );
}

function FullscreenDetailStat({ label, value, helper }: { label: string; value: string; helper: string }) {
  return (
    <div className="fullscreen-detail-stat">
      <span>{label}</span>
      <b>{value}</b>
      <em>{helper}</em>
    </div>
  );
}

function FullscreenUsageMonitorList({
  title,
  subtitle,
  items,
  emptyTitle
}: {
  title: string;
  subtitle: string;
  items: Array<{ id: string; name: string; requests: number; tokens: number; costUsd: number; errorRate: number }>;
  emptyTitle: string;
}) {
  const rows = items.slice(0, 8);
  const maxTokens = Math.max(1, ...rows.map((item) => item.tokens));
  return (
    <div className="fullscreen-panel">
      <div className="fullscreen-panel-head">
        <div>
          <span>{subtitle}</span>
          <h3>{title}</h3>
        </div>
        <b>{rows.length}</b>
      </div>
      {rows.length ? (
        <div className="fullscreen-monitor-table">
          {rows.map((item) => (
            <div className="fullscreen-monitor-row" key={item.id || item.name}>
              <div className="fullscreen-row-copy">
                <b title={item.name}>{item.name || "未标记"}</b>
                <span>{formatCompactNumber(item.requests)} 请求 · {formatMoney(item.costUsd)}</span>
              </div>
              <div className="fullscreen-row-meter" aria-label={`${item.name} Token 监测`}>
                <i style={{ width: `${Math.max(4, Math.round((item.tokens / maxTokens) * 100))}%` }} />
              </div>
              <strong>{formatToken(item.tokens)}</strong>
              <em>{item.errorRate.toFixed(1)}%</em>
            </div>
          ))}
        </div>
      ) : (
        <div className="cockpit-empty compact">{emptyTitle}</div>
      )}
    </div>
  );
}

function FullscreenPrivateMonitorList({ items, loading }: { items: PrivateChannel[]; loading: boolean }) {
  const rows = [...items]
    .sort((a, b) => statusPriority(a.status) - statusPriority(b.status) || a.uptime24h - b.uptime24h)
    .slice(0, 8);
  return (
    <div className="fullscreen-panel">
      <div className="fullscreen-panel-head">
        <div>
          <span>私有通道 · L3 探测</span>
          <h3>通道健康监测</h3>
        </div>
        <b>{loading ? "..." : rows.length}</b>
      </div>
      {rows.length ? (
        <div className="fullscreen-channel-list">
          {rows.map((item) => (
            <div className="fullscreen-channel-row" key={item.id}>
              <span className={`sdot ${dotClass(item.status === "healthy" ? "ok" : item.status)}`} />
              <div>
                <b title={item.name}>{item.name}</b>
                <span>{item.provider} · {item.model}</span>
              </div>
              <strong style={{ color: uptimeColor(item.uptime24h) }}>{item.uptime24h.toFixed(1)}%</strong>
              <em>{item.l3LatencyMs ? fmtMs(item.l3LatencyMs) : "—"}</em>
              <i>{item.probesUsedToday}/{item.probeDaily}</i>
            </div>
          ))}
        </div>
      ) : (
        <div className="cockpit-empty compact">{loading ? "正在加载私有通道" : "还没有私有通道"}</div>
      )}
    </div>
  );
}

function FullscreenRecentList({ items, loading }: { items: GatewayUsageSummary["recent"]; loading: boolean }) {
  const rows = items.slice(0, 10);
  return (
    <div className="fullscreen-recent-table">
      <div className="fullscreen-recent-head">
        <span>时间</span>
        <span>网关 / Key</span>
        <span>模型 / 通道</span>
        <span>状态</span>
        <span>Token</span>
        <span>延迟</span>
      </div>
      {rows.length ? rows.map((item) => (
        <div className="fullscreen-recent-row" key={item.id}>
          <span>{timeLabel(item.createdAt)}</span>
          <span><b>{item.gateway}</b><em>{item.keyName}</em></span>
          <span><b>{item.model}</b><em>{item.channel}</em></span>
          <span className={item.statusCode >= 400 ? "recent-status bad" : "recent-status ok"}>{item.statusCode}</span>
          <span>{formatToken(item.tokens)}</span>
          <span>{fmtMs(item.latencyMs)}</span>
        </div>
      )) : (
        <div className="cockpit-empty">{loading ? "正在加载最近请求" : "今日暂无专属中转站请求"}</div>
      )}
    </div>
  );
}

function KPI({ label, value, foot, color, soft, icon = "✓" }: DashboardKPI) {
  return (
    <div className="kpi" style={{ "--c": color, "--cs": soft } as CSSProperties}>
      <div className="k-top">
        <span className="k-label">{label}</span>
        <span className="k-ico">{icon}</span>
      </div>
      <div className="k-value">{value}</div>
      <div className="k-foot">{foot}</div>
    </div>
  );
}

function RankPanel({ title, subtitle, rows, emptyTitle, actionHref, actionLabel }: { title: string; subtitle: string; rows: UsageRankRow[]; emptyTitle: string; actionHref: string; actionLabel: string }) {
  return (
    <div className="cockpit-panel">
      <div className="cockpit-panel-head">
        <div>
          <span>{subtitle}</span>
          <h3>{title}</h3>
        </div>
        <a className="cockpit-link" href={actionHref}>{actionLabel}</a>
      </div>
      {rows.length ? (
        <div className="usage-rank-list">
          {rows.map((row, index) => (
            <div className="usage-rank-row" key={row.id || `${row.name}-${index}`}>
              <div className="rank-copy">
                <span>{String(index + 1).padStart(2, "0")}</span>
                <b title={row.name}>{row.name}</b>
                <em>{row.meta}</em>
              </div>
              <div className="rank-meter" aria-label={`${row.name} 占比 ${row.pct.toFixed(1)}%`}>
                <i style={{ width: `${Math.max(4, Math.min(100, row.pct))}%`, background: row.tone }} />
              </div>
              <strong>{row.value}</strong>
            </div>
          ))}
        </div>
      ) : (
        <div className="cockpit-empty">{emptyTitle}</div>
      )}
    </div>
  );
}

function UsageSparkline({ points, loading }: { points: GatewayUsageSummary["trend"]; loading: boolean }) {
  const values = points.slice(-14);
  const max = Math.max(1, ...values.map((item) => item.tokens));
  return (
    <div className="usage-sparkline" aria-label="近 14 天 Token 趋势">
      <div className="sparkline-head">
        <span>近 14 天 Token 趋势</span>
        <b>{values.length ? formatToken(values.reduce((sum, item) => sum + item.tokens, 0)) : loading ? "加载中" : "暂无数据"}</b>
      </div>
      <div className="sparkline-bars">
        {values.length ? values.map((item) => (
          <span key={item.date} title={`${item.date} · ${formatInt(item.tokens)} token`}>
            <i style={{ height: `${Math.max(8, Math.round((item.tokens / max) * 100))}%` }} />
          </span>
        )) : Array.from({ length: 14 }, (_, index) => <span className="empty" key={index}><i /></span>)}
      </div>
    </div>
  );
}

function MiniMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="mini-metric">
      <span>{label}</span>
      <b>{value}</b>
    </div>
  );
}

function FavoriteRow({ item, selected, onSelect, onRemove }: { item: PublicChannel; selected: boolean; onSelect: (checked: boolean) => void; onRemove: () => void }) {
  const score = scoreColor(item.score);
  return (
    <tr onClick={() => (window.location.href = publicChannelPath(item))}>
      <td>
        <input
          type="checkbox"
          aria-label={`选择关注 ${item.name}`}
          checked={selected}
          onClick={(event) => event.stopPropagation()}
          onChange={(event) => onSelect(event.target.checked)}
        />
      </td>
      <td>
        <div className="prov">
          <div className="mark" style={{ background: item.mark }}>{item.provider[0]}</div>
          <div className="meta">
            <div className="name">
              {item.name}
              <span className="chan b-blue">{item.type.toUpperCase()}</span>
              <span className="fav-mark" title="已关注">★</span>
            </div>
            <div className="sub">{item.model} · {item.upstreamModel}</div>
          </div>
        </div>
      </td>
      <td>
        <span className={`badge ${statusClass[item.status] ?? "b-gray"} dot`}>{item.statusLabel}</span>
        <div className="st-sub"><span className="st-time">{timeLabel(item.lastProbeAt)}</span>{item.errorType ? <><span className="st-sep">·</span><span className="st-err">{item.errorType}</span></> : null}</div>
      </td>
      <td><LayerPair a={item.l1Status} b={item.l2Status} aMs={item.l1LatencyMs} bMs={item.l2LatencyMs} /></td>
      <td><LayerPair a={item.l3Status} b={`${item.successRate.toFixed(1)}%`} aMs={item.l3LatencyMs} /></td>
      <td><span className="lat">{item.l3LatencyMs ? fmtMs(item.l3LatencyMs) : "—"}</span></td>
      <td><span className="uptime" style={{ color: uptimeColor(item.uptime24h) }}>{item.uptime24h.toFixed(1)}%</span></td>
      <td><Score value={item.score} color={score} /></td>
      <td>
        <button
          className="icon-btn"
          title="取消关注"
          onClick={(event) => {
            event.stopPropagation();
            onRemove();
          }}
        >
          ★
        </button>
      </td>
    </tr>
  );
}

function PrivateRow({
  item,
  selected,
  probing,
  validating,
  canOperate,
  onSelect,
  onProbe,
  onValidate,
  onEdit,
  onDelete
}: {
  item: PrivateChannel;
  selected: boolean;
  probing: boolean;
  validating: boolean;
  canOperate: boolean;
  onSelect: (checked: boolean) => void;
  onProbe: () => void;
  onValidate: () => void;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const score = scoreColor(item.score);
  const quotaPct = item.probeDaily ? Math.min(100, Math.round((item.probesUsedToday / item.probeDaily) * 100)) : 0;
  const quotaClass = quotaPct >= 90 ? "full" : quotaPct >= 70 ? "warn" : "";
  return (
    <tr>
      <td><input type="checkbox" aria-label={`选择 ${item.name}`} checked={selected} disabled={!canOperate} onChange={(event) => onSelect(event.target.checked)} /></td>
      <td className="private-channel-summary-cell">
        <div className="prov private-channel-summary">
          <div className="mark" style={{ background: item.mark }}>{item.provider[0]}</div>
          <div className="meta">
            <div className="name" title={item.name}>
              <span>{item.name}</span>
              <span className="badge b-blue dot private-badge">私有</span>
            </div>
            <div className="private-channel-meta">
              <span>{item.provider}</span>
              <span>{item.type}</span>
              <span>{item.model}</span>
            </div>
            <div className="sub mono private-endpoint" title={item.endpoint}>{item.endpoint}</div>
          </div>
        </div>
      </td>
      <td className="private-health-cell">
        <span className={`badge ${statusClass[item.status] ?? "b-gray"} dot`}>{item.statusLabel}</span>
        <div className="private-health-metrics">
          <span className="uptime" style={{ color: uptimeColor(item.uptime24h) }}>{item.uptime24h.toFixed(1)}%</span>
          <Score value={item.score} color={score} />
        </div>
      </td>
      <td className="private-probe-cell">
        <div><span className="lat">{item.l3LatencyMs ? fmtMs(item.l3LatencyMs) : "—"}</span><span className="muted-time"> 延迟</span></div>
        <div><span className={`quota-tag ${quotaClass}`}>{item.probesUsedToday} / {item.probeDaily}</span></div>
        <div className="muted-time">最后 {timeLabel(item.lastProbeAt)}</div>
      </td>
      <td className="private-key-cell">
        <span className="keyval compact-key" title={`${item.keyMask} · ${shortFingerprint(item.keyFingerprint)}`}>
          {compactKeyMask(item.keyMask)}
          <span className="rev">{shortFingerprint(item.keyFingerprint)}</span>
        </span>
      </td>
      <td className="row-actions private-row-actions">
        <button className="btn btn-ghost btn-sm" disabled={validating || !canOperate} onClick={onValidate}>
          {validating ? "测试中" : "测试连接"}
        </button>
        <button className="btn btn-ghost btn-sm" disabled={probing || item.quotaExhausted || !canOperate} onClick={onProbe}>
          {probing ? "探测中" : item.quotaExhausted ? "配额用尽" : "探测"}
        </button>
        <button className="btn btn-ghost btn-sm" disabled={!canOperate} onClick={onEdit}>编辑</button>
        <button className="icon-btn" title="移除" disabled={!canOperate} onClick={onDelete}>×</button>
      </td>
    </tr>
  );
}

function ActionResultDialog({ result, onClose }: { result: ActionResultDialogState | null; onClose: () => void }) {
  if (!result) return null;
  const validation = result.result;
  const channel = result.channel;
  const statusText = result.ok ? "通过" : "失败";
  return (
    <Dialog
      open={Boolean(result)}
      onOpenChange={(open) => { if (!open) onClose(); }}
      title={result.title}
      description={result.message}
      footer={<button className="btn btn-primary btn-sm" onClick={onClose}>知道了</button>}
    >
      <div className={`probe-result-panel ${result.ok ? "ok" : "fail"}`}>
        <div className="probe-result-status">
          <span className="probe-result-icon">{result.ok ? "✓" : "!"}</span>
          <div>
            <b>{statusText}</b>
            <span>{result.kind === "probe" ? "真实 L3 探测结果" : "上游连接测试结果"}</span>
          </div>
        </div>
        <div className="probe-result-grid">
          {channel ? <ResultMetric label="通道" value={channel.name} /> : null}
          {channel ? <ResultMetric label="状态" value={channel.statusLabel} /> : null}
          {validation ? <ResultMetric label="阶段" value={validation.stage || "—"} /> : null}
          {validation ? <ResultMetric label="状态码" value={validation.statusCode ? String(validation.statusCode) : "—"} /> : null}
          <ResultMetric label="延迟" value={validation ? fmtMs(validation.latencyMs) : channel?.l3LatencyMs ? fmtMs(channel.l3LatencyMs) : "—"} />
          {validation ? <ResultMetric label="模型数" value={String(validation.modelCount)} /> : null}
          {validation ? <ResultMetric label="Tokens" value={String(validation.tokens)} /> : null}
          {channel ? <ResultMetric label="今日配额" value={`${channel.probesUsedToday} / ${channel.probeDaily}`} /> : null}
          {channel ? <ResultMetric label="24h 可用率" value={`${channel.uptime24h.toFixed(1)}%`} /> : null}
          {channel ? <ResultMetric label="质量评分" value={channel.score.toFixed(0)} /> : null}
        </div>
        <div className="probe-result-detail">
          <div>
            <span>模型</span>
            <b>{validation?.model || channel?.model || "—"}</b>
          </div>
          <div>
            <span>Endpoint</span>
            <b className="mono">{validation?.endpoint || channel?.endpoint || "—"}</b>
          </div>
          {validation?.errorType ? (
            <div>
              <span>错误类型</span>
              <b>{validation.errorType}</b>
            </div>
          ) : null}
        </div>
      </div>
    </Dialog>
  );
}

function ResultMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="probe-result-metric">
      <span>{label}</span>
      <b>{value}</b>
    </div>
  );
}

function PrivateChannelEditor({
  open,
  editing,
  form,
  setForm,
  saving,
  validating,
  canOperate,
  onSubmit,
  onValidate,
  onClose
}: {
  open: boolean;
  editing: PrivateChannel | null;
  form: PrivateChannelInput;
  setForm: (value: PrivateChannelInput) => void;
  saving: boolean;
  validating: boolean;
  canOperate: boolean;
  onSubmit: (event: FormEvent) => void;
  onValidate: () => void;
  onClose: () => void;
}) {
  return (
    <div className={`drawer-mask ${open ? "open" : ""}`} onClick={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <form className={`drawer ${open ? "open" : ""} private-editor`} onSubmit={onSubmit}>
        <div className="dh">
          <div>
            <h3>{editing ? "编辑我的私有通道" : "添加我的私有通道"}</h3>
            <div>填写中转站信息和 API Key，TokHub 会按你设定的额度发起探测</div>
          </div>
          <button type="button" className="icon-btn" onClick={onClose}>×</button>
        </div>
        <div className="db">
          {!editing ? (
            <div className="private-recommend-guide">
              <div className="private-recommend-guide-icon">★</div>
              <div className="private-recommend-guide-copy">
                <b>还没选定哪家中转站？</b>
                <span>先看精选推荐里的编辑首推、场景榜单和新人福利，再回来填写 Endpoint 和 Key。</span>
              </div>
              <a className="btn btn-ghost btn-sm" href="/recommend" target="_blank" rel="noreferrer">
                打开精选推荐
              </a>
            </div>
          ) : null}
          <div className="tk-form">
            <div className="row">
              <label htmlFor="f-name">通道名称</label>
              <input id="f-name" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} placeholder="例如：MyCorp-OpenAI" />
            </div>
            <div className="row">
              <label htmlFor="f-provider">服务商</label>
              <select id="f-provider" value={form.provider} onChange={(event) => setForm({ ...form, provider: event.target.value })}>
                <option value="OpenAI">OpenAI</option>
                <option value="Anthropic">Anthropic</option>
                <option value="Gemini">Gemini</option>
                <option value="Mixed">混合通道</option>
              </select>
            </div>
            <div className="row">
              <label htmlFor="f-type">分类</label>
              <select id="f-type" value={form.type} onChange={(event) => setForm({ ...form, type: event.target.value })}>
                <option value="openai-compatible">OpenAI 兼容</option>
                <option value="anthropic">Claude 通道</option>
                <option value="gemini">Gemini 通道</option>
                <option value="mixed">混合通道</option>
              </select>
            </div>
            <div className="row">
              <label htmlFor="f-endpoint">Endpoint URL</label>
              <input id="f-endpoint" value={form.endpoint} onChange={(event) => setForm({ ...form, endpoint: event.target.value })} placeholder="https://api.example.com/v1" />
            </div>
            <div className="row">
              <label htmlFor="f-key">API Key</label>
              <input id="f-key" type="password" value={form.apiKey ?? ""} onChange={(event) => setForm({ ...form, apiKey: event.target.value })} placeholder={editing ? "留空则不轮换 Key" : "sk-..."} />
            </div>
            <div className="row">
              <label htmlFor="f-model">探测模型</label>
              <input id="f-model" value={form.model} onChange={(event) => setForm({ ...form, model: event.target.value })} placeholder="gpt-4o-mini / claude-haiku-4.5" />
            </div>
            <div className="row col">
              <label htmlFor="f-quota">每日探测额度 <span>Token 消耗算你的</span></label>
              <div className="row-inline" style={{ marginTop: 6 }}>
                <select id="f-quota" value={quotaPreset(form.probeDaily)} onChange={(event) => setForm({ ...form, probeDaily: Number(event.target.value === "custom" ? 50 : event.target.value) })}>
                  <option value="10">10 次 / 天（轻量）</option>
                  <option value="50">50 次 / 天（推荐）</option>
                  <option value="100">100 次 / 天</option>
                  <option value="500">500 次 / 天</option>
                  <option value="custom">自定义</option>
                </select>
                <input type="number" min={1} max={500} value={form.probeDaily} onChange={(event) => setForm({ ...form, probeDaily: Number(event.target.value) || 50 })} />
              </div>
              <div className="hint">L1/L2 链路探测不消耗 Token，L3 真实生成探测会按你的 Key 计费。</div>
            </div>
          </div>
        </div>
        <div className="df">
          <span>Key 只加密入库，后台和 API 都不会回显明文。</span>
          <div className="drawer-actions">
            <button type="button" className="btn btn-ghost btn-sm" onClick={onClose}>取消</button>
            <button type="button" className="btn btn-ghost btn-sm" disabled={saving || validating || !canOperate || !form.endpoint || !form.apiKey} onClick={onValidate}>{validating ? "测试中..." : "测试连接"}</button>
            <button type="submit" className="btn btn-primary btn-sm" disabled={saving || !canOperate}>{saving ? "保存中..." : "保存并开始探测"}</button>
          </div>
        </div>
      </form>
    </div>
  );
}

function LayerPair({ a, b, aMs, bMs }: { a: string; b: string; aMs?: number; bMs?: number }) {
  return (
    <div className="duo">
      <div className="line"><span className="lab">L1</span><span className={`sdot ${dotClass(a)}`} />{a}{aMs ? <span className="lat">{aMs}ms</span> : null}</div>
      <div className="line"><span className="lab">L2</span><span className={`sdot ${dotClass(String(b))}`} />{b}{bMs ? <span className="lat">{bMs}ms</span> : null}</div>
    </div>
  );
}

function Score({ value, color }: { value: number; color: string }) {
  return (
    <div className="score">
      <div className="ring" style={{ background: `conic-gradient(${color} ${Math.max(value, 0) * 3.6}deg,#eef0f4 0)` }}>
        <span style={{ color }}>{value.toFixed(0)}</span>
      </div>
    </div>
  );
}

function dotClass(value: string) {
  if (value === "ok" || value.includes("%")) return "s-ok";
  if (value === "warn" || value === "rate_limited" || value === "slow_response") return "s-warn";
  if (value === "down" || value === "auth_error") return "s-down";
  return "s-na";
}

function scoreColor(score: number) {
  if (score >= 90) return "var(--green)";
  if (score >= 75) return "var(--blue)";
  if (score >= 60) return "var(--amber)";
  return "var(--red)";
}

function uptimeColor(value: number) {
  if (value >= 99) return "var(--green)";
  if (value >= 95) return "var(--amber)";
  return "var(--red)";
}

function fmtMs(value: number) {
  return value >= 1000 ? `${(value / 1000).toFixed(2)}s` : `${value}ms`;
}

function timeLabel(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function shortFingerprint(value: string) {
  return value ? value.slice(0, 10) : "未记录";
}

function compactKeyMask(value: string) {
  if (!value) return "未配置";
  if (value.length <= 15) return value;
  return `${value.slice(0, 6)}••••${value.slice(-4)}`;
}

function buildUsageRows(items: GatewayUsageSummary["models"], totalTokens: number, tones: string[]): UsageRankRow[] {
  return items.slice(0, 4).map((item, index) => ({
    id: item.id || item.name || `usage-${index}`,
    name: item.name || "未标记",
    value: formatToken(item.tokens),
    meta: `${formatCompactNumber(item.requests)} 请求 · ${formatMoney(item.costUsd)}`,
    pct: totalTokens > 0 ? (item.tokens / totalTokens) * 100 : 0,
    tone: tones[index % tones.length]
  }));
}

function buildMemberRows(usage: GatewayUsageSummary | null, members: GatewayMember[]): UsageRankRow[] {
  const memberMap = new Map(members.map((item) => [item.userId, item]));
  const grouped = new Map<string, { requests: number; tokens: number; costUsd: number }>();
  for (const item of usage?.rollups ?? []) {
    if (item.source !== "gateway") continue;
    const memberID = item.memberUserId || "unassigned";
    const current = grouped.get(memberID) ?? { requests: 0, tokens: 0, costUsd: 0 };
    current.requests += item.requests;
    current.tokens += item.tokens;
    current.costUsd += item.costUsd;
    grouped.set(memberID, current);
  }
  const totalTokens = Array.from(grouped.values()).reduce((sum, item) => sum + item.tokens, 0);
  return Array.from(grouped.entries())
    .sort((a, b) => b[1].tokens - a[1].tokens)
    .slice(0, 4)
    .map(([memberID, item], index) => {
      const member = memberMap.get(memberID);
      return {
        id: memberID,
        name: memberID === "unassigned" ? "未归属 Key" : member?.name || member?.email || `成员 ${memberID.slice(-6)}`,
        value: formatToken(item.tokens),
        meta: `${formatCompactNumber(item.requests)} 请求 · ${formatMoney(item.costUsd)}`,
        pct: totalTokens > 0 ? (item.tokens / totalTokens) * 100 : 0,
        tone: ["var(--green)", "var(--brand)", "var(--blue)", "var(--amber)"][index % 4]
      };
    });
}

function statusPriority(status: string) {
  if (status === "healthy") return 3;
  if (status === "unknown" || status === "disabled") return 2;
  if (status === "degraded") return 1;
  return 0;
}

function formatInt(value: number) {
  return new Intl.NumberFormat("zh-CN").format(Math.round(value || 0));
}

function formatCompactNumber(value: number) {
  const safe = Math.max(0, value || 0);
  if (safe >= 100000000) return `${trimNumber(safe / 100000000, safe >= 1000000000 ? 1 : 2)}亿`;
  if (safe >= 10000) return `${trimNumber(safe / 10000, safe >= 100000 ? 1 : 2)}万`;
  return formatInt(safe);
}

function formatToken(value: number) {
  return formatCompactNumber(value || 0);
}

function formatMoney(value: number) {
  const safe = value || 0;
  if (safe >= 1000) return `$${formatInt(safe)}`;
  if (safe >= 1) return `$${trimNumber(safe, 2)}`;
  return `$${safe.toFixed(3)}`;
}

function trimNumber(value: number, digits: number) {
  return value.toFixed(digits).replace(/\.0+$/, "").replace(/(\.\d*?)0+$/, "$1");
}

function quotaPreset(value: number) {
  return [10, 50, 100, 500].includes(value) ? String(value) : "custom";
}
