#!/usr/bin/env node
import { chmod, readFile, writeFile } from "node:fs/promises";
import { basename } from "node:path";
import { createInterface } from "node:readline/promises";
import { Writable } from "node:stream";

const BASE_URL = (process.env.TOKHUB_BASE_URL || "").replace(/\/+$/, "");
const TOKEN = process.env.TOKHUB_ADMIN_AGENT_TOKEN || "";
const SENSITIVE_FIELD_NAMES = new Set([
  "apikey",
  "api_key",
  "gatewaykey",
  "gateway_key",
  "plainkey",
  "plain_key",
  "plaintoken",
  "plain_token",
  "plainruntimekey",
  "plain_runtime_key",
  "providerkey",
  "provider_key",
  "runtimekey",
  "runtime_key",
  "secret",
  "secretkey",
  "secret_key",
  "sitekey",
  "site_key",
  "tokenplaintext",
  "token_plaintext",
  "password",
]);

function fail(message, code = 1) {
  console.error(`FAIL: ${message}`);
  process.exit(code);
}

function usage() {
  console.log(`Usage:
  tokhub-admin.mjs bootstrap [--admin-url https://host/admin] [--identifier user@example.com] [--token-name codex-local] [--scopes admin:*] [--ttl-hours 24] [--save-env .env.tokhub-admin]
  tokhub-admin.mjs preflight
  tokhub-admin.mjs request METHOD /api/admin/path [--execute] [--reason "..."] [--idempotency-key "..."] [--json '{"ok":true}'] [--body file.json] [--form key=value] [--form-file field=path] [--output file]
  tokhub-admin.mjs audit-verify [--token-id aat_...] [--idempotency-key key] [--limit 500]
`);
}

function parseOptions(argv) {
  const positional = [];
  const options = {};
  const repeatable = new Set(["form", "form-file"]);
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (!arg.startsWith("--")) {
      positional.push(arg);
      continue;
    }
    const key = arg.slice(2);
    if (key === "execute") {
      options.execute = true;
      continue;
    }
    if (key === "help") {
      options.help = true;
      continue;
    }
    const value = argv[i + 1];
    if (value === undefined || value.startsWith("--")) {
      fail(`missing value for --${key}`);
    }
    if (repeatable.has(key)) {
      options[key] ||= [];
      options[key].push(value);
    } else {
      options[key] = value;
    }
    i += 1;
  }
  return { positional, options };
}

function requireEnv() {
  if (!BASE_URL) {
    fail("TOKHUB_BASE_URL is required");
  }
  if (!TOKEN) {
    fail("TOKHUB_ADMIN_AGENT_TOKEN is required");
  }
}

function normalizeAdminPath(path) {
  if (!path || !path.startsWith("/api/admin/")) {
    fail(`admin-agent client only allows /api/admin/* paths, got ${path || "<empty>"}`);
  }
  if (path.startsWith("/api/admin/agent-tokens")) {
    fail("admin-agent bearer tokens cannot manage /api/admin/agent-tokens");
  }
  return path;
}

function isReadMethod(method) {
  return method === "GET" || method === "HEAD";
}

function needsWriteGuard(method, path) {
  return !isReadMethod(method) || (path.includes("/channel-sites/") && path.endsWith("/download"));
}

function redactHeaders(headers) {
  return Object.fromEntries([...headers.entries()].filter(([key]) => key.toLowerCase() !== "authorization"));
}

function isSensitiveField(key) {
  return SENSITIVE_FIELD_NAMES.has(String(key).toLowerCase());
}

function redactSensitive(value) {
  if (Array.isArray(value)) {
    return value.map((item) => redactSensitive(item));
  }
  if (!value || typeof value !== "object") {
    return value;
  }
  return Object.fromEntries(Object.entries(value).map(([key, item]) => {
    if (isSensitiveField(key)) {
      return [key, "[REDACTED]"];
    }
    return [key, redactSensitive(item)];
  }));
}

function isTextContent(contentType) {
  const type = contentType.toLowerCase();
  return type.startsWith("text/") || type.includes("csv") || type.includes("yaml") || type.includes("xml");
}

function parseAssignment(raw, flag) {
  const index = String(raw).indexOf("=");
  if (index <= 0) {
    fail(`${flag} must use field=value syntax`);
  }
  return [String(raw).slice(0, index), String(raw).slice(index + 1)];
}

function shellQuote(value) {
  return `'${String(value).replace(/'/g, "'\\''")}'`;
}

