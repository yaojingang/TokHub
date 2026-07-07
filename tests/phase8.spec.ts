import { expect, Page, test } from "@playwright/test";
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { adminLogin } from "./helpers";

test.describe.configure({ mode: "serial" });

async function writeJSON(page: Page, path: string, method: string, body?: unknown) {
  const token = await page.evaluate(async () => {
    const response = await fetch("/api/auth/csrf", { credentials: "include" });
    return ((await response.json()) as { csrfToken: string }).csrfToken;
  });
  return page.evaluate(
    async ({ path: targetPath, method: targetMethod, body: payload, token: csrfToken }) => {
      const response = await fetch(targetPath, {
        method: targetMethod,
        credentials: "include",
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": csrfToken
        },
        body: payload === undefined ? undefined : JSON.stringify(payload)
      });
      const contentType = response.headers.get("content-type") ?? "";
      const payloadBody = contentType.includes("application/json") ? await response.json() : await response.text();
      return { ok: response.ok, status: response.status, payload: payloadBody };
    },
    { path, method, body, token }
  );
}

async function readJSON(page: Page, path: string) {
  return page.evaluate(async (targetPath) => {
    const response = await fetch(targetPath, { credentials: "include" });
    const contentType = response.headers.get("content-type") ?? "";
    const payload = contentType.includes("application/json") ? await response.json() : await response.text();
    return { ok: response.ok, status: response.status, payload };
  }, path);
}

