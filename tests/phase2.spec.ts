import { expect, test, type APIRequestContext, type Route } from "@playwright/test";

type PublicChannel = {
  id: string;
  publicSlug: string;
  name: string;
  provider: string;
  introTitle?: string;
  model: string;
  status: string;
};

async function firstPublicChannel(request: APIRequestContext) {
  const response = await request.get("/api/public/channels?page=1&pageSize=20");
  expect(response.ok()).toBeTruthy();
  const body = await response.json();
  expect(body.items.length).toBeGreaterThan(0);
  return body.items[0] as PublicChannel;
}

test("phase 2 public api returns stable channel data", async ({ request }) => {
  const overview = await request.get("/api/public/overview");
  expect(overview.ok()).toBeTruthy();
  const overviewBody = await overview.json();
  expect(overviewBody.total).toBeGreaterThan(0);
  expect(overviewBody.healthy).toBeGreaterThanOrEqual(0);

  const healthy = await request.get("/api/public/channels?status=healthy&pageSize=50");
  expect(healthy.ok()).toBeTruthy();
  const healthyBody = await healthy.json();
  expect(healthyBody.items.every((item: { status: string }) => item.status === "healthy")).toBeTruthy();

  const channel = await firstPublicChannel(request);
  expect(channel.publicSlug).toMatch(/^[a-z0-9]{6,16}$/);
  const searched = await request.get(`/api/public/channels?query=${encodeURIComponent(channel.name)}&pageSize=50`);
  expect(searched.ok()).toBeTruthy();
  const searchedBody = await searched.json();
  expect(searchedBody.items.some((item: { id: string }) => item.id === channel.id)).toBeTruthy();

  const detail = await request.get(`/api/public/channels/${channel.id}`);
  expect(detail.ok()).toBeTruthy();
  const detailBody = await detail.json();
  expect(detailBody.channel.id).toBe(channel.id);
  expect(detailBody.layers.length).toBeGreaterThanOrEqual(4);

  const slugDetail = await request.get(`/api/public/channels/${channel.publicSlug}`);
  expect(slugDetail.ok()).toBeTruthy();
  const slugDetailBody = await slugDetail.json();
  expect(slugDetailBody.channel.id).toBe(channel.id);

  const series = await request.get(`/api/public/channels/${channel.publicSlug}/series?days=30`);
  expect(series.ok()).toBeTruthy();
  const seriesBody = await series.json();
  expect(seriesBody.items).toHaveLength(30);
});

test("phase 2 public pages support filters and deep links", async ({ page, request }) => {
  const channel = await firstPublicChannel(request);
  await page.goto("/");
  await expect(page.getByRole("heading", { name: /先看可用性，再选择中转站/ })).toBeVisible();
  await expect(page.getByText("当前严格健康率").first()).toBeVisible();
  await page.goto("/dashboard");
  await expect(page.getByRole("heading", { name: "监控总览" })).toBeVisible();
  await expect(page.getByRole("heading", { name: /通道明细看板/ })).toBeVisible();
  await expect(page.getByRole("columnheader", { name: "操作" })).toBeVisible();
  const officialRegisterLink = page.getByRole("link", { name: "官网" }).first();
  await expect(officialRegisterLink).toBeVisible();
  await expect(officialRegisterLink).toHaveAttribute("href", /^https:\/\//);
  await expect(officialRegisterLink).toHaveAttribute("target", "_blank");
  await page.getByPlaceholder("搜索服务商 / 模型…").fill(channel.name);
  await expect(page.getByRole("heading", { name: /通道明细看板\s+1 个中转站/ })).toBeVisible();
  await expect(page.locator("tbody tr").filter({ hasText: channel.provider }).first()).toBeVisible();
  await page.getByRole("button", { name: "模型" }).click();
  await expect(page.getByRole("columnheader", { name: "模型" })).toBeVisible();

  await page.goto("/recommend");
  await expect(page.getByRole("columnheader", { name: "24 小时成功率" })).toBeVisible();

  await page.goto(`/channels/${channel.id}`);
  await page.waitForURL((url) => url.pathname === `/channels/${channel.publicSlug}`);
  await expect(page.getByText(channel.introTitle || `${channel.provider} 官方介绍`)).toBeVisible();
  await expect(page.getByText("综合健康指数 · TokHub Index")).toBeVisible();
  await expect(page.getByRole("heading", { name: new RegExp(channel.name) })).toBeVisible();
  await expect(page.getByText(/基础监控/).first()).toBeVisible();
  await expect(page.getByText(/真实监控/).first()).toBeVisible();
});

test("phase 2 dashboard preserves sub-cent probe cost display", async ({ page }) => {
  const fulfillJSON = (route: Route, value: unknown) =>
    route.fulfill({ contentType: "application/json", body: JSON.stringify(value) });

  await page.route("**/api/public/**", (route) => {
    const url = new URL(route.request().url());
    switch (url.pathname) {
      case "/api/public/overview":
        return fulfillJSON(route, {
          total: 1,
          healthy: 1,
          functionalDown: 0,
          connectivityDown: 0,
          degraded: 0,
          unknown: 0,
          healthyRate: 100,
          p95LatencySeconds: 0.82,
          averageLatencyMs: 820,
          slowRate: 0,
          probeCostToday: 0.002044,
          probeTokensToday: 393,
          probeRunsToday: 235,
          updatedAt: new Date().toISOString()
        });
      case "/api/public/channels":
        return fulfillJSON(route, { items: [], total: 0, page: 1, pageSize: 100 });
      case "/api/public/providers/rank":
      case "/api/public/errors/summary":
        return fulfillJSON(route, { items: [] });
      case "/api/public/site-config":
        return fulfillJSON(route, {
          registrationOpen: true,
          showRegisterCta: true,
          emailVerificationRequired: false,
          adminPath: "/admin",
          brandName: "TokHub",
          logoMark: "T",
          subtitle: "",
          publicUrl: "",
          footerText: "",
          defaultGatewayPolicy: "latency",
          timezone: "Asia/Shanghai",
          navItems: [],
          footerLinks: [],
          monitorModels: [],
          analyticsCode: ""
        });
      default:
        return route.fallback();
    }
  });
  await page.route("**/api/auth/me", (route) => fulfillJSON(route, { user: null }));

  await page.goto("/dashboard");
  const card = page.locator(".kpi", { hasText: "今日探测 Token 成本" });
  await expect(card.locator(".k-value")).toHaveText("$0.002");
  await expect(card.locator(".k-foot")).toHaveText("393 tokens · 235 次探测");
});

test("phase 2 channel row opens preview drawer before full detail", async ({ page, request }) => {
  await firstPublicChannel(request);
  await page.goto("/dashboard");
  await expect(page.getByRole("heading", { name: "监控总览" })).toBeVisible();

  const row = page.locator(".brand-table tbody tr.channel-click-row").first();
  await expect(row).toBeVisible();
  await row.locator("td").nth(1).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await expect(page.getByText("基础链路瀑布 · L1/L2")).toBeVisible();
  await expect(page.getByText("真实探测日志 · L3")).toBeVisible();
  await expect(page).toHaveURL(/\/dashboard$/);

  const detailLink = page.getByRole("link", { name: /查看完整详情/ }).first();
  const detailHref = await detailLink.getAttribute("href");
  expect(detailHref).toMatch(/^\/channels\/[a-z0-9]+(?:\?range=(?:24|7|30|all))?$/);
  await detailLink.click();
  await page.waitForURL(`**${detailHref}`);
  await expect(page.getByText("综合健康指数 · TokHub Index")).toBeVisible();
});