function normalizeBaseURL(raw) {
  const value = String(raw || "").trim();
  if (!value) {
    fail("--admin-url, TOKHUB_ADMIN_URL, or TOKHUB_BASE_URL is required");
  }
  let parsed;
  try {
    parsed = new URL(value);
  } catch {
    fail(`invalid admin URL: ${value}`);
  }
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    fail("admin URL must use http or https");
  }
  if (parsed.username || parsed.password) {
    fail("admin URL must not include username or password");
  }
  return parsed.origin.replace(/\/+$/, "");
}

function parseTTLHours(value) {
  const raw = String(value || "").trim();
  if (!raw) {
    return 24;
  }
  const parsed = Number.parseInt(raw, 10);
  if (!Number.isFinite(parsed) || parsed < 1 || parsed > 720) {
    fail("--ttl-hours must be an integer between 1 and 720");
  }
  return parsed;
}

function isSecretArtifactPath(path) {
  return path.endsWith("/channels/export") || (path.includes("/channel-sites/") && path.endsWith("/download"));
}

function createMutedOutput() {
  const output = new Writable({
    write(chunk, encoding, callback) {
      if (!output.muted) {
        process.stdout.write(chunk, encoding);
      }
      callback();
    },
  });
  output.muted = false;
  return output;
}

async function promptBootstrapInputs(options) {
  const adminURL = options["admin-url"] || process.env.TOKHUB_ADMIN_URL || process.env.TOKHUB_BASE_URL || "";
  let identifier = options.identifier || process.env.TOKHUB_ADMIN_IDENTIFIER || process.env.TOKHUB_ADMIN_EMAIL || "";
  let password = process.env.TOKHUB_ADMIN_PASSWORD || "";

  if (adminURL && identifier && password) {
    return { adminURL, identifier, password };
  }
  if (!process.stdin.isTTY) {
    fail("missing bootstrap input; provide --admin-url, --identifier, and TOKHUB_ADMIN_PASSWORD in non-interactive mode");
  }

  const output = createMutedOutput();
  const rl = createInterface({ input: process.stdin, output, terminal: true });
  try {
    const nextAdminURL = adminURL || (await rl.question("TokHub admin URL: "));
    if (!identifier) {
      identifier = (await rl.question("Admin username/email: ")).trim();
    }
    if (!password) {
      process.stdout.write("Admin password: ");
      output.muted = true;
      password = await rl.question("");
      output.muted = false;
      process.stdout.write("\n");
    }
    return { adminURL: nextAdminURL, identifier, password };
  } finally {
    output.muted = false;
    rl.close();
  }
}

function setCookieValues(headers) {
  if (typeof headers.getSetCookie === "function") {
    return headers.getSetCookie();
  }
  const raw = headers.get("set-cookie");
  if (!raw) {
    return [];
  }
  return raw.split(/,(?=\s*[^;,=\s]+=)/g);
}

function storeCookies(jar, headers) {
  for (const raw of setCookieValues(headers)) {
    const pair = raw.split(";")[0]?.trim();
    if (!pair) {
      continue;
    }
    const index = pair.indexOf("=");
    if (index <= 0) {
      continue;
    }
    jar.set(pair.slice(0, index), pair.slice(index + 1));
  }
}

function cookieHeader(jar) {
  return [...jar.entries()].map(([key, value]) => `${key}=${value}`).join("; ");
}

async function browserJSONFetch(baseURL, path, { method = "GET", body, csrfToken, jar } = {}) {
  const headers = new Headers({ Accept: "application/json" });
  if (body !== undefined) {
    headers.set("Content-Type", "application/json");
  }
  if (csrfToken) {
    headers.set("X-CSRF-Token", csrfToken);
  }
  const cookie = cookieHeader(jar);
  if (cookie) {
    headers.set("Cookie", cookie);
  }
  const response = await fetch(`${baseURL}${path}`, { method, headers, body });
  storeCookies(jar, response.headers);
  const text = await response.text();
  let payload = {};
  if (text.trim()) {
    try {
      payload = JSON.parse(text);
    } catch {
      fail(`HTTP ${response.status} from ${path}: non-JSON response`);
    }
  }
  if (!response.ok) {
    const message = payload?.error?.message || payload?.error?.code || text || response.statusText;
    fail(`HTTP ${response.status} from ${path}: ${message}`);
  }
  return payload;
}

