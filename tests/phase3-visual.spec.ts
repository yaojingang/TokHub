import { expect, test } from "@playwright/test";
import { adminLogin } from "./helpers";

for (const viewport of [
  { width: 1440, height: 1000 },
  { width: 1280, height: 900 },
  { width: 390, height: 844 }
]) {
  test(`phase 3 admin channels visual ${viewport.width}`, async ({ page }) => {
    await page.setViewportSize(viewport);
    await adminLogin(page, "/admin");
    await page.goto("/admin/channels");
    await expect(page.getByRole("heading", { name: "通道接入" })).toBeVisible();
    await page.screenshot({ path: `test-results/phase3-admin-channels-${viewport.width}.png`, fullPage: true });
  });
}
