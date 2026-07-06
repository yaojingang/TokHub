import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { AdminShell } from "../components/AdminShell";
import {
  adminRecommend,
  PublicChannel,
  RecommendAdminData,
  RecommendPick,
  RecommendRankRule,
  RecommendReward,
  RecommendScenario,
  saveAdminRecommend
} from "../lib/api";

type Tab = "picks" | "ranks" | "rewards" | "scenarios";

const maxRecommendPicks = 3;
const defaultCTALabel = "去官方体验";

export function AdminRecommendPage() {
  const [tab, setTab] = useState<Tab>("picks");
  const [data, setData] = useState<RecommendAdminData | null>(null);
  const [draft, setDraft] = useState<RecommendAdminData | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  useEffect(() => {
    void load();
  }, []);

  async function load() {
    setLoading(true);
    setError("");
    try {
      const payload = await adminRecommend();
      const normalized = normalizeRecommendDraft(payload);
      setData(normalized);
      setDraft(cloneRecommend(normalized));
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载推荐配置失败");
    } finally {
      setLoading(false);
    }
  }

  async function save() {
    if (!draft) return;
    const normalized = normalizeRecommendDraft(draft);
    const validation = validateRecommendDraft(normalized);
    if (validation) {
      setError(validation);
      setNotice("");
      return;
    }
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const payload = await saveAdminRecommend({
        picks: normalized.picks,
        rankRules: normalized.rankRules,
        rewards: normalized.rewards,
        scenarios: normalized.scenarios
      });
      const next = normalizeRecommendDraft(payload);
      setData(next);
      setDraft(cloneRecommend(next));
      setNotice("推荐配置已保存，前台 /recommend 会立即读取最新配置。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存推荐配置失败");
    } finally {
      setSaving(false);
    }
  }

  function reset() {
    if (data) setDraft(cloneRecommend(normalizeRecommendDraft(data)));
    setError("");
    setNotice("");
  }

  const stats = useMemo(() => {
    const d = draft ?? data;
    return [
      ["精选位", d?.picks.filter((item) => item.enabled).length ?? 0, "TOP3 编辑首推"],
      ["上榜中转站", d?.ranks.length ?? 0, "综合榜默认顺序"],
      ["点击量", d?.stats.clicks ?? 0, "推荐页累计埋点"],
      ["入口说明", d?.rewards.filter((item) => item.enabled).length ?? 0, "有效接入说明"],
      ["场景", d?.scenarios.filter((item) => item.enabled).length ?? 0, "场景化推荐位"]
    ];
  }, [data, draft]);

  return (
    <AdminShell title="精选推荐管理" crumb="/ 平台运营">
      <div className="admin-actions-line">
        <p className="page-intro">维护前台「精选推荐」页面的 TOP3、多维榜单、推荐入口与场景推荐。保存后前台页面和点击统计都走真实 API，不再依赖前端静态 mock</p>
        <div className="admin-actions-buttons">
          <a className="btn btn-ghost btn-sm" href="/recommend" target="_blank" rel="noreferrer">预览前台 →</a>
          <button className="btn btn-ghost btn-sm" onClick={reset} disabled={loading || saving}>放弃更改</button>
          <button className="btn btn-primary btn-sm" onClick={() => void save()} disabled={loading || saving}>{saving ? "保存中..." : "保存全部配置"}</button>
        </div>
      </div>
      {error ? <div className="form-error">{error}</div> : null}
      {notice ? <div className="form-notice">{notice}</div> : null}

      <div className="stat-row">
        {stats.map(([label, value, hint]) => (
          <div className="stat" key={label}>
            <div className="l">{label}</div>
            <div className="v">{formatInt(Number(value))}</div>
            <div className="d">{hint}</div>
          </div>
        ))}
      </div>

      <div className="tabs">
        <button className={`tab ${tab === "picks" ? "active" : ""}`} onClick={() => setTab("picks")}>TOP 3 编辑首推</button>
        <button className={`tab ${tab === "ranks" ? "active" : ""}`} onClick={() => setTab("ranks")}>多维度榜单</button>
        <button className={`tab ${tab === "rewards" ? "active" : ""}`} onClick={() => setTab("rewards")}>推荐入口</button>
        <button className={`tab ${tab === "scenarios" ? "active" : ""}`} onClick={() => setTab("scenarios")}>场景化推荐</button>
      </div>

      {loading || !draft ? (
        <div className="card card-pad empty-state"><h4>正在加载推荐配置</h4></div>
      ) : (
        <>
          {tab === "picks" ? <PickEditor draft={draft} setDraft={setDraft} /> : null}
          {tab === "ranks" ? <RankEditor draft={draft} setDraft={setDraft} /> : null}
          {tab === "rewards" ? <RewardEditor draft={draft} setDraft={setDraft} /> : null}
          {tab === "scenarios" ? <ScenarioEditor draft={draft} setDraft={setDraft} /> : null}
        </>
      )}
    </AdminShell>
  );
}

