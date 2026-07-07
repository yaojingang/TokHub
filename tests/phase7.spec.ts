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
    return { ok: response.ok, status: response.status, payload: await response.json() };
  }, path);
}

async function gatewayJSON(page: Page, path: string, method: string, key: string, body?: unknown) {
  return page.evaluate(
    async ({ targetPath, targetMethod, apiKey, payload }) => {
      const response = await fetch(targetPath, {
        method: targetMethod,
        headers: {
          "Content-Type": "application/json",
          "Authorization": `Bearer ${apiKey}`
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

test("phase 7 usage rollup, alerts, audit export and metrics", async ({ page }) => {
  await adminLogin(page, "/console");

  const suffix = Date.now();
  const upstreamSecret = `phase7-upstream-${suffix}`;
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
            id: "chatcmpl-phase7-probe",
            object: "chat.completion",
            model: "gpt-phase7",
            choices: [{ index: 0, message: { role: "assistant", content: "K" }, finish_reason: "stop" }],
            usage: { prompt_tokens: 4, completion_tokens: 1, total_tokens: 5 }
          }));
          return;
        }
        res.writeHead(200, { "Content-Type": "application/json" });
        res.end(JSON.stringify({
          id: "chatcmpl-phase7",
          object: "chat.completion",
          model: "gpt-phase7",
          choices: [{ index: 0, message: { role: "assistant", content: "phase7 ok" }, finish_reason: "stop" }],
          usage: { prompt_tokens: 9, completion_tokens: 2, total_tokens: 11 }
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

  const privateCreated = await writeJSON(page, "/api/me/private-channels", "POST", {
    name: `Phase7 Private ${suffix}`,
    provider: "OpenAI",
    type: "openai-compatible",
    model: "gpt-phase7",
    endpoint: endpointRoot,
    apiKey: upstreamSecret,
    probeDaily: 5
  });
  expect(privateCreated.ok).toBeTruthy();
  const privateID = privateCreated.payload.channel.id as string;
  const privateProbe = await writeJSON(page, `/api/me/private-channels/${privateID}/probe-now`, "POST", {});
  expect(privateProbe.ok).toBeTruthy();

  const brokenPrivate = await writeJSON(page, "/api/me/private-channels", "POST", {
    name: `Phase7 Broken ${Date.now()}`,
    provider: "OpenAI",
    type: "openai-compatible",
    model: "gpt-4o-mini",
    endpoint: "http://127.0.0.1:9/v1",
    apiKey: "sk-phase7-broken-secret",
    probeDaily: 5
  });
  expect(brokenPrivate.ok).toBeTruthy();
  const brokenID = brokenPrivate.payload.channel.id as string;
  const brokenProbe = await writeJSON(page, `/api/me/private-channels/${brokenID}/probe-now`, "POST", {});
  expect(brokenProbe.ok).toBeTruthy();

  const gatewayCreated = await writeJSON(page, "/api/console/gateways", "POST", {
    name: `Phase7 Gateway ${Date.now()}`,
    policy: "latency",
    upstreamIds: [privateID],
    qpsLimit: 20,
    quotaMonth: 1000
  });
  expect(gatewayCreated.ok).toBeTruthy();

  const keyCreated = await writeJSON(page, "/api/console/gateway-keys", "POST", {
    gatewayId: gatewayCreated.payload.gateway.id,
    name: "Phase7 Key",
    quotaMonth: 1000,
    qpsLimit: 20
  });
  expect(keyCreated.ok).toBeTruthy();
  const plainKey = keyCreated.payload.key.plainKey as string;

  const chat = await gatewayJSON(page, "/gateway/v1/chat/completions", "POST", plainKey, {
    model: "gpt-phase7",
    messages: [{ role: "user", content: "phase7 usage" }]
  });
  expect(chat.ok).toBeTruthy();

  const usageBefore = await readJSON(page, "/api/console/usage");
  expect(usageBefore.ok).toBeTruthy();
  const rollupRequestsBefore = sumGatewayRollups(usageBefore.payload.rollups);
  expect(rollupRequestsBefore).toBeGreaterThanOrEqual(1);

  await writeJSON(page, "/api/console/usage/rollup/recompute", "POST", {});
  await writeJSON(page, "/api/console/usage/rollup/recompute", "POST", {});
  const usageAfter = await readJSON(page, "/api/console/usage");
  expect(sumGatewayRollups(usageAfter.payload.rollups)).toBe(rollupRequestsBefore);

  const baselineRule = await writeJSON(page, "/api/console/alerts/rules", "POST", {
    name: `Phase7 baseline rule ${suffix}`,
    kind: "l3_consecutive_failures",
    severity: "warning",
    threshold: 1,
    windowMinutes: 60,
    dedupeMinutes: 30,
    enabled: true
  });
  expect(baselineRule.ok).toBeTruthy();

  const baselineChannel = await writeJSON(page, "/api/console/alerts/channels", "POST", {
    name: `Phase7 baseline notify ${suffix}`,
    type: "email",
    target: `phase7-${suffix}@tokhub.run`,
    enabled: true
  });
  expect(baselineChannel.ok).toBeTruthy();

  const center = await readJSON(page, "/api/console/alerts");
  expect(center.ok).toBeTruthy();
  expect(center.payload.rules.length).toBeGreaterThanOrEqual(1);
  expect(center.payload.channels.length).toBeGreaterThanOrEqual(1);
  const brokenIncident = center.payload.incidents.find((item: { channelId: string }) => item.channelId === brokenID);
  expect(brokenIncident?.events?.length).toBeGreaterThan(0);
  expect(JSON.stringify(brokenIncident.events)).toContain("opened");

  const customRule = await writeJSON(page, "/api/console/alerts/rules", "POST", {
    name: "Phase7 cost smoke",
    kind: "cost_threshold",
    severity: "warning",
    threshold: 0,
    windowMinutes: 1440,
    dedupeMinutes: 30,
    enabled: true
  });
  expect(customRule.ok).toBeTruthy();

  const evaluated = await writeJSON(page, "/api/console/alerts/evaluate", "POST", {});
  expect(evaluated.ok).toBeTruthy();
  expect(evaluated.payload.deliveries.length).toBeGreaterThan(0);
  const evaluatedAgain = await writeJSON(page, "/api/console/alerts/evaluate", "POST", {});
  expect(evaluatedAgain.ok).toBeTruthy();
  expect(JSON.stringify(evaluatedAgain.payload.deliveries)).toContain("suppressed");

  const channelID = baselineChannel.payload.channel.id as string;
  const testDelivery = await writeJSON(page, `/api/console/alerts/channels/${channelID}/test`, "POST", {});
  expect(testDelivery.ok).toBeTruthy();
  expect(testDelivery.payload.delivery.status).toBe("test");

  const audit = await readJSON(page, "/api/console/audit?limit=50");
  expect(audit.ok).toBeTruthy();
  expect(audit.payload.items.length).toBeGreaterThan(0);
  expect(JSON.stringify(audit.payload)).not.toContain(plainKey);

  const exported = await page.evaluate(async () => {
    const response = await fetch("/api/console/audit/export?limit=500", { credentials: "include" });
    return { ok: response.ok, text: await response.text(), contentType: response.headers.get("content-type") };
  });
  expect(exported.ok).toBeTruthy();
  expect(exported.contentType).toContain("text/csv");
  expect(exported.text).toContain("created_at,actor_email,action");
  expect(exported.text).not.toContain(plainKey);
  expect(exported.text).not.toContain(upstreamSecret);
  expect(exported.text).not.toContain("sk-phase7-broken-secret");

  const metrics = await page.evaluate(async () => {
    const response = await fetch("/metrics");
    return response.text();
  });
  expect(metrics).toContain("tokhub_gateway_requests_total");
  expect(metrics).toContain("tokhub_probe_runs_total");
  expect(metrics).toContain("tokhub_alert_deliveries_total");
  expect(metrics).toContain("tokhub_usage_rollups_total");

  await new Promise<void>((resolve) => upstream.close(() => resolve()));
});

test("phase 7 audit is filtered by workspace and regular users cannot access admin governance", async ({ page }) => {
  await adminLogin(page, "/admin");
  const settings = await writeJSON(page, "/api/admin/settings", "PATCH", { registrationOpen: true });
  expect(settings.ok).toBeTruthy();
  const email = `phase7-${Date.now()}@example.com`;
  const registered = await writeJSON(page, "/api/auth/register", "POST", {
    email,
    password: "admin@tokhub.local",
    name: "Phase7 User"
  });
  expect(registered.ok).toBeTruthy();
  if (registered.payload.devVerificationToken) {
    await writeJSON(page, "/api/auth/verify-email", "POST", { token: registered.payload.devVerificationToken });
  }
  const loggedIn = await writeJSON(page, "/api/auth/login", "POST", { email, password: "admin@tokhub.local" });
  expect(loggedIn.ok).toBeTruthy();

  const adminAudit = await readJSON(page, "/api/admin/audit");
  expect(adminAudit.status).toBe(403);
  const adminGovernance = await readJSON(page, "/api/admin/governance/summary");
  expect(adminGovernance.status).toBe(403);

  const consoleAudit = await readJSON(page, "/api/console/audit");
  expect(consoleAudit.ok).toBeTruthy();
  expect(JSON.stringify(consoleAudit.payload)).not.toContain("TokHub Admin");
});

function sumGatewayRollups(rows: Array<{ source: string; requests: number }>) {
  return rows.filter((row) => row.source === "gateway").reduce((sum, row) => sum + row.requests, 0);
}
