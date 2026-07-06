import { FormEvent, useEffect, useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { AdminShell } from "../components/AdminShell";
import {
  adminRecommend,
  PublicChannel,
  RecommendAdminData,
  RecommendPick,
  saveAdminRecommend
} from "../lib/api";
import { FormField, PageHeader, SelectField } from "../ui";

type RecommendCreateDraft = {
  channelId: string;
  title: string;
  ribbon: string;
  summary: string;
  points: string;
  ctaLabel: string;
  ctaUrl: string;
  enabled: boolean;
};

const defaultPoints = ["真实探测数据驱动", "可接入个人或团队网关", "支持后台运营随时下线"].join("\n");
const maxRecommendPicks = 3;
const defaultCTALabel = "去官方体验";

export function AdminRecommendNewPage() {
  const navigate = useNavigate();
  const [data, setData] = useState<RecommendAdminData | null>(null);
  const [draft, setDraft] = useState<RecommendCreateDraft | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    let active = true;
    async function load() {
      setLoading(true);
      setError("");
      try {
        const payload = await adminRecommend();
        if (!active) return;
        setData(payload);
        setDraft(createDraftFromChannel(payload.channels[0]));
      } catch (err) {
        if (active) setError(err instanceof Error ? err.message : "加载推荐配置失败");
      } finally {
        if (active) setLoading(false);
      }
    }
    void load();
    return () => {
      active = false;
    };
  }, []);

  const channel = useMemo(() => {
    if (!data || !draft) return undefined;
    return channelByID(data.channels, draft.channelId);
  }, [data, draft]);
  const canSubmit = Boolean(draft && data?.channels.length);

  function patch<K extends keyof RecommendCreateDraft>(key: K, value: RecommendCreateDraft[K]) {
    setDraft((current) => current ? { ...current, [key]: value } : current);
  }

  function handleChannelChange(channelId: string) {
    if (!data) return;
    const nextChannel = channelByID(data.channels, channelId);
    const previousChannel = draft ? channelByID(data.channels, draft.channelId) : undefined;
    setDraft((current) => {
      if (!current) return createDraftFromChannel(nextChannel);
      const previousDefaultCTA = defaultOfficialURL(previousChannel);
      return {
        ...current,
        channelId,
        title: !current.title.trim() || current.title === previousChannel?.name ? nextChannel?.name ?? current.title : current.title,
        summary: !current.summary.trim() || current.summary === channelSummary(previousChannel) ? channelSummary(nextChannel) : current.summary,
        ctaUrl: !current.ctaUrl.trim() || current.ctaUrl === previousDefaultCTA ? defaultOfficialURL(nextChannel) : current.ctaUrl
      };
    });
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!data || !draft) return;
    const selectedChannel = channelByID(data.channels, draft.channelId);
    const validation = validateDraft(draft, selectedChannel, data.picks.length);
    if (validation) {
      setError(validation);
      return;
    }
    const pick: RecommendPick = {
      id: "",
      channelId: selectedChannel!.id,
      position: data.picks.length + 1,
      title: draft.title.trim(),
      ribbon: draft.ribbon.trim() || "编辑精选",
      summary: draft.summary.trim(),
      points: cleanLines(draft.points).slice(0, 6),
      ctaLabel: draft.ctaLabel.trim() || defaultCTALabel,
      ctaUrl: draft.ctaUrl.trim() || defaultOfficialURL(selectedChannel),
      enabled: draft.enabled,
      clicks: 0,
      ctr: 0,
      channel: selectedChannel!
    };

    setSaving(true);
    setError("");
    try {
      await saveAdminRecommend({
        picks: [...data.picks, pick],
        rankRules: data.rankRules,
        rewards: data.rewards,
        scenarios: data.scenarios
      });
      navigate("/admin/recommend", { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存推荐位失败");
    } finally {
      setSaving(false);
    }
  }

  return (
    <AdminShell title="新增推荐位" crumb="/ 平台运营 / 精选推荐 / 新增">
      <PageHeader
        description={<p className="page-intro">为前台 /recommend 的「本周编辑精选」新增一个推荐位。先选择已公开平台通道，再填写推荐语、卖点和官方体验按钮</p>}
        actions={(
          <>
            <Link className="btn btn-ghost btn-sm" to="/admin/recommend">返回推荐管理</Link>
            <button className="btn btn-primary btn-sm" type="submit" form="admin-recommend-create-form" disabled={loading || saving || !canSubmit}>
              {saving ? "保存中..." : "保存推荐位"}
            </button>
          </>
        )}
      />

      {error ? <div className="form-error">{error}</div> : null}

      {loading ? (
        <div className="card card-pad empty-state"><h4>正在加载推荐配置</h4></div>
      ) : !data?.channels.length || !draft ? (
        <div className="card card-pad empty-state">
          <h4>暂无可推荐通道</h4>
          <p>请先在平台通道中创建并公开至少一个真实通道，然后再新增推荐位。</p>
          <Link className="btn btn-primary btn-sm" to="/admin/channels">去管理平台通道</Link>
        </div>
      ) : (
        <div className="admin-user-create-layout recommend-create-layout">
          <form id="admin-recommend-create-form" className="card admin-user-create-form recommend-create-form" onSubmit={(event) => void submit(event)}>
            <section className="admin-user-form-section">
              <div className="admin-user-form-section-head">
                <div>
                  <h2>选择推荐通道</h2>
                  <p>只允许选择公开的平台通道。保存后前台推荐页会读取这条配置。</p>
                </div>
                <span className="admin-user-step">01</span>
              </div>
              <div className="admin-user-form-grid">
                <SelectField label="推荐通道" value={draft.channelId} onChange={(event) => handleChannelChange(event.target.value)}>
                  {data.channels.map((item) => (
                    <option value={item.id} key={item.id}>{item.name} · {item.provider} · {item.model}</option>
                  ))}
                </SelectField>
                <div className="recommend-create-channel-card">
                  <span className="pk-mk" style={{ background: channel?.mark || "var(--brand)" }}>{initials(channel?.name || "T")}</span>
                  <div>
                    <b>{channel?.name}</b>
                    <small>{channel?.provider} · {channel?.model}</small>
                  </div>
                  <span className={`badge ${channel?.status === "healthy" ? "b-green" : "b-red"} dot`}>{channel?.statusLabel || channel?.status}</span>
                </div>
              </div>
            </section>

            <section className="admin-user-form-section">
              <div className="admin-user-form-section-head">
                <div>
                  <h2>推荐内容</h2>
                  <p>标题、角标和摘要会直接显示在推荐页卡片中，卖点每行一条。</p>
                </div>
                <span className="admin-user-step">02</span>
              </div>
              <div className="admin-user-form-grid">
                <FormField label="推荐标题">
                  <input className="input" value={draft.title} onChange={(event) => patch("title", event.target.value)} placeholder={channel?.name || "例如 AIGoCode"} required />
                </FormField>
                <FormField label="角标">
                  <input className="input" value={draft.ribbon} onChange={(event) => patch("ribbon", event.target.value)} placeholder="例如 编辑精选 / 低延迟" />
                </FormField>
                <FormField label="推荐摘要" className="recommend-create-full">
                  <textarea className="input recommend-create-textarea" value={draft.summary} onChange={(event) => patch("summary", event.target.value)} placeholder="说明适合什么场景、主要模型或调用方式" required />
                </FormField>
                <FormField label="卖点（一行一个，最多 6 条）" className="recommend-create-full">
                  <textarea className="input recommend-create-textarea tall" value={draft.points} onChange={(event) => patch("points", event.target.value)} required />
                </FormField>
              </div>
            </section>

            <section className="admin-user-form-section">
              <div className="admin-user-form-section-head">
                <div>
                  <h2>跳转与发布</h2>
                  <p>按钮会在新标签页打开官方注册或体验页。URL 需要以 http(s) 开头，停用时保存配置但不在前台展示。</p>
                </div>
                <span className="admin-user-step">03</span>
              </div>
              <div className="admin-user-form-grid">
                <FormField label="按钮文案">
                  <input className="input" value={draft.ctaLabel} onChange={(event) => patch("ctaLabel", event.target.value)} placeholder={defaultCTALabel} required />
                </FormField>
                <FormField label="官方注册页 URL">
                  <input className="input" value={draft.ctaUrl} onChange={(event) => patch("ctaUrl", event.target.value)} placeholder="https://example.com/register" required />
                </FormField>
                <div className="admin-user-verified-card">
                  <label className="check-line">
                    <input type="checkbox" checked={draft.enabled} onChange={(event) => patch("enabled", event.target.checked)} />
                    保存后立即启用
                  </label>
                  <p>关闭后只保存到后台草稿区，不会出现在前台推荐页。</p>
                </div>
              </div>
            </section>

            <div className="admin-user-form-footer">
              <Link className="btn btn-ghost btn-sm" to="/admin/recommend">取消</Link>
              <button className="btn btn-primary btn-sm" type="submit" disabled={saving}>{saving ? "保存中..." : "保存推荐位"}</button>
            </div>
          </form>

          <aside className="admin-user-create-side recommend-create-side">
            <section className="card admin-user-help-card recommend-preview-card">
              <h3>前台卡片预览</h3>
              <div className="pk-card recommend-create-preview">
                <div className="pk-card-toolbar">
                  <span className="badge b-blue dot">{draft.ribbon || "编辑精选"}</span>
                  <span className="pk-pos">#{data.picks.length + 1}</span>
                </div>
                <div className="pk-head">
                  <span className="pk-mk" style={{ background: channel?.mark || "var(--brand)" }}>{initials(draft.title || channel?.name || "T")}</span>
                  <div className="pk-meta">
                    <b>{draft.title || channel?.name}</b>
                    <small>{channel?.provider} · {channel?.model}</small>
                  </div>
                </div>
                <p className="recommend-preview-summary">{draft.summary || channelSummary(channel)}</p>
                <ul className="recommend-preview-points">
                  {cleanLines(draft.points).slice(0, 6).map((point) => <li key={point}>{point}</li>)}
                </ul>
                <div className="recommend-preview-cta">{draft.ctaLabel || defaultCTALabel}</div>
              </div>
            </section>
            <section className="card admin-user-help-card muted-card">
              <h3>保存规则</h3>
              <p>推荐位最多 {maxRecommendPicks} 个，当前已有 {data.picks.length} 个。保存时会写入推荐配置和审计事件。</p>
            </section>
          </aside>
        </div>
      )}
    </AdminShell>
  );
}