function PickEditor({ draft, setDraft }: { draft: RecommendAdminData; setDraft: (value: RecommendAdminData) => void }) {
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState<"all" | "enabled" | "disabled">("all");
  const [selectedIDs, setSelectedIDs] = useState<string[]>([]);

  const filteredPicks = useMemo(() => {
    const keyword = query.trim().toLowerCase();
    return draft.picks.map((pick, index) => ({ pick, index })).filter(({ pick }) => {
      if (status === "enabled" && !pick.enabled) return false;
      if (status === "disabled" && pick.enabled) return false;
      if (!keyword) return true;
      return `${pick.title} ${pick.ribbon} ${pick.summary} ${pick.channel.provider} ${pick.channel.model}`.toLowerCase().includes(keyword);
    });
  }, [draft.picks, query, status]);

  useEffect(() => {
    const ids = new Set(draft.picks.map((pick) => pick.id));
    setSelectedIDs((current) => current.filter((id) => ids.has(id)));
  }, [draft.picks]);

  function update(index: number, patch: Partial<RecommendPick>) {
    const picks = draft.picks.map((item, i) => {
      if (i !== index) return item;
      const next = { ...item, ...patch };
      if (patch.channelId) next.channel = channelByID(draft.channels, patch.channelId, item.channel);
      return next;
    });
    setDraft({ ...draft, picks });
  }

  function duplicatePick(index: number) {
    if (draft.picks.length >= maxRecommendPicks) return;
    const source = draft.picks[index];
    if (!source) return;
    const pick: RecommendPick = { ...source, id: `tmp_${Date.now()}`, position: draft.picks.length + 1, title: `${source.title} 副本`, clicks: 0, ctr: 0 };
    setDraft({ ...draft, picks: [...draft.picks, pick] });
  }

  function removePick(index: number) {
    const picks = draft.picks.filter((_, i) => i !== index).map((item, i) => ({ ...item, position: i + 1 }));
    setDraft({ ...draft, picks });
  }

  function movePick(index: number, dir: -1 | 1) {
    const target = index + dir;
    if (target < 0 || target >= draft.picks.length) return;
    const picks = [...draft.picks];
    [picks[index], picks[target]] = [picks[target], picks[index]];
    setDraft({ ...draft, picks: picks.map((item, i) => ({ ...item, position: i + 1 })) });
  }

  function toggleSelection(id: string) {
    setSelectedIDs((current) => current.includes(id) ? current.filter((item) => item !== id) : [...current, id]);
  }

  function setAllSelection(checked: boolean) {
    setSelectedIDs(checked ? filteredPicks.map(({ pick }) => pick.id) : []);
  }

  function bulkEnabled(enabled: boolean) {
    const selected = new Set(selectedIDs);
    setDraft({ ...draft, picks: draft.picks.map((pick) => selected.has(pick.id) ? { ...pick, enabled } : pick) });
  }

  function bulkDuplicate() {
    const selected = new Set(selectedIDs);
    const slots = Math.max(0, maxRecommendPicks - draft.picks.length);
    const copies = draft.picks.filter((pick) => selected.has(pick.id)).slice(0, slots).map((source, index) => ({
      ...source,
      id: tmpID("pick"),
      position: draft.picks.length + index + 1,
      title: `${source.title} 副本`,
      clicks: 0,
      ctr: 0
    }));
    setDraft({ ...draft, picks: [...draft.picks, ...copies] });
    setSelectedIDs([]);
  }

  function bulkDelete() {
    const selected = new Set(selectedIDs);
    setDraft({ ...draft, picks: draft.picks.filter((pick) => !selected.has(pick.id)).map((item, index) => ({ ...item, position: index + 1 })) });
    setSelectedIDs([]);
  }

  const selectedSet = useMemo(() => new Set(selectedIDs), [selectedIDs]);
  const allFilteredSelected = filteredPicks.length > 0 && filteredPicks.every(({ pick }) => selectedSet.has(pick.id));

  return (
    <div className="set-pane">
      <div className="card set-card">
        <div className="set-h">
          <span>★ 本周编辑首推 · TOP 3</span>
          {draft.channels.length && draft.picks.length < maxRecommendPicks ? (
            <Link className="btn btn-ghost btn-sm" to="/admin/recommend/new">＋ 添加推荐位</Link>
          ) : (
            <button className="btn btn-ghost btn-sm" disabled>＋ 添加推荐位</button>
          )}
        </div>
        <div className="ops-help">显示在 /recommend 的「本周编辑精选」区。每个位置选择一个已认证平台通道，并维护推荐语、卖点、按钮文案和官方注册页 URL。</div>
        <div className="toolbar bulk-toolbar recommend-toolbar">
          <label className="bulk-select">
            <input type="checkbox" aria-label="选择当前筛选的推荐位" checked={allFilteredSelected} disabled={!filteredPicks.length} onChange={(event) => setAllSelection(event.target.checked)} />
            已选 {selectedIDs.length}
          </label>
          <input className="input" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索推荐标题 / 摘要 / 模型..." />
          <select className="input compact-select" value={status} onChange={(event) => setStatus(event.target.value as typeof status)}>
            <option value="all">全部状态</option>
            <option value="enabled">已启用</option>
            <option value="disabled">已停用</option>
          </select>
          <button className="btn btn-ghost btn-sm" disabled={!selectedIDs.length} onClick={() => bulkEnabled(true)}>批量启用</button>
          <button className="btn btn-ghost btn-sm" disabled={!selectedIDs.length} onClick={() => bulkEnabled(false)}>批量停用</button>
          <button className="btn btn-ghost btn-sm" disabled={!selectedIDs.length} onClick={bulkDuplicate}>批量复制</button>
          <button className="btn btn-ghost btn-sm" disabled={!selectedIDs.length} onClick={bulkDelete}>批量删除</button>
        </div>
        <div className="pk-grid ops-pad">
          {filteredPicks.length ? filteredPicks.map(({ pick, index }) => (
            <div className="pk-card" key={pick.id}>
              <div className="pk-card-toolbar">
                <label className="bulk-select recommend-card-select">
                  <input type="checkbox" aria-label={`选择推荐位 ${pick.title}`} checked={selectedSet.has(pick.id)} onChange={() => toggleSelection(pick.id)} />
                  选择
                </label>
                <span className="pk-pos">#{index + 1}</span>
              </div>
              <div className="pk-head">
                <span className="pk-mk" style={{ background: pick.channel.mark || "var(--brand)" }}>{initials(pick.title)}</span>
                <div className="pk-meta">
                  <b>{pick.title}</b>
                  <small>{pick.channel.provider} · {pick.channel.model}</small>
                </div>
              </div>
              <div className="ops-field">
                <label>推荐通道</label>
                <select className="input" value={pick.channelId} onChange={(event) => update(index, { channelId: event.target.value, title: channelByID(draft.channels, event.target.value, pick.channel).name })}>
                  {draft.channels.map((channel) => <option value={channel.id} key={channel.id}>{channel.name} · {channel.provider}</option>)}
                </select>
              </div>
              <div className="ops-field">
                <label>角标</label>
                <input className="input" value={pick.ribbon} onChange={(event) => update(index, { ribbon: event.target.value })} />
              </div>
              <div className="ops-field">
                <label>推荐摘要</label>
                <textarea className="input ops-textarea ops-textarea-summary" value={pick.summary} onChange={(event) => update(index, { summary: event.target.value })} />
              </div>
              <div className="ops-field">
                <label>卖点（一行一个）</label>
                <textarea className="input ops-textarea ops-textarea-points" value={pick.points.join("\n")} onChange={(event) => update(index, { points: event.target.value.split("\n").map((line) => line.trim()).filter(Boolean) })} />
              </div>
              <div className="ops-field">
                <label>官方体验按钮</label>
                <div className="ops-inline">
                  <input className="input" aria-label={`${pick.title} 按钮文案`} value={pick.ctaLabel} onChange={(event) => update(index, { ctaLabel: event.target.value })} placeholder={defaultCTALabel} />
                  <input className="input" aria-label={`${pick.title} 官方注册页 URL`} value={pick.ctaUrl} onChange={(event) => update(index, { ctaUrl: event.target.value })} placeholder="https://example.com/register" />
                </div>
              </div>
              <div className="pk-stats">
                <span className="pk-st">评分<b>{pick.channel.score}</b></span>
                <span className="pk-st">点击<b>{pick.clicks}</b></span>
                <span className="pk-st">CTR<b>{pick.ctr.toFixed(1)}%</b></span>
              </div>
              <div className="table-actions">
                <button className="btn btn-ghost btn-sm" onClick={() => movePick(index, -1)} disabled={index === 0}>上移</button>
                <button className="btn btn-ghost btn-sm" onClick={() => movePick(index, 1)} disabled={index === draft.picks.length - 1}>下移</button>
                <button className="btn btn-ghost btn-sm" onClick={() => duplicatePick(index)}>复制</button>
                <button className="btn btn-ghost btn-sm" onClick={() => removePick(index)}>删除</button>
                <button className={`switch ${pick.enabled ? "on" : ""}`} aria-label="启用精选位" onClick={() => update(index, { enabled: !pick.enabled })} />
              </div>
            </div>
          )) : <div className="empty-state rec-wide"><h4>暂无推荐位</h4><p>{draft.picks.length ? "调整搜索或状态筛选后再试。" : "添加公开平台通道后可创建 TOP3 推荐配置。"}</p></div>}
        </div>
      </div>
    </div>
  );
}