async function gatewayJSON(page: Page, path: string, method: string, key: string, body?: unknown) {
  return page.evaluate(
    async ({ targetPath, targetMethod, apiKey, payload }) => {
      const response = await fetch(targetPath, {
        method: targetMethod,
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${apiKey}`
        },
        body: payload === undefined ? undefined : JSON.stringify(payload)
      });
      return { ok: response.ok, status: response.status, payload: await response.json() };
    },
    { targetPath: path, targetMethod: method, apiKey: key, payload: body }
  );
}

function readRequestBody(req: IncomingMessage) {
  return new Promise<string>((resolve) => {
    const chunks: Buffer[] = [];
    req.on("data", (chunk) => chunks.push(Buffer.from(chunk)));
    req.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
  });
}

test("phase 8 full release journey and security gates", async ({ page }) => {
  await adminLogin(page, "/admin");
  const suffix = Date.now();
  const upstreamSecret = `phase8-upstream-${suffix}`;
  const upstream = createServer((req: IncomingMessage, res: ServerResponse) => {
    void (async () => {
      const body = await readRequestBody(req);
      if (req.headers.authorization !== `Bearer ${upstreamSecret}`) {
        res.writeHead(401, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ error: "bad auth" }));
        return;
      }
      if (req.url === "/v1/chat/completions") {
        if (body.includes("Reply exactly: K")) {
          res.writeHead(200, { "Content-Type": "application/json" });
          res.end(JSON.stringify({
            id: "chatcmpl-phase8-probe",
            object: "chat.completion",
            model: "gpt-phase8",
            choices: [{ index: 0, message: { role: "assistant", content: "K" }, finish_reason: "stop" }],
            usage: { prompt_tokens: 4, completion_tokens: 1, total_tokens: 5 }
          }));
          return;
        }
        res.writeHead(200, { "Content-Type": "application/json" });
        res.end(JSON.stringify({
          id: "chatcmpl-phase8",
          object: "chat.completion",
          model: "gpt-phase8",
          choices: [{ index: 0, message: { role: "assistant", content: "TokHub gateway response from phase8 release" }, finish_reason: "stop" }],
          usage: { prompt_tokens: 8, completion_tokens: 5, total_tokens: 13 }
        }));
        return;
      }
      res.writeHead(404, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "not found" }));
    })();
  });
  await new Promise<void>((resolve) => upstream.listen(0, "0.0.0.0", resolve));
  const address = upstream.address();
  if (typeof address !== "object" || address === null) throw new Error("upstream failed to bind");
  const upstreamHost = process.env.TOKHUB_TEST_UPSTREAM_HOST || "host.docker.internal";
  const endpointRoot = `http://${upstreamHost}:${address.port}`;

  const openedRegistration = await writeJSON(page, "/api/admin/settings", "PATCH", { registrationOpen: true });
  expect(openedRegistration.ok).toBeTruthy();

  const publicChannels = await readJSON(page, "/api/public/channels?page=1&pageSize=3");
  expect(publicChannels.ok).toBeTruthy();
  expect(publicChannels.payload.items.length).toBeGreaterThan(0);
  const channelID = publicChannels.payload.items[0].id as string;

  const email = `phase8-${suffix}-release-user-with-long-email-address@example.com`;
  const registered = await writeJSON(page, "/api/auth/register", "POST", {
    email,
    password: "admin@tokhub.local",
    name: "Phase8 Release Workspace"
  });
  expect(registered.status).toBe(201);
  if (registered.payload.devVerificationToken) {
    await writeJSON(page, "/api/auth/verify-email", "POST", { token: registered.payload.devVerificationToken });
  }
  const loggedIn = await writeJSON(page, "/api/auth/login", "POST", { email, password: "admin@tokhub.local" });
  expect(loggedIn.ok).toBeTruthy();

  const favorite = await writeJSON(page, `/api/me/favorites/${channelID}`, "PUT", {});
  expect(favorite.ok).toBeTruthy();
  const favorites = await readJSON(page, "/api/me/favorites");
  expect(favorites.payload.ids).toContain(channelID);

  const privateChannel = await writeJSON(page, "/api/me/private-channels", "POST", {
    name: "Phase8 Very Long Private Channel Name For Release Text Overflow Verification",
    provider: "OpenAI",
    type: "openai-compatible",
    model: "gpt-phase8-with-a-long-release-model-name",
    endpoint: endpointRoot,
    apiKey: upstreamSecret,
    probeDaily: 5
  });
  expect(privateChannel.ok).toBeTruthy();
  expect(JSON.stringify(privateChannel.payload)).not.toContain(upstreamSecret);
  expect(privateChannel.payload.channel.keyMask).toContain("****");
  const privateID = privateChannel.payload.channel.id as string;
  const privateProbe = await writeJSON(page, `/api/me/private-channels/${privateID}/probe-now`, "POST", {});
  expect(privateProbe.ok).toBeTruthy();

  const gateway = await writeJSON(page, "/api/console/gateways", "POST", {
    name: "Phase8 Release Gateway",
    policy: "latency",
    upstreamIds: [privateID],
    qpsLimit: 100,
    quotaMonth: 1000
  });
  expect(gateway.ok).toBeTruthy();

  const key = await writeJSON(page, "/api/console/gateway-keys", "POST", {
    gatewayId: gateway.payload.gateway.id,
    name: "Phase8 Release Key",
    quotaMonth: 1000,
    qpsLimit: 100
  });
  expect(key.ok).toBeTruthy();
  const plainKey = key.payload.key.plainKey as string;
  expect(plainKey).toMatch(/^sk-th-/);
  const keyList = await readJSON(page, "/api/console/gateway-keys");
  expect(JSON.stringify(keyList.payload)).not.toContain(plainKey);

  const gatewayCall = await gatewayJSON(page, "/gateway/v1/chat/completions", "POST", plainKey, {
    model: "gpt-phase8",
    messages: [{ role: "user", content: "phase8 release" }]
  });
  expect(gatewayCall.ok).toBeTruthy();
  expect(gatewayCall.payload.choices[0].message.content).toContain("TokHub gateway response");

  const alertRule = await writeJSON(page, "/api/console/alerts/rules", "POST", {
    name: "Phase8 release cost guard",
    kind: "cost_threshold",
    severity: "warning",
    threshold: 0,
    windowMinutes: 1440,
    dedupeMinutes: 30,
    enabled: true
  });
  expect(alertRule.ok).toBeTruthy();
  const alertEvaluation = await writeJSON(page, "/api/console/alerts/evaluate", "POST", {});
  expect(alertEvaluation.ok).toBeTruthy();
  expect(alertEvaluation.payload.deliveries.length).toBeGreaterThan(0);

  const audit = await readJSON(page, "/api/console/audit?limit=100");
  expect(audit.ok).toBeTruthy();
  expect(JSON.stringify(audit.payload)).not.toContain(plainKey);
  expect(JSON.stringify(audit.payload)).not.toContain(upstreamSecret);
  const auditExport = await page.evaluate(async () => {
    const response = await fetch("/api/console/audit/export?limit=500", { credentials: "include" });
    return { ok: response.ok, text: await response.text() };
  });
  expect(auditExport.ok).toBeTruthy();
  expect(auditExport.text).toContain("created_at,actor_email,action");
  expect(auditExport.text).not.toContain(plainKey);
  expect(auditExport.text).not.toContain(upstreamSecret);

  const adminDenied = await readJSON(page, "/api/admin/audit");
  expect(adminDenied.status).toBe(403);

  await page.goto("/console/gateways");
  await expect(page.getByRole("heading", { name: "一键生成工作区专属中转站" })).toBeVisible();
  await page.keyboard.press("Tab");
  await expect(page.locator(":focus")).toBeVisible();
  await page.goto(`/channels/${channelID}`);
  await expect(page.getByRole("heading")).toBeVisible();
  await page.goto("/recommend");
  await expect(page.getByRole("heading", { name: /不踩坑的/ })).toBeVisible();
  await page.goBack();
  await expect(page.getByRole("heading")).toBeVisible();

  const metrics = await page.evaluate(async () => (await fetch("/metrics")).text());
  expect(metrics).toContain("tokhub_gateway_requests_total");
  expect(metrics).toContain("tokhub_alert_deliveries_total");

  await new Promise<void>((resolve) => upstream.close(() => resolve()));
});

test("phase 8 public, health and deep-link routes are stable", async ({ page }) => {
  const health = await page.request.get("/healthz");
  expect(health.ok()).toBeTruthy();
  const ready = await page.request.get("/readyz");
  expect(ready.ok()).toBeTruthy();
  const overview = await page.request.get("/api/public/overview");
  expect(overview.ok()).toBeTruthy();

  await page.goto("/");
  await expect(page.getByRole("heading", { name: "先看可用性，再选择中转站" })).toBeVisible();
  await page.goto("/dashboard");
  await expect(page.getByRole("heading", { name: "监控总览" })).toBeVisible();
  const channelID = ((await (await page.request.get("/api/public/channels?page=1&pageSize=1")).json()).items[0].id) as string;
  await page.goto(`/channels/${channelID}`);
  await expect(page.getByText("综合健康指数").first()).toBeVisible();
  await page.goto("/dashboard");
  await expect(page.getByRole("heading", { name: "监控总览" })).toBeVisible();
});
