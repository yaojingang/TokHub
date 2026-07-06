import { expect, test } from "@playwright/test";
import { adminLogin } from "./helpers";

const viewports = [
  { width: 1440, height: 1100 },
  { width: 1280, height: 1000 },
  { width: 390, height: 1100 }
];

const publicPages = [
  { path: "/", marker: /先看可用性/, name: "public-home" },
  { path: "/dashboard", marker: /监控总览/, name: "public-dashboard" },
  { path: "/login", marker: /欢迎回到 TokHub/, name: "login" },
  { path: "/recommend", marker: /不踩坑的/, name: "recommend" }
];

const consolePages = [
  { path: "/console", marker: /控制台首页/, name: "console-home" },
  { path: "/console/channels", marker: /我的私有通道|我的通道/, name: "console-channels" },
  { path: "/console/gateways", marker: /一键生成工作区专属中转站/, name: "console-gateways" },
  { path: "/console/keys", marker: /成员与密钥/, name: "console-keys" },
  { path: "/console/members", marker: /成员与密钥/, name: "console-members" },
  { path: "/console/usage", marker: /用量数据/, name: "console-usage" },
  { path: "/console/alerts", marker: /告警规则/, name: "console-alerts" },
  { path: "/console/audit", marker: /审计日志/, name: "console-audit" },
  { path: "/console/settings", marker: /工作区设置/, name: "console-settings" },
  { path: "/console/help", marker: /帮助中心/, name: "console-help" }
];

const adminPages = [
  { path: "/admin", marker: /平台总览/, name: "admin-home" },
  { path: "/admin/channels", marker: /通道接入/, name: "admin-channels" },
  { path: "/admin/users", marker: /用户管理/, name: "admin-users" },
  { path: "/admin/orgs", marker: /组织管理/, name: "admin-orgs" },
  { path: "/admin/open-api", marker: /开放 API/, name: "admin-open-api" },
  { path: "/admin/recommend", marker: /精选推荐管理/, name: "admin-recommend" },
  { path: "/admin/usage", marker: /用量数据/, name: "admin-usage" },
  { path: "/admin/alerts", marker: /告警规则/, name: "admin-alerts" },
  { path: "/admin/audit", marker: /审计日志/, name: "admin-audit" },
  { path: "/admin/settings", marker: /系统设置/, name: "admin-settings" },
  { path: "/admin/web", marker: /网站设置/, name: "admin-web" }
];

async function expectMarker(page: import("@playwright/test").Page, marker: RegExp) {
  const heading = page.getByRole("heading", { name: marker }).first();
  if (await heading.count()) {
    await expect(heading).toBeVisible();
    return;
  }
  await expect(page.locator("main, .admin-body, .page, .auth-wrap").getByText(marker).first()).toBeVisible();
}

for (const viewport of viewports) {
  test(`phase 8 full visual acceptance ${viewport.width}`, async ({ page }) => {
    await page.setViewportSize(viewport);

    const publicChannels = await (await page.request.get("/api/public/channels?page=1&pageSize=1")).json();
    const optionalDetailPage = publicChannels.items?.[0]?.id
      ? [{ path: `/channels/${publicChannels.items[0].id}`, marker: /综合健康指数/, name: "channel-detail" }]
      : [];
    for (const item of [...publicPages, ...optionalDetailPage]) {
      await page.goto(item.path);
      await expectMarker(page, item.marker);
      await page.screenshot({ path: `test-results/phase8-${item.name}-${viewport.width}.png`, fullPage: true });
    }

    await adminLogin(page, "/admin");

    for (const item of consolePages) {
      await page.goto(item.path);
      await expectMarker(page, item.marker);
      await page.screenshot({ path: `test-results/phase8-${item.name}-${viewport.width}.png`, fullPage: true });
    }

    for (const item of adminPages) {
      await page.goto(item.path);
      await expectMarker(page, item.marker);
      await page.screenshot({ path: `test-results/phase8-${item.name}-${viewport.width}.png`, fullPage: true });
    }
  });
}
