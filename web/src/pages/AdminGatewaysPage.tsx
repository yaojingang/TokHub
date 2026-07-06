import { FormEvent, useEffect, useMemo, useState } from "react";
import { AdminShell } from "../components/AdminShell";
import { ConsoleShell } from "../components/ConsoleShell";
import {
  adminGateways,
  adminSettings,
  bulkAdminGateways,
  bulkConsoleGateways,
  consoleSettings,
  consoleGateways,
  createAdminGateway,
  createAdminGatewayKey,
  createGateway,
  createGatewayKey,
  debugGateway,
  deleteAdminGateway,
  deleteConsoleGateway,
  Gateway,
  GatewayDebugResult,
  GatewayKey,
  GatewayUpstream,
  updateConsoleSettings,
  updateAdminGateway,
  updateConsoleGateway,
  WorkspaceSettings,
  workspaceCanManage,
  workspaceCanOperate
} from "../lib/api";

const policies = [
  { id: "latency", icon: "⚡", title: "最低延迟优先", desc: "总是路由到当前响应最快的健康上游，适合对体验敏感的业务" },
  { id: "success", icon: "✓", title: "最高成功率", desc: "优先选择真实成功率最高的上游，最大化稳定性" },
  { id: "cost", icon: "$", title: "成本优先", desc: "在健康上游中选择成本最低的通道，控制开支" }
];

const defaultGatewayQPSLimit = 60;
const defaultGatewayMonthlyQuota = 100000;

type GatewayPolicy = "latency" | "success" | "cost";

type GatewayDraft = {
  name: string;
  policy: string;
  status: string;
  qpsLimit: number;
  quotaMonth: number;
  upstreamIds: string[];
};

