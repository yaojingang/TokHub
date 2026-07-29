import { expect, test } from "@playwright/test";

test("AI connection center exposes seven official developer products and a responsive setup flow", async ({ page }) => {
  const suffix = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  await page.goto("/login?next=%2Fconsole%2Fconnections");
  await page.getByRole("button", { name: "注册新账号", exact: true }).click();
  await page.getByLabel("邮箱").fill(`ai-connections-${suffix}@example.test`);
  await page.getByLabel("设置密码").fill(`AIConnections-${suffix}!`);
  await page.getByRole("button", { name: "创建账号并进入控制台 →", exact: true }).click();
  await page.waitForURL((url) => url.pathname === "/console/connections");

  await expect(page.getByRole("heading", { name: "连接你的 AI 服务" })).toBeVisible();
  await expect(page.locator(".ai-provider-item")).toHaveCount(7);
  await expect(page.getByText(/连接固定保存在个人空间/)).toBeVisible();
  await expect(page.getByText(/服务商密码、验证码、浏览器 Cookie、Local Storage、cf_clearance 与 PoW 数据均不采集/)).toBeVisible();

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

test("AI connection center renders Gemini OAuth, DeepSeek guided key, and ChatGPT experimental authorization controls", async ({ page }) => {
  const suffix = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  const providers = [
    provider("gemini", "Gemini", [
      authMethod("api_key", "官方 API Key", "stable"),
      authMethod("oauth", "使用 Google 账号授权", "stable", "redirect_callback")
    ]),
    provider("deepseek", "DeepSeek", [
      authMethod("api_key", "官方 API Key", "stable"),
      authMethod("api_key_guided", "前往 DeepSeek 开放平台", "stable", "guided_api_key")
    ]),
    provider("openai", "ChatGPT", [
      authMethod("api_key", "官方 API Key", "stable"),
      authMethod("codex_oauth", "登录 ChatGPT", "experimental", "paste_callback")
    ])
  ];
  await page.route("**/api/me/ai-connection-providers", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ items: providers, policyVersion: "ai-authorization-v2", credentialPolicy: { accepted: [], rejected: [] } })
    });
  });
  await page.route("**/api/me/ai-connections", async (route) => {
    if (route.request().method() === "GET") {
      await route.fulfill({ contentType: "application/json", body: JSON.stringify({ items: [] }) });
      return;
    }
    await route.continue();
  });
  await page.route("**/api/me/ai-auth/step-up", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ grant: "step_test", expiresAt: new Date(Date.now() + 600_000).toISOString() })
    });
  });
  await page.route("**/api/me/ai-authorizations", async (route) => {
    await route.fulfill({
      status: 201,
      contentType: "application/json",
      body: JSON.stringify({
        id: "authz_test",
        authorizationUrl: `${new URL(page.url()).origin}/healthz`,
        completionMode: "guided_api_key",
        expiresAt: new Date(Date.now() + 600_000).toISOString(),
        pollIntervalMs: 60_000
      })
    });
  });
  await page.route("**/api/me/ai-authorizations/authz_test", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        authorization: {
          id: "authz_test",
          provider: "deepseek",
          authMethod: "api_key_guided",
          status: "authorization_pending",
          completionMode: "guided_api_key",
          startedAt: new Date().toISOString(),
          expiresAt: new Date(Date.now() + 600_000).toISOString()
        }
      })
    });
  });

  await page.goto("/login?next=%2Fconsole%2Fconnections");
  await page.getByRole("button", { name: "注册新账号", exact: true }).click();
  await page.getByLabel("邮箱").fill(`ai-auth-${suffix}@example.test`);
  await page.getByLabel("设置密码").fill(`AIWebAuth-${suffix}!`);
  await page.getByRole("button", { name: "创建账号并进入控制台 →", exact: true }).click();
  await page.waitForURL((url) => url.pathname === "/console/connections");

  await page.getByRole("button", { name: /Gemini/ }).click();
  await expect(page.getByRole("radio", { name: /使用 Google 账号授权/ })).toHaveAttribute("aria-checked", "true");
  await expect(page.getByLabel("Google Cloud Project ID")).toBeVisible();
  await expect(page.getByLabel(/TokHub 登录密码/)).toBeVisible();

  await page.getByRole("button", { name: /ChatGPT/ }).click();
  await expect(page.getByRole("radio", { name: /登录 ChatGPT/ })).toHaveAttribute("aria-checked", "true");
  await expect(page.getByText(/消费者授权和私有接口/)).toBeVisible();
  await expect(page.locator(".ai-experimental-confirm input")).toBeVisible();

  await page.getByRole("button", { name: /DeepSeek/ }).click();
  await page.getByLabel(/TokHub 登录密码/).fill("local-password");
  const popupPromise = page.waitForEvent("popup");
  await page.getByRole("button", { name: "前往开放平台", exact: true }).click();
  const popup = await popupPromise;
  await popup.close();
  await expect(page.getByText("请在 DeepSeek 开放平台创建 API Key")).toBeVisible();
  await expect(page.getByPlaceholder("粘贴官方开发者 API Key")).toBeVisible();

  await page.setViewportSize({ width: 375, height: 812 });
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth)).toBe(375);
});

