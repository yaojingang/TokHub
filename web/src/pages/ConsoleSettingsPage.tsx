import { useEffect, useState } from "react";
import { ConsoleShell } from "../components/ConsoleShell";
import { consoleSettings, currentUser, resetConsoleSettings, revokeOtherSessions, updateConsoleSettings, updateMeProfile, User, WorkspaceSettings, workspaceCanManage } from "../lib/api";
import { SelectField } from "../ui";

export function ConsoleSettingsPage() {
  const [workspace, setWorkspace] = useState<WorkspaceSettings | null>(null);
  const [draft, setDraft] = useState<WorkspaceSettings | null>(null);
  const [profile, setProfile] = useState<User | null>(null);
  const [profileName, setProfileName] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [savingProfile, setSavingProfile] = useState(false);
  const [revokingSessions, setRevokingSessions] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  useEffect(() => {
    void load();
  }, []);

  async function load() {
    setLoading(true);
    setError("");
    try {
      const [settingsPayload, me] = await Promise.all([consoleSettings(), currentUser({ force: true })]);
      setWorkspace(settingsPayload.workspace);
      setDraft(settingsPayload.workspace);
      setProfile(me);
      setProfileName(me?.name || me?.email.split("@")[0] || "");
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载工作区设置失败");
    } finally {
      setLoading(false);
    }
  }

  async function save() {
    if (!draft) return;
    if (!workspaceCanManage(workspace?.role)) {
      setError("当前工作区角色为只读，不能修改工作区设置。");
      return;
    }
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const payload = await updateConsoleSettings({
        name: draft.name,
        timezone: draft.timezone,
        defaultGatewayPolicy: draft.defaultGatewayPolicy,
        defaultNotificationChannelId: draft.defaultNotificationChannelId
      });
      setWorkspace(payload.workspace);
      setDraft(payload.workspace);
      setNotice("工作区设置已保存。网关向导会默认使用新的策略。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存工作区设置失败");
    } finally {
      setSaving(false);
    }
  }

  async function restoreDefaults() {
    if (!workspaceCanManage(workspace?.role)) {
      setError("当前工作区角色为只读，不能恢复工作区默认设置。");
      return;
    }
    if (!window.confirm("确认恢复工作区默认设置？不会删除成员、通道、网关或 Key。")) return;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const payload = await resetConsoleSettings();
      setWorkspace(payload.workspace);
      setDraft(payload.workspace);
      setNotice("工作区设置已恢复默认。成员、通道、网关和 Key 均未变更。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "恢复默认设置失败");
    } finally {
      setSaving(false);
    }
  }

  async function saveProfile() {
    const name = profileName.trim();
    if (!name) {
      setError("请输入显示名称");
      return;
    }
    setSavingProfile(true);
    setError("");
    setNotice("");
    try {
      const next = await updateMeProfile({ name });
      setProfile(next);
      setProfileName(next.name);
      setNotice("个人显示名称已保存，控制台顶部、侧栏和首页标题会同步更新。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存个人资料失败");
    } finally {
      setSavingProfile(false);
    }
  }

  async function revokeSessions() {
    if (!window.confirm("确认撤销当前账号在其他设备上的会话？当前浏览器会保持登录。")) return;
    setRevokingSessions(true);
    setError("");
    setNotice("");
    try {
      await revokeOtherSessions();
      setNotice("其他设备上的登录会话已撤销，当前浏览器会话保持有效。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "撤销其他会话失败");
    } finally {
      setRevokingSessions(false);
    }
  }

  const channels = draft?.notificationChannels ?? [];
  const profileInitial = profile?.avatar || firstMark(profileName || profile?.email || "U");
  const canManageWorkspace = workspaceCanManage(workspace?.role);

  return (
    <ConsoleShell title="工作区设置" crumb="/ 工作区 / 设置">
      <div className="admin-actions-line">
        <p className="page-intro">这里管理个人显示名称和当前工作区配置。个人名称会同步到控制台顶栏、侧栏和控制台首页</p>
        <div className="admin-actions-buttons">
          <button className="btn btn-ghost btn-sm" onClick={() => { setDraft(workspace); setNotice(""); setError(""); }} disabled={loading || saving}>放弃更改</button>
          <button className="btn btn-ghost btn-sm" onClick={() => void restoreDefaults()} disabled={loading || saving || !workspace || !canManageWorkspace}>恢复默认</button>
          <button className="btn btn-primary btn-sm" onClick={() => void save()} disabled={loading || saving || !draft || !canManageWorkspace}>{saving ? "保存中..." : "保存设置"}</button>
        </div>
      </div>

      {error ? <div className="form-error">{error}</div> : null}
      {notice ? <div className="form-notice">{notice}</div> : null}

      <section className="card set-card console-profile-card" id="profile">
        <div className="set-h">个人设置</div>
        <div className="ops-pad console-profile-grid">
          <div className="console-profile-preview">
            <span className="console-profile-avatar">{profileInitial}</span>
            <div>
              <b>{profileName.trim() || profile?.email || "用户"}</b>
              <span>{profile?.email || "登录邮箱"}</span>
            </div>
          </div>
          <div className="console-profile-form">
            <label className="field">
              <span>显示名称</span>
              <input className="input" value={profileName} onChange={(event) => setProfileName(event.target.value)} placeholder="例如：姚金刚" maxLength={40} />
            </label>
            <label className="field">
              <span>登录邮箱</span>
              <input className="input" value={profile?.email ?? ""} readOnly aria-readonly="true" />
            </label>
            <div className="console-profile-actions">
              <div className="module-sub">登录邮箱用于账号识别，显示名称可以随时修改。</div>
              <button className="btn btn-ghost btn-sm" type="button" onClick={() => void revokeSessions()} disabled={loading || revokingSessions}>{revokingSessions ? "撤销中..." : "撤销其他会话"}</button>
              <button className="btn btn-primary btn-sm" type="button" onClick={() => void saveProfile()} disabled={loading || savingProfile}>{savingProfile ? "保存中..." : "保存个人设置"}</button>
            </div>
          </div>
        </div>
      </section>

      <div className="stat-row console-settings-stat-row">
        <Stat label="成员" value={workspace?.members ?? 0} hint="当前工作区 active 成员" />
        <Stat label="私有通道" value={workspace?.privateChannels ?? 0} hint="只归属当前工作区" />
        <Stat label="专属网关" value={workspace?.gateways ?? 0} hint="OpenAI 兼容入口" />
        <Stat label="有效 Key" value={workspace?.activeKeys ?? 0} hint="可调用网关的 Key" />
        <Stat label="通知渠道" value={channels.length} hint="告警和测试发送目标" />
      </div>

      <div className="grid cols-2">
        <section className="card set-card">
          <div className="set-h">基础信息</div>
          <div className="ops-pad">
            <label className="field">
              <span>工作区名称</span>
              <input className="input" value={draft?.name ?? ""} disabled={!canManageWorkspace} onChange={(event) => draft && setDraft({ ...draft, name: event.target.value })} placeholder="例如：研发团队工作区" />
            </label>
            <label className="field">
              <span>时区</span>
              <SelectField className="console-settings-select" value={draft?.timezone ?? "Asia/Shanghai"} disabled={!canManageWorkspace} onChange={(event) => draft && setDraft({ ...draft, timezone: event.target.value })}>
                <option value="Asia/Shanghai">Asia/Shanghai</option>
                <option value="UTC">UTC</option>
                <option value="America/Los_Angeles">America/Los_Angeles</option>
              </SelectField>
            </label>
            <div className="module-sub">工作区名称会显示在控制台顶栏和成员协作场景，不影响平台公开前台。</div>
          </div>
        </section>

        <section className="card set-card">
          <div className="set-h">默认通知渠道</div>
          <div className="ops-pad">
            <label className="field">
              <span>告警默认发送到</span>
              <SelectField className="console-settings-select" value={draft?.defaultNotificationChannelId ?? ""} disabled={!canManageWorkspace} onChange={(event) => draft && setDraft({ ...draft, defaultNotificationChannelId: event.target.value })}>
                <option value="">不指定默认渠道</option>
                {channels.map((channel) => <option value={channel.id} key={channel.id}>{channel.name} · {channel.type}</option>)}
              </SelectField>
            </label>
            <div className="module-sub">通知渠道在“告警规则”页面创建和测试；这里仅选择工作区默认渠道。</div>
          </div>
        </section>
      </div>
    </ConsoleShell>
  );
}

function Stat({ label, value, hint }: { label: string; value: number; hint: string }) {
  return <div className="stat"><div className="l">{label}</div><div className="v">{value}</div><div className="d">{hint}</div></div>;
}

function firstMark(value: string) {
  return (Array.from(value.trim())[0] || "U").toUpperCase();
}
