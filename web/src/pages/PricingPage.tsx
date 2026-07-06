import type { CSSProperties } from "react";
import { useMemo, useState } from "react";
import { Footer } from "../components/Footer";
import { PublicNav } from "../components/PublicNav";
import {
  categoryLabels,
  priceRows,
  pricingScenarios,
  type ModelPrice,
  type PriceCategory,
  type PricingScenario
} from "../data/modelPricing";

function modelMonthlyCost(row: ModelPrice, scenario: PricingScenario) {
  const normalInputTokens = scenario.inputTokens * (1 - scenario.cacheHitRate / 100);
  const cachedInputTokens = scenario.inputTokens * (scenario.cacheHitRate / 100);
  const inputCost = normalInputTokens * row.input;
  const cacheCost = cachedInputTokens * (row.cacheRead ?? row.input);
  const outputCost = scenario.outputTokens * row.output;
  return ((inputCost + cacheCost + outputCost) * scenario.dailyRequests * 30) / 1_000_000;
}

function formatUSD(value: number) {
  if (value >= 1000) return `$${Math.round(value).toLocaleString("en-US")}`;
  if (value >= 100) return `$${value.toFixed(0)}`;
  if (value >= 10) return `$${value.toFixed(1)}`;
  return `$${value.toFixed(2)}`;
}

function formatPrice(value?: number) {
  if (value === undefined) return "-";
  return `$${value.toFixed(value < 1 ? 3 : 2)}`;
}

function formatVolume(value: number) {
  if (value >= 1000) return `${Math.round(value).toLocaleString("en-US")}`;
  if (value >= 100) return value.toFixed(0);
  if (value >= 10) return value.toFixed(1);
  return value.toFixed(2);
}

