"use strict";

(() => {
  const requests = {
    TOKHUB_DEEPSEEK_SESSION_REQUEST: {
      responseType: "TOKHUB_DEEPSEEK_SESSION_RESPONSE",
      extensionRequestType: "TOKHUB_READ_DEEPSEEK_SESSION",
      requestPrefix: "ds_"
    },
    TOKHUB_CHATGPT_CALLBACK_REQUEST: {
      responseType: "TOKHUB_CHATGPT_CALLBACK_RESPONSE",
      extensionRequestType: "TOKHUB_READ_CHATGPT_CALLBACK",
      requestPrefix: "cg_"
    }
  };
  const trustedLocalPorts = new Set(["5173", "8080", "28125"]);

  if (!isTrustedTokHubOrigin(window.location.origin)) return;

  window.addEventListener("message", (event) => {
    const request = event.data;
    const requestConfig = requests[request?.type];
    if (
      event.source !== window ||
      event.origin !== window.location.origin ||
      request?.source !== "tokhub-web" ||
      !requestConfig ||
      request.version !== 1 ||
      !validRequestID(request.requestId, requestConfig.requestPrefix)
    ) {
      return;
    }

    const extensionMessage = { type: requestConfig.extensionRequestType };
    if (request.type === "TOKHUB_CHATGPT_CALLBACK_REQUEST") {
      extensionMessage.authorizationId = request.authorizationId;
    }
    chrome.runtime.sendMessage(extensionMessage, (response) => {
      const status = chrome.runtime.lastError ? "read_failed" : response?.status || "read_failed";
      const payload = {
        source: "tokhub-extension",
        type: requestConfig.responseType,
        version: 1,
        requestId: request.requestId,
        status
      };
      if (status === "ok" && typeof response?.token === "string") {
        payload.token = response.token;
      }
      if (status === "ok" && typeof response?.callbackUrl === "string") {
        payload.callbackUrl = response.callbackUrl;
      }
      window.postMessage(payload, window.location.origin);
    });
  });

  function validRequestID(value, prefix) {
    return typeof value === "string" &&
      value.startsWith(prefix) &&
      /^[A-Za-z0-9_-]{8,80}$/.test(value);
  }

  function isTrustedTokHubOrigin(origin) {
    try {
      const url = new URL(origin);
      if (url.protocol === "https:" && (url.hostname === "tokhub.me" || url.hostname === "www.tokhub.me")) {
        return true;
      }
      return url.protocol === "http:" &&
        (url.hostname === "localhost" || url.hostname === "127.0.0.1") &&
        trustedLocalPorts.has(url.port);
    } catch {
      return false;
    }
  }
})();
