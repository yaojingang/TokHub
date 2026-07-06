import { FormEvent, useEffect, useMemo, useState } from "react";
import { useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { AdminShell } from "../components/AdminShell";
import { ConsoleShell } from "../components/ConsoleShell";
import {
  adminGateways,
  adminMembers,
  bulkAdminGatewayKeys,
  bulkAdminMembers,
  bulkConsoleGatewayKeys,
  bulkConsoleMembers,
  consoleGateways,
  consoleMembers,
  consoleSettings,
  createAdminGatewayKey,
  deleteAdminGatewayKey,
  deleteGatewayKey,
  createGatewayKey,
  debugGateway,
  Gateway,
  GatewayDebugResult,
  GatewayKey,
  GatewayMember,
  inviteAdminMember,
  inviteConsoleMember,
  removeAdminMember,
  removeConsoleMember,
  revokeAdminGatewayKey,
  revokeGatewayKey,
  updateAdminGatewayKey,
  updateAdminMember,
  updateConsoleGatewayKey,
  updateConsoleMember,
  workspaceCanManage,
  workspaceCanOperate
} from "../lib/api";
import { BulkActionBar, DataTable, DataTableColumn, Dialog, FilterBar, FormField, SelectField, StatGrid } from "../ui";

type Tab = "members" | "users" | "keys";
type KeyTestState = {
  status: "running" | "ok" | "failed";
  result?: GatewayDebugResult;
  error?: string;
};

const defaultGatewayKeyQuotaMonth = 100000;
const defaultGatewayKeyQPSLimit = 60;

export function AdminMembersPage({ scope = "console" }: { scope?: "admin" | "console" }) {
  const { t } = useTranslation(["common", "admin", "console"]);
  const adminMode = scope === "admin";
  const Shell = adminMode ? AdminShell : ConsoleShell;
  const location = useLocation();
  const [tab, setTab] = useState<Tab>("members");
  const [members, setMembers] = useState<GatewayMember[]>([]);
  const [keys, setKeys] = useState<GatewayKey[]>([]);
  const [gateways, setGateways] = useState<Gateway[]>([]);
  const [workspaceRole, setWorkspaceRole] = useState("");
  const [keyName, setKeyName] = useState("生产服务 Key");
  const [inviteEmail, setInviteEmail] = useState("");
  const [inviteRole, setInviteRole] = useState("viewer");
  const [inviteGroup, setInviteGroup] = useState("工作区成员");
  const [gatewayID, setGatewayID] = useState("");
  const [quotaMonth, setQuotaMonth] = useState(defaultGatewayKeyQuotaMonth);
  const [qpsLimit, setQpsLimit] = useState(defaultGatewayKeyQPSLimit);
  const [createdKey, setCreatedKey] = useState<GatewayKey | null>(null);
  const [createdKeyDialogOpen, setCreatedKeyDialogOpen] = useState(false);
  const [editingKey, setEditingKey] = useState<GatewayKey | null>(null);
  const [keyTests, setKeyTests] = useState<Record<string, KeyTestState>>({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [query, setQuery] = useState("");
  const [roleFilter, setRoleFilter] = useState("all");
  const [keyStatusFilter, setKeyStatusFilter] = useState("all");
  const [keyGatewayFilter, setKeyGatewayFilter] = useState("all");
  const [selectedMemberIDs, setSelectedMemberIDs] = useState<string[]>([]);
  const [selectedKeyIDs, setSelectedKeyIDs] = useState<string[]>([]);
  const [bulkMemberRole, setBulkMemberRole] = useState("viewer");
  const [bulkKeyStatus, setBulkKeyStatus] = useState("active");

  useEffect(() => {
    void load();
  }, []);

  useEffect(() => {
    if (location.pathname.endsWith("/keys")) {
      setTab("keys");
    } else if (location.pathname.endsWith("/members")) {
      setTab("members");
    }
  }, [location.pathname]);

  useEffect(() => {
    setQuery("");
    setRoleFilter("all");
    setKeyStatusFilter("all");
    setKeyGatewayFilter("all");
    setSelectedMemberIDs([]);
    setSelectedKeyIDs([]);
  }, [tab]);

  useEffect(() => {
    const gateway = gateways.find((item) => item.id === gatewayID);
    if (!gateway) return;
    setQuotaMonth(gateway.quotaMonth);
    setQpsLimit(gateway.qpsLimit);
  }, [gatewayID, gateways]);

  async function load() {
    setLoading(true);
    setError("");
    try {
      const [memberPayload, gatewayPayload, settingsPayload] = await Promise.all([
        adminMode ? adminMembers() : consoleMembers(),
        adminMode ? adminGateways() : consoleGateways(),
        adminMode ? Promise.resolve(null) : consoleSettings()
      ]);
      setMembers(memberPayload.members ?? []);
      setKeys(memberPayload.keys ?? []);
      setGateways(gatewayPayload.items ?? []);
      setWorkspaceRole(settingsPayload?.workspace.role ?? (adminMode ? "admin" : ""));
      setGatewayID((current) => current || gatewayPayload.items?.[0]?.id || "");
      setSelectedMemberIDs((current) => current.filter((id) => (memberPayload.members ?? []).some((item) => item.userId === id)));
      setSelectedKeyIDs((current) => current.filter((id) => (memberPayload.keys ?? []).some((item) => item.id === id)));
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载成员与密钥失败");
    } finally {
      setLoading(false);
    }
  }

  async function submitKey() {
    if (!adminMode && !workspaceCanOperate(workspaceRole)) {
      setError("当前工作区角色不能签发 Gateway Key。");
      return;
    }
    if (!gatewayID) {
      setError("请先创建一个专属中转站。");
      return;
    }
    if (!isPositiveInteger(quotaMonth)) {
      setError("月度限额必须是大于 0 的整数。");
      return;
    }
    if (!isPositiveInteger(qpsLimit)) {
      setError("QPS 必须是大于 0 的整数。");
      return;
    }
    setSaving(true);
    setError("");
    setNotice("");
    setCreatedKey(null);
    setCreatedKeyDialogOpen(false);
    try {
      const payload = adminMode
        ? await createAdminGatewayKey({ gatewayId: gatewayID, name: keyName, quotaMonth, qpsLimit })
        : await createGatewayKey({ gatewayId: gatewayID, name: keyName, quotaMonth, qpsLimit });
      const listKey: GatewayKey = { ...payload.key, plainKey: undefined };
      setCreatedKey(payload.key);
      setCreatedKeyDialogOpen(true);
      setKeys((items) => [listKey, ...items]);
      setTab("keys");
      setNotice("API Key 已签发。完整 Key 只展示一次，请先复制保存。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "签发 Key 失败");
    } finally {
      setSaving(false);
    }
  }

  function closeCreatedKeyDialog() {
    setCreatedKeyDialogOpen(false);
    setCreatedKey(null);
  }

  async function revoke(key: GatewayKey) {
    if (!adminMode && !workspaceCanOperate(workspaceRole)) {
      setError("当前工作区角色不能吊销 Gateway Key。");
      return;
    }
    if (!window.confirm(`确认吊销 ${key.name}？吊销后调用会立即返回 401。`)) return;
    setError("");
    try {
      if (adminMode) {
        await revokeAdminGatewayKey(key.id);
      } else {
        await revokeGatewayKey(key.id);
      }
      setKeys((items) => items.map((item) => item.id === key.id ? { ...item, status: "revoked" } : item));
      setNotice("Key 已吊销。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "吊销失败");
    }
  }

  async function removeKey(key: GatewayKey) {
    if (!adminMode && !workspaceCanOperate(workspaceRole)) {
      setError("当前工作区角色不能删除 Gateway Key。");
      return;
    }
    if (!window.confirm(`确认删除 ${key.name}？删除后该 Key 会从列表移除，调用会立即返回 401，历史用量和审计仍会保留。`)) return;
    setError("");
    setNotice("");
    try {
      if (adminMode) {
        await deleteAdminGatewayKey(key.id);
      } else {
        await deleteGatewayKey(key.id);
      }
      setKeys((items) => items.filter((item) => item.id !== key.id));
      setSelectedKeyIDs((items) => items.filter((id) => id !== key.id));
      setNotice("Key 已删除。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "删除 Key 失败");
    }
  }

  async function inviteMember() {
    if (!adminMode && !workspaceCanManage(workspaceRole)) {
      setError("当前工作区角色不能邀请成员。");
      return;
    }
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const payload = adminMode
        ? await inviteAdminMember({ email: inviteEmail, role: inviteRole, groupName: inviteGroup })
        : await inviteConsoleMember({ email: inviteEmail, role: inviteRole, groupName: inviteGroup });
      setMembers((items) => [payload.member, ...items.filter((item) => item.userId !== payload.member.userId)]);
      setInviteEmail("");
      setNotice("成员已加入当前工作区。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "邀请成员失败");
    } finally {
      setSaving(false);
    }
  }

  async function changeMemberRole(member: GatewayMember, role: string) {
    if (!adminMode && !workspaceCanManage(workspaceRole)) {
      setError("当前工作区角色不能修改成员角色。");
      return;
    }
    setError("");
    setNotice("");
    try {
      const payload = adminMode
        ? await updateAdminMember(member.userId, { role, groupName: member.groupName })
        : await updateConsoleMember(member.userId, { role, groupName: member.groupName });
      setMembers((items) => items.map((item) => item.userId === member.userId ? payload.member : item));
      setNotice("成员角色已更新。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "更新成员失败");
    }
  }

  async function changeMemberGroup(member: GatewayMember, groupName: string) {
    if (!adminMode && !workspaceCanManage(workspaceRole)) {
      setError("当前工作区角色不能修改成员分组。");
      return;
    }
    const normalized = groupName.trim();
    if (normalized === member.groupName) return;
    setError("");
    setNotice("");
    try {
      const payload = adminMode
        ? await updateAdminMember(member.userId, { role: member.role, groupName: normalized })
        : await updateConsoleMember(member.userId, { role: member.role, groupName: normalized });
      setMembers((items) => items.map((item) => item.userId === member.userId ? payload.member : item));
      setNotice("成员分组已更新。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "更新成员分组失败");
    }
  }

  async function removeMember(member: GatewayMember) {
    if (!adminMode && !workspaceCanManage(workspaceRole)) {
      setError("当前工作区角色不能移除成员。");
      return;
    }
    if (!window.confirm(`确认从工作区移除 ${member.email}？该用户将不能再查看此工作区成员、审计和网关配置。`)) return;
    setError("");
    setNotice("");
    try {
      if (adminMode) {
        await removeAdminMember(member.userId);
      } else {
        await removeConsoleMember(member.userId);
      }
      setMembers((items) => adminMode
        ? items.map((item) => item.userId === member.userId ? { ...item, role: "user", groupName: "默认组" } : item)
        : items.filter((item) => item.userId !== member.userId));
      setNotice("成员已移除。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "移除成员失败");
    }
  }

  async function saveKey(keyID: string, input: { name: string; quotaMonth: number; qpsLimit: number; status: string }) {
    if (!adminMode && !workspaceCanOperate(workspaceRole)) {
      setError("当前工作区角色不能保存 Gateway Key。");
      return;
    }
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const updateKey = adminMode ? updateAdminGatewayKey : updateConsoleGatewayKey;
      const payload = await updateKey(keyID, input);
      setKeys((items) => items.map((item) => item.id === keyID ? payload.key : item));
      setEditingKey(null);
      setNotice("Key 配置已保存。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存 Key 失败");
    } finally {
      setSaving(false);
    }
  }

  async function testKeyGateway(key: GatewayKey) {
    if (adminMode) {
      setError("平台后台暂不支持从 Key 列表发起工作区网关测试，请到用户控制台测试。");
      return;
    }
    if (!workspaceCanOperate(workspaceRole)) {
      setError("当前工作区角色不能测试网关。");
      return;
    }
    setError("");
    setNotice("");
    setKeyTests((items) => ({ ...items, [key.id]: { status: "running" } }));
    try {
      const gateway = gateways.find((item) => item.id === key.gatewayId);
      const model = gateway?.upstreams[0]?.model ?? "";
      const payload = await debugGateway(key.gatewayId, { model, prompt: "Reply exactly: OK", kind: "chat" });
      setKeyTests((items) => ({
        ...items,
        [key.id]: { status: payload.result.ok ? "ok" : "failed", result: payload.result }
      }));
      setNotice(payload.result.ok ? `网关测试通过：${payload.result.latencyMs}ms。` : `网关测试失败：${payload.result.message}`);
    } catch (err) {
      setKeyTests((items) => ({
        ...items,
        [key.id]: { status: "failed", error: err instanceof Error ? err.message : "网关测试失败" }
      }));
    }
  }

  function toggleMemberSelection(userID: string, checked: boolean) {
    setSelectedMemberIDs((current) => checked ? Array.from(new Set([...current, userID])) : current.filter((id) => id !== userID));
  }

  function toggleVisibleMembers(checked: boolean) {
    const visibleIDs = filteredMembers.filter((member) => member.role !== "owner").map((member) => member.userId);
    setSelectedMemberIDs((current) => {
      if (!checked) return current.filter((id) => !visibleIDs.includes(id));
      return Array.from(new Set([...current, ...visibleIDs]));
    });
  }

  function toggleKeySelection(keyID: string, checked: boolean) {
    setSelectedKeyIDs((current) => checked ? Array.from(new Set([...current, keyID])) : current.filter((id) => id !== keyID));
  }

  function toggleVisibleKeys(checked: boolean) {
    const visibleIDs = filteredKeys.map((key) => key.id);
    setSelectedKeyIDs((current) => {
      if (!checked) return current.filter((id) => !visibleIDs.includes(id));
      return Array.from(new Set([...current, ...visibleIDs]));
    });
  }

  async function runBulkMembers(action: "role" | "delete") {
    if (!adminMode && !workspaceCanManage(workspaceRole)) {
      setError("当前工作区角色不能批量治理成员。");
      return;
    }
    if (!selectedMemberIDs.length) return;
    const ids = selectedMemberIDs.filter((id) => members.some((member) => member.userId === id && member.role !== "owner"));
    if (!ids.length) {
      setError("请先选择可治理成员。");
      return;
    }
    const text = action === "delete" ? `移除 ${ids.length} 名成员` : `将 ${ids.length} 名成员改为 ${roleText(bulkMemberRole)}`;
    if (!window.confirm(`确认${text}？该操作会写入审计。`)) return;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const bulkMembers = adminMode ? bulkAdminMembers : bulkConsoleMembers;
      const payload = action === "delete"
        ? await bulkMembers({ action: "delete", ids })
        : await bulkMembers({ action: "role", ids, role: bulkMemberRole });
      setMembers(payload.members ?? []);
      setKeys(payload.keys ?? keys);
      setSelectedMemberIDs([]);
      setNotice(action === "delete" ? "批量移除成员完成。" : "批量角色更新完成。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "成员批量操作失败");
    } finally {
      setSaving(false);
    }
  }

  async function runBulkKeys(action: "status" | "revoke" | "delete") {
    if (!adminMode && !workspaceCanOperate(workspaceRole)) {
      setError("当前工作区角色不能批量治理 Gateway Key。");
      return;
    }
    if (!selectedKeyIDs.length) return;
    const ids = selectedKeyIDs.filter((id) => keys.some((key) => key.id === id));
    if (!ids.length) {
      setError("请先选择 Gateway Key。");
      return;
    }
    const text = action === "delete"
      ? `删除 ${ids.length} 个 Key`
      : action === "revoke"
        ? `吊销 ${ids.length} 个 Key`
        : `将 ${ids.length} 个 Key 改为 ${keyStatusText(bulkKeyStatus)}`;
    const suffix = action === "delete"
      ? "删除后会从列表移除，历史用量和审计仍会保留。"
      : action === "revoke"
        ? "吊销后调用会立即返回 401，且不能重新改回 Active。"
        : "该操作会写入审计。";
    if (!window.confirm(`确认${text}？${suffix}`)) return;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const bulkKeys = adminMode ? bulkAdminGatewayKeys : bulkConsoleGatewayKeys;
      const payload = action === "delete"
        ? await bulkKeys({ action: "delete", ids })
        : action === "revoke"
          ? await bulkKeys({ action: "revoke", ids })
          : await bulkKeys({ action: "status", ids, status: bulkKeyStatus });
      setKeys(payload.items ?? []);
      setSelectedKeyIDs([]);
      setNotice(action === "delete" ? "批量删除完成。" : action === "revoke" ? "批量吊销完成。" : "Key 批量状态更新完成。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Key 批量操作失败");
    } finally {
      setSaving(false);
    }
  }

  const workspaceMembers = useMemo(() => members.filter((item) => item.role !== "user"), [members]);
  const users = useMemo(() => members.filter((item) => item.role === "user"), [members]);
  const activeKeys = keys.filter((item) => item.status === "active").length;
  const term = query.trim().toLowerCase();
  const filteredMembers = useMemo(() => workspaceMembers.filter((member) => {
    const roleOK = roleFilter === "all" || member.role === roleFilter;
    const textOK = !term || [member.name, member.email, member.groupName, member.role].join(" ").toLowerCase().includes(term);
    return roleOK && textOK;
  }), [workspaceMembers, roleFilter, term]);
  const filteredUsers = useMemo(() => users.filter((member) => {
    return !term || [member.name, member.email, member.status].join(" ").toLowerCase().includes(term);
  }), [users, term]);
  const filteredKeys = useMemo(() => keys.filter((key) => {
    const statusOK = keyStatusFilter === "all" || key.status === keyStatusFilter;
    const gatewayOK = keyGatewayFilter === "all" || key.gatewayId === keyGatewayFilter;
    const textOK = !term || [key.name, key.keyMask, key.keyPrefix, key.gatewayName, key.status].join(" ").toLowerCase().includes(term);
    return statusOK && gatewayOK && textOK;
  }), [keys, keyStatusFilter, keyGatewayFilter, term]);
  const selectedMemberSet = useMemo(() => new Set(selectedMemberIDs), [selectedMemberIDs]);
  const selectedKeySet = useMemo(() => new Set(selectedKeyIDs), [selectedKeyIDs]);
  const canManageWorkspace = adminMode || workspaceCanManage(workspaceRole);
  const canOperateWorkspace = adminMode || workspaceCanOperate(workspaceRole);
  const selectableVisibleMemberIDs = filteredMembers.filter((member) => member.role !== "owner").map((member) => member.userId);
  const allVisibleMembersSelected = selectableVisibleMemberIDs.length > 0 && selectableVisibleMemberIDs.every((id) => selectedMemberSet.has(id));
  const allVisibleKeysSelected = filteredKeys.length > 0 && filteredKeys.every((key) => selectedKeySet.has(key.id));

  return (
    <Shell title={adminMode ? t("admin:members.title") : t("console:members.title")} crumb={adminMode ? t("admin:crumbs.members") : t("console:crumbs.keys")}>
      <p className="page-intro">{adminMode ? t("admin:members.intro") : t("console:members.intro")}</p>
      {error ? <div className="form-error">{error}</div> : null}
      {notice ? <div className="form-notice">{notice}</div> : null}

      <StatGrid
        className="member-key-stats"
        items={[
          { label: "工作区成员", value: workspaceMembers.length, hint: "含 Owner、Admin 和协作成员" },
          { label: "专属中转站", value: gateways.length, hint: "可签发 Key 的入口" },
          { label: "有效 API Key", value: activeKeys, hint: `${keys.length - activeKeys} 个已吊销` },
          { label: "今日调用", value: keys.reduce((sum, item) => sum + item.requestsUsed, 0), hint: "按 Key 累计请求" },
          { label: "普通用户", value: users.length, hint: "前台注册账号" }
        ]}
      />

      <div className="card card-pad key-create-panel">
        <div className="section-head" style={{ margin: "0 0 14px" }}>
          <h2 style={{ fontSize: 15 }}>签发 Gateway Key</h2>
          <span className="sub">完整 Key 只在签发时展示一次</span>
        </div>
        {!gateways.length ? (
          <div className="key-empty-guidance">
            <div>
              <b>先创建专属中转站，再签发 API Key</b>
              <p>{adminMode ? "当前还没有可签发 Key 的平台网关。" : "普通用户的 Key 只能绑定自己的专属中转站，不能直接调用平台公开监控通道或系统 API Key。"}</p>
            </div>
            <a className="btn btn-primary btn-sm" href={adminMode ? "/admin/gateways" : "/console/gateways"}>去创建中转站</a>
          </div>
        ) : null}
        <div className="settings-grid-inline">
          <FormField label="Key 名称">
            <input className="input" value={keyName} onChange={(event) => setKeyName(event.target.value)} />
          </FormField>
          <FormField label="关联网关">
            <SelectField value={gatewayID} onChange={(event) => setGatewayID(event.target.value)}>
              {gateways.map((gateway) => <option value={gateway.id} key={gateway.id}>{gateway.name}</option>)}
            </SelectField>
          </FormField>
          <FormField label="月度限额">
            <input className="input" type="number" min={1} step={1} value={quotaMonth} onChange={(event) => setQuotaMonth(Number(event.target.value))} />
          </FormField>
          <FormField label="QPS">
            <input className="input" type="number" min={1} step={1} value={qpsLimit} onChange={(event) => setQpsLimit(Number(event.target.value))} />
          </FormField>
          <button className="btn btn-primary" disabled={saving || !canOperateWorkspace || !gatewayID || !isPositiveInteger(quotaMonth) || !isPositiveInteger(qpsLimit)} onClick={() => void submitKey()}>{saving ? "签发中..." : gatewayID ? "＋ 签发 API Key" : "先创建中转站"}</button>
        </div>
      </div>

      <div className="card card-pad key-create-panel">
        <div className="section-head" style={{ margin: "0 0 14px" }}>
          <h2 style={{ fontSize: 15 }}>{adminMode ? "邀请平台成员" : "团队协作成员（可选）"}</h2>
          <span className="sub">{adminMode ? "成员必须先注册并完成邮箱验证，才能加入平台工作区" : "个人使用可以先忽略；需要多人共用中转站时，成员需先注册并完成邮箱验证"}</span>
        </div>
        <div className="settings-grid-inline">
          <FormField label="成员邮箱">
            <input className="input" type="email" value={inviteEmail} onChange={(event) => setInviteEmail(event.target.value)} placeholder="teammate@company.com" />
          </FormField>
          <FormField label="角色">
            <SelectField value={inviteRole} onChange={(event) => setInviteRole(event.target.value)}>
              <option value="viewer">Viewer · 只读</option>
              <option value="operator">Operator · 可操作</option>
              <option value="admin">Admin · 可管理</option>
            </SelectField>
          </FormField>
          <FormField label="分组">
            <input className="input" value={inviteGroup} onChange={(event) => setInviteGroup(event.target.value)} />
          </FormField>
          <button className="btn btn-primary" disabled={saving || !canManageWorkspace || !inviteEmail} onClick={() => void inviteMember()}>＋ 邀请成员</button>
        </div>
      </div>

      <div className="tabs">
        <button className={`tab ${tab === "members" ? "active" : ""}`} onClick={() => setTab("members")}>工作区成员 · {workspaceMembers.length}</button>
        <button className={`tab ${tab === "users" ? "active" : ""}`} onClick={() => setTab("users")}>注册用户 · {users.length}</button>
        <button className={`tab ${tab === "keys" ? "active" : ""}`} onClick={() => setTab("keys")}>API Key · {keys.length}</button>
      </div>

      <FilterBar className="card member-list-filter">
        <input className="input" value={query} onChange={(event) => setQuery(event.target.value)} placeholder={tab === "keys" ? "搜索 Key / mask / 网关..." : "搜索成员 / 邮箱 / 分组..."} />
        {tab === "members" ? (
          <SelectField aria-label="成员角色筛选" value={roleFilter} onChange={(event) => setRoleFilter(event.target.value)}>
            <option value="all">所有角色</option>
            <option value="owner">Owner</option>
            <option value="admin">Admin</option>
            <option value="operator">Operator</option>
            <option value="viewer">Viewer</option>
          </SelectField>
        ) : null}
        {tab === "keys" ? (
          <>
            <SelectField aria-label="Key 状态筛选" value={keyStatusFilter} onChange={(event) => setKeyStatusFilter(event.target.value)}>
              <option value="all">所有状态</option>
              <option value="active">Active</option>
              <option value="expired">Expired</option>
              <option value="revoked">Revoked</option>
            </SelectField>
            <SelectField aria-label="Key 网关筛选" value={keyGatewayFilter} onChange={(event) => setKeyGatewayFilter(event.target.value)}>
              <option value="all">所有网关</option>
              {gateways.map((gateway) => <option value={gateway.id} key={gateway.id}>{gateway.name}</option>)}
            </SelectField>
          </>
        ) : null}
        <button className="btn btn-ghost btn-sm" onClick={() => { setQuery(""); setRoleFilter("all"); setKeyStatusFilter("all"); setKeyGatewayFilter("all"); }}>清空筛选</button>
      </FilterBar>

      {tab === "members" ? (
        <BulkActionBar className="bulk-toolbar member-bulk-toolbar">
          <label className="bulk-select">
            <input type="checkbox" checked={allVisibleMembersSelected} disabled={!canManageWorkspace} onChange={(event) => toggleVisibleMembers(event.target.checked)} />
            <span>已选 {selectedMemberIDs.length} 名成员</span>
          </label>
          <SelectField className="compact-select" value={bulkMemberRole} onChange={(event) => setBulkMemberRole(event.target.value)}>
            <option value="viewer">Viewer</option>
            <option value="operator">Operator</option>
            <option value="admin">Admin</option>
          </SelectField>
          <button className="btn btn-ghost btn-sm" disabled={!selectedMemberIDs.length || saving || !canManageWorkspace} onClick={() => void runBulkMembers("role")}>批量改角色</button>
          <button className="btn btn-ghost btn-sm" disabled={!selectedMemberIDs.length || saving || !canManageWorkspace} onClick={() => void runBulkMembers("delete")}>批量移除</button>
        </BulkActionBar>
      ) : null}

      {tab === "keys" ? (
        <BulkActionBar className="bulk-toolbar">
          <label className="bulk-select">
            <input type="checkbox" checked={allVisibleKeysSelected} disabled={!canOperateWorkspace} onChange={(event) => toggleVisibleKeys(event.target.checked)} />
            <span>已选 {selectedKeyIDs.length} 个 Key</span>
          </label>
          <SelectField className="compact-select" aria-label="批量 Key 状态" value={bulkKeyStatus} onChange={(event) => setBulkKeyStatus(event.target.value)}>
            <option value="active">Active</option>
            <option value="expired">Expired</option>
          </SelectField>
          <button className="btn btn-ghost btn-sm" disabled={!selectedKeyIDs.length || saving || !canOperateWorkspace} onClick={() => void runBulkKeys("status")}>批量改状态</button>
          <button className="btn btn-ghost btn-sm" disabled={!selectedKeyIDs.length || saving || !canOperateWorkspace} onClick={() => void runBulkKeys("revoke")}>批量吊销</button>
          <button className="btn btn-ghost btn-sm" disabled={!selectedKeyIDs.length || saving || !canOperateWorkspace} onClick={() => void runBulkKeys("delete")}>批量删除</button>
        </BulkActionBar>
      ) : null}

      {tab === "members" ? <MembersTable members={filteredMembers} loading={loading} selected={selectedMemberSet} canManage={canManageWorkspace} onSelect={toggleMemberSelection} onRoleChange={(member, role) => void changeMemberRole(member, role)} onGroupChange={(member, groupName) => void changeMemberGroup(member, groupName)} onRemove={(member) => void removeMember(member)} /> : null}
      {tab === "users" ? <UsersTable members={filteredUsers} loading={loading} adminMode={adminMode} /> : null}
      {tab === "keys" ? <KeysTable keys={filteredKeys} loading={loading} selected={selectedKeySet} canOperate={canOperateWorkspace} testStates={keyTests} onSelect={toggleKeySelection} onTestGateway={adminMode || !canOperateWorkspace ? undefined : (key) => void testKeyGateway(key)} onEdit={(key) => setEditingKey(key)} onRevoke={(key) => void revoke(key)} onDelete={(key) => void removeKey(key)} /> : null}
      <KeyEditor item={editingKey} saving={saving} onClose={() => setEditingKey(null)} onSubmit={saveKey} />
      <OneTimeKeyDialog keyItem={createdKey} open={createdKeyDialogOpen} onClose={closeCreatedKeyDialog} />
    </Shell>
  );
}

function MembersTable({ members, loading, selected, canManage, onSelect, onRoleChange, onGroupChange, onRemove }: { members: GatewayMember[]; loading: boolean; selected: Set<string>; canManage: boolean; onSelect: (userID: string, checked: boolean) => void; onRoleChange: (member: GatewayMember, role: string) => void; onGroupChange: (member: GatewayMember, groupName: string) => void; onRemove: (member: GatewayMember) => void }) {
  const maxRequests = Math.max(...members.map((item) => item.requestsToday), 1);
  const columns = useMemo<DataTableColumn<GatewayMember>[]>(() => [
    {
      id: "select",
      header: "选择",
      meta: { width: "50px", align: "center", className: "member-select-cell", headerClassName: "member-select-cell" },
      cell: ({ row }) => <input className="member-select-checkbox" type="checkbox" aria-label={`选择 ${row.original.email}`} disabled={row.original.role === "owner" || !canManage} checked={selected.has(row.original.userId)} onChange={(event) => onSelect(row.original.userId, event.target.checked)} />
    },
    { id: "member", header: "成员", meta: { width: "250px" }, cell: ({ row }) => <UserCell member={row.original} /> },
    {
      id: "role",
      header: "角色",
      meta: { width: "128px" },
      cell: ({ row }) => row.original.role === "owner" ? <span className="badge b-magenta member-role-badge">Owner</span> : (
        <SelectField className="compact-select member-role-select" aria-label={`角色 ${row.original.email}`} value={row.original.role} disabled={!canManage} onChange={(event) => onRoleChange(row.original, event.target.value)}>
          <option value="viewer">Viewer</option>
          <option value="operator">Operator</option>
          <option value="admin">Admin</option>
        </SelectField>
      )
    },
    { id: "group", header: "所属组", meta: { width: "180px" }, cell: ({ row }) => <MemberGroupInput member={row.original} disabled={!canManage} onGroupChange={onGroupChange} /> },
    { id: "usage", header: "今日用量", meta: { width: "160px" }, cell: ({ row }) => <UsageBar value={row.original.requestsToday} max={maxRequests} /> },
    { id: "lastActive", header: "最近活跃", meta: { width: "120px", className: "muted-time member-time-cell" }, cell: ({ row }) => timeLabel(row.original.lastActiveAt) },
    { id: "status", header: "状态", meta: { width: "94px" }, cell: ({ row }) => <span className={`badge ${row.original.status === "active" ? "b-green" : "b-gray"} dot member-status-badge`}>{row.original.status === "active" ? "活跃" : "休眠"}</span> },
    { id: "actions", header: "", meta: { width: "78px", align: "end", className: "member-action-cell" }, cell: ({ row }) => <button className="btn btn-ghost btn-sm member-remove-button" disabled={row.original.role === "owner" || !canManage} onClick={() => onRemove(row.original)}>移除</button> }
  ], [canManage, maxRequests, onGroupChange, onRemove, onRoleChange, onSelect, selected]);

  return (
    <DataTable
      data={members}
      columns={columns}
      loading={loading}
      pageSize={10}
      loadingText="正在加载成员..."
      footerNote="成员角色和分组变更会写入审计。"
      empty="暂无匹配成员。"
      cardClassName="member-access-board"
      tableClassName="member-access-table"
      wrapClassName="member-access-wrap"
    />
  );
}

function MemberGroupInput({ member, disabled, onGroupChange }: { member: GatewayMember; disabled: boolean; onGroupChange: (member: GatewayMember, groupName: string) => void }) {
  const [groupName, setGroupName] = useState(member.groupName);

  useEffect(() => {
    setGroupName(member.groupName);
  }, [member.groupName]);

  if (member.role === "owner") {
    const groupLabel = member.groupName || "个人工作区";
    return <span className="member-group-chip" title={groupLabel}>{groupLabel}</span>;
  }

  return (
    <input
      className="input compact-name member-group-input"
      aria-label={`分组 ${member.email}`}
      value={groupName}
      disabled={disabled}
      onChange={(event) => setGroupName(event.target.value)}
      onBlur={() => onGroupChange(member, groupName)}
    />
  );
}

function UsersTable({ members, loading, adminMode }: { members: GatewayMember[]; loading: boolean; adminMode: boolean }) {
  const columns = useMemo<DataTableColumn<GatewayMember>[]>(() => [
    { id: "user", header: "用户", cell: ({ row }) => <UserCell member={row.original} /> },
    { id: "role", header: "角色", cell: () => <span className="badge b-gray">注册用户</span> },
    { accessorKey: "requestsToday", header: "今日网关请求", meta: { className: "mono" } },
    { id: "lastActive", header: "最近活跃", meta: { className: "muted-time" }, cell: ({ row }) => timeLabel(row.original.lastActiveAt) },
    { id: "status", header: "状态", cell: ({ row }) => <span className={`badge ${row.original.status === "active" ? "b-green" : "b-gray"} dot`}>{row.original.status === "active" ? "活跃" : "休眠"}</span> },
    { id: "actions", header: "", cell: ({ row }) => adminMode ? <a className="row-act" href={`/admin/users?q=${encodeURIComponent(row.original.email)}`}>查看</a> : <span className="muted-time">-</span> }
  ], [adminMode]);

  return <DataTable data={members} columns={columns} loading={loading} pageSize={10} loadingText="正在加载用户..." footerNote="注册用户不能读取网关 Key，只有管理员可签发和吊销。" empty="暂无普通注册用户。" />;
}

function KeysTable({ keys, loading, selected, canOperate, testStates, onSelect, onTestGateway, onEdit, onRevoke, onDelete }: { keys: GatewayKey[]; loading: boolean; selected: Set<string>; canOperate: boolean; testStates: Record<string, KeyTestState>; onSelect: (keyID: string, checked: boolean) => void; onTestGateway?: (key: GatewayKey) => void; onEdit: (key: GatewayKey) => void; onRevoke: (key: GatewayKey) => void; onDelete: (key: GatewayKey) => void }) {
  const columns = useMemo<DataTableColumn<GatewayKey>[]>(() => [
    {
      id: "select",
      header: "选择",
      meta: { width: "42px", align: "center", className: "key-select-cell", headerClassName: "key-select-cell" },
      cell: ({ row }) => <input className="key-select-checkbox" type="checkbox" aria-label={`选择 ${row.original.name}`} checked={selected.has(row.original.id)} disabled={!canOperate} onChange={(event) => onSelect(row.original.id, event.target.checked)} />
    },
    { id: "identity", header: "名称 / 网关", meta: { width: "310px" }, cell: ({ row }) => <KeyIdentityCell item={row.original} testState={testStates[row.original.id]} onTest={onTestGateway ? () => onTestGateway(row.original) : undefined} /> },
    { id: "key", header: "密钥", meta: { width: "230px" }, cell: ({ row }) => <KeyMaskCell item={row.original} /> },
    { id: "quota", header: "月度限额", meta: { width: "95px", className: "mono key-quota-cell" }, cell: ({ row }) => `${row.original.quotaMonth} / 月` },
    { accessorKey: "qpsLimit", header: "QPS", meta: { width: "54px", className: "mono key-qps-cell" } },
    { id: "used", header: "已用", meta: { width: "100px" }, cell: ({ row }) => <KeyUsageCell item={row.original} /> },
    { id: "status", header: "状态", meta: { width: "82px" }, cell: ({ row }) => <span className={`badge ${keyStatusClass(row.original.status)} dot key-status-badge`}>{keyStatusText(row.original.status)}</span> },
    {
      id: "actions",
      header: "",
      meta: { width: "150px", align: "end", className: "key-action-cell" },
      cell: ({ row }) => (
        <div className="table-actions key-table-actions">
          <button className="btn btn-ghost btn-sm" disabled={!canOperate} onClick={() => onEdit(row.original)}>编辑</button>
          <button className="btn btn-ghost btn-sm" disabled={row.original.status === "revoked" || !canOperate} onClick={() => onRevoke(row.original)}>吊销</button>
          <button className="btn btn-ghost btn-sm" disabled={!canOperate} onClick={() => onDelete(row.original)}>删除</button>
        </div>
      )
    }
  ], [canOperate, onDelete, onEdit, onRevoke, onSelect, onTestGateway, selected, testStates]);

  return (
    <DataTable
      data={keys}
      columns={columns}
      loading={loading}
      pageSize={10}
      loadingText="正在加载 Key..."
      footerNote="完整 Key 仅在创建时展示一次。忘记后请轮换或重新签发。"
      empty="暂无匹配 Key。"
      cardClassName="key-access-board"
      tableClassName="key-access-table"
      wrapClassName="key-access-wrap"
    />
  );
}

function KeyIdentityCell({ item, testState, onTest }: { item: GatewayKey; testState?: KeyTestState; onTest?: () => void }) {
  const running = testState?.status === "running";
  return (
    <div className="key-identity-cell">
      <div className="key-name-row">
        <b className="key-name-cell" title={item.name}>{item.name}</b>
        {onTest ? (
          <button type="button" className="key-test-button" title="测试网关连通性和响应速度" disabled={running} onClick={onTest}>
            {running ? "测试中..." : "测试"}
          </button>
        ) : null}
      </div>
      <div className="key-gateway-line">
        <span className="key-gateway-label">网关</span>
        <span className="key-gateway-name" title={item.gatewayName}>{item.gatewayName}</span>
      </div>
      {testState ? <KeyGatewayTestResult state={testState} /> : null}
    </div>
  );
}

function KeyGatewayTestResult({ state }: { state: KeyTestState }) {
  if (state.status === "running") {
    return <div className="key-test-result running" aria-live="polite"><span className="key-test-dot" />正在测试网关...</div>;
  }
  const result = state.result;
  if (state.status === "ok" && result) {
    return (
      <div className="key-test-result ok" aria-live="polite" title={result.preview || result.message}>
        <span className="key-test-dot" />
        可用 · 延迟 {result.latencyMs}ms · {result.upstream || "默认上游"} · Token {result.tokens}
      </div>
    );
  }
  const failureText = result ? `不可用 · ${result.statusCode || "-"} · 延迟 ${result.latencyMs}ms · ${result.errorType || result.message}` : `不可用 · ${state.error || "测试失败"}`;
  return <div className="key-test-result failed" aria-live="polite" title={failureText}><span className="key-test-dot" />{failureText}</div>;
}

function KeyUsageCell({ item }: { item: GatewayKey }) {
  const pct = Math.round((item.requestsUsed / Math.max(item.quotaMonth, 1)) * 100);
  return (
    <div className="key-usage-cell" aria-label={`已用 ${item.requestsUsed}`}>
      <div className={`key-usage-meter ${pct >= 90 ? "red" : pct >= 70 ? "warn" : ""}`}><i style={{ width: `${Math.min(pct, 100)}%` }} /></div>
      <span className="mono key-usage-value">{item.requestsUsed}</span>
    </div>
  );
}

function KeyMaskCell({ item }: { item: GatewayKey }) {
  return (
    <div className="key-mask-cell" title="完整 Key 仅在创建时展示一次。">
      <span className="keyval">{item.keyMask}<span className="rev">hash</span></span>
      <div className="key-mask-meta">
        <small>仅创建时展示，忘记请轮换</small>
      </div>
    </div>
  );
}

function OneTimeKeyDialog({ keyItem, open, onClose }: { keyItem: GatewayKey | null; open: boolean; onClose: () => void }) {
  if (!keyItem?.plainKey || !open) return null;
  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => { if (!nextOpen) onClose(); }}
      title="API Key 已生成"
      description="这是完整密钥。请先复制保存；关闭后无法再次取回。"
      footer={<button type="button" className="btn btn-primary" onClick={onClose}>我已保存</button>}
    >
      <span className="badge b-green dot">只保存哈希</span>
      <div className="one-time-key-row modal-row">
        <span className="one-time-key-label">API_KEY</span>
        <code className="one-time-key-value">{keyItem.plainKey}</code>
        <CopyButton text={keyItem.plainKey} label="复制完整 Key" />
      </div>
      <div className="one-time-key-dialog-note">
        列表只显示 mask；关闭后无法取回完整 Key，需要时请轮换或重新签发。
      </div>
    </Dialog>
  );
}

function KeyEditor({ item, saving, onClose, onSubmit }: { item: GatewayKey | null; saving: boolean; onClose: () => void; onSubmit: (keyID: string, input: { name: string; quotaMonth: number; qpsLimit: number; status: string }) => Promise<void> }) {
  const [name, setName] = useState("");
  const [quotaMonth, setQuotaMonth] = useState(defaultGatewayKeyQuotaMonth);
  const [qpsLimit, setQpsLimit] = useState(defaultGatewayKeyQPSLimit);
  const [status, setStatus] = useState("active");
  const open = Boolean(item);

  useEffect(() => {
    if (!item) return;
    setName(item.name);
    setQuotaMonth(item.quotaMonth);
    setQpsLimit(item.qpsLimit);
    setStatus(item.status);
  }, [item]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!item) return;
    await onSubmit(item.id, { name, quotaMonth, qpsLimit, status });
  }

  return (
    <div className={`drawer-mask ${open ? "open" : ""}`} onClick={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <form className={`drawer ${open ? "open" : ""} private-editor`} onSubmit={(event) => void submit(event)}>
        <div className="dh">
          <div><h3>编辑 Gateway Key</h3><p>只修改元信息、限额和状态，不回显密钥明文。</p></div>
          <button type="button" className="icon-btn" onClick={onClose}>×</button>
        </div>
        <div className="db form-grid">
          <label><span>名称</span><input className="input" value={name} disabled={item?.status === "revoked"} onChange={(event) => setName(event.target.value)} required /></label>
          <label><span>月度限额</span><input className="input" type="number" min={1} value={quotaMonth} disabled={item?.status === "revoked"} onChange={(event) => setQuotaMonth(Number(event.target.value))} /></label>
          <label><span>QPS</span><input className="input" type="number" min={1} value={qpsLimit} disabled={item?.status === "revoked"} onChange={(event) => setQpsLimit(Number(event.target.value))} /></label>
          <label><span>状态</span><SelectField aria-label="Key 编辑状态" value={status} disabled={item?.status === "revoked"} onChange={(event) => setStatus(event.target.value)}><option value="active">Active</option><option value="expired">Expired</option>{item?.status === "revoked" ? <option value="revoked">Revoked</option> : null}</SelectField></label>
        </div>
        <div className="df">
          <button type="button" className="btn btn-ghost btn-sm" onClick={onClose}>取消</button>
          <button type="submit" className="btn btn-primary btn-sm" disabled={saving || item?.status === "revoked" || !isPositiveInteger(quotaMonth) || !isPositiveInteger(qpsLimit)}>{saving ? "保存中..." : "保存 Key"}</button>
        </div>
      </form>
    </div>
  );
}

function UserCell({ member }: { member: GatewayMember }) {
  return (
    <div className="u-cell member-user-cell">
      <span className="av">{member.avatar || member.name[0] || "U"}</span>
      <span className="nm">{member.name}<small>{member.email}</small></span>
    </div>
  );
}

function UsageBar({ value, max }: { value: number; max: number }) {
  const pct = Math.round((value / Math.max(max, 1)) * 100);
  return (
    <div className="member-usage-cell" aria-label={`今日用量 ${value}`}>
      <div className="member-usage-meter"><i style={{ width: `${Math.min(pct, 100)}%` }} /></div>
      <span className="mono member-usage-value">{value}</span>
    </div>
  );
}

function CopyButton({ text, label = "复制" }: { text: string; label?: string }) {
  const [copied, setCopied] = useState(false);
  return <button type="button" className="copy" onClick={() => { void copyText(text); setCopied(true); setTimeout(() => setCopied(false), 1200); }}>{copied ? "已复制 ✓" : label}</button>;
}

async function copyText(value: string) {
  if (!value) return;
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(value);
      return;
    }
  } catch {
    // Fall back to the textarea path below.
  }
  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.setAttribute("readonly", "true");
  textarea.style.position = "fixed";
  textarea.style.left = "-9999px";
  document.body.appendChild(textarea);
  textarea.select();
  document.execCommand("copy");
  textarea.remove();
}

function roleText(role: string) {
  if (role === "owner") return "管理员";
  if (role === "admin") return "管理员";
  if (role === "operator") return "开发者";
  return role === "user" ? "注册用户" : "只读";
}

function keyStatusText(status: string) {
  if (status === "active") return "启用";
  if (status === "expired") return "已过期";
  if (status === "revoked") return "已吊销";
  return status;
}

function keyStatusClass(status: string) {
  if (status === "active") return "b-green";
  if (status === "expired") return "b-gray";
  return "b-red";
}

function isPositiveInteger(value: number) {
  return Number.isFinite(value) && Number.isInteger(value) && value > 0;
}

function timeLabel(value?: string) {
  if (!value) return "未活跃";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "未活跃" : date.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}
