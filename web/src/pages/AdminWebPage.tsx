import { useEffect, useMemo, useState } from "react";
import { AdminShell } from "../components/AdminShell";
import { adminWebConfig, NavItem, resetAdminWebConfig, saveAdminWebConfig, SiteConfig } from "../lib/api";
import { defaultFooterPublicLinks, defaultPrimaryPublicLinks, normalizeLegacyPublicLinks } from "../lib/publicLinks";

export function AdminWebPage() {
  const [site, setSite] = useState<SiteConfig | null>(null);
  const [draft, setDraft] = useState<SiteConfig | null>(null);
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
      const payload = await adminWebConfig();
      setSite(payload.site);
      setDraft(normalizeSite(payload.site));
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载网站配置失败");
    } finally {
      setLoading(false);
    }
  }

  async function save() {
    if (!draft) return;
    const normalized = normalizeSite(draft);
    const validation = validateSiteDraft(normalized);
    if (validation) {
      setError(validation);
      setNotice("");
      return;
    }
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const payload = await saveAdminWebConfig(normalized);
      setSite(payload.site);
      setDraft(normalizeSite(payload.site));
      setNotice("网站配置已保存，公开页面导航、页脚和注册入口会同步更新。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存网站配置失败");
    } finally {
      setSaving(false);
    }
  }

  function reset() {
    if (site) setDraft(normalizeSite(site));
    setError("");
    setNotice("");
  }

  async function restoreDefaults() {
    if (!window.confirm("确认恢复默认网站配置？当前导航、页脚和品牌文案会被默认值覆盖。")) return;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const payload = await resetAdminWebConfig();
      setSite(payload.site);
      setDraft(normalizeSite(payload.site));
      setNotice("网站配置已恢复默认，并已写入审计记录。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "恢复默认配置失败");
    } finally {
      setSaving(false);
    }
  }

  return (
    <AdminShell title="网站设置" crumb="/ 前台首页">
      <div className="admin-actions-line">
        <p className="page-intro">自定义前台站点标识、顶部导航、页脚链接和注册入口显示规则。保存后 `/api/public/site-config` 会驱动所有公开页面</p>
        <div className="admin-actions-buttons">
          <a className="btn btn-ghost btn-sm" href="/" target="_blank" rel="noreferrer">查看前台 →</a>
          <a className="btn btn-ghost btn-sm" href="/dashboard" target="_blank" rel="noreferrer">查看监控总览 →</a>
          <button className="btn btn-ghost btn-sm" onClick={reset} disabled={loading || saving}>放弃更改</button>
          <button className="btn btn-ghost btn-sm" onClick={() => void restoreDefaults()} disabled={loading || saving}>恢复默认</button>
          <button className="btn btn-primary btn-sm" onClick={() => void save()} disabled={loading || saving}>{saving ? "保存中..." : "保存更改"}</button>
        </div>
      </div>
      {error ? <div className="form-error">{error}</div> : null}
      {notice ? <div className="form-notice">{notice}</div> : null}

      {loading || !draft ? (
        <div className="card card-pad empty-state"><h4>正在加载网站配置</h4></div>
      ) : (
        <>
          <div className="nav-preview">
            <div className="pv-bar">
              <span className="dot" style={{ background: "#ff5f57" }} />
              <span className="dot" style={{ background: "#febc2e" }} />
              <span className="dot" style={{ background: "#28c840" }} />
              <span className="pv-url">{draft.publicUrl || "http://localhost:8080"}</span>
              <span style={{ marginLeft: "auto", fontSize: 11, color: "var(--text-4)" }}>实时预览</span>
            </div>
            <div className="pv-nav">
              <div className="pv-logo">
                <span className="lm">{draft.logoMark || "T"}</span>
                <span className="logo-text"><b>{draft.brandName || "TokHub"}</b><small>{draft.subtitle}</small></span>
              </div>
              <div className="pv-links">
                {draft.navItems.map((item, index) => (
                  <a className={index === 0 ? "on" : ""} href={item.href} key={`nav-preview-${index}`}>{item.label}</a>
                ))}
              </div>
              <span style={{ marginLeft: "auto" }} />
              {draft.registrationOpen && draft.showRegisterCta ? <span className="btn btn-ghost btn-sm">登录 / 注册</span> : <span className="btn btn-ghost btn-sm">登录</span>}
            </div>
          </div>

          <div className="card set-card">
            <div className="set-h">站点信息</div>
            <div className="set-row">
              <div className="lbl"><b>站点名称</b><small>显示在公开页面 Logo 与页脚</small></div>
              <div className="ctl"><input className="input" style={{ maxWidth: 320 }} value={draft.brandName} onChange={(event) => setDraft({ ...draft, brandName: event.target.value })} /></div>
            </div>
            <div className="set-row">
              <div className="lbl"><b>副标题</b><small>Logo 下方小字</small></div>
              <div className="ctl"><input className="input" style={{ maxWidth: 320 }} value={draft.subtitle} onChange={(event) => setDraft({ ...draft, subtitle: event.target.value })} /></div>
            </div>
            <div className="set-row">
              <div className="lbl"><b>Logo 标记</b><small>方形图标内的 1-2 个字符</small></div>
              <div className="ctl"><input className="input" style={{ maxWidth: 90 }} maxLength={2} value={draft.logoMark} onChange={(event) => setDraft({ ...draft, logoMark: event.target.value })} /></div>
            </div>
            <div className="set-row">
              <div className="lbl"><b>注册入口显示</b><small>关闭时只保留登录入口，公开看板仍可访问</small></div>
              <div className="ctl">
                <button className={`switch ${draft.showRegisterCta ? "on" : ""}`} aria-label="注册入口显示" onClick={() => setDraft({ ...draft, showRegisterCta: !draft.showRegisterCta })} />
                <span className="muted-time">{draft.showRegisterCta ? "显示登录 / 注册 CTA" : "隐藏注册 CTA"}</span>
              </div>
            </div>
          </div>

          <MenuEditor title="顶部导航菜单" help="最多 5 个一级菜单，保存后由 PublicNav 渲染。" items={draft.navItems} onChange={(navItems) => setDraft({ ...draft, navItems })} max={5} />
          <MenuEditor title="页脚链接" help="页脚中间栏链接，适合放公开页面和控制台入口。" items={draft.footerLinks} onChange={(footerLinks) => setDraft({ ...draft, footerLinks })} max={8} />

          <div className="card set-card">
            <div className="set-h">页脚与版权</div>
            <div className="set-row">
              <div className="lbl"><b>版权声明</b><small>显示在全局页脚右侧</small></div>
              <div className="ctl"><input className="input" style={{ maxWidth: 560 }} value={draft.footerText} onChange={(event) => setDraft({ ...draft, footerText: event.target.value })} /></div>
            </div>
            <div className="set-row">
              <div className="lbl"><b>公开访问地址</b><small>由部署环境变量控制</small></div>
              <div className="ctl"><span className="keyval">{draft.publicUrl}</span></div>
            </div>
          </div>
        </>
      )}
    </AdminShell>
  );
}

