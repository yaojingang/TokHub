import { FormEvent, useEffect, useMemo, useState } from "react";
import {
  adminChannels,
  probeLogDetail,
  ProbeLogDetail,
  probeLogExportURL,
  ProbeLogItem,
  probeLogs,
  ProbeLogResult
} from "../lib/api";
import { Button, CheckboxField, Drawer, FilterBar, Pagination, SelectField, StatGrid, StatusBadge } from "../ui";

type ProbeFilters = {
  range: "24h" | "7d" | "30d";
  provider: string;
  channelId: string;
  layer: string;
  status: string;
  httpStatus: string;
  errorType: string;
  source: string;
  onlyAbnormal: boolean;
  page: number;
  pageSize: 25 | 50 | 100;
};

type ProbeExportRange = "current" | "24h" | "7d" | "30d" | "all";

const emptyFilters: ProbeFilters = {
  range: "24h",
  provider: "",
  channelId: "",
  layer: "",
  status: "",
  httpStatus: "",
  errorType: "",
  source: "",
  onlyAbnormal: false,
  page: 1,
  pageSize: 50
};

const emptyResult: ProbeLogResult = {
  items: [],
  total: 0,
  page: 1,
  pageSize: 50,
  summary: { total: 0, abnormal: 0, authErrors: 0, slowResponses: 0 }
};

