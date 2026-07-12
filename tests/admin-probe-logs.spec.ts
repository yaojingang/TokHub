import { expect, test } from "@playwright/test";
import { adminLogin } from "./helpers";

test("admin can diagnose an upstream authorization failure from probe logs", async ({ page }) => {
  await adminLogin(page, "/admin/audit");
  let exportRequestURL = "";

  await page.route("**/api/admin/channels", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        items: [{ id: "ch_packy", name: "PackyCode", provider: "PackyCode", model: "claude-sonnet-4-6" }],
        private: [], total: 1, summary: { platform: 1, private: 0 }
      })
    });
  });
  await page.route(/\/api\/admin\/probe-logs\/pr_l3_packy$/, async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        id: "pr_l3_packy", channelId: "ch_packy", channelName: "PackyCode", provider: "PackyCode",
        type: "anthropic", model: "claude-sonnet-4-6", endpoint: "https://api.packy.example/v1", layer: "l3",
        source: "scheduler", status: "auth_error", runStatus: "failed", step: "generate", httpStatus: 401,
        errorType: "auth_error", latencyMs: 238, stepCount: 1, startedAt: "2026-07-12T02:00:00Z",
        finishedAt: "2026-07-12T02:00:00.238Z",
        steps: [{
          step: "generate", status: "auth_error", latencyMs: 238, httpStatus: 401, errorType: "auth_error",
          metadata: { upstream_error_summary: "code=ip_not_allowed · type=auth_error" },
          createdAt: "2026-07-12T02:00:00Z"
        }]
      })
    });
  });
  await page.route(/\/api\/admin\/probe-logs\/export(?:\?.*)?$/, async (route) => {
    exportRequestURL = route.request().url();
    await route.fulfill({
      contentType: "text/csv; charset=utf-8",
      headers: { "Content-Disposition": 'attachment; filename="tokhub-probe-logs-all-20260712.csv"' },
      body: "started_at,status,status_zh\n2026-07-12T02:00:00Z,auth_error,认证失败\n"
    });
  });
  await page.route(/\/api\/admin\/probe-logs(?:\?.*)?$/, async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        items: [{
          id: "pr_l3_packy", channelId: "ch_packy", channelName: "PackyCode", provider: "PackyCode",
          type: "anthropic", model: "claude-sonnet-4-6", endpoint: "https://api.packy.example/v1", layer: "l3",
          source: "scheduler", status: "auth_error", runStatus: "failed", step: "generate", httpStatus: 401,
          errorType: "auth_error", latencyMs: 238, stepCount: 1, startedAt: "2026-07-12T02:00:00Z", finishedAt: "2026-07-12T02:00:00.238Z"
        }, {
          id: "pr_l1_packy", channelId: "ch_packy", channelName: "PackyCode", provider: "PackyCode",
          type: "anthropic", model: "claude-sonnet-4-6", endpoint: "https://api.packy.example/v1", layer: "l1",
          source: "scheduler", status: "ok", runStatus: "success", step: "http", httpStatus: 404,
          errorType: "", latencyMs: 97, stepCount: 4, startedAt: "2026-07-12T01:59:30Z", finishedAt: "2026-07-12T01:59:30.097Z"
        }],
        total: 2, page: 1, pageSize: 50,
        summary: { total: 2, abnormal: 1, authErrors: 1, slowResponses: 0, latestAt: "2026-07-12T02:00:00Z" }
      })
    });
  });

  await page.goto("/admin/audit?tab=probe");
  await expect(page.getByRole("heading", { name: "日志中心" })).toBeVisible();
  await expect(page.getByRole("tab", { name: "探测日志" })).toHaveClass(/active/);
  await expect(page.getByText("认证失败，Key、来源 IP 或授权策略不通过")).toBeVisible();
  await expect(page.getByText("端点可达，HEAD 路径未提供资源")).toBeVisible();

  await page.getByLabel("探测层级").selectOption("l3");
  await page.getByLabel("仅看异常").check();
  await page.getByRole("button", { name: "筛选" }).click();
  await page.getByLabel("导出范围").selectOption("all");
  page.once("dialog", (dialog) => void dialog.accept());
  const [download] = await Promise.all([
    page.waitForEvent("download"),
    page.getByRole("button", { name: "导出 CSV" }).click()
  ]);
  expect(download.suggestedFilename()).toBe("tokhub-probe-logs-all-20260712.csv");
  expect(exportRequestURL).toContain("range=all");
  expect(exportRequestURL).toContain("layer=l3");
  expect(exportRequestURL).toContain("onlyAbnormal=true");

  await page.getByRole("button", { name: "查看" }).first().click();
  const detailHeading = page.getByRole("heading", { name: "PackyCode · L3 探测详情" });
  await expect(detailHeading).toBeVisible();
  const detailBounds = await detailHeading.boundingBox();
  const viewport = page.viewportSize();
  expect(detailBounds && viewport && detailBounds.x < viewport.width && detailBounds.y < viewport.height).toBeTruthy();
  await expect(page.getByText("code=ip_not_allowed · type=auth_error")).toBeVisible();
  await expect(page.getByText("认证信息或来源授权未通过")).toBeVisible();
});
