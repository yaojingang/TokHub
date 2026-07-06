import { FormEvent, useEffect, useMemo, useState } from "react";
import { AdminShell } from "../components/AdminShell";
import { ConsoleShell } from "../components/ConsoleShell";
import {
  AlertCenter,
  AlertDelivery,
  AlertRule,
  alerts,
  adminChannels,
  bulkAlertRules,
  bulkIncidents,
  bulkNotificationChannels,
  createAlertRule,
  createIncident,
  createNotificationChannel,
  consoleSettings,
  deleteAlertRule,
  deleteIncident,
  deleteNotificationChannel,
  evaluateAlerts,
  IncidentItem,
  incidents,
  NotificationChannel,
  privateChannels,
  reopenIncident,
  resolveIncident,
  testNotificationChannel,
  updateAlertRule,
  updateIncident,
  updateNotificationChannel,
  workspaceCanOperate
} from "../lib/api";
import { BulkActionBar, Button, DataTable, DataTableColumn, FilterBar, Pagination, SelectField, StatGrid, StatusBadge } from "../ui";

type Scope = "admin" | "console";
type AlertSection = "rules" | "channels" | "history" | "incidents";

const emptyRule = {
  name: "",
  kind: "l3_consecutive_failures" as AlertRule["kind"],
  severity: "warning" as AlertRule["severity"],
  threshold: 2,
  windowMinutes: 60,
  dedupeMinutes: 30,
  enabled: true
};

const emptyChannel = {
  name: "",
  type: "email" as NotificationChannel["type"],
  target: "",
  enabled: true
};

const emptyIncident = {
  channelId: "",
  status: "manual",
  title: "",
  message: ""
};

type IncidentChannelOption = {
  id: string;
  label: string;
  meta: string;
};

const INCIDENT_PAGE_SIZE = 10;

