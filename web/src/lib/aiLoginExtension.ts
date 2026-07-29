export const deepSeekExtensionRequestType = "TOKHUB_DEEPSEEK_SESSION_REQUEST";
export const deepSeekExtensionResponseType = "TOKHUB_DEEPSEEK_SESSION_RESPONSE";
export const chatGPTCallbackRequestType = "TOKHUB_CHATGPT_CALLBACK_REQUEST";
export const chatGPTCallbackResponseType = "TOKHUB_CHATGPT_CALLBACK_RESPONSE";

export type DeepSeekExtensionStatus =
  | "ok"
  | "extension_unavailable"
  | "deepseek_not_open"
  | "not_logged_in"
  | "permission_denied"
  | "read_failed";

export type DeepSeekExtensionResult = {
  status: DeepSeekExtensionStatus;
  token?: string;
};

export type ChatGPTCallbackStatus =
  | "ok"
  | "extension_unavailable"
  | "callback_not_found"
  | "permission_denied"
  | "read_failed";

export type ChatGPTCallbackResult = {
  status: ChatGPTCallbackStatus;
  callbackUrl?: string;
};

type ExtensionResponse = {
  source?: string;
  type?: string;
  version?: number;
  requestId?: string;
  status?: string;
  token?: string;
  callbackUrl?: string;
};

export function requestDeepSeekSessionFromExtension(timeoutMs = 1800): Promise<DeepSeekExtensionResult> {
  const requestId = newExtensionRequestID("ds");
  return requestExtension<DeepSeekExtensionResult>({
    requestId,
    requestType: deepSeekExtensionRequestType,
    responseType: deepSeekExtensionResponseType,
    timeoutMs,
    unavailable: { status: "extension_unavailable" },
    parse(response) {
      if (response.status === "ok") {
        const token = response.token?.trim() || "";
        return token.length >= 32 && token.length <= 8192
          ? { status: "ok", token }
          : { status: "read_failed" };
      }
      if (isDeepSeekExtensionFailureStatus(response.status)) {
        return { status: response.status };
      }
      return { status: "read_failed" };
    }
  });
}

export function requestChatGPTCallbackFromExtension(
  authorizationId: string,
  timeoutMs = 1800
): Promise<ChatGPTCallbackResult> {
  const requestId = newExtensionRequestID("cg");
  return requestExtension<ChatGPTCallbackResult>({
    requestId,
    requestType: chatGPTCallbackRequestType,
    responseType: chatGPTCallbackResponseType,
    requestPayload: { authorizationId },
    timeoutMs,
    unavailable: { status: "extension_unavailable" },
    parse(response) {
      if (response.status === "ok") {
        const callbackUrl = normalizeChatGPTCallbackURL(response.callbackUrl);
        return callbackUrl
          ? { status: "ok", callbackUrl }
          : { status: "read_failed" };
      }
      if (isChatGPTCallbackFailureStatus(response.status)) {
        return { status: response.status };
      }
      return { status: "read_failed" };
    }
  });
}

function requestExtension<T>(input: {
  requestId: string;
  requestType: string;
  responseType: string;
  requestPayload?: Record<string, string>;
  timeoutMs: number;
  unavailable: T;
  parse(response: ExtensionResponse): T;
}): Promise<T> {
  if (typeof window === "undefined") {
    return Promise.resolve(input.unavailable);
  }
  return new Promise((resolve) => {
    let settled = false;
    const finish = (result: T) => {
      if (settled) return;
      settled = true;
      window.clearTimeout(timer);
      window.removeEventListener("message", receive);
      resolve(result);
    };
    const receive = (event: MessageEvent<ExtensionResponse>) => {
      if (event.source !== window || event.origin !== window.location.origin) return;
      const response = event.data;
      if (
        response?.source !== "tokhub-extension" ||
        response.type !== input.responseType ||
        response.version !== 1 ||
        response.requestId !== input.requestId
      ) {
        return;
      }
      finish(input.parse(response));
    };
    const timer = window.setTimeout(() => finish(input.unavailable), input.timeoutMs);
    window.addEventListener("message", receive);
    window.postMessage({
      source: "tokhub-web",
      type: input.requestType,
      version: 1,
      requestId: input.requestId,
      ...input.requestPayload
    }, window.location.origin);
  });
}

function normalizeChatGPTCallbackURL(value?: string): string {
  const raw = value?.trim() || "";
  if (!raw || raw.length > 8192) return "";
  try {
    const callback = new URL(raw);
    if (
      callback.protocol !== "http:" ||
      callback.hostname !== "localhost" ||
      callback.port !== "1455" ||
      callback.pathname !== "/auth/callback" ||
      callback.username ||
      callback.password ||
      callback.hash ||
      !callback.searchParams.get("code") ||
      !callback.searchParams.get("state")
    ) {
      return "";
    }
    return callback.toString();
  } catch {
    return "";
  }
}

function isDeepSeekExtensionFailureStatus(value?: string): value is Exclude<DeepSeekExtensionStatus, "ok" | "extension_unavailable"> {
  return ["deepseek_not_open", "not_logged_in", "permission_denied", "read_failed"].includes(value || "");
}

function isChatGPTCallbackFailureStatus(value?: string): value is Exclude<ChatGPTCallbackStatus, "ok" | "extension_unavailable"> {
  return ["callback_not_found", "permission_denied", "read_failed"].includes(value || "");
}

function newExtensionRequestID(prefix: "ds" | "cg"): string {
  if (typeof globalThis.crypto?.randomUUID === "function") {
    return `${prefix}_${globalThis.crypto.randomUUID()}`;
  }
  const bytes = new Uint8Array(16);
  if (typeof globalThis.crypto?.getRandomValues === "function") {
    globalThis.crypto.getRandomValues(bytes);
    return `${prefix}_${Array.from(bytes, (value) => value.toString(16).padStart(2, "0")).join("")}`;
  }
  return `${prefix}_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 18)}`;
}
