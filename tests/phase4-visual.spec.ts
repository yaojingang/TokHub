import { expect, test } from "@playwright/test";
import { adminLogin } from "./helpers";

for (const viewport of [
  { width: 1440, height: 1000 },
  { width: 1280, height: 900 },
  { width: 390, height: 844 }
]) {
  test(`phase 4 dashboard and settings visual ${viewport.width}`, async ({ page }) => {
    await page.setViewportSize(viewport);
    await adminLogin(page, "/admin");

    await page.goto("/console");
    await expect(page.getByRole("heading", { name: /用户控制台/ })).toBeVisible();
    await page.screenshot({ path: `test-results/phase4-dashboard-${viewport.width}.png`, fullPage: true });

    await page.goto("/admin/settings");
    await expect(page.getByRole("heading", { name: "设置" })).toBeVisible();
    await page.screenshot({ path: `test-results/phase4-admin-settings-${viewport.width}.png`, fullPage: true });

    await page.goto("/admin/channels");
    await expect(page.getByRole("heading", { name: "通道接入" })).toBeVisible();
    await page.getByRole("button", { name: /用户私有通道/ }).click();
    await expect(page.getByText("管理员仅查看汇总")).toBeVisible();
    await page.screenshot({ path: `test-results/phase4-admin-private-channels-${viewport.width}.png`, fullPage: true });
  });
}