export function AdminGatewaysPage({ scope = "console" }: { scope?: "admin" | "console" }) {
  const adminMode = scope === "admin";
  const Shell = adminMode ? AdminShell : ConsoleShell;
  const [gateways, setGateways] = useState<Gateway[]>([]);
  const [upstreams, setUpstreams] = useState<GatewayUpstream[]>([]);
  const [step, setStep] = useState(1);
  const [name, setName] = useState(adminMode ? "平台生产网关" : "我的专属中转站");
  const [qpsLimit, setQPSLimit] = useState(defaultGatewayQPSLimit);
  const [quotaMonth, setQuotaMonth] = useState(defaultGatewayMonthlyQuota);
  const [defaultPolicy, setDefaultPolicy] = useState<GatewayPolicy>("latency");
  const [policy, setPolicy] = useState<GatewayPolicy>("latency");
  const [workspaceSettings, setWorkspaceSettings] = useState<WorkspaceSettings | null>(null);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [createdGateway, setCreatedGateway] = useState<Gateway | null>(null);
  const [createdKey, setCreatedKey] = useState<GatewayKey | null>(null);
  const [editor, setEditor] = useState<Gateway | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [savingDefaultPolicy, setSavingDefaultPolicy] = useState(false);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [selectedIDs, setSelectedIDs] = useState<string[]>([]);
  const [allowPlatformUpstreams, setAllowPlatformUpstreams] = useState(adminMode);

  useEffect(() => {
    void load();
  }, [adminMode]);

  async function load() {
    setLoading(true);
    setError("");
    try {
      const [payload, configured] = await Promise.all([
        adminMode ? adminGateways() : consoleGateways(),
        adminMode
          ? adminSettings().then((settings) => ({ defaultGatewayPolicy: settings.site.defaultGatewayPolicy, workspace: null }))
          : consoleSettings().then((settings) => ({ defaultGatewayPolicy: settings.workspace.defaultGatewayPolicy, workspace: settings.workspace }))
      ]);
      const nextPolicy = normalizeGatewayPolicy(configured.defaultGatewayPolicy);
      setDefaultPolicy(nextPolicy);
      setPolicy(nextPolicy);
      setWorkspaceSettings(configured.workspace);
      setGateways(payload.items ?? []);
      setUpstreams(payload.upstreams ?? []);
      setAllowPlatformUpstreams(adminMode || Boolean(payload.allowPlatformUpstreams));
      setSelectedIDs((current) => current.filter((id) => (payload.items ?? []).some((item) => item.id === id)));
      const usableUpstreamIDs = new Set((payload.upstreams ?? []).filter(isGatewaySelectableUpstream).map((item) => item.channelId));
      setPicked((current) => {
        const retained = Array.from(current).filter((id) => usableUpstreamIDs.has(id));
        return retained.length ? new Set(retained) : new Set(Array.from(usableUpstreamIDs).slice(0, 3));
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载网关失败");
    } finally {
      setLoading(false);
    }
  }

  async function saveDefaultGatewayPolicy(nextPolicy: GatewayPolicy) {
    setPolicy(nextPolicy);
    if (adminMode) return;
    if (!workspaceCanManage(workspaceSettings?.role)) {
      setError("当前工作区角色不能修改默认路由策略。");
      return;
    }
    if (!workspaceSettings) {
      setError("工作区设置还没有加载完成，请稍后再试。");
      return;
    }
    if (defaultPolicy === nextPolicy) return;
    setSavingDefaultPolicy(true);
    setError("");
    setNotice("");
    try {
      const payload = await updateConsoleSettings({
        name: workspaceSettings.name,
        timezone: workspaceSettings.timezone,
        defaultGatewayPolicy: nextPolicy,
        defaultNotificationChannelId: workspaceSettings.defaultNotificationChannelId || ""
      });
      setWorkspaceSettings(payload.workspace);
      setDefaultPolicy(normalizeGatewayPolicy(payload.workspace.defaultGatewayPolicy));
      setPolicy(normalizeGatewayPolicy(payload.workspace.defaultGatewayPolicy));
      setNotice("默认路由策略已保存。之后新建专属中转站会默认使用这个策略。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存默认路由策略失败");
    } finally {
      setSavingDefaultPolicy(false);
    }
  }

  function togglePick(channelID: string) {
    const upstream = upstreams.find((item) => item.channelId === channelID);
    if (upstream && !isGatewaySelectableUpstream(upstream)) {
      setError(unusableUpstreamReason(upstream));
      return;
    }
    setError("");
    setPicked((current) => {
      const next = new Set(current);
      next.has(channelID) ? next.delete(channelID) : next.add(channelID);
      return next;
    });
  }

  async function nextStep() {
    setError("");
    setNotice("");
    if (!adminMode && !workspaceCanOperate(workspaceSettings?.role)) {
      setError("当前工作区角色不能创建或下发专属中转站。");
      return;
    }
    if (step === 1 && !name.trim()) {
      setError("请先填写网关名称。");
      return;
    }
    if (step === 1 && !isPositiveInteger(qpsLimit)) {
      setError("QPS 限制必须是大于 0 的整数。");
      return;
    }
    if (step === 1 && !isPositiveInteger(quotaMonth)) {
      setError("月度额度必须是大于 0 的整数。");
      return;
    }
    if (step === 2 && !selectedUsableUpstreams.length) {
      setError(adminMode
        ? "请至少选择一个健康或降级的可网关化平台通道。"
        : "请至少选择一个已通过连接测试的私有通道。若通道显示未知或鉴权异常，请先回到“我的通道”完成测试连接。");
      return;
    }
    if (step < 3) {
      setStep(step + 1);
      return;
    }
    if (step === 4) {
      setStep(1);
      setCreatedGateway(null);
      setCreatedKey(null);
      setName(adminMode ? "平台生产网关" : "我的专属中转站");
      setQPSLimit(defaultGatewayQPSLimit);
      setQuotaMonth(defaultGatewayMonthlyQuota);
      setPolicy(defaultPolicy);
      return;
    }
    if (!selectedUsableUpstreams.length) {
      setError(adminMode
        ? "至少选择一个健康或降级的可网关化平台通道。"
        : "至少选择一个已通过连接测试的私有通道。平台监控通道不会向普通用户开放。");
      return;
    }
    setSaving(true);
    try {
      const createGatewayFn = adminMode ? createAdminGateway : createGateway;
      const createKeyFn = adminMode ? createAdminGatewayKey : createGatewayKey;
      const normalizedQPSLimit = Math.trunc(qpsLimit);
      const normalizedQuotaMonth = Math.trunc(quotaMonth);
      const created = await createGatewayFn({ name: name.trim(), policy, upstreamIds: selectedUsableUpstreams.map((item) => item.channelId), qpsLimit: normalizedQPSLimit, quotaMonth: normalizedQuotaMonth });
      const key = await createKeyFn({ gatewayId: created.gateway.id, name: `${name} 默认 Key`, quotaMonth: normalizedQuotaMonth, qpsLimit: normalizedQPSLimit });
      setCreatedGateway(created.gateway);
      setCreatedKey(key.key);
      setGateways((items) => [created.gateway, ...items]);
      setStep(4);
      setNotice("网关已生成，API Key 只在本次创建结果中展示，请先复制保存。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "创建网关失败");
    } finally {
      setSaving(false);
    }
  }

  async function saveGateway(gatewayID: string, draft: GatewayDraft) {
    if (!adminMode && !workspaceCanOperate(workspaceSettings?.role)) {
      setError("当前工作区角色不能保存网关配置。");
      return;
    }
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const updateGateway = adminMode ? updateAdminGateway : updateConsoleGateway;
      const payload = await updateGateway(gatewayID, draft);
      setGateways((items) => items.map((item) => item.id === gatewayID ? payload.gateway : item));
      setEditor(null);
      setNotice("网关配置已保存，并写入审计。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存网关失败");
    } finally {
      setSaving(false);
    }
  }

  async function setGatewayStatus(gateway: Gateway, status: string) {
    if (!adminMode && !workspaceCanOperate(workspaceSettings?.role)) {
      setError("当前工作区角色不能修改网关状态。");
      return;
    }
    if (gateway.status === status) return;
    if (status !== "active" && !window.confirm(`确认将 ${gateway.name} 改为 ${statusTextForGateway(status)}？`)) return;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const updateGateway = adminMode ? updateAdminGateway : updateConsoleGateway;
      const payload = await updateGateway(gateway.id, { status });
      setGateways((items) => items.map((item) => item.id === gateway.id ? payload.gateway : item));
      setNotice("网关状态已更新。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "更新网关状态失败");
    } finally {
      setSaving(false);
    }
  }

  async function removeGateway(gateway: Gateway) {
    if (!adminMode && !workspaceCanOperate(workspaceSettings?.role)) {
      setError("当前工作区角色不能删除网关。");
      return;
    }
    if (!window.confirm(`确认删除 ${gateway.name}？该网关会停止服务，相关 active Key 会被吊销。`)) return;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const deleteGateway = adminMode ? deleteAdminGateway : deleteConsoleGateway;
      await deleteGateway(gateway.id);
      setGateways((items) => items.filter((item) => item.id !== gateway.id));
      setNotice("网关已删除，相关 Key 已吊销。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "删除网关失败");
    } finally {
      setSaving(false);
    }
  }

  function toggleGatewaySelection(gatewayID: string, checked: boolean) {
    setSelectedIDs((current) => checked ? Array.from(new Set([...current, gatewayID])) : current.filter((id) => id !== gatewayID));
  }

  function toggleVisibleGateways(checked: boolean) {
    const visibleIDs = filteredGateways.map((gateway) => gateway.id);
    setSelectedIDs((current) => {
      if (!checked) return current.filter((id) => !visibleIDs.includes(id));
      return Array.from(new Set([...current, ...visibleIDs]));
    });
  }

  async function runBulkGateways(action: "status" | "delete", status?: string) {
    if (!adminMode && !workspaceCanOperate(workspaceSettings?.role)) {
      setError("当前工作区角色不能批量操作网关。");
      return;
    }
    if (!selectedIDs.length) return;
    const validIDs = selectedIDs.filter((id) => gateways.some((gateway) => gateway.id === id));
    if (!validIDs.length) {
      setError("请先选择网关。");
      return;
    }
    const text = action === "delete" ? `删除 ${validIDs.length} 个网关` : `将 ${validIDs.length} 个网关改为 ${statusTextForGateway(status ?? "")}`;
    if (!window.confirm(`确认${text}？批量删除会吊销相关 active Key。`)) return;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const bulkGateways = adminMode ? bulkAdminGateways : bulkConsoleGateways;
      const payload = action === "delete"
        ? await bulkGateways({ action: "delete", ids: validIDs })
        : await bulkGateways({ action: "status", ids: validIDs, status });
      setGateways(payload.items ?? []);
      setUpstreams(payload.upstreams ?? upstreams);
      setSelectedIDs([]);
      setNotice(action === "delete" ? "批量删除完成，相关 active Key 已吊销。" : "批量状态更新完成。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "批量操作失败");
    } finally {
      setSaving(false);
    }
  }

  const pickedUpstreams = useMemo(() => upstreams.filter((item) => picked.has(item.channelId)), [upstreams, picked]);
  const selectedUsableUpstreams = useMemo(() => pickedUpstreams.filter(isGatewaySelectableUpstream), [pickedUpstreams]);
  const selectableUpstreamCount = useMemo(() => upstreams.filter(isGatewaySelectableUpstream).length, [upstreams]);
  const filteredGateways = useMemo(() => {
    const term = query.trim().toLowerCase();
    return gateways.filter((gateway) => {
      const statusOK = statusFilter === "all" || gateway.status === statusFilter;
      const textOK = !term || [
        gateway.name,
        gateway.slug,
        gateway.policyLabel,
        gateway.baseUrl,
        ...gateway.upstreams.flatMap((item) => [item.name, item.provider, item.model])
      ].join(" ").toLowerCase().includes(term);
      return statusOK && textOK;
    });
  }, [gateways, query, statusFilter]);
  const selectedSet = useMemo(() => new Set(selectedIDs), [selectedIDs]);
  const allVisibleSelected = filteredGateways.length > 0 && filteredGateways.every((gateway) => selectedSet.has(gateway.id));
  const canManageWorkspace = adminMode || workspaceCanManage(workspaceSettings?.role);
  const canOperateWorkspace = adminMode || workspaceCanOperate(workspaceSettings?.role);
  const title = adminMode ? "企业网关治理" : "专属中转站";
  const crumb = adminMode ? "/ 平台资源 / 网关" : "/ 工作区 / 网关";
  const consoleIntro = allowPlatformUpstreams
    ? "Super VIP 可将平台监控通道和自己添加的私有 API 组合为专属中转站；平台 Key 不会展示给用户"
    : "专属中转站只使用你自己添加的私有 API。平台公开监控通道和系统 API Key 不会开放给普通用户";
  const upstreamHint = adminMode
    ? "平台网关只显示平台可网关化通道"
    : allowPlatformUpstreams
      ? "Super VIP 可选择平台通道或本工作区私有通道"
      : "仅显示你自己添加的私有通道";

  return (
    <Shell title={title} crumb={crumb}>
      {error ? <div className="form-error">{error}</div> : null}
      {notice ? <div className="form-notice">{notice}</div> : null}

      <div className="admin-actions-line" style={{ marginBottom: 14 }}>
        <p className="page-intro">
          {adminMode ? "平台管理员可以创建、编辑、暂停和删除平台网关，并治理纳入网关的上游通道" : consoleIntro}
        </p>
        <div className="admin-actions-buttons">
          <button className="btn btn-ghost btn-sm" onClick={() => void load()} disabled={loading}>刷新</button>
        </div>
      </div>

      {!adminMode ? (
        <section className="card set-card gateway-policy-card">
          <div className="set-h">
            <span>默认路由策略</span>
            <small>用于新建专属中转站，创建时仍可单独调整</small>
          </div>
          <div className="opt-grid ops-pad gateway-policy-grid">
            {policies.map((item) => {
              const active = defaultPolicy === item.id;
              return (
                <button
                  type="button"
                  className={`opt gateway-policy-option ${active ? "sel" : ""}`}
                  key={item.id}
                  onClick={() => void saveDefaultGatewayPolicy(normalizeGatewayPolicy(item.id))}
                  disabled={savingDefaultPolicy || loading || !canManageWorkspace}
                >
                  <div className="oh"><span className="ic">{item.icon}</span>{item.title}</div>
                  <p>{item.desc}</p>
                  <span className={`policy-state ${active ? "active" : ""}`}>{active ? "当前默认" : "设为默认"}</span>
                </button>
              );
            })}
          </div>
        </section>
      ) : null}

      <div className="section-head" style={{ marginTop: 6 }}>
        <h2>{adminMode ? "创建平台企业网关" : "一键生成工作区专属中转站"} <span className="tag">向导</span></h2>
        <span className="sub">{adminMode ? "仅允许纳入平台通道，私有通道不会进入平台网关" : upstreamHint}</span>
      </div>

      <div className="card card-pad">
        <div className="steps">
          {["命名网关", "选择上游", "路由策略", "生成"].map((label, index) => {
            const n = index + 1;
            return (
              <div className={`step ${n < step ? "done" : n === step ? "active" : ""}`} key={label}>
                <span className="n">{n < step ? "✓" : n}</span>
                <span className="t">{label}</span>
                {n < 4 ? <span className="step-line" /> : null}
              </div>
            );
          })}
        </div>

        {step === 1 ? (
          <div className="wz-panel">
            <div className="field">
              <label>网关名称 <span className="hint">便于团队识别</span></label>
              <input className="input" value={name} onChange={(event) => setName(event.target.value)} placeholder={adminMode ? "例如：平台生产网关" : "例如：核心生产网关"} />
            </div>
            <div className="settings-grid-inline">
              <label className="field">
                <span>QPS 限制</span>
                <input className="input" type="number" min={1} step={1} value={qpsLimit} onChange={(event) => setQPSLimit(Number(event.target.value))} />
              </label>
              <label className="field">
                <span>月度请求额度</span>
                <input className="input" type="number" min={1} step={1} value={quotaMonth} onChange={(event) => setQuotaMonth(Number(event.target.value))} />
              </label>
            </div>
            <div className="module-sub">默认值为 {defaultGatewayQPSLimit} QPS / {formatMetricCount(defaultGatewayMonthlyQuota)} 月请求；默认 Key 会继承这里填写的限额。</div>
          </div>
        ) : null}

        {step === 2 ? (
          <div className="wz-panel">
            <div className="field">
              <label>选择纳入的上游通道 <span className="hint">{upstreamHint}</span></label>
              <div className="gateway-upstream-guide">
                {selectableUpstreamCount
                  ? `当前有 ${selectableUpstreamCount} 个通道可用于网关。只有健康或降级通道会被纳入，系统不会向普通用户展示平台 API Key。`
                  : adminMode
                    ? "当前没有健康或降级的平台通道可纳入网关，请先到平台通道页验证通道。"
                    : "当前没有已通过连接测试的私有通道。请先到“我的通道”添加 API 并点击“测试连接”。"}
              </div>
              <div className="pick-grid">
                {loading ? <div className="module-sub">正在加载可用通道…</div> : upstreams.length ? upstreams.map((upstream) => {
                  const selected = picked.has(upstream.channelId);
                  const selectable = isGatewaySelectableUpstream(upstream);
                  return (
                    <button type="button" className={`pick ${selected ? "sel" : ""} ${selectable ? "" : "disabled"}`} key={upstream.channelId} onClick={() => togglePick(upstream.channelId)} disabled={!selectable} title={selectable ? "可纳入网关" : unusableUpstreamReason(upstream)}>
                      <span className="mk" style={{ background: upstream.mark }}>{upstream.provider[0]}</span>
                      <span className="info">
                        <b>{upstream.name}</b>
                        <small>{upstream.model} · {upstream.latencyP95Ms}ms · {upstream.successRate.toFixed(1)}% · {upstream.ownerType === "platform" ? "平台通道" : "私有通道"} <span className={`badge ${statusClass(upstream.status)}`}>{statusText(upstream.status)}</span></small>
                        {!selectable ? <small className="pick-reason">{unusableUpstreamReason(upstream)}</small> : null}
                      </span>
                      <span className="ck">{selected ? "✓" : ""}</span>
                    </button>
                  );
                }) : <div className="empty-state"><h4>没有可用上游</h4><p>{adminMode ? "请先在平台通道页启用可网关化通道。" : "先到“我的通道”添加自己的 API；如需一键使用平台通道，请联系管理员开通 Super VIP。"}</p></div>}
              </div>
            </div>
          </div>
        ) : null}

        {step === 3 ? (
          <div className="wz-panel">
            <div className="field">
              <label>选择路由策略 <span className="hint">所有策略均自动开启健康检查与故障转移</span></label>
              <div className="opt-grid">
                {policies.map((item) => (
                  <button type="button" className={`opt ${policy === item.id ? "sel" : ""}`} key={item.id} onClick={() => setPolicy(normalizeGatewayPolicy(item.id))}>
                    <div className="oh"><span className="ic">{item.icon}</span>{item.title}</div>
                    <p>{item.desc}</p>
                  </button>
                ))}
              </div>
            </div>
            <div className="module-sub">已选择 {selectedUsableUpstreams.length} 个可用上游：{selectedUsableUpstreams.map((item) => item.name).join(" / ") || "无"}</div>
          </div>
        ) : null}

        {step === 4 ? (
          <div className="wz-panel">
            <div style={{ display: "flex", alignItems: "center", gap: 9, marginBottom: 14 }}>
              <span className="badge b-green dot">已生成</span>
              <span style={{ fontSize: 14, fontWeight: 650 }}>网关已就绪，API Key 已生成</span>
            </div>
            <div className="endpoint">
              <span className="k">BASE_URL</span>
              <span className="v">{createdGateway?.baseUrl}</span>
              <CopyButton text={createdGateway?.baseUrl ?? ""} />
            </div>
            <div className="endpoint" style={{ marginTop: 8 }}>
              <span className="k">API_KEY</span>
              <span className="v">{createdKey?.plainKey ?? createdKey?.keyMask}</span>
              <CopyButton text={createdKey?.plainKey ?? ""} />
            </div>
            <div className="module-sub" style={{ marginTop: 14 }}>
              已纳入 <b>{createdGateway?.upstreams.length ?? 0}</b> 个上游通道 · 路由策略 <b>{createdGateway?.policyLabel}</b> · 列表只显示 mask，完整 Key 关闭后只能轮换或重新签发。
            </div>
          </div>
        ) : null}

        <div style={{ display: "flex", alignItems: "center", gap: 12, marginTop: 24, paddingTop: 18, borderTop: "1px solid var(--border)" }}>
          <button className="btn btn-ghost" style={{ visibility: step === 1 ? "hidden" : "visible" }} onClick={() => setStep(Math.max(1, step - 1))}>← 上一步</button>
          <span style={{ flex: 1 }} />
          <button className="btn btn-primary" onClick={() => void nextStep()} disabled={saving || !canOperateWorkspace}>{saving ? "生成中..." : step === 4 ? "完成并下发 ✓" : step === 3 ? "生成网关 ✦" : "下一步 →"}</button>
        </div>
      </div>

      <div className="section-head">
        <h2>{adminMode ? "平台企业网关" : "已有专属中转站"}</h2>
        <span className="sub">{adminMode ? "可编辑、暂停和软删除" : "可编辑、暂停、删除和调试"}</span>
      </div>
      <div className="toolbar card gateway-filter-toolbar">
        <select className="input" aria-label="网关状态筛选" value={statusFilter} onChange={(event) => setStatusFilter(event.target.value)}>
          <option value="all">所有状态</option>
          <option value="active">运行中</option>
          <option value="paused">已暂停</option>
        </select>
        <input className="input" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索网关 / 上游 / Base URL..." />
        <button className="btn btn-ghost btn-sm" onClick={() => { setQuery(""); setStatusFilter("all"); }}>清空筛选</button>
      </div>
      <div className="toolbar bulk-toolbar">
        <label className="bulk-select">
          <input type="checkbox" checked={allVisibleSelected} disabled={!canOperateWorkspace} onChange={(event) => toggleVisibleGateways(event.target.checked)} />
          <span>已选 {selectedIDs.length} 个网关</span>
        </label>
        <button className="btn btn-ghost btn-sm" disabled={!selectedIDs.length || saving || !canOperateWorkspace} onClick={() => void runBulkGateways("status", "paused")}>批量暂停</button>
        <button className="btn btn-ghost btn-sm" disabled={!selectedIDs.length || saving || !canOperateWorkspace} onClick={() => void runBulkGateways("status", "active")}>批量启用</button>
        <button className="btn btn-ghost btn-sm" disabled={!selectedIDs.length || saving || !canOperateWorkspace} onClick={() => void runBulkGateways("delete")}>批量删除</button>
      </div>
      <div>
        {filteredGateways.length ? filteredGateways.map((gateway) => (
          <GatewayCard
            adminMode={adminMode}
            canOperate={canOperateWorkspace}
            gateway={gateway}
            key={gateway.id}
            selected={selectedSet.has(gateway.id)}
            onSelect={(checked) => toggleGatewaySelection(gateway.id, checked)}
            onEdit={() => setEditor(gateway)}
            onDelete={() => void removeGateway(gateway)}
            onStatus={(status) => void setGatewayStatus(gateway, status)}
          />
        )) : (
          <div className="card card-pad empty-state">
            <div className="ico">✦</div>
            <h4>{gateways.length ? "没有匹配的网关" : "还没有网关"}</h4>
            <p>{gateways.length ? "调整筛选条件后再查看。" : "完成上方向导后，会在这里展示 Base URL、上游通道、策略和今日用量。"}</p>
          </div>
        )}
      </div>

      <GatewayEditor gateway={editor} upstreams={upstreams} saving={saving} onClose={() => setEditor(null)} onSubmit={saveGateway} />
    </Shell>
  );
}

function GatewayEditor({ gateway, upstreams, saving, onClose, onSubmit }: { gateway: Gateway | null; upstreams: GatewayUpstream[]; saving: boolean; onClose: () => void; onSubmit: (gatewayID: string, draft: GatewayDraft) => Promise<void> }) {
  const [draft, setDraft] = useState<GatewayDraft>({ name: "", policy: "latency", status: "active", qpsLimit: defaultGatewayQPSLimit, quotaMonth: defaultGatewayMonthlyQuota, upstreamIds: [] });
  const open = Boolean(gateway);
  const hasInvalidUpstream = draft.upstreamIds.some((id) => {
    const upstream = upstreams.find((item) => item.channelId === id);
    return !upstream || !isGatewaySelectableUpstream(upstream);
  });

  useEffect(() => {
    if (!gateway) return;
    setDraft({
      name: gateway.name,
      policy: gateway.policy,
      status: gateway.status,
      qpsLimit: gateway.qpsLimit,
      quotaMonth: gateway.quotaMonth,
      upstreamIds: gateway.upstreams.map((item) => item.channelId)
    });
  }, [gateway]);

  function patch<K extends keyof GatewayDraft>(key: K, value: GatewayDraft[K]) {
    setDraft((current) => ({ ...current, [key]: value }));
  }

  function toggle(channelID: string) {
    setDraft((current) => {
      const upstream = upstreams.find((item) => item.channelId === channelID);
      const selected = new Set(current.upstreamIds);
      if (selected.has(channelID)) {
        selected.delete(channelID);
      } else if (upstream && isGatewaySelectableUpstream(upstream)) {
        selected.add(channelID);
      }
      return { ...current, upstreamIds: Array.from(selected) };
    });
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!gateway) return;
    await onSubmit(gateway.id, draft);
  }

  return (
    <div className={`drawer-mask ${open ? "open" : ""}`} onClick={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <form className={`drawer ${open ? "open" : ""} private-editor`} onSubmit={(event) => void submit(event)}>
        <div className="dh">
          <div><h3>编辑网关</h3><p>修改网关策略、限额、状态和上游集合。</p></div>
          <button type="button" className="icon-btn" onClick={onClose}>×</button>
        </div>
        <div className="db form-grid">
          <label><span>名称</span><input className="input" value={draft.name} onChange={(event) => patch("name", event.target.value)} required /></label>
          <label><span>状态</span><select className="input" value={draft.status === "deleted" ? "paused" : draft.status} onChange={(event) => patch("status", event.target.value)}><option value="active">Active</option><option value="paused">Paused</option></select></label>
          <label><span>策略</span><select className="input" value={draft.policy} onChange={(event) => patch("policy", event.target.value)}><option value="latency">最低延迟</option><option value="success">最高成功率</option><option value="cost">成本优先</option></select></label>
          <label><span>QPS</span><input className="input" type="number" min={1} value={draft.qpsLimit} onChange={(event) => patch("qpsLimit", Number(event.target.value))} /></label>
          <label><span>月度额度</span><input className="input" type="number" min={1} value={draft.quotaMonth} onChange={(event) => patch("quotaMonth", Number(event.target.value))} /></label>
          <div className="field" style={{ gridColumn: "1 / -1" }}>
            <label>上游通道 <span className="hint">至少保留一个</span></label>
            <div className="pick-grid">
              {upstreams.map((upstream) => {
                const selected = draft.upstreamIds.includes(upstream.channelId);
                const selectable = isGatewaySelectableUpstream(upstream);
                return (
                  <button type="button" className={`pick ${selected ? "sel" : ""} ${selectable ? "" : "disabled"}`} key={upstream.channelId} onClick={() => toggle(upstream.channelId)} disabled={!selected && !selectable} title={selectable ? "可纳入网关" : unusableUpstreamReason(upstream)}>
                    <span className="mk" style={{ background: upstream.mark }}>{upstream.provider[0]}</span>
                    <span className="info"><b>{upstream.name}</b><small>{upstream.model} · {statusText(upstream.status)}</small>{!selectable ? <small className="pick-reason">{unusableUpstreamReason(upstream)}</small> : null}</span>
                    <span className="ck">{selected ? "✓" : ""}</span>
                  </button>
                );
              })}
            </div>
          </div>
        </div>
        <div className="df">
          <button type="button" className="btn btn-ghost btn-sm" onClick={onClose}>取消</button>
          <button type="submit" className="btn btn-primary btn-sm" disabled={saving || !draft.upstreamIds.length || hasInvalidUpstream}>{saving ? "保存中..." : "保存网关"}</button>
        </div>
      </form>
    </div>
  );
}

function GatewayCard({ gateway, adminMode, canOperate, selected = false, onSelect, onEdit, onDelete, onStatus }: { gateway: Gateway; adminMode: boolean; canOperate: boolean; selected?: boolean; onSelect?: (checked: boolean) => void; onEdit: () => void; onDelete: () => void; onStatus: (status: string) => void }) {
  const [prompt, setPrompt] = useState("Reply exactly: OK");
  const [model, setModel] = useState(gateway.upstreams[0]?.model ?? "");
  const [debugging, setDebugging] = useState(false);
  const [result, setResult] = useState<GatewayDebugResult | null>(null);
  const [error, setError] = useState("");

  async function runDebug() {
    if (!canOperate) {
      setError("当前工作区角色不能发起网关调试调用。");
      return;
    }
    setDebugging(true);
    setError("");
    setResult(null);
    try {
      const payload = await debugGateway(gateway.id, { model, prompt, kind: "chat" });
      setResult(payload.result);
    } catch (err) {
      setError(err instanceof Error ? err.message : "调试调用失败");
    } finally {
      setDebugging(false);
    }
  }

  return (
    <div className="card card-pad gateway-card">
      <div className="gateway-card-head">
        <input type="checkbox" aria-label={`选择 ${gateway.name}`} checked={selected} disabled={!canOperate} onChange={(event) => onSelect?.(event.target.checked)} />
        <div className="gw-name" style={{ fontSize: 16 }}><span className={`badge ${gateway.status === "active" ? "b-green" : gateway.status === "paused" ? "b-amber" : "b-gray"} dot`}>{statusTextForGateway(gateway.status)}</span>{gateway.name}</div>
        <span className={`switch gateway-status-switch ${gateway.status === "active" ? "on" : ""}`} />
        <button className="btn btn-ghost btn-sm" disabled={!canOperate} onClick={() => onStatus(gateway.status === "active" ? "paused" : "active")}>{gateway.status === "active" ? "暂停" : "启用"}</button>
        <button className="btn btn-ghost btn-sm" disabled={!canOperate} onClick={onEdit}>编辑</button>
        <button className="btn btn-ghost btn-sm" disabled={!canOperate} onClick={onDelete}>删除</button>
        {!adminMode && canOperate ? <a className="btn btn-ghost btn-sm" href="/console/keys">签发 Key</a> : null}
      </div>
      <div className="endpoint">
        <span className="k">BASE_URL</span>
        <span className="v">{gateway.baseUrl}</span>
        <CopyButton text={gateway.baseUrl} />
      </div>
      <div style={{ display: "flex", flexWrap: "wrap", gap: 8, margin: "13px 0" }}>
        {gateway.upstreams.map((upstream) => (
          <span className="chip" key={upstream.channelId}><span className="d" style={{ background: upstream.mark }} />{upstream.name}<span className="lat">{upstream.latencyP95Ms}ms</span></span>
        ))}
      </div>
      <div style={{ display: "flex", gap: 26, flexWrap: "wrap", paddingTop: 13, borderTop: "1px solid var(--border)" }}>
        <Metric label="今日请求" value={formatMetricCount(gateway.stats.requestsToday)} />
        <Metric label="Token" value={`${gateway.stats.tokensToday}`} />
        <Metric label="成功率" value={`${gateway.stats.successRate}%`} />
        <Metric label="P95" value={`${gateway.stats.p95LatencyMs || 0}ms`} />
        <Metric label="策略" value={gateway.policyLabel} />
        <Metric label="有效 Key" value={`${gateway.stats.keysActive}`} />
      </div>
      {!adminMode ? (
        <div className="gateway-debug">
          <div className="gd-head">
            <b>真实调试调用</b>
            <span>从当前工作区网关路由中选择健康上游，执行最小 chat 请求并写入用量事件。</span>
          </div>
          <div className="settings-grid-inline">
            <label className="field">
              <span>模型</span>
              <input className="input" value={model} onChange={(event) => setModel(event.target.value)} placeholder={gateway.upstreams[0]?.model ?? "gpt-4o-mini"} />
            </label>
            <label className="field gd-prompt">
              <span>Prompt</span>
              <input className="input" value={prompt} onChange={(event) => setPrompt(event.target.value)} />
            </label>
            <button className="btn btn-primary" disabled={debugging || !gateway.upstreams.length || !canOperate} onClick={() => void runDebug()}>{debugging ? "调用中..." : "测试调用"}</button>
          </div>
          {error ? <div className="form-error">{error}</div> : null}
          {result ? (
            <div className={`debug-result ${result.ok ? "ok" : "bad"}`}>
              <span className={`badge ${result.ok ? "b-green" : "b-red"} dot`}>{result.ok ? "成功" : "失败"}</span>
              <span>上游：{result.upstream || "未命中"}</span>
              <span>状态码：{result.statusCode}</span>
              <span>延迟：{result.latencyMs}ms</span>
              <span>Token：{result.tokens}</span>
              {result.errorType ? <span>错误：{result.errorType}</span> : null}
              {result.preview ? <code>{result.preview}</code> : null}
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return <div><div style={{ fontSize: 11, color: "var(--text-3)" }}>{label}</div><div className="num" style={{ fontSize: 15, fontWeight: 700, marginTop: 2 }}>{value}</div></div>;
}

function formatMetricCount(value: number) {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`;
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`;
  return `${value}`;
}

function isPositiveInteger(value: number) {
  return Number.isFinite(value) && Number.isInteger(value) && value > 0;
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button className="copy" onClick={() => {
      if (text) void navigator.clipboard?.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 1200);
    }}>
      {copied ? "已复制 ✓" : "复制"}
    </button>
  );
}

function statusClass(status: string) {
  if (status === "healthy") return "b-green";
  if (status === "degraded") return "b-amber";
  if (status === "connectivity_down" || status === "auth_error" || status === "functional_down") return "b-red";
  return "b-gray";
}

function statusText(status: string) {
  if (status === "healthy") return "健康";
  if (status === "degraded") return "降级";
  if (status === "auth_error") return "认证异常";
  if (status === "connectivity_down") return "连通异常";
  if (status === "functional_down") return "功能故障";
  return "未知";
}

function isGatewaySelectableUpstream(upstream: GatewayUpstream) {
  return upstream.enabled && (upstream.status === "healthy" || upstream.status === "degraded");
}

function unusableUpstreamReason(upstream: GatewayUpstream) {
  if (!upstream.enabled) return "这个通道已禁用，不能纳入网关。";
  if (upstream.status === "auth_error") return "这个通道鉴权失败，请先检查 API Key。";
  if (upstream.status === "connectivity_down") return "这个通道连接失败，请先检查 Base URL 或网络。";
  if (upstream.status === "functional_down") return "这个通道模型不可用，请先完成验证。";
  if (upstream.status === "unknown") return "这个通道尚未完成连接测试，请先在“我的通道”测试连接。";
  return "这个通道当前不可用于网关。";
}

function statusTextForGateway(status: string) {
  if (status === "active") return "运行中";
  if (status === "paused") return "已暂停";
  if (status === "deleted") return "已删除";
  return status;
}

function normalizeGatewayPolicy(policy: string): GatewayPolicy {
  if (policy === "success" || policy === "cost") return policy;
  return "latency";
}
