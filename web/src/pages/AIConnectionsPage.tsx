import { FormEvent, useEffect, useMemo, useState } from "react";
import { ConsoleShell } from "../components/ConsoleShell";
import {
  AIAuthorizationStart,
  AIBrowserConnector,
  AIBrowserConnectorCreateResult,
  AIBrowserRiskState,
  AIConnection,
  AIConnectionAuthMethod,
  AIConnectionProvider,
  AIQuickRelayResult,
  aiConnectionAuthorization,
  aiConnectionProviders,
  aiConnections,
  aiBrowserConnectionRisk,
  aiBrowserConnectors,
  cancelAIConnectionAuthorization,
  createAIBrowserConnection,
  createAIBrowserConnector,
  completeAIConnectionAuthorization,
  createAIConnection,
  deleteAIConnection,
  disconnectAIConnection,
  quickCreateAIConnectionRelay,
  pauseAIBrowserConnection,
  revokeAIBrowserConnector,
  rotateAIConnectionCredential,
  resumeAIBrowserConnection,
  startAIConnectionAuthorization,
  stepUpAIConnectionAuthorization,
  validateAIConnection
} from "../lib/api";
import {
  ChatGPTCallbackStatus,
  DeepSeekExtensionStatus,
  requestChatGPTCallbackFromExtension,
  requestDeepSeekSessionFromExtension
} from "../lib/aiLoginExtension";

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
  deepSeekToken: string;
  confirmBillable: boolean;
  confirmExperimental: boolean;
  connectorId: string;
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
const deepSeekWebLoginURL = "https://chat.deepseek.com";
const aiLoginExtensionDownloadURL = "/downloads/tokhub-ai-login-helper.zip";

