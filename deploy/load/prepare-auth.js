import { baseURL } from "./_lib.js";

const base = baseURL();
const jar = new Map();

function storeCookies(response) {
  const cookies = response.headers.getSetCookie ? response.headers.getSetCookie() : [];
  for (const cookie of cookies) {
    const pair = cookie.split(";", 1)[0];
    const [name, value] = pair.split("=");
    if (name && value !== undefined) {
      jar.set(name, value);
    }
  }
}

function cookieHeader() {
  return Array.from(jar.entries()).map(([name, value]) => `${name}=${value}`).join("; ");
}

async function getCSRF() {
  const response = await fetch(base + "/api/auth/csrf", { headers: { Cookie: cookieHeader() } });
  storeCookies(response);
  const payload = await response.json();
  return payload.csrfToken;
}

async function writeJSON(path, body) {
  const csrfToken = await getCSRF();
  const response = await fetch(base + path, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-CSRF-Token": csrfToken,
      Cookie: cookieHeader()
    },
    body: JSON.stringify(body ?? {})
  });
  storeCookies(response);
  const payload = await response.json();
  if (!response.ok) {
    throw new Error(`${path} failed: ${response.status} ${JSON.stringify(payload)}`);
  }
  return payload;
}

async function patchJSON(path, body) {
  const csrfToken = await getCSRF();
  const response = await fetch(base + path, {
    method: "PATCH",
    headers: {
      "Content-Type": "application/json",
      "X-CSRF-Token": csrfToken,
      Cookie: cookieHeader()
    },
    body: JSON.stringify(body ?? {})
  });
  storeCookies(response);
  const payload = await response.json();
  if (!response.ok) {
    throw new Error(`${path} failed: ${response.status} ${JSON.stringify(payload)}`);
  }
  return payload;
}

async function readJSON(path) {
  const response = await fetch(base + path, { headers: { Cookie: cookieHeader() } });
  storeCookies(response);
  const payload = await response.json();
  if (!response.ok) {
    throw new Error(`${path} failed: ${response.status} ${JSON.stringify(payload)}`);
  }
  return payload;
}

await writeJSON("/api/auth/login", {
  email: process.env.TOKHUB_ADMIN_EMAIL || "admin@tokhub.local",
  password: process.env.TOKHUB_ADMIN_PASSWORD || "ChangeMe123!"
});
await patchJSON("/api/admin/settings", { registrationOpen: true });

const channelList = await readJSON("/api/public/channels?page=1&pageSize=1");
const channelId = channelList.items[0].id;
const unique = Date.now();
const privateChannel = await writeJSON("/api/me/private-channels", {
  name: `Load Private ${unique}`,
  provider: "OpenAI",
  type: "openai-compatible",
  model: "gpt-4o-mini",
  endpoint: "https://load.example/v1",
  apiKey: `sk-load-${unique}-private-secret`,
  probeDaily: 5000
});
const gateway = await writeJSON("/api/console/gateways", {
  name: `Load Gateway ${unique}`,
  policy: "latency",
  upstreamIds: [privateChannel.channel.id],
  qpsLimit: 300,
  quotaMonth: 100000
});
const key = await writeJSON("/api/console/gateway-keys", {
  gatewayId: gateway.gateway.id,
  name: `Load Key ${unique}`,
  quotaMonth: 100000,
  qpsLimit: 300
});
const csrfToken = await getCSRF();

console.log(JSON.stringify({
  baseURL: base,
  cookie: cookieHeader(),
  csrfToken,
  gatewayKey: key.key.plainKey,
  channelId
}, null, 2));