async function bootstrap(options) {
  const inputs = await promptBootstrapInputs(options);
  const baseURL = normalizeBaseURL(inputs.adminURL);
  const tokenName = options["token-name"] || process.env.TOKHUB_ADMIN_AGENT_TOKEN_NAME || "codex-local";
  const scopes = String(options.scopes || process.env.TOKHUB_ADMIN_AGENT_TOKEN_SCOPES || "admin:*")
    .split(",")
    .map((scope) => scope.trim())
    .filter(Boolean);
  const ttlHours = parseTTLHours(options["ttl-hours"] || process.env.TOKHUB_ADMIN_AGENT_TOKEN_TTL_HOURS || "24");
  if (!inputs.identifier.trim()) {
    fail("admin username/email is required");
  }
  if (!inputs.password) {
    fail("admin password is required");
  }

  const jar = new Map();
  const csrf = await browserJSONFetch(baseURL, "/api/auth/csrf", { jar });
  if (!csrf.csrfToken) {
    fail("CSRF token was not returned by target admin backend");
  }
  await browserJSONFetch(baseURL, "/api/auth/login", {
    method: "POST",
    jar,
    csrfToken: csrf.csrfToken,
    body: JSON.stringify({ identifier: inputs.identifier.trim(), password: inputs.password }),
  });
  const created = await browserJSONFetch(baseURL, "/api/admin/agent-tokens", {
    method: "POST",
    jar,
    csrfToken: csrf.csrfToken,
    body: JSON.stringify({ name: tokenName, scopes, ttlHours }),
  });
  const plainToken = created?.token?.plainToken;
  if (!plainToken) {
    fail("admin-agent token response did not include plainToken");
  }

  const envText = `export TOKHUB_BASE_URL=${shellQuote(baseURL)}\nexport TOKHUB_ADMIN_AGENT_TOKEN=${shellQuote(plainToken)}\n`;
  const envFile = options["save-env"] || options["env-file"] || "";
  if (envFile) {
    await writeFile(envFile, envText, { mode: 0o600 });
    await chmod(envFile, 0o600);
    console.log(JSON.stringify({
      ok: true,
      baseUrl: baseURL,
      envFile,
      tokenMask: created.token.tokenMask,
      scopes: created.token.scopes,
      expiresAt: created.token.expiresAt || null,
      next: `source ${envFile} && node ${process.argv[1]} preflight`,
    }, null, 2));
    return;
  }

  console.log("# Paste these exports into your current shell. Treat the token as a secret.");
  process.stdout.write(envText);
}

async function readRequestBody(options) {
  const usesJSON = Boolean(options.json || options.body);
  const usesForm = Boolean(options.form || options["form-file"]);
  if (usesJSON && usesForm) {
    fail("use JSON body options or form options, not both");
  }
  if (options.json) {
    JSON.parse(options.json);
    return { body: options.json, contentType: "application/json" };
  }
  if (options.body) {
    const text = await readFile(options.body, "utf8");
    JSON.parse(text);
    return { body: text, contentType: "application/json" };
  }
  if (usesForm) {
    const form = new FormData();
    for (const raw of options.form || []) {
      const [field, value] = parseAssignment(raw, "--form");
      form.set(field, value);
    }
    for (const raw of options["form-file"] || []) {
      const [field, filePath] = parseAssignment(raw, "--form-file");
      const data = await readFile(filePath);
      form.set(field, new Blob([data]), basename(filePath));
    }
    return { body: form, contentType: "" };
  }
  return { body: undefined, contentType: "" };
}

async function adminFetch(path, init = {}) {
  requireEnv();
  const headers = new Headers(init.headers || {});
  headers.set("Authorization", `Bearer ${TOKEN}`);
  const response = await fetch(`${BASE_URL}${path}`, { ...init, headers });
  const contentType = response.headers.get("content-type") || "";
  const buffer = Buffer.from(await response.arrayBuffer());
  if (!response.ok) {
    const body = buffer.toString("utf8");
    fail(`HTTP ${response.status} ${response.statusText} for ${path}: ${body}`);
  }
  if (contentType.includes("application/json")) {
    return { response, body: JSON.parse(buffer.toString("utf8")), raw: buffer, contentType, json: true };
  }
  return { response, body: buffer.toString("utf8"), raw: buffer, contentType, json: false };
}

async function preflight() {
  requireEnv();
  const health = await fetch(`${BASE_URL}/healthz`);
  if (!health.ok) {
    fail(`healthz failed with HTTP ${health.status}`);
  }
  const admin = await adminFetch("/api/admin/production-health", { method: "GET" });
  console.log(JSON.stringify({
    ok: true,
    baseUrl: BASE_URL,
    healthz: await health.json(),
    productionHealthKeys: Object.keys(admin.body || {}),
  }, null, 2));
}