function MenuEditor({ title, help, items, onChange, max }: { title: string; help: string; items: NavItem[]; onChange: (items: NavItem[]) => void; max: number }) {
  const [selected, setSelected] = useState<number[]>([]);
  const selectedSet = useMemo(() => new Set(selected), [selected]);
  const allSelected = items.length > 0 && items.every((_, index) => selectedSet.has(index));
  const canDeleteSelected = selected.length > 0 && selected.length < items.length;
  const canDuplicateSelected = selected.length > 0 && items.length < max;

  useEffect(() => {
    setSelected((value) => value.filter((index) => index < items.length));
  }, [items.length]);

  function update(index: number, patch: Partial<NavItem>) {
    onChange(items.map((item, i) => i === index ? { ...item, ...patch } : item));
  }

  function add() {
    if (items.length >= max) return;
    onChange([...items, { label: "新菜单", href: "/" }]);
  }

  function remove(index: number) {
    onChange(items.filter((_, i) => i !== index));
  }

  function toggle(index: number, checked: boolean) {
    setSelected((value) => checked ? [...new Set([...value, index])] : value.filter((item) => item !== index));
  }

  function toggleAll(checked: boolean) {
    setSelected(checked ? items.map((_, index) => index) : []);
  }

  function removeSelected() {
    if (!canDeleteSelected) return;
    onChange(items.filter((_, index) => !selectedSet.has(index)));
    setSelected([]);
  }

  function duplicateSelected() {
    if (!canDuplicateSelected) return;
    const remaining = max - items.length;
    const copies = selected
      .slice()
      .sort((a, b) => a - b)
      .map((index) => items[index])
      .filter(Boolean)
      .slice(0, remaining)
      .map((item) => ({ ...item, label: `${item.label} 副本` }));
    onChange([...items, ...copies]);
    setSelected([]);
  }

  function move(index: number, dir: -1 | 1) {
    const target = index + dir;
    if (target < 0 || target >= items.length) return;
    const next = [...items];
    [next[index], next[target]] = [next[target], next[index]];
    onChange(next);
  }

  return (
    <div className="card set-card">
      <div className="set-h">{title}<span style={{ marginLeft: "auto", fontSize: 12, fontWeight: 500, color: "var(--text-3)" }}>{items.length} / {max}</span></div>
      <div className="ops-help">{help}</div>
      <div style={{ padding: "16px 22px" }}>
        <div className="toolbar bulk-toolbar">
          <label className="bulk-select">
            <input type="checkbox" checked={allSelected} onChange={(event) => toggleAll(event.target.checked)} />
            已选 {selected.length}
          </label>
          <button className="btn btn-ghost btn-sm" onClick={duplicateSelected} disabled={!canDuplicateSelected}>批量复制</button>
          <button className="btn btn-ghost btn-sm" onClick={removeSelected} disabled={!canDeleteSelected}>批量删除</button>
        </div>
        {items.map((item, index) => (
          <div className="menu-item" key={`${title}-${index}`}>
            <div className="mi-head">
              <input type="checkbox" checked={selectedSet.has(index)} aria-label={`选择${title}${item.label}`} onChange={(event) => toggle(index, event.target.checked)} />
              <span className="grip">↕</span>
              <span className="lvl">一级</span>
              <div className="mi-field" style={{ flex: 1 }}>
                <label>菜单名称</label>
                <input className="input" value={item.label} onChange={(event) => update(index, { label: event.target.value })} />
              </div>
              <div className="mi-field" style={{ flex: 1.4 }}>
                <label>跳转链接</label>
                <input className="input mono" value={item.href} onChange={(event) => update(index, { href: event.target.value })} />
              </div>
              <button className="rm-btn" onClick={() => move(index, -1)} disabled={index === 0}>↑</button>
              <button className="rm-btn" onClick={() => move(index, 1)} disabled={index === items.length - 1}>↓</button>
              <button className="rm-btn" onClick={() => remove(index)} disabled={items.length <= 1}>×</button>
            </div>
          </div>
        ))}
        <button className="btn btn-ghost btn-sm" onClick={add} disabled={items.length >= max}>＋ 添加一级菜单</button>
      </div>
    </div>
  );
}

