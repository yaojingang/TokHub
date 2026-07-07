import { expect, Page, test } from "@playwright/test";
import { adminLogin, waitForPath } from "./helpers";

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

test("phase 6.5 splits public frontend, user console and platform admin", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("contentinfo").getByRole("link", { name: "平台管理" })).toHaveCount(0);

  await adminLogin(page, "/console");
  await expect(page.getByRole("heading", { name: /用户控制台/ })).toBeVisible();
  await expect(page.getByRole("banner").getByRole("link", { name: "平台管理" })).toHaveCount(0);

  await page.goto("/admin");
  await expect(page.getByRole("heading", { name: "平台总览" })).toBeVisible();
  await expect(page.getByRole("link", { name: "专属网关" })).toHaveCount(0);
  await expect(page.getByRole("link", { name: "成员密钥" })).toHaveCount(0);

  await page.goto("/admin/gateways");
  await waitForPath(page, "/admin/gateways");
  await expect(page.getByRole("heading", { name: "企业网关治理" })).toBeVisible();

  await page.goto("/admin/members");
  await waitForPath(page, "/admin/members");
  await expect(page.getByRole("heading", { name: "成员与密钥" })).toBeVisible();

  await page.goto("/dashboard");
  await waitForPath(page, "/dashboard");
  await expect(page.getByRole("heading", { name: "监控总览" })).toBeVisible();
});

test("phase 6.5 regular user stays in console and cannot access platform admin", async ({ page }) => {
  await adminLogin(page, "/admin/users");
  const email = `phase65-${Date.now()}@example.com`;
  const createdUser = await writeJSON(page, "/api/admin/users", "POST", {
    email,
    password: "admin@tokhub.local",
    name: "Phase65 User",
    role: "user",
    plan: "free",
    status: "active",
    emailVerified: true,
    dataOrigin: "test"
});
  expect(createdUser.status).toBe(201);

  const loggedIn = await writeJSON(page, "/api/auth/login", "POST", {
    email,
    password: "admin@tokhub.local"
  });
  expect(loggedIn.ok).toBeTruthy();

  await page.goto("/console");
  await expect(page.getByRole("heading", { name: "控制台首页" })).toBeVisible();
  await expect(page.locator(".org-pick")).toContainText("Phase65 User");
  await expect(page.getByRole("link", { name: "平台管理" })).toHaveCount(0);

  const adminAPI = await readJSON(page, "/api/admin/gateways");
  expect(adminAPI.status).toBe(403);

  await page.goto("/admin");
  await waitForPath(page, "/console");
  await expect(page.getByRole("heading", { name: "控制台首页" })).toBeVisible();
  await expect(page.locator(".org-pick")).toContainText("Phase65 User");
});

