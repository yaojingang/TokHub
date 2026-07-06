import { expect, test } from "@playwright/test";

for (const viewport of [
  { width: 1440, height: 1000 },
  { width: 1280, height: 900 },
  { width: 390, height: 844 }
]) {
  test(`phase 2 public visual ${viewport.width}`, async ({ page }) => {
    await page.setViewportSize(viewport);
    await page.goto("/");
    await expect(page.getByRole("heading", { name: "先看可用性，再选择中转站" })).toBeVisible();
    await page.screenshot({ path: `test-results/phase2-home-${viewport.width}.png`, fullPage: true });

    await page.goto("/dashboard");
    await expect(page.getByRole("heading", { name: "监控总览" })).toBeVisible();
    await page.screenshot({ path: `test-results/phase2-dashboard-${viewport.width}.png`, fullPage: true });

    const publicChannels = await (await page.request.get("/api/public/channels?page=1&pageSize=1")).json();
    const channelID = publicChannels.items?.[0]?.id;
    if (channelID) {
      await page.goto(`/channels/${channelID}`);
      await expect(page.getByText(/综合健康指数/).first()).toBeVisible();
      await page.screenshot({ path: `test-results/phase2-detail-${viewport.width}.png`, fullPage: true });
    }
  });
}