export function ProbeLogsPanel() {
  const initial = useMemo(() => initialProbeFilters(), []);
  const [filters, setFilters] = useState<ProbeFilters>(initial);
  const [appliedFilters, setAppliedFilters] = useState<ProbeFilters>(initial);
  const [result, setResult] = useState<ProbeLogResult>(emptyResult);
  const [channels, setChannels] = useState<Array<{ id: string; name: string; provider: string }>>([]);
  const [selected, setSelected] = useState<ProbeLogItem | null>(null);
  const [detail, setDetail] = useState<ProbeLogDetail | null>(null);
  const [exportRange, setExportRange] = useState<ProbeExportRange>("current");
  const [loading, setLoading] = useState(true);
  const [detailLoading, setDetailLoading] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    void load(initial);
    void adminChannels().then((response) => {
      setChannels(response.items.map((item) => ({ id: item.id, name: item.name, provider: item.provider })));
    }).catch(() => undefined);
  }, [initial]);

  async function load(nextFilters: ProbeFilters) {
    const cleaned = cleanProbeFilters(nextFilters);
    setLoading(true);
    setError("");
    try {
      const response = await probeLogs(cleaned);
      setResult(response);
      setAppliedFilters(cleaned);
      setFilters(cleaned);
      syncProbeFiltersToURL(cleaned);
    } catch (err) {
      setError(err instanceof Error ? err.message : "无法加载探测日志");
    } finally {
      setLoading(false);
    }
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    await load({ ...filters, page: 1 });
  }

  async function openDetail(item: ProbeLogItem) {
    setSelected(item);
    setDetail(null);
    setDetailLoading(true);
    setError("");
    try {
      setDetail(await probeLogDetail(item.id));
    } catch (err) {
      setError(err instanceof Error ? err.message : "无法加载探测详情");
    } finally {
      setDetailLoading(false);
    }
  }

  function exportCSV() {
    const range = exportRange === "current" ? appliedFilters.range : exportRange;
    if (range === "all" && !window.confirm("确认导出全部历史探测日志？数据量可能较大，下载时间取决于历史记录数量。")) return;
    window.location.href = probeLogExportURL({
      range,
      provider: appliedFilters.provider,
      channelId: appliedFilters.channelId,
      layer: appliedFilters.layer,
      status: appliedFilters.status,
      httpStatus: appliedFilters.httpStatus,
      errorType: appliedFilters.errorType,
      source: appliedFilters.source,
      onlyAbnormal: appliedFilters.onlyAbnormal || undefined
    });
  }

  const providers = useMemo(() => Array.from(new Set(channels.map((item) => item.provider).filter(Boolean))).sort(), [channels]);
  const visibleChannels = useMemo(() => channels.filter((item) => !filters.provider || item.provider === filters.provider), [channels, filters.provider]);
  const totalPages = Math.max(1, Math.ceil(result.total / Math.max(1, result.pageSize)));
  const stats = [
    { label: "匹配探测", value: String(result.summary.total), hint: rangeLabel(appliedFilters.range) },
    { label: "异常探测", value: String(result.summary.abnormal), hint: "警告、失败与认证异常", tone: result.summary.abnormal ? "red" as const : "gray" as const },
    { label: "认证异常", value: String(result.summary.authErrors), hint: "401 / 403 / auth_error", tone: result.summary.authErrors ? "amber" as const : "gray" as const },
    { label: "慢响应", value: String(result.summary.slowResponses), hint: "超过通道告警阈值", tone: result.summary.slowResponses ? "amber" as const : "gray" as const },
    { label: "最近探测", value: result.summary.latestAt ? shortTime(result.summary.latestAt) : "-", hint: result.summary.latestAt ? dateLabel(result.summary.latestAt) : "暂无记录" }
  ];

  return (
    <section className="probe-log-panel" aria-label="探测日志">
      {error ? <div className="form-error">{error}</div> : null}
      <StatGrid items={stats} className="audit-stat-row probe-log-stats" />

      <div className="phase14-risk-note probe-log-note">
        状态码和错误摘要来自探测服务器的真实上游请求。错误摘要仅提取允许字段并脱敏，不保存 API Key、Authorization、请求正文或成功响应正文。
      </div>

      <FilterBar as="form" className="probe-log-filter-toolbar" onSubmit={(event) => void submit(event)}>
        <SelectField value={filters.range} onChange={(event) => setFilters({ ...filters, range: event.target.value as ProbeFilters["range"] })} aria-label="时间范围">
          <option value="24h">近 24 小时</option>
          <option value="7d">近 7 天</option>
          <option value="30d">近 30 天</option>
        </SelectField>
        <SelectField value={filters.provider} onChange={(event) => setFilters({ ...filters, provider: event.target.value, channelId: "" })} aria-label="服务商">
          <option value="">全部服务商</option>
          {providers.map((provider) => <option value={provider} key={provider}>{provider}</option>)}
        </SelectField>
        <SelectField value={filters.channelId} onChange={(event) => setFilters({ ...filters, channelId: event.target.value })} aria-label="通道">
          <option value="">全部通道</option>
          {visibleChannels.map((channel) => <option value={channel.id} key={channel.id}>{channel.name}</option>)}
        </SelectField>
        <SelectField value={filters.layer} onChange={(event) => setFilters({ ...filters, layer: event.target.value })} aria-label="探测层级">
          <option value="">全部层级</option>
          <option value="l1">L1 基础连通</option>
          <option value="l2">L2 模型列表</option>
          <option value="l3">L3 真实生成</option>
        </SelectField>
        <SelectField value={filters.status} onChange={(event) => setFilters({ ...filters, status: event.target.value })} aria-label="探测结果">
          <option value="">全部结果</option>
          <option value="ok">成功</option>
          <option value="warn">警告</option>
          <option value="auth_error">认证失败</option>
          <option value="down">不可用</option>
          <option value="failed">探测中断</option>
          <option value="na">未执行</option>
          <option value="running">执行中</option>
        </SelectField>
        <input className="input" inputMode="numeric" value={filters.httpStatus} onChange={(event) => setFilters({ ...filters, httpStatus: event.target.value.replace(/\D/g, "").slice(0, 3) })} placeholder="HTTP 状态码" aria-label="HTTP 状态码" />
        <input className="input" value={filters.errorType} onChange={(event) => setFilters({ ...filters, errorType: event.target.value })} placeholder="错误类型，如 auth_error" aria-label="错误类型" />
        <SelectField value={filters.source} onChange={(event) => setFilters({ ...filters, source: event.target.value })} aria-label="探测来源">
          <option value="">全部来源</option>
          <option value="scheduler">定时任务</option>
          <option value="manual">手动探测</option>
          <option value="admin_validate">通道验证</option>
        </SelectField>
        <CheckboxField wrapperClassName="probe-log-abnormal" label="仅看异常" checked={filters.onlyAbnormal} onChange={(event) => setFilters({ ...filters, onlyAbnormal: event.target.checked })} />
        <SelectField value={filters.pageSize} onChange={(event) => setFilters({ ...filters, pageSize: Number(event.target.value) as ProbeFilters["pageSize"] })} aria-label="每页条数">
          <option value={25}>25 条</option>
          <option value={50}>50 条</option>
          <option value={100}>100 条</option>
        </SelectField>
        <Button type="submit" variant="primary" size="sm" disabled={loading}>筛选</Button>
        <Button size="sm" type="button" onClick={() => void load(emptyFilters)}>重置</Button>
      </FilterBar>

      <div className="probe-log-export-bar" aria-label="导出探测日志">
        <div className="probe-log-export-copy">
          <b>导出探测日志</b>
          <small>保留当前通道与异常筛选，可单独选择导出时间范围</small>
        </div>
        <SelectField value={exportRange} onChange={(event) => setExportRange(event.target.value as ProbeExportRange)} aria-label="导出范围">
          <option value="current">当前筛选范围（{rangeLabel(appliedFilters.range)}）</option>
          <option value="24h">近 24 小时</option>
          <option value="7d">近 7 天</option>
          <option value="30d">近 30 天</option>
          <option value="all">全部历史</option>
        </SelectField>
        <Button size="sm" variant="primary" onClick={exportCSV}>导出 CSV</Button>
      </div>

      <div className="card board probe-log-board">
        <div className="dt-wrap tk-data-table-wrap">
          <table className="dt tk-data-table probe-log-table">
            <thead><tr>
              <th>时间</th><th>通道</th><th>层级</th><th>步骤</th><th>结果</th><th>HTTP 状态</th><th className="tk-align-end">耗时</th><th className="tk-align-end">详情</th>
            </tr></thead>
            <tbody>
              {loading ? <tr><td colSpan={8}>正在加载探测日志...</td></tr> : result.items.length ? result.items.map((item) => (
                <tr key={item.id} className={isAbnormal(item.status) ? "probe-log-row-abnormal" : undefined}>
                  <td><span className="mono probe-log-time">{dateTimeLabel(item.startedAt)}</span><small>{sourceLabel(item.source)}</small></td>
                  <td><div className="probe-log-channel"><b>{item.channelName}</b><small>{item.provider} · {item.model || "未配置模型"}</small></div></td>
                  <td><span className="pill mono">{item.layer.toUpperCase()}</span><small>{layerLabel(item.layer)}</small></td>
                  <td><span className="mono">{stepLabel(item.step)}</span><small>{item.stepCount} 个步骤</small></td>
                  <td><StatusBadge tone={statusTone(item.status)}>{statusLabel(item.status)}</StatusBadge><small className="mono">{item.errorType || "-"}</small></td>
                  <td><span className="mono probe-http-code">{item.httpStatus ?? "-"}</span><small>{httpStatusMeaning(item.httpStatus, item.step)}</small></td>
                  <td className="tk-align-end mono probe-log-number">{latencyLabel(item.latencyMs)}</td>
                  <td className="tk-align-end"><Button size="sm" onClick={() => void openDetail(item)}>查看</Button></td>
                </tr>
              )) : <tr><td colSpan={8}><div className="empty-state"><h4>没有匹配的探测记录</h4><p>调整时间范围或清除异常筛选后重试。</p></div></td></tr>}
            </tbody>
          </table>
        </div>
        <Pagination page={result.page} totalPages={totalPages} pageSize={result.pageSize} total={result.total} note={`平台探测日志 · ${rangeLabel(appliedFilters.range)}`} onPageChange={(page) => void load({ ...appliedFilters, page })} />
      </div>

      <Drawer
        open={Boolean(selected)}
        onOpenChange={(open) => { if (!open) { setSelected(null); setDetail(null); } }}
        title={selected ? `${selected.channelName} · ${selected.layer.toUpperCase()} 探测详情` : "探测详情"}
        description={selected?.id}
        className="probe-log-drawer"
      >
        <div className="probe-log-detail">
          {detailLoading ? <div className="empty-state"><h4>正在加载探测步骤</h4></div> : detail ? <ProbeDetailContent detail={detail} /> : null}
        </div>
      </Drawer>
    </section>
  );
}