test("phase 6.5 workspace gateway uses private channels by default and platform channels only for Super VIP", async ({ page }) => {
  await adminLogin(page, "/admin/users");
  const suffix = Date.now();
  const email = `phase65-gateway-${suffix}@example.com`;
  const createdUser = await writeJSON(page, "/api/admin/users", "POST", {
    email,
    password: "admin@tokhub.local",
    name: "Phase65 Gateway User",
    role: "user",
    plan: "free",
    status: "active",
    emailVerified: true,
    dataOrigin: "test"
  });
  expect(createdUser.status).toBe(201);

  const loggedIn = await writeJSON(page, "/api/auth/login", "POST", {
    email,
    password: "admin@tokhub.local"
  });
  expect(loggedIn.ok).toBeTruthy();

  const freeConsoleData = await readJSON(page, "/api/console/gateways");
  expect(freeConsoleData.ok).toBeTruthy();
  expect(freeConsoleData.payload.allowPlatformUpstreams).toBeFalsy();
  expect((freeConsoleData.payload.upstreams as Array<{ ownerType: string }>).every((item) => item.ownerType !== "platform")).toBeTruthy();

  const createdPrivate = await writeJSON(page, "/api/me/private-channels", "POST", {
    name: `Phase65 Private ${suffix}`,
    provider: "OpenAI",
    type: "openai-compatible",
    model: "gpt-4o-mini",
    endpoint: "https://phase65.example/v1",
    apiKey: "sk-phase65-private-secret",
    probeDaily: 5
  });
  expect(createdPrivate.ok).toBeTruthy();
  const privateID = createdPrivate.payload.channel.id as string;
  const privateProbe = await writeJSON(page, `/api/me/private-channels/${privateID}/probe-now`, "POST", {});
  expect(privateProbe.ok).toBeTruthy();

  const consoleData = await readJSON(page, "/api/console/gateways");
  expect(consoleData.ok).toBeTruthy();
  expect(consoleData.payload.upstreams.map((item: { channelId: string }) => item.channelId)).toContain(privateID);
  expect((consoleData.payload.upstreams as Array<{ ownerType: string }>).every((item) => item.ownerType !== "platform")).toBeTruthy();

  await adminLogin(page, "/admin/gateways");
  const adminData = await readJSON(page, "/api/admin/gateways");
  const platformUpstream = (adminData.payload.upstreams as Array<{ channelId: string; ownerType: string }>).find((item) => item.ownerType === "platform");
  expect(platformUpstream?.channelId).toBeTruthy();

  const adminRejected = await writeJSON(page, "/api/admin/gateways", "POST", {
    name: "Phase65 Wrong Boundary",
    policy: "latency",
    upstreamIds: [privateID]
  });
  expect(adminRejected.status).toBe(400);

  const reloggedFree = await writeJSON(page, "/api/auth/login", "POST", {
    email,
    password: "admin@tokhub.local"
  });
  expect(reloggedFree.ok).toBeTruthy();

  const platformRejected = await writeJSON(page, "/api/console/gateways", "POST", {
    name: "Phase65 Platform Rejected",
    policy: "latency",
    upstreamIds: [platformUpstream!.channelId],
    qpsLimit: 10,
    quotaMonth: 100
  });
  expect(platformRejected.status).toBe(400);
  expect(platformRejected.payload.error.message).toContain("Super VIP");

  const consoleCreated = await writeJSON(page, "/api/console/gateways", "POST", {
    name: "Phase65 Workspace Gateway",
    policy: "latency",
    upstreamIds: [privateID],
    qpsLimit: 10,
    quotaMonth: 100
  });
  expect(consoleCreated.ok).toBeTruthy();
  expect(JSON.stringify(consoleCreated.payload.gateway)).toContain(privateID);

  await adminLogin(page, "/admin/users");
  const promoted = await writeJSON(page, `/api/admin/users/${createdUser.payload.user.id}`, "PATCH", { plan: "super_vip" });
  expect(promoted.ok).toBeTruthy();
  expect(promoted.payload.user.plan).toBe("super_vip");

  const reloggedVIP = await writeJSON(page, "/api/auth/login", "POST", {
    email,
    password: "admin@tokhub.local"
  });
  expect(reloggedVIP.ok).toBeTruthy();
  const vipConsoleData = await readJSON(page, "/api/console/gateways");
  expect(vipConsoleData.payload.allowPlatformUpstreams).toBeTruthy();
  expect(vipConsoleData.payload.upstreams.map((item: { channelId: string }) => item.channelId)).toContain(platformUpstream!.channelId);

  const platformCreated = await writeJSON(page, "/api/console/gateways", "POST", {
    name: "Phase65 Super VIP Platform Gateway",
    policy: "latency",
    upstreamIds: [platformUpstream!.channelId],
    qpsLimit: 10,
    quotaMonth: 100
  });
  expect(platformCreated.status).toBe(201);
  expect(JSON.stringify(platformCreated.payload.gateway)).toContain(platformUpstream!.channelId);

  await adminLogin(page, "/admin/users");
  const demoted = await writeJSON(page, `/api/admin/users/${createdUser.payload.user.id}`, "PATCH", { plan: "free" });
  expect(demoted.ok).toBeTruthy();
  expect(demoted.payload.user.plan).toBe("free");

  const reloggedDemoted = await writeJSON(page, "/api/auth/login", "POST", {
    email,
    password: "admin@tokhub.local"
  });
  expect(reloggedDemoted.ok).toBeTruthy();
  const demotedConsoleData = await readJSON(page, "/api/console/gateways");
  expect(demotedConsoleData.payload.allowPlatformUpstreams).toBeFalsy();
  expect((demotedConsoleData.payload.upstreams as Array<{ ownerType: string }>).every((item) => item.ownerType !== "platform")).toBeTruthy();
  const demotedGateways = demotedConsoleData.payload.items as Array<{ name: string; status: string; upstreams: Array<{ ownerType: string; channelId: string }> }>;
  expect(demotedGateways.flatMap((item) => item.upstreams).every((item) => item.ownerType !== "platform")).toBeTruthy();
  const strippedGateway = demotedGateways.find((item) => item.name === "Phase65 Super VIP Platform Gateway");
  expect(strippedGateway?.status).toBe("paused");
  expect(strippedGateway?.upstreams ?? []).toHaveLength(0);
});
