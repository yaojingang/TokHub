"use strict";

const deepSeekRequestType = "TOKHUB_READ_DEEPSEEK_SESSION";
const chatGPTCallbackRequestType = "TOKHUB_READ_CHATGPT_CALLBACK";
const deepSeekURLPattern = "https://chat.deepseek.com/*";
const tokenPattern = /^[A-Za-z0-9._~+/=-]+$/;
const trustedLocalPorts = new Set(["5173", "8080", "28125"]);

chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message?.type !== deepSeekRequestType && message?.type !== chatGPTCallbackRequestType) {
    return false;
  }
  if (!isTrustedTokHubURL(sender.tab?.url || "")) {
    sendResponse({ status: "permission_denied" });
    return false;
  }
  if (
    message.type === chatGPTCallbackRequestType &&
    !validAuthorizationID(message.authorizationId)
  ) {
    sendResponse({ status: "permission_denied" });
    return false;
  }
  const request = message.type === chatGPTCallbackRequestType
    ? readChatGPTCallback(message.authorizationId)
    : readDeepSeekSession();
  void request
    .then(sendResponse)
    .catch(() => sendResponse({ status: "read_failed" }));
  return true;
});

async function readDeepSeekSession() {
  const tabs = await chrome.tabs.query({ url: [deepSeekURLPattern] });
  const candidates = tabs
    .filter((tab) => Number.isInteger(tab.id))
    .sort((left, right) => Number(Boolean(right.active)) - Number(Boolean(left.active)) || (right.lastAccessed || 0) - (left.lastAccessed || 0));
  if (candidates.length === 0) {
    return { status: "deepseek_not_open" };
  }

  let permissionFailure = false;
  for (const tab of candidates) {
    try {
      const results = await chrome.scripting.executeScript({
        target: { tabId: tab.id },
        world: "MAIN",
        func: readUserTokenFromDeepSeekPage
      });
      const value = results[0]?.result;
      if (value?.status === "ok") {
        const token = typeof value.token === "string" ? value.token.trim() : "";
        if (token.length >= 32 && token.length <= 8192 && tokenPattern.test(token)) {
          return { status: "ok", token };
        }
        return { status: "read_failed" };
      }
    } catch {
      permissionFailure = true;
    }
  }
  return { status: permissionFailure ? "permission_denied" : "not_logged_in" };
}

async function readChatGPTCallback(authorizationId) {
  const tabs = await chrome.tabs.query({});
  const candidates = tabs
    .filter((tab) => Number.isInteger(tab.id))
    .sort((left, right) => Number(Boolean(right.active)) - Number(Boolean(left.active)) || (right.lastAccessed || 0) - (left.lastAccessed || 0));
  for (const tab of candidates) {
    for (const rawURL of [tab.pendingUrl, tab.url]) {
      const callbackUrl = normalizeChatGPTCallbackURL(rawURL, authorizationId);
      if (callbackUrl) {
        return { status: "ok", callbackUrl };
      }
    }
  }
  return { status: "callback_not_found" };
}

function readUserTokenFromDeepSeekPage() {
  const stored = globalThis.localStorage?.getItem("userToken");
  if (!stored) return { status: "not_logged_in" };
  try {
    const parsed = JSON.parse(stored);
    const token = typeof parsed === "string" ? parsed : parsed?.value;
    return typeof token === "string" && token.trim()
      ? { status: "ok", token: token.trim() }
      : { status: "not_logged_in" };
  } catch {
    return { status: "read_failed" };
  }
}

function normalizeChatGPTCallbackURL(rawURL, authorizationId) {
  if (typeof rawURL !== "string" || rawURL.length > 8192) return "";
  try {
    const url = new URL(rawURL);
    if (
      url.protocol !== "http:" ||
      url.hostname !== "localhost" ||
      url.port !== "1455" ||
      url.pathname !== "/auth/callback" ||
      url.username ||
      url.password ||
      url.hash ||
      !url.searchParams.get("code") ||
      !url.searchParams.get("state") ||
      !url.searchParams.get("state").startsWith(`${authorizationId}.`)
    ) {
      return "";
    }
    return url.toString();
  } catch {
    return "";
  }
}

function validAuthorizationID(value) {
  return typeof value === "string" &&
    /^authz_[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(value);
}

function isTrustedTokHubURL(rawURL) {
  try {
    const url = new URL(rawURL);
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
