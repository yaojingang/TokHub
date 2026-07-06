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

test("phase 11.2 platform channel CRUD, credential rotation, validation and visibility gates", async ({ page }) => {
  await adminLogin(page, "/admin/channels");

  const suffix = Date.now();
  const name = `Phase112 Platform ${suffix}`;
  const secret = `sk-phase112-secret-${suffix}`;
  const rotatedSecret = `sk-phase112-rotated-${suffix}`;

  const created = await writeJSON(page, "/api/admin/channels", "POST", {
    name,
    provider: "OpenAI",
    type: "openai-compatible",
    model: "gpt-4o-mini",
    upstreamModel: "gpt-4o-mini",
    endpoint: "https://ok.example/v1",
    apiKey: secret,
    probeDaily: 1440,
    publicVisible: true,
    gatewayEnabled: true,
    enabled: true,
    inputPerMtok: 0.15,
    outputPerMtok: 0.6
  });
  expect(created.ok).toBeTruthy();
  expect(JSON.stringify(created.payload)).not.toContain(secret);
  expect(created.payload.channel.keyMask).toContain("****");
  const channelID = created.payload.channel.id as string;

  const adminList = await readJSON(page, "/api/admin/channels");
  expect(JSON.stringify(adminList.payload)).toContain(name);
  expect(JSON.stringify(adminList.payload)).not.toContain(secret);

  const publicBefore = await readJSON(page, `/api/public/channels/${channelID}`);
  expect(publicBefore.ok).toBeTruthy();
  expect(publicBefore.payload.channel.id).toBe(channelID);

  const validated = await writeJSON(page, `/api/admin/channels/${channelID}/validate`, "POST", {});
  expect(validated.ok).toBeTruthy();
  expect(["healthy", "connectivity_down", "auth_error", "functional_down", "degraded", "unknown"]).toContain(validated.payload.channel.status);
  expect(JSON.stringify(validated.payload)).not.toContain(secret);

  const rotated = await writeJSON(page, `/api/admin/channels/${channelID}/credentials`, "POST", { apiKey: rotatedSecret });
  expect(rotated.ok).toBeTruthy();
  expect(JSON.stringify(rotated.payload)).not.toContain(rotatedSecret);
  expect(rotated.payload.channel.keyFingerprint).not.toBe(created.payload.channel.keyFingerprint);

  const disabled = await writeJSON(page, `/api/admin/channels/${channelID}/disable`, "POST", {});
  expect(disabled.ok).toBeTruthy();
  expect(disabled.payload.channel.status).toBe("disabled");
  expect(disabled.payload.channel.publicVisible).toBe(false);
  expect(disabled.payload.channel.gatewayEnabled).toBe(false);

  const publicAfterDisable = await readJSON(page, `/api/public/channels/${channelID}`);
  expect(publicAfterDisable.status).toBe(404);

  const gatewayData = await readJSON(page, "/api/admin/gateways");
  const upstreamIDs = (gatewayData.payload.upstreams as Array<{ channelId: string }>).map((item) => item.channelId);
  expect(upstreamIDs).not.toContain(channelID);

  const deleted = await writeJSON(page, `/api/admin/channels/${channelID}`, "DELETE", {});
  expect(deleted.ok).toBeTruthy();
  const afterDelete = await readJSON(page, "/api/admin/channels");
  expect(JSON.stringify(afterDelete.payload)).not.toContain(channelID);
});

test("phase 11.2 platform validation sends credential to real L2 and L3 upstreams", async ({ page }) => {
  await adminLogin(page, "/admin/channels");

  const secret = `sk-phase112-real-${Date.now()}`;
  const seenAuth: string[] = [];
  const upstream = createServer((req: IncomingMessage, res: ServerResponse) => {
    if (req.method === "HEAD" || req.method === "GET") {
      if (req.url === "/v1" || req.url === "/v1/") {
        res.writeHead(200);
        res.end();
        return;
      }
      if (req.url === "/v1/models") {
        seenAuth.push(`models:${req.headers.authorization ?? ""}`);
        if (req.headers.authorization !== `Bearer ${secret}`) {
          res.writeHead(401, { "Content-Type": "application/json" });
          res.end(JSON.stringify({ error: "missing bearer token" }));
          return;
        }
        res.writeHead(200, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ data: [{ id: "gpt-4o-mini" }] }));
        return;
      }
    }
    if (req.method === "POST" && req.url === "/v1/chat/completions") {
      seenAuth.push(`chat:${req.headers.authorization ?? ""}`);
      if (req.headers.authorization !== `Bearer ${secret}`) {
        res.writeHead(401, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ error: "missing bearer token" }));
        return;
      }
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({
        choices: [{ message: { content: "tokhub-ok" } }],
        usage: { prompt_tokens: 3, completion_tokens: 2, total_tokens: 5 }
      }));
      return;
    }
    res.writeHead(404, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ error: "not found" }));
  });

  await new Promise<void>((resolve) => upstream.listen(0, "0.0.0.0", resolve));
  const address = upstream.address();
  if (typeof address !== "object" || address === null) {
    throw new Error("test upstream did not bind a TCP port");
  }
  const upstreamHost = process.env.TOKHUB_TEST_UPSTREAM_HOST || "host.docker.internal";
  const endpoint = `http://${upstreamHost}:${address.port}/v1`;
  const name = `Phase112 Real Upstream ${Date.now()}`;
  let channelID = "";

  try {
    const created = await writeJSON(page, "/api/admin/channels", "POST", {
      name,
      provider: "OpenAI",
      type: "openai-compatible",
      model: "gpt-4o-mini",
      upstreamModel: "gpt-4o-mini",
      endpoint,
      apiKey: secret,
      publicVisible: false,
      gatewayEnabled: false,
      enabled: true
    });
    expect(created.ok).toBeTruthy();
    channelID = created.payload.channel.id as string;

    const validated = await writeJSON(page, `/api/admin/channels/${channelID}/validate`, "POST", {});
    expect(validated.ok).toBeTruthy();
    expect(validated.payload.channel.status).toBe("healthy");
    expect(seenAuth).toContain(`models:Bearer ${secret}`);
    expect(seenAuth).toContain(`chat:Bearer ${secret}`);
  } finally {
    if (channelID) {
      await writeJSON(page, `/api/admin/channels/${channelID}`, "DELETE", {});
    }
    await new Promise<void>((resolve) => upstream.close(() => resolve()));
  }
});

test("phase 11.2 platform channel rejects endpoint credentials", async ({ page }) => {
  await adminLogin(page, "/admin/channels");

  const created = await writeJSON(page, "/api/admin/channels", "POST", {
    name: `Phase112 Invalid Endpoint ${Date.now()}`,
    provider: "OpenAI",
    type: "openai-compatible",
    model: "gpt-4o-mini",
    upstreamModel: "gpt-4o-mini",
    endpoint: "https://user:pass@example.com/v1?token=bad#frag",
    apiKey: "sk-phase112-invalid-endpoint",
    publicVisible: true,
    gatewayEnabled: true,
    enabled: true
  });

  expect(created.status).toBe(400);
  expect(JSON.stringify(created.payload)).toContain("without credentials");
});