function ProbeDetailContent({ detail }: { detail: ProbeLogDetail }) {
  return (
    <>
      <div className="probe-detail-summary">
        <DetailFact label="通道" value={`${detail.channelName} / ${detail.provider}`} />
        <DetailFact label="模型" value={detail.model || "未配置"} />
        <DetailFact label="请求端点" value={detail.endpoint} />
        <DetailFact label="来源" value={sourceLabel(detail.source)} />
        <DetailFact label="开始时间" value={dateTimeLabel(detail.startedAt)} />
        <DetailFact label="总耗时" value={latencyLabel(detail.latencyMs)} />
      </div>
      <div className="probe-step-heading"><h4>探测步骤</h4><span>{detail.steps.length} 个步骤</span></div>
      <ol className="probe-step-list">
        {detail.steps.map((step, index) => {
          const summary = typeof step.metadata.upstream_error_summary === "string" ? step.metadata.upstream_error_summary : "";
          const metadata = Object.entries(step.metadata).filter(([key]) => key !== "upstream_error_summary");
          return (
            <li className={isAbnormal(step.status) ? "abnormal" : ""} key={`${step.step}-${index}`}>
              <span className="probe-step-index">{index + 1}</span>
              <div className="probe-step-main">
                <div className="probe-step-title">
                  <b>{stepLabel(step.step)}</b>
                  <StatusBadge tone={statusTone(step.status)}>{statusLabel(step.status)}</StatusBadge>
                  <span className="mono">{latencyLabel(step.latencyMs)}</span>
                </div>
                <div className="probe-step-http">
                  <span className="mono">HTTP {step.httpStatus ?? "-"}</span>
                  <span>{httpStatusMeaning(step.httpStatus, step.step)}</span>
                </div>
                {step.errorType ? <div className="probe-step-error"><code>{step.errorType}</code><span>{errorTypeMeaning(step.errorType)}</span></div> : null}
                {summary ? <div className="probe-upstream-summary"><b>上游错误摘要</b><p>{summary}</p></div> : null}
                {metadata.length ? <dl className="probe-step-metadata">{metadata.map(([key, value]) => <div key={key}><dt>{metadataLabel(key)}</dt><dd>{metadataValue(value)}</dd></div>)}</dl> : null}
              </div>
            </li>
          );
        })}
      </ol>
      {!detail.steps.some((step) => step.metadata.upstream_error_summary) && detail.errorType ? <div className="probe-history-note">该记录采集时未保存上游错误摘要，仍可依据 HTTP 状态码、错误类型和步骤定位问题。</div> : null}
    </>
  );
}