function createDraftFromChannel(channel?: PublicChannel): RecommendCreateDraft {
  return {
    channelId: channel?.id || "",
    title: channel?.name || "",
    ribbon: "编辑精选",
    summary: channelSummary(channel),
    points: defaultPoints,
    ctaLabel: defaultCTALabel,
    ctaUrl: defaultOfficialURL(channel),
    enabled: true
  };
}

function validateDraft(draft: RecommendCreateDraft, channel: PublicChannel | undefined, existingCount: number) {
  if (existingCount >= maxRecommendPicks) return `推荐位最多支持 ${maxRecommendPicks} 个，请先删除或停用旧推荐位。`;
  if (!channel) return "请选择一个已公开的平台通道。";
  if (!draft.title.trim()) return "请填写推荐标题。";
  if (!draft.summary.trim()) return "请填写推荐摘要。";
  if (!cleanLines(draft.points).length) return "请至少填写一个卖点。";
  if (!draft.ctaLabel.trim()) return "请填写按钮文案。";
  if (!isValidCTA(draft.ctaUrl)) return "官方注册页 URL 必须是 http(s) 链接。";
  return "";
}

function channelByID(channels: PublicChannel[], channelID: string) {
  return channels.find((item) => item.id === channelID) ?? channels[0];
}

function channelSummary(channel?: PublicChannel) {
  return channel ? `${channel.provider} · ${channel.model}` : "";
}