function RankEditor({ draft, setDraft }: { draft: RecommendAdminData; setDraft: (value: RecommendAdminData) => void }) {
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState<"all" | "enabled" | "disabled">("all");
  const [selectedIDs, setSelectedIDs] = useState<string[]>([]);
  const rankRules = draft.rankRules ?? [];
  const filteredRules = useMemo(() => {
    const keyword = query.trim().toLowerCase();
    return rankRules.map((rule, index) => ({ rule, index })).filter(({ rule }) => {
      if (status === "enabled" && !rule.enabled) return false;
      if (status === "disabled" && rule.enabled) return false;
      if (!keyword) return true;
      return `${rule.label} ${rule.description} ${rule.metric}`.toLowerCase().includes(keyword);
    });
  }, [rankRules, query, status]);

  useEffect(() => {
    const ids = new Set(rankRules.map((rule) => rule.id));
    setSelectedIDs((current) => current.filter((id) => ids.has(id)));
  }, [rankRules]);

  function move(index: number, dir: -1 | 1) {
    const target = index + dir;
    if (target < 0 || target >= draft.picks.length) return;
    const picks = [...draft.picks];
    [picks[index], picks[target]] = [picks[target], picks[index]];
    const normalized = picks.map((item, i) => ({ ...item, position: i + 1 }));
    setDraft({ ...draft, picks: normalized });
  }

  function updateScoreWeight(index: number, value: string) {
    const picks = draft.picks.map((item, i) => i === index ? { ...item, ribbon: value } : item);
    setDraft({ ...draft, picks });
  }

  function updateRule(index: number, patch: Partial<RecommendRankRule>) {
    const rankRules = draft.rankRules.map((rule, i) => i === index ? { ...rule, ...patch } : rule);
    setDraft({ ...draft, rankRules });
  }

  function moveRule(index: number, dir: -1 | 1) {
    const target = index + dir;
    if (target < 0 || target >= rankRules.length) return;
    const next = [...rankRules];
    [next[index], next[target]] = [next[target], next[index]];
    setDraft({ ...draft, rankRules: next.map((rule, i) => ({ ...rule, position: i + 1 })) });
  }

  function addRule() {
    const id = tmpID("rank");
    setDraft({
      ...draft,
      rankRules: [
        ...rankRules,
        { id, label: "自定义榜单", description: "按运营定义展示", metric: "custom", position: rankRules.length + 1, enabled: true }
      ]
    });
  }

  function duplicateRule(index: number) {
    const source = rankRules[index];
    if (!source) return;
    const copy = { ...source, id: tmpID("rank"), label: `${source.label} 副本`, position: index + 2 };
    const next = [...rankRules.slice(0, index + 1), copy, ...rankRules.slice(index + 1)].map((rule, i) => ({ ...rule, position: i + 1 }));
    setDraft({ ...draft, rankRules: next });
  }

  function removeRule(index: number) {
    setDraft({ ...draft, rankRules: rankRules.filter((_, i) => i !== index).map((rule, i) => ({ ...rule, position: i + 1 })) });
  }

  function toggleSelection(id: string) {
    setSelectedIDs((current) => current.includes(id) ? current.filter((item) => item !== id) : [...current, id]);
  }

  function setAllSelection(checked: boolean) {
    setSelectedIDs(checked ? filteredRules.map(({ rule }) => rule.id) : []);
  }

  function bulkEnabled(enabled: boolean) {
    const selected = new Set(selectedIDs);
    setDraft({ ...draft, rankRules: rankRules.map((rule) => selected.has(rule.id) ? { ...rule, enabled } : rule) });
  }

  function bulkDelete() {
    const selected = new Set(selectedIDs);
    setDraft({ ...draft, rankRules: rankRules.filter((rule) => !selected.has(rule.id)).map((rule, i) => ({ ...rule, position: i + 1 })) });
    setSelectedIDs([]);
  }

  function bulkDuplicate() {
    const selected = new Set(selectedIDs);
    const copies = rankRules.filter((rule) => selected.has(rule.id)).map((source, index) => ({
      ...source,
      id: tmpID("rank"),
      label: `${source.label} 副本`,
      position: rankRules.length + index + 1
    }));
    setDraft({ ...draft, rankRules: [...rankRules, ...copies].map((rule, i) => ({ ...rule, position: i + 1 })) });
    setSelectedIDs([]);
  }

  const selectedSet = useMemo(() => new Set(selectedIDs), [selectedIDs]);
  const allFilteredSelected = filteredRules.length > 0 && filteredRules.every(({ rule }) => selectedSet.has(rule.id));

  return (
    <div className="set-pane">
      <div className="card set-card">
        <div className="set-h">综合榜单顺序</div>
        <div className="ops-help">综合榜默认按 TOP3 配置顺序展示；速度王、性价比、稳定王由前台基于真实探测指标自动排序。</div>
        <div className="rank-cfg ops-pad">
          {draft.picks.length ? draft.picks.map((pick, index) => (
            <div className="rank-row" key={pick.id}>
              <span className="pos">{index + 1}</span>
              <span className="nm"><span className="av" style={{ background: pick.channel.mark || "var(--brand)" }}>{initials(pick.title)}</span><span>{pick.title}</span></span>
              <input value={pick.ribbon} onChange={(event) => updateScoreWeight(index, event.target.value)} />
              <span className="mono">{pick.channel.latencyP95Ms}ms</span>
              <span className="mono">{pick.channel.score}</span>
              <span className="drag">
                <button className="rm-btn" onClick={() => move(index, -1)} disabled={index === 0}>↑</button>
                <button className="rm-btn" onClick={() => move(index, 1)} disabled={index === draft.picks.length - 1}>↓</button>
              </span>
            </div>
          )) : <div className="empty-state"><h4>暂无榜单顺序</h4><p>先在 TOP3 页签添加推荐位。</p></div>}
        </div>
      </div>
      <div className="card set-card">
        <div className="set-h">
          <span>自动榜单规则</span>
          <button className="btn btn-ghost btn-sm" onClick={addRule}>＋ 新增规则</button>
        </div>
        <div className="ops-help">控制 /recommend 多维榜单 Tab 的显示、名称和排序。关闭规则后，前台不会展示对应榜单入口。</div>
        <div className="toolbar bulk-toolbar recommend-toolbar">
          <label className="bulk-select">
            <input type="checkbox" aria-label="选择当前筛选的榜单规则" checked={allFilteredSelected} disabled={!filteredRules.length} onChange={(event) => setAllSelection(event.target.checked)} />
            已选 {selectedIDs.length} 条规则
          </label>
          <input className="input" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索榜单名称 / 指标 / 说明..." />
          <select className="input compact-select" value={status} onChange={(event) => setStatus(event.target.value as typeof status)}>
            <option value="all">全部状态</option>
            <option value="enabled">已启用</option>
            <option value="disabled">已停用</option>
          </select>
          <button className="btn btn-ghost btn-sm" disabled={!selectedIDs.length} onClick={() => bulkEnabled(true)}>批量启用</button>
          <button className="btn btn-ghost btn-sm" disabled={!selectedIDs.length} onClick={() => bulkEnabled(false)}>批量停用</button>
          <button className="btn btn-ghost btn-sm" disabled={!selectedIDs.length} onClick={bulkDuplicate}>批量复制</button>
          <button className="btn btn-ghost btn-sm" disabled={!selectedIDs.length} onClick={bulkDelete}>批量删除</button>
        </div>
        <div className="ops-list recommend-rule-list">
          {filteredRules.length ? <div className="recommend-rule-head">
            <span>选择</span>
            <span>顺序</span>
            <span>榜单名称</span>
            <span>指标</span>
            <span>说明</span>
            <span>排序</span>
            <span>操作</span>
            <span>状态</span>
          </div> : null}
          {filteredRules.length ? filteredRules.map(({ rule, index }) => (
            <div className="scenario-row recommend-rule-row" key={rule.id}>
              <input type="checkbox" aria-label={`选择榜单规则 ${rule.label}`} checked={selectedSet.has(rule.id)} onChange={() => toggleSelection(rule.id)} />
              <span className="pos">{index + 1}</span>
              <input className="input" value={rule.label} onChange={(event) => updateRule(index, { label: event.target.value })} />
              <select className="input" value={rule.metric} onChange={(event) => updateRule(index, { metric: event.target.value as RecommendRankRule["metric"] })}>
                <option value="overall">综合榜</option>
                <option value="speed">速度王</option>
                <option value="price">性价比</option>
                <option value="stable">稳定王</option>
                <option value="custom">自定义</option>
              </select>
              <input className="input" value={rule.description} onChange={(event) => updateRule(index, { description: event.target.value })} />
              <span className="recommend-row-actions">
                <button className="btn btn-ghost btn-sm" onClick={() => moveRule(index, -1)} disabled={index === 0}>↑</button>
                <button className="btn btn-ghost btn-sm" onClick={() => moveRule(index, 1)} disabled={index === rankRules.length - 1}>↓</button>
              </span>
              <span className="recommend-row-actions">
                <button className="btn btn-ghost btn-sm" onClick={() => duplicateRule(index)}>复制</button>
                <button className="btn btn-ghost btn-sm" onClick={() => removeRule(index)}>删除</button>
              </span>
              <button className={`switch ${rule.enabled ? "on" : ""}`} aria-label="启用榜单规则" onClick={() => updateRule(index, { enabled: !rule.enabled })} />
            </div>
          )) : <div className="empty-state"><h4>暂无榜单规则</h4><p>{rankRules.length ? "调整搜索或状态筛选后再试。" : "新增规则并保存后，前台多维榜单会按规则显示。"}</p></div>}
        </div>
      </div>
    </div>
  );
}

