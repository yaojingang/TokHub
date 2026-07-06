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

async function setRegistration(page: Page, open: boolean) {
  const response = await writeJSON(page, "/api/admin/settings", "PATCH", { registrationOpen: open });
  expect(response.ok).toBeTruthy();
}

test("phase 4 registration switch and registration flow", async ({ page }) => {
  await adminLogin(page);
  await setRegistration(page, false);
  try {
    await page.goto("/login");
    await expect(page.getByRole("button", { name: "注册新账号" })).toHaveCount(0);
    const closedRegister = await writeJSON(page, "/api/auth/register", "POST", {
      email: `closed-${Date.now()}@example.com`,
      password: "ChangeMe123!",
      name: "Closed"
    });
    expect(closedRegister.ok).toBeFalsy();
    expect(closedRegister.status).toBe(403);
  } finally {
    await adminLogin(page, "/admin");
    await setRegistration(page, true);
  }

  await page.goto("/login");
  await page.getByRole("button", { name: "注册新账号" }).click();
  const email = `phase4-${Date.now()}@example.com`;
  await page.getByLabel("邮箱").fill(email);
  await page.getByLabel("设置密码").fill("ChangeMe123!");
  await page.getByRole("button", { name: "创建账号并进入控制台 →" }).click();
  const verificationToken = page.getByText("邮箱验证令牌", { exact: true });
  if (await verificationToken.isVisible({ timeout: 2_000 }).catch(() => false)) {
    await page.getByRole("button", { name: "完成邮箱验证" }).click();
  }
  await waitForPath(page, "/console");
  await expect(page.getByRole("heading", { name: `${email.split("@")[0]} 的用户控制台` })).toBeVisible();
});

test("phase 4 favorites, private channels, quota and key masking", async ({ page }) => {
  await adminLogin(page, "/admin");
  await setRegistration(page, true);

  await page.goto("/");
  const email = `phase4-flow-${Date.now()}@example.com`;
  const registered = await writeJSON(page, "/api/auth/register", "POST", {
    email,
    password: "ChangeMe123!",
    name: "Phase4 Flow"
  });
  expect(registered.ok).toBeTruthy();
  const blockedLogin = await writeJSON(page, "/api/auth/login", "POST", {
    email,
    password: "ChangeMe123!"
  });
  if (registered.payload.verificationRequired) {
    expect(blockedLogin.status).toBe(403);
    await writeJSON(page, "/api/auth/verify-email", "POST", { token: registered.payload.devVerificationToken });
    const loggedIn = await writeJSON(page, "/api/auth/login", "POST", {
      email,
      password: "ChangeMe123!"
    });
    expect(loggedIn.ok).toBeTruthy();
  } else {
    expect(blockedLogin.ok).toBeTruthy();
  }

  const channels = await readJSON(page, "/api/public/channels?page=1&pageSize=1");
  const channelID = channels.payload.items[0].id as string;
  const favAdded = await writeJSON(page, `/api/me/favorites/${channelID}`, "PUT", {});
  expect(favAdded.ok).toBeTruthy();
  const favorites = await readJSON(page, "/api/me/favorites");
  expect(favorites.payload.ids).toContain(channelID);

  const secret = "sk-phase4-private-secret";
  const created = await writeJSON(page, "/api/me/private-channels", "POST", {
    name: "Phase4 Private",
    provider: "OpenAI",
    type: "openai-compatible",
    model: "gpt-4o-mini",
    endpoint: "https://ok.example/v1",
    apiKey: secret,
    probeDaily: 1
  });
  expect(created.ok).toBeTruthy();
  expect(JSON.stringify(created.payload)).not.toContain(secret);
  expect(created.payload.channel.keyMask).toContain("****");
  expect(created.payload.channel.apiKey).toBeUndefined();
  const privateID = created.payload.channel.id as string;

  const probed = await writeJSON(page, `/api/me/private-channels/${privateID}/probe-now`, "POST");
  expect(probed.ok).toBeTruthy();
  expect(JSON.stringify(probed.payload)).not.toContain(secret);

  const quota = await writeJSON(page, `/api/me/private-channels/${privateID}/probe-now`, "POST");
  expect(quota.status).toBe(429);

  const updated = await writeJSON(page, `/api/me/private-channels/${privateID}`, "PATCH", {
    name: "Phase4 Private Updated",
    provider: "OpenAI",
    type: "openai-compatible",
    model: "gpt-4o-mini",
    endpoint: "https://ok.example/v1",
    probeDaily: 2
  });
  expect(updated.ok).toBeTruthy();
  expect(updated.payload.channel.keyMask).toBe(created.payload.channel.keyMask);

  await page.goto("/console");
  await expect(page.getByText("Phase4 Private Updated")).toBeVisible();
  await expect(page.getByText(secret)).toHaveCount(0);

  await adminLogin(page, "/admin");
  const adminChannels = await readJSON(page, "/api/admin/channels");
  expect(JSON.stringify(adminChannels.payload)).toContain("Phase4 Private Updated");
  expect(JSON.stringify(adminChannels.payload)).not.toContain(secret);
});