function defaultOfficialURL(channel?: PublicChannel) {
  return channel ? (channel.officialSiteUrl || "").trim() || officialEntryURL(channel.endpoint) : "";
}

function cleanLines(value: string) {
  return value.split("\n").map((item) => item.trim()).filter(Boolean);
}

function isValidCTA(value: string) {
  const url = value.trim();
  return isHTTPURL(url);
}

function officialEntryURL(endpoint: string) {
  try {
    const parsed = new URL(endpoint);
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") return "";
    const hostname = publicWebsiteHost(parsed.hostname);
    return hostname ? `https://${hostname}` : "";
  } catch {
    return "";
  }
}

function publicWebsiteHost(hostname: string) {
  const parts = hostname.toLowerCase().split(".").filter(Boolean);
  if (parts.length < 2) return "";
  const apiPrefixes = new Set(["api", "api2", "cc-api", "chat-api", "openapi", "gateway", "proxy", "relay", "upstream"]);
  while (parts.length > 2 && apiPrefixes.has(parts[0])) {
    parts.shift();
  }
  return parts.join(".");
}

function isHTTPURL(value: string) {
  try {
    const parsed = new URL(value);
    return (parsed.protocol === "http:" || parsed.protocol === "https:") && Boolean(parsed.hostname);
  } catch {
    return false;
  }
}

function initials(value: string) {
  return value.split(/\s+/).filter(Boolean).slice(0, 2).map((part) => part[0]).join("").toUpperCase().slice(0, 2) || "T";
}