function RewardEditor({ draft, setDraft }: { draft: RecommendAdminData; setDraft: (value: RecommendAdminData) => void }) {
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState<"all" | "enabled" | "disabled">("all");
  const [selectedIDs, setSelectedIDs] = useState<string[]>([]);

  const filteredRewards = useMemo(() => {
    const keyword = query.trim().toLowerCase();
    return draft.rewards.map((reward, index) => ({ reward, index })).filter(({ reward }) => {
      if (status === "enabled" && !reward.enabled) return false;
      if (status === "disabled" && reward.enabled) return false;
      if (!keyword) return true;
      return `${reward.providerName} ${reward.rewardType} ${reward.rewardValue} ${reward.code} ${reward.expiresAtText}`.toLowerCase().includes(keyword);
    });
  }, [draft.rewards, query, status]);

  useEffect(() => {
    const ids = new Set(draft.rewards.map((reward) => reward.id));
    setSelectedIDs((current) => current.filter((id) => ids.has(id)));
  }, [draft.rewards]);

  function update(index: number, patch: Partial<RecommendReward>) {
    const rewards = draft.rewards.map((item, i) => {
      if (i !== index) return item;
      const next = { ...item, ...patch };
      if (patch.channelId) next.providerName = channelByID(draft.channels, patch.channelId).name;
      return next;
    });
    setDraft({ ...draft, rewards });
  }

  function addReward() {
    const channel = draft.channels[0];
    if (!channel) return;
    const reward: RecommendReward = {
      id: `tmp_${Date.now()}`,
      channelId: channel.id,
      providerName: channel.name,
      rewardType: "接入说明",
      rewardValue: "以官网为准",
      code: "OFFICIAL",
      expiresAtText: "长期有效",
      enabled: true,
      clicks: 0
    };
    setDraft({ ...draft, rewards: [...draft.rewards, reward] });
  }

  function duplicateReward(index: number) {
    const source = draft.rewards[index];
    if (!source) return;
    setDraft({ ...draft, rewards: [...draft.rewards, { ...source, id: `tmp_${Date.now()}`, providerName: `${source.providerName} 副本`, clicks: 0 }] });
  }

  function removeReward(index: number) {
    setDraft({ ...draft, rewards: draft.rewards.filter((_, i) => i !== index) });
  }

  function toggleSelection(id: string) {
    setSelectedIDs((current) => current.includes(id) ? current.filter((item) => item !== id) : [...current, id]);
  }

  function setAllSelection(checked: boolean) {
    setSelectedIDs(checked ? filteredRewards.map(({ reward }) => reward.id) : []);
  }

  function bulkEnabled(enabled: boolean) {
    const selected = new Set(selectedIDs);
    setDraft({ ...draft, rewards: draft.rewards.map((reward) => selected.has(reward.id) ? { ...reward, enabled } : reward) });
  }

  function bulkDuplicate() {
    const selected = new Set(selectedIDs);
    const copies = draft.rewards.filter((reward) => selected.has(reward.id)).map((source) => ({ ...source, id: tmpID("reward"), providerName: `${source.providerName} 副本`, clicks: 0 }));
    setDraft({ ...draft, rewards: [...draft.rewards, ...copies] });
    setSelectedIDs([]);
  }

  function bulkDelete() {
    const selected = new Set(selectedIDs);
    setDraft({ ...draft, rewards: draft.rewards.filter((reward) => !selected.has(reward.id)) });
    setSelectedIDs([]);
  }

  const selectedSet = useMemo(() => new Set(selectedIDs), [selectedIDs]);
  const allFilteredSelected = filteredRewards.length > 0 && filteredRewards.every(({ reward }) => selectedSet.has(reward.id));

  return (
    <div className="set-pane">
      <div className="card set-card">
        <div className="set-h">
          <span>推荐入口配置</span>
          <button className="btn btn-ghost btn-sm" onClick={addReward}>＋ 新增入口说明</button>
        </div>
        <div className="ops-help">入口说明显示在精选卡片和榜单「官方入口」列，由运营后台保存后前台立即读取。</div>
        <div className="toolbar bulk-toolbar recommend-toolbar">
          <label className="bulk-select">
            <input type="checkbox" aria-label="选择当前筛选的入口说明" checked={allFilteredSelected} disabled={!filteredRewards.length} onChange={(event) => setAllSelection(event.target.checked)} />
            已选 {selectedIDs.length}
          </label>
          <input className="input" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索服务商 / 入口类型 / 备注..." />
          <select className="input compact-select" value={status} onChange={(event) => setStatus(event.target.value as typeof status)}>
            <option value="all">全部状态</option>
            <option value="enabled">已启用</option>
            <option value="disabled">已停用</option>
          </select>
          <button className="btn btn-ghost btn-sm" disabled={!selectedIDs.length} onClick={() => bulkEnabled(true)}>批量启用</button>
          <button className="btn btn-ghost btn-sm" disabled={!selectedIDs.length} onClick={() => bulkEnabled(false)}>批量停用</button>
          <button className="btn btn-ghost btn-sm" disabled={!selectedIDs.length} onClick={bulkDuplicate}>批量复制</button>
          <button className="btn btn-ghost btn-sm" disabled={!selectedIDs.length} onClick={bulkDelete}>批量删除</button>
        </div>
        <div className="reward-grid ops-pad">
          {filteredRewards.length ? filteredRewards.map(({ reward, index }) => (
            <div className="reward-card" key={reward.id}>
              <label className="bulk-select recommend-card-select" style={{ marginBottom: 8 }}>
                <input type="checkbox" aria-label={`选择入口说明 ${reward.providerName}`} checked={selectedSet.has(reward.id)} onChange={() => toggleSelection(reward.id)} />
                选择
              </label>
              <div className="rh"><span className="badge b-amber dot">{reward.rewardValue}</span><b>{reward.providerName}</b></div>
              <label>关联通道</label>
              <select value={reward.channelId} onChange={(event) => update(index, { channelId: event.target.value })}>
                {draft.channels.map((channel) => <option value={channel.id} key={channel.id}>{channel.name}</option>)}
              </select>
              <label>入口类型</label>
              <input value={reward.rewardType} onChange={(event) => update(index, { rewardType: event.target.value })} />
              <label>说明 / 备注</label>
              <div className="ops-inline">
                <input value={reward.rewardValue} onChange={(event) => update(index, { rewardValue: event.target.value })} />
                <input value={reward.code} onChange={(event) => update(index, { code: event.target.value.toUpperCase() })} />
              </div>
              <label>有效期</label>
              <input value={reward.expiresAtText} onChange={(event) => update(index, { expiresAtText: event.target.value })} />
              <div className="ops-card-foot">
                <span className="muted-time">点击 {reward.clicks}</span>
                <button className="btn btn-ghost btn-sm" onClick={() => duplicateReward(index)}>复制</button>
                <button className="btn btn-ghost btn-sm" onClick={() => removeReward(index)}>删除</button>
                <button className={`switch ${reward.enabled ? "on" : ""}`} aria-label="启用入口说明" onClick={() => update(index, { enabled: !reward.enabled })} />
              </div>
            </div>
          )) : <div className="empty-state rec-wide"><h4>暂无入口说明</h4><p>{draft.rewards.length ? "调整搜索或状态筛选后再试。" : "添加公开平台通道后可创建入口说明。"}</p></div>}
        </div>
      </div>
    </div>
  );
}

