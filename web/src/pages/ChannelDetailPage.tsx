import { useEffect, useMemo, useState, type MouseEvent } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { Footer } from "../components/Footer";
import { PublicNav } from "../components/PublicNav";
import { adminProbeNow, currentUser, disableAdminChannel, publicChannel, publicChannelPath, publicChannelSeries, PublicChannel, PublicChannelDetail, SeriesPoint, User } from "../lib/api";

const statusClass: Record<string, string> = {
  healthy: "b-green",
  degraded: "b-amber",
  functional_down: "b-magenta",
  connectivity_down: "b-red",
  auth_error: "b-red",
  disabled: "b-gray",
  unknown: "b-gray"
};

export function ChannelDetailPage() {
  const { channelID = "ch_cc_claude" } = useParams();
  const navigate = useNavigate();
  const [detail, setDetail] = useState<PublicChannelDetail | null>(null);
  const [series, setSeries] = useState<SeriesPoint[]>([]);
  const [user, setUser] = useState<User | null>(null);
  const [range, setRange] = useState(30);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  useEffect(() => {
    let active = true;
    currentUser({ force: true })
      .then((value) => {
        if (active) setUser(value);
      })
      .catch(() => {
        if (active) setUser(null);
      });
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    let active = true;
    setLoading(true);
    setError("");
    Promise.all([publicChannel(channelID), publicChannelSeries(channelID, range)])
      .then(([detailValue, seriesValue]) => {
        if (!active) return;
        setDetail(detailValue);
        setSeries(seriesValue.items);
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
  }, [channelID, range]);

  const current = detail?.channel;
  const canAdminManage = user?.role === "owner" || user?.role === "admin";
  const quote = useMemo(() => {
    if (!series.length) return { change: 0, min: 0, max: 0 };
    const latest = series[series.length - 1].healthIndex;
    const previous = series[Math.max(series.length - 2, 0)].healthIndex;
    return {
      change: +(latest - previous).toFixed(1),
      min: Math.min(...series.map((item) => item.healthIndex)),
      max: Math.max(...series.map((item) => item.healthIndex))
    };
  }, [series]);
  const officialHref = current ? externalHTTPHref(current.officialSiteUrl) : "";
  const officialHost = officialHref ? hostLabel(officialHref) : "";
  const endpointHost = current ? hostLabel(current.endpoint) : "";
  const detailPath = current ? publicChannelPath(current) : `/channels/${channelID}`;
  const intro = current ? channelIntroContent(current, officialHost, endpointHost) : null;

  useEffect(() => {
    if (!current?.publicSlug || channelID === current.publicSlug) return;
    navigate(publicChannelPath(current), { replace: true });
  }, [channelID, current, navigate]);

  async function refreshDetail() {
    const [detailValue, seriesValue] = await Promise.all([publicChannel(channelID), publicChannelSeries(channelID, range)]);
    setDetail(detailValue);
    setSeries(seriesValue.items);
  }

  async function runProbeNow() {
    if (!canAdminManage || !current) return;
    setActionLoading("probe");
    setError("");
    setNotice("");
    try {
      const payload = await adminProbeNow(current.id);
      setDetail((value) => value ? { ...value, channel: payload.channel } : value);
      await refreshDetail().catch(() => undefined);
      setNotice("已触发真实探测，结果会同步到当前详情和后台通道列表。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "触发探测失败");
    } finally {
      setActionLoading("");
    }
  }

  async function pauseProbe() {
    if (!canAdminManage || !current) return;
    if (!window.confirm(`确认暂停 ${current.name} 的真实探测和公开展示？该通道也会从网关候选中移除。`)) return;
    setActionLoading("pause");
    setError("");
    setNotice("");
    try {
      const payload = await disableAdminChannel(current.id);
      setDetail((value) => value ? { ...value, channel: payload.channel } : value);
      setNotice("平台通道已暂停，并已从公开看板和网关候选中隐藏。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "暂停真实探测失败");
    } finally {
      setActionLoading("");
    }
  }

  return (
    <>
      <PublicNav />
      <div className="subnav channel-detail-subnav">
        <div className="subnav-inner channel-detail-subnav-inner">
          <div className="channel-detail-backline">
            <a className="back" href="/dashboard">← 监控总览</a>
          </div>
          <div className="channel-detail-toolbar">
            {current ? (
              <div className="dv-head">
                <span className="mark" style={{ background: current.mark }}>{current.provider[0]}</span>
                <div>
                  <h1>{current.name} <span className="chan">{current.provider}</span> <span className={`badge ${statusClass[current.status] ?? "b-gray"} dot`}>{current.statusLabel}</span></h1>
                  <div className="sub">{current.model} · 上游 {current.upstreamModel} · 最后探测 {timeLabel(current.lastProbeAt)}</div>
                </div>
              </div>
            ) : <div className="dv-head"><h1>通道详情</h1></div>}
            <div className="detail-actions">
              <div className="seg">
                {[7, 30, 90].map((item) => (
                  <button className={range === item ? "active" : ""} onClick={() => setRange(item)} key={item}>{item}天</button>
                ))}
              </div>
              {officialHref ? (
                <a
                  className="btn btn-primary btn-sm official-entry-btn"
                  href={officialHref}
                  target="_blank"
                  rel="noopener noreferrer"
                  title={officialHost ? `打开 ${officialHost} 官方入口` : "打开官方入口"}
                >
                  官方注册 / 体验
                </a>
              ) : null}
              {canAdminManage ? (
                <>
                  <button className="btn btn-ghost btn-sm" type="button" disabled={!current || current.status === "disabled" || actionLoading === "pause"} onClick={() => void pauseProbe()}>
                    {actionLoading === "pause" ? "暂停中..." : "暂停真实探测"}
                  </button>
                  <button className="btn btn-primary btn-sm" type="button" disabled={!current || actionLoading === "probe"} onClick={() => void runProbeNow()}>
                    {actionLoading === "probe" ? "探测中..." : "立即探测"}
                  </button>
                </>
              ) : user ? (
                <a className="btn btn-primary btn-sm" href="/console">进入用户控制台</a>
              ) : (
                <a className="btn btn-primary btn-sm" href={`/login?next=${encodeURIComponent(detailPath)}`}>登录后管理</a>
              )}
            </div>
          </div>
        </div>
      </div>

      <main className="page public-detail-page">
        {error ? <div className="form-error">{error}</div> : null}
        {notice ? <div className="form-notice">{notice}</div> : null}
        {loading ? <div className="card card-pad">正在加载通道详情…</div> : null}
        {detail && current ? (
          <>
            <section className={`diagnosis-banner ${diagnosisClass(current.diagnosis?.severity)}`} aria-label="监控诊断">
              <div>
                <b>{current.diagnosis?.label || current.statusLabel}</b>
                <span>{current.diagnosis?.hint || "系统已根据最近 L1/L2/L3 探测结果生成当前判断。"}</span>
              </div>
              <span className="diagnosis-code">{current.diagnosis?.code || current.status}</span>
            </section>
            <section className="card card-pad channel-intro-card" aria-labelledby="channel-intro-title">
              <div className="channel-intro-main">
                <div className="channel-intro-brandline">
                  <ChannelLogo channel={current} />
                  <div>
                    <div className="module-title" id="channel-intro-title">{intro?.title}</div>
                    <div className="module-sub">{intro?.summary}</div>
                  </div>
                </div>
              </div>
              <div className="channel-intro-layout">
                <div className="channel-intro-copy">
                  <RichIntro text={intro?.body ?? ""} />
                  {intro?.highlights.length ? (
                    <ul className="channel-intro-highlights">
                      {intro.highlights.map((item, index) => <li key={`${item}-${index}`}>{item}</li>)}
                    </ul>
                  ) : null}
                  <div className="channel-intro-ctas">
                    {officialHref ? (
                      <a
                        className="btn btn-primary btn-sm"
                        href={officialHref}
                        target="_blank"
                        rel="noopener noreferrer"
                        title={officialHost ? `打开 ${officialHost} 官方入口` : "打开官方入口"}
                      >
                        去官方注册体验
                      </a>
                    ) : null}
                    <a className="btn btn-ghost btn-sm" href="#tokhub-index" onClick={scrollToTokHubIndex}>查看最新监控数据</a>
                  </div>
                </div>
                <dl className="channel-intro-facts">
                  <div><dt>服务商</dt><dd>{current.provider}</dd></div>
                  <div><dt>模型</dt><dd>{current.model}</dd></div>
                  <div><dt>上游模型</dt><dd>{current.upstreamModel}</dd></div>
                  <div><dt>兼容类型</dt><dd>{current.type.toUpperCase()}</dd></div>
                  <div><dt>官方入口</dt><dd>{officialHost || "暂未配置"}</dd></div>
                  <div><dt>接入域名</dt><dd>{endpointHost || "暂未识别"}</dd></div>
                  <div><dt>公开短地址</dt><dd>{detailPath}</dd></div>
                  <div><dt>当前状态</dt><dd>{current.statusLabel}</dd></div>
                  <div><dt>诊断</dt><dd>{current.diagnosis?.label || "等待诊断"}</dd></div>
                </dl>
              </div>
            </section>

            <div className="card detail-quote-card" id="tokhub-index">
              <div className="quote">
                <div className="q-main">
                  <div className="q-cap">综合健康指数 · TokHub Index</div>
                  <div className="q-index">
                    {current.score.toFixed(1)}
                    <span className="chg" style={{ color: quote.change >= 0 ? "var(--green)" : "var(--red)" }}>
                      {quote.change >= 0 ? "▲" : "▼"} {Math.abs(quote.change).toFixed(1)} 今日
                    </span>
                  </div>
                  <div className="q-sub">近{range}天最高 <b>{quote.max.toFixed(1)}</b> · 最低 <b>{quote.min.toFixed(1)}</b> · 综合链路与生成可用性</div>
                </div>
                <div className="ticker">
                  <Ticker label="真实成功率" value={`${current.successRate.toFixed(1)}%`} />
                  <Ticker label="P95 延迟" value={`${current.latencyP95Ms}ms`} />
                  <Ticker label="24h 可用率" value={`${current.uptime24h.toFixed(1)}%`} />
                  <Ticker label="今日成本" value={`$${current.costUsd.toFixed(3)}`} />
                  <Ticker label="探测 Tokens" value={compact(current.tokensUsed)} />
                  <Ticker label="状态" value={current.statusLabel} />
                </div>
              </div>
              <div className="chart-head">
                <div className="ct">可用率指数走势 <span className="pill">近{range}天</span></div>
                <div className="legend">
                  <span><i style={{ background: "var(--brand)" }} />可用率指数</span>
                  <span><i style={{ background: "var(--green)" }} />真实成功率</span>
                </div>
              </div>
              <LineChart series={series} />
            </div>

            <div className="dkpis detail-dkpis">
              <DKPI label="综合健康指数" value={current.score.toFixed(1)} hint={quote.change >= 0 ? "今日回升" : "今日回落"} />
              <DKPI label="真实调用成功率" value={`${current.successRate.toFixed(1)}%`} hint="近30天窗口" />
              <DKPI label="P95 真实延迟" value={`${current.latencyP95Ms}ms`} hint="最新真实探测窗口" />
              <DKPI label="24h 可用率" value={`${current.uptime24h.toFixed(1)}%`} hint={current.uptime24h >= 99 ? "达标" : "低于 SLA"} />
              <DKPI label="连续正常时长" value="暂无足够数据" hint="等待事件聚合" />
              <DKPI label="今日探测成本" value={`$${current.costUsd.toFixed(3)}`} hint={`${compact(current.tokensUsed)} tokens`} />
            </div>

            <div className="grid cols-2 detail-grid">
              <div className="card card-pad">
                <div className="module-title">基础监控 · L1 / L2</div>
                <div className="module-sub">最近一次链路探测瀑布与证书状态</div>
                <Waterfall layers={detail.layers} />
              </div>
              <div className="card card-pad">
                <div className="module-title">真实监控 · L3</div>
                <div className="module-sub">真实生成探测成功率与内容校验</div>
                <div className="l3-grid">
                  <div className="donut" style={{ background: `conic-gradient(var(--green) ${detail.l3.successRate * 3.6}deg,#eef0f4 0)` }}>
                    <div className="hole"><b>{detail.l3.successRate.toFixed(1)}%</b><small>真实成功率</small></div>
                  </div>
                  <div className="l3-stats">
                    <Stat label="content 校验" value={`${detail.l3.contentValidRate.toFixed(1)}%`} />
                    <Stat label="首 token" value={`${detail.l3.firstTokenMs}ms`} />
                    <Stat label="tokens/sec" value={`${detail.l3.tokensPerSecond}`} />
                    <Stat label="单次 tokens" value={`${detail.l3.averageTokens}`} />
                  </div>
                </div>
              </div>
            </div>

            <div className="grid cols-2 detail-grid">
              <div className="card card-pad">
                <div className="module-title">最近探测记录</div>
                <div className="module-sub">基础与真实探测的逐条结果</div>
                <div className="scroll">
                  <table className="rec">
                    <thead><tr><th>时间</th><th>层级</th><th>类型</th><th>HTTP</th><th>延迟</th><th>结果</th></tr></thead>
                    <tbody>
                      {detail.recentRecords.map((record, index) => (
                        <tr key={`${record.time}-${index}`}>
                          <td className="t">{timeLabel(record.time)}</td>
                          <td><span className="pill">{record.layer}</span></td>
                          <td className="t">{record.type}</td>
                          <td className="num">{record.httpCode || "—"}</td>
                          <td className="num">{record.latencyMs ? `${record.latencyMs}ms` : "—"}</td>
                          <td><span className={`badge ${record.result === "ok" ? "b-green" : "b-red"} dot`}>{record.errorType || record.result}</span></td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
              <div className="grid detail-side">
                <div className="card card-pad">
                  <div className="module-title">错误类型分布</div>
                  <div className="module-sub">该通道近30天失败探测</div>
                  <ErrorBars errors={detail.errors} />
                </div>
                <div className="card card-pad">
                  <div className="module-title">探测 Token 成本（近7天）</div>
                  <div className="module-sub">按真实生成探测累计</div>
                  <CostBars costs={detail.costs} />
                </div>
              </div>
            </div>
          </>
        ) : null}
      </main>
      <Footer />
    </>
  );
}

function Ticker({ label, value }: { label: string; value: string }) {
  return <div className="ti"><span className="l">{label}</span><span className="v">{value}</span></div>;
}

function ChannelLogo({ channel }: { channel: PublicChannel }) {
  const [failed, setFailed] = useState(false);
  if (channel.logoUrl && !failed) {
    return <img className="channel-intro-logo" src={channel.logoUrl} alt={`${channel.provider} logo`} referrerPolicy="no-referrer" onError={() => setFailed(true)} />;
  }
  return <span className="channel-intro-logo fallback" style={{ background: channel.mark }}>{channel.provider[0]}</span>;
}

function RichIntro({ text }: { text: string }) {
  const blocks = text.trim().split(/\n{2,}/).map((block) => block.trim()).filter(Boolean);
  if (!blocks.length) return null;
  return (
    <>
      {blocks.map((block, index) => {
        const lines = block.split("\n").map((line) => line.trim()).filter(Boolean);
        const listLines = lines.filter((line) => /^[-*]\s+/.test(line));
        if (listLines.length && listLines.length === lines.length) {
          return (
            <ul className="channel-intro-rich-list" key={`${block}-${index}`}>
              {listLines.map((line, lineIndex) => <li key={`${lineIndex}-${line}`}>{renderInlineStrong(line.replace(/^[-*]\s+/, ""))}</li>)}
            </ul>
          );
        }
        return <p key={`${block}-${index}`}>{renderInlineStrong(lines.join(" "))}</p>;
      })}
    </>
  );
}

function renderInlineStrong(text: string) {
  return text.split(/(\*\*[^*]+\*\*)/g).map((part, index) => {
    if (part.startsWith("**") && part.endsWith("**") && part.length > 4) {
      return <strong key={`${part}-${index}`}>{part.slice(2, -2)}</strong>;
    }
    return <span key={`${part}-${index}`}>{part}</span>;
  });
}

function scrollToTokHubIndex(event: MouseEvent<HTMLAnchorElement>) {
  const target = document.getElementById("tokhub-index");
  if (!target) return;
  event.preventDefault();
  target.scrollIntoView({ behavior: "smooth", block: "start" });
  history.replaceState(null, "", "#tokhub-index");
}

function channelIntroContent(channel: PublicChannel, officialHost: string, endpointHost: string) {
  const title = channel.introTitle?.trim() || `${channel.name} 官方介绍`;
  const summary = channel.introSummary?.trim() || "基于官网入口、通道配置和 TokHub 真实监控数据整理";
  const body = channel.introBody?.trim() || fallbackChannelIntroBody(channel, officialHost, endpointHost);
  const highlights = channel.introHighlights?.length ? channel.introHighlights : fallbackChannelHighlights(channel, officialHost, endpointHost);
  return { title, summary, body, highlights };
}

function fallbackChannelIntroBody(channel: PublicChannel, officialHost: string, endpointHost: string) {
  const official = officialHost || "官方站点";
  const endpoint = endpointHost || "当前公开 Endpoint";
  return `**${channel.name}** 是 TokHub 收录的 ${channel.model} API 中转站服务商入口，当前关联服务商为 **${channel.provider}**，主要面向 ${channel.type.toUpperCase()} 兼容协议和 ${channel.upstreamModel} 上游模型场景。

这个详情页把官网入口、模型信息、协议类型、Endpoint 域名和真实探测结果整理在同一处，方便开发者在注册或接入前先完成可用性判断。官方侧可通过 ${official} 进入注册或体验，TokHub 侧持续记录 L1 DNS/TCP/TLS/HTTP、L2 模型列表与鉴权、L3 最小生成调用。

对于正在评估 ${endpoint} 的团队，可以先查看 ${channel.score} 分综合指数、${channel.uptime24h.toFixed(1)}% 24H 可用率、${channel.successRate.toFixed(1)}% 真实生成成功率和 ${channel.latencyP95Ms}ms P95 延迟，再决定是否进入官网注册、加入个人关注、配置私有 API Key 或放入企业网关候选。`;
}

function fallbackChannelHighlights(channel: PublicChannel, officialHost: string, endpointHost: string) {
  return [
    `服务商：${channel.provider}`,
    `模型：${channel.model}`,
    `上游模型：${channel.upstreamModel}`,
    `兼容类型：${channel.type.toUpperCase()}`,
    officialHost ? `官方入口：${officialHost}` : "",
    endpointHost ? `接入域名：${endpointHost}` : ""
  ].filter(Boolean);
}

function DKPI({ label, value, hint }: { label: string; value: string; hint: string }) {
  return <div className="dkpi"><div className="l">{label}</div><div className="v">{value}</div><div className="d">{hint}</div></div>;
}

function LineChart({ series }: { series: SeriesPoint[] }) {
  const path = linePath(series.map((item) => item.healthIndex), 1000, 250);
  const success = linePath(series.map((item) => item.successRate), 1000, 250);
  return (
    <div className="chart-wrap">
      <svg viewBox="0 0 1000 250" preserveAspectRatio="none" className="detail-line">
        <g className="chart-grid">
          {[0, 25, 50, 75, 100].map((value) => {
            const y = 230 - value * 2.05;
            return <g key={value}><line x1="20" y1={y} x2="980" y2={y} /><text x="20" y={y - 4}>{value}</text></g>;
          })}
        </g>
        <path d={`${path} L980,230 L20,230 Z`} className="area" />
        <path d={success} className="success-line" />
        <path d={path} className="health-line" />
      </svg>
    </div>
  );
}

function Waterfall({ layers }: { layers: PublicChannelDetail["layers"] }) {
  const max = Math.max(...layers.map((item) => item.latencyMs), 120);
  let offset = 0;
  return (
    <div className="fall">
      {layers.map((layer) => {
        const left = offset;
        offset += layer.latencyMs;
        return (
          <div className="f" key={layer.name}>
            <span className="fl">{layer.name}</span>
            <span className="ft">
              <i className={layer.status} style={{ left: `${(left / max) * 100}%`, width: `${Math.max((layer.latencyMs / max) * 100, layer.status === "na" ? 0 : 2)}%` }} />
            </span>
            <span className="fv">{layer.latencyMs ? `${layer.latencyMs}ms` : layer.status}</span>
          </div>
        );
      })}
    </div>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return <div className="stat-line"><span>{label}</span><b>{value}</b></div>;
}

function ErrorBars({ errors }: { errors: PublicChannelDetail["errors"] }) {
  const max = Math.max(...errors.map((item) => item.count), 1);
  return (
    <div className="errlist">
      {errors.map((item) => (
        <div className="er" key={item.type}>
          <span className="et">{item.label}</span>
          <span className="eb"><i style={{ width: `${(item.count / max) * 100}%` }} /></span>
          <span className="ev">{item.count}</span>
        </div>
      ))}
    </div>
  );
}

function CostBars({ costs }: { costs: PublicChannelDetail["costs"] }) {
  const max = Math.max(...costs.map((item) => item.costUsd), 0.01);
  return (
    <div className="bars-mini detail-cost-bars">
      {costs.map((item) => (
        <i key={item.date} style={{ height: `${Math.max((item.costUsd / max) * 100, 8)}%` }} title={`${item.date} $${item.costUsd.toFixed(3)}`} />
      ))}
    </div>
  );
}

function linePath(values: number[], width: number, height: number) {
  if (!values.length) return "";
  const max = 100;
  const min = 0;
  return values
    .map((value, index) => {
      const x = 20 + (index / Math.max(values.length - 1, 1)) * (width - 40);
      const y = 20 + (1 - (value - min) / (max - min)) * (height - 40);
      return `${index === 0 ? "M" : "L"}${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(" ");
}

function timeLabel(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function hostLabel(href: string) {
  try {
    return new URL(href).hostname.replace(/^www\./, "");
  } catch {
    return "";
  }
}

function externalHTTPHref(value?: string) {
  try {
    const parsed = new URL((value || "").trim());
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") return "";
    return parsed.href;
  } catch {
    return "";
  }
}

function diagnosisClass(severity?: string) {
  if (severity === "error") return "is-error";
  if (severity === "warn") return "is-warn";
  if (severity === "ok") return "is-ok";
  return "is-info";
}

function compact(value: number) {
  if (value >= 1000) return `${(value / 1000).toFixed(1)}K`;
  return String(value);
}
