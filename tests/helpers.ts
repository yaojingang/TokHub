import { expect, Page } from "@playwright/test";
import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";

let localEnv: Record<string, string> | null = null;

function projectEnvValue(key: string) {
  if (process.env[key]) return process.env[key];
  if (!localEnv) {
    localEnv = {};
    const envPath = resolve(process.cwd(), ".env");
    if (existsSync(envPath)) {
      for (const line of readFileSync(envPath, "utf8").split(/\r?\n/)) {
        const trimmed = line.trim();
        if (!trimmed || trimmed.startsWith("#")) continue;
        const separator = trimmed.indexOf("=");
        if (separator < 1) continue;
        const envKey = trimmed.slice(0, separator).trim();
        const rawValue = trimmed.slice(separator + 1).trim();
        localEnv[envKey] = rawValue.replace(/^["']|["']$/g, "");
      }
    }
  }
  return localEnv[key] || "";
}

export async function waitForPath(page: Page, pathname: string) {
  if (new URL(page.url()).pathname === pathname) {
    return;
  }
  await page.waitForURL((url) => url.pathname === pathname);
}

export async function expectLoggedInRole(page: Page, roles: string[] = ["owner", "admin", "user"]) {
  await expect
    .poll(
      async () =>
        page.evaluate(async () => {
          const response = await fetch("/api/auth/me", { credentials: "include" });
          const payload = await response.json();
          return payload.user?.role ?? "";
        }),
      { timeout: 5_000 }
    )
    .toMatch(new RegExp(`^(${roles.join("|")})$`));
}

export function adminIdentifier() {
  return (
    projectEnvValue("TOKHUB_E2E_ADMIN_USERNAME") ||
    projectEnvValue("TOKHUB_ADMIN_USERNAME") ||
    projectEnvValue("TOKHUB_E2E_ADMIN_EMAIL") ||
    projectEnvValue("TOKHUB_ADMIN_EMAIL") ||
    "admin"
  );
}

export function adminEmail() {
  return projectEnvValue("TOKHUB_E2E_ADMIN_EMAIL") || projectEnvValue("TOKHUB_ADMIN_EMAIL") || adminIdentifier();
}

export function adminPassword() {
  return projectEnvValue("TOKHUB_E2E_ADMIN_PASSWORD") || projectEnvValue("TOKHUB_ADMIN_PASSWORD") || "ChangeMe123!";
}

export async function adminLogin(page: Page, next = "/admin") {
  const adminTarget = next === "/admin" || next.startsWith("/admin/");
  await page.goto(`${adminTarget ? "/admin/login" : "/login"}?next=${encodeURIComponent(next)}`);
  if (adminTarget) {
    const identifier = page.getByLabel("管理员用户名");
    if (!(await identifier.isVisible().catch(() => false))) {
      const switchButton = page.getByRole("button", { name: "退出并切换管理员" });
      if (await switchButton.isVisible().catch(() => false)) {
        await switchButton.click();
      }
    }
    if (!(await identifier.isVisible().catch(() => false))) {
      const alreadyAdmin = await page
        .evaluate(async () => {
          const response = await fetch("/api/auth/me", { credentials: "include" });
          const payload = await response.json();
          return payload.user?.role === "owner" || payload.user?.role === "admin";
        })
        .catch(() => false);
      if (alreadyAdmin && new URL(page.url()).pathname === next) {
        return;
      }
      await forceLogout(page);
      await page.goto(`/admin/login?next=${encodeURIComponent(next)}`);
    }
    await expect(identifier).toBeVisible();
    await identifier.fill(adminIdentifier());
  } else {
    await page.getByLabel("邮箱").fill(adminEmail());
  }
  await page.getByLabel("密码").fill(adminPassword());
  await Promise.all([
    waitForPath(page, next),
    page.getByRole("button", { name: adminTarget ? "进入平台管理 →" : "登录 →" }).click()
  ]);
  await expectLoggedInRole(page, ["owner", "admin"]);
}

async function forceLogout(page: Page) {
  await page.evaluate(async () => {
    const csrfResponse = await fetch("/api/auth/csrf", { credentials: "include" });
    const csrfPayload = await csrfResponse.json();
    await fetch("/api/auth/logout", {
      method: "POST",
      credentials: "include",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfPayload.csrfToken
      },
      body: "{}"
    });
  });
}