export function AlertsPage({ scope = "admin" }: { scope?: Scope }) {
  const [center, setCenter] = useState<AlertCenter | null>(null);
  const [incidentItems, setIncidentItems] = useState<IncidentItem[]>([]);
  const [incidentChannelOptions, setIncidentChannelOptions] = useState<IncidentChannelOption[]>([]);
  const [workspaceRole, setWorkspaceRole] = useState("");
  const [ruleForm, setRuleForm] = useState(emptyRule);
  const [channelForm, setChannelForm] = useState(emptyChannel);
  const [incidentForm, setIncidentForm] = useState(emptyIncident);
  const [editingRuleID, setEditingRuleID] = useState("");
  const [editingChannelID, setEditingChannelID] = useState("");
  const [editingChannelMask, setEditingChannelMask] = useState("");
  const [editingIncidentID, setEditingIncidentID] = useState("");
  const [loading, setLoading] = useState(true);
  const [incidentLoading, setIncidentLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [ruleQuery, setRuleQuery] = useState("");
  const [ruleStatus, setRuleStatus] = useState<"all" | "enabled" | "disabled">("all");
  const [channelQuery, setChannelQuery] = useState("");
  const [channelStatus, setChannelStatus] = useState<"all" | "enabled" | "disabled">("all");
  const [incidentQuery, setIncidentQuery] = useState("");
  const [incidentStatus, setIncidentStatus] = useState("all");
  const [incidentPage, setIncidentPage] = useState(1);
  const [incidentSelectionMode, setIncidentSelectionMode] = useState<"browse" | "bulk">("browse");
  const [activeSection, setActiveSection] = useState<AlertSection>("rules");
  const [selectedRuleIDs, setSelectedRuleIDs] = useState<string[]>([]);
  const [selectedChannelIDs, setSelectedChannelIDs] = useState<string[]>([]);
  const [selectedIncidentIDs, setSelectedIncidentIDs] = useState<string[]>([]);

  useEffect(() => {
    void load();
    setSelectedRuleIDs([]);
    setSelectedChannelIDs([]);
    setSelectedIncidentIDs([]);
    setIncidentForm(emptyIncident);
    setEditingIncidentID("");
    setActiveSection("rules");
  }, [scope]);

  useEffect(() => {
    const ruleIDs = new Set((center?.rules ?? []).map((rule) => rule.id));
    const channelIDs = new Set((center?.channels ?? []).map((channel) => channel.id));
    const incidentIDs = new Set(incidentItems.map((incident) => incident.id));
    setSelectedRuleIDs((ids) => ids.filter((id) => ruleIDs.has(id)));
    setSelectedChannelIDs((ids) => ids.filter((id) => channelIDs.has(id)));
    setSelectedIncidentIDs((ids) => ids.filter((id) => incidentIDs.has(id)));
    setEditingIncidentID((id) => id && !incidentIDs.has(id) ? "" : id);
  }, [center, incidentItems]);

  async function load() {
    setLoading(true);
    setError("");
    try {
      const [nextCenter, incidentResult, settingsPayload] = await Promise.all([
        alerts(scope),
        incidents(scope, { status: incidentStatus, q: incidentQuery, limit: 100 }),
        scope === "console" ? consoleSettings() : Promise.resolve(null)
      ]);
      setCenter(nextCenter);
      setIncidentItems(incidentResult.items);
      setWorkspaceRole(settingsPayload?.workspace.role ?? (scope === "admin" ? "admin" : ""));
      try {
        setIncidentChannelOptions(await loadIncidentChannelOptions(scope));
      } catch {
        setIncidentChannelOptions([]);
      }
      setIncidentPage(1);
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载告警中心失败");
    } finally {
      setLoading(false);
    }
  }

  async function loadIncidentList(nextStatus = incidentStatus, nextQuery = incidentQuery) {
    setIncidentLoading(true);
    setError("");
    try {
      const result = await incidents(scope, { status: nextStatus, q: nextQuery, limit: 100 });
      setIncidentItems(result.items);
      setIncidentPage(1);
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载 Incident 失败");
    } finally {
      setIncidentLoading(false);
    }
  }

  function resetIncidentFilters() {
    setIncidentQuery("");
    setIncidentStatus("all");
    setSelectedIncidentIDs([]);
    setIncidentPage(1);
    void loadIncidentList("all", "");
  }

  async function saveRule(event: FormEvent) {
    event.preventDefault();
    if (!canOperateWorkspace) {
      setError("当前工作区角色不能保存告警规则。");
      return;
    }
    setSaving(true);
    setError("");
    setNotice("");
    try {
      if (editingRuleID) {
        await updateAlertRule(scope, editingRuleID, ruleForm);
      } else {
        await createAlertRule(scope, ruleForm);
      }
      setRuleForm(emptyRule);
      setEditingRuleID("");
      setNotice(editingRuleID ? "告警规则已更新。" : "告警规则已创建。");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存告警规则失败");
    } finally {
      setSaving(false);
    }
  }

  function editRule(rule: AlertRule) {
    if (!canOperateWorkspace) {
      setError("当前工作区角色不能编辑告警规则。");
      return;
    }
    setEditingRuleID(rule.id);
    setRuleForm({
      name: rule.name,
      kind: rule.kind,
      severity: rule.severity,
      threshold: rule.threshold,
      windowMinutes: rule.windowMinutes,
      dedupeMinutes: rule.dedupeMinutes,
      enabled: rule.enabled
    });
    setNotice("");
    setError("");
  }

  function cancelRuleEdit() {
    setEditingRuleID("");
    setRuleForm(emptyRule);
  }

  async function toggleRule(rule: AlertRule) {
    if (!canOperateWorkspace) {
      setError("当前工作区角色不能切换告警规则。");
      return;
    }
    setSaving(true);
    setError("");
    try {
      await updateAlertRule(scope, rule.id, {
        name: rule.name,
        kind: rule.kind,
        severity: rule.severity,
        threshold: rule.threshold,
        windowMinutes: rule.windowMinutes,
        dedupeMinutes: rule.dedupeMinutes,
        enabled: !rule.enabled
      });
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "更新告警规则失败");
    } finally {
      setSaving(false);
    }
  }

  async function removeRule(rule: AlertRule) {
    if (!canOperateWorkspace) {
      setError("当前工作区角色不能删除告警规则。");
      return;
    }
    if (!window.confirm(`确认删除告警规则「${rule.name}」？历史发送记录会保留。`)) return;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      await deleteAlertRule(scope, rule.id);
      if (editingRuleID === rule.id) cancelRuleEdit();
      setNotice("告警规则已删除。");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "删除告警规则失败");
    } finally {
      setSaving(false);
    }
  }

  function toggleRuleSelection(ruleID: string) {
    setSelectedRuleIDs((ids) => ids.includes(ruleID) ? ids.filter((id) => id !== ruleID) : [...ids, ruleID]);
  }

  function setAllRuleSelection(checked: boolean) {
    setSelectedRuleIDs(checked ? filteredRules.map((rule) => rule.id) : []);
  }

  async function runRuleBulk(action: "enable" | "disable" | "delete") {
    if (!canOperateWorkspace) {
      setError("当前工作区角色不能批量治理告警规则。");
      return;
    }
    if (!selectedRuleIDs.length) return;
    if (action === "delete" && !window.confirm(`确认删除选中的 ${selectedRuleIDs.length} 条告警规则？历史发送记录会保留。`)) return;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      await bulkAlertRules(scope, action, selectedRuleIDs);
      setSelectedRuleIDs([]);
      setNotice(action === "enable" ? "选中告警规则已启用。" : action === "disable" ? "选中告警规则已停用。" : "选中告警规则已删除。");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "批量处理告警规则失败");
    } finally {
      setSaving(false);
    }
  }

  async function saveChannel(event: FormEvent) {
    event.preventDefault();
    if (!canOperateWorkspace) {
      setError("当前工作区角色不能保存通知渠道。");
      return;
    }
    setSaving(true);
    setError("");
    setNotice("");
    try {
      if (editingChannelID) {
        await updateNotificationChannel(scope, editingChannelID, channelForm);
      } else {
        await createNotificationChannel(scope, channelForm);
      }
      setChannelForm(emptyChannel);
      setEditingChannelID("");
      setNotice(editingChannelID ? "通知渠道已更新。" : "通知渠道已创建。");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存通知渠道失败");
    } finally {
      setSaving(false);
    }
  }

  function editChannel(channel: NotificationChannel) {
    if (!canOperateWorkspace) {
      setError("当前工作区角色不能编辑通知渠道。");
      return;
    }
    setEditingChannelID(channel.id);
    setEditingChannelMask(targetLabel(channel));
    setChannelForm({
      name: channel.name,
      type: channel.type,
      target: "",
      enabled: channel.enabled
    });
    setNotice("");
    setError("");
  }

  function cancelChannelEdit() {
    setEditingChannelID("");
    setEditingChannelMask("");
    setChannelForm(emptyChannel);
  }

  async function toggleChannel(channel: NotificationChannel) {
    if (!canOperateWorkspace) {
      setError("当前工作区角色不能切换通知渠道。");
      return;
    }
    setSaving(true);
    setError("");
    setNotice("");
    try {
      await updateNotificationChannel(scope, channel.id, { ...channel, enabled: !channel.enabled });
      setNotice(channel.enabled ? "通知渠道已停用。" : "通知渠道已启用。");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "更新通知渠道失败");
    } finally {
      setSaving(false);
    }
  }

  async function removeChannel(channel: NotificationChannel) {
    if (!canOperateWorkspace) {
      setError("当前工作区角色不能删除通知渠道。");
      return;
    }
    if (!window.confirm(`确认删除通知渠道「${channel.name}」？历史发送记录会保留。`)) return;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      await deleteNotificationChannel(scope, channel.id);
      if (editingChannelID === channel.id) cancelChannelEdit();
      setNotice("通知渠道已删除。");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "删除通知渠道失败");
    } finally {
      setSaving(false);
    }
  }

  function toggleChannelSelection(channelID: string) {
    setSelectedChannelIDs((ids) => ids.includes(channelID) ? ids.filter((id) => id !== channelID) : [...ids, channelID]);
  }

  function setAllChannelSelection(checked: boolean) {
    setSelectedChannelIDs(checked ? filteredChannels.map((channel) => channel.id) : []);
  }

  async function runChannelBulk(action: "enable" | "disable" | "delete") {
    if (!canOperateWorkspace) {
      setError("当前工作区角色不能批量治理通知渠道。");
      return;
    }
    if (!selectedChannelIDs.length) return;
    if (action === "delete" && !window.confirm(`确认删除选中的 ${selectedChannelIDs.length} 个通知渠道？历史发送记录会保留。`)) return;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      await bulkNotificationChannels(scope, action, selectedChannelIDs);
      setSelectedChannelIDs([]);
      setNotice(action === "enable" ? "选中通知渠道已启用。" : action === "disable" ? "选中通知渠道已停用。" : "选中通知渠道已删除。");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "批量处理通知渠道失败");
    } finally {
      setSaving(false);
    }
  }

  async function runEvaluate() {
    if (!canOperateWorkspace) {
      setError("当前工作区角色不能立即评估告警规则。");
      return;
    }
    if (!window.confirm("确认立即评估当前告警规则？可能会产生真实发送记录，并触发已配置的通知渠道。")) return;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const result = await evaluateAlerts(scope);
      setNotice(result.deliveries.length ? `已产生 ${result.deliveries.length} 条告警事件。` : "已完成评估，当前没有新增事件。");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "执行告警评估失败");
    } finally {
      setSaving(false);
    }
  }

  async function testChannel(channelID: string) {
    if (!canOperateWorkspace) {
      setError("当前工作区角色不能测试通知渠道。");
      return;
    }
    if (!window.confirm("确认向该通知渠道发送测试消息？这会写入告警发送记录。")) return;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      await testNotificationChannel(scope, channelID);
      setNotice("测试通知已写入发送记录。");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "测试发送失败");
    } finally {
      setSaving(false);
    }
  }

  async function saveIncident(event: FormEvent) {
    event.preventDefault();
    if (!canOperateWorkspace) {
      setError("当前工作区角色不能保存 Incident。");
      return;
    }
    setSaving(true);
    setError("");
    setNotice("");
    try {
      if (editingIncidentID) {
        await updateIncident(scope, editingIncidentID, {
          status: incidentForm.status,
          title: incidentForm.title,
          message: incidentForm.message || "后台编辑 Incident"
        });
        setNotice("Incident 已更新。");
      } else {
        await createIncident(scope, incidentForm);
        setNotice("Incident 已登记。");
      }
      setIncidentForm(emptyIncident);
      setEditingIncidentID("");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存 Incident 失败");
    } finally {
      setSaving(false);
    }
  }

  function editIncident(incident: IncidentItem) {
    if (!canOperateWorkspace) {
      setError("当前工作区角色不能编辑 Incident。");
      return;
    }
    setEditingIncidentID(incident.id);
    setIncidentForm({
      channelId: incident.channelId,
      status: incident.status,
      title: incident.title,
      message: incident.events?.[0]?.message ?? ""
    });
    setError("");
    setNotice("");
  }

  function cancelIncidentEdit() {
    setEditingIncidentID("");
    setIncidentForm(emptyIncident);
    setError("");
    setNotice("");
  }

  async function closeIncident(incident: IncidentItem) {
    if (!canOperateWorkspace) {
      setError("当前工作区角色不能关闭 Incident。");
      return;
    }
    setSaving(true);
    setError("");
    setNotice("");
    try {
      await resolveIncident(scope, incident.id, "后台手动关闭");
      setNotice("Incident 已关闭。");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "关闭 Incident 失败");
    } finally {
      setSaving(false);
    }
  }

  async function openIncident(incident: IncidentItem) {
    if (!canOperateWorkspace) {
      setError("当前工作区角色不能重开 Incident。");
      return;
    }
    setSaving(true);
    setError("");
    setNotice("");
    try {
      await reopenIncident(scope, incident.id, "后台手动重开");
      setNotice("Incident 已重开。");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "重开 Incident 失败");
    } finally {
      setSaving(false);
    }
  }

  async function removeIncident(incident: IncidentItem) {
    if (!canOperateWorkspace) {
      setError("当前工作区角色不能删除 Incident。");
      return;
    }
    if (!window.confirm(`确认删除 Incident「${incident.title}」？时间线会保留在数据库审计中，但列表不再显示。`)) return;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      await deleteIncident(scope, incident.id, "后台手动删除");
      setNotice("Incident 已删除。");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "删除 Incident 失败");
    } finally {
      setSaving(false);
    }
  }

  function toggleIncidentSelection(incidentID: string) {
    setSelectedIncidentIDs((ids) => ids.includes(incidentID) ? ids.filter((id) => id !== incidentID) : [...ids, incidentID]);
  }

  function setAllIncidentSelection(checked: boolean) {
    const totalPages = Math.max(1, Math.ceil(incidentItems.length / INCIDENT_PAGE_SIZE));
    const currentPage = Math.min(incidentPage, totalPages);
    const pageStart = (currentPage - 1) * INCIDENT_PAGE_SIZE;
    const pageIDs = incidentItems.slice(pageStart, pageStart + INCIDENT_PAGE_SIZE).map((incident) => incident.id);
    setSelectedIncidentIDs((ids) => checked ? Array.from(new Set([...ids, ...pageIDs])) : ids.filter((id) => !pageIDs.includes(id)));
  }

  async function runIncidentBulk(action: "resolve" | "reopen" | "delete") {
    if (!canOperateWorkspace) {
      setError("当前工作区角色不能批量治理 Incident。");
      return;
    }
    if (!selectedIncidentIDs.length) return;
    const label = action === "resolve" ? "关闭" : action === "reopen" ? "重开" : "删除";
    if (!window.confirm(`确认批量${label} ${selectedIncidentIDs.length} 个 Incident？操作会写入事件时间线和审计。`)) return;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const result = await bulkIncidents(scope, action, selectedIncidentIDs, `后台批量${label} Incident`);
      setIncidentItems(result.items);
      setSelectedIncidentIDs([]);
      setNotice(`Incident 已批量${label}。`);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Incident 批量操作失败");
    } finally {
      setSaving(false);
    }
  }

  const stats = useMemo(() => {
    const summary = center?.summary;
    return [
      { label: "启用规则", value: String(summary?.enabledRules ?? 0), hint: "active rules" },
      { label: "打开事件", value: String(summary?.openIncidents ?? 0), hint: "open incidents", tone: (summary?.openIncidents ?? 0) > 0 ? "red" as const : "gray" as const },
      { label: "今日发送", value: String(summary?.sentToday ?? 0), hint: "sent deliveries" },
      { label: "今日抑制", value: String(summary?.suppressedToday ?? 0), hint: "dedupe window" },
      { label: "今日恢复", value: String(summary?.recoveredToday ?? 0), hint: "recovery" }
    ];
  }, [center]);
  const filteredRules = useMemo(() => {
    const query = ruleQuery.trim().toLowerCase();
    return (center?.rules ?? []).filter((rule) => {
      const matchesStatus = ruleStatus === "all" || (ruleStatus === "enabled" ? rule.enabled : !rule.enabled);
      const text = `${rule.name} ${rule.kind} ${rule.severity}`.toLowerCase();
      return matchesStatus && (!query || text.includes(query));
    });
  }, [center, ruleQuery, ruleStatus]);
  const filteredChannels = useMemo(() => {
    const query = channelQuery.trim().toLowerCase();
    return (center?.channels ?? []).filter((channel) => {
      const matchesStatus = channelStatus === "all" || (channelStatus === "enabled" ? channel.enabled : !channel.enabled);
      const text = `${channel.name} ${channel.type} ${targetLabel(channel)}`.toLowerCase();
      return matchesStatus && (!query || text.includes(query));
    });
  }, [center, channelQuery, channelStatus]);
  const selectedRuleSet = useMemo(() => new Set(selectedRuleIDs), [selectedRuleIDs]);
  const selectedChannelSet = useMemo(() => new Set(selectedChannelIDs), [selectedChannelIDs]);
  const selectedIncidentSet = useMemo(() => new Set(selectedIncidentIDs), [selectedIncidentIDs]);
  const allFilteredRulesSelected = filteredRules.length > 0 && filteredRules.every((rule) => selectedRuleSet.has(rule.id));
  const allFilteredChannelsSelected = filteredChannels.length > 0 && filteredChannels.every((channel) => selectedChannelSet.has(channel.id));
  const incidentTotalPages = Math.max(1, Math.ceil(incidentItems.length / INCIDENT_PAGE_SIZE));
  const incidentCurrentPage = Math.min(incidentPage, incidentTotalPages);
  const incidentPageStart = (incidentCurrentPage - 1) * INCIDENT_PAGE_SIZE;
  const pagedIncidents = incidentItems.slice(incidentPageStart, incidentPageStart + INCIDENT_PAGE_SIZE);
  const incidentBulkMode = incidentSelectionMode === "bulk";
  const allPagedIncidentsSelected = pagedIncidents.length > 0 && pagedIncidents.every((incident) => selectedIncidentSet.has(incident.id));
  const canOperateWorkspace = scope === "admin" || workspaceCanOperate(workspaceRole);

  useEffect(() => {
    setIncidentPage((current) => Math.min(Math.max(current, 1), incidentTotalPages));
  }, [incidentTotalPages]);

  const Shell = scope === "console" ? ConsoleShell : AdminShell;
  return (
    <Shell title="告警规则" crumb={scope === "console" ? "/ 工作区 / 告警" : "/ 平台治理 / 告警"}>
      <div className="page-intro">{scope === "console" ? "工作区告警只覆盖当前用户的专属中转站、私有通道和成员密钥" : "平台告警覆盖全局通道、网关、Open API 与系统事件"}</div>
      {error ? <div className="form-error">{error}</div> : null}
      {notice ? <div className="form-notice">{notice}</div> : null}

      <StatGrid items={stats} className="alert-summary-stats" />

      <div className="phase14-section-switch alert-section-switch" role="tablist" aria-label="告警治理模块">
        <button type="button" role="tab" aria-selected={activeSection === "rules"} className={activeSection === "rules" ? "active" : ""} onClick={() => setActiveSection("rules")}>规则治理 <span>{center?.rules.length ?? 0}</span></button>
        <button type="button" role="tab" aria-selected={activeSection === "channels"} className={activeSection === "channels" ? "active" : ""} onClick={() => setActiveSection("channels")}>通知渠道 <span>{center?.channels.length ?? 0}</span></button>
        <button type="button" role="tab" aria-selected={activeSection === "history"} className={activeSection === "history" ? "active" : ""} onClick={() => setActiveSection("history")}>发送历史 <span>{center?.deliveries.length ?? 0}</span></button>
        <button type="button" role="tab" aria-selected={activeSection === "incidents"} className={activeSection === "incidents" ? "active" : ""} onClick={() => setActiveSection("incidents")}>Incident 治理 <span>{incidentItems.length}</span></button>
      </div>
      <div className="phase14-risk-note">建议先处理打开事件和规则配置；真实通知测试、立即评估、删除和批量治理都会写入审计。</div>

      {activeSection === "rules" ? (
        <section className="alert-section-panel" aria-label="告警规则治理">
          <div className="section-head" style={{ marginTop: 6 }}>
            <h2>规则列表 <span className="tag">{filteredRules.length}/{center?.rules.length ?? 0}</span></h2>
            <button className="btn btn-primary btn-sm" onClick={runEvaluate} disabled={saving || loading || !canOperateWorkspace}>立即评估</button>
          </div>
          <div className="grid alert-rules-grid alert-module-stack">
        <div className="card card-pad alert-rules-card">
          <BulkActionBar className="bulk-toolbar alert-rules-toolbar">
            <label className="bulk-select">
              <input type="checkbox" checked={allFilteredRulesSelected} disabled={!filteredRules.length || saving || loading || !canOperateWorkspace} onChange={(event) => setAllRuleSelection(event.target.checked)} />
              已选 {selectedRuleIDs.length}
            </label>
            <input className="input" style={{ maxWidth: 230 }} value={ruleQuery} onChange={(event) => setRuleQuery(event.target.value)} placeholder="筛选规则名称 / 类型 / 级别" />
            <SelectField style={{ maxWidth: 150 }} value={ruleStatus} onChange={(event) => setRuleStatus(event.target.value as typeof ruleStatus)}>
              <option value="all">状态：全部</option>
              <option value="enabled">状态：已启用</option>
              <option value="disabled">状态：已停用</option>
            </SelectField>
            <Button variant="ghost" size="sm" onClick={() => void runRuleBulk("enable")} disabled={!selectedRuleIDs.length || saving || !canOperateWorkspace}>批量启用</Button>
            <Button variant="ghost" size="sm" onClick={() => void runRuleBulk("disable")} disabled={!selectedRuleIDs.length || saving || !canOperateWorkspace}>批量停用</Button>
            <Button variant="danger" size="sm" onClick={() => void runRuleBulk("delete")} disabled={!selectedRuleIDs.length || saving || !canOperateWorkspace}>批量删除</Button>
          </BulkActionBar>
          {loading ? <div className="empty-state"><h4>正在加载规则</h4></div> : filteredRules.length ? filteredRules.map((rule) => (
            <RuleRow key={rule.id} rule={rule} selected={selectedRuleSet.has(rule.id)} onSelect={() => toggleRuleSelection(rule.id)} onToggle={() => void toggleRule(rule)} onEdit={() => editRule(rule)} onDelete={() => void removeRule(rule)} disabled={saving || !canOperateWorkspace} />
          )) : <div className="empty-state"><h4>暂无告警规则</h4></div>}
        </div>

        <form className="card card-pad alert-rule-form alert-module-form" onSubmit={(event) => void saveRule(event)}>
          <div className="section-head" style={{ margin: "0 0 14px" }}>
            <h2 style={{ fontSize: 15 }}>{editingRuleID ? "编辑规则" : "新增规则"}</h2>
            {editingRuleID ? <button className="btn btn-ghost btn-sm" type="button" onClick={cancelRuleEdit}>取消编辑</button> : null}
          </div>
          <div className="field">
            <label>规则名称</label>
            <input className="input" value={ruleForm.name} onChange={(event) => setRuleForm({ ...ruleForm, name: event.target.value })} placeholder="成本或错误率告警" />
          </div>
          <div className="grid alert-rule-form-grid">
            <div className="field">
              <label>类型</label>
              <SelectField value={ruleForm.kind} onChange={(event) => setRuleForm({ ...ruleForm, kind: event.target.value as AlertRule["kind"] })}>
                <option value="l3_consecutive_failures">L3 连续失败</option>
                <option value="cost_threshold">成本阈值</option>
                <option value="gateway_error_rate">网关错误率</option>
                <option value="quota_anomaly">配额异常</option>
              </SelectField>
            </div>
            <div className="field">
              <label>级别</label>
              <SelectField value={ruleForm.severity} onChange={(event) => setRuleForm({ ...ruleForm, severity: event.target.value as AlertRule["severity"] })}>
                <option value="warning">warning</option>
                <option value="critical">critical</option>
                <option value="info">info</option>
              </SelectField>
            </div>
            <div className="field">
              <label>阈值</label>
              <input className="input" type="number" value={ruleForm.threshold} onChange={(event) => setRuleForm({ ...ruleForm, threshold: Number(event.target.value) })} />
            </div>
            <div className="field">
              <label>窗口分钟</label>
              <input className="input" type="number" value={ruleForm.windowMinutes} onChange={(event) => setRuleForm({ ...ruleForm, windowMinutes: Number(event.target.value) })} />
            </div>
            <div className="field">
              <label>去重分钟</label>
              <input className="input" type="number" value={ruleForm.dedupeMinutes} onChange={(event) => setRuleForm({ ...ruleForm, dedupeMinutes: Number(event.target.value) })} />
            </div>
            <div className="field">
              <label>状态</label>
              <button className={`switch ${ruleForm.enabled ? "on" : ""}`} type="button" aria-label="启用规则" onClick={() => setRuleForm({ ...ruleForm, enabled: !ruleForm.enabled })} />
            </div>
          </div>
          <button type="submit" className="btn btn-primary btn-sm" disabled={saving || !canOperateWorkspace}>{editingRuleID ? "更新规则" : "保存规则"}</button>
        </form>
      </div>
        </section>
      ) : null}

      {activeSection === "channels" ? (
        <section className="alert-section-panel" aria-label="通知渠道治理">
          <div className="section-head">
            <h2>通知渠道 <span className="tag">{filteredChannels.length}/{center?.channels.length ?? 0}</span></h2>
            <span className="sub">email / webhook / feishu</span>
          </div>
          <div className="grid alert-channel-grid alert-module-stack">
        <div className="card card-pad alert-channel-card">
          <BulkActionBar className="bulk-toolbar">
            <label className="bulk-select">
              <input type="checkbox" checked={allFilteredChannelsSelected} disabled={!filteredChannels.length || saving || loading || !canOperateWorkspace} onChange={(event) => setAllChannelSelection(event.target.checked)} />
              已选 {selectedChannelIDs.length}
            </label>
            <input className="input" style={{ maxWidth: 230 }} value={channelQuery} onChange={(event) => setChannelQuery(event.target.value)} placeholder="筛选渠道名称 / 类型 / 目标" />
            <SelectField style={{ maxWidth: 150 }} value={channelStatus} onChange={(event) => setChannelStatus(event.target.value as typeof channelStatus)}>
              <option value="all">状态：全部</option>
              <option value="enabled">状态：已启用</option>
              <option value="disabled">状态：已停用</option>
            </SelectField>
            <Button variant="ghost" size="sm" onClick={() => void runChannelBulk("enable")} disabled={!selectedChannelIDs.length || saving || !canOperateWorkspace}>批量启用</Button>
            <Button variant="ghost" size="sm" onClick={() => void runChannelBulk("disable")} disabled={!selectedChannelIDs.length || saving || !canOperateWorkspace}>批量停用</Button>
            <Button variant="danger" size="sm" onClick={() => void runChannelBulk("delete")} disabled={!selectedChannelIDs.length || saving || !canOperateWorkspace}>批量删除</Button>
          </BulkActionBar>
          <div className="ch-grid">
            {loading ? <div className="empty-state"><h4>正在加载通知渠道</h4></div> : filteredChannels.length ? filteredChannels.map((channel) => (
              <div className="ch-item" key={channel.id}>
                <input type="checkbox" checked={selectedChannelSet.has(channel.id)} aria-label={`选择通知渠道 ${channel.name}`} onChange={() => toggleChannelSelection(channel.id)} disabled={saving || !canOperateWorkspace} />
                <span className="ci">{channelIcon(channel.type)}</span>
                <span className="cb"><b>{channel.name}</b><small>{channel.type} · {targetLabel(channel)}</small></span>
                <span className={`badge ${channel.enabled ? "b-green" : "b-gray"} dot`}>{channel.enabled ? "on" : "off"}</span>
                <button className={`switch ${channel.enabled ? "on" : ""}`} aria-label="切换渠道" onClick={() => void toggleChannel(channel)} disabled={saving || !canOperateWorkspace} />
                <button className="btn btn-ghost btn-sm" onClick={() => editChannel(channel)} disabled={saving || !canOperateWorkspace}>编辑</button>
                <button className="btn btn-ghost btn-sm" onClick={() => void testChannel(channel.id)} disabled={saving || !canOperateWorkspace}>测试</button>
                <button className="btn btn-ghost btn-sm" onClick={() => void removeChannel(channel)} disabled={saving || !canOperateWorkspace}>删除</button>
              </div>
            )) : <div className="empty-state"><h4>暂无通知渠道</h4></div>}
          </div>
        </div>
        <form className="card card-pad alert-channel-form alert-module-form" onSubmit={(event) => void saveChannel(event)}>
          <div className="section-head" style={{ margin: "0 0 14px" }}>
            <h2 style={{ fontSize: 15 }}>{editingChannelID ? "编辑渠道" : "新增渠道"}</h2>
            {editingChannelID ? <button className="btn btn-ghost btn-sm" type="button" onClick={cancelChannelEdit}>取消编辑</button> : null}
          </div>
          <div className="field"><label>名称</label><input className="input" value={channelForm.name} onChange={(event) => setChannelForm({ ...channelForm, name: event.target.value })} placeholder="值班邮件" /></div>
          <div className="field">
            <label>类型</label>
            <SelectField value={channelForm.type} onChange={(event) => setChannelForm({ ...channelForm, type: event.target.value as NotificationChannel["type"] })}>
              <option value="email">email</option>
              <option value="webhook">webhook</option>
              <option value="feishu">feishu</option>
            </SelectField>
          </div>
          <div className="field">
            <label>{editingChannelID ? "新目标" : "目标"}</label>
            <input className="input" value={channelForm.target} onChange={(event) => setChannelForm({ ...channelForm, target: event.target.value })} placeholder={editingChannelID ? `留空保留当前目标：${editingChannelMask || "已配置"}` : "ops@example.com"} />
          </div>
          <div className="field">
            <label>状态</label>
            <button className={`switch ${channelForm.enabled ? "on" : ""}`} type="button" aria-label="启用渠道" onClick={() => setChannelForm({ ...channelForm, enabled: !channelForm.enabled })} />
          </div>
          <button type="submit" className="btn btn-primary btn-sm" disabled={saving || !canOperateWorkspace}>{editingChannelID ? "更新渠道" : "保存渠道"}</button>
        </form>
      </div>
        </section>
      ) : null}

      {activeSection === "history" ? (
        <section className="alert-section-panel" aria-label="告警发送历史">
          <div className="section-head">
            <h2>告警历史</h2>
            <span className="sub">最近 50 条 delivery</span>
          </div>
          <DeliveryTable items={center?.deliveries ?? []} loading={loading} />
        </section>
      ) : null}

      {activeSection === "incidents" ? (
        <section className="alert-section-panel" aria-label="Incident 治理">
          <div className="section-head">
            <h2>Incident 治理 <span className="tag">{incidentItems.length}</span></h2>
            <span className="sub">筛选、登记、编辑、关闭、重开、删除、批量治理</span>
          </div>
          <div className="grid incident-grid alert-module-stack">
        <div className="card board incident-card">
          <FilterBar className="bulk-toolbar incident-toolbar">
            <input className="input" value={incidentQuery} onChange={(event) => setIncidentQuery(event.target.value)} placeholder="筛选标题 / 通道 / 状态" />
            <SelectField value={incidentStatus} onChange={(event) => setIncidentStatus(event.target.value)}>
              <option value="all">事件：全部</option>
              <option value="open">事件：打开中</option>
              <option value="resolved">事件：已关闭</option>
              <option value="manual">事件：人工登记</option>
              <option value="connectivity_down">事件：连通异常</option>
              <option value="auth_error">事件：鉴权异常</option>
              <option value="unknown">事件：未知</option>
            </SelectField>
            <SelectField className="incident-mode-select" value={incidentSelectionMode} onChange={(event) => {
              const nextMode = event.target.value === "bulk" ? "bulk" : "browse";
              setIncidentSelectionMode(nextMode);
              if (nextMode === "browse") setSelectedIncidentIDs([]);
            }}>
              <option value="browse">浏览</option>
              <option value="bulk">多选</option>
            </SelectField>
            <div className="incident-filter-actions">
              <Button variant="ghost" size="sm" onClick={() => void loadIncidentList()} disabled={incidentLoading || saving}>筛选</Button>
              <Button variant="ghost" size="sm" onClick={resetIncidentFilters} disabled={incidentLoading || saving}>重置</Button>
            </div>
            {incidentBulkMode ? (
              <>
                <label className="bulk-select incident-bulk-select">
                  <input type="checkbox" aria-label="选择当前页 Incident" checked={allPagedIncidentsSelected} disabled={!pagedIncidents.length || incidentLoading || saving || loading || !canOperateWorkspace} onChange={(event) => setAllIncidentSelection(event.target.checked)} />
                  已选 {selectedIncidentIDs.length}
                </label>
                <div className="incident-bulk-actions">
                  <Button variant="ghost" size="sm" title="批量关闭" onClick={() => void runIncidentBulk("resolve")} disabled={!selectedIncidentIDs.length || incidentLoading || saving || !canOperateWorkspace}>关闭</Button>
                  <Button variant="ghost" size="sm" title="批量重开" onClick={() => void runIncidentBulk("reopen")} disabled={!selectedIncidentIDs.length || incidentLoading || saving || !canOperateWorkspace}>重开</Button>
                  <Button variant="danger" size="sm" title="批量删除" onClick={() => void runIncidentBulk("delete")} disabled={!selectedIncidentIDs.length || incidentLoading || saving || !canOperateWorkspace}>删除</Button>
                </div>
              </>
            ) : null}
          </FilterBar>
          {incidentLoading || loading ? (
            <div className="empty-state incident-empty-state"><h4>正在加载 Incident…</h4></div>
          ) : incidentItems.length ? (
            <>
              <div className="dt-wrap incident-table-wrap">
                <table className={`dt incident-table ${incidentBulkMode ? "select-mode" : "browse-mode"}`}>
                  <colgroup>
                    {incidentBulkMode ? <col className="incident-select-col" /> : null}
                    <col className="incident-status-col" />
                    <col className="incident-channel-col" />
                    <col className="incident-title-col" />
                    <col className="incident-time-col" />
                    <col className="incident-event-col" />
                    <col className="incident-actions-col" />
                  </colgroup>
                  <thead>
                    <tr>
                      {incidentBulkMode ? <th><input type="checkbox" aria-label="选择当前页全部 Incident" checked={allPagedIncidentsSelected} disabled={!pagedIncidents.length || saving || loading || !canOperateWorkspace} onChange={(event) => setAllIncidentSelection(event.target.checked)} /></th> : null}
                      <th>状态</th><th>通道</th><th>标题</th><th>打开时间</th><th>最近事件</th><th>操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pagedIncidents.map((incident) => (
                    <tr key={incident.id}>
                      {incidentBulkMode ? <td><input type="checkbox" aria-label={`选择 Incident ${incident.title}`} checked={selectedIncidentSet.has(incident.id)} onChange={() => toggleIncidentSelection(incident.id)} disabled={saving || !canOperateWorkspace} /></td> : null}
                      <td className="incident-status-cell"><span className={`badge ${incident.open ? "b-red" : "b-green"} dot`}>{incident.open ? "open" : "resolved"}</span><span className={`badge ${incidentBadge(incident.status)} dot`}>{incident.status}</span></td>
                      <td className="incident-channel-cell"><b>{incident.channel}</b><div className="muted mono">{incident.channelId}</div></td>
                      <td className="incident-title-cell" title={incident.title}>{incident.title}</td>
                      <td className="mono incident-time-cell">{timeLabel(incident.openedAt)}</td>
                      <td className="incident-event-cell" title={incident.events?.[0]?.message ?? "-"}>{incident.events?.[0]?.message ?? "-"}</td>
                      <td className="incident-actions-cell">
                        <div className="inline-actions">
                          <button className="btn btn-ghost btn-sm" onClick={() => editIncident(incident)} disabled={saving || !canOperateWorkspace}>编辑</button>
                          {incident.open ? <button className="btn btn-ghost btn-sm" onClick={() => void closeIncident(incident)} disabled={saving || !canOperateWorkspace}>关闭</button> : <button className="btn btn-ghost btn-sm" onClick={() => void openIncident(incident)} disabled={saving || !canOperateWorkspace}>重开</button>}
                          <button className="btn btn-ghost btn-sm" onClick={() => void removeIncident(incident)} disabled={saving || !canOperateWorkspace}>删除</button>
                        </div>
                      </td>
                    </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <Pagination
                page={incidentCurrentPage}
                totalPages={incidentTotalPages}
                pageSize={INCIDENT_PAGE_SIZE}
                total={incidentItems.length}
                note="Incident 列表 · 批量治理需先切换到多选模式"
                onPageChange={setIncidentPage}
              />
            </>
          ) : (
            <div className="empty-state incident-empty-state">
              <h4>暂无 Incident</h4>
              <p>当前筛选条件下没有打开或历史事件。你也可以在右侧登记人工 Incident。</p>
            </div>
          )}
        </div>
        <form className="card card-pad incident-form alert-module-form" onSubmit={(event) => void saveIncident(event)}>
          <div className="section-head" style={{ margin: "0 0 14px" }}>
            <h2 style={{ fontSize: 15 }}>{editingIncidentID ? "编辑 Incident" : "登记 Incident"}</h2>
            {editingIncidentID ? <button className="btn btn-ghost btn-sm" type="button" onClick={cancelIncidentEdit}>取消编辑</button> : <span className="sub">选择已有通道</span>}
          </div>
          <div className="field">
            <label>关联通道</label>
            {editingIncidentID ? (
              <input className="input mono" value={incidentForm.channelId} disabled />
            ) : incidentChannelOptions.length ? (
              <SelectField value={incidentForm.channelId} onChange={(event) => setIncidentForm({ ...incidentForm, channelId: event.target.value })}>
                <option value="">选择要登记的通道</option>
                {incidentChannelOptions.map((option) => (
                  <option value={option.id} key={option.id}>{option.label} · {option.meta}</option>
                ))}
              </SelectField>
            ) : (
              <input className="input mono" value={incidentForm.channelId} onChange={(event) => setIncidentForm({ ...incidentForm, channelId: event.target.value })} placeholder="暂无可选通道，可先去添加私有通道" />
            )}
            <div className="form-help">
              {incidentChannelOptions.length
                ? "Incident 会绑定到选中的通道，后续可在列表里按通道筛选。"
                : <><span>当前没有可选通道。</span><a href={scope === "console" ? "/console/channels" : "/admin/channels"}>去添加通道</a></>}
            </div>
          </div>
          <div className="field">
            <label>状态</label>
            <SelectField value={incidentForm.status} onChange={(event) => setIncidentForm({ ...incidentForm, status: event.target.value })}>
              <option value="manual">manual</option>
              <option value="connectivity_down">connectivity_down</option>
              <option value="auth_error">auth_error</option>
              <option value="unknown">unknown</option>
            </SelectField>
          </div>
          <div className="field"><label>标题</label><input className="input" value={incidentForm.title} onChange={(event) => setIncidentForm({ ...incidentForm, title: event.target.value })} placeholder="例如：供应商模型不可用" /></div>
          <div className="field"><label>事件说明</label><textarea className="input" rows={4} value={incidentForm.message} onChange={(event) => setIncidentForm({ ...incidentForm, message: event.target.value })} placeholder="记录排查背景、影响范围或处置动作" /></div>
          <button type="submit" className="btn btn-primary btn-sm" disabled={saving || !canOperateWorkspace || !incidentForm.channelId.trim() || !incidentForm.title.trim()}>{editingIncidentID ? "更新事件" : "登记事件"}</button>
        </form>
      </div>
        </section>
      ) : null}
    </Shell>
  );
}

async function loadIncidentChannelOptions(scope: Scope): Promise<IncidentChannelOption[]> {
  if (scope === "console") {
    const payload = await privateChannels();
    return (payload.items ?? []).map((item) => ({
      id: item.id,
      label: item.name,
      meta: `${item.provider} · ${item.model}`
    }));
  }
  const payload = await adminChannels();
  const platform = (payload.items ?? []).map((item) => ({
    id: item.id,
    label: item.name,
    meta: `${item.provider} · ${item.model}`
  }));
  const privateItems = (payload.private ?? []).map((item) => ({
    id: item.id,
    label: item.name,
    meta: `${item.ownerEmail} · ${item.model}`
  }));
  return [...platform, ...privateItems];
}

function RuleRow({ rule, selected, onSelect, onToggle, onEdit, onDelete, disabled }: { rule: AlertRule; selected: boolean; onSelect: () => void; onToggle: () => void; onEdit: () => void; onDelete: () => void; disabled: boolean }) {
  return (
    <div className="rule alert-rule-row">
      <input type="checkbox" checked={selected} aria-label={`选择告警规则 ${rule.name}`} onChange={onSelect} disabled={disabled} />
      <div className="ic" style={{ background: severitySoft(rule.severity), color: severityColor(rule.severity) }}>{kindIcon(rule.kind)}</div>
      <div className="b">
        <div className="t">{rule.name}<span className={`badge ${severityBadge(rule.severity)} dot`}>{rule.severity}</span></div>
        <div className="c">
          <span className="cond">{kindLabel(rule.kind)}</span>
          <span className="cond">threshold {rule.threshold}</span>
          <span className="cond">{rule.windowMinutes}m window</span>
          <span className="cond">{rule.dedupeMinutes}m dedupe</span>
        </div>
      </div>
      <div className="alert-rule-actions">
        <button className="btn btn-ghost btn-sm" onClick={onEdit} disabled={disabled}>编辑</button>
        <button className="btn btn-ghost btn-sm" onClick={onDelete} disabled={disabled}>删除</button>
        <button className={`switch ${rule.enabled ? "on" : ""}`} aria-label="切换规则" onClick={onToggle} disabled={disabled} />
      </div>
    </div>
  );
}

function DeliveryTable({ items, loading }: { items: AlertDelivery[]; loading: boolean }) {
  const columns = useMemo<DataTableColumn<AlertDelivery>[]>(() => [
    { id: "time", header: "时间", cell: ({ row }) => <span className="mono">{timeLabel(row.original.createdAt)}</span>, meta: { width: "14%", wrap: "nowrap" } },
    { id: "rule", header: "规则", cell: ({ row }) => <span title={row.original.ruleName || row.original.title}>{row.original.ruleName || row.original.title}</span>, meta: { width: "22%", truncate: true } },
    { id: "severity", header: "级别", cell: ({ row }) => <StatusBadge tone={severityTone(row.original.severity)}>{row.original.severity}</StatusBadge>, meta: { width: "10%" } },
    { id: "status", header: "状态", cell: ({ row }) => <StatusBadge tone={deliveryTone(row.original.status)}>{row.original.status}</StatusBadge>, meta: { width: "10%" } },
    { id: "channel", header: "渠道", cell: ({ row }) => <span title={row.original.channelName || "-"}>{row.original.channelName || "-"}</span>, meta: { width: "14%", truncate: true } },
    { id: "message", header: "消息", cell: ({ row }) => <span title={row.original.message}>{row.original.message}</span>, meta: { width: "30%", truncate: true } }
  ], []);
  return (
    <DataTable
      data={items}
      columns={columns}
      loading={loading}
      pageSize={10}
      rowKey={(row) => row.id}
      tableClassName="alert-delivery-table"
      loadingText="正在加载告警历史..."
      empty={<div className="empty-state alert-delivery-empty"><h4>暂无发送记录</h4></div>}
      footerNote="alert delivery"
    />
  );
}

function severityTone(severity: string): "red" | "amber" | "blue" {
  if (severity === "critical") return "red";
  if (severity === "warning") return "amber";
  return "blue";
}

function deliveryTone(status: string): "green" | "amber" | "red" | "gray" {
  if (status === "sent" || status === "delivered") return "green";
  if (status === "suppressed") return "amber";
  if (status === "failed") return "red";
  return "gray";
}

function kindLabel(kind: AlertRule["kind"]) {
  if (kind === "cost_threshold") return "成本阈值";
  if (kind === "gateway_error_rate") return "网关错误率";
  if (kind === "quota_anomaly") return "配额异常";
  return "L3 连续失败";
}

function kindIcon(kind: AlertRule["kind"]) {
  if (kind === "cost_threshold") return "$";
  if (kind === "gateway_error_rate") return "%";
  if (kind === "quota_anomaly") return "Q";
  return "L3";
}

function severityBadge(value: string) {
  if (value === "critical") return "b-red";
  if (value === "info") return "b-blue";
  return "b-amber";
}

function statusBadge(value: string) {
  if (value === "sent" || value === "test" || value === "recovered") return "b-green";
  if (value === "suppressed") return "b-amber";
  return "b-red";
}

function incidentBadge(value: string) {
  if (value === "manual") return "b-blue";
  if (value === "auth_error" || value === "connectivity_down") return "b-red";
  if (value === "unknown") return "b-amber";
  return "b-gray";
}

function severitySoft(value: string) {
  if (value === "critical") return "var(--red-soft)";
  if (value === "info") return "var(--blue-soft)";
  return "var(--amber-soft)";
}

function severityColor(value: string) {
  if (value === "critical") return "var(--red)";
  if (value === "info") return "var(--blue)";
  return "var(--amber)";
}

function channelIcon(value: string) {
  if (value === "webhook") return "↗";
  if (value === "feishu") return "飞";
  return "@";
}

function targetLabel(channel: NotificationChannel) {
  return channel.targetMask || channel.target || "已配置";
}

function timeLabel(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "-" : date.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}
