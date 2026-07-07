import { expect, Page, test } from "@playwright/test";
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { adminLogin } from "./helpers";

test.describe.configure({ mode: "serial" });

type UpstreamCall = {
  method: string;
  url: string;
  auth: string;
  body: string;
};

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
    const response = await fetch(targetPath, { credentials: "include", cache: "no-store" });
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

test("phase 11.4 real gateway passthrough, failover, SSE and usage events", async ({ page }) => {
  await adminLogin(page, "/admin/gateways");

  const suffix = Date.now();
  const okSecret = `sk-phase114-ok-${suffix}`;
  const failSecret = `sk-phase114-fail-${suffix}`;
  const okName = `Phase114 Real OK ${suffix}`;
  const failName = `Phase114 Real Fail ${suffix}`;
  const gatewayName = `Phase114 Real Gateway ${suffix}`;
  const keyName = `Phase114 Real Key ${suffix}`;
  const calls: UpstreamCall[] = [];

  const upstream = createServer((req: IncomingMessage, res: ServerResponse) => {
    void (async () => {
      const body = await readRequestBody(req);
      calls.push({ method: req.method ?? "", url: req.url ?? "", auth: req.headers.authorization ?? "", body });

      if (req.url === "/fail/v1/models") {
        if (req.headers.authorization !== `Bearer ${failSecret}`) {
          res.writeHead(401, { "Content-Type": "application/json" });
          res.end(JSON.stringify({ error: "bad auth" }));
          return;
        }
        res.writeHead(200, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ object: "list", data: [{ id: "gpt-4o-mini", object: "model", owned_by: "test-upstream" }] }));
        return;
      }

      if (req.url === "/fail/v1/chat/completions" && body.includes("Reply exactly: K")) {
        if (req.headers.authorization !== `Bearer ${failSecret}`) {
          res.writeHead(401, { "Content-Type": "application/json" });
          res.end(JSON.stringify({ error: "bad auth" }));
          return;
        }
        res.writeHead(200, { "Content-Type": "application/json" });
        res.end(JSON.stringify({
          id: "chatcmpl-real-fail-probe",
          object: "chat.completion",
          model: "gpt-4o-mini",
          choices: [{ index: 0, message: { role: "assistant", content: "K" }, finish_reason: "stop" }],
          usage: { prompt_tokens: 4, completion_tokens: 1, total_tokens: 5 }
        }));
        return;
      }

      if (req.url === "/fail/v1/chat/completions") {
        res.writeHead(503, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ error: { type: "unavailable", message: "temporary failure" } }));
        return;
      }

      if (req.url === "/ok/v1/models") {
        if (req.headers.authorization !== `Bearer ${okSecret}`) {
          res.writeHead(401, { "Content-Type": "application/json" });
          res.end(JSON.stringify({ error: "bad auth" }));
          return;
        }
        res.writeHead(200, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ object: "list", data: [{ id: "gpt-real", object: "model", owned_by: "test-upstream" }] }));
        return;
      }

      if (req.url === "/ok/v1/chat/completions") {
        if (req.headers.authorization !== `Bearer ${okSecret}`) {
          res.writeHead(401, { "Content-Type": "application/json" });
          res.end(JSON.stringify({ error: "bad auth" }));
          return;
        }
        if (body.includes("Reply exactly: K")) {
          res.writeHead(200, { "Content-Type": "application/json" });
          res.end(JSON.stringify({
            id: "chatcmpl-real-ok-probe",
            object: "chat.completion",
            model: "gpt-4o-mini",
            choices: [{ index: 0, message: { role: "assistant", content: "K" }, finish_reason: "stop" }],
            usage: { prompt_tokens: 4, completion_tokens: 1, total_tokens: 5 }
          }));
          return;
        }
        if (body.includes('"stream":true')) {
          res.writeHead(200, { "Content-Type": "text/event-stream" });
          res.write(`data: ${JSON.stringify({ choices: [{ delta: { content: "real-stream-1" } }] })}\n\n`);
          res.write(`data: ${JSON.stringify({ choices: [{ delta: { content: "real-stream-2" } }] })}\n\n`);
          res.end("data: [DONE]\n\n");
          return;
        }
        res.writeHead(200, { "Content-Type": "application/json" });
        res.end(JSON.stringify({
          id: "chatcmpl-real",
          object: "chat.completion",
          model: "gpt-4o-mini",
          choices: [{ index: 0, message: { role: "assistant", content: "real-upstream-ok" }, finish_reason: "stop" }],
          usage: { prompt_tokens: 17, completion_tokens: 25, total_tokens: 42 }
        }));
        return;
      }

      if (req.url === "/ok/v1/responses") {
        if (req.headers.authorization !== `Bearer ${okSecret}`) {
          res.writeHead(401, { "Content-Type": "application/json" });
          res.end(JSON.stringify({ error: "bad auth" }));
          return;
        }
        res.writeHead(200, { "Content-Type": "application/json" });
        res.end(JSON.stringify({
          id: "resp-real",
          object: "response",
          model: "gpt-4o-mini",
          status: "completed",
          output: [{ type: "message", role: "assistant", content: [{ type: "output_text", text: "real-response-ok" }] }],
          usage: { input_tokens: 11, output_tokens: 13, total_tokens: 24 }
        }));
        return;
      }

      res.writeHead(404, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "not found" }));
    })();
  });

  await new Promise<void>((resolve) => upstream.listen(0, "0.0.0.0", resolve));
  const address = upstream.address();
  if (typeof address !== "object" || address === null) {
    throw new Error("test upstream did not bind a TCP port");
  }
  const upstreamHost = process.env.TOKHUB_TEST_UPSTREAM_HOST || "host.docker.internal";
  const base = `http://${upstreamHost}:${address.port}`;
  const channelIDs: string[] = [];

  try {
    const failChannel = await writeJSON(page, "/api/admin/channels", "POST", {
      name: failName,
      provider: "OpenAI",
      type: "openai-compatible",
      model: "gpt-4o-mini",
      upstreamModel: "gpt-4o-mini",
      endpoint: `${base}/fail/v1`,
      apiKey: failSecret,
      publicVisible: false,
      gatewayEnabled: true,
      enabled: true
    });
    expect(failChannel.ok).toBeTruthy();
    channelIDs.push(failChannel.payload.channel.id as string);

    const okChannel = await writeJSON(page, "/api/admin/channels", "POST", {
      name: okName,
      provider: "OpenAI",
      type: "openai-compatible",
      model: "gpt-4o-mini",
      upstreamModel: "gpt-4o-mini",
      endpoint: `${base}/ok/v1`,
      apiKey: okSecret,
      publicVisible: false,
      gatewayEnabled: true,
      enabled: true
    });
    expect(okChannel.ok).toBeTruthy();
    channelIDs.push(okChannel.payload.channel.id as string);

    for (const channelID of channelIDs) {
      const probe = await writeJSON(page, `/api/admin/channels/${channelID}/probe-now`, "POST", {});
      expect(probe.ok).toBeTruthy();
    }

    const gateway = await writeJSON(page, "/api/admin/gateways", "POST", {
      name: gatewayName,
      policy: "latency",
      upstreamIds: channelIDs,
      qpsLimit: 20,
      quotaMonth: 1000
    });
    expect(gateway.ok).toBeTruthy();

    const key = await writeJSON(page, "/api/admin/gateway-keys", "POST", {
      gatewayId: gateway.payload.gateway.id,
      name: keyName,
      quotaMonth: 1000,
      qpsLimit: 20
    });
    expect(key.ok).toBeTruthy();
    const plainKey = key.payload.key.plainKey as string;

    const chat = await gatewayJSON(page, "/gateway/v1/chat/completions", "POST", plainKey, {
      model: "gpt-4o-mini",
      messages: [{ role: "user", content: "real ping" }]
    });
    expect(chat.ok).toBeTruthy();
    expect(chat.payload.choices[0].message.content).toBe("real-upstream-ok");
    expect(JSON.stringify(chat.payload)).not.toContain("TokHub gateway response");
    expect(chat.payload.usage.total_tokens).toBe(42);
    expect(calls.some((call) => call.url === "/fail/v1/chat/completions")).toBeTruthy();
    expect(calls.some((call) => call.url === "/ok/v1/chat/completions" && call.auth === `Bearer ${okSecret}`)).toBeTruthy();

    const usageAfterChat = await readJSON(page, "/api/admin/usage");
    expect(usageAfterChat.ok).toBeTruthy();
    expect(usageAfterChat.payload.recent.some((event: { gateway: string; keyName: string; channel: string; tokens: number; estimated: boolean }) =>
      event.gateway === gatewayName && event.keyName === keyName && event.channel === okName && event.tokens === 42 && event.estimated === false
    )).toBeTruthy();

    const models = await gatewayJSON(page, "/gateway/v1/models", "GET", plainKey);
    expect(models.ok).toBeTruthy();
    expect(models.payload.data.map((item: { id: string }) => item.id)).toContain("gpt-real");

    const response = await gatewayJSON(page, "/gateway/v1/responses", "POST", plainKey, {
      model: "gpt-4o-mini",
      input: "real response"
    });
    expect(response.ok).toBeTruthy();
    expect(response.payload.status).toBe("completed");
    expect(JSON.stringify(response.payload)).toContain("real-response-ok");

    const stream = await page.evaluate(async (apiKey) => {
      const response = await fetch("/gateway/v1/chat/completions", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "Authorization": `Bearer ${apiKey}`
        },
        body: JSON.stringify({ model: "gpt-4o-mini", stream: true, messages: [{ role: "user", content: "stream" }] })
      });
      return { ok: response.ok, text: await response.text(), contentType: response.headers.get("content-type") };
    }, plainKey);
    expect(stream.ok).toBeTruthy();
    expect(stream.contentType).toContain("text/event-stream");
    expect(stream.text).toContain("real-stream-1");
    expect(stream.text).toContain("real-stream-2");
    expect(stream.text).toContain("[DONE]");

    const usageAfterStream = await readJSON(page, "/api/admin/usage");
    expect(usageAfterStream.ok).toBeTruthy();
    expect(usageAfterStream.payload.recent.some((event: { gateway: string; keyName: string; channel: string; tokens: number; statusCode: number; stream: boolean; estimated: boolean }) =>
      event.gateway === gatewayName &&
      event.keyName === keyName &&
      event.channel === okName &&
      event.tokens > 0 &&
      event.statusCode >= 200 &&
      event.statusCode < 300 &&
      event.stream === true &&
      event.estimated === true
    )).toBeTruthy();
  } finally {
    for (const id of channelIDs) {
      await writeJSON(page, `/api/admin/channels/${id}`, "DELETE", {});
    }
    await new Promise<void>((resolve) => upstream.close(() => resolve()));
  }
});
