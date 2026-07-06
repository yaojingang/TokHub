import { expect, test } from "@playwright/test";
import { adminLogin } from "./helpers";

test("pwa metadata and static resources are exposed", async ({ page, request }) => {
  await page.goto("/");

  await expect(page.locator('link[rel="manifest"]')).toHaveAttribute("href", "/manifest.webmanifest");
  await expect(page.locator('meta[name="theme-color"]')).toHaveAttribute("content", "#dbe2f9");
  await expect(page.locator('link[rel="apple-touch-icon"]')).toHaveAttribute("href", "/icons/apple-touch-icon.png");

  const manifestResponse = await request.get("/manifest.webmanifest");
  expect(manifestResponse.ok()).toBeTruthy();
  const manifest = await manifestResponse.json();
  expect(manifest).toMatchObject({
    name: "TokHub",
    short_name: "TokHub",
    start_url: "/",
    scope: "/",
    display: "standalone",
    theme_color: "#dbe2f9",
    background_color: "#ffffff"
  });
  expect(manifest.icons).toEqual(
    expect.arrayContaining([
      expect.objectContaining({ src: "/icons/icon-192.png", sizes: "192x192", type: "image/png" }),
      expect.objectContaining({ src: "/icons/icon-512.png", sizes: "512x512", type: "image/png" })
    ])
  );

  const serviceWorkerResponse = await request.get("/sw.js");
  expect(serviceWorkerResponse.ok()).toBeTruthy();
  const serviceWorker = await serviceWorkerResponse.text();
  expect(serviceWorker).toContain("tokhub-pwa-v2");
  expect(serviceWorker).toContain("NETWORK_ONLY_PATHS");
  expect(serviceWorker).toContain("api");
  expect(serviceWorker).toContain("gateway");
});

test("home page exposes desktop workbench entrances", async ({ page }) => {
  await page.goto("/");

  for (const href of ["/dashboard", "/pricing", "/recommend", "/console"]) {
    await expect(page.locator(`a[href="${href}"]`).first()).toBeVisible();
  }
  await expect(page.getByRole("button", { name: "安装工作台" })).toBeVisible();
});

test("public nav exposes install entry outside the home page", async ({ page }) => {
  await page.goto("/dashboard");

  await expect(page.getByRole("button", { name: "安装工作台" })).toBeVisible();
});

test("install button falls back to manual browser guidance", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "安装工作台" }).click();

  await expect(page.getByText("如果没有弹出安装框")).toBeVisible();
});

test("deep links continue to fall back into app routes", async ({ page }) => {
  await page.goto("/dashboard");
  await expect(page.getByRole("heading", { name: "监控总览" })).toBeVisible();

  await page.goto("/console");
  await expect(page).toHaveURL(/\/login\?next=%2Fconsole/);
  await expect(page.getByRole("heading", { name: "欢迎回到 TokHub" })).toBeVisible();

  await page.goto("/admin");
  await expect(page).toHaveURL(/\/admin\/login\?next=%2Fadmin/);
  await expect(page.getByRole("heading", { name: "进入 TokHub 平台管理" })).toBeVisible();
});

test("platform admins see the platform management workbench entrance", async ({ page }) => {
  await adminLogin(page, "/admin");
  await page.goto("/");

  await expect(page.locator('a[href="/admin"]').first()).toBeVisible();
});