function ScenarioEditor({ draft, setDraft }: { draft: RecommendAdminData; setDraft: (value: RecommendAdminData) => void }) {
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState<"all" | "enabled" | "disabled">("all");
  const [selectedIDs, setSelectedIDs] = useState<string[]>([]);

  const filteredScenarios = useMemo(() => {
    const keyword = query.trim().toLowerCase();
    return draft.scenarios.map((scenario, index) => ({ scenario, index })).filter(({ scenario }) => {
      if (status === "enabled" && !scenario.enabled) return false;
      if (status === "disabled" && scenario.enabled) return false;
      if (!keyword) return true;
      return `${scenario.title} ${scenario.summary} ${scenario.icon} ${scenario.channel.provider} ${scenario.channel.model}`.toLowerCase().includes(keyword);
    });
  }, [draft.scenarios, query, status]);

  useEffect(() => {
    const ids = new Set(draft.scenarios.map((scenario) => scenario.id));
    setSelectedIDs((current) => current.filter((id) => ids.has(id)));
  }, [draft.scenarios]);

  function update(index: number, patch: Partial<RecommendScenario>) {
    const scenarios = draft.scenarios.map((item, i) => {
      if (i !== index) return item;
      const next = { ...item, ...patch };
      if (patch.channelId) next.channel = channelByID(draft.channels, patch.channelId, item.channel);
      return next;
    });
    setDraft({ ...draft, scenarios });
  }

  function addScenario() {
    const channel = draft.channels[0];
    if (!channel) return;
    const scenario: RecommendScenario = {
      id: `tmp_${Date.now()}`,
      title: "新场景推荐",
      icon: "AI",
      channelId: channel.id,
      summary: `${channel.provider} · ${channel.model}`,
      position: draft.scenarios.length + 1,
      enabled: true,
      clicks: 0,
      channel
    };
    setDraft({ ...draft, scenarios: [...draft.scenarios, scenario] });
  }

  function duplicateScenario(index: number) {
    const source = draft.scenarios[index];
    if (!source) return;
    const scenario: RecommendScenario = { ...source, id: `tmp_${Date.now()}`, position: draft.scenarios.length + 1, title: `${source.title} 副本`, clicks: 0 };
    setDraft({ ...draft, scenarios: [...draft.scenarios, scenario] });
  }

  function removeScenario(index: number) {
    const scenarios = draft.scenarios.filter((_, i) => i !== index).map((item, i) => ({ ...item, position: i + 1 }));
    setDraft({ ...draft, scenarios });
  }

  function moveScenario(index: number, dir: -1 | 1) {
    const target = index + dir;
    if (target < 0 || target >= draft.scenarios.length) return;
    const scenarios = [...draft.scenarios];
    [scenarios[index], scenarios[target]] = [scenarios[target], scenarios[index]];
    setDraft({ ...draft, scenarios: scenarios.map((item, i) => ({ ...item, position: i + 1 })) });
  }

  function toggleSelection(id: string) {
    setSelectedIDs((current) => current.includes(id) ? current.filter((item) => item !== id) : [...current, id]);
  }

  function setAllSelection(checked: boolean) {
    setSelectedIDs(checked ? filteredScenarios.map(({ scenario }) => scenario.id) : []);
  }

  function bulkEnabled(enabled: boolean) {
    const selected = new Set(selectedIDs);
    setDraft({ ...draft, scenarios: draft.scenarios.map((scenario) => selected.has(scenario.id) ? { ...scenario, enabled } : scenario) });
  }

  function bulkDuplicate() {
    const selected = new Set(selectedIDs);
    const copies = draft.scenarios.filter((scenario) => selected.has(scenario.id)).map((source, index) => ({
      ...source,
      id: tmpID("scenario"),
      position: draft.scenarios.length + index + 1,
      title: `${source.title} 副本`,
      clicks: 0
    }));
    setDraft({ ...draft, scenarios: [...draft.scenarios, ...copies] });
    setSelectedIDs([]);
  }

  function bulkDelete() {
    const selected = new Set(selectedIDs);
    setDraft({ ...draft, scenarios: draft.scenarios.filter((scenario) => !selected.has(scenario.id)).map((item, index) => ({ ...item, position: index + 1 })) });
    setSelectedIDs([]);
  }

  const selectedSet = useMemo(() => new Set(selectedIDs), [selectedIDs]);
  const allFilteredSelected = filteredScenarios.length > 0 && filteredScenarios.every(({ scenario }) => selectedSet.has(scenario.id));

  return (
    <div className="set-pane">
      <div className="card set-card">
        <div className="set-h">
          <span>场景化推荐位</span>
          <button className="btn btn-ghost btn-sm" onClick={addScenario} disabled={!draft.channels.length}>＋ 新增场景</button>
        </div>
        <div className="ops-help">控制 /recommend「看你怎么用」区的场景卡。每个场景关联一个推荐通道，并独立统计点击。</div>
        <div className="toolbar bulk-toolbar recommend-toolbar">
          <label className="bulk-select">
            <input type="checkbox" aria-label="选择当前筛选的场景推荐" checked={allFilteredSelected} disabled={!filteredScenarios.length} onChange={(event) => setAllSelection(event.target.checked)} />
            已选 {selectedIDs.length}
          </label>
          <input className="input" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索场景标题 / 摘要 / 模型..." />
          <select className="input compact-select" value={status} onChange={(event) => setStatus(event.target.value as typeof status)}>
            <option value="all">全部状态</option>
            <option value="enabled">已启用</option>
            <option value="disabled">已停用</option>
          </select>
          <button className="btn btn-ghost btn-sm" disabled={!selectedIDs.length} onClick={() => bulkEnabled(true)}>批量启用</button>
          <button className="btn btn-ghost btn-sm" disabled={!selectedIDs.length} onClick={() => bulkEnabled(false)}>批量停用</button>
          <button className="btn btn-ghost btn-sm" disabled={!selectedIDs.length} onClick={bulkDuplicate}>批量复制</button>
          <button className="btn btn-ghost btn-sm" disabled={!selectedIDs.length} onClick={bulkDelete}>批量删除</button>
        </div>
        <div className="ops-list recommend-scenario-list">
          {filteredScenarios.length ? <div className="recommend-scenario-head">
            <span>选择</span>
            <span>图标</span>
            <span>标题</span>
            <span>关联通道</span>
            <span>说明</span>
            <span>点击</span>
            <span>排序</span>
            <span>操作</span>
            <span>状态</span>
          </div> : null}
          {filteredScenarios.length ? filteredScenarios.map(({ scenario, index }) => (
            <div className="scenario-row recommend-scenario-row" key={scenario.id}>
              <input type="checkbox" aria-label={`选择场景推荐 ${scenario.title}`} checked={selectedSet.has(scenario.id)} onChange={() => toggleSelection(scenario.id)} />
              <input className="input" value={scenario.icon} onChange={(event) => update(index, { icon: event.target.value.slice(0, 2) })} />
              <input className="input" value={scenario.title} onChange={(event) => update(index, { title: event.target.value })} />
              <select className="input" value={scenario.channelId} onChange={(event) => update(index, { channelId: event.target.value })}>
                {draft.channels.map((channel) => <option value={channel.id} key={channel.id}>{channel.name}</option>)}
              </select>
              <input className="input" value={scenario.summary} onChange={(event) => update(index, { summary: event.target.value })} />
              <span className="mono">{scenario.clicks}</span>
              <span className="recommend-row-actions">
                <button className="btn btn-ghost btn-sm" onClick={() => moveScenario(index, -1)} disabled={index === 0}>↑</button>
                <button className="btn btn-ghost btn-sm" onClick={() => moveScenario(index, 1)} disabled={index === draft.scenarios.length - 1}>↓</button>
              </span>
              <span className="recommend-row-actions">
                <button className="btn btn-ghost btn-sm" onClick={() => duplicateScenario(index)}>复制</button>
                <button className="btn btn-ghost btn-sm" onClick={() => removeScenario(index)}>删除</button>
              </span>
              <button className={`switch ${scenario.enabled ? "on" : ""}`} aria-label="启用场景" onClick={() => update(index, { enabled: !scenario.enabled })} />
            </div>
          )) : <div className="empty-state"><h4>暂无场景推荐</h4><p>{draft.scenarios.length ? "调整搜索或状态筛选后再试。" : "添加公开平台通道后可创建场景化推荐位。"}</p></div>}
        </div>
      </div>
    </div>
  );
}

