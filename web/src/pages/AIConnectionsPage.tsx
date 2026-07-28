import { FormEvent, useEffect, useMemo, useState } from "react";
import { ConsoleShell } from "../components/ConsoleShell";
import {
  AIConnection,
  AIConnectionProvider,
  AIQuickRelayResult,
  aiConnectionProviders,
  aiConnections,
  createAIConnection,
  deleteAIConnection,
  quickCreateAIConnectionRelay,
  rotateAIConnectionCredential,
  validateAIConnection
} from "../lib/api";

type ConnectionDraft = {
  displayName: string;
  region: string;
  workspaceId: string;
  apiKey: string;
  models: string;
  confirmBillable: boolean;
};

type RelayDraft = {
  name: string;
  policy: string;
  qpsLimit: number;
  quotaMonth: number;
  modelIds: string[];
};

const providerMarks: Record<string, string> = {
  openai: "OA",
  gemini: "G",
  kimi: "K",
  deepseek: "DS",
  doubao: "豆",
  claude: "C",
  qwen: "千"
};

export function AIConnectionsPage() {
  const [providers, setProviders] = useState<AIConnectionProvider[]>([]);
  const [items, setItems] = useState<AIConnection[]>([]);
  const [selectedProviderCode, setSelectedProviderCode] = useState("");
  const [selectedConnectionId, setSelectedConnectionId] = useState("");
  const [draft, setDraft] = useState<ConnectionDraft>(emptyConnectionDraft);
  const [relayDraft, setRelayDraft] = useState<RelayDraft>(emptyRelayDraft);
  const [rotateKey, setRotateKey] = useState("");
  const [rotateBillableConfirmed, setRotateBillableConfirmed] = useState(false);
  const [setupOpen, setSetupOpen] = useState(false);
  const [rotateOpen, setRotateOpen] = useState(false);
  const [relayOpen, setRelayOpen] = useState(false);
  const [deleteArmed, setDeleteArmed] = useState(false);
  const [relayResult, setRelayResult] = useState<AIQuickRelayResult | null>(null);
  const [relayAttempt, setRelayAttempt] = useState<{ signature: string; key: string } | null>(null);
  const [loading, setLoading] = useState(true);
  const [working, setWorking] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const selectedProvider = useMemo(
    () => providers.find((provider) => provider.code === selectedProviderCode),
    [providers, selectedProviderCode]
  );
  const selectedConnection = useMemo(
    () => items.find((item) => item.id === selectedConnectionId) ?? null,
    [items, selectedConnectionId]
  );

  useEffect(() => {
    let active = true;
    Promise.all([aiConnectionProviders(), aiConnections()])
      .then(([catalog, connections]) => {
        if (!active) return;
        setProviders(catalog.items);
        setItems(connections.items);
        if (connections.items[0]) setSelectedConnectionId(connections.items[0].id);
      })
      .catch((err) => active && setError(errorMessage(err)))
      .finally(() => active && setLoading(false));
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    if (!selectedProvider) return;
    setDraft({
      displayName: `我的 ${selectedProvider.name}`,
      region: selectedProvider.defaultRegion,
      workspaceId: "",
      apiKey: "",
      models: selectedProvider.recommendedModels.join("\n"),
      confirmBillable: false
    });
  }, [selectedProvider]);

  useEffect(() => {
    if (!selectedConnection) return;
    setRelayDraft({
      name: `${selectedConnection.displayName} 个人中转`,
      policy: "latency",
      qpsLimit: 20,
      quotaMonth: 100000,
      modelIds: selectedConnection.models
        .filter((model) => model.enabled && model.verificationStatus === "verified")
        .map((model) => model.id)
    });
    setRotateOpen(false);
    setRelayOpen(false);
    setDeleteArmed(false);
    setRelayResult(null);
    setRelayAttempt(null);
  }, [selectedConnectionId]);

  function openProvider(provider: AIConnectionProvider) {
    setSelectedProviderCode(provider.code);
    setSetupOpen(true);
    setError("");
    setNotice("");
  }

  async function submitConnection(event: FormEvent) {
    event.preventDefault();
    if (!selectedProvider) return;
    setWorking("create");
    setError("");
    setNotice("");
    try {
      const payload = await createAIConnection({
        provider: selectedProvider.code,
        region: draft.region,
        workspaceId: draft.workspaceId.trim() || undefined,
        displayName: draft.displayName.trim(),
        apiKey: draft.apiKey,
        models: splitModels(draft.models),
        confirmBillable: draft.confirmBillable
      });
      setItems((current) => [payload.connection, ...current]);
      setSelectedConnectionId(payload.connection.id);
      setSetupOpen(false);
      setDraft(emptyConnectionDraft);
      setNotice(payload.validation.ok
        ? `${payload.connection.displayName} 已连接并完成最小生成验证。`
        : `${payload.connection.displayName} 已安全保存，请根据验证结果修正凭证或模型。`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setWorking("");
    }
  }

  async function runValidation() {
    if (!selectedConnection) return;
    if (!globalThis.confirm("重新验证会为每个已配置模型发送最小生成请求，并可能产生少量官方费用。确认继续？")) return;
    setWorking("validate");
    setError("");
    setNotice("");
    try {
      const payload = await validateAIConnection(selectedConnection.id);
      replaceConnection(payload.connection);
      setNotice(payload.validation.ok ? "重新验证通过，连接已恢复可用。" : payload.validation.message);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setWorking("");
    }
  }

  async function rotateCredential(event: FormEvent) {
    event.preventDefault();
    if (!selectedConnection) return;
    setWorking("rotate");
    setError("");
    setNotice("");
    try {
      const payload = await rotateAIConnectionCredential(selectedConnection.id, rotateKey);
      replaceConnection(payload.connection);
      setRotateKey("");
      setRotateBillableConfirmed(false);
      setRotateOpen(false);
      setNotice("新凭证验证通过并已完成轮换，旧凭证已失效。");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setWorking("");
    }
  }

  async function createRelay(event: FormEvent) {
    event.preventDefault();
    if (!selectedConnection) return;
    setWorking("relay");
    setError("");
    setNotice("");
    try {
      const signature = JSON.stringify(relayDraft);
      const attempt = relayAttempt?.signature === signature
        ? relayAttempt
        : { signature, key: newRelayIdempotencyKey() };
      setRelayAttempt(attempt);
      const payload = await quickCreateAIConnectionRelay(
        selectedConnection.id,
        relayDraft,
        attempt.key
      );
      setRelayResult(payload);
      setNotice(payload.replay ? "已恢复本次创建结果。" : "个人中转已创建，调用密钥只在当前结果中展示。");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setWorking("");
    }
  }

  async function removeConnection() {
    if (!selectedConnection) return;
    setWorking("delete");
    setError("");
    setNotice("");
    try {
      await deleteAIConnection(selectedConnection.id);
      const remaining = items.filter((item) => item.id !== selectedConnection.id);
      setItems(remaining);
      setSelectedConnectionId(remaining[0]?.id ?? "");
      setDeleteArmed(false);
      setNotice("连接、受管通道和上游凭证已停用，密文已擦除。");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setWorking("");
    }
  }

  function replaceConnection(next: AIConnection) {
    setItems((current) => current.map((item) => item.id === next.id ? next : item));
  }

  function toggleRelayModel(modelId: string, checked: boolean) {
    setRelayDraft((current) => ({
      ...current,
      modelIds: checked
        ? Array.from(new Set([...current.modelIds, modelId]))
        : current.modelIds.filter((id) => id !== modelId)
    }));
  }

  function toggleRelayOpen() {
    if (!relayOpen) {
      setRelayResult(null);
      setRelayAttempt(null);
    }
    setRelayOpen((open) => !open);
  }

  return (
    <ConsoleShell title="AI 服务连接" crumb="/ 工作区 / AI 服务连接">
      <main className="console-page ai-connections-page">
        <header className="ai-connect-hero">
          <div>
            <span className="ai-connect-eyebrow">OFFICIAL DEVELOPER CREDENTIALS</span>
            <h1>连接你的 AI 开发者服务</h1>
            <p>统一保存官方 API Key、验证模型权限，并快速封装为你的个人中转站。</p>
          </div>
          <button
            className="btn btn-primary"
            type="button"
            onClick={() => {
              setSetupOpen(true);
              setSelectedProviderCode(selectedProviderCode || providers[0]?.code || "");
            }}
          >
            ＋ 新增连接
          </button>
        </header>

        <section className="ai-safety-strip" aria-label="凭证安全说明">
          <span className="ai-safety-icon">⌁</span>
          <div>
            <b>凭证保护已开启</b>
            <p>连接固定保存在个人空间，不随顶部工作区切换。仅接受官方开发者 API Key；账号密码、短信验证码、浏览器 Cookie、消费者登录态和 CLI OAuth 会话均不采集。</p>
          </div>
          <span className="ai-safety-meta">AES-256-GCM · 版本化密钥 · 审计留痕</span>
        </section>

        {error ? <div className="form-error ai-live-message" role="alert">{error}</div> : null}
        {notice ? <div className="form-notice ai-live-message" role="status">{notice}</div> : null}

        <section className="ai-provider-section">
          <div className="section-head">
            <div>
              <h2>支持的官方服务</h2>
              <span className="sub">选择产品线后填写对应的开发者凭证和模型 ID</span>
            </div>
            <span className="tag">{providers.length || 7} 家</span>
          </div>
          <div className="ai-provider-grid" aria-busy={loading}>
            {providers.map((provider) => (
              <button
                className={`ai-provider-item ${selectedProviderCode === provider.code && setupOpen ? "selected" : ""}`}
                type="button"
                key={provider.code}
                onClick={() => openProvider(provider)}
              >
                <span className={`ai-provider-mark provider-${provider.code}`}>{providerMarks[provider.code] || provider.name.slice(0, 1)}</span>
                <span className="ai-provider-copy">
                  <b>{provider.name}</b>
                  <small>{provider.productLine}</small>
                </span>
                <span className="ai-provider-action">连接</span>
              </button>
            ))}
            {loading ? <div className="ai-provider-loading">正在加载服务商目录…</div> : null}
          </div>
        </section>

        {setupOpen && selectedProvider ? (
          <section className="ai-setup-panel" aria-labelledby="ai-setup-title">
            <div className="ai-panel-head">
              <div className="ai-panel-title">
                <span className={`ai-provider-mark provider-${selectedProvider.code}`}>{providerMarks[selectedProvider.code]}</span>
                <div>
                  <span>新增连接</span>
                  <h2 id="ai-setup-title">{selectedProvider.name}</h2>
                </div>
              </div>
              <button className="btn btn-ghost btn-sm" type="button" onClick={() => setSetupOpen(false)}>关闭</button>
            </div>
            <form className="ai-setup-form" onSubmit={submitConnection}>
              <label>
                <span>连接名称</span>
                <input className="input" required maxLength={80} value={draft.displayName} onChange={(event) => setDraft({ ...draft, displayName: event.target.value })} />
              </label>
              <label>
                <span>地域 / API 产品区</span>
                <select className="input" value={draft.region} onChange={(event) => setDraft({ ...draft, region: event.target.value, workspaceId: "" })}>
                  {selectedProvider.regions.map((region) => <option value={region.code} key={region.code}>{region.name}</option>)}
                </select>
              </label>
              {selectedProvider.regions.find((region) => region.code === draft.region)?.workspaceId ? (
                <label>
                  <span>Workspace ID <em>选填，用于专属接入点</em></span>
                  <input className="input" value={draft.workspaceId} onChange={(event) => setDraft({ ...draft, workspaceId: event.target.value })} placeholder="例如 workspace-id" />
                </label>
              ) : null}
              <label className="ai-form-wide">
                <span>模型 ID <em>每行或逗号分隔，最多 16 个</em></span>
                <textarea className="input ai-model-input" required value={draft.models} onChange={(event) => setDraft({ ...draft, models: event.target.value })} />
              </label>
              <label className="ai-form-wide">
                <span>{selectedProvider.credentialLabel}</span>
                <input className="input ai-secret-input" type="password" autoComplete="new-password" required value={draft.apiKey} onChange={(event) => setDraft({ ...draft, apiKey: event.target.value })} placeholder="粘贴官方开发者 API Key" />
                <small>提交后会为每个模型发送一次最小生成请求，可能产生少量官方用量。页面不会回显完整凭证。</small>
              </label>
              <label className="ai-billable-confirm ai-form-wide">
                <input
                  type="checkbox"
                  required
                  checked={draft.confirmBillable}
                  onChange={(event) => setDraft({ ...draft, confirmBillable: event.target.checked })}
                />
                <span>我确认验证会向服务商发送最小生成请求，并可能产生少量费用。</span>
              </label>
              <div className="ai-setup-footer ai-form-wide">
                <a href={selectedProvider.docsUrl} target="_blank" rel="noreferrer">查看官方凭证文档 ↗</a>
                <button className="btn btn-primary" disabled={working === "create"} type="submit">
                  {working === "create" ? "正在连接并验证…" : "连接并验证"}
                </button>
              </div>
            </form>
          </section>
        ) : null}

        <section className="ai-connection-workspace">
          <aside className="ai-connection-list" aria-label="已连接账号">
            <div className="ai-list-head">
              <div>
                <span>已连接账号</span>
                <b>{items.length}</b>
              </div>
              <small>仅当前工作区可见</small>
            </div>
            {loading ? <div className="ai-list-empty">正在加载连接…</div> : null}
            {!loading && !items.length ? (
              <div className="ai-list-empty">
                <span>⌁</span>
                <b>还没有 AI 服务连接</b>
                <p>从上方选择一家官方服务开始。</p>
              </div>
            ) : null}
            {items.map((item) => (
              <button
                className={`ai-connection-row ${selectedConnectionId === item.id ? "active" : ""}`}
                type="button"
                key={item.id}
                onClick={() => setSelectedConnectionId(item.id)}
              >
                <span className={`ai-provider-mark provider-${item.provider}`}>{providerMarks[item.provider] || "AI"}</span>
                <span className="ai-connection-row-copy">
                  <b>{item.displayName}</b>
                  <small>{item.models.length} 个模型 · {item.secretMask}</small>
                </span>
                <ConnectionStatus status={item.status} compact />
              </button>
            ))}
          </aside>

          <section className="ai-connection-detail">
            {selectedConnection ? (
              <>
                <div className="ai-detail-head">
                  <div>
                    <span>{selectedConnection.productLine}</span>
                    <h2>{selectedConnection.displayName}</h2>
                    <p>{selectedConnection.endpoint}</p>
                  </div>
                  <ConnectionStatus status={selectedConnection.status} />
                </div>

                <div className="ai-detail-metrics">
                  <Metric label="凭证" value={selectedConnection.secretMask} />
                  <Metric label="模型" value={`${selectedConnection.models.length} 个`} />
                  <Metric label="验证耗时" value={`${selectedConnection.validationLatencyMs || 0} ms`} />
                  <Metric label="最后验证" value={formatDate(selectedConnection.lastValidatedAt)} />
                </div>

                {selectedConnection.status !== "active" ? (
                  <div className="ai-attention-box">
                    <b>连接需要处理</b>
                    <p>{selectedConnection.lastErrorMessage || "请检查 API Key、模型权限、余额和地域后重新验证。"}</p>
                  </div>
                ) : null}

                <div className="ai-model-list">
                  <div className="ai-subsection-head">
                    <div>
                      <span>已配置模型</span>
                      <small>创建个人中转时可以选择其中一项或多项</small>
                    </div>
                  </div>
                  {selectedConnection.models.map((model) => (
                    <div className="ai-model-row" key={model.id}>
                      <span className={`ai-model-dot ${model.verificationStatus === "verified" ? "" : "unverified"}`} />
                      <span className="ai-model-copy">
                        <code>{model.providerModelId}</code>
                        {model.lastErrorMessage ? <small>{model.lastErrorMessage}</small> : null}
                      </span>
                      <span>{model.verificationStatus !== "verified" ? "未通过" : model.routeChannelId ? "已封装通道" : "可封装"} · {model.validationLatencyMs || 0} ms</span>
                    </div>
                  ))}
                </div>

                <div className="ai-detail-actions">
                  <button className="btn btn-ghost" type="button" disabled={!!working} onClick={() => void runValidation()}>
                    {working === "validate" ? "正在验证…" : "重新验证"}
                  </button>
                  <button className="btn btn-ghost" type="button" disabled={!!working} onClick={() => setRotateOpen((open) => !open)}>
                    轮换凭证
                  </button>
                  <button className="btn btn-primary" type="button" disabled={!selectedConnection.models.some((model) => model.enabled && model.verificationStatus === "verified") || !!working} onClick={toggleRelayOpen}>
                    创建个人中转
                  </button>
                </div>

                {rotateOpen ? (
                  <form className="ai-inline-form" onSubmit={rotateCredential}>
                    <div>
                      <b>安全轮换凭证</b>
                      <p>新凭证通过全部已配置模型的最小生成验证后才会替换当前凭证。</p>
                    </div>
                    <label>
                      <span>新的官方 API Key</span>
                      <input className="input" type="password" autoComplete="new-password" required value={rotateKey} onChange={(event) => setRotateKey(event.target.value)} />
                    </label>
                    <label className="ai-billable-confirm">
                      <input
                        type="checkbox"
                        required
                        checked={rotateBillableConfirmed}
                        onChange={(event) => setRotateBillableConfirmed(event.target.checked)}
                      />
                      <span>我确认轮换验证可能产生少量服务商费用。</span>
                    </label>
                    <div className="ai-inline-actions">
                      <button className="btn btn-ghost btn-sm" type="button" onClick={() => { setRotateOpen(false); setRotateKey(""); setRotateBillableConfirmed(false); }}>取消</button>
                      <button className="btn btn-primary btn-sm" type="submit" disabled={working === "rotate"}>{working === "rotate" ? "验证中…" : "验证并轮换"}</button>
                    </div>
                  </form>
                ) : null}

                {relayOpen ? (
                  <form className="ai-relay-form" onSubmit={createRelay}>
                    <div className="ai-subsection-head">
                      <div>
                        <span>一键个人中转</span>
                        <small>创建网关、受管通道和一次性调用密钥</small>
                      </div>
                      <span className="ai-step-badge">约 10 秒</span>
                    </div>
                    <div className="ai-relay-fields">
                      <label>
                        <span>中转名称</span>
                        <input className="input" required maxLength={80} value={relayDraft.name} onChange={(event) => setRelayDraft({ ...relayDraft, name: event.target.value })} />
                      </label>
                      <label>
                        <span>路由策略</span>
                        <select className="input" value={relayDraft.policy} onChange={(event) => setRelayDraft({ ...relayDraft, policy: event.target.value })}>
                          <option value="latency">最低延迟优先</option>
                          <option value="success">成功率优先</option>
                          <option value="cost">成本优先</option>
                        </select>
                      </label>
                      <label>
                        <span>每秒请求上限</span>
                        <input className="input" type="number" min={1} max={1000} value={relayDraft.qpsLimit} onChange={(event) => setRelayDraft({ ...relayDraft, qpsLimit: Number(event.target.value) })} />
                      </label>
                      <label>
                        <span>累计请求次数上限</span>
                        <input className="input" type="number" min={1} value={relayDraft.quotaMonth} onChange={(event) => setRelayDraft({ ...relayDraft, quotaMonth: Number(event.target.value) })} />
                      </label>
                    </div>
                    <fieldset className="ai-model-picker">
                      <legend>选择模型</legend>
                      {selectedConnection.models.map((model) => (
                        <label key={model.id}>
                          <input
                            type="checkbox"
                            disabled={!model.enabled || model.verificationStatus !== "verified"}
                            checked={relayDraft.modelIds.includes(model.id)}
                            onChange={(event) => toggleRelayModel(model.id, event.target.checked)}
                          />
                          <code>{model.providerModelId}</code>
                        </label>
                      ))}
                    </fieldset>
                    <div className="ai-inline-actions">
                      <button className="btn btn-ghost btn-sm" type="button" onClick={() => setRelayOpen(false)}>取消</button>
                      <button className="btn btn-primary btn-sm" type="submit" disabled={working === "relay" || !relayDraft.modelIds.length}>{working === "relay" ? "正在创建…" : "创建个人中转"}</button>
                    </div>
                  </form>
                ) : null}

                {relayResult ? <RelayResult result={relayResult} /> : null}

                <div className="ai-danger-zone">
                  <div>
                    <b>删除连接</b>
                    <p>删除会停用受管通道、暂停失去全部路由的中转站、吊销其调用密钥，并擦除凭证密文。</p>
                  </div>
                  {deleteArmed ? (
                    <div className="ai-danger-actions">
                      <button className="btn btn-ghost btn-sm" type="button" onClick={() => setDeleteArmed(false)}>取消</button>
                      <button className="btn btn-sm danger-lite" type="button" disabled={working === "delete"} onClick={() => void removeConnection()}>{working === "delete" ? "正在删除…" : "确认删除连接"}</button>
                    </div>
                  ) : (
                    <button className="btn btn-ghost btn-sm danger-lite" type="button" onClick={() => setDeleteArmed(true)}>删除连接</button>
                  )}
                </div>
              </>
            ) : (
              <div className="ai-detail-empty">
                <span>AI</span>
                <h2>选择一个连接查看详情</h2>
                <p>连接成功后可在这里验证、轮换凭证并创建个人中转。</p>
              </div>
            )}
          </section>
        </section>
      </main>
    </ConsoleShell>
  );
}

function ConnectionStatus({ status, compact = false }: { status: string; compact?: boolean }) {
  const active = status === "active";
  return (
    <span className={`ai-status ${active ? "active" : "attention"} ${compact ? "compact" : ""}`}>
      <i />
      {active ? "已连接" : "需处理"}
    </span>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return <div className="ai-metric"><span>{label}</span><b>{value}</b></div>;
}

function RelayResult({ result }: { result: AIQuickRelayResult }) {
  const [copied, setCopied] = useState("");
  async function copy(value: string, label: string) {
    await navigator.clipboard.writeText(value);
    setCopied(label);
    window.setTimeout(() => setCopied(""), 1800);
  }
  return (
    <section className="ai-relay-result" aria-live="polite">
      <div className="ai-relay-result-head">
        <span>✓</span>
        <div>
          <b>个人中转已就绪</b>
          <p>{result.gateway.name} · {result.gateway.upstreams.length} 条上游路由</p>
        </div>
        <a className="btn btn-ghost btn-sm" href="/console/gateways">查看中转站</a>
      </div>
      <div className="ai-result-field">
        <span>Base URL</span>
        <code>{result.gateway.baseUrl}</code>
        <button type="button" onClick={() => void copy(result.gateway.baseUrl, "url")}>{copied === "url" ? "已复制" : "复制"}</button>
      </div>
      <div className="ai-result-field secret">
        <span>Gateway Key · 仅展示一次</span>
        <code>{result.key.plainKey}</code>
        <button type="button" onClick={() => void copy(result.key.plainKey || "", "key")}>{copied === "key" ? "已复制" : "复制"}</button>
      </div>
      <p className="ai-result-warning">请立即保存 Gateway Key。离开当前结果后只能吊销并重新签发。</p>
    </section>
  );
}

function splitModels(value: string) {
  return Array.from(new Set(value.split(/[\n,，]/).map((item) => item.trim()).filter(Boolean)));
}

function formatDate(value?: string) {
  if (!value) return "尚未验证";
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit"
  }).format(new Date(value));
}

function errorMessage(value: unknown) {
  return value instanceof Error ? value.message : "操作失败，请稍后重试";
}

function newRelayIdempotencyKey() {
  const browserCrypto = globalThis.crypto;
  if (typeof browserCrypto?.randomUUID === "function") {
    return `ai-relay-${browserCrypto.randomUUID()}`;
  }
  if (typeof browserCrypto?.getRandomValues === "function") {
    const bytes = browserCrypto.getRandomValues(new Uint8Array(16));
    return `ai-relay-${Array.from(bytes, (value) => value.toString(16).padStart(2, "0")).join("")}`;
  }
  return `ai-relay-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 18)}`;
}

const emptyConnectionDraft: ConnectionDraft = {
  displayName: "",
  region: "",
  workspaceId: "",
  apiKey: "",
  models: "",
  confirmBillable: false
};

const emptyRelayDraft: RelayDraft = {
  name: "",
  policy: "latency",
  qpsLimit: 20,
  quotaMonth: 100000,
  modelIds: []
};
