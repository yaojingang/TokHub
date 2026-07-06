import { useEffect, useState } from "react";
import { AdminShell } from "../components/AdminShell";
import {
  AdminAgentScope,
  AdminAgentToken,
  AdminSettingsSummary,
  MonitorModelConfig,
  SiteConfig,
  User,
  adminAgentTokens,
  adminSettings,
  createAdminAgentToken,
  currentUser,
  revokeAdminAgentToken,
  revokeOtherSessions,
  updateAdminSettings
} from "../lib/api";
import { adminPath, getAdminPath, normalizeAdminPath, setAdminPath, validateAdminPathInput } from "../lib/adminPath";

type SettingsTab = "org" | "monitor" | "bill" | "sec" | "intg";

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

export function AdminSettingsPage() {
  const [tab, setTab] = useState<SettingsTab>("org");
  const [site, setSite] = useState<SiteConfig | null>(null);
  const [draft, setDraft] = useState<SiteConfig | null>(null);
  const [summary, setSummary] = useState<AdminSettingsSummary | null>(null);
  const [viewer, setViewer] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
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
      const [payload, me] = await Promise.all([adminSettings(), currentUser({ force: true })]);
      setSite(payload.site);
      setDraft(payload.site);
      setSummary(payload.summary);
      setViewer(me);
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载设置失败");
    } finally {
      setLoading(false);
    }
  }

  async function save() {
    if (!draft) return;
    const validation = validateSettingsDraft(draft);
    if (validation) {
      setError(validation);
      setNotice("");
      return;
    }
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const previousAdminPath = getAdminPath();
      const payload = await updateAdminSettings({
        registrationOpen: draft.registrationOpen,
        showRegisterCta: draft.showRegisterCta,
        emailVerificationRequired: draft.emailVerificationRequired,
        adminPath: normalizeAdminPath(draft.adminPath),
        brandName: draft.brandName,
        logoMark: draft.logoMark,
        subtitle: draft.subtitle,
        footerText: draft.footerText,
        defaultGatewayPolicy: draft.defaultGatewayPolicy,
        timezone: draft.timezone,
        monitorModels: draft.monitorModels,
        ...(viewer?.role === "owner" ? { analyticsCode: draft.analyticsCode } : {})
      });
      setSite(payload.site);
      setDraft(payload.site);
      setAdminPath(payload.site.adminPath);
      if (previousAdminPath !== getAdminPath()) {
        window.location.href = adminPath("/settings");
        return;
      }
      setNotice("设置已保存。后台入口、注册入口、登录页、前台 CTA 和全站统计代码会同步更新。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存设置失败");
    } finally {
      setSaving(false);
    }
  }

  function reset() {
    setDraft(site);
    setError("");
    setNotice("");
  }

  function restoreBrandDefaults() {
    if (!draft) return;
    setDraft({
      ...draft,
      brandName: "TokHub",
      logoMark: "T",
      subtitle: "API 中转站监控",
      footerText: "TokHub API 中转站监控与企业网关"
    });
    setError("");
    setNotice("已恢复默认品牌文案，点击保存后生效。");
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

  const platformOrg = summary?.platformOrg;
  const platformOrgLink = adminPath(`/orgs${platformOrg?.slug ? `?q=${encodeURIComponent(platformOrg.slug)}` : ""}`);

  return (
    <AdminShell title="系统设置" crumb="/ 系统">
      {error ? <div className="form-error">{error}</div> : null}
      {notice ? <div className="form-notice">{notice}</div> : null}

      <div className="settings-top-actions">
        <div className="tabs">
          <button className={`tab ${tab === "org" ? "active" : ""}`} onClick={() => setTab("org")}>组织</button>
          <button className={`tab ${tab === "monitor" ? "active" : ""}`} onClick={() => setTab("monitor")}>监控</button>
          <button className={`tab ${tab === "bill" ? "active" : ""}`} onClick={() => setTab("bill")}>计费</button>
          <button className={`tab ${tab === "sec" ? "active" : ""}`} onClick={() => setTab("sec")}>安全</button>
          <button className={`tab ${tab === "intg" ? "active" : ""}`} onClick={() => setTab("intg")}>集成</button>
        </div>
        <div className="settings-save">
          <button className="btn btn-ghost btn-sm" onClick={reset} disabled={loading || saving}>放弃更改</button>
          <button className="btn btn-ghost btn-sm" onClick={restoreBrandDefaults} disabled={loading || saving}>恢复品牌默认</button>
          <button className="btn btn-primary btn-sm" onClick={() => void save()} disabled={loading || saving}>{saving ? "保存中..." : "保存更改"}</button>
        </div>
      </div>

      {tab === "org" && draft ? (
        <div className="set-pane">
          <div className="card set-card">
            <div className="set-h">⚙ 组织信息</div>
            <div className="set-row">
              <div className="lbl"><b>组织名称</b><small>展示在控制台与账单</small></div>
              <div className="ctl"><span className="keyval"><b>{platformOrg?.name ?? "加载中"}</b></span><a className="btn btn-ghost btn-sm" href={platformOrgLink}>编辑组织资料</a></div>
            </div>
            <div className="set-row">
              <div className="lbl"><b>组织标识 Slug</b><small>用于网关域名前缀</small></div>
              <div className="ctl"><span className="keyval mono">{platformOrg?.slug ?? "-"}</span></div>
            </div>
            <div className="set-row">
              <div className="lbl"><b>平台组织状态</b><small>套餐、状态和时区来自组织管理，不再使用设置页硬编码</small></div>
              <div className="ctl">
                <span className="keyval">{platformOrg?.plan ?? "-"} / {platformOrg?.status ?? "-"} / {platformOrg?.timezone ?? "-"}</span>
                <a className="btn btn-ghost btn-sm" href={adminPath("/orgs")}>进入组织管理</a>
              </div>
            </div>
            <div className="set-row">
              <div className="lbl"><b>默认路由策略</b><small>新建网关时的默认值</small></div>
              <div className="ctl">
                <div className="tb-select">
                  <select
                    aria-label="默认路由策略"
                    value={draft.defaultGatewayPolicy ?? "latency"}
                    onChange={(event) => setDraft({ ...draft, defaultGatewayPolicy: event.target.value as SiteConfig["defaultGatewayPolicy"] })}
                  >
                    <option value="latency">最低延迟优先</option>
                    <option value="success">最高成功率</option>
                    <option value="cost">成本优先</option>
                  </select>
                </div>
              </div>
            </div>
            <div className="set-row">
              <div className="lbl"><b>时区</b><small>影响报表与账单出账</small></div>
              <div className="ctl">
                <div className="tb-select">
                  <select
                    aria-label="平台时区"
                    value={draft.timezone ?? "Asia/Shanghai"}
                    onChange={(event) => setDraft({ ...draft, timezone: event.target.value })}
                  >
                    <option value="Asia/Shanghai">(GMT+8) 北京</option>
                    <option value="UTC">(GMT+0) UTC</option>
                    <option value="America/Los_Angeles">(GMT-8) 太平洋</option>
                  </select>
                </div>
              </div>
            </div>
          </div>

          <div className="card set-card">
            <div className="set-h">⛬ 平台访问与注册</div>
            <div className="set-row">
              <div className="lbl"><b>后台管理员地址</b><small>管理端页面前缀，建议使用不易猜测但便于团队记忆的站内路径；API 地址仍保持 /api/admin</small></div>
              <div className="ctl">
                <input
                  className="input mono"
                  style={{ maxWidth: 260 }}
                  aria-label="后台管理员地址"
                  value={draft.adminPath || "/admin"}
                  onChange={(event) => setDraft({ ...draft, adminPath: event.target.value })}
                  placeholder="/admin"
                />
                <span className="reg-switch-hint">保存后入口为 {normalizeAdminPath(draft.adminPath || "/admin")}</span>
              </div>
            </div>
            <div className="set-row">
              <div className="lbl"><b>开放公共注册</b><small>允许访客在登录页注册账号、订阅平台通道、添加自己的私有通道。关闭后前台仅显示只读监控信息</small></div>
              <div className="ctl">
                <button
                  className={`switch ${draft.registrationOpen ? "on" : ""}`}
                  aria-label="开放公共注册"
                  onClick={() => setDraft({ ...draft, registrationOpen: !draft.registrationOpen })}
                />
                <span className="reg-switch-hint">
                  {draft.registrationOpen ? "当前开放注册，访客可在登录页直接注册" : "当前关闭注册，前台收藏 / 注册入口已禁用"}
                </span>
              </div>
            </div>
            <div className="set-row">
              <div className="lbl"><b>显示注册入口</b><small>控制前台导航、推荐页 CTA 是否展示注册文案；公共注册关闭时后端仍会拒绝注册请求</small></div>
              <div className="ctl">
                <button
                  className={`switch ${draft.showRegisterCta ? "on" : ""}`}
                  aria-label="显示注册入口"
                  onClick={() => setDraft({ ...draft, showRegisterCta: !draft.showRegisterCta })}
                />
                <span className="reg-switch-hint">
                  {draft.showRegisterCta ? "前台显示登录 / 注册入口" : "前台只保留登录入口"}
                </span>
              </div>
            </div>
            <div className="set-row">
              <div className="lbl"><b>注册用户管理</b><small>新增、编辑、禁用、批量治理用户在用户管理页面完成</small></div>
              <div className="ctl"><a className="btn btn-ghost btn-sm" href={adminPath("/users")}>进入用户管理</a></div>
            </div>
            <div className="set-row">
              <div className="lbl"><b>私有通道总数</b><small>所有注册用户添加的私有通道汇总</small></div>
              <div className="ctl"><span className="keyval"><b>{summary?.privateChannels ?? 0}</b> 个</span><a className="btn btn-ghost btn-sm" href={adminPath("/channels")}>去通道接入查看</a></div>
            </div>
            <div className="set-row">
              <div className="lbl"><b>注册需邮箱验证</b><small>默认关闭。开启前需要先配置 SMTP/邮件服务，否则新用户会被阻塞在验证步骤</small></div>
              <div className="ctl">
                <button
                  className={`switch ${draft.emailVerificationRequired ? "on" : ""}`}
                  aria-label="注册需邮箱验证"
                  onClick={() => setDraft({ ...draft, emailVerificationRequired: !draft.emailVerificationRequired })}
                />
                <span className="reg-switch-hint">
                  {draft.emailVerificationRequired ? "已开启，注册后必须验证邮箱" : "已关闭，注册后直接进入用户控制台"}
                </span>
              </div>
            </div>
          </div>

          <div className="card set-card">
            <div className="set-h">© 站点底部 / 版权声明</div>
            <div className="set-row">
              <div className="lbl"><b>品牌名称</b><small>显示在公开页面和页脚</small></div>
              <div className="ctl"><input className="input" style={{ maxWidth: 320 }} value={draft.brandName} onChange={(event) => setDraft({ ...draft, brandName: event.target.value })} /></div>
            </div>
            <div className="set-row">
              <div className="lbl"><b>Logo 标记</b><small>公开页面左上角方形图标</small></div>
              <div className="ctl"><input className="input" style={{ maxWidth: 90 }} maxLength={2} value={draft.logoMark} onChange={(event) => setDraft({ ...draft, logoMark: event.target.value })} /></div>
            </div>
            <div className="set-row">
              <div className="lbl"><b>品牌副标题</b><small>Logo 下方的小字</small></div>
              <div className="ctl"><input className="input" style={{ maxWidth: 320 }} value={draft.subtitle} onChange={(event) => setDraft({ ...draft, subtitle: event.target.value })} /></div>
            </div>
            <div className="set-row">
              <div className="lbl"><b>公共访问地址</b><small>由部署环境变量控制，用于 CORS 与链接生成</small></div>
              <div className="ctl"><span className="keyval">{draft.publicUrl}</span></div>
            </div>
            <div className="set-row">
              <div className="lbl"><b>版权声明</b><small>显示在底部右侧</small></div>
              <div className="ctl"><input className="input" style={{ maxWidth: 560 }} value={draft.footerText} onChange={(event) => setDraft({ ...draft, footerText: event.target.value })} /></div>
            </div>
          </div>
        </div>
      ) : null}

      {tab === "monitor" && draft ? <MonitorSettings draft={draft} setDraft={setDraft} /> : null}
      {tab === "bill" ? <BillingSettings summary={summary} /> : null}
      {tab === "sec" ? (
        <SecuritySettings
          summary={summary}
          registrationOpen={draft?.registrationOpen ?? false}
          emailVerificationRequired={draft?.emailVerificationRequired ?? false}
          viewer={viewer}
          revokingSessions={revokingSessions}
          onRevokeSessions={() => void revokeSessions()}
        />
      ) : null}
      {tab === "intg" && draft ? <IntegrationSettings summary={summary} draft={draft} setDraft={setDraft} canManageAnalytics={viewer?.role === "owner"} /> : null}
    </AdminShell>
  );
}