export function PricingPage() {
  const [provider, setProvider] = useState("all");
  const [category, setCategory] = useState<PriceCategory | "all">("all");
  const [query, setQuery] = useState("");
  const [scenarioId, setScenarioId] = useState(pricingScenarios[0].id);
  const [dailyRequests, setDailyRequests] = useState(pricingScenarios[0].dailyRequests);
  const [inputTokens, setInputTokens] = useState(pricingScenarios[0].inputTokens);
  const [outputTokens, setOutputTokens] = useState(pricingScenarios[0].outputTokens);
  const [cacheHitRate, setCacheHitRate] = useState(pricingScenarios[0].cacheHitRate);

  const providers = useMemo(() => Array.from(new Set(priceRows.map((row) => row.provider))).sort(), []);
  const activeScenario = useMemo(
    () => ({
      ...(pricingScenarios.find((item) => item.id === scenarioId) ?? pricingScenarios[0]),
      dailyRequests,
      inputTokens,
      outputTokens,
      cacheHitRate
    }),
    [cacheHitRate, dailyRequests, inputTokens, outputTokens, scenarioId]
  );

  const pricedRows = useMemo(() => {
    const normalizedQuery = query.trim().toLowerCase();
    return priceRows
      .filter((row) => provider === "all" || row.provider === provider)
      .filter((row) => category === "all" || row.category === category)
      .filter((row) => {
        if (!normalizedQuery) return true;
        return [row.provider, row.family, row.model, row.modelId, row.note].some((value) => value.toLowerCase().includes(normalizedQuery));
      })
      .map((row) => ({ row, monthlyCost: modelMonthlyCost(row, activeScenario) }))
      .sort((a, b) => a.monthlyCost - b.monthlyCost);
  }, [activeScenario, category, provider, query]);

  const baseline = pricedRows.find((item) => item.row.modelId === "gpt-4.1-mini")?.monthlyCost ?? pricedRows[0]?.monthlyCost ?? 1;
  const cheapest = pricedRows[0];
  const median = pricedRows[Math.floor(pricedRows.length / 2)] ?? cheapest;
  const monthlyInputVolume = formatVolume((dailyRequests * inputTokens * 30) / 1_000_000);
  const monthlyOutputVolume = formatVolume((dailyRequests * outputTokens * 30) / 1_000_000);
  const medianSpread = median ? `${(median.monthlyCost / Math.max(cheapest?.monthlyCost ?? 1, 0.01)).toFixed(1)}x` : "-";
  const frontierRows = pricedRows.filter((item) => item.row.category === "frontier").slice(0, 3);
  const economyRows = pricedRows.filter((item) => item.row.category === "economy").slice(0, 3);

  function applyScenario(nextId: string) {
    const next = pricingScenarios.find((item) => item.id === nextId) ?? pricingScenarios[0];
    setScenarioId(next.id);
    setDailyRequests(next.dailyRequests);
    setInputTokens(next.inputTokens);
    setOutputTokens(next.outputTokens);
    setCacheHitRate(next.cacheHitRate);
  }

  return (
    <>
      <PublicNav />
      <main className="pricing-workbench">
        <section className="pricing-console-shell" aria-labelledby="pricing-title">
          <div className="pricing-console-head">
            <div className="pricing-title-block">
              <span className="pricing-kicker">
                <i /> 成本预估基准 <b>·</b> 核对至 2026-06-26
              </span>
              <h1 id="pricing-title">模型成本预估</h1>
              <p>
                用每日请求量、平均输入输出 Token 和缓存命中率先预估月度账单，再把官方输入价、输出价、缓存价和上下文窗口放回同一张表里比较。
                这里适合做接入前的成本基准判断，最终报价仍需要以供应商官网和你的真实用量为准。
              </p>
            </div>
            <div className="pricing-meta-strip" aria-label="价格数据范围">
              <span><b>{providers.length}</b> 供应商</span>
              <span><b>{priceRows.length}</b> 价格行</span>
              <span><b>{pricedRows.length}</b> 当前匹配</span>
            </div>
          </div>

          <div className="pricing-console-grid">
            <section className="pricing-planner-panel" aria-label="成本估算参数">
              <div className="pricing-panel-title">
                <span>估算参数</span>
                <strong>{activeScenario.name}</strong>
              </div>
              <div className="pricing-scenario-tabs" aria-label="快速场景">
                {pricingScenarios.map((item) => (
                  <button
                    className={item.id === scenarioId ? "active" : ""}
                    key={item.id}
                    type="button"
                    onClick={() => applyScenario(item.id)}
                  >
                    {item.name}
                  </button>
                ))}
              </div>
              <div className="pricing-controls pricing-controls-console" aria-label="价格筛选和成本参数">
                <NumberControl
                  id="daily-requests"
                  label="每日请求"
                  value={dailyRequests}
                  min={1}
                  step={100}
                  onChange={setDailyRequests}
                />
                <NumberControl
                  id="input-tokens"
                  label="平均输入"
                  value={inputTokens}
                  min={1}
                  step={100}
                  onChange={setInputTokens}
                />
                <NumberControl
                  id="output-tokens"
                  label="平均输出"
                  value={outputTokens}
                  min={1}
                  step={50}
                  onChange={setOutputTokens}
                />
                <NumberControl
                  id="cache-rate"
                  label="缓存命中 %"
                  value={cacheHitRate}
                  min={0}
                  max={100}
                  step={5}
                  onChange={setCacheHitRate}
                />
              </div>
              <div className="pricing-volume-grid" aria-label="当前估算摘要">
                <PricingMetric label="月输入量" value={monthlyInputVolume} suffix="MToken" />
                <PricingMetric label="月输出量" value={monthlyOutputVolume} suffix="MToken" />
                <PricingMetric label="中位 / 最低" value={medianSpread} />
              </div>
              <div className="pricing-planner-note">
                <b>{activeScenario.description}</b>
                <span>估算公式：普通输入 + 缓存输入 + 输出，再乘以每日请求和 30 天。</span>
              </div>
            </section>

            <aside className="pricing-result-panel" aria-label="当前成本结果">
              <div className="pricing-result-primary">
                <span>最低官方月成本</span>
                <strong>{cheapest ? formatUSD(cheapest.monthlyCost) : "-"}</strong>
                <small>{cheapest?.row.provider} · {cheapest?.row.model}</small>
              </div>
              <div className="pricing-result-lines">
                <SnapshotLine label="当前场景" value={activeScenario.name} />
                <SnapshotLine label="中位月成本" value={median ? formatUSD(median.monthlyCost) : "-"} />
                <SnapshotLine label="价格表范围" value={`${providers.length} 家 / ${priceRows.length} 条`} />
              </div>
              <div className="pricing-quick-rank">
                <span>低价候选</span>
                {pricedRows.slice(0, 3).map(({ row, monthlyCost }, index) => (
                  <div key={`quick-${row.modelId}`}>
                    <b>{index + 1}</b>
                    <em>{row.provider}</em>
                    <strong>{formatUSD(monthlyCost)}</strong>
                  </div>
                ))}
              </div>
            </aside>
          </div>
        </section>

        <div className="section-head pricing-section-head">
          <h2>
            官方价格表 <span className="tag">per 1M tokens</span>
          </h2>
          <span className="sub">按当前参数实时估算，默认从最低月成本开始排序</span>
        </div>

        <section className="pricing-filter-row card" aria-label="表格筛选">
          <select value={provider} onChange={(event) => setProvider(event.target.value)} aria-label="供应商">
            <option value="all">全部供应商</option>
            {providers.map((item) => (
              <option value={item} key={item}>
                {item}
              </option>
            ))}
          </select>
          <select value={category} onChange={(event) => setCategory(event.target.value as PriceCategory | "all")} aria-label="模型类型">
            <option value="all">全部类型</option>
            {Object.entries(categoryLabels).map(([key, value]) => (
              <option value={key} key={key}>
                {value}
              </option>
            ))}
          </select>
          <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索模型、供应商、用途" />
          <button
            className="btn btn-ghost btn-sm"
            type="button"
            onClick={() => {
              setProvider("all");
              setCategory("all");
              setQuery("");
            }}
          >
            清空筛选
          </button>
        </section>

        <section className="card board pricing-board">
          <div className="dt-wrap">
            <table className="dt pricing-table">
              <thead>
                <tr>
                  <th>模型</th>
                  <th>类型</th>
                  <th>输入价</th>
                  <th>输出价</th>
                  <th>缓存读</th>
                  <th>上下文</th>
                  <th>估算月成本</th>
                  <th>价格指数</th>
                  <th>来源</th>
                </tr>
              </thead>
              <tbody>
                {pricedRows.map(({ row, monthlyCost }) => (
                  <tr key={`${row.provider}-${row.modelId}`}>
                    <td>
                      <div className="pricing-model-cell">
                        <span className="pricing-provider-mark" style={providerMarkStyle(row.provider)}>{providerInitial(row.provider)}</span>
                        <span>
                          <b>{row.model}</b>
                          <small>{row.provider} · {row.modelId}</small>
                        </span>
                      </div>
                    </td>
                    <td>
                      <span className={`pricing-type ${row.category}`}>{categoryLabels[row.category]}</span>
                    </td>
                    <td className="mono">{formatPrice(row.input)}</td>
                    <td className="mono pricing-output-price">{formatPrice(row.output)}</td>
                    <td className="mono">{formatPrice(row.cacheRead)}</td>
                    <td>
                      <div className="pricing-context">
                        <b>{row.context}</b>
                        <small>输出 {row.maxOutput}</small>
                      </div>
                    </td>
                    <td className="mono pricing-monthly">{formatUSD(monthlyCost)}</td>
                    <td>
                      <PriceIndex value={monthlyCost / Math.max(baseline, 0.01)} />
                    </td>
                    <td>
                      <a className="pricing-source-link" href={row.source} target="_blank" rel="noreferrer">
                        官方价
                      </a>
                    </td>
                  </tr>
                ))}
                {pricedRows.length === 0 ? (
                  <tr>
                    <td colSpan={9}>
                      <div className="empty-state">
                        <h4>没有匹配的模型价格</h4>
                        <p>调整供应商、类型或搜索词后再试。</p>
                      </div>
                    </td>
                  </tr>
                ) : null}
              </tbody>
            </table>
          </div>
          <div className="pricing-board-foot">
            <span>输入价、输出价、缓存读均为 USD / 1M tokens。估算不含税、最低消费、区域价、搜索/工具/图片等附加费用。</span>
          </div>
        </section>

        <section className="pricing-rank-grid" aria-label="价格排行">
          <RankPanel title="当前场景低价优先" rows={pricedRows.slice(0, 4)} />
          <RankPanel title="旗舰模型成本对比" rows={frontierRows.length ? frontierRows : pricedRows.slice(0, 4)} />
          <RankPanel title="高频任务候选" rows={economyRows.length ? economyRows : pricedRows.slice(0, 4)} />
        </section>

        <div className="section-head pricing-section-head">
          <h2>
            读表口径 <span className="tag">避免误判</span>
          </h2>
          <span className="sub">价格不能单独决定选择，输出比例和稳定性会改变实际账单</span>
        </div>
        <section className="pricing-notes">
          <NoteCard title="先看输出价" text="写作、代码和推理任务常常输出很长，输出价通常比输入价更决定总成本。" />
          <NoteCard title="缓存命中不是免费" text="Anthropic、OpenAI、Gemini、DeepSeek 等都有缓存读价或缓存规则，命中率越高，长上下文任务越划算。" />
          <NoteCard title="长上下文可能分层" text="Gemini 等供应商会按提示词长度切换价格，表内默认采用短上下文层级并在备注里说明。" />
          <NoteCard title="TokHub 后续可接实测" text="官方价负责成本基准，TokHub 的 L1/L2/L3 探测负责成功率、P95 延迟和可用性判断。" />
        </section>

        <div className="section-head pricing-section-head">
          <h2>
            官方来源 <span className="tag">可追溯</span>
          </h2>
          <span className="sub">第一版为静态数据，后续建议由后台维护来源 URL 和生效时间</span>
        </div>
        <section className="pricing-source-grid">
          {providers.map((item) => {
            const source = priceRows.find((row) => row.provider === item);
            return source ? (
              <a className="pricing-source-card" href={source.source} target="_blank" rel="noreferrer" key={item}>
                <span className="pricing-provider-mark pricing-source-mark" style={providerMarkStyle(item)}>{providerInitial(item)}</span>
                <div>
                  <b>{item}</b>
                  <small>最后核对 {source.checkedAt}</small>
                </div>
              </a>
            ) : null;
          })}
        </section>
      </main>
      <Footer />
    </>
  );
}