function DetailFact({ label, value }: { label: string; value: string }) {
  return <div><span>{label}</span><b title={value}>{value}</b></div>;
}

function initialProbeFilters(): ProbeFilters {
  const params = new URLSearchParams(window.location.search);
  return cleanProbeFilters({
    range: params.get("range") === "7d" || params.get("range") === "30d" ? params.get("range") as ProbeFilters["range"] : "24h",
    provider: params.get("provider") || "",
    channelId: params.get("channelId") || "",
    layer: params.get("layer") || "",
    status: params.get("status") || "",
    httpStatus: params.get("httpStatus") || "",
    errorType: params.get("errorType") || "",
    source: params.get("source") || "",
    onlyAbnormal: params.get("onlyAbnormal") === "true",
    page: Number(params.get("page") || 1),
    pageSize: Number(params.get("pageSize") || 50) as ProbeFilters["pageSize"]
  });
}

function cleanProbeFilters(filters: ProbeFilters): ProbeFilters {
  const range = filters.range === "7d" || filters.range === "30d" ? filters.range : "24h";
  const pageSize = filters.pageSize === 25 || filters.pageSize === 100 ? filters.pageSize : 50;
  return {
    range,
    provider: filters.provider.trim(),
    channelId: filters.channelId.trim(),
    layer: ["l1", "l2", "l3"].includes(filters.layer) ? filters.layer : "",
    status: ["ok", "warn", "auth_error", "down", "failed", "na", "running"].includes(filters.status) ? filters.status : "",
    httpStatus: filters.httpStatus.trim(),
    errorType: filters.errorType.trim(),
    source: filters.source.trim(),
    onlyAbnormal: filters.onlyAbnormal,
    page: Number.isFinite(filters.page) && filters.page > 0 ? Math.floor(filters.page) : 1,
    pageSize
  };
}

function syncProbeFiltersToURL(filters: ProbeFilters) {
  const params = new URLSearchParams(window.location.search);
  for (const key of ["range", "provider", "channelId", "layer", "status", "httpStatus", "errorType", "source", "onlyAbnormal", "page", "pageSize"]) params.delete(key);
  if (filters.range !== "24h") params.set("range", filters.range);
  if (filters.provider) params.set("provider", filters.provider);
  if (filters.channelId) params.set("channelId", filters.channelId);
  if (filters.layer) params.set("layer", filters.layer);
  if (filters.status) params.set("status", filters.status);
  if (filters.httpStatus) params.set("httpStatus", filters.httpStatus);
  if (filters.errorType) params.set("errorType", filters.errorType);
  if (filters.source) params.set("source", filters.source);
  if (filters.onlyAbnormal) params.set("onlyAbnormal", "true");
  if (filters.page > 1) params.set("page", String(filters.page));
  if (filters.pageSize !== 50) params.set("pageSize", String(filters.pageSize));
  window.history.replaceState(null, "", `${window.location.pathname}?${params.toString()}${window.location.hash}`);
}

function statusTone(status: string): "green" | "amber" | "red" | "blue" | "gray" {
  if (status === "ok") return "green";
  if (status === "warn") return "amber";
  if (status === "running") return "blue";
  if (status === "auth_error" || status === "down" || status === "failed") return "red";
  return "gray";
}

function statusLabel(status: string) {
  return ({ ok: "成功", warn: "警告", auth_error: "认证失败", down: "不可用", na: "未执行", running: "执行中", failed: "失败/中断" } as Record<string, string>)[status] || status || "未知";
}

