export const deepSeekExtensionRequestType = "TOKHUB_DEEPSEEK_SESSION_REQUEST";
export const deepSeekExtensionResponseType = "TOKHUB_DEEPSEEK_SESSION_RESPONSE";

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

type DeepSeekExtensionResponse = {
  source?: string;
  type?: string;
  version?: number;
  requestId?: string;
  status?: string;
  token?: string;
};

export function requestDeepSeekSessionFromExtension(timeoutMs = 1800): Promise<DeepSeekExtensionResult> {
  if (typeof window === "undefined") {
    return Promise.resolve({ status: "extension_unavailable" });
  }
  const requestId = newDeepSeekExtensionRequestID();
  return new Promise((resolve) => {
    let settled = false;
    const finish = (result: DeepSeekExtensionResult) => {
      if (settled) return;
      settled = true;
      window.clearTimeout(timer);
      window.removeEventListener("message", receive);
      resolve(result);
    };
    const receive = (event: MessageEvent<DeepSeekExtensionResponse>) => {
      if (event.source !== window || event.origin !== window.location.origin) return;
      const response = event.data;
      if (
        response?.source !== "tokhub-extension" ||
        response.type !== deepSeekExtensionResponseType ||
        response.version !== 1 ||
        response.requestId !== requestId
      ) {
        return;
      }
      if (response.status === "ok") {
        const token = response.token?.trim() || "";
        if (token.length >= 32 && token.length <= 8192) {
          finish({ status: "ok", token });
          return;
        }
        finish({ status: "read_failed" });
        return;
      }
      if (isDeepSeekExtensionFailureStatus(response.status)) {
        finish({ status: response.status });
        return;
      }
      finish({ status: "read_failed" });
    };
    const timer = window.setTimeout(() => finish({ status: "extension_unavailable" }), timeoutMs);
    window.addEventListener("message", receive);
    window.postMessage({
      source: "tokhub-web",
      type: deepSeekExtensionRequestType,
      version: 1,
      requestId
    }, window.location.origin);
  });
}

function isDeepSeekExtensionFailureStatus(value?: string): value is Exclude<DeepSeekExtensionStatus, "ok" | "extension_unavailable"> {
  return ["deepseek_not_open", "not_logged_in", "permission_denied", "read_failed"].includes(value || "");
}

function newDeepSeekExtensionRequestID(): string {
  if (typeof globalThis.crypto?.randomUUID === "function") {
    return `ds_${globalThis.crypto.randomUUID()}`;
  }
  const bytes = new Uint8Array(16);
  if (typeof globalThis.crypto?.getRandomValues === "function") {
    globalThis.crypto.getRandomValues(bytes);
    return `ds_${Array.from(bytes, (value) => value.toString(16).padStart(2, "0")).join("")}`;
  }
  return `ds_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 18)}`;
}