type NumberControlProps = {
  id: string;
  label: string;
  value: number;
  min: number;
  max?: number;
  step: number;
  onChange: (value: number) => void;
};

function NumberControl({ id, label, value, min, max, step, onChange }: NumberControlProps) {
  return (
    <div className="pricing-control-group">
      <label htmlFor={id}>{label}</label>
      <input
        id={id}
        type="number"
        min={min}
        max={max}
        step={step}
        value={value}
        onChange={(event) => onChange(Number(event.target.value || min))}
      />
    </div>
  );
}

function SnapshotLine({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <span>{label}</span>
      <b>{value}</b>
    </div>
  );
}

function PricingMetric({ value, label, suffix }: { value: string; label: string; suffix?: string }) {
  return (
    <div className="pricing-metric">
      <span>{label}</span>
      <b>{value}</b>
      {suffix ? <small>{suffix}</small> : null}
    </div>
  );
}

function PriceIndex({ value }: { value: number }) {
  const capped = Math.min(100, Math.max(5, value * 24));
  return (
    <span className="pricing-index" style={{ "--w": `${capped}%` } as CSSProperties}>
      <i />
      <b>{value.toFixed(2)}x</b>
    </span>
  );
}

function RankPanel({ title, rows }: { title: string; rows: Array<{ row: ModelPrice; monthlyCost: number }> }) {
  return (
    <div className="card pricing-rank-panel">
      <h3>{title}</h3>
      {rows.map(({ row, monthlyCost }, index) => (
        <div className="pricing-rank-row" key={`${title}-${row.modelId}`}>
          <span className="pricing-rank-num">{index + 1}</span>
          <div>
            <b>{row.model}</b>
            <small>{row.provider} · {categoryLabels[row.category]}</small>
          </div>
          <strong>{formatUSD(monthlyCost)}</strong>
        </div>
      ))}
    </div>
  );
}

function NoteCard({ title, text }: { title: string; text: string }) {
  return (
    <div className="card pricing-note-card">
      <b>{title}</b>
      <p>{text}</p>
    </div>
  );
}

function providerInitial(provider: string) {
  if (provider === "Alibaba Cloud") return "Q";
  return provider.slice(0, 1).toUpperCase();
}

function providerMarkStyle(provider: string) {
  return { "--provider-ink": providerInk(provider) } as CSSProperties;
}

function providerInk(provider: string) {
  const colors: Record<string, string> = {
    Anthropic: "#8a5a35",
    Cohere: "#3956a8",
    DeepSeek: "#2563eb",
    Google: "#1a73e8",
    Mistral: "#c2410c",
    OpenAI: "#087f5b",
    xAI: "#111827"
  };
  return colors[provider] ?? "#1f2937";
}
