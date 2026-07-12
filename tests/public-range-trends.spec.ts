import { expect, test, type Page } from "@playwright/test";

async function trendMetrics(page: Page) {
  return page.locator(".tk-trend-bars").first().evaluate((trend) => {
    const bars = Array.from(trend.querySelectorAll("i"));
    const rail = trend.getBoundingClientRect();
    const first = bars[0]?.getBoundingClientRect();
    const last = bars[bars.length - 1]?.getBoundingClientRect();
    return {
      barCount: bars.length,
      firstSlotsEmpty: bars.slice(0, 20).every((bar) => bar.classList.contains("empty")),
      firstBarWidth: first ? first.width : 0,
      railWidth: rail.width,
      span: first && last ? last.right - first.left : 0
    };
  });
}

test("public seven-day trend uses the same dense rail as thirty-day trend", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/dashboard");
  await expect(page.getByRole("heading", { name: "监控总览" })).toBeVisible();

  await page.getByRole("button", { name: "近7天" }).first().click();
  await expect(page.getByRole("columnheader", { name: "近7天趋势" })).toBeVisible();
  const sevenDay = await trendMetrics(page);
  expect(sevenDay.barCount).toBe(30);
  expect(sevenDay.firstSlotsEmpty).toBe(true);
  expect(sevenDay.firstBarWidth).toBeGreaterThanOrEqual(2.5);
  expect(sevenDay.firstBarWidth).toBeLessThanOrEqual(3.5);
  expect(Math.abs(sevenDay.span - sevenDay.railWidth)).toBeLessThanOrEqual(1);

  await page.getByRole("button", { name: "近30天" }).first().click();
  await expect(page.getByRole("columnheader", { name: "近30天趋势" })).toBeVisible();
  const thirtyDay = await trendMetrics(page);
  expect(thirtyDay.barCount).toBe(30);
  expect(thirtyDay.firstBarWidth).toBeGreaterThanOrEqual(2.5);
  expect(thirtyDay.firstBarWidth).toBeLessThanOrEqual(3.5);
  expect(Math.abs(thirtyDay.span - thirtyDay.railWidth)).toBeLessThanOrEqual(1);
});
