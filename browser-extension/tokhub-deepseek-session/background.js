"use strict";

const requestType = "TOKHUB_READ_DEEPSEEK_SESSION";
const deepSeekURLPattern = "https://chat.deepseek.com/*";
const tokenPattern = /^[A-Za-z0-9._~+/=-]+$/;
const trustedLocalPorts = new Set(["5173", "8080", "28125"]);

chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message?.type !== requestType) return false;
  if (!isTrustedTokHubURL(sender.tab?.url || "")) {
    sendResponse({ status: "permission_denied" });
    return false;
  }
  void readDeepSeekSession()
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
