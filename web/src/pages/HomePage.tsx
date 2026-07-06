import type { CSSProperties } from "react";
import { useEffect, useMemo, useState } from "react";
import { Footer } from "../components/Footer";
import { PublicNav } from "../components/PublicNav";
import {
  currentUser,
  publicChannelPath,
  publicOverview,
  publicRecommend,
  type PublicOverview,
  type RecommendConfig,
  type RecommendPick,
  type User
} from "../lib/api";

const statusClass: Record<string, string> = {
  healthy: "b-green",
  degraded: "b-amber",
  functional_down: "b-magenta",
  connectivity_down: "b-red",
  auth_error: "b-red",
  unknown: "b-gray"
};
const defaultRecommendCTALabel = "去官方体验";

export function HomePage() {
  const [overview, setOverview] = useState<PublicOverview | null>(null);
  const [recommend, setRecommend] = useState<RecommendConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [user, setUser] = useState<User | null>(null);

  useEffect(() => {
    let active = true;
    setLoading(true);
    Promise.allSettled([publicOverview(), publicRecommend()])
      .then(([overviewResult, recommendResult]) => {
        if (!active) return;
        if (overviewResult.status === "fulfilled") {
          setOverview(overviewResult.value);
        }
        if (recommendResult.status === "fulfilled") {
          setRecommend(recommendResult.value);
        }
        if (overviewResult.status === "rejected" && recommendResult.status === "rejected") {
          setError("公开数据暂时不可用，监控总览和推荐页仍可直接访问。");
        }
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    let active = true;
    currentUser()
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

  const picks = useMemo(() => (recommend?.picks ?? []).filter((item) => item.enabled).slice(0, 3), [recommend?.picks]);
  const overviewLoading = loading && !overview;
  const recommendLoading = loading && !recommend;
  const totalChannels = overview?.total ?? 0;
  const healthRate = overview?.healthyRate ?? 0;
  const p95 = overview?.p95LatencySeconds ?? 0;
  const updatedAt = overview ? timeLabel(overview.updatedAt) : loading ? "同步中" : "暂无数据";
  const isPlatformAdmin = user?.role === "owner" || user?.role === "admin";

  return (
    <>
      <PublicNav />
      <section className="home-hero-shell" aria-labelledby="home-title">
        <section className="home-hero" aria-labelledby="home-title">
          <div className="home-hero-copy">
            <span className="home-kicker">
              <i /> AI API 中转站监控与选择入口 <b>·</b> {updatedAt}
            </span>
            <h1 id="home-title">
              先看可用性，<span className="hl">再选择中转站</span>
            </h1>
            <p className="home-lead">
              TokHub 把公开中转站、精选推荐和个人专属网关放在同一条判断链路里。先看真实监控，再决定关注、试用或接入自己的 API Key
            </p>
            <div className="home-actions">
              <a className="btn btn-primary" href="/dashboard">
                查看监控总览 →
              </a>
              <a className="btn btn-ghost" href="/pricing">
                成本预估
              </a>
              <a className="btn btn-ghost" href="/recommend">
                精选推荐
              </a>
              <a className="btn btn-ghost" href="/console">
                我的工作区
              </a>
              {isPlatformAdmin ? (
                <a className="btn btn-ghost" href="/admin">
                  平台管理
                </a>
              ) : null}
            </div>
            <div className="home-proof-row" aria-label="TokHub 当前公开监控指标">
              <HomeProof value={overview ? String(totalChannels) : "—"} label="公开通道" loading={overviewLoading} width="4ch" />
              <HomeProof value={overview ? `${healthRate.toFixed(1)}%` : "—"} label="健康率" loading={overviewLoading} width="6ch" />
              <HomeProof value={overview ? `${p95.toFixed(2)}s` : "—"} label="真实延迟" loading={overviewLoading} width="6ch" />
            </div>
          </div>

          <div className="home-monitor-card" aria-label="实时监控快照">
            <div className="hm-head">
              <span className="hm-live">✓ 实时监控快照</span>
              <span>{loading ? "同步中" : updatedAt}</span>
            </div>
            <div className="hm-score">
              <div>
                <span className="hm-score-label">综合健康率</span>
                <b><MetricValue value={overview ? `${healthRate.toFixed(1)}%` : "—"} loading={overviewLoading} width="6ch" /></b>
              </div>
              <div className={`hm-ring ${overviewLoading ? "is-loading" : ""}`} style={{ "--p": `${overview ? Math.max(0, Math.min(100, healthRate)) : 0}%` } as CSSProperties}>
                <span className="hm-ring-value"><MetricValue value={overview ? String(overview.healthy) : "—"} loading={overviewLoading} width="2ch" /></span>
              </div>
            </div>
            <div className="hm-layers">
              <LayerBadge label="L1" title="基础链路" text="DNS / TLS" />
              <LayerBadge label="L2" title="模型入口" text="鉴权" />
              <LayerBadge label="L3" title="真实生成" text="Token" strong />
            </div>
            <div className="hm-metrics">
              <MetricLine label="功能性故障" value={overview ? `${overview.functionalDown}` : "—"} loading={overviewLoading} tone="magenta" width="2ch" />
              <MetricLine label="连通异常" value={overview ? `${overview.connectivityDown}` : "—"} loading={overviewLoading} tone="red" width="2ch" />
              <MetricLine label="今日探测" value={overview ? `${compact(overview.probeRunsToday)} 次` : "—"} loading={overviewLoading} tone="blue" width="5ch" />
            </div>
          </div>
        </section>
      </section>

      <main className="page home-page" id="main-content">
        {error ? <div className="form-error home-error">{error}</div> : null}

        <section className="home-stats">
          <HomeStat value={overview ? `${totalChannels}` : "—"} label="公开监控通道" loading={overviewLoading} width="4ch" />
          <HomeStat value={overview ? `${healthRate.toFixed(1)}%` : "—"} label="综合健康率" loading={overviewLoading} width="6ch" />
          <HomeStat value={overview ? `${p95.toFixed(2)}s` : "—"} label="真实调用延迟" loading={overviewLoading} width="6ch" />
          <HomeStat value={recommend ? `${recommend.stats.picks}` : "—"} label="精选推荐位" loading={recommendLoading} width="4ch" />
          <HomeStat value={overview ? `${compact(overview.probeRunsToday)}` : "—"} label="今日探测次数" loading={overviewLoading} width="5ch" />
        </section>

        <div className="section-head">
          <h2>
            桌面工作台入口 <span className="tag">可安装</span>
          </h2>
          <span className="sub">安装到 Dock 或 Chrome 应用后，仍从同一个入口分流到公开前台、我的工作区和平台管理</span>
        </div>
        <section className="home-entry-grid" aria-label="核心入口">
          <EntryCard
            eyebrow="公开监控"
            title="监控总览"
            text="查看全部公开通道、状态矩阵、供应商排行和错误分布，适合先判断哪家正在稳定。"
            href="/dashboard"
            cta="进入监控"
            meta={`${totalChannels || "多"} 个通道`}
          />
          <EntryCard
            eyebrow="成本判断"
            title="成本预估"
            text="按真实请求量和 Token 结构估算月度账单，再对照主流模型的输入、输出、缓存和上下文价格。"
            href="/pricing"
            cta="估算成本"
            meta="模型成本"
          />
          <EntryCard
            eyebrow="运营推荐"
            title="精选推荐"
            text="把监控评分、速度、成功率、价格和接入入口合在一起，减少从榜单到试用之间的判断成本。"
            href="/recommend"
            cta="查看推荐"
            meta={`${recommend?.stats.picks ?? 3} 个精选位`}
          />
          <EntryCard
            eyebrow="个人工作区"
            title="用户控制台"
            text="关注公开通道，添加自己的私有 API Key，创建专属中转站并查看工作区用量和告警。"
            href="/console"
            cta="打开控制台"
            meta="私有通道"
          />
          {isPlatformAdmin ? (
            <EntryCard
              eyebrow="平台运营"
              title="平台管理"
              text="管理平台通道、推荐配置、开放 API、用户组织、用量、告警和审计，保持运营与治理分层。"
              href="/admin"
              cta="进入后台"
              meta="Owner/Admin"
            />
          ) : null}
        </section>

        <section className="home-feature-band" aria-labelledby="features-title">
          <div className="section-head">
            <h2 id="features-title">
              监控亮点 <span className="tag">真实 Token 探测</span>
            </h2>
            <span className="sub">不是只看入口能不能打开，而是看模型能不能真实返回</span>
          </div>
          <div className="home-feature-grid">
            <FeatureItem title="基础链路和真实生成分层" text="L1/L2 负责入口和鉴权，L3 用最小请求验证模型真的可用，避免 HTTP 200 但业务不可用。" />
            <FeatureItem title="公开看板和个人关注联动" text="游客能看公开状态，登录后可以把重点通道加入关注，异常优先回到自己的控制台。" />
            <FeatureItem title="精选推荐可运营" text="后台维护 TOP3、榜单规则、场景推荐和官方入口，前台推荐与监控数据保持同一套可信来源。" />
            <FeatureItem title="专属中转站闭环" text="用户用自己的上游 Key 创建私有通道，再组合为专属网关，调用、成本、告警和审计留在工作区。" />
          </div>
        </section>

        <section className="home-recommend-panel" aria-labelledby="home-recommend-title">
          <div className="section-head">
            <h2 id="home-recommend-title">
              精选推荐预览 <span className="tag">TOP 3</span>
            </h2>
            <span className="sub">首页给出可行动的选择，完整榜单交给精选推荐页</span>
            <a className="btn btn-ghost btn-sm" href="/recommend">查看完整推荐</a>
          </div>
          <div className="home-pick-grid">
            {picks.length ? (
              picks.map((pick, index) => <HomePickCard key={pick.id} pick={pick} index={index} />)
            ) : (
              fallbackPicks.map((pick, index) => <FallbackPickCard key={pick.title} pick={pick} index={index} />)
            )}
          </div>
        </section>

        <section className="home-flow-section" aria-labelledby="flow-title">
          <div className="section-head">
            <h2 id="flow-title">
              使用流程 <span className="tag">4 步闭环</span>
            </h2>
            <span className="sub">从公开判断到个人接入，保持一条路径</span>
          </div>
          <div className="home-flow">
            <FlowStep n="01" title="看监控总览" text="用状态、延迟、成功率和错误类型排除不稳定通道。" />
            <FlowStep n="02" title="参考精选推荐" text="按速度、稳定性、价格和使用场景缩小选择范围。" />
            <FlowStep n="03" title="关注或添加私有通道" text="登录后收藏公开通道，或填入自己的 Endpoint 和 API Key。" />
            <FlowStep n="04" title="生成专属网关" text="把可用通道收敛成统一 OpenAI 兼容入口，后续用量和告警都在控制台看。" />
          </div>
        </section>

        <section className="home-faq-section" aria-labelledby="home-faq-title">
          <div className="home-faq-heading">
            <h2 id="home-faq-title">常见问题</h2>
            <p>关于公开监控、精选推荐、专属网关和工作区接入，先看这里能不能直接解答。</p>
          </div>
          <div className="home-faq-list">
            {homeFaqs.map((item, index) => (
              <FAQItem key={item.q} question={item.q} answer={item.a} defaultOpen={index === 0} />
            ))}
          </div>
        </section>
      </main>
      <Footer />
    </>
  );
}

function HomeProof({ value, label, loading = false, width }: { value: string; label: string; loading?: boolean; width?: string }) {
  return (
    <div className="home-proof-item">
      <b><MetricValue value={value} loading={loading} width={width} /></b>
      <small>{label}</small>
    </div>
  );
}

function HomeStat({ value, label, loading = false, width }: { value: string; label: string; loading?: boolean; width?: string }) {
  return (
    <div className="rs home-stat">
      <div className="rs-v"><MetricValue value={value} loading={loading} width={width} /></div>
      <div className="rs-l">{label}</div>
    </div>
  );
}

function LayerBadge({ label, title, text, strong = false }: { label: string; title: string; text: string; strong?: boolean }) {
  return (
    <div className={`hm-layer ${strong ? "strong" : ""}`}>
      <span>{label}</span>
      <div>
        <b>{title}</b>
        <small>{text}</small>
      </div>
    </div>
  );
}

function MetricLine({ label, value, loading = false, tone, width }: { label: string; value: string; loading?: boolean; tone: "magenta" | "red" | "blue"; width?: string }) {
  return (
    <div className={`hm-line ${tone}`}>
      <span className="hm-line-label">{label}</span>
      <b><MetricValue value={value} loading={loading} width={width} /></b>
    </div>
  );
}

function MetricValue({ value, loading = false, width }: { value: string; loading?: boolean; width?: string }) {
  const style = width ? ({ "--metric-width": width } as CSSProperties) : undefined;
  return (
    <span
      className={`metric-value ${loading ? "is-loading" : ""}`}
      style={style}
      aria-busy={loading}
      aria-label={loading ? "加载中" : value}
    >
      {loading ? <i aria-hidden="true" /> : value}
    </span>
  );
}

function EntryCard({ eyebrow, title, text, href, cta, meta }: { eyebrow: string; title: string; text: string; href: string; cta: string; meta: string }) {
  return (
    <a className="home-entry-card" href={href}>
      <span className="home-entry-eyebrow">{eyebrow}</span>
      <div>
        <h2>{title}</h2>
        <p>{text}</p>
      </div>
      <div className="home-entry-foot">
        <span>{meta}</span>
        <b>{cta}</b>
      </div>
    </a>
  );
}

function FeatureItem({ title, text }: { title: string; text: string }) {
  return (
    <article className="home-feature-item">
      <h3>{title}</h3>
      <p>{text}</p>
    </article>
  );
}

function HomePickCard({ pick, index }: { pick: RecommendPick; index: number }) {
  const channel = pick.channel;
  const href = recommendCTAHref(pick);
  return (
    <article className="home-pick-card">
      <div className="home-pick-top">
        <span className="home-rank">{index + 1}</span>
        <span className={`badge ${statusClass[channel?.status || "unknown"] ?? "b-gray"} dot`}>
          {channel?.statusLabel || "待配置"}
        </span>
      </div>
      <h3>{pick.title}</h3>
      <p>{pick.summary}</p>
      {pick.points.length ? (
        <ul className="home-pick-points">
          {pick.points.map((point) => (
            <li key={point}>{point}</li>
          ))}
        </ul>
      ) : null}
      <div className="home-pick-metrics">
        <span>
          <b>{channel?.score ?? "-"}</b>评分
        </span>
        <span>
          <b>{channel?.latencyP95Ms ?? 0}ms</b>P95
        </span>
        <span>
          <b>{(channel?.successRate ?? 0).toFixed(1)}%</b>成功率
        </span>
      </div>
      <a href={href} className="home-pick-link" {...externalLinkProps(href)}>
        {recommendCTALabel(pick.ctaLabel)}
      </a>
    </article>
  );
}

function recommendCTAHref(pick: RecommendPick) {
  const href = cleanHref(pick.ctaUrl);
  if (href && !isLegacyRecommendCTAURL(href)) return href;
  const official = cleanHref(pick.channel.officialSiteUrl || "");
  return official || officialEntryURL(pick.channel.endpoint) || href || publicChannelPath(pick.channel);
}

function recommendCTALabel(value: string) {
  const label = (value || "").trim();
  return label && !["查看详情", "立即试用"].includes(label) ? label : defaultRecommendCTALabel;
}

function externalLinkProps(href: string) {
  return /^https?:\/\//i.test(href) ? { target: "_blank", rel: "noreferrer" } : {};
}

function cleanHref(value: string) {
  return (value || "").trim();
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

function FallbackPickCard({ pick, index }: { pick: { title: string; text: string; metric: string }; index: number }) {
  return (
    <article className="home-pick-card muted">
      <div className="home-pick-top">
        <span className="home-rank">{index + 1}</span>
        <span className="badge b-gray dot">待同步</span>
      </div>
      <h3>{pick.title}</h3>
      <p>{pick.text}</p>
      <div className="home-pick-metrics">
        <span>
          <b>{pick.metric}</b>推荐信号
        </span>
      </div>
      <a href="/recommend" className="home-pick-link">
        进入推荐页
      </a>
    </article>
  );
}

function FlowStep({ n, title, text }: { n: string; title: string; text: string }) {
  return (
    <article className="home-flow-step">
      <span>{n}</span>
      <h3>{title}</h3>
      <p>{text}</p>
    </article>
  );
}

function FAQItem({ question, answer, defaultOpen }: { question: string; answer: string | string[]; defaultOpen?: boolean }) {
  const paragraphs = Array.isArray(answer) ? answer : [answer];

  return (
    <details className="home-faq-item" open={defaultOpen}>
      <summary>
        <span>{question}</span>
        <i aria-hidden="true" />
      </summary>
      <div className="home-faq-answer">
        {paragraphs.map((paragraph, index) => (
          <p key={index}>{paragraph}</p>
        ))}
      </div>
    </details>
  );
}

function compact(value: number) {
  return new Intl.NumberFormat("zh-CN", { notation: "compact", maximumFractionDigits: 1 }).format(value);
}

function timeLabel(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "实时";
  return date.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

const fallbackPicks = [
  { title: "稳定优先", text: "适合生产业务和企业网关，重点看 24h 可用率、真实成功率和功能性故障次数。", metric: "L3" },
  { title: "速度优先", text: "适合交互式开发工具，重点看 P95 延迟、首 Token 和慢响应率。", metric: "P95" },
  { title: "成本优先", text: "适合批量任务和低频调用，重点看价格倍数、Token 成本和额度风险。", metric: "Cost" }
];

const homeFaqs = [
  {
    q: "TokHub 首页主要解决什么问题？",
    a: [
      "首页是公开用户的第一入口，核心任务不是把所有数据一次性塞出来，而是帮用户快速判断：这个平台能不能帮我筛选更稳定的 AI API 中转站。",
      "它把路径拆成三步：先看公开监控，确认哪些通道当前可用；再看精选推荐，按稳定性、速度和成本缩小范围；最后进入个人控制台，把自己的 API Key 或私有通道接进来。",
      "这样做的好处是用户不需要一开始就理解全部后台能力，先完成“看状态、做选择、再接入”的基本判断。"
    ]
  },
  {
    q: "监控总览和首页有什么区别？",
    a: [
      "首页更像导航和判断入口，负责说明 TokHub 能解决什么问题，并把用户引到监控、推荐、成本预估和工作区。",
      "监控总览是数据看板，重点展示通道健康状态、L1/L2/L3 探测、P95 延迟、成功率、错误分类和供应商表现。用户想快速了解产品时看首页，想比较具体通道时看监控总览。",
      "两者的关系是先粗后细：首页帮用户建立判断框架，监控总览提供真实数据依据。"
    ]
  },
  {
    q: "精选推荐是人工主观推荐吗？",
    a: [
      "精选推荐不是纯人工拍脑袋排序。推荐位可以由后台运营维护，但页面上的评分、成功率、P95 延迟、状态标签和场景说明都会参考真实监控数据。",
      "人工运营主要负责补充上下文，比如这个通道更适合开发调试、批量任务、企业网关，或者官方入口是否稳定可访问。监控数据负责约束推荐结果，避免只靠主观印象。",
      "用户看到推荐时，可以同时知道“为什么被推荐”和“当前监控表现如何”，更容易做取舍。"
    ]
  },
  {
    q: "L1、L2、L3 分别代表什么？",
    a: [
      "L1 是基础链路探测，检查 DNS、TCP、TLS、HTTP 这些入口层能力。它回答的是“这个地址能不能连上”，成本低、频率高，不需要消耗 Token。",
      "L2 是模型入口和鉴权探测，通常会检查 API Key 是否有效、模型列表能不能返回、目标模型是否存在。它回答的是“入口看起来能不能提供模型服务”。",
      "L3 是真实生成探测，会用最小 prompt 发起一次真实 API 调用，并校验内容、延迟、首 Token、Token 数和错误类型。它回答的是“业务上是不是真的能生成结果”。",
      "三层合在一起的优势是能区分不同故障：L1 失败多半是连通问题，L2 失败常见于鉴权或模型入口问题，L3 失败则可能是入口正常但模型不可用、内容为空、超时或配额异常。"
    ]
  },
  {
    q: "普通用户能看到平台 API Key 吗？",
    a: [
      "不能。公开监控只展示通道名称、状态、延迟、成功率和错误类型，不会把平台用于探测的上游 API Key 暴露给普通用户。",
      "如果用户要建立自己的专属中转入口，需要在个人工作区添加自己的私有通道和 API Key。TokHub 只负责加密存储、探测配额、状态展示和网关路由，不会在页面上回显明文 Key。",
      "这样既能保留公开监控的参考价值，也能把平台通道和用户私有通道隔离开。"
    ]
  },
  {
    q: "我可以只用 TokHub 做公开监控吗？",
    a: [
      "可以。游客不登录也能查看公开页面，了解哪些中转站通道健康、哪些通道延迟高、哪些出现连通异常或功能性故障。",
      "登录后的价值会更多一些：你可以关注重点通道，把自己常用的服务放进个人控制台，后续更方便回看状态变化和异常记录。",
      "如果暂时不需要专属网关，也可以只把 TokHub 当成公开状态页和选型参考。"
    ]
  },
  {
    q: "专属网关适合什么场景？",
    a: [
      "专属网关适合已经在用多个上游通道，但希望对业务侧只暴露一个 OpenAI 兼容入口的团队或个人。应用只接一个网关地址，背后再由 TokHub 管理上游选择。",
      "它可以按最低延迟、最高成功率或成本优先等策略规划路由，并把用量、成本、告警和审计集中到同一个工作区里。",
      "典型场景包括企业内部 AI 工具、Claude Code 或 Cursor 的统一入口、多个供应商之间的故障切换，以及需要控制 API Key 权限和调用成本的团队环境。"
    ]
  },
  {
    q: "监控数据多久更新一次？",
    a: [
      "更新频率取决于部署实例和探测配置。通常 L1/L2 这种低成本探测可以更频繁，L3 因为会产生真实 Token 消耗，会按更谨慎的频率和配额执行。",
      "页面会展示最近更新时间，公开 API 也会返回当前聚合后的状态。用户判断通道是否可用时，应优先看最近更新时间、连续异常次数和真实生成成功率。",
      "如果某个通道刚刚恢复或刚刚故障，前台数据会以最近一次探测结果为准，后续探测会继续修正状态。"
    ]
  }
];