function cloneRecommend(payload: RecommendAdminData): RecommendAdminData {
  return JSON.parse(JSON.stringify(payload)) as RecommendAdminData;
}

function normalizeRecommendDraft(payload: RecommendAdminData): RecommendAdminData {
  const channels = payload.channels;
  const rankRules = (payload.rankRules?.length ? payload.rankRules : defaultRankRules()).map((rule, index) => ({
    ...rule,
    id: stableID(rule.id),
    label: cleanText(rule.label) || `榜单规则 ${index + 1}`,
    description: cleanText(rule.description),
    metric: (["overall", "speed", "price", "stable", "custom"].includes(rule.metric) ? rule.metric : "custom") as RecommendRankRule["metric"],
    position: index + 1
  }));
  const picks = payload.picks.map((item, index) => {
    const channel = channelByID(channels, item.channelId, item.channel);
    const channelID = item.channelId || channel?.id || "";
    const title = cleanText(item.title) || channel?.name || `推荐位 ${index + 1}`;
    return {
      ...item,
      id: stableID(item.id),
      channelId: channelID,
      channel,
      position: index + 1,
      title,
      ribbon: cleanText(item.ribbon) || "编辑精选",
      summary: cleanText(item.summary) || [channel?.provider, channel?.model].filter(Boolean).join(" · "),
      points: cleanLines(item.points).slice(0, 6),
      ctaLabel: normalizeRecommendCTALabel(item.ctaLabel),
      ctaUrl: normalizeRecommendCTAURL(item.ctaUrl, channel)
    };
  });
  const rewards = payload.rewards.map((item) => {
    const channel = channelByID(channels, item.channelId);
    const channelID = item.channelId || channel?.id || "";
    return {
      ...item,
      id: stableID(item.id),
      channelId: channelID,
      providerName: cleanText(item.providerName) || channel?.name || "未命名入口",
      rewardType: cleanText(item.rewardType) || "接入说明",
      rewardValue: cleanText(item.rewardValue),
      code: cleanText(item.code).toUpperCase(),
      expiresAtText: cleanText(item.expiresAtText) || "长期有效"
    };
  });
  const scenarios = payload.scenarios.map((item, index) => {
    const channel = channelByID(channels, item.channelId, item.channel);
    const channelID = item.channelId || channel?.id || "";
    return {
      ...item,
      id: stableID(item.id),
      title: cleanText(item.title) || `场景 ${index + 1}`,
      icon: cleanText(item.icon).slice(0, 2) || "AI",
      channelId: channelID,
      channel,
      summary: cleanText(item.summary) || [channel?.provider, channel?.model].filter(Boolean).join(" · "),
      position: index + 1
    };
  });
  return { ...payload, picks, rankRules, rewards, scenarios };
}

