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
      return { ok: response.ok, status: response.status, payload: await response.json() };
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

function readRequestBody(req: IncomingMessage) {
  return new Promise<string>((resolve) => {
    const chunks: Buffer[] = [];
    req.on("data", (chunk) => chunks.push(Buffer.from(chunk)));
    req.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
  });
}

test("phase 5 gateway create, key lifecycle, OpenAI compatible calls and usage", async ({ page }) => {
  await adminLogin(page, "/console");

  const suffix = Date.now();
  const upstreamSecret = `phase5-upstream-${suffix}`;
  const upstream = createServer((req: IncomingMessage, res: ServerResponse) => {
    void (async () => {
      const body = await readRequestBody(req);
      if (req.headers.authorization !== `Bearer ${upstreamSecret}`) {
        res.writeHead(401, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ error: "bad auth" }));
        return;
      }
      if (req.url === "/v1/models") {
        res.writeHead(200, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ object: "list", data: [{ id: "gpt-phase5", object: "model", owned_by: "phase5" }] }));
        return;
      }
      if (req.url === "/v1/chat/completions" && body.includes("\"stream\":true")) {
        res.writeHead(200, { "Content-Type": "text/event-stream" });
        res.end([
          "data: {\"id\":\"chatcmpl-phase5\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\"TokHub gateway response\"}}]}",
          "",
          "data: [DONE]",
          "",
          ""
        ].join("\n"));
        return;
      }
      if (req.url === "/v1/chat/completions") {
        if (body.includes("Reply exactly: K")) {
          res.writeHead(200, { "Content-Type": "application/json" });
          res.end(JSON.stringify({
            id: "chatcmpl-phase5-probe",
            object: "chat.completion",
            model: "gpt-phase5",
            choices: [{ index: 0, message: { role: "assistant", content: "K" }, finish_reason: "stop" }],
            usage: { prompt_tokens: 4, completion_tokens: 1, total_tokens: 5 }
          }));
          return;
        }
        res.writeHead(200, { "Content-Type": "application/json" });
        res.end(JSON.stringify({
          id: "chatcmpl-phase5",
          object: "chat.completion",
          model: "gpt-phase5",
          choices: [{ index: 0, message: { role: "assistant", content: "TokHub gateway response from phase5" }, finish_reason: "stop" }],
          usage: { prompt_tokens: 7, completion_tokens: 4, total_tokens: 11 }
        }));
        return;
      }
      if (req.url === "/v1/responses") {
        res.writeHead(200, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ id: "resp_phase5", object: "response", status: "completed", model: "gpt-phase5" }));
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
    name: `Phase5 Private ${suffix}`,
    provider: "OpenAI",
    type: "openai-compatible",
    model: "gpt-phase5",
    endpoint: endpointRoot,
    apiKey: upstreamSecret,
    probeDaily: 5
  });
  expect(privateCreated.ok).toBeTruthy();
  const privateID = privateCreated.payload.channel.id as string;
  const privateProbe = await writeJSON(page, `/api/me/private-channels/${privateID}/probe-now`, "POST", {});
  expect(privateProbe.ok).toBeTruthy();

  const gatewayData = await readJSON(page, "/api/console/gateways");
  expect(gatewayData.ok).toBeTruthy();
  const upstreams = gatewayData.payload.upstreams as Array<{ channelId: string; status: string }>;
  expect(upstreams.length).toBeGreaterThanOrEqual(2);
  expect(upstreams.map((item) => item.channelId)).toContain(privateID);

  const privateRejected = await writeJSON(page, "/api/admin/gateways", "POST", {
    name: "Private Should Fail",
    policy: "latency",
    upstreamIds: [privateID]
  });
  expect(privateRejected.status).toBe(400);

  const selected = [privateID];
  const createdGateway = await writeJSON(page, "/api/console/gateways", "POST", {
    name: `Phase5 Gateway ${suffix}`,
    policy: "latency",
    upstreamIds: selected,
    qpsLimit: 20,
    quotaMonth: 1000
  });
  expect(createdGateway.ok).toBeTruthy();
  expect(createdGateway.payload.gateway.upstreams).toHaveLength(selected.length);
  expect(JSON.stringify(createdGateway.payload.gateway)).toContain(privateID);

  const keyCreated = await writeJSON(page, "/api/console/gateway-keys", "POST", {
    gatewayId: createdGateway.payload.gateway.id,
    name: "Phase5 Test Key",
    quotaMonth: 1000,
    qpsLimit: 20
  });
  expect(keyCreated.ok).toBeTruthy();
  const plainKey = keyCreated.payload.key.plainKey as string;
  expect(plainKey).toMatch(/^sk-th-/);

  const keysAfterCreate = await readJSON(page, "/api/console/gateway-keys");
  expect(JSON.stringify(keysAfterCreate.payload)).not.toContain(plainKey);
  expect(JSON.stringify(keysAfterCreate.payload)).toContain(keyCreated.payload.key.keyMask);

  const models = await gatewayJSON(page, "/gateway/v1/models", "GET", plainKey);
  expect(models.ok).toBeTruthy();
  expect(models.payload.object).toBe("list");
  expect(models.payload.data.length).toBeGreaterThan(0);

  const chat = await gatewayJSON(page, "/gateway/v1/chat/completions", "POST", plainKey, {
    model: "gpt-phase5",
    messages: [{ role: "user", content: "ping" }]
  });
  expect(chat.ok).toBeTruthy();
  expect(chat.payload.choices[0].message.content).toContain("TokHub gateway response");

  const anthropic = await gatewayJSON(page, "/gateway/v1/messages", "POST", plainKey, {
    model: "gpt-phase5",
    max_tokens: 64,
    messages: [{ role: "user", content: [{ type: "text", text: "ping" }] }]
  });
  expect(anthropic.ok).toBeTruthy();
  expect(anthropic.payload.type).toBe("message");
  expect(anthropic.payload.content[0].text).toContain("TokHub gateway response");

  const anthropicStream = await page.evaluate(async (key) => {
    const response = await fetch("/gateway/v1/v1/messages", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-API-Key": key
      },
      body: JSON.stringify({
        model: "gpt-phase5",
        max_tokens: 64,
        stream: true,
        messages: [{ role: "user", content: "stream" }]
      })
    });
    return { ok: response.ok, text: await response.text(), contentType: response.headers.get("content-type") };
  }, plainKey);
  expect(anthropicStream.ok).toBeTruthy();
  expect(anthropicStream.contentType).toContain("text/event-stream");
  expect(anthropicStream.text).toContain("event: message_start");
  expect(anthropicStream.text).toContain("event: content_block_delta");

  const responses = await gatewayJSON(page, "/gateway/v1/responses", "POST", plainKey, {
    model: "gpt-phase5",
    input: "hello"
  });
  expect(responses.ok).toBeTruthy();
  expect(responses.payload.status).toBe("completed");

  const streamText = await page.evaluate(async (key) => {
    const response = await fetch("/gateway/v1/chat/completions", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Authorization": `Bearer ${key}`
      },
      body: JSON.stringify({ model: "gpt-phase5", stream: true, messages: [{ role: "user", content: "stream" }] })
    });
    return { ok: response.ok, text: await response.text(), contentType: response.headers.get("content-type") };
  }, plainKey);
  expect(streamText.ok).toBeTruthy();
  expect(streamText.contentType).toContain("text/event-stream");
  expect(streamText.text).toContain("data:");
  expect(streamText.text).toContain("[DONE]");

  const usage = await readJSON(page, "/api/console/usage");
  expect(usage.ok).toBeTruthy();
  expect(usage.payload.totals.requests).toBeGreaterThanOrEqual(4);
  expect(JSON.stringify(usage.payload)).toContain("Phase5 Test Key");

  const qpsKey = await writeJSON(page, "/api/console/gateway-keys", "POST", {
    gatewayId: createdGateway.payload.gateway.id,
    name: "Phase5 QPS Key",
    quotaMonth: 1000,
    qpsLimit: 1
  });
  expect(qpsKey.ok).toBeTruthy();
  const qpsPlain = qpsKey.payload.key.plainKey as string;
  const qpsResponses = await page.evaluate(async (key) => {
    return Promise.all([0, 1].map(async () => {
      const response = await fetch("/gateway/v1/models", { headers: { Authorization: `Bearer ${key}` } });
      return response.status;
    }));
  }, qpsPlain);
  expect(qpsResponses).toContain(429);

  const quotaKey = await writeJSON(page, "/api/console/gateway-keys", "POST", {
    gatewayId: createdGateway.payload.gateway.id,
    name: "Phase5 Quota Key",
    quotaMonth: 1,
    qpsLimit: 20
  });
  expect(quotaKey.ok).toBeTruthy();
  const quotaPlain = quotaKey.payload.key.plainKey as string;
  const quotaFirst = await gatewayJSON(page, "/gateway/v1/models", "GET", quotaPlain);
  expect(quotaFirst.ok).toBeTruthy();
  const quotaSecond = await gatewayJSON(page, "/gateway/v1/models", "GET", quotaPlain);
  expect(quotaSecond.status).toBe(429);

  const revoked = await writeJSON(page, `/api/console/gateway-keys/${keyCreated.payload.key.id}/revoke`, "POST", {});
  expect(revoked.ok).toBeTruthy();
  const afterRevoke = await gatewayJSON(page, "/gateway/v1/models", "GET", plainKey);
  expect(afterRevoke.status).toBe(401);

  await new Promise<void>((resolve) => upstream.close(() => resolve()));
});

