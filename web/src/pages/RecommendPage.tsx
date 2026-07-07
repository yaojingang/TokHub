import { useEffect, useMemo, useState } from "react";
import { Footer } from "../components/Footer";
import { PublicNav } from "../components/PublicNav";
import {
  publicOverview,
  publicRecommend,
  publicChannelPath,
  trackRecommendClick,
  type PublicOverview,
  type RecommendConfig,
  type RecommendPick,
  type RecommendRankRule,
  type RecommendReward,
  type RecommendScenario
} from "../lib/api";

type RankTab = RecommendRankRule["metric"];

const recommendToolOptions = ["Claude Code", "Cursor", "Cline", "ChatWise", "Open WebUI", "更多工具"];
const defaultCTALabel = "去官方体验";

const certificationSteps = [
  "连续监控 L1 + L2 + L3",
  "真实成功率、P95、成本综合评分",
  "推荐入口由运营后台审核",
  "状态异常时自动从榜单降权",
  "所有运营变更写入审计"
];

const pendingCertificationSteps = [
  "正在读取后台推荐配置",
  "同步完成后再展示本次稳定结果",
  "首页和推荐页读取同一份后台推荐",
  "接口不可用时不展示过期推荐内容"
];

type TrackRecommendation = (itemType: string, itemId: string, channelId?: string) => Promise<void>;
const emptyRecommendConfig: RecommendConfig = {
  stats: { picks: 0, ranked: 0, rewards: 0, scenarios: 0, clicks: 0, ctr: 0, averageScore: 0 },
  picks: [],
  ranks: [],
  rankRules: [],
  rewards: [],
  scenarios: [],
  updatedAt: ""
};

