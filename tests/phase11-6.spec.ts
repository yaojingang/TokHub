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

async function openAPI(page: Page, path: string, key: string) {
  return page.evaluate(
    async ({ targetPath, siteKey }) => {
      const response = await fetch(targetPath, { headers: { "X-Site-Key": siteKey } });
      return { ok: response.ok, status: response.status, payload: await response.json() };
    },
    { targetPath: path, siteKey: key }
  );
}

function readRequestBody(req: IncomingMessage) {
  return new Promise<string>((resolve) => {
    const chunks: Buffer[] = [];
    req.on("data", (chunk) => chunks.push(Buffer.from(chunk)));
    req.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
  });
}

test("phase 11.6 admin users, orgs, Open API governance, notifications and recommendation empty state", async ({ page }) => {
  await adminLogin(page, "/admin");
  await writeJSON(page, "/api/admin/settings", "PATCH", { registrationOpen: true });

  const suffix = Date.now();
  const email = `phase116-user-${suffix}@example.com`;
  const registered = await writeJSON(page, "/api/auth/register", "POST", {
    email,
    password: "ChangeMe123!",
    name: "Phase116 User"
  });
  expect(registered.ok).toBeTruthy();
  if (registered.payload.devVerificationToken) {
    await writeJSON(page, "/api/auth/verify-email", "POST", { token: registered.payload.devVerificationToken });
  }
  await adminLogin(page, "/admin");

  const users = await readJSON(page, "/api/admin/users");
  expect(users.ok).toBeTruthy();
  const user = (users.payload.items as Array<{ id: string; email: string; status: string; emailVerified: boolean }>).find((item) => item.email === email);
  expect(user?.emailVerified).toBeTruthy();
  const suspended = await writeJSON(page, `/api/admin/users/${user?.id}/status`, "PATCH", { status: "suspended" });
  expect(suspended.ok).toBeTruthy();
  expect(suspended.payload.user.status).toBe("suspended");
  const invalidUserStatus = await writeJSON(page, `/api/admin/users/${user?.id}/status`, "PATCH", { status: "enabled" });
  expect(invalidUserStatus.ok).toBeFalsy();
  expect(invalidUserStatus.status).toBe(400);
  const blockedLogin = await writeJSON(page, "/api/auth/login", "POST", { email, password: "ChangeMe123!" });
  expect(blockedLogin.ok).toBeFalsy();
  await adminLogin(page, "/admin");
  const reactivated = await writeJSON(page, `/api/admin/users/${user?.id}/status`, "PATCH", { status: "active" });
  expect(reactivated.ok).toBeTruthy();

  const orgs = await readJSON(page, "/api/admin/orgs");
  expect(orgs.ok).toBeTruthy();
  expect(orgs.payload.items.some((item: { id: string }) => item.id === "org_default")).toBeTruthy();
  const protectedOrg = await writeJSON(page, "/api/admin/orgs/org_default/status", "PATCH", { status: "suspended" });
  expect(protectedOrg.ok).toBeFalsy();
  expect(protectedOrg.status).toBe(400);
  const invalidOrgStatus = await writeJSON(page, "/api/admin/orgs/org_default/status", "PATCH", { status: "enabled" });
  expect(invalidOrgStatus.ok).toBeFalsy();
  expect(invalidOrgStatus.status).toBe(400);

  const invalidSite = await writeJSON(page, "/api/admin/open-api/sites", "POST", {
    name: `Phase116 Invalid Site ${suffix}`,
    scopes: ["admin"],
    qpsLimit: 30
  });
  expect(invalidSite.ok).toBeFalsy();
  expect(invalidSite.status).toBe(400);

  const createdSite = await writeJSON(page, "/api/admin/open-api/sites", "POST", {
    name: `Phase116 Site ${suffix}`,
    scopes: ["overview", "incidents"],
    qpsLimit: 30
  });
  expect(createdSite.status).toBe(201);
  const siteID = createdSite.payload.site.id as string;
  const siteKey = createdSite.payload.site.plainKey as string;
  expect((await openAPI(page, "/v1/status/overview", siteKey)).ok).toBeTruthy();

  const pausedSite = await writeJSON(page, `/api/admin/open-api/sites/${siteID}`, "PATCH", {
    name: `Phase116 Site ${suffix}`,
    scopes: ["overview"],
    qpsLimit: 10,
    status: "paused"
  });
  expect(pausedSite.ok).toBeTruthy();
  expect((await openAPI(page, "/v1/status/overview", siteKey)).status).toBe(403);
  const invalidSiteStatus = await writeJSON(page, `/api/admin/open-api/sites/${siteID}`, "PATCH", {
    status: "enabled"
  });
  expect(invalidSiteStatus.ok).toBeFalsy();
  expect(invalidSiteStatus.status).toBe(400);
  expect((await openAPI(page, "/v1/status/overview", siteKey)).status).toBe(403);

  const activeSite = await writeJSON(page, `/api/admin/open-api/sites/${siteID}`, "PATCH", {
    name: `Phase116 Site ${suffix}`,
    scopes: ["incidents"],
    qpsLimit: 10,
    status: "active"
  });
  expect(activeSite.ok).toBeTruthy();
  expect((await openAPI(page, "/v1/status/overview", siteKey)).status).toBe(403);
  expect((await openAPI(page, "/v1/status/incidents", siteKey)).ok).toBeTruthy();
  const invalidSiteScope = await writeJSON(page, `/api/admin/open-api/sites/${siteID}`, "PATCH", {
    scopes: ["admin"],
    status: "active"
  });
  expect(invalidSiteScope.ok).toBeFalsy();
  expect(invalidSiteScope.status).toBe(400);
  expect((await openAPI(page, "/v1/status/overview", siteKey)).status).toBe(403);
  expect((await openAPI(page, "/v1/status/incidents", siteKey)).ok).toBeTruthy();
  const revoked = await writeJSON(page, `/api/admin/open-api/sites/${siteID}/revoke`, "POST", {});
  expect(revoked.payload.site.status).toBe("revoked");
  expect((await openAPI(page, "/v1/status/incidents", siteKey)).status).toBe(403);

  const received: string[] = [];
  const webhook = createServer((req: IncomingMessage, res: ServerResponse) => {
    void (async () => {
      received.push(`${req.method ?? ""} ${req.url ?? ""} ${await readRequestBody(req)}`);
      res.writeHead(204);
      res.end();
    })();
  });
  await new Promise<void>((resolve) => webhook.listen(0, "0.0.0.0", resolve));
  const address = webhook.address();
  if (typeof address !== "object" || address === null) throw new Error("webhook failed to bind");
  const webhookHost = process.env.TOKHUB_TEST_UPSTREAM_HOST || "host.docker.internal";
  try {
    const channel = await writeJSON(page, "/api/admin/alerts/channels", "POST", {
      name: `Phase116 Webhook ${suffix}`,
      type: "webhook",
      target: `http://${webhookHost}:${address.port}/hook`,
      enabled: true
    });
    expect(channel.ok).toBeTruthy();
    const testDelivery = await writeJSON(page, `/api/admin/alerts/channels/${channel.payload.channel.id}/test`, "POST", {});
    expect(testDelivery.ok).toBeTruthy();
    expect(testDelivery.payload.delivery.status).toBe("test");
    await expect.poll(() => received.length).toBeGreaterThan(0);
    expect(received.join("\n")).toContain("测试通知");
  } finally {
    await new Promise<void>((resolve) => webhook.close(() => resolve()));
  }

  const health = await readJSON(page, "/api/admin/production-health");
  expect(health.ok).toBeTruthy();
  expect(health.payload.checks.some((item: { id: string }) => item.id === "seed_mode")).toBeTruthy();

  const originalRecommend = await readJSON(page, "/api/admin/recommend");
  expect(originalRecommend.ok).toBeTruthy();
  try {
    const emptied = await writeJSON(page, "/api/admin/recommend", "PUT", { picks: [], rewards: [], scenarios: [] });
    expect(emptied.ok).toBeTruthy();
    const publicRecommend = await readJSON(page, "/api/public/recommend");
    expect(publicRecommend.payload.picks).toHaveLength(0);
    expect(publicRecommend.payload.rewards).toHaveLength(0);
    expect(publicRecommend.payload.scenarios).toHaveLength(0);
    await page.goto("/recommend");
    await expect(page.getByText("暂无精选推荐")).toBeVisible();
    await expect(page.getByText("暂无福利配置")).toBeVisible();
    await page.goto("/admin/recommend");
    await expect(page.getByText("暂无推荐位")).toBeVisible();
  } finally {
    await writeJSON(page, "/api/admin/recommend", "PUT", {
      picks: originalRecommend.payload.picks,
      rewards: originalRecommend.payload.rewards,
      scenarios: originalRecommend.payload.scenarios
    });
  }
});
