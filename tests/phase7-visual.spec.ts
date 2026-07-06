import { expect, test } from "@playwright/test";
import { adminLogin } from "./helpers";

const viewports = [
  { width: 1440, height: 1100 },
  { width: 1280, height: 1000 },
  { width: 390, height: 1100 }
];

for (const viewport of viewports) {
  test(`phase 7 governance pages visual ${viewport.width}`, async ({ page }) => {
    await page.setViewportSize(viewport);
    await adminLogin(page, "/admin");

    await page.goto("/admin/usage");
    await expect(page.getByRole("heading", { name: "用量数据" })).toBeVisible();
    await page.screenshot({ path: `test-results/phase7-admin-usage-${viewport.width}.png`, fullPage: true });

    await page.goto("/admin/alerts");
    await expect(page.getByRole("heading", { name: "告警规则" })).toBeVisible();
    await page.screenshot({ path: `test-results/phase7-admin-alerts-${viewport.width}.png`, fullPage: true });

    await page.goto("/admin/audit");
    await expect(page.getByRole("heading", { name: "审计日志" })).toBeVisible();
    await page.screenshot({ path: `test-results/phase7-admin-audit-${viewport.width}.png`, fullPage: true });

    await page.goto("/console/alerts");
    await expect(page.getByRole("heading", { name: "告警规则" })).toBeVisible();
    await page.screenshot({ path: `test-results/phase7-console-alerts-${viewport.width}.png`, fullPage: true });

    await page.goto("/console/audit");
    await expect(page.getByRole("heading", { name: "审计日志" })).toBeVisible();
    await page.screenshot({ path: `test-results/phase7-console-audit-${viewport.width}.png`, fullPage: true });
  });
}