function MonitorSettings({ draft, setDraft }: { draft: SiteConfig; setDraft: (site: SiteConfig) => void }) {
  const models = draft.monitorModels?.length ? draft.monitorModels : defaultMonitorModels;
  const enabledCount = models.filter((item) => item.enabled).length;
  const defaultCount = models.filter((item) => item.enabled && item.defaultSelected).length;

  function updateModel(index: number, patch: Partial<MonitorModelConfig>) {
    setDraft({
      ...draft,
      monitorModels: models.map((item, itemIndex) => (itemIndex === index ? { ...item, ...patch } : item))
    });
  }

  function addModel() {
    const index = models.length + 1;
    setDraft({
      ...draft,
      monitorModels: [
        ...models,
        {
          key: `custom-monitor-${index}`,
          label: `自定义模型 ${index}`,
          model: "",
          upstreamModel: "",
          type: "openai-compatible",
          enabled: true,
          defaultSelected: false,
          inputPerMtok: 0,
          outputPerMtok: 0,
          aliases: []
        }
      ]
    });
  }

  function removeModel(index: number) {
    if (models.length <= 1) return;
    setDraft({ ...draft, monitorModels: models.filter((_, itemIndex) => itemIndex !== index) });
  }

  function restoreMonitorDefaults() {
    setDraft({ ...draft, monitorModels: defaultMonitorModels.map((item) => ({ ...item, aliases: [...item.aliases] })) });
  }

  return (
    <div className="set-pane">
      <div className="stat-row">
        <SettingsStat label="监控模型" value={String(models.length)} hint="system defaults" />
        <SettingsStat label="已启用" value={String(enabledCount)} hint="新增通道可选择" />
        <SettingsStat label="默认勾选" value={String(defaultCount)} hint="新增通道预选" />
        <SettingsStat label="探测节流" value="串行" hint="错开测试通道" />
      </div>
      <div className="card set-card">
        <div className="set-h monitor-settings-head">
          <span>默认监控模型</span>
          <div className="monitor-settings-actions">
            <button className="btn btn-ghost btn-sm" type="button" onClick={restoreMonitorDefaults}>恢复默认模型</button>
            <button className="btn btn-primary btn-sm" type="button" onClick={addModel}>新增模型</button>
          </div>
        </div>
        <div className="monitor-model-list">
          {models.map((model, index) => (
            <div className="monitor-model-row" key={`${model.key || model.model || "monitor-model"}:${index}`}>
              <div className="monitor-model-head">
                <div>
                  <b>{model.label || model.model || `监控模型 ${index + 1}`}</b>
                  <small className="mono">{model.model || "未填写模型 ID"} · {model.type || "openai-compatible"}</small>
                </div>
                <div className="monitor-model-toggles">
                  <label><input type="checkbox" checked={model.enabled} onChange={(event) => updateModel(index, { enabled: event.target.checked })} /> 启用</label>
                  <label><input type="checkbox" checked={model.defaultSelected} onChange={(event) => updateModel(index, { defaultSelected: event.target.checked })} /> 默认勾选</label>
                  <button className="btn btn-ghost btn-sm danger-lite" type="button" onClick={() => removeModel(index)} disabled={models.length <= 1}>删除</button>
                </div>
              </div>
              <div className="monitor-model-grid">
                <label className="field">
                  <span>显示名称</span>
                  <input className="input" value={model.label} onChange={(event) => updateModel(index, { label: event.target.value })} placeholder="GPT-5.5" />
                </label>
                <label className="field">
                  <span>模型 ID</span>
                  <input
                    className="input mono"
                    value={model.model}
                    onChange={(event) => updateModel(index, { model: event.target.value, key: model.key || event.target.value, upstreamModel: model.upstreamModel || event.target.value })}
                    placeholder="gpt-5.5"
                  />
                </label>
                <label className="field">
                  <span>上游模型</span>
                  <input className="input mono" value={model.upstreamModel} onChange={(event) => updateModel(index, { upstreamModel: event.target.value })} placeholder="实际请求模型名" />
                </label>
                <label className="field">
                  <span>Adapter</span>
                  <div className="tb-select">
                    <select value={model.type || "openai-compatible"} onChange={(event) => updateModel(index, { type: event.target.value })}>
                      <option value="openai-compatible">OpenAI 兼容</option>
                      <option value="openai">OpenAI</option>
                      <option value="anthropic">Anthropic</option>
                      <option value="gemini">Gemini</option>
                      <option value="google">Google</option>
                    </select>
                  </div>
                </label>
              </div>
              <div className="monitor-model-grid compact">
                <label className="field">
                  <span>Key</span>
                  <input className="input mono" value={model.key} onChange={(event) => updateModel(index, { key: event.target.value })} placeholder="唯一标识" />
                </label>
                <label className="field">
                  <span>输入 / MTok</span>
                  <input className="input" type="number" min={0} step="0.0001" value={model.inputPerMtok} onChange={(event) => updateModel(index, { inputPerMtok: Number(event.target.value) || 0 })} />
                </label>
                <label className="field">
                  <span>输出 / MTok</span>
                  <input className="input" type="number" min={0} step="0.0001" value={model.outputPerMtok} onChange={(event) => updateModel(index, { outputPerMtok: Number(event.target.value) || 0 })} />
                </label>
                <label className="field aliases">
                  <span>别名</span>
                  <input className="input mono" value={(model.aliases ?? []).join(", ")} onChange={(event) => updateModel(index, { aliases: splitAliases(event.target.value) })} placeholder="逗号分隔" />
                </label>
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

function BillingSettings({ summary }: { summary: AdminSettingsSummary | null }) {
  return (
    <div className="set-pane">
      <div className="stat-row">
        <SettingsStat label="本月请求" value={formatInt(summary?.usageRequestsMonth ?? 0)} hint="usage rollup" />
        <SettingsStat label="本月成本" value={`$${(summary?.usageCostMonth ?? 0).toFixed(4)}`} hint="按模型价格估算" />
        <SettingsStat label="网关总数" value={String(summary?.gateways ?? 0)} hint={`${summary?.activeGateways ?? 0} active`} />
        <SettingsStat label="Active Key" value={String(summary?.activeGatewayKeys ?? 0)} hint="可调用密钥" />
      </div>
      <div className="card set-card">
        <div className="set-h">套餐与计费治理</div>
        <ActionRow title="用量与成本报表" help="按网关、模型、通道、成员筛选真实 usage，支持 CSV 导出和 rollup 重算。" href={adminPath("/usage")} action="查看用量" />
        <ActionRow title="网关额度与策略" help="创建、编辑、暂停、删除平台网关，管理 QPS、月度额度和上游集合。" href={adminPath("/gateways")} action="管理网关" />
        <ActionRow title="成员 Key 与限额" help="签发、编辑、吊销 Gateway Key，按成员和网关控制调用额度。" href={adminPath("/members")} action="管理 Key" />
        <ActionRow title="成本告警" help="配置成本阈值、网关错误率和恢复通知，避免用量异常无人处理。" href={adminPath("/alerts")} action="配置告警" />
      </div>
    </div>
  );
}

function SecuritySettings({
  summary,
  registrationOpen,
  emailVerificationRequired,
  viewer,
  revokingSessions,
  onRevokeSessions
}: {
  summary: AdminSettingsSummary | null;
  registrationOpen: boolean;
  emailVerificationRequired: boolean;
  viewer: User | null;
  revokingSessions: boolean;
  onRevokeSessions: () => void;
}) {
  return (
    <div className="set-pane">
      <div className="stat-row">
        <SettingsStat label="活跃会话" value={String(summary?.activeSessions ?? 0)} hint="未撤销且未过期" />
        <SettingsStat label="今日人工审计" value={String(summary?.auditToday ?? 0)} hint="不含系统探测" />
        <SettingsStat label="注册状态" value={registrationOpen ? "开放" : "关闭"} hint="后端真实开关" />
        <SettingsStat label="邮箱验证" value={emailVerificationRequired ? "开启" : "关闭"} hint="注册后是否阻塞登录" />
        <SettingsStat label="CSRF/Cookie" value="启用" hint="写接口保护" />
      </div>
      <div className="card set-card">
        <div className="set-h">安全策略治理</div>
        <ActionRow title="账号与会话治理" help="新增、禁用、删除用户会撤销 active session；Owner 和当前操作者有保护。" href={adminPath("/users")} action="管理用户" />
        <div className="set-row">
          <div className="lbl"><b>当前账号其他会话</b><small>撤销当前账号在其他浏览器或设备上的会话，当前浏览器保持登录。</small></div>
          <div className="ctl">
            <button className="btn btn-ghost btn-sm" type="button" onClick={onRevokeSessions} disabled={revokingSessions}>
              {revokingSessions ? "撤销中..." : "撤销其他会话"}
            </button>
          </div>
        </div>
        <ActionRow title="审计查询与导出" help="按操作人、对象、时间和结果筛选审计记录，CSV 导出不包含敏感 metadata。" href={adminPath("/audit")} action="查看审计" />
        <ActionRow title="注册入口与邮箱验证" help="开放公共注册、显示注册入口和品牌文案仍在组织页签保存。" href={adminPath("/settings")} action="回到组织页签" />
        <ActionRow title="生产数据健康检查" help="检查 demo/test 数据、真实通知渠道、真实运营配置和发布闸门状态。" href={adminPath()} action="查看总览" />
      </div>
      {viewer?.role === "owner" ? <AdminAgentTokenManager /> : null}
    </div>
  );
}

function IntegrationSettings({ summary, draft, setDraft, canManageAnalytics }: { summary: AdminSettingsSummary | null; draft: SiteConfig; setDraft: (site: SiteConfig) => void; canManageAnalytics: boolean }) {
  const analyticsEnabled = Boolean(draft.analyticsCode?.trim());

  return (
    <div className="set-pane">
      <div className="stat-row">
        <SettingsStat label="启用告警规则" value={String(summary?.enabledAlertRules ?? 0)} hint="alert rules" />
        <SettingsStat label="启用通知渠道" value={String(summary?.enabledNotificationChannels ?? 0)} hint="email/webhook/feishu" />
        <SettingsStat label="Open API 站点" value={String(summary?.openApiSites ?? 0)} hint="active site keys" />
        <SettingsStat label="私有通道" value={String(summary?.privateChannels ?? 0)} hint="用户工作区" />
        <SettingsStat label="统计代码" value={analyticsEnabled ? "已启用" : "未配置"} hint="site-wide inject" />
      </div>
      <div className="card set-card">
        <div className="set-h">全站统计代码</div>
        <div className="set-row analytics-code-row">
          <div className="lbl">
            <b>统计代码</b>
            <small>粘贴百度统计或 Google Analytics 的完整代码。保存后公开页面、通道详情页、登录页、用户控制台和管理后台的 HTML 都会注入。</small>
          </div>
          <div className="ctl analytics-code-control">
            <textarea
              className="input mono analytics-code-textarea"
              aria-label="统计代码"
              rows={9}
              value={draft.analyticsCode ?? ""}
              onChange={(event) => setDraft({ ...draft, analyticsCode: event.target.value })}
              disabled={!canManageAnalytics}
              placeholder={`<!-- 百度统计或 Google Analytics 代码 -->\n<script>\n  // paste analytics snippet here\n</script>`}
            />
            <div className="analytics-code-actions">
              <span className="reg-switch-hint">{canManageAnalytics ? `当前长度 ${(draft.analyticsCode ?? "").length} / 20000` : "只有 owner 可以修改统计代码"}</span>
              <button className="btn btn-ghost btn-sm" type="button" onClick={() => setDraft({ ...draft, analyticsCode: "" })} disabled={!canManageAnalytics || !analyticsEnabled}>
                清空统计代码
              </button>
            </div>
          </div>
        </div>
      </div>
      <div className="card set-card">
        <div className="set-h">集成与通知治理</div>
        <ActionRow title="通知渠道" help="新增、编辑、测试、启停、删除 email/webhook/飞书通知渠道，支持批量治理。" href={adminPath("/alerts")} action="管理通知" />
        <ActionRow title="Open API 授权站点" help="创建 Site Key、限定 scope/QPS、暂停或吊销第三方只读状态接口。" href={adminPath("/open-api")} action="管理授权" />
        <ActionRow title="前台站点集成" help="维护公开导航、页脚链接、品牌和注册 CTA，保存后公开页面实时读取。" href={adminPath("/web")} action="管理网站" />
        <ActionRow title="平台通道集成" help="维护平台供应商通道、真实凭据、探测开关和网关准入状态。" href={adminPath("/channels")} action="管理通道" />
      </div>
    </div>
  );
}

const adminAgentScopeOptions: Array<{ value: AdminAgentScope; label: string; help: string }> = [
  { value: "admin:read", label: "读取", help: "查询后台资源和状态" },
  { value: "admin:write", label: "写入", help: "创建或更新后台资源" },
  { value: "admin:dangerous", label: "高危", help: "删除、禁用、吊销等操作" },
  { value: "admin:secrets", label: "密钥", help: "创建或轮换敏感凭据" },
  { value: "admin:export", label: "导出", help: "导出审计、用量或站点包" }
];

function AdminAgentTokenManager() {
  const [items, setItems] = useState<AdminAgentToken[]>([]);
  const [name, setName] = useState("");
  const [ttlHours, setTTLHours] = useState("720");
  const [scopes, setScopes] = useState<AdminAgentScope[]>(["admin:read"]);
  const [plainToken, setPlainToken] = useState("");
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [revokingID, setRevokingID] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  useEffect(() => {
    void loadTokens();
  }, []);

  async function loadTokens() {
    setLoading(true);
    setError("");
    try {
      const payload = await adminAgentTokens();
      setItems(payload.items);
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载 Admin agent token 失败");
    } finally {
      setLoading(false);
    }
  }

  async function createToken() {
    const trimmedName = name.trim();
    if (!trimmedName) {
      setError("请输入 token 名称");
      return;
    }
    if (scopes.length === 0) {
      setError("至少选择一个 scope");
      return;
    }
    const parsedTTL = Number(ttlHours);
    if (!Number.isFinite(parsedTTL) || parsedTTL < 0) {
      setError("有效期不合法");
      return;
    }
    setCreating(true);
    setError("");
    setNotice("");
    setPlainToken("");
    try {
      const payload = await createAdminAgentToken({
        name: trimmedName,
        scopes,
        ...(parsedTTL > 0 ? { ttlHours: parsedTTL } : {})
      });
      setItems((current) => [payload.token, ...current.filter((item) => item.id !== payload.token.id)]);
      setPlainToken(payload.token.plainToken ?? "");
      setName("");
      setScopes(["admin:read"]);
      setTTLHours("720");
      setNotice("Admin agent token 已创建。明文 token 只会展示这一次。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "创建 Admin agent token 失败");
    } finally {
      setCreating(false);
    }
  }

  async function revokeToken(item: AdminAgentToken) {
    if (!window.confirm(`确认吊销 ${item.name}？吊销后该 token 不能再调用后台接口。`)) return;
    setRevokingID(item.id);
    setError("");
    setNotice("");
    try {
      const payload = await revokeAdminAgentToken(item.id);
      setItems((current) => current.map((candidate) => candidate.id === item.id ? payload.token : candidate));
      setNotice(`${item.name} 已吊销。`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "吊销 Admin agent token 失败");
    } finally {
      setRevokingID("");
    }
  }

  async function copyPlainToken() {
    if (!plainToken || !navigator.clipboard) return;
    try {
      await navigator.clipboard.writeText(plainToken);
      setNotice("明文 token 已复制。");
    } catch {
      setError("复制失败，请手动选中复制。");
    }
  }

  function toggleScope(scope: AdminAgentScope) {
    setScopes((current) => {
      const next = new Set(current);
      if (next.has(scope)) {
        next.delete(scope);
        if (scope === "admin:read") next.delete("admin:export");
        if (scope === "admin:write") {
          next.delete("admin:dangerous");
          next.delete("admin:secrets");
        }
      } else {
        next.add(scope);
        if (scope === "admin:export") next.add("admin:read");
        if (scope === "admin:dangerous" || scope === "admin:secrets") next.add("admin:write");
      }
      return orderAdminAgentScopes(Array.from(next));
    });
  }

  return (
    <div className="card set-card">
      <div className="set-h">Admin agent token</div>
      {error ? <div className="form-error">{error}</div> : null}
      {notice ? <div className="form-notice">{notice}</div> : null}
      {plainToken ? (
        <div className="set-row">
          <div className="lbl"><b>一次性明文 token</b><small>离开或刷新页面后无法再次查看，只能重新创建。</small></div>
          <div className="ctl" style={{ alignItems: "stretch", flexDirection: "column" }}>
            <textarea className="input mono" readOnly rows={2} value={plainToken} />
            <button className="btn btn-ghost btn-sm" type="button" onClick={() => void copyPlainToken()}>复制 token</button>
          </div>
        </div>
      ) : null}
      <div className="set-row">
        <div className="lbl"><b>创建 token</b><small>用于受控自动化访问后台 API。默认只授予读取权限。</small></div>
        <div className="ctl" style={{ alignItems: "stretch", flexDirection: "column" }}>
          <input className="input" value={name} onChange={(event) => setName(event.target.value)} placeholder="例如：prod-ops-agent" maxLength={80} />
          <div className="tb-select">
            <select aria-label="Admin agent token 有效期" value={ttlHours} onChange={(event) => setTTLHours(event.target.value)}>
              <option value="24">24 小时</option>
              <option value="168">7 天</option>
              <option value="720">30 天</option>
              <option value="2160">90 天</option>
              <option value="0">不过期</option>
            </select>
          </div>
          <div className="scope-checks">
            {adminAgentScopeOptions.map((option) => (
              <label className="checkline" key={option.value}>
                <input type="checkbox" checked={scopes.includes(option.value)} onChange={() => toggleScope(option.value)} />
                <span><b>{option.label}</b><small>{option.value} · {option.help}</small></span>
              </label>
            ))}
          </div>
          <button className="btn btn-primary btn-sm" type="button" onClick={() => void createToken()} disabled={creating}>
            {creating ? "创建中..." : "创建 token"}
          </button>
        </div>
      </div>
      <div className="set-row">
        <div className="lbl"><b>已创建 token</b><small>这里只显示摘要和状态，明文不会再次返回。</small></div>
        <div className="ctl" style={{ alignItems: "stretch", flexDirection: "column" }}>
          {loading ? <span className="module-sub">加载中...</span> : null}
          {!loading && items.length === 0 ? <span className="module-sub">暂无 Admin agent token。</span> : null}
          {items.map((item) => {
            const tokenState = adminAgentTokenState(item);
            return (
              <div className="mini-row" key={item.id}>
                <div>
                  <b>{item.name}</b>
                  <small className="mono">{item.tokenMask || item.tokenPrefix}</small>
                  <small>{item.scopes.join(" · ")}</small>
                  <small>创建：{formatDateTime(item.createdAt)} · 最后使用：{formatDateTime(item.lastUsedAt)}</small>
                  <small>过期：{formatDateTime(item.expiresAt)} · 创建人：{item.createdByEmail || "-"}</small>
                </div>
                <div className="row-actions">
                  <span className={`pill ${tokenState === "active" ? "ok" : "bad"}`}>{tokenState}</span>
                  <button className="btn btn-ghost btn-sm" type="button" onClick={() => void revokeToken(item)} disabled={Boolean(item.revokedAt) || revokingID === item.id}>
                    {revokingID === item.id ? "吊销中..." : "吊销"}
                  </button>
                </div>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}

function SettingsStat({ label, value, hint }: { label: string; value: string; hint: string }) {
  return <div className="stat"><div className="l">{label}</div><div className="v">{value}</div><div className="d">{hint}</div></div>;
}

function ActionRow({ title, help, href, action }: { title: string; help: string; href: string; action: string }) {
  return (
    <div className="set-row">
      <div className="lbl"><b>{title}</b><small>{help}</small></div>
      <div className="ctl"><a className="btn btn-ghost btn-sm" href={href}>{action}</a></div>
    </div>
  );
}

function validateSettingsDraft(site: SiteConfig) {
  if (!site.brandName.trim()) return "品牌名称不能为空。";
  if (Array.from(site.logoMark.trim()).length < 1 || Array.from(site.logoMark.trim()).length > 2) return "Logo 标记必须是 1-2 个字符。";
  if (site.subtitle.length > 80) return "品牌副标题不能超过 80 个字符。";
  if (site.footerText.length > 120) return "版权声明不能超过 120 个字符。";
  if ((site.analyticsCode ?? "").length > 20000) return "统计代码不能超过 20000 个字符。";
  const adminPathError = validateAdminPathInput(site.adminPath || "/admin");
  if (adminPathError) return adminPathError;
  if (!["latency", "success", "cost"].includes(site.defaultGatewayPolicy)) return "默认路由策略不合法。";
  if (!site.timezone.trim() || /\s/.test(site.timezone) || site.timezone.length > 64) return "时区格式不合法。";
  const monitorModels = site.monitorModels ?? [];
  if (monitorModels.length === 0) return "至少配置一个监控模型。";
  if (monitorModels.length > 8) return "监控模型最多配置 8 个。";
  const seen = new Set<string>();
  for (let index = 0; index < monitorModels.length; index += 1) {
    const item = monitorModels[index];
    const row = `监控模型第 ${index + 1} 项`;
    const key = (item.key || item.model).trim();
    const model = item.model.trim();
    const upstreamModel = (item.upstreamModel || item.model).trim();
    if (!model) return `${row}缺少模型 ID。`;
    if (item.label.trim().length > 40) return `${row}名称不能超过 40 个字符。`;
    if (!validMonitorModelKey(key) || !validMonitorModelKey(model) || !validMonitorModelKey(upstreamModel)) return `${row}模型 ID 格式不合法。`;
    if (!["openai-compatible", "openai", "anthropic", "gemini", "google"].includes(item.type)) return `${row}Adapter 不合法。`;
    if (item.inputPerMtok < 0 || item.outputPerMtok < 0) return `${row}价格不能为负数。`;
    if ((item.aliases ?? []).length > 12) return `${row}别名过多。`;
    for (const alias of item.aliases ?? []) {
      if (!validMonitorModelKey(alias)) return `${row}别名格式不合法。`;
    }
    const dedupeKey = key.toLowerCase();
    if (seen.has(dedupeKey)) return "监控模型 Key 不能重复。";
    seen.add(dedupeKey);
  }
  return "";
}

function splitAliases(value: string) {
  return value
    .split(/[,，\n]/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function validMonitorModelKey(value: string) {
  return /^[a-zA-Z0-9._:/-]{1,120}$/.test(value);
}

function formatInt(value: number) {
  if (value >= 1000000) return `${(value / 1000000).toFixed(1)}M`;
  if (value >= 1000) return `${(value / 1000).toFixed(1)}K`;
  return String(value);
}

function orderAdminAgentScopes(scopes: AdminAgentScope[]) {
  const selected = new Set(scopes);
  return adminAgentScopeOptions.map((option) => option.value).filter((scope) => selected.has(scope));
}

function adminAgentTokenState(item: AdminAgentToken) {
  if (item.revokedAt) return "revoked";
  if (item.expiresAt) {
    const expiresAt = new Date(item.expiresAt);
    if (!Number.isNaN(expiresAt.getTime()) && expiresAt.getTime() <= Date.now()) return "expired";
  }
  return "active";
}

function formatDateTime(value?: string) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "-";
  return date.toLocaleString();
}