test("OAuth disconnect asks for the current TokHub password", async ({ page }) => {
  const suffix = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  const now = new Date().toISOString();
  await page.route("**/api/me/ai-connections", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        items: [{
          id: "aic_oauth_test",
          orgId: "org_test",
          provider: "gemini",
          productLine: "Google AI Studio",
          region: "global",
          authMethod: "oauth",
          protocol: "gemini",
          adapterType: "gemini",
          endpoint: "https://generativelanguage.googleapis.com/v1beta",
          providerConfig: {},
          displayName: "我的 Gemini OAuth",
          status: "active",
          authStatus: "active",
          sharingScope: "personal",
          riskLevel: "standard",
          providerAdapterVersion: "gemini-oauth-v1",
          accountMask: "p***@example.test",
          validationStage: "generation",
          validationLatencyMs: 120,
          modelCount: 1,
          policyVersion: "ai-authorization-v2",
          secretMask: "OAuth · p***@example.test",
          models: [{
            id: "aicm_oauth_test",
            connectionId: "aic_oauth_test",
            providerModelId: "gemini-test",
            displayName: "gemini-test",
            enabled: true,
            verificationStatus: "verified",
            validationLatencyMs: 120,
            capabilities: {},
            createdAt: now,
            updatedAt: now
          }],
          createdAt: now,
          updatedAt: now
        }]
      })
    });
  });

  await page.goto("/login?next=%2Fconsole%2Fconnections");
  await page.getByRole("button", { name: "注册新账号", exact: true }).click();
  await page.getByLabel("邮箱").fill(`ai-disconnect-${suffix}@example.test`);
  await page.getByLabel("设置密码").fill(`AIDisconnect-${suffix}!`);
  await page.getByRole("button", { name: "创建账号并进入控制台 →", exact: true }).click();
  await page.waitForURL((url) => url.pathname === "/console/connections");

  await expect(page.getByText("我的 Gemini OAuth", { exact: true }).first()).toBeVisible();
  await page.getByRole("button", { name: "删除连接", exact: true }).click();
  const disconnectButton = page.getByRole("button", { name: "验证并断开连接", exact: true });
  await expect(page.getByLabel("当前 TokHub 登录密码")).toBeVisible();
  await expect(disconnectButton).toBeDisabled();
  await page.getByLabel("当前 TokHub 登录密码").fill("current-password");
  await expect(disconnectButton).toBeEnabled();
  await page.setViewportSize({ width: 375, height: 812 });
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth)).toBe(375);
});

function authMethod(code: string, label: string, release: string, completionMode = "api_key") {
  return {
    code,
    label,
    release,
    sharingScope: "personal",
    completionMode,
    enabled: true,
    description: `${label} 测试说明`,
    docsUrl: "https://example.test/docs"
  };
}

function provider(code: string, name: string, authMethods: ReturnType<typeof authMethod>[]) {
  return {
    code,
    name,
    productLine: `${name} API`,
    protocol: "openai",
    type: code === "gemini" ? "gemini" : "openai-compatible",
    authMethod: "api_key",
    credentialLabel: `${name} API Key`,
    defaultRegion: "global",
    regions: [{ code: "global", name: "Global", workspaceId: false }],
    validationMode: "generation",
    generationKind: "chat",
    recommendedModels: [`${code}-test-model`],
    docsUrl: "https://example.test/docs",
    authMethods
  };
}
