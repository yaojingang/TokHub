import type { CSSProperties, FormEvent } from "react";
import { Fragment, useEffect, useMemo, useState } from "react";
import { Footer } from "../components/Footer";
import { PublicNav } from "../components/PublicNav";
import { Dialog, FilterBar, SelectField, TrendBars } from "../ui";
import {
  addFavorite,
  createPrivateChannel,
  currentUser,
  deletePrivateChannel,
  errorsSummary,
  ErrorBucket,
  favoriteChannels,
  login,
  MonitorModelConfig,
  officialExperienceHref,
  providerRank,
  ProviderRank,
  PrivateChannel,
  PrivateChannelInput,
  privateChannels,
  probePrivateChannel,
  publicChannelPath,
  publicChannels,
  publicOverview,
  PublicChannel,
  PublicOverview,
  register,
  removeFavorite,
  siteConfig,
  TrendBucket,
  User,
  verifyEmail
} from "../lib/api";

type Dimension = "brand" | "model";
type BoardTab = "all" | "favorites" | "private";
type BoardFilter = {
  category: string;
  provider: string;
  channelID: string;
  status: string;
  query: string;
};
type PendingPublicAction =
  | { kind: "tab"; tab: Exclude<BoardTab, "all"> }
  | { kind: "favorite"; channelID: string }
  | { kind: "newPrivate" };

const emptyPrivateForm: PrivateChannelInput = {
  name: "",
  provider: "OpenAI",
  type: "openai-compatible",
  model: "gpt-4o-mini",
  endpoint: "",
  apiKey: "",
  probeDaily: 50
};

const statusOptions = [
  ["all", "所有状态"],
  ["healthy", "健康"],
  ["degraded", "降级"],
  ["functional_down", "功能性故障"],
  ["connectivity_down", "连通异常"],
  ["auth_error", "认证异常"],
  ["unknown", "未知"]
];

function rangeOptionLabel(range: string) {
  if (range === "24") return "近24h";
  if (range === "7") return "近7天";
  if (range === "30") return "近30天";
  return "全量";
}

function trendHeaderLabel(range: string) {
  return `${rangeOptionLabel(range)}趋势`;
}

function uptimeHeaderLabel(range: string) {
  return `${rangeOptionLabel(range)}可用率`;
}

const statusClass: Record<string, string> = {
  healthy: "b-green",
  degraded: "b-amber",
  functional_down: "b-magenta",
  connectivity_down: "b-red",
  auth_error: "b-red",
  unknown: "b-gray"
};

type CoreMonitorModel = {
  key: string;
  label: string;
  type: string;
  aliases: string[];
};

const DEFAULT_CORE_MONITOR_MODELS: CoreMonitorModel[] = [
  {
    key: "claude-sonnet-4-6",
    label: "Claude Sonnet 4.6",
    type: "anthropic",
    aliases: ["claude-sonnet-4-6", "claude-sonnet-4.6", "claude-4-6-sonnet"]
  },
  {
    key: "gpt-5.5",
    label: "GPT-5.5",
    type: "openai-compatible",
    aliases: ["gpt-5.5", "gpt-5-5", "gpt55"]
  }
];

