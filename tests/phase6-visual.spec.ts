import { expect, test } from "@playwright/test";
import { adminLogin } from "./helpers";

const viewports = [
  { width: 1440, height: 1100 },
  { width: 1280, height: 1000 },
  { width: 390, height: 1100 }
];

for (const viewport of viewports) {
  test(`phase 6 operations pages visual ${viewport.width}`, async ({ page }) => {
    await page.setViewportSize(viewport);

    await page.goto("/recommend");
    await expect(page.getByRole("heading", { name: /不踩坑的/ })).toBeVisible();
    await page.screenshot({ path: `test-results/phase6-recommend-${viewport.width}.png`, fullPage: true });

    await adminLogin(page, "/admin");
    await page.goto("/admin/recommend");
    await expect(page.getByRole("heading", { name: "精选推荐管理" })).toBeVisible();
    await page.screenshot({ path: `test-results/phase6-admin-recommend-${viewport.width}.png`, fullPage: true });

    await page.goto("/admin/open-api");
    await expect(page.getByRole("heading", { name: "开放 API" })).toBeVisible();
    await page.screenshot({ path: `test-results/phase6-admin-open-api-${viewport.width}.png`, fullPage: true });

    await page.goto("/admin/web");
    await expect(page.getByRole("heading", { name: "网站设置" })).toBeVisible();
    await page.screenshot({ path: `test-results/phase6-admin-web-${viewport.width}.png`, fullPage: true });
  });
}
