import { expect, test } from "@playwright/test";

test("public home renders before site config finishes", async ({ page }) => {
  let releaseSiteConfig: () => void = () => undefined;
  let siteConfigRequests = 0;
  const siteConfigReady = new Promise<void>((resolve) => {
    releaseSiteConfig = resolve;
  });

  await page.route("**/api/public/site-config", async (route) => {
    siteConfigRequests += 1;
    await siteConfigReady;
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        registrationOpen: true,
        showRegisterCta: true,
        emailVerificationRequired: false,
        adminPath: "/admin",
        brandName: "TokHub",
        logoMark: "T",
        subtitle: "API 中转站监控",
        publicUrl: "",
        footerText: "",
        defaultGatewayPolicy: "latency",
        timezone: "Asia/Shanghai",
        navItems: [],
        footerLinks: [],
        monitorModels: []
      })
    });
  });

  await page.goto("/");

  await expect.poll(() => siteConfigRequests).toBeGreaterThan(0);
  await expect(page.getByRole("navigation")).toBeVisible();
  await expect(page.getByRole("heading", { name: /先看可用性/ })).toBeVisible();
  await expect(page.getByText("正在加载站点配置")).toHaveCount(0);

  releaseSiteConfig();
  await expect(page.getByText("正在加载站点配置")).toHaveCount(0);
});
