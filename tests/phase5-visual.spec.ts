import { expect, test } from "@playwright/test";
import { adminLogin } from "./helpers";

const viewports = [
  { width: 1440, height: 1100 },
  { width: 1280, height: 1000 },
  { width: 390, height: 1100 }
];

for (const viewport of viewports) {
  test(`phase 5 gateway pages visual ${viewport.width}`, async ({ page }) => {
    await page.setViewportSize(viewport);
    await adminLogin(page, "/console");

    await page.goto("/console/gateways");
    await expect(page.getByRole("heading", { name: "一键生成工作区专属中转站" })).toBeVisible();
    await page.screenshot({ path: `test-results/phase5-console-gateways-${viewport.width}.png`, fullPage: true });

    await page.goto("/console/members");
    await expect(page.getByText("签发 Gateway Key")).toBeVisible();
    await page.screenshot({ path: `test-results/phase5-console-members-${viewport.width}.png`, fullPage: true });

    await page.goto("/console/usage");
    await expect(page.getByRole("heading", { name: "用量数据" })).toBeVisible();
    await page.screenshot({ path: `test-results/phase5-console-usage-${viewport.width}.png`, fullPage: true });
  });
}
