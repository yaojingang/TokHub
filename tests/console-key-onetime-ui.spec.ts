import { expect, Page, test } from "@playwright/test";
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { adminLogin } from "./helpers";

async function writeJSON(page: Page, path: string, method: string, body?: unknown, extraHeaders: Record<string, string> = {}) {
  const token = await page.evaluate(async () => {
    const response = await fetch("/api/auth/csrf", { credentials: "include" });
    return ((await response.json()) as { csrfToken: string }).csrfToken;
  });
  return page.evaluate(
    async ({ path: targetPath, method: targetMethod, body: payload, token: csrfToken, extraHeaders: headers }) => {
      const response = await fetch(targetPath, {
        method: targetMethod,
        credentials: "include",
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": csrfToken,
          ...headers
        },
        body: payload === undefined ? undefined : JSON.stringify(payload)
      });
      return { ok: response.ok, status: response.status, payload: await response.json() };
    },
    { path, method, body, token, extraHeaders }
  );
}

async function readJSON(page: Page, path: string) {
  return page.evaluate(async (targetPath) => {
    const response = await fetch(targetPath, { credentials: "include", cache: "no-store" });
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

async function cleanupOneTimeKeyFixture(page: Page, names: { keyName: string; gatewayName: string; channelName: string }) {
  const keys = await readJSON(page, "/api/console/gateway-keys");
  for (const item of keys.payload.items ?? []) {
    if (item.name === names.keyName) {
      await writeJSON(page, `/api/console/gateway-keys/${item.id}`, "DELETE", {});
    }
  }

  const gateways = await readJSON(page, "/api/console/gateways");
  for (const item of gateways.payload.items ?? []) {
    if (item.name === names.gatewayName) {
      await writeJSON(page, `/api/console/gateways/${item.id}`, "DELETE", {});
    }
  }

  const channels = await readJSON(page, "/api/me/private-channels?page=1&pageSize=200");
  for (const item of channels.payload.items ?? []) {
    if (item.name === names.channelName) {
      await writeJSON(page, `/api/me/private-channels/${item.id}`, "DELETE", {});
    }
  }
}

test("console key creation only shows the full key once", async ({ page }) => {
  await adminLogin(page, "/console/keys");
  const suffix = Date.now();
  const keyName = `UI One Time Key ${suffix}`;
  const gatewayName = `UI One Time Key Gateway ${suffix}`;
  const channelName = `UI One Time Key Channel ${suffix}`;
  const upstreamSecret = `sk-ui-one-time-key-upstream-${suffix}`;
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
            id: "chatcmpl-ui-key-probe",
            object: "chat.completion",
            model: "gpt-4o-mini",
            choices: [{ index: 0, message: { role: "assistant", content: "K" }, finish_reason: "stop" }],
            usage: { prompt_tokens: 4, completion_tokens: 1, total_tokens: 5 }
          }));
          return;
        }
        res.writeHead(200, { "Content-Type": "application/json" });
        res.end(JSON.stringify({
          id: "chatcmpl-ui-key-test",
          object: "chat.completion",
          model: "gpt-4o-mini",
          choices: [{ index: 0, message: { role: "assistant", content: "OK" }, finish_reason: "stop" }],
          usage: { prompt_tokens: 4, completion_tokens: 1, total_tokens: 5 }
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

  try {
    const privateChannel = await writeJSON(page, "/api/me/private-channels", "POST", {
      name: channelName,
      provider: "OpenAI",
      type: "openai-compatible",
      model: "gpt-4o-mini",
      endpoint: endpointRoot,
      apiKey: upstreamSecret,
      probeDaily: 5
    });
    expect(privateChannel.status).toBe(201);
    const privateProbe = await writeJSON(page, `/api/me/private-channels/${privateChannel.payload.channel.id}/probe-now`, "POST", {});
    expect(privateProbe.ok).toBeTruthy();

    const gateway = await writeJSON(page, "/api/console/gateways", "POST", {
      name: gatewayName,
      policy: "latency",
      upstreamIds: [privateChannel.payload.channel.id],
      qpsLimit: 20,
      quotaMonth: 1000
    });
    expect(gateway.status).toBe(201);

    await page.goto("/console/keys");
    const createPanel = page.locator(".key-create-panel").filter({ hasText: "签发 Gateway Key" });
    await createPanel.getByLabel("Key 名称").fill(keyName);
    await createPanel.getByLabel("关联网关").selectOption(gateway.payload.gateway.id);
    await createPanel.getByRole("button", { name: /签发 API Key/ }).click();

    const dialog = page.getByRole("dialog", { name: "API Key 已生成" });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByText("这是完整密钥。请先复制保存")).toBeVisible();

    const plainKey = (await dialog.locator(".one-time-key-value").textContent())?.trim();
    expect(plainKey).toMatch(/^sk-th-/);
    await expect(dialog.getByRole("button", { name: "复制完整 Key" })).toBeVisible();
    await dialog.getByRole("button", { name: "复制完整 Key" }).click();
    await expect(dialog.getByRole("button", { name: "已复制 ✓" })).toBeVisible();

    await dialog.getByRole("button", { name: "我已保存" }).click();
    await expect(dialog).not.toBeVisible();
    await expect(page.locator("body")).not.toContainText(plainKey!);
    await expect(page.getByText("仅创建时展示，忘记请轮换").first()).toBeVisible();

    await page.reload();
    await expect(page.locator("body")).not.toContainText(plainKey!);
    await expect(page.getByText("完整 Key 仅在创建时展示一次。忘记后请轮换或重新签发。")).toBeVisible();

    const keysAfterRefresh = await readJSON(page, "/api/console/gateway-keys");
    const createdKey = (keysAfterRefresh.payload.items as Array<{ id: string; name: string }>).find((item) => item.name === keyName);
    expect(createdKey?.id).toBeTruthy();

    const keyRow = page.locator("tr").filter({ hasText: keyName }).first();
    await expect(keyRow.getByText(gatewayName)).toBeVisible();
    await expect(keyRow.getByRole("button", { name: "查看 Key" })).toHaveCount(0);
    const revealStatus = await page.evaluate(async (keyID) => {
      const csrfResponse = await fetch("/api/auth/csrf", { credentials: "include" });
      const { csrfToken } = await csrfResponse.json() as { csrfToken: string };
      const response = await fetch(`/api/console/gateway-keys/${keyID}/reveal`, {
        method: "POST",
        credentials: "include",
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": csrfToken
        },
        body: JSON.stringify({ password: "wrong-password" })
      });
      return response.status;
    }, createdKey!.id);
    expect([404, 405]).toContain(revealStatus);

    await keyRow.getByRole("button", { name: "测试" }).click();
    await expect(keyRow.getByText(/可用 · 延迟 \d+ms/)).toBeVisible({ timeout: 15_000 });

    const rateLimitIP = `203.0.113.${suffix}`;
    for (let i = 0; i < 12; i += 1) {
      const attempt = await writeJSON(page, `/api/console/gateways/${gateway.payload.gateway.id}/debug`, "POST", {
        model: "gpt-4o-mini",
        prompt: "Reply exactly: OK",
        kind: "chat"
      }, { "X-Forwarded-For": rateLimitIP });
      expect(attempt.status).toBe(200);
    }
    const debugLimited = await writeJSON(page, `/api/console/gateways/${gateway.payload.gateway.id}/debug`, "POST", {
      model: "gpt-4o-mini",
      prompt: "Reply exactly: OK",
      kind: "chat"
    }, { "X-Forwarded-For": rateLimitIP });
    expect(debugLimited.status).toBe(429);

  } finally {
    await cleanupOneTimeKeyFixture(page, { keyName, gatewayName, channelName });
    await new Promise<void>((resolve) => upstream.close(() => resolve()));
  }
});