export function RecommendPage() {
  const [data, setData] = useState<RecommendConfig | null>(null);
  const [overview, setOverview] = useState<PublicOverview | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [tab, setTab] = useState<RankTab>("overall");

  useEffect(() => {
    let active = true;
    setLoading(true);
    setError("");
    Promise.allSettled([publicRecommend(), publicOverview()])
      .then(([recommendResult, overviewResult]) => {
        if (!active) return;
        if (recommendResult.status === "fulfilled") {
          setData(recommendResult.value);
        } else {
          setData(null);
          const err = recommendResult.reason;
          setError(err instanceof Error ? err.message : "加载推荐失败");
        }
        if (overviewResult.status === "fulfilled") {
          setOverview(overviewResult.value);
        } else {
          setOverview(null);
        }
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, []);

  const isInitialSync = loading && !data;
  const displayData = data ?? emptyRecommendConfig;
  const rankRules = useMemo(() => displayData.rankRules.filter((rule) => rule.enabled), [displayData.rankRules]);
  useEffect(() => {
    if (!rankRules.length) return;
    if (!rankRules.some((rule) => rule.metric === tab)) {
      setTab(rankRules[0].metric);
    }
  }, [rankRules, tab]);
  const activeRule = rankRules.find((rule) => rule.metric === tab) ?? rankRules[0];
  const rankItems = displayData.ranks.length ? displayData.ranks : displayData.picks;
  const ranks = useMemo(() => activeRule ? rankByTab(rankItems, activeRule.metric) : [], [rankItems, activeRule]);
  const rewardsByChannel = useMemo(() => new Map(displayData.rewards.map((reward) => [reward.channelId, reward])), [displayData.rewards]);
  const certifySteps = isInitialSync ? pendingCertificationSteps : certificationSteps;
  const statusLabel = isInitialSync ? "同步中" : displayData.updatedAt ? dateLabel(displayData.updatedAt) : "暂无推荐";
  const monitoredChannelValue = overview ? formatInt(overview.total) : "—";

  async function record(itemType: string, itemId: string, channelId?: string) {
    try {
      await trackRecommendClick({ itemType, itemId, channelId });
    } catch {
      // 点击埋点不能阻塞用户跳转。
    }
  }

  return (
    <>
      <PublicNav />
      <section className="rec-hero">
        <div className="rec-hero-inner">
          <div className="rec-hero-left">
            <span className="rec-tag-pill">
              <i /> 编辑精选 · 每周更新 <b>·</b> {statusLabel}
            </span>
            <h1>
              不踩坑的<span className="hl">中转站</span>，都在这里
            </h1>
            <p className="rec-lead">
              TokHub 用 <b>真实 Token 探测</b> + <b>三层链路监控</b> 跑过主流中转站。这一页和首页读取同一份后台 TOP 3 推荐，再接入你自己的 API Key
            </p>
            <div className="rec-cta-row">
              <a href="#picks" className="btn btn-primary">
                查看本周精选 →
              </a>
              <a href="#quick-try" className="btn btn-ghost">
                创建个人网关
              </a>
              <span className="trust-mini">
                <span className="ts-av">L</span>
                <span className="ts-av">Y</span>
                <span className="ts-av">C</span>
                <span className="ts-av">+</span>
                <b>{monitoredChannelValue}</b> 个通道监控中
              </span>
            </div>
          </div>
          <div className="rec-hero-right">
            <div className="trust-card">
              <div className="trust-h">
                <span className="trust-ico">✓</span>
                <b>靠谱认证体系</b>
              </div>
              <TrustRow k={isInitialSync ? "数据状态" : "连续监控"} v={isInitialSync ? "同步中" : "L1 / L2 / L3"} />
              <TrustRow k="综合评分" v={isInitialSync ? "同步中" : `平均 ${displayData.stats.averageScore}`} />
              <TrustRow k="真实成功率" v={isInitialSync ? "同步中" : "以当前探测为准"} />
              <TrustRow k="P95 延迟" v={isInitialSync ? "同步中" : "稳定优先"} />
              <TrustRow k="推荐入口" v={isInitialSync ? "同步中" : `${displayData.stats.rewards} 项`} />
              <div className="trust-foot">
                推荐配置由后台运营维护 · <a href="#certify">了解认证逻辑</a>
              </div>
            </div>
          </div>
        </div>
      </section>

      <main className="page rec-page">
        {error ? <div className="form-error">后台推荐配置暂时不可用：{error}</div> : null}
        <section className="rec-stats">
          <Stat value={isInitialSync ? "—" : `${displayData.stats.picks}`} label="编辑精选位" />
          <Stat value={isInitialSync ? "—" : `${displayData.stats.ranked}`} label="上榜中转站" />
          <Stat value={monitoredChannelValue} label="监控通道数" />
          <Stat value={isInitialSync ? "—" : `${displayData.stats.averageScore}`} label="精选平均评分" />
        </section>

        <div className="section-head" id="picks">
          <h2>
            本周编辑精选 <span className="tag">TOP 3</span>
          </h2>
          <span className="sub">{isInitialSync ? "正在读取后台推荐配置，避免展示过期推荐" : "基于真实探测 · 综合速度 / 成功率 / 价格 / 售后评分"}</span>
        </div>

        <section className="rec-picks">
          {isInitialSync ? (
            <div className="card card-pad empty-state rec-wide">
              <h4>正在同步精选推荐</h4>
              <p>读取后台推荐配置后再展示，避免首屏内容和最新榜单不一致。</p>
            </div>
          ) : displayData.picks.length ? (
            displayData.picks.map((pick, index) => (
              <PickCard key={pick.id} pick={pick} reward={rewardsByChannel.get(pick.channelId)} primary={index === 0} onTrack={record} />
            ))
          ) : (
            <div className="card card-pad empty-state rec-wide">
              <h4>暂无精选推荐</h4>
              <p>后台保存 TOP3 配置后会在这里显示。</p>
            </div>
          )}
        </section>

        <div className="section-head" id="ranks">
          <h2>
            多维度榜单 <span className="tag">按你的需求挑</span>
          </h2>
          <span className="sub">{isInitialSync ? "正在同步榜单配置" : "所有数据来自 TokHub 实测探测，综合榜复用 TOP 3 推荐"}</span>
        </div>
        <div className="rank-tabs">
          {!isInitialSync && rankRules.length ? (
            rankRules.map((rule) => (
              <button className={`rt ${tab === rule.metric ? "active" : ""}`} onClick={() => setTab(rule.metric)} key={rule.id || rule.metric}>
                {rule.label}
              </button>
            ))
          ) : (
            <span className="muted-time">{isInitialSync ? "后台榜单同步中" : "后台暂未启用榜单规则"}</span>
          )}
        </div>
        <div className="card board rank-board">
          <div className="dt-wrap">
            <table className="dt rec-table">
              <thead>
                <tr>
                  <th>#</th>
                  <th>中转站</th>
                  <th>综合状态</th>
                  <th>真实延迟 P95</th>
                  <th>30 天成功率</th>
                  <th>价格 / MTok</th>
                  <th>评分</th>
                  <th className="rank-official-head">官方入口</th>
                </tr>
              </thead>
              <tbody>
                {isInitialSync ? (
                  <tr>
                    <td colSpan={8}>
                      <div className="empty-state">
                        <h4>正在同步榜单</h4>
                        <p>读取后台榜单配置后再展示，当前页面不会先显示旧榜单再替换。</p>
                      </div>
                    </td>
                  </tr>
                ) : ranks.length ? (
                  ranks.map((pick, index) => {
                    const reward = rewardsByChannel.get(pick.channelId);
                    const ctaHref = recommendCTAHref(pick);
                    return (
                      <tr key={`${pick.id}-${tab}`}>
                        <td>
                          <span className="rk-num">{index + 1}</span>
                        </td>
                        <td>
                          <div className="u-cell">
                            <span className="av" style={{ background: pick.channel.mark || "var(--brand)" }}>
                              {initials(pick.channel.name || pick.title)}
                            </span>
                            <span className="nm">
                              {pick.title}
                              <small>{pick.channel.provider} · {pick.channel.model}</small>
                            </span>
                          </div>
                        </td>
                        <td>
                          <span className={`badge ${statusClass(pick.channel.status)} dot`}>
                            {pick.channel.statusLabel || pick.channel.status}
                          </span>
                        </td>
                        <td className="mono">{latencyLabel(pick)}</td>
                        <td className="mono">{successLabel(pick)}</td>
                        <td className="mono price-down">{priceLabel(pick)}</td>
                        <td className="mono">{pick.channel.score}</td>
                        <td className="rank-official-cell">
                          <div className="rank-official-entry">
                            <a className="try-btn" href={ctaHref} {...externalLinkProps(ctaHref)} onClick={() => void record("rank", pick.id, pick.channelId)}>
                              去体验
                            </a>
                            {reward ? <span className="reward-tag">{reward.rewardValue || "以官网为准"}</span> : null}
                          </div>
                        </td>
                      </tr>
                    );
                  })
                ) : (
                  <tr>
                    <td colSpan={8}>
                      <div className="empty-state">
                        <h4>暂无榜单数据</h4>
                        <p>后台配置推荐位或录入公开平台通道后会在这里显示。</p>
                      </div>
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
          <div className="rec-board-foot">
            <span>{isInitialSync ? "推荐配置同步中" : `共 ${displayData.stats.ranked} 个上榜中转站 · 与首页 TOP 3 保持一致`}</span>
            <a href="/dashboard">看完整监控总览</a>
          </div>
        </div>

        <div className="section-head" id="quick-try">
          <h2>
            创建个人网关 <span className="tag">真实 Key</span>
          </h2>
          <span className="sub">登录后用你自己的上游 API Key 生成专属中转入口，TokHub 不展示虚假的试用密钥</span>
        </div>
        <section className="quick-try">
          <TryCard step="1" title="登录后生成专属端点" lines={[["Endpoint", "在控制台创建网关后显示"], ["API Key", "创建成功时一次性展示"]]} />
          <div className="card try-card">
            <div className="try-step">2</div>
            <div className="try-h">粘到常用工具里</div>
            <div className="tool-pick">
              {recommendToolOptions.map((tool) => (
                <div className="tp" key={tool}>
                  <span className="tp-ico">{initials(tool)}</span>
                  <b>{tool}</b>
                  <small>OpenAI 兼容协议</small>
                </div>
              ))}
            </div>
          </div>
          <div className="card try-card">
            <div className="try-step">3</div>
            <div className="try-h">配置后再接入业务</div>
            <p>你可以先在推荐列表选择供应商，再把自己的 API Key 添加到个人控制台。活动、价格和条款以供应商官网实时页面为准。</p>
            <div className="try-rewards">
              {isInitialSync ? (
                <div className="empty-state">
                  <h4>推荐入口同步中</h4>
                  <p>同步完成后展示后台推荐入口。</p>
                </div>
              ) : displayData.rewards.slice(0, 3).map((reward) => (
                <div className="tr-r" key={reward.id}>
                  <span className="tr-ico">{initials(reward.providerName)}</span>
                  <div>
                    <b>{reward.providerName}</b>
                    <small>{reward.rewardValue}{reward.code ? ` · ${reward.code}` : ""}</small>
                  </div>
                </div>
              ))}
              {!isInitialSync && displayData.rewards.length === 0 ? (
                <div className="empty-state">
                  <h4>暂无接入说明</h4>
                  <p>后台配置推荐渠道后会在这里展示。</p>
                </div>
              ) : null}
            </div>
            <a className="btn btn-primary rec-full-action" href="/login">
              登录并创建个人网关 →
            </a>
          </div>
        </section>

        <div className="section-head" id="scenarios">
          <h2>
            看你怎么用 <span className="tag">场景对号入座</span>
          </h2>
          <span className="sub">{isInitialSync ? "正在同步后台场景推荐" : "场景卡片来自后台精选推荐配置，并关联真实通道状态"}</span>
        </div>
        <section className="scenarios">
          {isInitialSync ? (
            <div className="card card-pad empty-state rec-wide">
              <h4>正在同步场景推荐</h4>
              <p>读取后台场景配置后再展示。</p>
            </div>
          ) : displayData.scenarios.length ? (
            displayData.scenarios.map((scenario) => <ScenarioCard key={scenario.id} scenario={scenario} onTrack={record} />)
          ) : (
            <div className="card card-pad empty-state rec-wide">
              <h4>暂无场景推荐</h4>
              <p>后台配置场景化推荐后会在这里显示。</p>
            </div>
          )}
        </section>

        <div className="grid cols-2 rec-cert-grid">
          <div className="card card-pad" id="certify">
            <div className="module-title">
              凭什么靠谱？看认证逻辑 <span className="badge b-blue dot module-title-badge">透明可复核</span>
            </div>
            <div className="module-sub">{isInitialSync ? "TokHub 正在读取后台推荐配置，避免首屏显示过期内容" : "TokHub 是中立第三方监控平台，推荐页使用真实状态 API 和运营配置共同驱动"}</div>
            <div className="cert-steps">
              {certifySteps.map((item, index) => (
                <div className="cs" key={item}>
                  <span className="cs-n">{index + 1}</span>
                  <div>
                    <b>{item}</b>
                    <small>可通过后台与 Open API 复核。</small>
                  </div>
                </div>
              ))}
            </div>
          </div>
          <div className="card card-pad" id="api">
            <div className="module-title">
              开放 API 同步推荐状态 <span className="badge b-green dot module-title-badge">Phase 6</span>
            </div>
            <div className="module-sub">第三方站点通过 Site Key 调用 /v1/status/*，公开数据与前台监控一致。</div>
            <div className="try-code">
              <span className="tc-l">GET</span>
              <code>/v1/status/channels</code>
            </div>
            <div className="try-code">
              <span className="tc-l">GET</span>
              <code>/v1/status/incidents</code>
            </div>
            <a className="btn btn-ghost btn-sm" href="/recommend#api">查看公开 API 说明 →</a>
          </div>
        </div>
      </main>
      <Footer />
    </>
  );
}

type PickCardProps = {
  pick: RecommendPick;
  reward?: RecommendReward;
  primary: boolean;
  onTrack: TrackRecommendation;
};

function PickCard({ pick, reward, primary, onTrack }: PickCardProps) {
  const endpoint = pick.channel.endpoint || publicChannelPath(pick.channel);
  const ctaHref = recommendCTAHref(pick);
  const displayHref = displayRecommendHref(ctaHref || endpoint);
  const copyHref = ctaHref || endpoint;
  return (
    <article className={`rec-pick ${primary ? "primary" : ""}`}>
      <div className={`pick-ribbon ${primary ? "" : "r-gold"}`}>{pick.ribbon || "编辑精选"}</div>
      <div className="pick-head">
        <span className="pick-mark" style={{ background: pick.channel.mark || "var(--brand)" }}>{initials(pick.title)}</span>
        <div className="pick-meta">
          <h3>
            {pick.title} <span className={`badge ${statusClass(pick.channel.status)} dot`}>{pick.channel.statusLabel || "Healthy"}</span>
          </h3>
          <p>{pick.summary || `${pick.channel.provider} · ${pick.channel.model}`}</p>
        </div>
        <div className="pick-score">
          <div className="ring" style={{ background: `conic-gradient(var(--brand) ${Math.min(pick.channel.score * 3.6, 360)}deg,#eaf1ff 0)` }}>
            <span>{pick.channel.score}</span>
          </div>
        </div>
      </div>
      <div className="pick-body">
        <ul className="pick-points">
          {pick.points.map((point) => (
            <li key={point}>{point}</li>
          ))}
        </ul>
        <div className="pick-spec">
          <div className="ps"><i className="ps-d s-ok" /><span>L1 / L2 / L3 接入 TokHub 状态体系</span></div>
          <div className="ps"><i className="ps-d s-ok" /><span>{metricSummary(pick)}</span></div>
          {reward ? <div className="ps"><i className="ps-d s-ok" /><span>官方入口与接入说明以供应商页面为准</span></div> : null}
        </div>
      </div>
      <div className="pick-foot">
        <code className="rec-endpoint">
          <a className="rec-endpoint-link" href={copyHref} {...externalLinkProps(copyHref)} onClick={() => void onTrack("endpoint", pick.id, pick.channelId)}>
            {displayHref}
          </a>
          <button className="copy-mini" title="复制" onClick={() => void navigator.clipboard?.writeText(copyHref)}>⧉</button>
        </code>
        <a className="btn btn-primary btn-sm" href={ctaHref} {...externalLinkProps(ctaHref)} onClick={() => void onTrack("pick", pick.id, pick.channelId)}>
          {recommendCTALabel(pick.ctaLabel)}
        </a>
      </div>
    </article>
  );
}

function ScenarioCard({ scenario, onTrack }: { scenario: RecommendScenario; onTrack: TrackRecommendation }) {
  const channel = scenario.channel;
  const href = channel?.id ? publicChannelPath(channel) : "/recommend";
  const channelName = channel?.name || "未关联通道";
  return (
    <article className="card sc">
      <div className="sc-h">
        <span className={`sc-ico ${statusClass(channel?.status || "unknown")}`}>{scenario.icon || "AI"}</span>
        <h4>{scenario.title}</h4>
      </div>
      <p>{scenario.summary}</p>
      <div className="sc-pick">
        <span className="sc-mark" style={{ background: channel?.mark || "var(--brand)" }}>{initials(channelName)}</span>
        <div>
          <b>{channelName}</b>
          <small>{channel ? channelMetricSummary(channel) : "等待后台关联通道"}</small>
        </div>
        <a className="btn btn-ghost btn-sm" href={href} onClick={() => void onTrack("scenario", scenario.id, scenario.channelId)}>
          查看
        </a>
      </div>
    </article>
  );
}

function TryCard({ step, title, lines }: { step: string; title: string; lines: Array<[string, string]> }) {
  return (
    <div className="card try-card">
      <div className="try-step">{step}</div>
      <div className="try-h">{title}</div>
      <p>TokHub 使用你自己的上游凭据创建专属入口，适合先验证工具兼容性和基础网络链路。</p>
      {lines.map(([label, value]) => (
        <div className="try-code" key={label}>
          <span className="tc-l">{label}</span>
          <code>{value}</code>
          <button className="copy-mini" onClick={() => void navigator.clipboard?.writeText(value)}>⧉</button>
        </div>
      ))}
    </div>
  );
}

function TrustRow({ k, v }: { k: string; v: string }) {
  return (
    <div className="trust-row">
      <span className="trust-k">{k}</span>
      <span className="trust-v">{v}</span>
    </div>
  );
}

function Stat({ value, label }: { value: string; label: string }) {
  return (
    <div className="rs">
      <div className="rs-v">{value}</div>
      <div className="rs-l">{label}</div>
    </div>
  );
}

function latencyLabel(pick: RecommendPick) {
  return `${pick.channel.latencyP95Ms || 0}ms`;
}

function successLabel(pick: RecommendPick) {
  return `${(pick.channel.successRate || 0).toFixed(2)}%`;
}

function metricSummary(pick: RecommendPick) {
  return channelMetricSummary(pick.channel);
}

function channelMetricSummary(channel: RecommendPick["channel"]) {
  return `P95 ${channel.latencyP95Ms || 0}ms · 成功率 ${(channel.successRate || 0).toFixed(2)}%`;
}

function recommendCTAHref(pick: RecommendPick) {
  const href = cleanHref(pick.ctaUrl);
  if (href && !isLegacyRecommendCTAURL(href)) return href;
  const official = cleanHref(pick.channel.officialSiteUrl || "");
  return official || officialEntryURL(pick.channel.endpoint) || href || publicChannelPath(pick.channel);
}

function recommendCTALabel(value: string) {
  const label = (value || "").trim();
  return label && !["查看详情", "立即试用"].includes(label) ? label : defaultCTALabel;
}

function externalLinkProps(href: string) {
  return /^https?:\/\//i.test(href) ? { target: "_blank", rel: "noreferrer" } : {};
}

function cleanHref(value: string) {
  return (value || "").trim();
}

function displayRecommendHref(href: string) {
  const value = cleanHref(href);
  try {
    const parsed = new URL(value);
    const path = parsed.pathname === "/" ? "" : parsed.pathname.replace(/\/$/, "");
    return `${parsed.origin}${path}`;
  } catch {
    return value;
  }
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

function isLegacyRecommendCTAURL(href: string) {
  return href === "/login" || href.startsWith("/channels/");
}

function rankByTab(items: RecommendPick[], tab: RankTab) {
  const rows = [...items];
  if (tab === "speed") return rows.sort((a, b) => (a.channel.latencyP95Ms || 999999) - (b.channel.latencyP95Ms || 999999));
  if (tab === "price") return rows.sort((a, b) => priceNumber(a) - priceNumber(b));
  if (tab === "stable") return rows.sort((a, b) => (b.channel.successRate || 0) - (a.channel.successRate || 0));
  return rows.sort((a, b) => a.position - b.position);
}

function priceNumber(pick: RecommendPick) {
  const input = Number(pick.channel.inputPerMtok || 0);
  const output = Number(pick.channel.outputPerMtok || 0);
  if (input <= 0 && output <= 0) return Number.POSITIVE_INFINITY;
  return input + output;
}

function priceLabel(pick: RecommendPick) {
  const input = Number(pick.channel.inputPerMtok || 0);
  const output = Number(pick.channel.outputPerMtok || 0);
  if (input <= 0 && output <= 0) return "暂无价格";
  return `$${input.toFixed(2)} / $${output.toFixed(2)}`;
}

function statusClass(status: string) {
  if (status === "healthy") return "b-green";
  if (status === "degraded") return "b-amber";
  if (status === "auth_error" || status === "connectivity_down" || status === "functional_down") return "b-red";
  return "b-gray";
}

function initials(value: string) {
  return value
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0])
    .join("")
    .toUpperCase()
    .slice(0, 2) || "T";
}

function formatInt(value: number) {
  if (value >= 1000000) return `${(value / 1000000).toFixed(1)}M`;
  if (value >= 1000) return `${(value / 1000).toFixed(1)}K`;
  return String(value);
}

function dateLabel(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "已刷新";
  return date.toLocaleDateString("zh-CN", { month: "2-digit", day: "2-digit" }) + " 已刷新";
}
