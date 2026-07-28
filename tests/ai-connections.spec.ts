import { expect, test } from "@playwright/test";

test("AI connection center exposes seven official developer products and a responsive setup flow", async ({ page }) => {
  const suffix = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  await page.goto("/login?next=%2Fconsole%2Fconnections");
  await page.getByRole("button", { name: "注册新账号", exact: true }).click();
  await page.getByLabel("邮箱").fill(`ai-connections-${suffix}@example.test`);
  await page.getByLabel("设置密码").fill(`AIConnections-${suffix}!`);
  await page.getByRole("button", { name: "创建账号并进入控制台 →", exact: true }).click();
  await page.waitForURL((url) => url.pathname === "/console/connections");

  await expect(page.getByRole("heading", { name: "连接你的 AI 开发者服务" })).toBeVisible();
  await expect(page.locator(".ai-provider-item")).toHaveCount(7);
  await expect(page.getByText(/连接固定保存在个人空间/)).toBeVisible();
  await expect(page.getByText(/账号密码、短信验证码、浏览器 Cookie、消费者登录态和 CLI OAuth 会话均不采集/)).toBeVisible();

  await page.getByRole("button", { name: /千问/ }).click();
  const setup = page.locator(".ai-setup-panel");
  await expect(setup.getByRole("heading", { name: "千问" })).toBeVisible();
  await setup.getByLabel("地域 / API 产品区").selectOption("ap-northeast-1");
  await expect(setup.getByLabel(/Workspace ID/)).toBeVisible();
  await expect(setup.getByLabel(/模型 ID/)).toHaveValue(/qwen/);
  await expect(setup.getByLabel("Model Studio API Key")).toHaveAttribute("type", "password");

  await page.setViewportSize({ width: 375, height: 812 });
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth)).toBe(375);
  await expect(page.getByRole("button", { name: "连接并验证", exact: true })).toBeVisible();
});