async function request(method, path, options) {
  method = method.toUpperCase();
  path = normalizeAdminPath(path);
  const guarded = needsWriteGuard(method, path);
  const reason = options.reason || process.env.TOKHUB_ADMIN_AGENT_REASON || "";
  const idempotencyKey = options["idempotency-key"] || process.env.TOKHUB_ADMIN_AGENT_IDEMPOTENCY_KEY || "";

  if (guarded && !options.execute) {
    console.log(JSON.stringify({
      blocked: true,
      reason: "mutating_or_sensitive_operation_requires_execute",
      method,
      path,
      requiredFlags: ["--execute", "--reason", "--idempotency-key"],
    }, null, 2));
    process.exit(2);
  }
  if (guarded && reason.trim().length < 3) {
    fail("--reason is required for this operation");
  }
  if (guarded && !/^\S{8,120}$/.test(idempotencyKey)) {
    fail("--idempotency-key must be 8-120 non-space characters");
  }

  const headers = new Headers();
  const requestBody = await readRequestBody(options);
  if (requestBody.contentType) {
    headers.set("Content-Type", requestBody.contentType);
  }
  if (reason) {
    headers.set("X-TokHub-Agent-Reason", reason);
  }
  if (idempotencyKey) {
    headers.set("X-Idempotency-Key", idempotencyKey);
  }

  if (!options.output && isSecretArtifactPath(path)) {
    fail(`${path} returns secret-bearing artifact content and requires --output`);
  }
  const result = await adminFetch(path, { method, headers, body: requestBody.body });
  if (options.output) {
    const output = result.json
      ? Buffer.from(`${JSON.stringify(redactSensitive(result.body), null, 2)}\n`, "utf8")
      : result.raw;
    await writeFile(options.output, output);
    console.log(JSON.stringify({
      ok: true,
      status: result.response.status,
      output: options.output,
      redacted: result.json,
      sensitivePackage: isSecretArtifactPath(path) || undefined,
      headers: redactHeaders(result.response.headers),
    }, null, 2));
    return;
  }
  if (!result.json && isTextContent(result.contentType)) {
    console.log(result.body);
    return;
  }
  if (!result.json) {
    fail(`non-text response from ${path} requires --output`);
  }
  console.log(JSON.stringify({
    ok: true,
    status: result.response.status,
    method,
    path,
    idempotencyKey: idempotencyKey || undefined,
    body: redactSensitive(result.body),
  }, null, 2));
}

async function auditVerify(options) {
  const tokenID = options["token-id"] || "";
  const idempotencyKey = options["idempotency-key"] || "";
  const limit = options.limit || "500";
  const params = new URLSearchParams({ limit });
  if (tokenID) {
    params.set("actor", tokenID);
  }
  const result = await adminFetch(`/api/admin/audit?${params.toString()}`, { method: "GET" });
  const items = Array.isArray(result.body.items) ? result.body.items : [];
  const matches = items.filter((item) => {
    const metadata = item.metadata || {};
    if (tokenID && item.actorId !== tokenID && metadata.agent_token_id !== tokenID) {
      return false;
    }
    if (idempotencyKey && metadata.idempotency_key !== idempotencyKey) {
      return false;
    }
    return item.actorType === "agent" || metadata.agent_token_id;
  });
  console.log(JSON.stringify({
    ok: matches.length > 0,
    checked: items.length,
    matches: matches.slice(0, 5),
  }, null, 2));
  if (matches.length === 0) {
    process.exit(3);
  }
}

async function main() {
  const { positional, options } = parseOptions(process.argv.slice(2));
  if (options.help || positional.length === 0) {
    usage();
    return;
  }
  const command = positional[0];
  if (command === "bootstrap") {
    await bootstrap(options);
    return;
  }
  if (command === "preflight") {
    await preflight();
    return;
  }
  if (command === "request") {
    const method = positional[1];
    const path = positional[2];
    if (!method || !path) {
      fail("request requires METHOD and /api/admin/path");
    }
    await request(method, path, options);
    return;
  }
  if (command === "audit-verify") {
    if (!options["token-id"] && !options["idempotency-key"]) {
      options["idempotency-key"] = process.env.TOKHUB_ADMIN_AGENT_IDEMPOTENCY_KEY || "";
    }
    if (!options["token-id"] && !options["idempotency-key"]) {
      fail("audit-verify requires --token-id, --idempotency-key, or TOKHUB_ADMIN_AGENT_IDEMPOTENCY_KEY");
    }
    await auditVerify(options);
    return;
  }
  fail(`unknown command: ${command}`);
}

main().catch((error) => fail(error?.stack || error?.message || String(error)));
