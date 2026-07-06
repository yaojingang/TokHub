import { expect, test } from "@playwright/test";
import { adminLogin } from "./helpers";

for (const viewport of [
  { width: 1440, height: 1000 },
  { width: 1280, height: 900 },
  { width: 390, height: 844 }
]) {
  test(`phase 1 visual smoke ${viewport.width}`, async ({ page }) => {
    await page.setViewportSize(viewport);
    await page.goto("/admin/login?next=/admin");
    await expect(page.getByRole("heading", { name: "进入 TokHub 平台管理" })).toBeVisible();
    await page.screenshot({ path: `test-results/phase1-login-${viewport.width}.png`, fullPage: true });
    await adminLogin(page, "/admin");
    await expect(page.getByRole("heading", { name: "平台总览" })).toBeVisible();
    await page.screenshot({ path: `test-results/phase1-admin-${viewport.width}.png`, fullPage: true });
  });
}
