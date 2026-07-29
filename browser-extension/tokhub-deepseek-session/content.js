"use strict";

(() => {
  const requestType = "TOKHUB_DEEPSEEK_SESSION_REQUEST";
  const responseType = "TOKHUB_DEEPSEEK_SESSION_RESPONSE";
  const extensionRequestType = "TOKHUB_READ_DEEPSEEK_SESSION";
  const trustedLocalPorts = new Set(["5173", "8080", "28125"]);

  if (!isTrustedTokHubOrigin(window.location.origin)) return;

  window.addEventListener("message", (event) => {
    const request = event.data;
    if (
      event.source !== window ||
      event.origin !== window.location.origin ||
      request?.source !== "tokhub-web" ||
      request.type !== requestType ||
      request.version !== 1 ||
      !validRequestID(request.requestId)
    ) {
      return;
    }

    chrome.runtime.sendMessage({ type: extensionRequestType }, (response) => {
      const status = chrome.runtime.lastError ? "read_failed" : response?.status || "read_failed";
      const payload = {
        source: "tokhub-extension",
        type: responseType,
        version: 1,
        requestId: request.requestId,
        status
      };
      if (status === "ok" && typeof response?.token === "string") {
        payload.token = response.token;
      }
      window.postMessage(payload, window.location.origin);
    });
  });

  function validRequestID(value) {
    return typeof value === "string" && /^ds_[A-Za-z0-9_-]{8,80}$/.test(value);
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