test("phase 5 gateway CORS preflight allows bearer authorization header", async ({ request, baseURL }) => {
  const origin = new URL(baseURL ?? "http://localhost:8080").origin;
  const response = await request.fetch("/gateway/v1/chat/completions", {
    method: "OPTIONS",
    headers: {
      "Origin": origin,
      "Access-Control-Request-Method": "POST",
      "Access-Control-Request-Headers": "authorization,content-type"
    }
  });
  expect(response.status()).toBe(204);
  expect((response.headers()["access-control-allow-headers"] ?? "").toLowerCase()).toContain("authorization");
});

test("phase 5 admin pages render", async ({ page }) => {
  await adminLogin(page, "/console");
  await page.goto("/console/gateways");
  await expect(page.getByRole("heading", { name: "一键生成工作区专属中转站" })).toBeVisible();
  await page.goto("/console/members");
  await expect(page.getByRole("heading", { name: "成员与密钥" })).toBeVisible();
  await expect(page.getByText("签发 Gateway Key")).toBeVisible();
  await page.goto("/console/usage");
  await expect(page.getByRole("heading", { name: "用量数据" })).toBeVisible();

  await adminLogin(page, "/admin/gateways");
  await page.goto("/admin/gateways");
  await expect(page.getByRole("heading", { name: "企业网关治理" })).toBeVisible();
  await page.goto("/admin/members");
  await expect(page.getByRole("heading", { name: "成员与密钥" })).toBeVisible();
  await page.goto("/admin/usage");
  await expect(page.getByRole("heading", { name: "用量数据" })).toBeVisible();
});

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