export function PublicHome() {
  const [overview, setOverview] = useState<PublicOverview | null>(null);
  const [channels, setChannels] = useState<PublicChannel[]>([]);
  const [total, setTotal] = useState(0);
  const [rank, setRank] = useState<ProviderRank[]>([]);
  const [errors, setErrors] = useState<ErrorBucket[]>([]);
  const [dimension, setDimension] = useState<Dimension>("brand");
  const [activeBoardTab, setActiveBoardTab] = useState<BoardTab>("all");
  const [category, setCategory] = useState("all");
  const [provider, setProvider] = useState("all");
  const [channelFilter, setChannelFilter] = useState("all");
  const [status, setStatus] = useState("all");
  const [query, setQuery] = useState("");
  const [range, setRange] = useState("24");
  const [loading, setLoading] = useState(true);
  const [publicDataRange, setPublicDataRange] = useState("");
  const [personalLoading, setPersonalLoading] = useState(false);
  const [user, setUser] = useState<User | null>(null);
  const [registrationOpen, setRegistrationOpen] = useState(true);
  const [showRegisterCta, setShowRegisterCta] = useState(true);
  const [emailVerificationRequired, setEmailVerificationRequired] = useState(false);
  const [favoriteIDs, setFavoriteIDs] = useState<Set<string>>(new Set());
  const [favoriteItems, setFavoriteItems] = useState<PublicChannel[]>([]);
  const [favoriteCount, setFavoriteCount] = useState(0);
  const [privateItems, setPrivateItems] = useState<PrivateChannel[]>([]);
  const [privateCount, setPrivateCount] = useState(0);
  const [coreMonitorModels, setCoreMonitorModels] = useState<CoreMonitorModel[]>(DEFAULT_CORE_MONITOR_MODELS);
  const [selectedChannel, setSelectedChannel] = useState<PublicChannel | null>(null);
  const [authOpen, setAuthOpen] = useState(false);
  const [pendingAction, setPendingAction] = useState<PendingPublicAction | null>(null);
  const [privateFormOpen, setPrivateFormOpen] = useState(false);
  const [privateForm, setPrivateForm] = useState<PrivateChannelInput>(emptyPrivateForm);
  const [privateSaving, setPrivateSaving] = useState(false);
  const [probingPrivateID, setProbingPrivateID] = useState("");
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    let active = true;
    setError("");
    publicOverview()
      .then((overviewValue) => {
        if (!active) return;
        setOverview(overviewValue);
      })
      .catch((err: Error) => {
        if (active) setError(err.message);
      });
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    let active = true;
    setLoading(true);
    setError("");
    Promise.all([
      publicChannels({ page: 1, pageSize: 100, range }),
      providerRank(range),
      errorsSummary(range)
    ])
      .then(([channelList, rankList, errorList]) => {
        if (!active) return;
        setChannels(channelList.items);
        setTotal(channelList.total);
        setRank(rankList.items);
        setErrors(errorList.items);
        setPublicDataRange(range);
      })
      .catch((err: Error) => {
        if (active) setError(err.message);
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [range]);

  useEffect(() => {
    let active = true;
    Promise.all([currentUser({ force: true }), siteConfig()])
      .then(([me, cfg]) => {
        if (!active) return;
        setUser(me);
        setRegistrationOpen(cfg.registrationOpen);
        setShowRegisterCta(cfg.showRegisterCta);
        setEmailVerificationRequired(Boolean(cfg.emailVerificationRequired));
        setCoreMonitorModels(publicMonitorModelsFromConfig(cfg.monitorModels));
        if (!me) return;
        return Promise.all([favoriteChannels(), privateChannels()]).then(([fav, privateList]) => {
          if (!active) return;
          const ids = fav.ids ?? [];
          setFavoriteIDs(new Set(ids));
          setFavoriteItems(fav.items ?? []);
          setFavoriteCount(ids.length);
          setPrivateItems(privateList.items ?? []);
          setPrivateCount((privateList.items ?? []).length);
        });
      })
      .catch(() => {
        if (!active) return;
        setUser(null);
      });
    return () => {
      active = false;
    };
  }, []);

  async function refreshPersonalData() {
    setPersonalLoading(true);
    try {
      const [fav, privateList] = await Promise.all([favoriteChannels(), privateChannels()]);
      const ids = fav.ids ?? [];
      setFavoriteIDs(new Set(ids));
      setFavoriteItems(fav.items ?? []);
      setFavoriteCount(ids.length);
      setPrivateItems(privateList.items ?? []);
      setPrivateCount((privateList.items ?? []).length);
    } finally {
      setPersonalLoading(false);
    }
  }

  function requireAuth(action: PendingPublicAction) {
    setPendingAction(action);
    setAuthOpen(true);
    setError("");
    setNotice("");
  }

  async function handleAuthenticated(nextUser: User) {
    setUser(nextUser);
    setAuthOpen(false);
    const action = pendingAction;
    setPendingAction(null);
    try {
      if (action?.kind === "favorite") {
        await addFavorite(action.channelID);
        setNotice("已加入我的关注。");
      }
      await refreshPersonalData();
      if (action?.kind === "tab") {
        setActiveBoardTab(action.tab);
      }
      if (action?.kind === "newPrivate") {
        setActiveBoardTab("private");
        resetBoardFilters();
        openPrivateCreator();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "登录成功，但继续操作失败");
    }
  }

  function switchBoardTab(tab: BoardTab) {
    setError("");
    setNotice("");
    resetBoardFilters();
    if (tab === "all") {
      setActiveBoardTab("all");
      return;
    }
    if (!user) {
      requireAuth({ kind: "tab", tab });
      return;
    }
    setActiveBoardTab(tab);
    void refreshPersonalData();
  }

  function resetBoardFilters() {
    setCategory("all");
    setProvider("all");
    setChannelFilter("all");
    setStatus("all");
    setQuery("");
  }

  async function toggleFavorite(channelID: string) {
    if (!user) {
      requireAuth({ kind: "favorite", channelID });
      return;
    }
    setError("");
    setNotice("");
    const next = new Set(favoriteIDs);
    try {
      if (next.has(channelID)) {
        await removeFavorite(channelID);
        next.delete(channelID);
        setFavoriteItems((items) => items.filter((item) => item.id !== channelID));
      } else {
        await addFavorite(channelID);
        next.add(channelID);
        const channel = channels.find((item) => item.id === channelID);
        if (channel) setFavoriteItems((items) => [channel, ...items.filter((item) => item.id !== channelID)]);
      }
      setFavoriteIDs(next);
      setFavoriteCount(next.size);
    } catch (err) {
      setError(err instanceof Error ? err.message : "收藏失败");
    }
  }

  function openPrivateCreator() {
    setPrivateForm(emptyPrivateForm);
    setPrivateFormOpen(true);
  }

  function requestPrivateCreator() {
    if (!user) {
      requireAuth({ kind: "newPrivate" });
      return;
    }
    setActiveBoardTab("private");
    openPrivateCreator();
  }

  async function savePrivateChannel(event: FormEvent) {
    event.preventDefault();
    setPrivateSaving(true);
    setError("");
    setNotice("");
    try {
      await createPrivateChannel(privateForm);
      setPrivateFormOpen(false);
      setNotice("私有通道已添加，正在更新监控结果。");
      await refreshPersonalData();
    } catch (err) {
      setError(err instanceof Error ? err.message : "添加私有通道失败");
    } finally {
      setPrivateSaving(false);
    }
  }

  async function removePrivateChannel(channelID: string) {
    if (!window.confirm("确认删除这个私有通道？删除后不会回显 API Key。")) return;
    setError("");
    setNotice("");
    try {
      await deletePrivateChannel(channelID);
      setPrivateItems((items) => items.filter((item) => item.id !== channelID));
      setPrivateCount((count) => Math.max(0, count - 1));
      setNotice("私有通道已删除。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "删除私有通道失败");
    }
  }

  async function probePrivateNow(channelID: string) {
    setProbingPrivateID(channelID);
    setError("");
    setNotice("");
    try {
      const payload = await probePrivateChannel(channelID);
      setPrivateItems((items) => items.map((item) => (item.id === channelID ? payload.channel : item)));
      setNotice("已触发一次真实探测。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "触发探测失败");
    } finally {
      setProbingPrivateID("");
    }
  }

  const rangedFavoriteItems = useMemo(() => {
    const byID = new Map(channels.map((item) => [item.id, item]));
    return favoriteItems.map((item) => byID.get(item.id) ?? item);
  }, [channels, favoriteItems]);
  const boardSource = useMemo(() => {
    if (activeBoardTab === "favorites") return rangedFavoriteItems;
    if (activeBoardTab === "private") return privateItems;
    return channels;
  }, [activeBoardTab, channels, rangedFavoriteItems, privateItems]);
  const providers = useMemo(() => Array.from(new Set(boardSource.map((ch) => ch.provider).filter(Boolean))).sort(), [boardSource]);
  const categoryOptions = useMemo(() => buildCategoryOptions(boardSource), [boardSource]);
  const channelOptions = useMemo(() => buildChannelOptions(boardSource), [boardSource]);
  const boardFilter = useMemo(
    () => ({ category, provider, channelID: channelFilter, status, query }),
    [category, provider, channelFilter, status, query]
  );
  const filteredChannels = useMemo(() => filterChannels(channels, boardFilter), [channels, boardFilter]);
  const brandChannels = useMemo(() => buildBrandRows(filteredChannels, coreMonitorModels), [filteredChannels, coreMonitorModels]);
  const modelRows = useMemo(() => buildModelRows(filteredChannels, coreMonitorModels), [filteredChannels, coreMonitorModels]);
  const filteredFavorites = useMemo(() => filterChannels(rangedFavoriteItems, boardFilter), [rangedFavoriteItems, boardFilter]);
  const brandFavorites = useMemo(() => buildBrandRows(filteredFavorites, coreMonitorModels), [filteredFavorites, coreMonitorModels]);
  const filteredPrivate = useMemo(() => filterChannels(privateItems, boardFilter), [privateItems, boardFilter]);
  const visiblePublicRange = publicDataRange || range;
  const initialPublicLoading = loading && !publicDataRange;
  const refreshingPublic = loading && Boolean(publicDataRange);
  const exportURL = useMemo(() => {
    const params = new URLSearchParams();
    if (provider !== "all") params.set("provider", provider);
    if (status !== "all") params.set("status", status);
    if (query.trim()) params.set("query", query.trim());
    const suffix = params.toString();
    return `/api/public/channels/export${suffix ? `?${suffix}` : ""}`;
  }, [provider, status, query]);
  const boardCount = activeBoardTab === "all" ? (dimension === "brand" ? brandChannels.length : modelRows.length) : activeBoardTab === "favorites" ? brandFavorites.length : filteredPrivate.length;
  const boardCountUnit = activeBoardTab === "all" && dimension === "model" ? "个模型" : activeBoardTab === "private" ? "个通道" : "个中转站";

  return (
    <>
      <PublicNav
        onAuthClick={() => {
          setPendingAction(null);
          setNotice("");
          setError("");
          setAuthOpen(true);
        }}
      />
      <main className="page public-dashboard-page">
        <div className="page-head">
          <div>
            <h1>监控总览</h1>
            <p>
              实时追踪 <b>{overview?.total ?? 0}</b> 个中转站通道 · 基础链路探测 + 真实生成探测双线监控 · 最近更新{" "}
              {overview ? timeLabel(overview.updatedAt) : "—"}
            </p>
          </div>
          <div className="page-actions public-page-actions">
            <a className="btn btn-ghost btn-sm" href={exportURL}>导出报表</a>
            <button type="button" className="btn btn-primary btn-sm" onClick={requestPrivateCreator}>
              ＋ 新增监控通道
            </button>
          </div>
        </div>

        {error ? <div className="form-error">{error}</div> : null}
        {notice ? <div className="form-notice">{notice}</div> : null}
        <section className="kpis">
          <KPI label="健康通道" value={`${overview?.healthy ?? 0}`} unit={`/ ${overview?.total ?? 0}`} foot={`${overview?.healthyRate ?? 0}% 链路与生成均正常`} color="var(--green)" soft="var(--green-soft)" />
          <KPI label="功能性故障" value={`${overview?.functionalDown ?? 0}`} foot="入口正常但模型不可用" color="var(--magenta)" soft="var(--magenta-soft)" />
          <KPI label="连通异常" value={`${overview?.connectivityDown ?? 0}`} foot={`${overview?.degraded ?? 0} 个降级通道`} color="var(--red)" soft="var(--red-soft)" />
          <KPI label="真实调用 P95 延迟" value={(overview?.p95LatencySeconds ?? 0).toFixed(2)} unit="s" foot={`平均 ${overview?.averageLatencyMs ?? 0}ms · 慢响应率 ${overview?.slowRate ?? 0}%`} color="var(--blue)" soft="var(--blue-soft)" />
          <KPI label="今日探测 Token 成本" value={formatSmallUSD(overview?.probeCostToday ?? 0)} foot={`${compact(overview?.probeTokensToday ?? 0)} tokens · ${overview?.probeRunsToday ?? 0} 次探测`} color="var(--amber)" soft="var(--amber-soft)" />
        </section>

        <div className="section-head" id="board">
          <h2>
            通道明细看板 <span className="tag">{boardCount} {boardCountUnit}</span>
          </h2>
          <span className="sub">{activeBoardTab === "all" ? "点击任意行查看链路瀑布与探测日志" : "轻量查看和单项操作，批量治理请进入用户控制台"}</span>
        </div>

        <div className="board-topline">
          <div className="board-view-controls">
            <div className="ch-tabs board-scope-tabs">
              <button
                type="button"
                className={`ch-tab ${activeBoardTab === "all" ? "active" : ""}`}
                onClick={() => switchBoardTab("all")}
              >
                全部通道 <span className="cnt">{overview?.total ?? total}</span>
              </button>
            </div>
            {activeBoardTab === "all" ? (
              <div className="ch-tabs board-dim-tabs" aria-label="看板维度">
                <span className="board-dim-label">维度</span>
                <button type="button" className={`ch-tab ${dimension === "brand" ? "active" : ""}`} onClick={() => setDimension("brand")}>
                  品牌
                </button>
                <button type="button" className={`ch-tab ${dimension === "model" ? "active" : ""}`} onClick={() => setDimension("model")}>
                  模型
                </button>
              </div>
            ) : null}
          </div>
          <div className="board-actions">
            <div className="ch-tabs board-personal-tabs">
              <button type="button" className={`ch-tab ${activeBoardTab === "favorites" ? "active" : ""}`} onClick={() => switchBoardTab("favorites")}>
                ★ 我的关注 <span className="cnt">{favoriteCount}</span>
              </button>
              <button type="button" className={`ch-tab ${activeBoardTab === "private" ? "active" : ""}`} onClick={() => switchBoardTab("private")}>
                ＋ 我的私有通道 <span className="cnt">{privateCount}</span>
              </button>
            </div>
            <div className="board-quick-actions">
              <a className="btn btn-ghost btn-sm" href="/console">
                个人中心 →
              </a>
              <button type="button" className="btn btn-primary btn-sm" onClick={requestPrivateCreator}>
                ＋ 添加我的通道
              </button>
            </div>
          </div>
        </div>

        <FilterBar className="public-board-filter">
          <SelectField aria-label="category filter" value={category} onChange={(event) => setCategory(event.target.value)}>
            <option value="all">所有分类</option>
            {categoryOptions.map((item) => (
              <option value={item.value} key={item.value}>
                {item.label}
              </option>
            ))}
          </SelectField>
          <SelectField aria-label="provider filter" value={provider} onChange={(event) => setProvider(event.target.value)}>
            <option value="all">所有服务商</option>
            {providers.map((item) => (
              <option value={item} key={item}>
                {item}
              </option>
            ))}
          </SelectField>
          <SelectField aria-label="channel filter" value={channelFilter} onChange={(event) => setChannelFilter(event.target.value)}>
            <option value="all">所有通道</option>
            {channelOptions.map((item) => (
              <option value={item.value} key={item.value}>
                {item.label}
              </option>
            ))}
          </SelectField>
          <SelectField aria-label="status filter" value={status} onChange={(event) => setStatus(event.target.value)}>
            {statusOptions.map(([value, label]) => (
              <option value={value} key={value}>
                {value === "all" ? "所有状态" : label}
              </option>
            ))}
          </SelectField>
          <div className="tb-search public-board-search">
            <span>⌕</span>
            <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder={activeBoardTab === "private" ? "搜索私有通道 / 端点 / 模型…" : "搜索服务商 / 模型…"} />
          </div>
          <div className="seg public-range-seg">
            {["24", "7", "30", "all"].map((item) => (
              <button className={[range === item ? "active" : "", refreshingPublic && range === item ? "refreshing" : ""].filter(Boolean).join(" ")} key={item} onClick={() => setRange(item)}>
                {rangeOptionLabel(item)}
              </button>
            ))}
          </div>
        </FilterBar>

        <div className={`card board public-board ${refreshingPublic && activeBoardTab !== "private" ? "is-refreshing" : ""}`} aria-busy={refreshingPublic && activeBoardTab !== "private"}>
          {activeBoardTab === "all" && initialPublicLoading ? <div className="empty-state"><div className="ico">⌁</div><h4>正在加载通道数据</h4><p>公开看板正在读取 `/api/public/*`。</p></div> : null}
          {activeBoardTab === "all" && !initialPublicLoading && dimension === "brand" ? <BrandTable channels={brandChannels} favorites={favoriteIDs} range={visiblePublicRange} onToggleFavorite={(id) => void toggleFavorite(id)} onOpenChannel={setSelectedChannel} /> : null}
          {activeBoardTab === "all" && !initialPublicLoading && dimension === "model" ? <ModelTable rows={modelRows} range={visiblePublicRange} /> : null}
          {activeBoardTab === "favorites" ? (
            <PublicFavoritePanel
              loading={personalLoading}
              channels={brandFavorites}
              total={favoriteItems.length}
              favorites={favoriteIDs}
              range={visiblePublicRange}
              onToggleFavorite={(id) => void toggleFavorite(id)}
              onOpenChannel={setSelectedChannel}
              onExplore={() => switchBoardTab("all")}
            />
          ) : null}
          {activeBoardTab === "private" ? (
            <PublicPrivateChannelPanel
              loading={personalLoading}
              items={filteredPrivate}
              total={privateItems.length}
              probingID={probingPrivateID}
              onCreate={requestPrivateCreator}
              onProbe={(id) => void probePrivateNow(id)}
              onDelete={(id) => void removePrivateChannel(id)}
            />
          ) : null}
        </div>

        <MonitoringStrategySection />

        <div className="section-head" id="providers">
          <h2>
            供应商排行 <span className="tag">{rangeOptionLabel(visiblePublicRange)}</span>
          </h2>
          <span className="sub">基于真实调用成功率、P95 延迟与故障次数综合排序</span>
        </div>
        <div className="grid cols-2">
          <div className="card card-pad provider-rank-card">
            <div className="module-title">真实调用成功率 TOP</div>
            <div className="module-sub">真实成功率 · 越高越稳定</div>
            <div className="rank provider-rank-list">
              {rank.map((item, index) => (
                <div className="rank-row" key={item.provider}>
                  <span className="n">{index + 1}</span>
                  <b>{item.provider}</b>
                  <span>{item.healthy}/{item.channels} healthy</span>
                  <strong>{item.successRate.toFixed(1)}%</strong>
                </div>
              ))}
            </div>
          </div>
          <div className="card card-pad error-summary-card">
            <div className="module-title">错误分类分布</div>
            <div className="module-sub">{rangeOptionLabel(visiblePublicRange)}失败探测的错误类型占比</div>
            <ErrorBars errors={errors} />
          </div>
        </div>
      </main>
      <ChannelPreviewDialog channel={selectedChannel} range={visiblePublicRange} isFavorite={selectedChannel ? favoriteIDs.has(selectedChannel.id) : false} onToggleFavorite={(id) => void toggleFavorite(id)} onClose={() => setSelectedChannel(null)} />
      <AuthDialog
        open={authOpen}
        onOpenChange={(open) => {
          setAuthOpen(open);
          if (!open) setPendingAction(null);
        }}
        registrationOpen={registrationOpen}
        showRegisterCta={showRegisterCta}
        emailVerificationRequired={emailVerificationRequired}
        onAuthenticated={(nextUser) => void handleAuthenticated(nextUser)}
      />
      <PrivateQuickCreateDialog
        open={privateFormOpen}
        form={privateForm}
        saving={privateSaving}
        onOpenChange={setPrivateFormOpen}
        onFormChange={setPrivateForm}
        onSubmit={savePrivateChannel}
      />
      <Footer />
    </>
  );
}

function MonitoringStrategySection() {
  return (
    <section className="monitoring-strategy-section" id="strategy" aria-labelledby="strategy-title">
      <div className="section-head">
        <h2 id="strategy-title">
          监控策略 <span className="tag">分层探测</span>
        </h2>
        <span className="sub">基础监控判断入口是否健康，真实监控判断模型是否可用</span>
      </div>
      <div className="grid cols-2 monitoring-strategy-grid">
        <div className="card card-pad monitor-strategy-card">
          <div className="module-title">两类策略 · 三层探测</div>
          <div className="module-sub">基础状态与真实可用性分开存储，前台合成为一个最终状态</div>
          <div className="monitor-lanes">
            <section className="monitor-lane">
              <div className="monitor-lane-head">
                <span className="monitor-lane-icon">L1</span>
                <div>
                  <h3>基础监控</h3>
                  <p>高频、低成本，不消耗 Token，负责发现链路与入口问题</p>
                </div>
              </div>
              <div className="layer-row">
                <span className="layer-badge">L1</span>
                <div>
                  <b>基础连通探测</b>
                  <p>DNS、TCP、TLS、HTTP 是否正常</p>
                </div>
                <span className="token-tag b-green">不消耗</span>
              </div>
              <div className="layer-row">
                <span className="layer-badge">L2</span>
                <div>
                  <b>API 元信息探测</b>
                  <p>Key 是否有效、/v1/models 是否可访问、模型是否存在</p>
                </div>
                <span className="token-tag b-green">不消耗</span>
              </div>
            </section>
            <section className="monitor-lane">
              <div className="monitor-lane-head">
                <span className="monitor-lane-icon l3">L3</span>
                <div>
                  <h3>真实 API 监控</h3>
                  <p>低频、高可信，用最小请求验证模型能否真实返回内容</p>
                </div>
              </div>
              <div className="layer-row l3-row">
                <span className="layer-badge l3">L3</span>
                <div>
                  <b>真实生成探测</b>
                  <p>校验 content、finish_reason、usage、tokens/sec 和首 token</p>
                </div>
                <span className="token-tag b-amber">少量消耗</span>
              </div>
              <p className="strategy-note">
                能发现“入口正常但模型不可用”、认证通过但生成超时、HTTP 200 但内容为空、模型路由错误和返回格式异常
              </p>
            </section>
          </div>
        </div>
        <div className="card card-pad status-matrix-card">
          <div className="module-title">状态判断矩阵</div>
          <div className="module-sub">由基础监控与真实监控结果合成前台最终状态</div>
          <table className="matrix strategy-matrix" aria-label="状态判断矩阵">
            <thead>
              <tr>
                <th>基础监控</th>
                <th>真实监控</th>
                <th>最终状态</th>
              </tr>
            </thead>
            <tbody>
              <StrategyMatrixRow base="正常" baseTone="green" real="正常" realTone="green" result="Healthy" resultClass="b-green" />
              <StrategyMatrixRow base="异常" baseTone="red" real="未执行" realTone="gray" result="Connectivity Down" resultClass="b-red" />
              <StrategyMatrixRow base="正常" baseTone="green" real="异常" realTone="red" result="Functional Down" resultClass="b-magenta" />
              <StrategyMatrixRow base="正常" baseTone="green" real="慢响应" realTone="amber" result="Degraded" resultClass="b-amber" />
              <StrategyMatrixRow base="正常" baseTone="green" real="额度异常" realTone="amber" result="Quota Risk" resultClass="b-amber" />
              <StrategyMatrixRow base="正常" baseTone="green" real="认证异常" realTone="red" result="Auth Error" resultClass="b-red" />
              <StrategyMatrixRow base="无数据" baseTone="gray" real="无数据" realTone="gray" result="Unknown" resultClass="b-gray" />
            </tbody>
          </table>
        </div>
      </div>
    </section>
  );
}

function StrategyMatrixRow({
  base,
  baseTone,
  real,
  realTone,
  result,
  resultClass
}: {
  base: string;
  baseTone: "green" | "red" | "amber" | "gray";
  real: string;
  realTone: "green" | "red" | "amber" | "gray";
  result: string;
  resultClass: string;
}) {
  return (
    <tr>
      <td><StrategySignal tone={baseTone}>{base}</StrategySignal></td>
      <td><StrategySignal tone={realTone}>{real}</StrategySignal></td>
      <td><span className={`badge ${resultClass} dot`}>{result}</span></td>
    </tr>
  );
}

function StrategySignal({ tone, children }: { tone: "green" | "red" | "amber" | "gray"; children: string }) {
  return <span className={`strategy-signal ${tone}`}>{children}</span>;
}

function KPI({ label, value, unit, foot, color, soft }: { label: string; value: string; unit?: string; foot: string; color: string; soft: string }) {
  return (
    <div className="kpi" style={{ "--c": color, "--cs": soft } as CSSProperties}>
      <div className="k-top">
        <span className="k-label">{label}</span>
        <span className="k-ico">✓</span>
      </div>
      <div className="k-value">
        {value}
        {unit ? <span className="unit">{unit}</span> : null}
      </div>
      <div className="k-foot">{foot}</div>
    </div>
  );
}

function PublicFavoritePanel({
  loading,
  channels,
  total,
  favorites,
  range,
  onToggleFavorite,
  onOpenChannel,
  onExplore
}: {
  loading: boolean;
  channels: BrandMonitorRow[];
  total: number;
  favorites: Set<string>;
  range: string;
  onToggleFavorite: (channelID: string) => void;
  onOpenChannel: (channel: PublicChannel) => void;
  onExplore: () => void;
}) {
  if (loading) {
    return <div className="empty-state"><div className="ico">★</div><h4>正在加载我的关注</h4><p>读取你收藏的平台通道和最新状态。</p></div>;
  }
  if (!total) {
    return (
      <div className="empty-state public-personal-empty">
        <div className="ico">★</div>
        <h4>还没有关注的平台通道</h4>
        <p>在全部通道里点击星标，后续可以直接在监控总览查看关注通道状态。</p>
        <button className="btn btn-primary btn-sm" type="button" onClick={onExplore}>去全部通道添加关注</button>
      </div>
    );
  }
  return <BrandTable channels={channels} favorites={favorites} range={range} onToggleFavorite={onToggleFavorite} onOpenChannel={onOpenChannel} />;
}

function PublicPrivateChannelPanel({
  loading,
  items,
  total,
  probingID,
  onCreate,
  onProbe,
  onDelete
}: {
  loading: boolean;
  items: PrivateChannel[];
  total: number;
  probingID: string;
  onCreate: () => void;
  onProbe: (channelID: string) => void;
  onDelete: (channelID: string) => void;
}) {
  if (loading) {
    return <div className="empty-state"><div className="ico">＋</div><h4>正在加载我的私有通道</h4><p>读取只属于你的私有通道和探测结果。</p></div>;
  }
  if (!total) {
    return (
      <div className="empty-state public-personal-empty">
        <div className="ico">＋</div>
        <h4>还没有添加私有通道</h4>
        <p>添加你自己的中转站 Endpoint 和 API Key，TokHub 会按额度做真实探测。</p>
        <div className="empty-actions">
          <button className="btn btn-primary btn-sm" type="button" onClick={onCreate}>＋ 添加私有通道</button>
          <a className="btn btn-ghost btn-sm" href="/console/channels">进入控制台管理</a>
        </div>
      </div>
    );
  }
  if (!items.length) {
    return <div className="empty-state"><div className="ico">⌕</div><h4>没有匹配的私有通道</h4><p>调整关键词或状态筛选后再试。</p></div>;
  }
  return (
    <>
      <div className="public-personal-toolbar">
        <div>
          <b>我的私有通道</b>
          <span>只显示当前账号创建的通道，API Key 不回显明文</span>
        </div>
        <button className="btn btn-primary btn-sm" type="button" onClick={onCreate}>＋ 添加私有通道</button>
      </div>
      <div className="scroll">
        <table className="tk public-private-table">
          <thead>
            <tr>
              <th>通道 / 端点</th>
              <th>状态</th>
              <th>L3 延迟</th>
              <th>24h 可用率</th>
              <th>今日配额</th>
              <th>Key</th>
              <th>最后探测</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {items.map((item) => (
              <tr key={item.id}>
                <td>
                  <div className="ch-name">
                    <span className="mark" style={{ background: item.mark }}>{item.provider[0]}</span>
                    <div>
                      <b>{item.name}</b>
                      <small className="mono">{item.endpoint}</small>
                    </div>
                  </div>
                </td>
                <td><StatusBadge status={item.status} label={item.statusLabel} /></td>
                <td className="num">{item.l3LatencyMs ? `${item.l3LatencyMs}ms` : "—"}</td>
                <td className="num">{item.uptime24h.toFixed(1)}%</td>
                <td><span className="quota-tag">{item.probesUsedToday} / {item.probeDaily}</span></td>
                <td><span className="keyval">{item.keyMask}<span className="rev">{shortFingerprint(item.keyFingerprint)}</span></span></td>
                <td><span className="muted-time">{timeLabel(item.lastProbeAt)}</span></td>
                <td>
                  <div className="row-actions compact-actions">
                    <button className="btn btn-ghost btn-sm" type="button" disabled={probingID === item.id || item.quotaExhausted} onClick={() => onProbe(item.id)}>
                      {probingID === item.id ? "探测中" : item.quotaExhausted ? "配额用尽" : "探测"}
                    </button>
                    <button className="btn btn-ghost btn-sm danger-lite" type="button" onClick={() => onDelete(item.id)}>删除</button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="board-foot">
        <span>需要编辑、批量治理或配置专属网关时进入用户控制台。</span>
        <a className="btn btn-ghost btn-sm" href="/console#mine">进入控制台管理 →</a>
      </div>
    </>
  );
}

function BrandTable({ channels, favorites, range, onToggleFavorite, onOpenChannel }: { channels: BrandMonitorRow[]; favorites: Set<string>; range: string; onToggleFavorite: (channelID: string) => void; onOpenChannel: (channel: PublicChannel) => void }) {
  if (!channels.length) {
    return <div className="empty-state"><div className="ico">⌕</div><h4>没有匹配通道</h4><p>调整服务商、状态或搜索条件后再试。</p></div>;
  }
  const trendLabel = trendHeaderLabel(range);
  const uptimeLabel = uptimeHeaderLabel(range);
  return (
    <>
      <div className="scroll">
        <table className="tk brand-table">
          <colgroup>
            <col className="brand-col-fav" />
            <col className="brand-col-name" />
            <col className="brand-col-status" />
            <col className="brand-col-layers" />
            <col className="brand-col-l3" />
            <col className="brand-col-latency" />
            <col className="brand-col-uptime" />
            <col className="brand-col-score" />
            <col className="brand-col-trend" />
            <col className="brand-col-action" />
          </colgroup>
          <thead>
            <tr>
              <th className="fav-col">关注</th>
              <th>服务商 / 通道</th>
              <th>综合状态</th>
              <th>基础监控 · L1/L2</th>
              <th>真实监控 · L3</th>
              <th>真实延迟</th>
              <th>{uptimeLabel}</th>
              <th>质量评分</th>
              <th>{trendLabel}</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {channels.map((ch) => (
              <tr className="channel-click-row" key={ch.id} onClick={() => onOpenChannel(ch.primaryChannel)}>
                <td className="fav-col">
                  <button
                    className={`fav-btn ${ch.channels.some((item) => favorites.has(item.id)) ? "on" : ""}`}
                    onClick={(event) => {
                      event.stopPropagation();
                      onToggleFavorite(ch.primaryChannel.id);
                    }}
                    title={ch.channels.some((item) => favorites.has(item.id)) ? "取消关注" : "加入关注"}
                  >
                    ★
                  </button>
                </td>
                <td>
                  <div className="ch-name">
                    <span className="mark" style={{ background: ch.mark }}>{ch.initial}</span>
                    <div>
                      <b>{ch.name}</b>
                      <small>{ch.subtitle}</small>
                    </div>
                  </div>
                </td>
                <td>
                  <StatusBadge status={ch.status} label={ch.statusLabel} />
                  <div className="st-sub"><span className="st-time">{timeLabel(ch.lastProbeAt)}</span>{ch.errorType ? <><span className="st-sep">·</span><span className="st-err">{ch.errorType}</span></> : null}</div>
                </td>
                <td><LayerPair a={ch.l1Status} b={ch.l2Status} aMs={ch.l1LatencyMs} bMs={ch.l2LatencyMs} /></td>
                <td><LayerPair a={ch.l3Status} b={`${ch.successRate.toFixed(1)}%`} aMs={ch.l3LatencyMs} /></td>
                <td className="num">{ch.latencyP95Ms ? `${ch.latencyP95Ms}ms` : "—"}</td>
                <td className="num">{ch.uptime24h.toFixed(1)}%</td>
                <td><Score value={ch.score} /></td>
                <td className="channel-trend-cell tk-trend-cell"><TrendBars values={trendValues(ch)} maxBars={trendBarCount(ch, range)} label={trendLabel} maxWidth="146px" /></td>
                <td className="channel-action-cell"><OfficialRegisterAction channel={ch.primaryChannel} /></td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="board-foot">
        <span className="legend">
          <span><i className="legend-dot-ok" />正常</span>
          <span><i className="legend-dot-warn" />降级/慢响应</span>
          <span><i className="legend-dot-error" />故障</span>
        </span>
        <span>公开 API 每 10 秒基础缓存，阶段 3 接入真实探测刷新</span>
      </div>
    </>
  );
}

function ChannelPreviewDialog({ channel, range, isFavorite, onToggleFavorite, onClose }: { channel: PublicChannel | null; range: string; isFavorite: boolean; onToggleFavorite: (channelID: string) => void; onClose: () => void }) {
  const open = Boolean(channel);
  if (!channel) {
    return null;
  }
  const statusTone = scoreColor(channel.score);
  const waterfall = buildPreviewWaterfall(channel);
  const officialHref = officialExperienceHref(channel);
  return (
    <div className={`drawer-mask ${open ? "open" : ""}`} role="presentation" onClick={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <section className={`drawer channel-preview-drawer ${open ? "open" : ""}`} role="dialog" aria-modal="true" aria-labelledby="channel-preview-title" onClick={(event) => event.stopPropagation()}>
        <div className="dh">
          <div className="preview-title">
            <div className="mark" style={{ background: channel.mark }}>{channel.provider[0]}</div>
            <div>
              <h3 id="channel-preview-title">{stationDisplayName(channel)}</h3>
              <div>{stationSubtitle(channel)}</div>
            </div>
            <span className={`badge ${statusClass[channel.status] ?? "b-gray"} dot`}>{channel.statusLabel}</span>
          </div>
          <div className="drawer-actions">
            <button
              className={`fav-btn fav-btn-lg ${isFavorite ? "on" : ""}`}
              onClick={() => onToggleFavorite(channel.id)}
              title={isFavorite ? "取消关注" : "加入关注"}
            >
              ★
            </button>
            <button className="icon-btn" onClick={onClose} aria-label="关闭通道预览">✕</button>
          </div>
        </div>

        <div className="db">
          <div className="preview-summary">
            <span className={`badge ${statusClass[channel.status] ?? "b-gray"} dot preview-status`}>{channel.statusLabel}</span>
            <span className={`diagnosis-pill ${diagnosisClass(channel.diagnosis?.severity)}`} title={channel.diagnosis?.hint || channel.diagnosis?.label}>
              {channel.diagnosis?.label || "等待诊断"}
            </span>
            <div className="score preview-score">
              <div className="ring" style={{ background: `conic-gradient(${statusTone} ${Math.max(channel.score, 0) * 3.6}deg,#eef0f4 0)` }}>
                <span style={{ color: statusTone }}>{channel.score}</span>
              </div>
              <small>质量评分</small>
            </div>
          </div>

          <div className="preview-grid">
            <PreviewMetric label={uptimeHeaderLabel(range)} value={`${channel.uptime24h.toFixed(1)}%`} />
            <PreviewMetric label="真实延迟" value={channel.latencyP95Ms ? `${channel.latencyP95Ms}ms` : "—"} />
            <PreviewMetric label="成功率" value={`${channel.successRate.toFixed(1)}%`} />
            <PreviewMetric label="L1 / L2" value={`${channel.l1LatencyMs || 0}ms / ${channel.l2LatencyMs || 0}ms`} />
            <PreviewMetric label="今日成本" value={formatSmallUSD(channel.costUsd)} />
            <PreviewMetric label="最后探测" value={timeLabel(channel.lastProbeAt)} />
          </div>

          <div className="module-title preview-section-title">基础链路瀑布 · L1/L2</div>
          <div className="fall preview-fall">
            {waterfall.map((item) => (
              <div className="f" key={item.label}>
                <span className="fl">{item.label}</span>
                <span className="ft"><i className={item.className} style={{ left: `${item.left}%`, width: `${item.width}%` }} /></span>
                <span className="fv">{item.value}</span>
              </div>
            ))}
          </div>

          <div className="module-title preview-section-title preview-section-title-tight">{trendHeaderLabel(range)}</div>
          <div className="preview-trend">
            <Spark values={trendValues(channel)} />
            <div className="heat">{trendValues(channel).slice(-24).map((value, index) => <i className={heatClass(value)} key={`${value ?? "empty"}-${index}`} />)}</div>
          </div>

          <div className="module-title preview-section-title preview-section-title-spaced">真实探测日志 · L3</div>
          <div className="log">
            <div className="mu">POST /v1/chat/completions</div>
            <div className={channel.l3Status === "ok" ? "ok" : channel.l3Status === "warn" ? "wn" : "er"}>{channel.l3Status === "ok" ? "200 OK" : channel.l3Status === "warn" ? "200 OK · slow response" : "probe failed"} · model={channel.upstreamModel || channel.model}</div>
            <div className={channel.successRate >= 99 ? "ok" : "wn"}>content check · success_rate={channel.successRate.toFixed(1)}% · p95={channel.latencyP95Ms || 0}ms</div>
            <div className={channel.diagnosis?.severity === "error" ? "er" : channel.diagnosis?.severity === "warn" ? "wn" : "ok"}>diagnosis={channel.diagnosis?.code || "unknown"} · {channel.diagnosis?.label || "等待诊断"}</div>
            {channel.errorType ? <div className="er">last_error={channel.errorType}</div> : <div className="ok">no active error</div>}
          </div>
        </div>

        <div className="df">
          <span className="preview-refresh-note">数据每 30s / 5min 自动刷新</span>
          <div className="drawer-actions">
            <button className="btn btn-ghost btn-sm" onClick={onClose}>关闭</button>
            {officialHref ? (
              <a className="btn btn-ghost btn-sm official-entry-btn" href={officialHref} target="_blank" rel="noopener noreferrer" title="新标签页访问通道官网">
                访问官网
              </a>
            ) : null}
            <a className="btn btn-primary btn-sm" href={publicChannelPath(channel)}>查看完整详情 →</a>
          </div>
        </div>
      </section>
    </div>
  );
}

function PreviewMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="card card-pad preview-metric">
      <div>{label}</div>
      <b>{value}</b>
    </div>
  );
}

function AuthDialog({
  open,
  onOpenChange,
  registrationOpen,
  showRegisterCta,
  emailVerificationRequired,
  onAuthenticated
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  registrationOpen: boolean;
  showRegisterCta: boolean;
  emailVerificationRequired: boolean;
  onAuthenticated: (user: User) => void;
}) {
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
      description="登录后可在当前监控总览查看我的关注、管理私有通道，并继续刚才的操作。"
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

function PrivateQuickCreateDialog({
  open,
  form,
  saving,
  onOpenChange,
  onFormChange,
  onSubmit
}: {
  open: boolean;
  form: PrivateChannelInput;
  saving: boolean;
  onOpenChange: (open: boolean) => void;
  onFormChange: (value: PrivateChannelInput) => void;
  onSubmit: (event: FormEvent) => void;
}) {
  const canSubmit = Boolean(form.endpoint.trim() && form.apiKey?.trim() && form.model.trim());
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="添加私有通道"
      description="只保存你自己的 Endpoint 和 API Key，监控总览展示状态，复杂管理仍在用户控制台完成。"
      footer={
        <>
          <button type="button" className="btn btn-ghost btn-sm" onClick={() => onOpenChange(false)}>取消</button>
          <button type="submit" form="public-private-create" className="btn btn-primary btn-sm" disabled={saving || !canSubmit}>
            {saving ? "保存中..." : "保存并开始监控"}
          </button>
        </>
      }
    >
      <form id="public-private-create" className="tk-form public-private-form" onSubmit={onSubmit}>
        <div className="row">
          <label htmlFor="public-private-name">通道名称</label>
          <input id="public-private-name" value={form.name} onChange={(event) => onFormChange({ ...form, name: event.target.value })} placeholder="例如：我的 Claude 中转站" />
        </div>
        <div className="row">
          <label htmlFor="public-private-provider">服务商</label>
          <SelectField id="public-private-provider" value={form.provider} onChange={(event) => onFormChange({ ...form, provider: event.target.value })}>
            <option value="OpenAI">OpenAI</option>
            <option value="Anthropic">Anthropic</option>
            <option value="Gemini">Gemini</option>
            <option value="Mixed">混合通道</option>
          </SelectField>
        </div>
        <div className="row">
          <label htmlFor="public-private-type">接口类型</label>
          <SelectField id="public-private-type" value={form.type} onChange={(event) => onFormChange({ ...form, type: event.target.value })}>
            <option value="openai-compatible">OpenAI 兼容</option>
            <option value="anthropic">Claude 通道</option>
            <option value="gemini">Gemini 通道</option>
            <option value="mixed">混合通道</option>
          </SelectField>
        </div>
        <div className="row">
          <label htmlFor="public-private-endpoint">Endpoint URL</label>
          <input id="public-private-endpoint" value={form.endpoint} onChange={(event) => onFormChange({ ...form, endpoint: event.target.value })} placeholder="https://api.example.com/v1" />
        </div>
        <div className="row">
          <label htmlFor="public-private-key">API Key</label>
          <input id="public-private-key" type="password" value={form.apiKey ?? ""} onChange={(event) => onFormChange({ ...form, apiKey: event.target.value })} placeholder="sk-..." />
        </div>
        <div className="row">
          <label htmlFor="public-private-model">探测模型</label>
          <input id="public-private-model" value={form.model} onChange={(event) => onFormChange({ ...form, model: event.target.value })} placeholder="gpt-4o-mini / claude-sonnet-4-6" />
        </div>
        <div className="row">
          <label htmlFor="public-private-quota">每日探测额度</label>
          <input id="public-private-quota" type="number" min={1} max={500} value={form.probeDaily} onChange={(event) => onFormChange({ ...form, probeDaily: Number(event.target.value) || 50 })} />
        </div>
        <div className="form-note">Key 会加密保存，监控总览和后台都不会回显明文。</div>
      </form>
    </Dialog>
  );
}

function buildPreviewWaterfall(channel: PublicChannel) {
  const dns = Math.max(18, Math.round((channel.l1LatencyMs || 160) * 0.18));
  const tcp = Math.max(24, Math.round((channel.l1LatencyMs || 160) * 0.28));
  const tls = Math.max(30, Math.round((channel.l1LatencyMs || 160) * 0.36));
  const app = Math.max(18, Math.round((channel.l2LatencyMs || 220) * 0.28));
  const values = [
    { label: "DNS", offset: 0, value: dns, className: dotClass(channel.l1Status) },
    { label: "TCP", offset: dns, value: tcp, className: dotClass(channel.l1Status) },
    { label: "TLS", offset: dns + tcp, value: tls, className: dotClass(channel.l1Status) },
    { label: "HTTP", offset: dns + tcp + tls, value: app, className: dotClass(channel.l2Status) }
  ];
  const max = Math.max(...values.map((item) => item.offset + item.value), 240);
  return values.map((item) => ({
    label: item.label,
    className: item.className.replace("s-", ""),
    left: Math.min((item.offset / max) * 100, 96),
    width: Math.max((item.value / max) * 100, 3),
    value: channel.l1Status === "down" && item.label !== "DNS" ? "—" : `${item.value}ms`
  }));
}

function heatClass(value: number | null) {
  if (value === null || value <= 0) return "na";
  if (value < 50) return "down";
  if (value < 75) return "warn";
  return "";
}

function scoreColor(score: number) {
  if (score >= 90) return "var(--green)";
  if (score >= 75) return "var(--blue)";
  if (score >= 60) return "var(--amber)";
  return "var(--red)";
}

type ModelRow = {
  key: string;
  model: CoreMonitorModel;
  providers: string[];
  primaryType: string;
  channels: PublicChannel[];
  brandRows: BrandMonitorRow[];
  onlineCount: number;
  bestLatency: PublicChannel | null;
  cheapest: PublicChannel | null;
  avgLatency: number | null;
  avgSuccess: number;
  avgScore: number;
  trend: number[];
  trendBuckets: TrendBucket[];
};

type BrandMonitorRow = {
  id: string;
  key: string;
  name: string;
  subtitle: string;
  initial: string;
  mark: string;
  channels: PublicChannel[];
  primaryChannel: PublicChannel;
  modelCoverage: number;
  status: string;
  statusLabel: string;
  score: number;
  uptime24h: number;
  successRate: number;
  latencyP95Ms: number;
  l1Status: string;
  l2Status: string;
  l3Status: string;
  l1LatencyMs: number;
  l2LatencyMs: number;
  l3LatencyMs: number;
  errorType: string;
  lastProbeAt: string;
  trend: number[];
  trendBuckets: TrendBucket[];
};

function ModelTable({ rows, range }: { rows: ModelRow[]; range: string }) {
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set([rows[0]?.key ?? DEFAULT_CORE_MONITOR_MODELS[0].key]));

  useEffect(() => {
    if (!rows.length) return;
    setExpanded((current) => {
      const rowKeys = new Set(rows.map((row) => row.key));
      if ([...current].some((key) => rowKeys.has(key))) return current;
      return new Set([rows[0].key]);
    });
  }, [rows]);

  function toggle(modelKey: string) {
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(modelKey)) {
        next.delete(modelKey);
      } else {
        next.add(modelKey);
      }
      return next;
    });
  }

  if (!rows.length) {
    return <div className="empty-state"><div className="ico">⌕</div><h4>暂无模型维度数据</h4><p>管理员录入平台通道并完成探测后，这里会展示模型聚合表现。</p></div>;
  }
  const trendLabel = trendHeaderLabel(range);
  return (
    <div className="scroll">
      <table className="tk model-table">
        <colgroup>
          <col className="model-col-name" />
          <col className="model-col-online" />
          <col className="model-col-best" />
          <col className="model-col-latency" />
          <col className="model-col-success" />
          <col className="model-col-price" />
          <col className="model-col-score" />
          <col className="model-col-trend" />
          <col className="model-col-action" />
        </colgroup>
        <thead>
          <tr>
            <th>模型</th>
            <th>在线家数</th>
            <th>最快</th>
            <th>平均延迟</th>
            <th>平均成功率</th>
            <th>最便宜</th>
            <th>平均评分</th>
            <th>{trendLabel}</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => {
            const open = expanded.has(row.key);
            return (
              <Fragment key={row.key}>
                <tr className={`model-row model-accordion-row ${open ? "is-open" : ""}`} onClick={() => toggle(row.key)}>
                  <td>
                    <div className="mname">
                      <button
                        type="button"
                        className="model-expand-btn"
                        aria-label={`${open ? "收起" : "展开"} ${row.model.label}`}
                        aria-expanded={open}
                        onClick={(event) => {
                          event.stopPropagation();
                          toggle(row.key);
                        }}
                      >
                        <span className="chev">⌄</span>
                      </button>
                      <span className={`mname-ico ${modelBrandClass(row.primaryType)}`}>{modelInitial(row.primaryType, row.model.key)}</span>
                      <div className="meta">
                        <div className="name">
                          <span>{row.model.label}</span>
                          <span className={`brand-tag ${modelBrandTagClass(row.primaryType)}`}>{channelTypeLabel(row.primaryType)}</span>
                        </div>
                        <div className="sub" title={row.providers.join(" / ")}>
                          {row.providers.length ? row.providers.join(" / ") : "等待品牌接入"}
                        </div>
                      </div>
                    </div>
                  </td>
                  <td className="num">
                    <span className="online-count"><b>{row.onlineCount}</b><em>/ {row.brandRows.length}</em></span>
                  </td>
                  <td>
                    <div className="best-cell">
                      <b title={row.bestLatency?.provider}>{row.bestLatency ? stationDisplayName(row.bestLatency) : "—"}</b>
                      <small>{formatLatency(row.bestLatency?.latencyP95Ms)}</small>
                    </div>
                  </td>
                  <td className="num metric-strong">{formatLatency(row.avgLatency)}</td>
                  <td className="num success-rate">{row.channels.length ? `${row.avgSuccess.toFixed(1)}%` : "—"}</td>
                  <td>
                    <div className="best-cell">
                      <b title={row.cheapest?.provider}>{row.cheapest ? stationDisplayName(row.cheapest) : "—"}</b>
                      <small>{row.cheapest ? `$${row.cheapest.costUsd.toFixed(3)}` : "—"}</small>
                    </div>
                  </td>
                  <td><ScoreRing value={row.avgScore} /></td>
                  <td className="model-trend-cell tk-trend-cell"><TrendBars values={trendValues(row)} maxBars={trendBarCount(row, range)} label={trendLabel} maxWidth="142px" /></td>
                  <td className="channel-action-cell"><OfficialRegisterAction channel={modelActionChannel(row)} /></td>
                </tr>
                {open ? (
                  <tr className="mrow-detail">
                    <td colSpan={9}>
                      <div className="det-wrap">
                        <div className="det-h">
                          品牌监控明细
                          <span className="pill badge b-blue">{row.model.label}</span>
                          <span className="pill badge b-gray">{row.brandRows.length} 个品牌</span>
                        </div>
                        {row.brandRows.length ? (
                          <table className="det-tk core-model-brand-table">
                            <thead>
                              <tr>
                                <th>品牌</th>
                                <th>状态</th>
                                <th>L1 / L2</th>
                                <th>L3</th>
                                <th>P95</th>
                                <th>成功率</th>
                                <th>评分</th>
                              </tr>
                            </thead>
                            <tbody>
                              {row.brandRows.map((brand, index) => (
                                <tr key={`${row.key}-${brand.key}`}>
                                  <td>
                                    <span className="core-brand-name">
                                      <span className="rank-pill">{index + 1}</span>
                                      <strong>{brand.name}</strong>
                                    </span>
                                  </td>
                                  <td><StatusBadge status={brand.status} label={brand.statusLabel} /></td>
                                  <td><LayerPair a={brand.l1Status} b={brand.l2Status} aMs={brand.l1LatencyMs} bMs={brand.l2LatencyMs} /></td>
                                  <td><LayerPair a={brand.l3Status} b={`${brand.successRate.toFixed(1)}%`} aMs={brand.l3LatencyMs} /></td>
                                  <td className="num">{formatLatency(brand.latencyP95Ms)}</td>
                                  <td className="num success-rate">{brand.successRate.toFixed(1)}%</td>
                                  <td><Score value={brand.score} /></td>
                                </tr>
                              ))}
                            </tbody>
                          </table>
                        ) : (
                          <div className="model-empty-detail">还没有品牌接入这个核心探测模型。</div>
                        )}
                      </div>
                    </td>
                  </tr>
                ) : null}
              </Fragment>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function OfficialRegisterAction({ channel }: { channel?: PublicChannel | null }) {
  const href = channel ? officialExperienceHref(channel) : "";
  if (!href) return <span className="channel-register-empty">-</span>;
  return (
    <a
      className="channel-register-link"
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      title="新标签页打开官网注册入口"
      onClick={(event) => event.stopPropagation()}
    >
      官网
    </a>
  );
}

function modelActionChannel(row: ModelRow) {
  return row.bestLatency || row.cheapest || row.channels[0] || null;
}

function publicMonitorModelsFromConfig(items?: MonitorModelConfig[]): CoreMonitorModel[] {
  const seen = new Set<string>();
  const models = (items ?? [])
    .filter((item) => item.enabled && item.model?.trim())
    .map((item) => {
      const key = (item.key || item.model).trim();
      const aliases = [key, item.model, item.upstreamModel, ...(item.aliases ?? [])]
        .map((value) => value?.trim())
        .filter((value): value is string => Boolean(value));
      return {
        key,
        label: (item.label || item.model).trim(),
        type: (item.type || "openai-compatible").trim(),
        aliases: Array.from(new Set(aliases))
      };
    })
    .filter((item) => {
      const normalized = normalizeModelKey(item.key);
      if (seen.has(normalized)) return false;
      seen.add(normalized);
      return true;
    });
  return models.length ? models : DEFAULT_CORE_MONITOR_MODELS;
}

function buildModelRows(channels: PublicChannel[], models: CoreMonitorModel[]): ModelRow[] {
  return models.map((model) => {
    const list = channels.filter((channel) => matchesCoreMonitorModel(channel, model));
    const brandRows = buildBrandRowsFromChannels(list, models);
    const availablePool = list.filter(isOnlineChannel);
    const candidatePool = availablePool.length ? availablePool : list;
    const latencyPool = candidatePool.filter((item) => item.latencyP95Ms > 0);
    const bestLatency = [...latencyPool].sort((a, b) => a.latencyP95Ms - b.latencyP95Ms)[0] ?? null;
    const cheapest = [...candidatePool].sort((a, b) => a.costUsd - b.costUsd)[0] ?? null;
    const avgLatency = latencyPool.length
      ? Math.round(latencyPool.reduce((sum, ch) => sum + ch.latencyP95Ms, 0) / latencyPool.length)
      : null;
    return {
      key: model.key,
      model,
      providers: Array.from(new Set(brandRows.map((row) => row.name))).sort(),
      primaryType: list.length ? dominantType(list) : model.type,
      channels: list,
      brandRows,
      onlineCount: brandRows.filter(isOnlineBrand).length,
      bestLatency,
      cheapest,
      avgLatency,
      avgSuccess: list.length ? list.reduce((sum, ch) => sum + ch.successRate, 0) / list.length : 0,
      avgScore: list.length ? Math.round(list.reduce((sum, ch) => sum + ch.score, 0) / list.length) : 0,
      trend: mergeTrend(list),
      trendBuckets: mergeTrendBuckets(list)
    };
  });
}

function isOnlineChannel(channel: PublicChannel) {
  return channel.status === "healthy" || channel.status === "degraded" || channel.l3Status === "ok";
}

function dominantType(channels: PublicChannel[]) {
  const counts = new Map<string, number>();
  for (const item of channels) {
    counts.set(item.type, (counts.get(item.type) ?? 0) + 1);
  }
  return [...counts.entries()].sort((a, b) => b[1] - a[1])[0]?.[0] ?? "openai-compatible";
}

function mergeTrend(channels: PublicChannel[]) {
  const maxLength = Math.max(...channels.map((item) => item.trend.length), 0);
  if (!maxLength) return [];
  const merged: number[] = [];
  for (let index = 0; index < maxLength; index += 1) {
    const values = channels
      .map((item) => item.trend[index])
      .filter((value): value is number => typeof value === "number");
    if (values.length) {
      merged.push(Math.round(values.reduce((sum, value) => sum + value, 0) / values.length));
    }
  }
  return merged.slice(-30);
}

function mergeTrendBuckets(channels: PublicChannel[]): TrendBucket[] {
  const template = channels.find((item) => item.trendBuckets?.length)?.trendBuckets ?? [];
  if (!template.length) return [];
  const bucketMaps = channels.map((channel) => new Map((channel.trendBuckets ?? []).map((bucket) => [bucket.key, bucket.value])));
  return template.map((bucket) => {
    const values = bucketMaps
      .map((map) => map.get(bucket.key))
      .filter((value): value is number => typeof value === "number");
    return {
      ...bucket,
      value: values.length ? Math.round(values.reduce((sum, value) => sum + value, 0) / values.length) : null
    };
  });
}

function trendValues(row: { trend: number[]; trendBuckets?: TrendBucket[] }) {
  return row.trendBuckets?.length ? row.trendBuckets.map((bucket) => bucket.value) : row.trend;
}

function trendBarCount(row: { trend: number[]; trendBuckets?: TrendBucket[] }, range: string) {
  if (row.trendBuckets?.length) return row.trendBuckets.length;
  if (range === "24") return 24;
  if (range === "7") return 7;
  return 30;
}

function buildBrandRows(items: PublicChannel[], models: CoreMonitorModel[]) {
  return buildBrandRowsFromChannels(items, models);
}

function buildBrandRowsFromChannels(items: PublicChannel[], models: CoreMonitorModel[]) {
  const order: string[] = [];
  const grouped = new Map<string, PublicChannel[]>();
  for (const item of items) {
    const key = stationKey(item);
    if (!grouped.has(key)) {
      order.push(key);
    }
    grouped.set(key, [...(grouped.get(key) ?? []), item]);
  }
  return order
    .map((key) => buildBrandMonitorRow(key, grouped.get(key) ?? [], models))
    .filter((item): item is BrandMonitorRow => Boolean(item))
    .sort((a, b) => {
      if (b.score !== a.score) return b.score - a.score;
      if (b.successRate !== a.successRate) return b.successRate - a.successRate;
      return a.name.localeCompare(b.name);
    });
}

function buildBrandMonitorRow(key: string, channels: PublicChannel[], models: CoreMonitorModel[]) {
  if (!channels.length) return null;
  const primaryChannel = pickBrandRepresentative(channels[0], channels.slice(1));
  const modelKeys = new Set(channels.map((channel) => coreModelForChannel(channel, models)).filter((item): item is CoreMonitorModel => Boolean(item)).map((item) => item.key));
  const status = aggregateChannelStatus(channels);
  const displayName = stationDisplayName(primaryChannel);
  return {
    id: key,
    key,
    name: displayName,
    subtitle: `核心模型 ${modelKeys.size}/${models.length} · ${dominantTypeLabel(channels)}`,
    initial: (displayName || primaryChannel.provider || primaryChannel.name).slice(0, 1).toUpperCase(),
    mark: primaryChannel.mark,
    channels,
    primaryChannel,
    modelCoverage: modelKeys.size,
    status,
    statusLabel: statusLabelFor(status),
    score: averageNumber(channels.map((channel) => channel.score)),
    uptime24h: averageNumber(channels.map((channel) => channel.uptime24h)),
    successRate: averageNumber(channels.map((channel) => channel.successRate)),
    latencyP95Ms: averageNumber(channels.map((channel) => channel.latencyP95Ms), { positiveOnly: true }),
    l1Status: aggregateLayerStatus(channels.map((channel) => channel.l1Status)),
    l2Status: aggregateLayerStatus(channels.map((channel) => channel.l2Status)),
    l3Status: aggregateLayerStatus(channels.map((channel) => channel.l3Status)),
    l1LatencyMs: averageNumber(channels.map((channel) => channel.l1LatencyMs), { positiveOnly: true }),
    l2LatencyMs: averageNumber(channels.map((channel) => channel.l2LatencyMs), { positiveOnly: true }),
    l3LatencyMs: averageNumber(channels.map((channel) => channel.l3LatencyMs), { positiveOnly: true }),
    errorType: uniqueText(channels.map((channel) => channel.errorType)).join(" / "),
    lastProbeAt: latestProbeAt(channels),
    trend: mergeTrend(channels),
    trendBuckets: mergeTrendBuckets(channels)
  } satisfies BrandMonitorRow;
}

function stationKey(channel: PublicChannel) {
  return stationDisplayName(channel).toLowerCase();
}

function stationDisplayName(channel: PublicChannel) {
  const provider = channel.provider.trim();
  if (provider) return provider;
  return stripModelSuffix(channel.name.trim()) || channel.name.trim();
}

function stationSubtitle(channel: PublicChannel) {
  return channelTypeLabel(channel.type || "openai-compatible");
}

function coreModelForChannel(channel: PublicChannel, models: CoreMonitorModel[]) {
  return models.find((model) => matchesCoreMonitorModel(channel, model)) ?? null;
}

function matchesCoreMonitorModel(channel: PublicChannel, model: CoreMonitorModel) {
  return [channel.model, channel.upstreamModel, channel.name].some((value) => {
    const normalized = normalizeModelKey(value);
    return normalized === model.key || model.aliases.some((alias) => normalized === normalizeModelKey(alias));
  });
}

function normalizeModelKey(value: string) {
  return value.trim().toLowerCase().replace(/_/g, "-").replace(/\s+/g, "-");
}

function dominantTypeLabel(channels: PublicChannel[]) {
  const types = Array.from(new Set(channels.map((channel) => channel.type).filter(Boolean)));
  if (types.length > 1) return "混合接口";
  return channelTypeLabel(types[0] || "openai-compatible");
}

function isOnlineBrand(row: BrandMonitorRow) {
  return row.status === "healthy" || row.status === "degraded" || row.l3Status === "ok";
}

function aggregateChannelStatus(channels: PublicChannel[]) {
  return [...channels].sort((a, b) => channelSeverity(b) - channelSeverity(a))[0]?.status ?? "unknown";
}

function aggregateLayerStatus(values: string[]) {
  const normalized = values.filter(Boolean);
  if (normalized.includes("auth_error")) return "auth_error";
  if (normalized.includes("down")) return "down";
  if (normalized.includes("warn")) return "warn";
  if (normalized.includes("ok")) return "ok";
  return "na";
}

function averageNumber(values: number[], options: { positiveOnly?: boolean } = {}) {
  const valid = values.filter((value) => Number.isFinite(value) && (!options.positiveOnly || value > 0));
  if (!valid.length) return 0;
  return Math.round(valid.reduce((sum, value) => sum + value, 0) / valid.length);
}

function uniqueText(values: Array<string | undefined>) {
  return Array.from(new Set(values.map((value) => value?.trim()).filter((value): value is string => Boolean(value))));
}

function latestProbeAt(channels: PublicChannel[]) {
  const latest = channels
    .map((channel) => new Date(channel.lastProbeAt))
    .filter((date) => !Number.isNaN(date.getTime()))
    .sort((a, b) => b.getTime() - a.getTime())[0];
  return latest?.toISOString() ?? "";
}

function stripModelSuffix(value: string) {
  const trimmed = value.trim();
  const parts = trimmed.split(/\s+[·|]\s+|\s+-\s+/);
  if (parts.length < 2) return trimmed;
  const suffix = parts[parts.length - 1]?.trim() ?? "";
  if (!looksLikeModelName(suffix)) return trimmed;
  return parts.slice(0, -1).join(" · ").trim();
}

function looksLikeModelName(value: string) {
  return /^(claude|gpt|gemini|o\d|llama|qwen|deepseek|kimi|glm|mistral|mixtral|yi|doubao|ernie|hunyuan)[\w.-]*$/i.test(value.trim());
}

function pickBrandRepresentative<T extends PublicChannel>(first: T, rest: T[]) {
  return rest.reduce((current, candidate) => {
    const currentSeverity = channelSeverity(current);
    const candidateSeverity = channelSeverity(candidate);
    if (candidateSeverity !== currentSeverity) {
      return candidateSeverity > currentSeverity ? candidate : current;
    }
    if (candidate.score !== current.score) {
      return candidate.score > current.score ? candidate : current;
    }
    const currentLatency = current.latencyP95Ms || Number.MAX_SAFE_INTEGER;
    const candidateLatency = candidate.latencyP95Ms || Number.MAX_SAFE_INTEGER;
    if (candidateLatency !== currentLatency) {
      return candidateLatency < currentLatency ? candidate : current;
    }
    return current;
  }, first);
}

function channelSeverity(channel: PublicChannel) {
  const severity: Record<string, number> = {
    connectivity_down: 5,
    functional_down: 4,
    auth_error: 4,
    degraded: 3,
    unknown: 2,
    healthy: 1
  };
  return severity[channel.status] ?? 2;
}

function statusLabelFor(status: string) {
  const labels: Record<string, string> = {
    healthy: "Healthy",
    degraded: "Degraded",
    functional_down: "Functional Down",
    connectivity_down: "Connectivity Down",
    auth_error: "Auth Error",
    unknown: "Unknown"
  };
  return labels[status] ?? status;
}

function diagnosisClass(severity?: string) {
  if (severity === "error") return "is-error";
  if (severity === "warn") return "is-warn";
  if (severity === "ok") return "is-ok";
  return "is-info";
}

function buildCategoryOptions<T extends PublicChannel>(items: T[]) {
  const typeMap = new Map<string, string>();
  for (const item of items) {
    if (!item.type) continue;
    typeMap.set(item.type, channelTypeLabel(item.type));
  }
  return Array.from(typeMap.entries())
    .map(([value, label]) => ({ value, label }))
    .sort((a, b) => a.label.localeCompare(b.label));
}

function buildChannelOptions<T extends PublicChannel>(items: T[]) {
  return [...items]
    .sort((a, b) => a.name.localeCompare(b.name))
    .map((item) => ({ value: item.id, label: item.name }));
}

function channelTypeLabel(type: string) {
  const normalized = type.trim();
  const labels: Record<string, string> = {
    anthropic: "Anthropic",
    "openai-compatible": "OpenAI 兼容",
    openai: "OpenAI",
    gemini: "Gemini"
  };
  return labels[normalized] ?? normalized;
}

function filterChannels<T extends PublicChannel>(items: T[], filter: BoardFilter) {
  const q = filter.query.trim().toLowerCase();
  return items.filter((item) => {
    if (filter.category !== "all" && item.type !== filter.category) return false;
    if (filter.provider !== "all" && item.provider !== filter.provider) return false;
    if (filter.channelID !== "all" && item.id !== filter.channelID) return false;
    if (filter.status !== "all" && item.status !== filter.status) return false;
    if (!q) return true;
    return [item.name, item.provider, item.model, item.upstreamModel, item.endpoint, item.type, item.statusLabel]
      .join(" ")
      .toLowerCase()
      .includes(q);
  });
}

function formatModelName(model: string) {
  const raw = model.trim();
  const claudeFamilyVersion = raw.match(/^claude-(sonnet|haiku|opus)-(\d+)-(\d+)$/i);
  if (claudeFamilyVersion) {
    return `Claude ${titleModelWords(claudeFamilyVersion[1])} ${claudeFamilyVersion[2]}.${claudeFamilyVersion[3]}`;
  }
  const claudeVersionFamily = raw.match(/^claude-(\d+)-(\d+)-(.+)$/i);
  if (claudeVersionFamily) {
    return `Claude ${claudeVersionFamily[1]}.${claudeVersionFamily[2]} ${titleModelWords(claudeVersionFamily[3])}`;
  }
  if (/^gpt-/i.test(raw)) {
    return raw.replace(/^gpt-/i, "GPT-").replace(/-/g, " ").replace(/^GPT (\S+)/, "GPT-$1");
  }
  if (/^gemini-/i.test(raw)) {
    return `Gemini ${titleModelWords(raw.replace(/^gemini-/i, ""))}`;
  }
  return titleModelWords(raw);
}

function titleModelWords(value: string) {
  return value.replace(/-/g, " ").replace(/\b([a-z])/gi, (match) => match.toUpperCase()).trim();
}

function modelInitial(type: string, model: string) {
  if (type === "anthropic") return "AN";
  if (type === "gemini") return "GO";
  if (type === "openai" || type === "openai-compatible") return "OA";
  return model.slice(0, 2).toUpperCase();
}

function modelBrandClass(type: string) {
  if (type === "anthropic") return "brand-ant";
  if (type === "gemini") return "brand-goo";
  return "brand-oai";
}

function modelBrandTagClass(type: string) {
  if (type === "anthropic") return "ant";
  if (type === "gemini") return "goo";
  return "oai";
}

function formatLatency(value?: number | null) {
  if (!value || value <= 0) return "—";
  return value >= 1000 ? `${(value / 1000).toFixed(2)}s` : `${value}ms`;
}

function StatusBadge({ status, label }: { status: string; label: string }) {
  return <span className={`badge ${statusClass[status] ?? "b-gray"} dot`}>{label}</span>;
}

function LayerPair({ a, b, aMs, bMs }: { a: string; b: string; aMs?: number; bMs?: number }) {
  return (
    <div className="layer-pair">
      <span><i className={`sdot ${dotClass(a)}`} />{a}{aMs ? <em>{aMs}ms</em> : null}</span>
      <span><i className={`sdot ${dotClass(String(b))}`} />{b}{bMs ? <em>{bMs}ms</em> : null}</span>
    </div>
  );
}

function Score({ value }: { value: number }) {
  return <div className="score-cell"><b>{value}</b><span><i style={{ width: `${value}%` }} /></span></div>;
}

function ScoreRing({ value }: { value: number }) {
  const safeValue = Math.max(0, Math.min(100, Math.round(value)));
  const style = {
    background: `conic-gradient(${scoreColor(safeValue)} ${safeValue * 3.6}deg, #edf0f4 0)`
  } as CSSProperties;
  return (
    <div className="model-score-ring" style={style}>
      <span>{safeValue}</span>
    </div>
  );
}

function Spark({ values }: { values: Array<number | null> }) {
  const fallback = values.find((value): value is number => typeof value === "number") ?? 50;
  const points = values.length ? values.map((value) => (typeof value === "number" ? value : fallback)) : [50, 52, 49, 56];
  const max = Math.max(...points);
  const min = Math.min(...points);
  const path = points
    .map((value, index) => {
      const x = (index / Math.max(points.length - 1, 1)) * 96;
      const y = 28 - ((value - min) / Math.max(max - min, 1)) * 24;
      return `${index === 0 ? "M" : "L"}${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(" ");
  return (
    <svg className="spark" viewBox="0 0 96 32" preserveAspectRatio="none">
      <path d={path} />
    </svg>
  );
}

function ErrorBars({ errors }: { errors: ErrorBucket[] }) {
  const items = Array.from(
    errors
      .filter((item) => item.count > 0)
      .reduce((map, item) => {
        const key = item.label || item.type;
        const existing = map.get(key);
        if (existing) {
          map.set(key, { ...existing, count: existing.count + item.count });
        } else {
          map.set(key, { ...item, label: key });
        }
        return map;
      }, new Map<string, ErrorBucket>())
      .values()
  ).sort((a, b) => b.count - a.count);

  if (!items.length) {
    return (
      <div className="errlist-empty">
        <strong>近7天暂无失败探测</strong>
        <span>当前公开平台通道没有失败错误记录</span>
      </div>
    );
  }

  const max = Math.max(...items.map((item) => item.count), 1);
  return (
    <div className="errlist">
      {items.map((item) => (
        <div className="er" key={`${item.label}-${item.type}`}>
          <span className="et">{item.label}</span>
          <span className="eb"><i style={{ width: `${(item.count / max) * 100}%` }} /></span>
          <span className="ev">{item.count}</span>
        </div>
      ))}
    </div>
  );
}

function dotClass(value: string) {
  if (value === "ok" || value.includes("%")) return "s-ok";
  if (value === "warn") return "s-warn";
  if (value === "down" || value === "auth_error") return "s-down";
  return "s-na";
}

function timeLabel(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function compact(value: number) {
  if (value >= 1000) return `${(value / 1000).toFixed(1)}K`;
  return String(value);
}

function formatSmallUSD(value: number) {
  const safe = Number.isFinite(value) ? Math.max(0, value) : 0;
  if (safe === 0) return "$0.00";
  if (safe < 0.001) return "<$0.001";
  if (safe < 1) return `$${safe.toFixed(3)}`;
  return `$${safe.toFixed(2)}`;
}

function shortFingerprint(value: string) {
  if (!value) return "";
  return value.length <= 8 ? value : value.slice(0, 6);
}
