import type { CSSProperties } from "react";
import { useEffect, useMemo, useState } from "react";
import { AdminShell } from "../components/AdminShell";
import { GovernanceSummary, ProductionHealth, governanceSummary, productionHealth, publicOverview, PublicOverview } from "../lib/api";
import { Pagination } from "../ui";

const HEALTH_PAGE_SIZE = 10;

export function AdminHome() {
  const [overview, setOverview] = useState<PublicOverview | null>(null);
  const [governance, setGovernance] = useState<GovernanceSummary | null>(null);
  const [health, setHealth] = useState<ProductionHealth | null>(null);
  const [healthPage, setHealthPage] = useState(1);
  const [copiedConsoleURL, setCopiedConsoleURL] = useState(false);

  useEffect(() => {
    publicOverview().then(setOverview).catch(() => setOverview(null));
    governanceSummary("admin").then(setGovernance).catch(() => setGovernance(null));
    productionHealth().then(setHealth).catch(() => setHealth(null));
  }, []);

  const kpis = useMemo(
    () => [
      ["平台通道", overview ? `${overview.total}` : "—", overview ? `${overview.healthy} 健康 · ${overview.degraded} 降级` : "监控数据同步中", "var(--green)", "var(--green-soft)"],
      ["连通异常", overview ? `${overview.connectivityDown}` : "—", "L1/L2 状态合成", "var(--red)", "var(--red-soft)"],
      ["认证异常", overview ? `${overview.functionalDown}` : "—", "Auth Error / Functional", "var(--magenta)", "var(--magenta-soft)"],
      ["P95 延迟", overview ? `${overview.p95LatencySeconds.toFixed(2)}s` : "—", "公开监控聚合", "var(--blue)", "var(--blue-soft)"],
      ["部署状态", "Ready", "API + Prober + NATS", "var(--amber)", "var(--amber-soft)"]
    ],
    [overview]
  );
  const healthChecks = health?.checks ?? [];
  const healthTotalPages = Math.max(1, Math.ceil(healthChecks.length / HEALTH_PAGE_SIZE));
  const healthCurrentPage = Math.min(healthPage, healthTotalPages);
  const healthPageStart = (healthCurrentPage - 1) * HEALTH_PAGE_SIZE;
  const pagedHealthChecks = healthChecks.slice(healthPageStart, healthPageStart + HEALTH_PAGE_SIZE);

  useEffect(() => {
    setHealthPage((current) => Math.min(Math.max(current, 1), healthTotalPages));
  }, [healthTotalPages]);

  async function copyConsoleURL() {
    await copyText("/console");
    setCopiedConsoleURL(true);
    window.setTimeout(() => setCopiedConsoleURL(false), 1200);
  }

  return (
    <AdminShell title="平台总览" crumb="/ 平台">
      <div className="section-head" style={{ margin: "0 0 12px" }}>
        <h2 style={{ fontSize: 14, color: "var(--text-3)", fontWeight: 650 }}>平台运营</h2>
        <span className="sub">平台后台只管理 TokHub 全局资源，个人和企业工作区能力已迁入用户控制台</span>
      </div>
      <div className="kpis" style={{ marginBottom: 18 }}>
        {kpis.map(([label, value, foot, color, soft]) => (
          <div className="kpi" style={{ "--c": color, "--cs": soft } as CSSProperties} key={label}>
            <div className="k-top">
              <span className="k-label">{label}</span>
              <span className="k-ico">✓</span>
            </div>
            <div className="k-value">{value}</div>
            <div className="k-foot">{foot}</div>
          </div>
        ))}
      </div>

      <div className="governance-strip">
        <div className="g-item"><span>打开事件</span><b>{governance?.openIncidents ?? 0}</b></div>
        <div className="g-item"><span>今日告警</span><b>{governance?.alertsToday ?? 0}</b></div>
        <div className="g-item"><span>今日人工审计</span><b>{governance?.auditToday ?? 0}</b></div>
        <div className="g-item"><span>今日成本</span><b>${(governance?.costTodayUsd ?? 0).toFixed(3)}</b></div>
      </div>

      <div className="section-head">
        <h2>
          生产数据健康检查 <span className={`tag ${health && (health.summary.fail ?? 0) > 0 ? "danger" : ""}`}>{health ? `${health.summary.fail ?? 0} fail` : "loading"}</span>
        </h2>
        <span className="sub">检查示例数据、测试来源、通知渠道和发布环境配置</span>
      </div>
      <div className="card board production-health-card">
        <div className="dt-wrap">
          <table className="dt production-health-table">
            <thead><tr><th>检查项</th><th>状态</th><th>数值</th><th>说明</th><th>建议动作</th></tr></thead>
            <tbody>
              {pagedHealthChecks.length ? pagedHealthChecks.map((check) => (
                <tr key={check.id}>
                  <td>{check.label}</td>
                  <td><span className={`badge ${check.status === "pass" ? "b-green" : check.status === "warn" ? "b-amber" : "b-red"} dot`}>{check.status}</span></td>
                  <td className="mono">{check.value}</td>
                  <td>{check.message}</td>
                  <td>{check.action}</td>
                </tr>
              )) : <tr><td colSpan={5}>正在加载生产健康检查...</td></tr>}
            </tbody>
          </table>
        </div>
        <Pagination
          page={healthCurrentPage}
          totalPages={healthTotalPages}
          pageSize={HEALTH_PAGE_SIZE}
          total={healthChecks.length}
          note={`pass ${health?.summary.pass ?? 0} · warn ${health?.summary.warn ?? 0} · fail ${health?.summary.fail ?? 0} · 脚本入口：deploy/scripts/no-demo-data-check.sh`}
          onPageChange={setHealthPage}
        />
      </div>

      <div className="section-head">
        <h2>
          用户控制台边界 <span className="tag">阶段 6.5</span>
        </h2>
        <span className="sub">专属网关、成员密钥和工作区用量统一在 `/console/*` 管理</span>
      </div>
      <div className="card gw-hero">
        <div className="l">
          <div className="module-title">TokHub Console</div>
          <div className="module-sub">工作区通道、专属网关、成员密钥和用量治理</div>
          <div className="endpoint">
            <span className="k">Console URL</span>
            <span className="v">/console</span>
            <button className="copy" type="button" onClick={() => void copyConsoleURL()}>{copiedConsoleURL ? "已复制 ✓" : "复制"}</button>
          </div>
        </div>
        <div className="r">
          <div className="chip">
            <span className="d s-ok" /> 后端 API <span className="lat">ready</span>
          </div>
          <div className="chip" style={{ marginTop: 10 }}>
            <span className="d s-ok" /> 前端构建 <span className="lat">ready</span>
          </div>
        </div>
      </div>

      <div className="grid phase-grid">
        <div className="card card-pad">
          <div className="module-title">最近事件 · 审计</div>
          <div className="tl">
            {governance?.recentAudit.length ? governance.recentAudit.map((item) => (
              <div className="ev" key={item.id}>
                <span className="pt" style={{ background: item.result === "success" ? "var(--green)" : "var(--red)" }} />
                <div className="body">
                  <div className="h">{item.action}</div>
                  <small>{item.actorEmail || item.actorId || item.actorType} · {timeLabel(item.createdAt)}</small>
                </div>
              </div>
            )) : (
              <div className="ev">
                <span className="pt" style={{ background: "var(--brand)" }} />
                <div className="body">
                  <div className="h">暂无新的审计事件</div>
                  <small>audit_events</small>
                </div>
              </div>
            )}
          </div>
        </div>
        <div className="card card-pad">
          <div className="module-title">治理入口</div>
          <div className="module-sub">用量、告警、审计和 incident 已进入统一后台</div>
          <a className="btn btn-primary btn-sm" href="/admin/alerts">
            查看告警规则
          </a>
        </div>
      </div>
    </AdminShell>
  );
}

async function copyText(value: string) {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(value);
    return;
  }
  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.setAttribute("readonly", "true");
  textarea.style.position = "fixed";
  textarea.style.opacity = "0";
  document.body.appendChild(textarea);
  textarea.select();
  document.execCommand("copy");
  document.body.removeChild(textarea);
}

function timeLabel(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "-" : date.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}
