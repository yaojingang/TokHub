import { expect, Page, test } from "@playwright/test";
import { adminLogin } from "./helpers";

const viewports = [
  { name: "desktop", width: 1440, height: 1000 },
  { name: "mobile", width: 390, height: 1000 }
];

const adminPages = [
  { path: "/admin", marker: /平台总览/ },
  { path: "/admin/channels", marker: /通道接入/ },
  { path: "/admin/gateways", marker: /平台网关|一键生成平台专属中转站/ },
  { path: "/admin/members", marker: /成员与密钥/ },
  { path: "/admin/users", marker: /用户管理/ },
  { path: "/admin/orgs", marker: /组织管理/ },
  { path: "/admin/open-api", marker: /开放 API/ },
  { path: "/admin/recommend", marker: /精选推荐管理/ },
  { path: "/admin/usage", marker: /用量数据/ },
  { path: "/admin/alerts", marker: /告警规则/ },
  { path: "/admin/audit", marker: /审计日志/ },
  { path: "/admin/settings", marker: /系统设置/ },
  { path: "/admin/web", marker: /网站设置/ }
];

const consolePages = [
  { path: "/console", marker: /控制台首页/ },
  { path: "/console/fullscreen", marker: /今日用量、模型分布和私有监控/ },
  { path: "/console/channels", marker: /我的私有通道|我的通道/ },
  { path: "/console/gateways", marker: /一键生成工作区专属中转站/ },
  { path: "/console/keys", marker: /成员与密钥/ },
  { path: "/console/members", marker: /成员与密钥/ },
  { path: "/console/usage", marker: /用量数据/ },
  { path: "/console/alerts", marker: /告警规则/ },
  { path: "/console/audit", marker: /审计日志/ },
  { path: "/console/settings", marker: /工作区设置/ },
  { path: "/console/help", marker: /帮助中心/ }
];

test.describe("admin and console visual interaction audit", () => {
  test("console fullscreen action opens a standalone monitor tab", async ({ page, context }) => {
    await page.setViewportSize({ width: 1440, height: 1000 });
    await adminLogin(page, "/console");

    const [popup] = await Promise.all([
      context.waitForEvent("page"),
      page.getByRole("link", { name: "全屏监控" }).click()
    ]);

    await popup.waitForLoadState("networkidle");
    await expectRouteReady(popup, /今日用量、模型分布和私有监控/);
    await expect(popup.locator(".console-fullscreen-page")).toBeVisible();
    await expect(popup.getByText("设置中心")).toHaveCount(0);
    await popup.close();
  });

  for (const viewport of viewports) {
    test(`all admin and console routes render without visual shell regressions on ${viewport.name}`, async ({ page }) => {
      await page.setViewportSize({ width: viewport.width, height: viewport.height });
      const browserErrors: string[] = [];
      page.on("pageerror", (error) => browserErrors.push(error.message));
      page.on("console", (message) => {
        if (message.type() === "error") browserErrors.push(message.text());
      });

      await adminLogin(page, "/admin");

      for (const route of [...adminPages, ...consolePages]) {
        await page.goto(route.path);
        await expectRouteReady(page, route.marker);
        await expect(page.locator("body")).not.toContainText(/Phase Backlog|规划中|只读发布占位状态|该平台管理模块已接入后台壳|页面不存在/);
        await expectNoPageOverflow(page, route.path);
      }

      expect(browserErrors).toEqual([]);
    });
  }
});

async function expectRouteReady(page: Page, marker: RegExp) {
  const heading = page.getByRole("heading", { name: marker }).first();
  if (await heading.count()) {
    await expect(heading).toBeVisible();
    return;
  }
  await expect(page.locator("main, .admin-body, .page, .auth-wrap").getByText(marker).first()).toBeVisible();
}

async function expectNoPageOverflow(page: Page, route: string) {
  const overflow = await page.evaluate(() => {
    const root = document.documentElement;
    const body = document.body;
    return {
      viewport: window.innerWidth,
      documentWidth: root.scrollWidth,
      bodyWidth: body.scrollWidth,
      offenders: Array.from(document.querySelectorAll<HTMLElement>("body *"))
        .filter((element) => {
          const rect = element.getBoundingClientRect();
          const style = window.getComputedStyle(element);
          if (style.position === "fixed" || style.position === "absolute") return false;
          if (style.display === "none" || style.visibility === "hidden") return false;
          return rect.width > window.innerWidth + 8 || rect.right > window.innerWidth + 8;
        })
        .slice(0, 6)
        .map((element) => {
          const rect = element.getBoundingClientRect();
          return {
            tag: element.tagName.toLowerCase(),
            className: String(element.className || ""),
            text: (element.textContent || "").trim().slice(0, 80),
            right: Math.round(rect.right),
            width: Math.round(rect.width)
          };
        })
    };
  });
  expect(overflow, `${route} should not create page-level horizontal overflow`).toMatchObject({
    documentWidth: expect.any(Number),
    bodyWidth: expect.any(Number)
  });
  expect(Math.max(overflow.documentWidth, overflow.bodyWidth), `${route} overflow offenders: ${JSON.stringify(overflow.offenders)}`).toBeLessThanOrEqual(overflow.viewport + 8);
}