function validateRecommendDraft(payload: RecommendAdminData): string {
  const channelIDs = new Set(payload.channels.map((item) => item.id));
  if (!payload.channels.length && (payload.picks.length || payload.rewards.length || payload.scenarios.length)) {
    return "请先在通道管理创建并公开至少一个平台通道，再维护推荐配置。";
  }
  if (payload.picks.length > maxRecommendPicks) {
    return `TOP3/推荐位最多支持 ${maxRecommendPicks} 项。`;
  }
  for (const [index, pick] of payload.picks.entries()) {
    if (!pick.channelId || !channelIDs.has(pick.channelId)) return `TOP3 第 ${index + 1} 项关联的通道不存在。`;
    if (!cleanText(pick.title)) return `TOP3 第 ${index + 1} 项缺少标题。`;
    if (!cleanText(pick.summary)) return `TOP3 第 ${index + 1} 项缺少推荐摘要。`;
    if (!cleanLines(pick.points).length) return `TOP3 第 ${index + 1} 项至少需要一个卖点。`;
    if (!cleanText(pick.ctaLabel)) return `TOP3 第 ${index + 1} 项缺少按钮文案。`;
    if (!isValidCTA(pick.ctaUrl)) return `TOP3 第 ${index + 1} 项官方注册页 URL 必须是 http(s) 链接。`;
  }
  for (const [index, rule] of (payload.rankRules ?? []).entries()) {
    if (!cleanText(rule.label)) return `榜单规则第 ${index + 1} 项缺少名称。`;
    if (!["overall", "speed", "price", "stable", "custom"].includes(rule.metric)) return `榜单规则第 ${index + 1} 项类型无效。`;
  }
  for (const [index, reward] of payload.rewards.entries()) {
    if (reward.channelId && !channelIDs.has(reward.channelId)) return `入口说明第 ${index + 1} 项关联的通道不存在。`;
    if (!cleanText(reward.providerName)) return `入口说明第 ${index + 1} 项缺少服务商名称。`;
    if (!cleanText(reward.rewardValue)) return `入口说明第 ${index + 1} 项缺少说明内容。`;
  }
  for (const [index, scenario] of payload.scenarios.entries()) {
    if (!scenario.channelId || !channelIDs.has(scenario.channelId)) return `场景第 ${index + 1} 项关联的通道不存在。`;
    if (!cleanText(scenario.title)) return `场景第 ${index + 1} 项缺少标题。`;
    if (!cleanText(scenario.summary)) return `场景第 ${index + 1} 项缺少说明。`;
  }
  return "";
}