export function AIConnectionsPage() {
  const [providers, setProviders] = useState<AIConnectionProvider[]>([]);
  const [items, setItems] = useState<AIConnection[]>([]);
  const [browserConnectors, setBrowserConnectors] = useState<AIBrowserConnector[]>([]);
  const [browserPairing, setBrowserPairing] = useState<AIBrowserConnectorCreateResult | null>(null);
  const [browserRisk, setBrowserRisk] = useState<AIBrowserRiskState | null>(null);
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
  const [deepSeekExtensionStatus, setDeepSeekExtensionStatus] = useState<DeepSeekExtensionStatus | "checking" | "">("");
  const [chatGPTCallbackStatus, setChatGPTCallbackStatus] = useState<ChatGPTCallbackStatus | "checking" | "">("");

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
  const selectedGeminiOAuthMethod = useMemo(
    () => selectedProvider?.code === "gemini"
      ? selectedProvider.authMethods.find((method) => method.code === "oauth") ?? null
      : null,
    [selectedProvider]
  );
  const browserConnectorEnabled = useMemo(
    () => providers.some((provider) => provider.authMethods.some((method) => method.code === "opencli_browser" && method.enabled)),
    [providers]
  );
  const usesInteractiveAuthorization = ["oauth", "codex_oauth", "deepseek_web_token"].includes(draft.authMethod);
  const usesGuidedAPIKey = draft.authMethod === "api_key_guided";
  const usesBrowserConnector = draft.authMethod === "opencli_browser";

  useEffect(() => {
    let active = true;
    Promise.all([
      aiConnectionProviders(),
      aiConnections(),
      aiBrowserConnectors().catch(() => ({ items: [] as AIBrowserConnector[] }))
    ])
      .then(([catalog, connections, connectors]) => {
        if (!active) return;
        setProviders(catalog.items);
        setItems(connections.items);
        setBrowserConnectors(connectors.items);
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
    setDeepSeekExtensionStatus("");
    setChatGPTCallbackStatus("");
  }, [selectedProvider, reauthorizeConnectionId]);

  useEffect(() => {
    if (!selectedConnection) return;
    const experimental = isExperimentalAuthorization(selectedConnection.authMethod);
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
    let active = true;
    setBrowserRisk(null);
    if (!selectedConnection || selectedConnection.authMethod !== "opencli_browser") {
      return () => {
        active = false;
      };
    }
    aiBrowserConnectionRisk(selectedConnection.id)
      .then((payload) => {
        if (active) setBrowserRisk(payload.risk);
      })
      .catch(() => {
        if (active) setBrowserRisk(null);
      });
    return () => {
      active = false;
    };
  }, [selectedConnection?.id, selectedConnection?.authMethod]);

  useEffect(() => {
    if (!authorization || ["guided_api_key", "paste_token"].includes(authorization.completionMode)) return;
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
    setDeepSeekExtensionStatus("");
    setChatGPTCallbackStatus("");
  }

  function chooseAuthMethod(method: AIConnectionAuthMethod) {
    if (!selectedProvider || !method.enabled || authorization) return;
    const next = connectionDraftForProvider(selectedProvider, method.code);
    if (method.code === "opencli_browser") {
      next.connectorId = browserConnectors.find((item) => item.online)?.id || browserConnectors[0]?.id || "";
      next.models = `${selectedProvider.code === "openai" ? "chatgpt" : selectedProvider.code}-web`;
    }
    setDraft(next);
    setError("");
    setDeepSeekExtensionStatus("");
    setChatGPTCallbackStatus("");
  }

  async function submitConnection(event: FormEvent) {
    event.preventDefault();
    if (!selectedProvider) return;
    if (usesBrowserConnector) {
      setWorking("create");
      setError("");
      setNotice("");
      try {
        const payload = await createAIBrowserConnection({
          connectorId: draft.connectorId,
          provider: selectedProvider.code,
          displayName: draft.displayName.trim(),
          models: splitModels(draft.models),
          termsAckVersion: "opencli-personal-browser-experimental-v1"
        });
        setItems((current) => [payload.connection, ...current]);
        setSelectedConnectionId(payload.connection.id);
        setSetupOpen(false);
        setDraft(emptyConnectionDraft);
        setNotice(`${payload.connection.displayName} 已通过本机浏览器识别账号，可继续创建个人中转。`);
      } catch (err) {
        setError(errorMessage(err));
      } finally {
        setWorking("");
      }
      return;
    }
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

  async function createBrowserConnector() {
    setWorking("connector-create");
    setError("");
    setNotice("");
    try {
      const result = await createAIBrowserConnector("我的 Chrome");
      setBrowserPairing(result);
      setBrowserConnectors((current) => [result.connector, ...current]);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setWorking("");
    }
  }

  async function refreshBrowserConnectors() {
    setWorking("connector-refresh");
    setError("");
    try {
      const result = await aiBrowserConnectors();
      setBrowserConnectors(result.items);
      setNotice(result.items.some((item) => item.online)
        ? "本地连接器在线，可以识别 ChatGPT、Gemini 或 DeepSeek 账号。"
        : "暂未检测到在线连接器，请确认本机程序正在运行。");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setWorking("");
    }
  }

  async function removeBrowserConnector(connectorID: string) {
    if (!globalThis.confirm("撤销后，关联的本地浏览器连接会停用。确认继续？")) return;
    setWorking(`connector-revoke:${connectorID}`);
    setError("");
    try {
      await revokeAIBrowserConnector(connectorID);
      setBrowserConnectors((current) => current.filter((item) => item.id !== connectorID));
      setBrowserPairing((current) => current?.connector.id === connectorID ? null : current);
      setNotice("本地连接器已撤销，设备令牌和关联任务已停用。");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setWorking("");
    }
  }

  async function copyPairCommand() {
    if (!browserPairing?.pairCommand) return;
    try {
      await navigator.clipboard.writeText(browserPairing.pairCommand);
      setNotice("配对命令已复制。请在本机终端运行，然后启动连接器。");
    } catch {
      setError("复制失败，请手动选择并复制配对命令。");
    }
  }

  async function beginAuthorization() {
    if (!selectedProvider || !selectedAuthMethod) return;
    const deepSeekWeb = draft.authMethod === "deepseek_web_token";
    const popup = deepSeekWeb
      ? null
      : window.open("", "tokhub-ai-authorization", "popup,width=760,height=820");
    setWorking("authorize");
    setError("");
    setNotice("");
    setDeepSeekExtensionStatus(deepSeekWeb ? "checking" : "");
    setChatGPTCallbackStatus("");
    try {
      const stepUp = deepSeekWeb ? null : await stepUpAIConnectionAuthorization(draft.password);
      const started = await startAIConnectionAuthorization({
        provider: selectedProvider.code,
        method: selectedAuthMethod.code,
        stepUpGrant: stepUp?.grant,
        displayName: draft.displayName.trim(),
        projectId: draft.projectId.trim() || undefined,
        models: splitModels(draft.models),
        termsAckVersion: authorizationTermsVersion(draft.authMethod),
        existingConnectionId: reauthorizeConnectionId || undefined
      });
      setAuthorization(started);
      setDraft((current) => ({ ...current, password: "" }));
      if (deepSeekWeb) {
        const extensionResult = await requestDeepSeekSessionFromExtension();
        setDeepSeekExtensionStatus(extensionResult.status);
        if (extensionResult.status === "ok" && extensionResult.token) {
          await completeDeepSeekWebTokenValue(started.id, extensionResult.token);
        }
      } else if (popup) {
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
    await completeChatGPTCallbackValue(authorization.id, draft.callbackUrl);
  }

  async function completeChatGPTCallbackFromExtension() {
    if (!authorization) return;
    setWorking("detect-callback");
    setError("");
    setNotice("");
    setChatGPTCallbackStatus("checking");
    const result = await requestChatGPTCallbackFromExtension(authorization.id);
    setChatGPTCallbackStatus(result.status);
    if (result.status !== "ok" || !result.callbackUrl) {
      setWorking("");
      return;
    }
    await completeChatGPTCallbackValue(authorization.id, result.callbackUrl);
  }

  async function completeChatGPTCallbackValue(authorizationID: string, callbackUrl: string) {
    setWorking("complete");
    setError("");
    try {
      const payload = await completeAIConnectionAuthorization(authorizationID, { callbackUrl });
      const connections = await aiConnections();
      setItems(connections.items);
      setSelectedConnectionId(payload.connection.id);
      setAuthorization(null);
      setSetupOpen(false);
      setReauthorizeConnectionId("");
      setDraft((current) => ({ ...current, callbackUrl: "" }));
      setChatGPTCallbackStatus("");
      setNotice("ChatGPT 授权、凭证加密保存和模型验证已完成。");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setWorking("");
    }
  }

  async function completeDeepSeekWebToken() {
    if (!authorization) return;
    await completeDeepSeekWebTokenValue(authorization.id, draft.deepSeekToken);
  }

  async function completeDeepSeekWebTokenValue(authorizationID: string, token: string) {
    setWorking("complete");
    setError("");
    setNotice("");
    try {
      const payload = await completeAIConnectionAuthorization(authorizationID, {
        deepSeekToken: token,
        termsAckVersion: "deepseek-web-session-experimental-v1"
      });
      const connections = await aiConnections();
      setItems(connections.items);
      setSelectedConnectionId(payload.connection.id);
      setAuthorization(null);
      setSetupOpen(false);
      setReauthorizeConnectionId("");
      setDraft((current) => ({ ...current, deepSeekToken: "" }));
      setDeepSeekExtensionStatus("");
      setNotice("DeepSeek 登录态验证通过，凭证已加密保存，个人连接已经可用。");
    } catch (err) {
      setDraft((current) => ({ ...current, deepSeekToken: "" }));
      setAuthorization(null);
      setDeepSeekExtensionStatus("");
      setError(`${errorMessage(err)} 本次识别已结束，请重新点击“一键读取当前登录态”。`);
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
      setDeepSeekExtensionStatus("");
      setChatGPTCallbackStatus("");
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
    setDeepSeekExtensionStatus("");
    setChatGPTCallbackStatus("");
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
    const validationMessage = selectedConnection.authMethod === "opencli_browser"
      ? "重新识别会通过本机连接器检查当前网页账号登录状态。请保持 Chrome 和连接器运行。确认继续？"
      : selectedConnection.authMethod === "deepseek_web_token"
        ? "重新验证会通过当前 DeepSeek 网页登录态发送最小生成请求，并占用消费者账号的使用额度。确认继续？"
        : "重新验证会为每个已配置模型发送最小生成请求，并可能产生少量官方费用。确认继续？";
    if (!globalThis.confirm(validationMessage)) return;
    setWorking("validate");
    setError("");
    setNotice("");
    try {
      const payload = await validateAIConnection(selectedConnection.id);
      replaceConnection(payload.connection);
      if (selectedConnection.authMethod === "opencli_browser") {
        const riskPayload = await aiBrowserConnectionRisk(selectedConnection.id);
        setBrowserRisk(riskPayload.risk);
      }
      setNotice(payload.validation.ok ? "重新验证通过，连接已恢复可用。" : payload.validation.message);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setWorking("");
    }
  }

  async function toggleBrowserPause(paused: boolean) {
    if (!selectedConnection || selectedConnection.authMethod !== "opencli_browser") return;
    const prompt = paused
      ? "暂停后，所有使用该网页登录账号的个人中转都会停止。确认暂停？"
      : "恢复后仍会执行账号级限流和登录身份核验。确认恢复？";
    if (!globalThis.confirm(prompt)) return;
    setWorking(paused ? "browser-pause" : "browser-resume");
    setError("");
    setNotice("");
    try {
      const payload = paused
        ? await pauseAIBrowserConnection(selectedConnection.id)
        : await resumeAIBrowserConnection(selectedConnection.id);
      setBrowserRisk(payload.risk);
      setItems((current) => current.map((item) => item.id === selectedConnection.id
        ? {
            ...item,
            status: paused ? "attention" : "active",
            authStatus: paused ? "attention" : "active",
            lastErrorMessage: paused ? "个人浏览器中转已由账号所有者暂停" : ""
          }
        : item));
      setNotice(paused ? "个人浏览器中转已暂停。" : "个人浏览器中转已恢复，后续请求仍受账号保护策略约束。");
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
      const safeDraft = isExperimentalAuthorization(selectedConnection.authMethod)
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
            <p>连接固定保存在个人空间。官方 API Key 与 OAuth 由 TokHub 加密保护；服务商密码、验证码、完整 Cookie、cf_clearance 与其他浏览器数据均不采集。本地浏览器连接只保存设备引用，Session 和网页 Token 始终留在用户电脑。</p>
          </div>
          <span className="ai-safety-meta">AES-256-GCM · 单次授权 · 个人隔离</span>
        </section>

        {browserConnectorEnabled ? (
          <section className="ai-browser-connector-panel" aria-labelledby="browser-connector-title">
            <div className="ai-browser-connector-head">
              <div>
                <span className="ai-connect-eyebrow">LOCAL BROWSER CONNECTOR · EXPERIMENTAL</span>
                <h2 id="browser-connector-title">连接本机已登录的 AI 网页</h2>
                <p>适用于 ChatGPT、Gemini 和 DeepSeek。TokHub 发送受限文本任务到本机，OpenCLI 在已连接的 Chrome Profile 中完成操作。</p>
              </div>
              <div className="ai-browser-connector-actions">
                <button className="btn btn-ghost btn-sm" type="button" disabled={!!working} onClick={() => void refreshBrowserConnectors()}>
                  {working === "connector-refresh" ? "检测中…" : "检测状态"}
                </button>
                <button className="btn btn-primary btn-sm" type="button" disabled={!!working} onClick={() => void createBrowserConnector()}>
                  {working === "connector-create" ? "创建中…" : "＋ 添加本机连接器"}
                </button>
              </div>
            </div>
            {browserPairing ? (
              <div className="ai-browser-pairing">
                <div>
                  <b>一次性配对命令</b>
                  <p>先安装 OpenCLI 1.8.6 或更高版本并连接 Chrome 扩展，再在本机终端运行此命令。配对码 10 分钟后失效。</p>
                  <p>
                    <a href="https://github.com/jackwener/OpenCLI" target="_blank" rel="noreferrer">安装 OpenCLI ↗</a>
                    {" · "}
                    <a href="https://github.com/yaojingang/TokHub#opencli-本机浏览器连接" target="_blank" rel="noreferrer">查看连接器使用说明 ↗</a>
                  </p>
                </div>
                <code>{browserPairing.pairCommand}</code>
                <button className="btn btn-ghost btn-sm" type="button" onClick={() => void copyPairCommand()}>复制命令</button>
              </div>
            ) : null}
            <div className="ai-browser-connector-list">
              {browserConnectors.map((connector) => (
                <div className="ai-browser-connector-item" key={connector.id}>
                  <span className={`ai-browser-status ${connector.online ? "online" : ""}`} />
                  <div>
                    <b>{connector.displayName}</b>
                    <small>
                      {connector.online ? "在线" : connector.status === "pending" ? "等待配对" : "离线"}
                      {connector.opencliVersion ? ` · OpenCLI ${connector.opencliVersion}` : ""}
                      {connector.capabilities.length ? ` · ${connector.capabilities.map(browserProviderLabel).join(" / ")}` : ""}
                    </small>
                  </div>
                  <button
                    className="btn btn-ghost btn-sm"
                    type="button"
                    disabled={working === `connector-revoke:${connector.id}`}
                    onClick={() => void removeBrowserConnector(connector.id)}
                  >
                    {working === `connector-revoke:${connector.id}` ? "撤销中…" : "撤销"}
                  </button>
                </div>
              ))}
              {!browserConnectors.length ? (
                <div className="ai-browser-connector-empty">尚未添加本机连接器。完成一次配对后，可在三家服务的连接方式中选择“连接本机已登录网页”。</div>
              ) : null}
            </div>
            <p className="ai-browser-risk">个人实验能力 · 默认单中转、单并发、每秒 1 次 · 暂不支持流式、工具调用、图片和团队共享 · 遇到验证码会立即停止</p>
          </section>
        ) : null}

        {error && !setupOpen ? <div className="form-error ai-live-message" role="alert">{error}</div> : null}
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
              const oauthEnabled = enabledMethods.some((method) => ["oauth", "codex_oauth", "deepseek_web_token", "opencli_browser"].includes(method.code));
              const guidedEnabled = enabledMethods.some((method) => method.code === "api_key_guided");
              const unavailableInteractive = unavailableInteractiveAuthMethods(provider);
              const methodLabels = enabledMethods.map((method) => method.label);
              for (const method of unavailableInteractive) {
                const label = unavailableAuthMethodSummary(method);
                if (!methodLabels.includes(label)) methodLabels.push(label);
              }
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
                  <span className="ai-provider-action">{oauthEnabled ? "登录 / 密钥" : guidedEnabled ? "官网引导" : unavailableInteractive.length > 0 ? "授权待配置" : "密钥连接"}</span>
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
                setDeepSeekExtensionStatus("");
                setChatGPTCallbackStatus("");
              }}>关闭</button>
            </div>

            <div className="ai-auth-methods" role="radiogroup" aria-label="连接方式">
              {selectedProvider.authMethods.map((method) => (
                <button
                  className={`ai-auth-method ${method.enabled && draft.authMethod === method.code ? "selected" : ""} ${method.enabled ? "" : "unavailable"}`}
                  type="button"
                  role="radio"
                  aria-checked={method.enabled && draft.authMethod === method.code}
                  aria-disabled={!method.enabled}
                  disabled={!method.enabled || !!authorization || (!!reauthorizeConnectionId && draft.authMethod !== method.code)}
                  key={method.code}
                  onClick={() => chooseAuthMethod(method)}
                >
                  <span>
                    <b>{method.label}</b>
                    <i>{method.enabled ? releaseLabel(method.release) : unavailableReleaseLabel(method)}</i>
                  </span>
                  <small>{method.enabled ? method.description : `${method.description} ${method.unavailableReason || "当前暂不可用。"}`}</small>
                </button>
              ))}
            </div>
            {selectedGeminiOAuthMethod && !selectedGeminiOAuthMethod.enabled ? (
              <section className="ai-auth-readiness" aria-label="Gemini OAuth 配置状态">
                <div>
                  <b>Gemini Google OAuth 等待部署配置</b>
                  <p>{selectedGeminiOAuthMethod.unavailableReason || "当前部署尚未完成 Google OAuth 配置。"}</p>
                  <small>管理员需要配置 Google Web OAuth Client、精确回调地址和可用的 Cloud Project。配置完成并重启服务后，此入口会自动开放。</small>
                </div>
                <a href={selectedGeminiOAuthMethod.docsUrl} target="_blank" rel="noreferrer">查看 Google OAuth 说明 ↗</a>
              </section>
            ) : null}

            <form className="ai-setup-form" onSubmit={submitConnection}>
              <label>
                <span>连接名称</span>
                <input className="input" required maxLength={80} value={draft.displayName} onChange={(event) => setDraft({ ...draft, displayName: event.target.value })} />
              </label>
              <label>
                <span>地域 / API 产品区</span>
                <select className="input" value={draft.region} disabled={usesInteractiveAuthorization || usesBrowserConnector} onChange={(event) => setDraft({ ...draft, region: event.target.value, workspaceId: "" })}>
                  {selectedProvider.regions.map((region) => <option value={region.code} key={region.code}>{region.name}</option>)}
                </select>
              </label>
              {selectedProvider.regions.find((region) => region.code === draft.region)?.workspaceId && !usesInteractiveAuthorization ? (
                <label>
                  <span>Workspace ID <em>选填，用于专属接入点</em></span>
                  <input className="input" value={draft.workspaceId} onChange={(event) => setDraft({ ...draft, workspaceId: event.target.value })} placeholder="例如 workspace-id" />
                </label>
              ) : null}
              {usesBrowserConnector ? (
                <>
                  <section className="ai-browser-login-guide ai-form-wide" aria-label={`${selectedProvider.name} 网页登录引导`}>
                    <div>
                      <b>先登录，再识别当前账号</b>
                      <p>第 1 步会在新标签页打开 {selectedProvider.name}。请确认它属于 OpenCLI 已连接或已选定的 Chrome Profile；登录完成后返回这里执行第 2 步。</p>
                    </div>
                    <a
                      className="btn btn-ghost"
                      href={browserProviderLoginURL(selectedProvider.code)}
                      target="_blank"
                      rel="noreferrer"
                    >
                      1. 打开 {selectedProvider.name} 登录 ↗
                    </a>
                  </section>
                  <label>
                    <span>本机连接器</span>
                    <select
                      className="input"
                      required
                      value={draft.connectorId}
                      onChange={(event) => setDraft({ ...draft, connectorId: event.target.value })}
                    >
                      <option value="">请选择在线连接器</option>
                      {browserConnectors.map((connector) => (
                        <option value={connector.id} disabled={!connector.online} key={connector.id}>
                          {connector.displayName} · {connector.online ? "在线" : connector.status === "pending" ? "等待配对" : "离线"}
                        </option>
                      ))}
                    </select>
                    <small>识别期间请保持本机程序、Chrome 和对应 AI 网页运行。</small>
                  </label>
                </>
              ) : null}
              {draft.authMethod === "oauth" ? (
                <label>
                  <span>Google Cloud Project ID</span>
                  <input
                    className="input"
                    required
                    minLength={6}
                    maxLength={30}
                    pattern="[a-z][a-z0-9-]{4,28}[a-z0-9]"
                    title="请输入 6–30 位 Google Cloud Project ID：小写字母开头，只含小写字母、数字或连字符，并以字母或数字结尾"
                    value={draft.projectId}
                    onChange={(event) => setDraft({ ...draft, projectId: event.target.value.trim().toLowerCase() })}
                    placeholder="例如 my-gemini-project"
                  />
                  <small>该项目用于 Gemini API 配额与计费。Google 账号需要拥有该项目的 Service Usage Consumer 权限，并已启用 Gemini API。</small>
                </label>
              ) : null}
              <label className="ai-form-wide">
                <span>模型 ID <em>每行或逗号分隔，最多 16 个</em></span>
                <textarea className="input ai-model-input" required value={draft.models} onChange={(event) => setDraft({ ...draft, models: event.target.value })} />
                {usesBrowserConnector ? <small>本机模式把这里作为 API 路由别名，实际网页模型由 OpenCLI 已连接的 Chrome Profile 和适配器决定。</small> : null}
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

              {(usesInteractiveAuthorization || (usesGuidedAPIKey && !authorization)) && draft.authMethod !== "deepseek_web_token" ? (
                <label className="ai-form-wide">
                  <span>TokHub 登录密码 <em>用于本次敏感操作二次验证</em></span>
                  <input className="input" type="password" autoComplete="current-password" required value={draft.password} onChange={(event) => setDraft({ ...draft, password: event.target.value })} />
                  <small>密码只提交给 TokHub，不会发送给 AI 服务商。</small>
                </label>
              ) : null}

              {(usesInteractiveAuthorization || (usesGuidedAPIKey && !authorization)) ? (
                <>
                  {isExperimentalAuthorization(draft.authMethod) ? (
                    <label className="ai-experimental-confirm ai-form-wide">
                      <input type="checkbox" required checked={draft.confirmExperimental} onChange={(event) => setDraft({ ...draft, confirmExperimental: event.target.checked })} />
                      <span>
                        {draft.authMethod === "deepseek_web_token"
                          ? "我了解该能力依赖 DeepSeek 网页私有协议，服务商变更可能造成中断或登录态失效。连接仅限本人使用，系统会执行单中转、单并发和每秒 1 次请求限制。"
                          : "我了解该能力依赖 ChatGPT Codex 的消费者授权和私有接口，服务商变更可能造成中断。连接仅限本人使用，系统会执行严格限流和重新授权保护。"}
                      </span>
                    </label>
                  ) : null}
                  {selectedAuthMethod?.riskNotice ? <p className="ai-risk-notice ai-form-wide">{selectedAuthMethod.riskNotice}</p> : null}
                </>
              ) : null}

              {usesBrowserConnector ? (
                <>
                  <label className="ai-experimental-confirm ai-form-wide">
                    <input type="checkbox" required checked={draft.confirmExperimental} onChange={(event) => setDraft({ ...draft, confirmExperimental: event.target.checked })} />
                    <span>我了解该能力通过本机 Chrome 自动执行网页任务，仅限本人低频使用。网页结构、平台规则或登录状态变化可能造成中断；系统执行单中转、单并发和每秒 1 次请求限制。</span>
                  </label>
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
                  {draft.authMethod === "deepseek_web_token" ? (
                    <>
                      {deepSeekExtensionStatus ? (
                        <div className="ai-deepseek-extension-status" role="status">
                          <span>{deepSeekExtensionStatusMessage(deepSeekExtensionStatus)}</span>
                          {deepSeekExtensionStatus === "extension_unavailable" ? (
                            <a href={aiLoginExtensionDownloadURL} download>下载 TokHub AI 登录助手</a>
                          ) : null}
                        </div>
                      ) : null}
                      <DeepSeekTokenGuide
                        token={draft.deepSeekToken}
                        working={working === "complete"}
                        onTokenChange={(deepSeekToken) => setDraft((current) => ({ ...current, deepSeekToken }))}
                        onComplete={() => void completeDeepSeekWebToken()}
                      />
                    </>
                  ) : null}
                  {draft.authMethod === "codex_oauth" ? (
                    <div className="ai-chatgpt-callback-helper">
                      <div>
                        <b>登录完成后，一键识别授权结果</b>
                        <p>OpenAI 会把授权结果带到 localhost 回调页。页面显示无法访问时直接返回 TokHub，登录助手会读取本次回调并立即提交验证。</p>
                        <small>登录助手只读取端口 1455 的单次 OAuth code 与 state，不访问 ChatGPT Cookie、网页 Token 或密码。</small>
                      </div>
                      <button
                        className="btn btn-primary btn-sm"
                        type="button"
                        disabled={working === "complete" || working === "detect-callback"}
                        onClick={() => void completeChatGPTCallbackFromExtension()}
                      >
                        {working === "detect-callback" ? "正在识别…" : "2. 一键识别授权结果"}
                      </button>
                      {chatGPTCallbackStatus ? (
                        <div className="ai-login-helper-status" role="status">
                          <span>{chatGPTCallbackStatusMessage(chatGPTCallbackStatus)}</span>
                          {chatGPTCallbackStatus === "extension_unavailable" ? (
                            <a href={aiLoginExtensionDownloadURL} download>下载 TokHub AI 登录助手</a>
                          ) : null}
                        </div>
                      ) : null}
                    </div>
                  ) : null}
                </section>
              ) : null}

              {error ? <div className="form-error ai-setup-error ai-form-wide" role="alert">{error}</div> : null}

              {!authorization && draft.authMethod === "codex_oauth" ? (
                <section className="ai-deepseek-entry ai-form-wide" aria-label="ChatGPT 登录准备">
                  <div className="ai-deepseek-entry-copy">
                    <b>首次使用请先安装登录助手</b>
                    <p>下载 ZIP 并解压，在 Chrome 扩展程序页开启开发者模式，选择“加载已解压的扩展程序”，随后刷新 TokHub。</p>
                    <small>扩展只识别本次 OAuth 回调地址。未安装时仍可通过手动粘贴 localhost 地址完成授权。</small>
                  </div>
                  <div className="ai-deepseek-entry-actions">
                    <a className="btn btn-ghost" href={aiLoginExtensionDownloadURL} download>安装 TokHub AI 登录助手</a>
                  </div>
                </section>
              ) : null}

              {!authorization && draft.authMethod === "deepseek_web_token" ? (
                <section className="ai-deepseek-entry ai-form-wide" aria-label="DeepSeek 网页登录步骤">
                  <div className="ai-deepseek-entry-copy">
                    <b>登录后，识别 OpenCLI 已连接的 Chrome 账号</b>
                    <p>先下载 ZIP 并解压，在 Chrome 扩展程序页开启开发者模式并加载文件夹；刷新 TokHub 后打开 DeepSeek 完成登录。</p>
                    <small>点击读取时，扩展只获取当前账号的 userToken.value，不读取 Cookie、密码或其他 Local Storage，也不会持久化 Token。</small>
                  </div>
                  <div className="ai-deepseek-entry-actions">
                    <a className="btn btn-ghost" href={deepSeekWebLoginURL} target="_blank" rel="noreferrer">1. 打开 DeepSeek 登录</a>
                    <a className="btn btn-ghost" href={aiLoginExtensionDownloadURL} download>下载 TokHub AI 登录助手</a>
                    <button className="btn btn-primary" disabled={!!working} type="submit">
                      {working === "authorize" || working === "complete" ? "正在读取并验证…" : "2. 一键读取当前登录态"}
                    </button>
                  </div>
                </section>
              ) : (
                <div className="ai-setup-footer ai-form-wide">
                  <a href={selectedAuthMethod?.docsUrl || selectedProvider.docsUrl} target="_blank" rel="noreferrer">查看官方说明 ↗</a>
                  {!authorization || (usesGuidedAPIKey && authorization) ? (
                    <button className="btn btn-primary" disabled={!!working} type="submit">
                      {submitLabel(draft.authMethod, !!authorization, working)}
                    </button>
                  ) : null}
                </div>
              )}
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
                    <p>{connectionEndpointLabel(selectedConnection)}</p>
                  </div>
                  <ConnectionStatus status={selectedConnection.status} authStatus={selectedConnection.authStatus} />
                </div>

                <div className="ai-detail-metrics">
                  <Metric label={selectedConnection.authMethod === "api_key" || selectedConnection.authMethod === "api_key_guided" ? "凭证" : "授权账号"} value={selectedConnection.accountMask || selectedConnection.secretMask} />
                  <Metric label="使用范围" value={selectedConnection.sharingScope === "personal" ? "仅本人" : selectedConnection.sharingScope} />
                  <Metric label="模型" value={`${selectedConnection.models.length} 个`} />
                  <Metric label="最后验证" value={formatDate(selectedConnection.lastValidatedAt)} />
                </div>

                {selectedConnection.authMethod === "opencli_browser" && browserRisk ? (
                  <section className={`ai-browser-risk-card state-${browserRisk.state}`} aria-label="个人浏览器账号保护状态">
                    <div className="ai-browser-risk-card-head">
                      <div>
                        <span>ACCOUNT SAFETY GOVERNOR</span>
                        <b>{browserRiskStateLabel(browserRisk.state)}</b>
                        <p>{browserRiskStateDescription(browserRisk)}</p>
                      </div>
                      <div className="ai-browser-risk-actions">
                        <button className="btn btn-ghost btn-sm" type="button" disabled={!!working} onClick={() => void runValidation()}>
                          重新识别账号
                        </button>
                        {["normal", "paused"].includes(browserRisk.state) ? (
                          <button
                            className={`btn btn-sm ${browserRisk.state === "paused" ? "btn-primary" : "danger-lite"}`}
                            type="button"
                            disabled={!!working}
                            onClick={() => void toggleBrowserPause(browserRisk.state !== "paused")}
                          >
                            {browserRisk.state === "paused" ? "恢复中转" : "立即暂停"}
                          </button>
                        ) : null}
                      </div>
                    </div>
                    <div className="ai-browser-risk-stats">
                      <Metric label="本小时" value={`${browserRisk.requestsHour} / ${browserRisk.hourlyLimit}`} />
                      <Metric label="近 24 小时" value={`${browserRisk.requestsDay} / ${browserRisk.dailyLimit}`} />
                      <Metric label="最小间隔" value={`${browserRisk.minimumIntervalSeconds} 秒`} />
                      <Metric label="连续失败" value={`${browserRisk.consecutiveFailures} 次`} />
                    </div>
                    <small>
                      {browserRisk.cooldownUntil ? `预计恢复：${formatDate(browserRisk.cooldownUntil)} · ` : ""}
                      最近成功：{formatDate(browserRisk.lastSuccessAt)}
                      {browserRisk.lastChallengeAt ? ` · 最近安全验证：${formatDate(browserRisk.lastChallengeAt)}` : ""}
                    </small>
                  </section>
                ) : null}

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
                  {selectedConnection.authMethod === "opencli_browser" ? null : isManagedAuthorization(selectedConnection.authMethod) ? (
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
                      <span className="ai-step-badge">{isExperimentalAuthorization(selectedConnection.authMethod) ? "实验限流" : "约 10 秒"}</span>
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
                        <input className="input" type="number" min={1} max={isExperimentalAuthorization(selectedConnection.authMethod) ? 1 : 1000} disabled={isExperimentalAuthorization(selectedConnection.authMethod)} value={relayDraft.qpsLimit} onChange={(event) => setRelayDraft({ ...relayDraft, qpsLimit: Number(event.target.value) })} />
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
  return isManagedAuthorization(authMethod);
}

function DeepSeekTokenGuide({
  token,
  working,
  onTokenChange,
  onComplete
}: {
  token: string;
  working: boolean;
  onTokenChange: (value: string) => void;
  onComplete: () => void;
}) {
  const [copied, setCopied] = useState(false);
  const command = 'copy(JSON.parse(localStorage.getItem("userToken")).value)';

  async function copyCommand() {
    await navigator.clipboard.writeText(command);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1800);
  }

  return (
    <div className="ai-deepseek-token-guide">
      <ol>
        <li>
          <span>1</span>
          <div>
            <b>登录 DeepSeek 网页版</b>
            <p>在刚打开的 DeepSeek 页面完成登录，并确认可以正常发起对话。</p>
          </div>
        </li>
        <li>
          <span>2</span>
          <div>
            <b>复制当前账号的 userToken</b>
            <p>打开开发者工具，进入 Application → Local Storage → https://chat.deepseek.com，找到 userToken，复制其中的 value。</p>
            <div className="ai-token-command">
              <code>{command}</code>
              <button type="button" onClick={() => void copyCommand()}>{copied ? "已复制" : "复制快捷命令"}</button>
            </div>
            <small>也可以在 DeepSeek 页面的 Console 中运行上面的快捷命令。它只读取 userToken 的 value。</small>
          </div>
        </li>
        <li>
          <span>3</span>
          <div>
            <b>粘贴并验证登录态</b>
            <p>TokHub 会通过受管桥发送最小生成请求。验证成功后加密保存，页面不会再次显示完整 Token。</p>
          </div>
        </li>
      </ol>
      <div className="ai-token-complete">
        <label>
          <span>DeepSeek userToken value</span>
          <input
            className="input ai-secret-input"
            type="password"
            autoComplete="new-password"
            spellCheck={false}
            required
            value={token}
            onChange={(event) => onTokenChange(event.target.value)}
            placeholder="粘贴 userToken 的 value"
          />
          <small>请勿粘贴账号密码、验证码、Cookie 字符串或 cf_clearance。</small>
        </label>
        <button className="btn btn-primary btn-sm" type="button" disabled={working || token.trim().length < 32} onClick={onComplete}>
          {working ? "正在识别并验证…" : "识别登录态并连接"}
        </button>
      </div>
    </div>
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

function preferredAuthMethod(provider: AIConnectionProvider) {
  for (const code of ["oauth", "codex_oauth", "deepseek_web_token", "api_key_guided"]) {
    const method = provider.authMethods.find((item) => item.enabled && item.code === code);
    if (method) return method;
  }
  return provider.authMethods.find((method) => method.enabled)
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

function unavailableAuthMethodSummary(method: AIConnectionAuthMethod) {
  return method.release === "unavailable" ? "消费者登录未开放" : "登录能力待配置";
}

function unavailableReleaseLabel(method: AIConnectionAuthMethod) {
  return method.release === "unavailable" ? "官方未开放" : "待配置";
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
    case "deepseek_web_token": return "DeepSeek 网页账号";
    case "opencli_browser": return "本机浏览器";
    case "api_key_guided": return "开放平台密钥";
    default: return "官方 API Key";
  }
}

function releaseLabel(release: string) {
  switch (release) {
    case "experimental": return "实验";
    case "preview": return "预览";
    case "unavailable": return "官方未开放";
    default: return "稳定";
  }
}

function authorizationTitle(method: string) {
  if (method === "api_key_guided") return "请在 DeepSeek 开放平台创建 API Key";
  if (method === "codex_oauth") return "请在新窗口完成 ChatGPT 登录";
  if (method === "deepseek_web_token") return "识别 DeepSeek 当前登录态";
  return "正在等待 Google 授权结果";
}

function authorizationInstructions(method: string) {
  if (method === "api_key_guided") return "创建密钥后返回此页粘贴。TokHub 不接触 DeepSeek 网页登录态。";
  if (method === "codex_oauth") return "完成登录后返回本页，一键识别 localhost 授权结果；也可以在下方手动粘贴回调地址。";
  if (method === "deepseek_web_token") return "扩展读取失败时，可按下方三步手动导入 userToken.value。";
  return "授权窗口完成后会自动关闭，本页将继续验证账号和模型。";
}

function submitLabel(method: string, hasAuthorization: boolean, working: string) {
  if (working === "authorize") return "正在发起授权…";
  if (working === "create") return "正在连接并验证…";
  if (method === "api_key_guided" && !hasAuthorization) return "前往开放平台";
  if (method === "deepseek_web_token") return "一键读取当前登录态";
  if (method === "opencli_browser") return "2. 已登录，识别并连接";
  if (method === "oauth" || method === "codex_oauth") return "打开登录授权";
  return "连接并验证";
}

function deepSeekExtensionStatusMessage(status: DeepSeekExtensionStatus | "checking"): string {
  switch (status) {
    case "checking":
      return "正在请求 Chrome 扩展读取当前 DeepSeek 登录态…";
    case "extension_unavailable":
      return "未检测到 TokHub AI 登录助手。安装扩展后可一键读取，也可以继续使用下方手动导入。";
    case "deepseek_not_open":
      return "没有找到已打开的 DeepSeek 网页。请先打开 DeepSeek、完成登录，再重新点击一键读取。";
    case "not_logged_in":
      return "已找到 DeepSeek 网页，但没有读取到可用登录态。请确认网页可以正常对话后重试。";
    case "permission_denied":
      return "Chrome 没有授予读取 DeepSeek 网页的权限。请在扩展管理页允许访问 chat.deepseek.com。";
    case "read_failed":
      return "扩展读取登录态失败。请刷新 DeepSeek 网页后重试，或使用下方手动导入。";
    default:
      return "";
  }
}

function chatGPTCallbackStatusMessage(status: ChatGPTCallbackStatus | "checking"): string {
  switch (status) {
    case "checking":
      return "正在请求 TokHub AI 登录助手识别 ChatGPT 授权结果…";
    case "ok":
      return "已识别本次 ChatGPT 授权结果，正在由 TokHub 验证账号和模型。";
    case "extension_unavailable":
      return "未检测到 TokHub AI 登录助手。安装并刷新此页后可一键识别，也可以继续手动粘贴回调地址。";
    case "callback_not_found":
      return "没有找到本次 ChatGPT 的 localhost 回调页。请先完成登录并保留最终页面，再重新识别。";
    case "permission_denied":
      return "Chrome 没有授予 localhost 回调读取权限。请重新加载扩展并允许 localhost 访问。";
    case "read_failed":
      return "授权结果读取失败。请重新打开授权窗口，或使用下方手动回调方式。";
    default:
      return "";
  }
}

function authorizationTermsVersion(method: string): string | undefined {
  if (method === "codex_oauth") return "chatgpt-codex-experimental-v1";
  if (method === "deepseek_web_token") return "deepseek-web-session-experimental-v1";
  if (method === "opencli_browser") return "opencli-personal-browser-experimental-v1";
  return undefined;
}

function isManagedAuthorization(method: string): boolean {
  return ["oauth", "codex_oauth", "deepseek_web_token", "opencli_browser"].includes(method);
}

function isExperimentalAuthorization(method: string): boolean {
  return method === "codex_oauth" || method === "deepseek_web_token" || method === "opencli_browser";
}

function connectionEndpointLabel(connection: AIConnection): string {
  if (connection.authMethod === "deepseek_web_token") return "DeepSeek 网页版 · TokHub 受管协议桥";
  if (connection.authMethod === "opencli_browser") return "本机 Chrome · OpenCLI 受限任务连接";
  return connection.endpoint;
}

function browserProviderLabel(provider: string): string {
  switch (provider) {
    case "openai": return "ChatGPT";
    case "gemini": return "Gemini";
    case "deepseek": return "DeepSeek";
    default: return provider;
  }
}

function browserProviderLoginURL(provider: string): string {
  switch (provider) {
    case "openai": return "https://auth.openai.com/log-in";
    case "gemini": return "https://accounts.google.com/ServiceLogin?continue=https%3A%2F%2Fgemini.google.com%2F";
    case "deepseek": return "https://chat.deepseek.com/sign_in";
    default: return "#";
  }
}

function browserRiskStateLabel(state: string): string {
  switch (state) {
    case "normal": return "账号保护正常";
    case "cooldown": return "账号正在冷却";
    case "reauth_required": return "需要重新识别";
    case "security_locked": return "安全验证锁定";
    case "adapter_blocked": return "适配器暂停";
    case "paused": return "已手动暂停";
    default: return "状态待确认";
  }
}

function browserRiskStateDescription(risk: AIBrowserRiskState): string {
  switch (risk.state) {
    case "normal":
      return "请求会经过账号身份核验、固定间隔、小时额度和每日额度保护。";
    case "cooldown":
      return "近期调用出现异常或服务商限流，系统正在等待安全恢复窗口。";
    case "reauth_required":
      return "浏览器登录已失效或账号发生变化，请重新登录并识别当前账号。";
    case "security_locked":
      return risk.cooldownUntil
        ? `服务商拒绝访问，账号保护会持续到 ${formatDate(risk.cooldownUntil)}，届时请重新识别。`
        : "检测到验证码或安全验证，完成网页处理后再执行重新识别。";
    case "adapter_blocked":
      return "OpenCLI 与当前网页结构不兼容，请更新 OpenCLI 后重新识别。";
    case "paused":
      return "账号所有者已暂停该账号的全部个人网页中转。";
    default:
      return "当前账号保护状态需要重新检测。";
  }
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
  deepSeekToken: "",
  confirmBillable: false,
  confirmExperimental: false,
  connectorId: ""
};

const emptyRelayDraft: RelayDraft = {
  name: "",
  policy: "latency",
  qpsLimit: 20,
  quotaMonth: 100000,
  modelIds: []
};
