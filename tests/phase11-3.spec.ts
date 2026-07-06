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
    const response = await fetch(targetPath, { credentials: "include", cache: "no-store" });
    return { ok: response.ok, status: response.status, payload: await response.json() };
  }, path);
}

test("phase 11.3 public detail and series use real probe data without synthetic history", async ({ page }) => {
  await adminLogin(page, "/admin/channels");

  const secret = `sk-phase113-real-${Date.now()}`;
  const upstream = createServer((req: IncomingMessage, res: ServerResponse) => {
    if ((req.method === "HEAD" || req.method === "GET") && (req.url === "/v1" || req.url === "/v1/")) {
      res.writeHead(200);
      res.end();
      return;
    }
    if (req.method === "GET" && req.url === "/v1/models") {
      if (req.headers.authorization !== `Bearer ${secret}`) {
        res.writeHead(401, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ error: "missing bearer token" }));
        return;
      }
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ data: [{ id: "gpt-4o-mini" }] }));
      return;
    }
    if (req.method === "POST" && req.url === "/v1/chat/completions") {
      if (req.headers.authorization !== `Bearer ${secret}`) {
        res.writeHead(401, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ error: "missing bearer token" }));
        return;
      }
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({
        choices: [{ message: { content: "tokhub-ok" } }],
        usage: { prompt_tokens: 6000, completion_tokens: 6000, total_tokens: 12000 }
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
  let channelID = "";

  try {
    const created = await writeJSON(page, "/api/admin/channels", "POST", {
      name: `Phase113 Real Aggregation ${Date.now()}`,
      provider: "OpenAI",
      type: "openai-compatible",
      model: "gpt-4o-mini",
      upstreamModel: "gpt-4o-mini",
      endpoint,
      apiKey: secret,
      publicVisible: true,
      gatewayEnabled: false,
      enabled: true
    });
    expect(created.ok).toBeTruthy();
    channelID = created.payload.channel.id as string;

    const validated = await writeJSON(page, `/api/admin/channels/${channelID}/validate`, "POST", {});
    expect(validated.ok).toBeTruthy();
    expect(validated.payload.channel.status).toBe("healthy");

    const detail = await readJSON(page, `/api/public/channels/${channelID}`);
    expect(detail.ok).toBeTruthy();
    expect(detail.payload.channel.id).toBe(channelID);
    expect(detail.payload.recentRecords.map((item: { type: string }) => item.type)).toEqual(expect.arrayContaining(["models", "generate"]));
    expect(detail.payload.errors).toEqual([]);
    expect(detail.payload.costs.slice(0, -1).every((item: { costUsd: number; tokens: number }) => item.costUsd === 0 && item.tokens === 0)).toBeTruthy();
    expect(detail.payload.costs.at(-1).tokens).toBeGreaterThanOrEqual(12000);
    expect(detail.payload.l3.averageTokens).toBeGreaterThanOrEqual(12000);
    expect(detail.payload.l3.contentValidRate).toBe(100);

    const series = await readJSON(page, `/api/public/channels/${channelID}/series?days=7`);
    expect(series.ok).toBeTruthy();
    expect(series.payload.items).toHaveLength(7);
    expect(series.payload.items.slice(0, -1).every((item: { probeCount: number; costUsd: number }) => item.probeCount === 0 && item.costUsd === 0)).toBeTruthy();
    expect(series.payload.items.at(-1).probeCount).toBeGreaterThanOrEqual(3);
    expect(series.payload.items.at(-1).costUsd).toBeGreaterThan(0);

    const overview = await readJSON(page, "/api/public/overview");
    expect(overview.ok).toBeTruthy();
    expect(overview.payload.probeTokensToday).toBeGreaterThanOrEqual(12000);
  } finally {
    if (channelID) {
      await writeJSON(page, `/api/admin/channels/${channelID}`, "DELETE", {});
    }
    await new Promise<void>((resolve) => upstream.close(() => resolve()));
  }
});

test("phase 11.3 public aggregation rejects hidden platform channels", async ({ page }) => {
  await adminLogin(page, "/admin/channels");

  let channelID = "";
  try {
    const created = await writeJSON(page, "/api/admin/channels", "POST", {
      name: `Phase113 Hidden Aggregation ${Date.now()}`,
      provider: "OpenAI",
      type: "openai-compatible",
      model: "gpt-4o-mini",
      upstreamModel: "gpt-4o-mini",
      endpoint: "https://hidden-phase113.example/v1",
      apiKey: "sk-phase113-hidden",
      publicVisible: false,
      gatewayEnabled: false,
      enabled: true
    });
    expect(created.ok).toBeTruthy();
    channelID = created.payload.channel.id as string;

    const detail = await readJSON(page, `/api/public/channels/${channelID}`);
    expect(detail.status).toBe(404);

    const series = await readJSON(page, `/api/public/channels/${channelID}/series?days=7`);
    expect(series.status).toBe(404);
  } finally {
    if (channelID) {
      await writeJSON(page, `/api/admin/channels/${channelID}`, "DELETE", {});
    }
  }
});
