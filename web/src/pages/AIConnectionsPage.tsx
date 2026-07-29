import { FormEvent, useEffect, useMemo, useState } from "react";
import { ConsoleShell } from "../components/ConsoleShell";
import {
  AIAuthorizationStart,
  AIConnection,
  AIConnectionAuthMethod,
  AIConnectionProvider,
  AIQuickRelayResult,
  aiConnectionAuthorization,
  aiConnectionProviders,
  aiConnections,
  cancelAIConnectionAuthorization,
  completeAIConnectionAuthorization,
  createAIConnection,
  deleteAIConnection,
  disconnectAIConnection,
  quickCreateAIConnectionRelay,
  rotateAIConnectionCredential,
  startAIConnectionAuthorization,
  stepUpAIConnectionAuthorization,
  validateAIConnection
} from "../lib/api";

type ConnectionDraft = {
  authMethod: string;
  displayName: string;
  region: string;
  workspaceId: string;
  projectId: string;
  apiKey: string;
  models: string;
  password: string;
  callbackUrl: string;
  confirmBillable: boolean;
  confirmExperimental: boolean;
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

const authorizationTerminalStatuses = new Set(["completed", "failed", "cancelled", "expired"]);

export function AIConnectionsPage() {
  const [providers, setProviders] = useState<AIConnectionProvider[]>([]);
  const [items, setItems] = useState<AIConnection[]>([]);
  const [selectedProviderCode, setSelectedProviderCode] = useState("");
  const [selectedConnectionId, setSelectedConnectionId] = useState("");
  const [draft, setDraft] = useState<ConnectionDraft>(emptyConnectionDraft);
  const [relayDraft, setRelayDraft] = useState<RelayDraft>(emptyRelayDraft);
  const [authorization, setAuthorization] = useState<AIAuthorizationStart | null>(null);
  const [reauthorizeConnectionId, setReauthorizeConnectionId] = useState("");
  const [rotateKey, setRotateKey] = useState("");
  const [rotateBillableConfirmed, setRotateBillableConfirmed] = useState(false);
  const [setupOpen, setSetupOpen] = useState(false);
  const [rotateOpen, setRotateOpen] = useState(false);
  const [relayOpen, setRelayOpen] = useState(false);
  const [deleteArmed, setDeleteArmed] = useState(false);
  const [deletePassword, setDeletePassword] = useState("");
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
  const selectedAuthMethod = useMemo(
    () => selectedProvider?.authMethods.find((method) => method.code === draft.authMethod) ?? null,
    [selectedProvider, draft.authMethod]
  );
  const usesInteractiveAuthorization = draft.authMethod === "oauth" || draft.authMethod === "codex_oauth";
  const usesGuidedAPIKey = draft.authMethod === "api_key_guided";

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
    if (!selectedProvider || reauthorizeConnectionId) return;
    const preferred = preferredAuthMethod(selectedProvider);
    setDraft(connectionDraftForProvider(selectedProvider, preferred.code));
    setAuthorization(null);
  }, [selectedProvider, reauthorizeConnectionId]);

  useEffect(() => {
    if (!selectedConnection) return;
    const experimental = selectedConnection.authMethod === "codex_oauth";
    setRelayDraft({
      name: `${selectedConnection.displayName} 个人中转`,
      policy: "latency",
      qpsLimit: experimental ? 1 : 20,
      quotaMonth: experimental ? 1000 : 100000,
      modelIds: selectedConnection.models
        .filter((model) => model.enabled && model.verificationStatus === "verified")
        .map((model) => model.id)
    });
    setRotateOpen(false);
    setRelayOpen(false);
    setDeleteArmed(false);
    setDeletePassword("");
    setRelayResult(null);
    setRelayAttempt(null);
  }, [selectedConnectionId]);

  useEffect(() => {
    if (!authorization) return;
    let active = true;
    let timer = 0;
    const poll = async () => {
      try {
        const payload = await aiConnectionAuthorization(authorization.id);
        if (!active) return;
        const attempt = payload.authorization;
        if (attempt.status === "completed") {
          const connections = await aiConnections();
          if (!active) return;
          setItems(connections.items);
          if (attempt.connectionId) setSelectedConnectionId(attempt.connectionId);
          setAuthorization(null);
          setSetupOpen(false);
          setReauthorizeConnectionId("");
          setNotice("账号授权和模型验证已完成，个人连接已经可用。");
          return;
        }
        if (authorizationTerminalStatuses.has(attempt.status)) {
          setError(attempt.errorMessage || "授权没有完成，请重新发起。");
          setAuthorization(null);
          return;
        }
      } catch (err) {
        if (active) setError(errorMessage(err));
      }
      if (active) {
        timer = window.setTimeout(poll, Math.max(1000, authorization.pollIntervalMs || 1500));
      }
    };
    timer = window.setTimeout(poll, 800);
    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, [authorization]);

  useEffect(() => {
    const receiveAuthorizationResult = (event: MessageEvent) => {
      if (event.origin !== window.location.origin) return;
      const value = event.data as { type?: string; status?: string; id?: string };
      if (value.type !== "tokhub:ai-authorization" || value.id !== authorization?.id) return;
      if (value.status === "failed") setError("服务商授权没有完成，请重新尝试。");
    };
    window.addEventListener("message", receiveAuthorizationResult);
    return () => window.removeEventListener("message", receiveAuthorizationResult);
  }, [authorization?.id]);

  function openProvider(provider: AIConnectionProvider) {
    const preferred = preferredAuthMethod(provider);
    setSelectedProviderCode(provider.code);
    setReauthorizeConnectionId("");
    setDraft(connectionDraftForProvider(provider, preferred.code));
    setSetupOpen(true);
    setAuthorization(null);
    setError("");
    setNotice("");
  }

  function chooseAuthMethod(method: AIConnectionAuthMethod) {
    if (!selectedProvider || !method.enabled || authorization) return;
    setDraft(connectionDraftForProvider(selectedProvider, method.code));
    setError("");
  }

  async function submitConnection(event: FormEvent) {
    event.preventDefault();
    if (!selectedProvider) return;
    if (usesInteractiveAuthorization || (usesGuidedAPIKey && !authorization)) {
      await beginAuthorization();
      return;
    }
    setWorking("create");
    setError("");
    setNotice("");
    try {
      const payload = await createAIConnection({
        provider: selectedProvider.code,
        authMethod: usesGuidedAPIKey ? "api_key_guided" : "api_key",
        authorizationId: usesGuidedAPIKey ? authorization?.id : undefined,
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
      setAuthorization(null);
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

  async function beginAuthorization() {
    if (!selectedProvider || !selectedAuthMethod) return;
    const popup = window.open("", "tokhub-ai-authorization", "popup,width=760,height=820");
    setWorking("authorize");
    setError("");
    setNotice("");
    try {
      const stepUp = await stepUpAIConnectionAuthorization(draft.password);
      const started = await startAIConnectionAuthorization({
        provider: selectedProvider.code,
        method: selectedAuthMethod.code,
        stepUpGrant: stepUp.grant,
        displayName: draft.displayName.trim(),
        projectId: draft.projectId.trim() || undefined,
        models: splitModels(draft.models),
        termsAckVersion: draft.authMethod === "codex_oauth" ? "chatgpt-codex-experimental-v1" : undefined,
        existingConnectionId: reauthorizeConnectionId || undefined
      });
      setAuthorization(started);
      setDraft((current) => ({ ...current, password: "" }));
      if (popup) {
        popup.location.href = started.authorizationUrl;
        popup.focus();
      } else {
        setNotice("浏览器阻止了授权窗口，请使用下方按钮继续。");
      }
    } catch (err) {
      popup?.close();
      setError(errorMessage(err));
    } finally {
      setWorking("");
    }
  }

  async function completePastedCallback() {
    if (!authorization) return;
    setWorking("complete");
    setError("");
    try {
      const payload = await completeAIConnectionAuthorization(authorization.id, draft.callbackUrl);
      const connections = await aiConnections();
      setItems(connections.items);
      setSelectedConnectionId(payload.connection.id);
      setAuthorization(null);
      setSetupOpen(false);
      setReauthorizeConnectionId("");
      setNotice("ChatGPT 授权、凭证加密保存和模型验证已完成。");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setWorking("");
    }
  }

  async function cancelAuthorization() {
    if (!authorization) return;
    setWorking("cancel");
    try {
      await cancelAIConnectionAuthorization(authorization.id);
      setAuthorization(null);
      setNotice("本次授权已取消，临时状态已清理。");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setWorking("");
    }
  }

  function openReauthorization(connection: AIConnection) {
    const provider = providers.find((item) => item.code === connection.provider);
    if (!provider) return;
    setSelectedProviderCode(provider.code);
    setReauthorizeConnectionId(connection.id);
    setAuthorization(null);
    setDraft({
      ...connectionDraftForProvider(provider, connection.authMethod),
      displayName: connection.displayName,
      models: connection.models.map((model) => model.providerModelId).join("\n"),
      projectId: typeof connection.providerConfig.projectId === "string" ? connection.providerConfig.projectId : ""
    });
    setSetupOpen(true);
    setError("");
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
      const safeDraft = selectedConnection.authMethod === "codex_oauth"
        ? { ...relayDraft, qpsLimit: 1 }
        : relayDraft;
      const signature = JSON.stringify(safeDraft);
      const attempt = relayAttempt?.signature === signature
        ? relayAttempt
        : { signature, key: newRelayIdempotencyKey() };
      setRelayAttempt(attempt);
      const payload = await quickCreateAIConnectionRelay(selectedConnection.id, safeDraft, attempt.key);
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
      if (requiresDisconnectPassword(selectedConnection.authMethod)) {
        if (!deletePassword.trim()) {
          setError("请输入当前 TokHub 登录密码后断开授权连接。");
          return;
        }
        await disconnectAIConnection(selectedConnection.id, deletePassword);
      } else {
        await deleteAIConnection(selectedConnection.id);
      }
      const remaining = items.filter((item) => item.id !== selectedConnection.id);
      setItems(remaining);
      setSelectedConnectionId(remaining[0]?.id ?? "");
      setDeleteArmed(false);
      setDeletePassword("");
      setNotice("连接、受管通道和上游凭证已停用，密文已擦除；支持撤销的服务商授权已提交撤销。");
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
    <ConsoleShell title="AI 服务连接" crumb="/ 个人空间 / AI 服务连接">
      <main className="console-page ai-connections-page">
        <header className="ai-connect-hero">
          <div>
            <span className="ai-connect-eyebrow">PERSONAL AI CONNECTIONS</span>
            <h1>连接你的 AI 服务</h1>
            <p>使用官方 API Key 或受控授权连接个人账号，随后快速创建个人中转站。</p>
          </div>
          <button
            className="btn btn-primary"
            type="button"
            onClick={() => {
              setReauthorizeConnectionId("");
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
            <p>连接固定保存在个人空间。系统只接受官方 API Key、官方 OAuth 和显式开启的 Codex OAuth；服务商密码、验证码、浏览器 Cookie、Local Storage、cf_clearance 与 PoW 数据均不采集。</p>
          </div>
          <span className="ai-safety-meta">AES-256-GCM · 单次 state · 审计留痕</span>
        </section>

        {error ? <div className="form-error ai-live-message" role="alert">{error}</div> : null}
        {notice ? <div className="form-notice ai-live-message" role="status">{notice}</div> : null}

        <section className="ai-provider-section">
          <div className="section-head">
            <div>
              <h2>支持的官方服务</h2>
              <span className="sub">可用方式由管理员开关和服务商配置共同决定</span>
            </div>
            <span className="tag">{providers.length || 7} 家</span>
          </div>
          <div className="ai-provider-grid" aria-busy={loading}>
            {providers.map((provider) => {
              const enabledMethods = provider.authMethods.filter((method) => method.enabled);
              const oauthEnabled = enabledMethods.some((method) => ["oauth", "codex_oauth"].includes(method.code));
              const guidedEnabled = enabledMethods.some((method) => method.code === "api_key_guided");
              const unavailableInteractive = unavailableInteractiveAuthMethods(provider);
              const methodLabels = enabledMethods.map((method) => method.label);
              if (unavailableInteractive.length > 0) methodLabels.push("登录能力待配置");
              return (
                <button
                  className={`ai-provider-item ${selectedProviderCode === provider.code && setupOpen ? "selected" : ""}`}
                  type="button"
                  key={provider.code}
                  onClick={() => openProvider(provider)}
                >
                  <span className={`ai-provider-mark provider-${provider.code}`}>{providerMarks[provider.code] || provider.name.slice(0, 1)}</span>
                  <span className="ai-provider-copy">
                    <b>{provider.name}</b>
                    <small>{methodLabels.join(" · ")}</small>
                  </span>
                  <span className="ai-provider-action">{oauthEnabled ? "授权 / 密钥" : guidedEnabled ? "官网引导" : unavailableInteractive.length > 0 ? "授权待配置" : "密钥连接"}</span>
                </button>
              );
            })}
            {loading ? <div className="ai-provider-loading">正在加载服务商目录…</div> : null}
          </div>
        </section>

        {setupOpen && selectedProvider ? (
          <section className="ai-setup-panel" aria-labelledby="ai-setup-title">
            <div className="ai-panel-head">
              <div className="ai-panel-title">
                <span className={`ai-provider-mark provider-${selectedProvider.code}`}>{providerMarks[selectedProvider.code]}</span>
                <div>
                  <span>{reauthorizeConnectionId ? "重新授权" : "新增个人连接"}</span>
                  <h2 id="ai-setup-title">{selectedProvider.name}</h2>
                </div>
              </div>
              <button className="btn btn-ghost btn-sm" type="button" onClick={() => {
                setSetupOpen(false);
                setAuthorization(null);
                setReauthorizeConnectionId("");
              }}>关闭</button>
            </div>

            <div className="ai-auth-methods" role="radiogroup" aria-label="连接方式">
              {selectedProvider.authMethods.filter((method) => method.enabled).map((method) => (
                <button
                  className={`ai-auth-method ${draft.authMethod === method.code ? "selected" : ""}`}
                  type="button"
                  role="radio"
                  aria-checked={draft.authMethod === method.code}
                  disabled={!!authorization || (!!reauthorizeConnectionId && draft.authMethod !== method.code)}
                  key={method.code}
                  onClick={() => chooseAuthMethod(method)}
                >
                  <span>
                    <b>{method.label}</b>
                    <i>{releaseLabel(method.release)}</i>
                  </span>
                  <small>{method.description}</small>
                </button>
              ))}
            </div>
            {unavailableInteractiveAuthMethods(selectedProvider).length > 0 ? (
              <div className="form-notice" role="status">
                <b>其他登录方式待部署配置</b>
                {unavailableInteractiveAuthMethods(selectedProvider).map((method) => (
                  <p key={method.code}><strong>{method.label}</strong>：{method.unavailableReason || "当前部署尚未启用。"}</p>
                ))}
              </div>
            ) : null}

            <form className="ai-setup-form" onSubmit={submitConnection}>
              <label>
                <span>连接名称</span>
                <input className="input" required maxLength={80} value={draft.displayName} onChange={(event) => setDraft({ ...draft, displayName: event.target.value })} />
              </label>
              <label>
                <span>地域 / API 产品区</span>
                <select className="input" value={draft.region} disabled={usesInteractiveAuthorization} onChange={(event) => setDraft({ ...draft, region: event.target.value, workspaceId: "" })}>
                  {selectedProvider.regions.map((region) => <option value={region.code} key={region.code}>{region.name}</option>)}
                </select>
              </label>
              {selectedProvider.regions.find((region) => region.code === draft.region)?.workspaceId && !usesInteractiveAuthorization ? (
                <label>
                  <span>Workspace ID <em>选填，用于专属接入点</em></span>
                  <input className="input" value={draft.workspaceId} onChange={(event) => setDraft({ ...draft, workspaceId: event.target.value })} placeholder="例如 workspace-id" />
                </label>
              ) : null}
              {draft.authMethod === "oauth" ? (
                <label>
                  <span>Google Cloud Project ID</span>
                  <input className="input" required value={draft.projectId} onChange={(event) => setDraft({ ...draft, projectId: event.target.value })} placeholder="用于 Gemini API 计费与配额" />
                </label>
              ) : null}
              <label className="ai-form-wide">
                <span>模型 ID <em>每行或逗号分隔，最多 16 个</em></span>
                <textarea className="input ai-model-input" required value={draft.models} onChange={(event) => setDraft({ ...draft, models: event.target.value })} />
              </label>

              {draft.authMethod === "api_key" || (usesGuidedAPIKey && authorization) ? (
                <>
                  <label className="ai-form-wide">
                    <span>{selectedProvider.credentialLabel}</span>
                    <input className="input ai-secret-input" type="password" autoComplete="new-password" required value={draft.apiKey} onChange={(event) => setDraft({ ...draft, apiKey: event.target.value })} placeholder="粘贴官方开发者 API Key" />
                    <small>页面不会回显完整凭证。提交后会发送最小生成请求验证模型权限。</small>
                  </label>
                  <label className="ai-billable-confirm ai-form-wide">
                    <input type="checkbox" required checked={draft.confirmBillable} onChange={(event) => setDraft({ ...draft, confirmBillable: event.target.checked })} />
                    <span>我确认验证会向服务商发送最小生成请求，并可能产生少量费用。</span>
                  </label>
                </>
              ) : null}

              {(usesInteractiveAuthorization || (usesGuidedAPIKey && !authorization)) ? (
                <>
                  <label className="ai-form-wide">
                    <span>TokHub 登录密码 <em>用于本次敏感操作二次验证</em></span>
                    <input className="input" type="password" autoComplete="current-password" required value={draft.password} onChange={(event) => setDraft({ ...draft, password: event.target.value })} />
                    <small>密码只提交给 TokHub，不会发送给 AI 服务商。</small>
                  </label>
                  {draft.authMethod === "codex_oauth" ? (
                    <label className="ai-experimental-confirm ai-form-wide">
                      <input type="checkbox" required checked={draft.confirmExperimental} onChange={(event) => setDraft({ ...draft, confirmExperimental: event.target.checked })} />
                      <span>
                        我了解该能力依赖 ChatGPT Codex 的消费者授权和私有接口，服务商变更可能造成中断。连接仅限本人使用，系统会执行严格限流和重新授权保护。
                      </span>
                    </label>
                  ) : null}
                  {selectedAuthMethod?.riskNotice ? <p className="ai-risk-notice ai-form-wide">{selectedAuthMethod.riskNotice}</p> : null}
                </>
              ) : null}

              {authorization ? (
                <section className="ai-authorization-pending ai-form-wide" aria-live="polite">
                  <div>
                    <span className="ai-auth-spinner" />
                    <div>
                      <b>{authorizationTitle(draft.authMethod)}</b>
                      <p>{authorizationInstructions(draft.authMethod)}</p>
                    </div>
                  </div>
                  <div className="ai-authorization-actions">
                    <a className="btn btn-ghost btn-sm" href={authorization.authorizationUrl} target="_blank" rel="noreferrer">继续授权 ↗</a>
                    <button className="btn btn-ghost btn-sm" type="button" disabled={working === "cancel"} onClick={() => void cancelAuthorization()}>取消本次授权</button>
                  </div>
                  {draft.authMethod === "codex_oauth" ? (
                    <div className="ai-callback-complete">
                      <label>
                        <span>浏览器最终停留的 localhost 回调地址</span>
                        <input className="input" type="url" required value={draft.callbackUrl} onChange={(event) => setDraft({ ...draft, callbackUrl: event.target.value })} placeholder="http://localhost:1455/auth/callback?code=…&state=…" />
                      </label>
                      <button className="btn btn-primary btn-sm" type="button" disabled={working === "complete" || !draft.callbackUrl} onClick={() => void completePastedCallback()}>
                        {working === "complete" ? "正在验证…" : "完成授权"}
                      </button>
                    </div>
                  ) : null}
                </section>
              ) : null}

              <div className="ai-setup-footer ai-form-wide">
                <a href={selectedAuthMethod?.docsUrl || selectedProvider.docsUrl} target="_blank" rel="noreferrer">查看官方说明 ↗</a>
                {!authorization || (usesGuidedAPIKey && authorization) ? (
                  <button className="btn btn-primary" disabled={!!working} type="submit">
                    {submitLabel(draft.authMethod, !!authorization, working)}
                  </button>
                ) : null}
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
              <small>个人空间</small>
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
              <button className={`ai-connection-row ${selectedConnectionId === item.id ? "active" : ""}`} type="button" key={item.id} onClick={() => setSelectedConnectionId(item.id)}>
                <span className={`ai-provider-mark provider-${item.provider}`}>{providerMarks[item.provider] || "AI"}</span>
                <span className="ai-connection-row-copy">
                  <b>{item.displayName}</b>
                  <small>{authMethodLabel(item.authMethod)} · {item.accountMask || item.secretMask}</small>
                </span>
                <ConnectionStatus status={item.status} authStatus={item.authStatus} compact />
              </button>
            ))}
          </aside>

          <section className="ai-connection-detail">
            {selectedConnection ? (
              <>
                <div className="ai-detail-head">
                  <div>
                    <span>{selectedConnection.productLine} · {authMethodLabel(selectedConnection.authMethod)}</span>
                    <h2>{selectedConnection.displayName}</h2>
                    <p>{selectedConnection.endpoint}</p>
                  </div>
                  <ConnectionStatus status={selectedConnection.status} authStatus={selectedConnection.authStatus} />
                </div>

                <div className="ai-detail-metrics">
                  <Metric label={selectedConnection.authMethod === "api_key" || selectedConnection.authMethod === "api_key_guided" ? "凭证" : "授权账号"} value={selectedConnection.accountMask || selectedConnection.secretMask} />
                  <Metric label="使用范围" value={selectedConnection.sharingScope === "personal" ? "仅本人" : selectedConnection.sharingScope} />
                  <Metric label="模型" value={`${selectedConnection.models.length} 个`} />
                  <Metric label="最后验证" value={formatDate(selectedConnection.lastValidatedAt)} />
                </div>

                {selectedConnection.riskLevel === "experimental" ? (
                  <div className="ai-experimental-box">
                    <b>实验连接已启用保护</b>
                    <p>该连接采用个人范围、单中转和低频率策略。服务商授权失效后会暂停转发并要求重新授权。</p>
                  </div>
                ) : null}

                {selectedConnection.status !== "active" || ["attention", "reauth_required", "revoked"].includes(selectedConnection.authStatus) ? (
                  <div className="ai-attention-box">
                    <b>连接需要处理</b>
                    <p>{selectedConnection.lastErrorMessage || "请检查授权状态、模型权限、余额和地域后重新验证。"}</p>
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
                  {selectedConnection.authMethod === "oauth" || selectedConnection.authMethod === "codex_oauth" ? (
                    <button className="btn btn-ghost" type="button" disabled={!!working} onClick={() => openReauthorization(selectedConnection)}>重新授权</button>
                  ) : (
                    <button className="btn btn-ghost" type="button" disabled={!!working} onClick={() => setRotateOpen((open) => !open)}>轮换凭证</button>
                  )}
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
                      <input type="checkbox" required checked={rotateBillableConfirmed} onChange={(event) => setRotateBillableConfirmed(event.target.checked)} />
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
                      <span className="ai-step-badge">{selectedConnection.authMethod === "codex_oauth" ? "实验限流" : "约 10 秒"}</span>
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
                        <input className="input" type="number" min={1} max={selectedConnection.authMethod === "codex_oauth" ? 1 : 1000} disabled={selectedConnection.authMethod === "codex_oauth"} value={relayDraft.qpsLimit} onChange={(event) => setRelayDraft({ ...relayDraft, qpsLimit: Number(event.target.value) })} />
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
                          <input type="checkbox" disabled={!model.enabled || model.verificationStatus !== "verified"} checked={relayDraft.modelIds.includes(model.id)} onChange={(event) => toggleRelayModel(model.id, event.target.checked)} />
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
                      {requiresDisconnectPassword(selectedConnection.authMethod) ? (
                        <label className="ai-danger-password">
                          <span>当前 TokHub 登录密码</span>
                          <input className="input" type="password" autoComplete="current-password" required value={deletePassword} onChange={(event) => setDeletePassword(event.target.value)} />
                        </label>
                      ) : null}
                      <div>
                        <button className="btn btn-ghost btn-sm" type="button" onClick={() => { setDeleteArmed(false); setDeletePassword(""); }}>取消</button>
                        <button className="btn btn-sm danger-lite" type="button" disabled={working === "delete" || (requiresDisconnectPassword(selectedConnection.authMethod) && !deletePassword.trim())} onClick={() => void removeConnection()}>{working === "delete" ? "正在删除…" : requiresDisconnectPassword(selectedConnection.authMethod) ? "验证并断开连接" : "确认删除连接"}</button>
                      </div>
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
                <p>连接成功后可在这里验证、更新授权并创建个人中转。</p>
              </div>
            )}
          </section>
        </section>
      </main>
    </ConsoleShell>
  );
}

function ConnectionStatus({ status, authStatus, compact = false }: { status: string; authStatus?: string; compact?: boolean }) {
  const active = status === "active" && (!authStatus || authStatus === "active" || authStatus === "refreshing");
  return (
    <span className={`ai-status ${active ? "active" : "attention"} ${compact ? "compact" : ""}`}>
      <i />
      {active ? (authStatus === "refreshing" ? "续期中" : "已连接") : authStatus === "reauth_required" ? "需重登" : "需处理"}
    </span>
  );
}

function requiresDisconnectPassword(authMethod: string): boolean {
  return authMethod === "oauth" || authMethod === "codex_oauth";
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

function preferredAuthMethod(provider: AIConnectionProvider) {
  return provider.authMethods.find((method) => method.enabled && ["oauth", "codex_oauth", "api_key_guided"].includes(method.code))
    || provider.authMethods.find((method) => method.enabled)
    || {
      code: "api_key",
      label: "官方 API Key",
      release: "stable",
      sharingScope: "personal",
      completionMode: "api_key",
      enabled: true,
      description: "使用官方开发者 API Key。",
      docsUrl: provider.docsUrl
    };
}

function unavailableInteractiveAuthMethods(provider: AIConnectionProvider) {
  return provider.authMethods.filter((method) => !method.enabled && method.code !== "api_key");
}

function connectionDraftForProvider(provider: AIConnectionProvider, authMethod: string): ConnectionDraft {
  return {
    ...emptyConnectionDraft,
    authMethod,
    displayName: `我的 ${provider.name}`,
    region: provider.defaultRegion,
    models: provider.recommendedModels.join("\n")
  };
}

function authMethodLabel(method: string) {
  switch (method) {
    case "oauth": return "官方 OAuth";
    case "codex_oauth": return "Codex OAuth";
    case "api_key_guided": return "开放平台密钥";
    default: return "官方 API Key";
  }
}

function releaseLabel(release: string) {
  switch (release) {
    case "experimental": return "实验";
    case "preview": return "预览";
    default: return "稳定";
  }
}

function authorizationTitle(method: string) {
  if (method === "api_key_guided") return "请在 DeepSeek 开放平台创建 API Key";
  if (method === "codex_oauth") return "请在新窗口完成 ChatGPT 登录";
  return "正在等待 Google 授权结果";
}

function authorizationInstructions(method: string) {
  if (method === "api_key_guided") return "创建密钥后返回此页粘贴。TokHub 不接触 DeepSeek 网页登录态。";
  if (method === "codex_oauth") return "登录完成后复制浏览器最终停留的 localhost 地址，再粘贴到下方。";
  return "授权窗口完成后会自动关闭，本页将继续验证账号和模型。";
}

function submitLabel(method: string, hasAuthorization: boolean, working: string) {
  if (working === "authorize") return "正在发起授权…";
  if (working === "create") return "正在连接并验证…";
  if (method === "api_key_guided" && !hasAuthorization) return "前往开放平台";
  if (method === "oauth" || method === "codex_oauth") return "打开登录授权";
  return "连接并验证";
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
  authMethod: "api_key",
  displayName: "",
  region: "",
  workspaceId: "",
  projectId: "",
  apiKey: "",
  models: "",
  password: "",
  callbackUrl: "",
  confirmBillable: false,
  confirmExperimental: false
};

const emptyRelayDraft: RelayDraft = {
  name: "",
  policy: "latency",
  qpsLimit: 20,
  quotaMonth: 100000,
  modelIds: []
};