function defaultRankRules(): RecommendRankRule[] {
  return [
    { id: "rank_rule_overall", label: "综合榜", description: "按运营配置顺序兜底展示", metric: "overall", position: 1, enabled: true },
    { id: "rank_rule_speed", label: "速度王", description: "按 P95 延迟由低到高排序", metric: "speed", position: 2, enabled: true },
    { id: "rank_rule_price", label: "性价比", description: "按价格倍数和质量评分加权", metric: "price", position: 3, enabled: true },
    { id: "rank_rule_stable", label: "稳定王", description: "按 30 天成功率排序", metric: "stable", position: 4, enabled: true }
  ];
}

function stableID(id: string) {
  return id.startsWith("tmp_") ? "" : id;
}

function tmpID(prefix: string) {
  return `tmp_${prefix}_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`;
}

function cleanText(value: string | undefined) {
  return (value ?? "").trim();
}

function cleanLines(value: string[] | undefined) {
  return (value ?? []).map((item) => item.trim()).filter(Boolean);
}

function isValidCTA(value: string) {
  const url = cleanText(value);
  return isHTTPURL(url);
}

function channelByID(channels: PublicChannel[], channelID: string, fallback?: PublicChannel): PublicChannel {
  return channels.find((item) => item.id === channelID) ?? fallback ?? channels[0];
}

function defaultOfficialURL(channel?: PublicChannel) {
  return channel ? cleanText(channel.officialSiteUrl) || officialEntryURL(channel.endpoint) : "";
}

function normalizeRecommendCTALabel(value: string | undefined) {
  const label = cleanText(value);
  return label && !["查看详情", "立即试用"].includes(label) ? label : defaultCTALabel;
}

function normalizeRecommendCTAURL(value: string | undefined, channel?: PublicChannel) {
  const href = cleanText(value);
  if (href && !isLegacyRecommendCTAURL(href)) return href;
  return defaultOfficialURL(channel) || href;
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

function isLegacyRecommendCTAURL(href: string) {
  return href === "/login" || href.startsWith("/channels/");
}

function initials(value: string) {
  return value.split(/\s+/).filter(Boolean).slice(0, 2).map((part) => part[0]).join("").toUpperCase().slice(0, 2) || "T";
}

function formatInt(value: number) {
  if (value >= 1000000) return `${(value / 1000000).toFixed(1)}M`;
  if (value >= 1000) return `${(value / 1000).toFixed(1)}K`;
  return String(value);
}