function normalizeSite(site: SiteConfig): SiteConfig {
  return {
    ...site,
    navItems: normalizeLegacyPublicLinks(site.navItems?.length ? site.navItems : defaultPrimaryPublicLinks),
    footerLinks: normalizeLegacyPublicLinks(site.footerLinks?.length ? site.footerLinks : defaultFooterPublicLinks),
    logoMark: site.logoMark || "T",
    subtitle: site.subtitle ?? "API 中转站监控",
    showRegisterCta: site.showRegisterCta ?? site.registrationOpen,
    defaultGatewayPolicy: site.defaultGatewayPolicy || "latency",
    timezone: site.timezone || "Asia/Shanghai"
  };
}

function validateSiteDraft(site: SiteConfig) {
  if (!site.brandName.trim()) return "站点名称不能为空。";
  if (site.logoMark.trim().length < 1 || Array.from(site.logoMark.trim()).length > 2) return "Logo 标记必须是 1-2 个字符。";
  const navError = validateNavItems(site.navItems, 1, 5, "顶部导航");
  if (navError) return navError;
  const footerError = validateNavItems(site.footerLinks, 1, 8, "页脚链接");
  if (footerError) return footerError;
  return "";
}

function validateNavItems(items: NavItem[], minItems: number, maxItems: number, label: string) {
  if (items.length < minItems || items.length > maxItems) return `${label}数量必须在 ${minItems}-${maxItems} 项之间。`;
  for (const [index, item] of items.entries()) {
    if (!item.label.trim()) return `${label}第 ${index + 1} 项缺少名称。`;
    if (!isValidHref(item.href)) return `${label}第 ${index + 1} 项链接必须是站内路径或 http(s) 链接。`;
  }
  return "";
}

function isValidHref(value: string) {
  const href = value.trim().toLowerCase();
  return (href.startsWith("/") && !href.startsWith("//")) || href.startsWith("https://") || href.startsWith("http://");
}