function httpStatusMeaning(status?: number, step = "") {
  if (!status) return ["dns", "tcp", "tls", "parse_url"].includes(step) ? "尚未进入 HTTP 阶段" : "未收到 HTTP 响应";
  if (step === "http" && status < 500) {
    if (status === 401) return "端点可达，HEAD 请求需要认证";
    if (status === 403) return "端点可达，HEAD 请求被访问策略拒绝";
    if (status === 404) return "端点可达，HEAD 路径未提供资源";
  }
  const exact: Record<number, string> = {
    200: "请求成功", 201: "资源已创建", 204: "请求成功，无响应正文", 400: "请求格式或参数错误",
    401: "认证失败，Key、来源 IP 或授权策略不通过", 403: "禁止访问，可能受权限、地区或 IP 白名单限制",
    404: "接口路径或模型不存在", 408: "上游请求超时", 409: "请求发生冲突", 422: "请求内容无法处理",
    429: "请求过于频繁或额度受限", 500: "上游内部错误", 502: "上游网关响应异常", 503: "上游暂时不可用", 504: "上游网关超时"
  };
  if (exact[status]) return exact[status];
  if (status >= 200 && status < 300) return "请求成功";
  if (status >= 400 && status < 500) return "客户端请求或授权被拒绝";
  if (status >= 500) return "上游服务异常";
  return "未收录状态码";
}

function errorTypeMeaning(errorType: string) {
  const labels: Record<string, string> = {
    auth_error: "认证信息或来源授权未通过", models_auth_error: "模型列表接口认证失败", model_not_found: "目标模型未出现在模型列表中",
    model_unavailable: "目标模型当前不可用", models_timeout: "模型列表请求超时", models_rate_limited: "模型列表请求被限流",
    models_probe_skipped: "根据通道配置跳过模型列表探测", l3_probe_skipped: "根据通道配置跳过真实生成探测",
    slow_response: "响应耗时超过通道阈值", rate_limited: "上游限流或额度不足", empty_content: "生成成功但响应内容为空",
    content_mismatch: "生成内容与探测预期不一致", reasoning_only: "只返回推理内容，没有最终回答", timeout: "请求超时",
    dns_failed: "域名解析失败", tcp_failed: "TCP 连接失败", tls_failed: "TLS 握手或证书校验失败", bad_endpoint: "通道端点格式错误",
    probe_interrupted: "探测进程未正常完成或结果未成功写入"
  };
  return labels[errorType] || "未收录错误类型，请结合状态码和上游摘要判断";
}

function stepLabel(step: string) {
  return ({ parse_url: "解析地址", dns: "DNS 解析", tcp: "TCP 连接", tls: "TLS 握手", http: "HTTP 连通", models: "模型列表", generate: "真实生成", probe: "探测准备" } as Record<string, string>)[step] || step || "-";
}

function layerLabel(layer: string) {
  return ({ l1: "基础连通", l2: "模型列表", l3: "真实生成" } as Record<string, string>)[layer] || layer;
}

function sourceLabel(source: string) {
  return ({ scheduler: "定时任务", manual: "手动探测", admin_validate: "通道验证", private_manual: "私有通道手动探测" } as Record<string, string>)[source] || source || "未知来源";
}

function metadataLabel(key: string) {
  return ({ cert_expires_at: "证书到期", models_count: "模型数量", model_found: "目标模型存在", probe_mode: "探测模式", reason: "原因", content_valid: "内容有效", content_error: "内容错误", first_token_ms: "首 Token", tokens_used: "Token 用量", tokens_per_second: "Token/秒", usage_estimated: "用量估算", warn_threshold_ms: "慢响应阈值" } as Record<string, string>)[key] || key;
}

function metadataValue(value: unknown) {
  if (typeof value === "boolean") return value ? "是" : "否";
  if (value === null || value === undefined || value === "") return "-";
  return String(value);
}

function isAbnormal(status: string) {
  return status === "warn" || status === "auth_error" || status === "down" || status === "failed";
}

function latencyLabel(value: number) {
  return value >= 1000 ? `${(value / 1000).toFixed(value >= 10000 ? 1 : 2)}s` : `${value}ms`;
}

function dateTimeLabel(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "-" : date.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function shortTime(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "-" : date.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" });
}

function dateLabel(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "-" : date.toLocaleDateString("zh-CN", { month: "2-digit", day: "2-digit" });
}

function rangeLabel(range: ProbeFilters["range"]) {
  return range === "7d" ? "近 7 天" : range === "30d" ? "近 30 天" : "近 24 小时";
}
