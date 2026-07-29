import { expect, test, type Page } from "@playwright/test";

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
  await expect(page.getByText(/服务商密码、验证码、完整 Cookie、cf_clearance 与其他浏览器数据均不采集/)).toBeVisible();

  await page.getByRole("button", { name: /DeepSeek/ }).click();
  await expect(page.getByRole("radio", { name: /前往 DeepSeek 开放平台/ })).toBeEnabled();
  await expect(page.getByRole("radio", { name: /登录 DeepSeek 网页账号/ })).toBeVisible();

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

test("AI connection center renders Gemini OAuth, DeepSeek web login, guided key, and ChatGPT experimental controls", async ({ page }) => {
  const suffix = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  let deepSeekStatusPolls = 0;
  const providers = [
    provider("gemini", "Gemini", [
      authMethod("api_key", "官方 API Key", "stable"),
      authMethod("oauth", "使用 Google 账号授权", "stable", "redirect_callback")
    ]),
    provider("deepseek", "DeepSeek", [
      authMethod("api_key", "官方 API Key", "stable"),
      authMethod("api_key_guided", "前往 DeepSeek 开放平台", "stable", "guided_api_key"),
      authMethod(
        "deepseek_web_token",
        "登录 DeepSeek 网页账号",
        "experimental",
        "paste_token",
        true,
        undefined,
        "依赖 DeepSeek 网页私有协议和独立桥接服务。"
      )
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
    const request = route.request().postDataJSON() as { password?: string };
    if (request.password === "wrong-password") {
      await route.fulfill({
        status: 401,
        contentType: "application/json",
        body: JSON.stringify({ error: { code: "step_up_failed", message: "当前账号密码验证失败" } })
      });
      return;
    }
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ grant: "step_test", expiresAt: new Date(Date.now() + 600_000).toISOString() })
    });
  });
  await page.route("**/api/me/ai-authorizations", async (route) => {
    const request = route.request().postDataJSON() as { method?: string };
    const deepSeekWeb = request.method === "deepseek_web_token";
    await route.fulfill({
      status: 201,
      contentType: "application/json",
      body: JSON.stringify({
        id: "authz_test",
        authorizationUrl: `${new URL(page.url()).origin}/healthz`,
        completionMode: deepSeekWeb ? "paste_token" : "guided_api_key",
        expiresAt: new Date(Date.now() + 600_000).toISOString(),
        pollIntervalMs: 60_000
      })
    });
  });
  await page.route("**/api/me/ai-authorizations/authz_test", async (route) => {
    deepSeekStatusPolls += 1;
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
  await page.route("**/api/me/ai-authorizations/authz_test/complete", async (route) => {
    await route.fulfill({
      status: 502,
      contentType: "application/json",
      body: JSON.stringify({ error: { message: "DeepSeek 登录态验证失败，请重新登录 DeepSeek 后复制新的 userToken" } })
    });
  });
  await page.context().route("https://chat.deepseek.com/**", async (route) => {
    await route.fulfill({ contentType: "text/html", body: "<title>DeepSeek</title><main>DeepSeek login</main>" });
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
  const deepSeekConsumerLogin = page.getByRole("radio", { name: /登录 DeepSeek 网页账号/ });
  await expect(deepSeekConsumerLogin).toBeEnabled();
  await expect(deepSeekConsumerLogin).toHaveAttribute("aria-checked", "true");
  await expect(deepSeekConsumerLogin).toContainText("实验");
  await expect(page.locator(".ai-risk-notice")).toContainText("DeepSeek 网页私有协议");
  const setup = page.locator(".ai-setup-panel");
  let unexpectedPopupCount = 0;
  const countUnexpectedPopup = async (popup: Page) => {
    unexpectedPopupCount += 1;
    await popup.close();
  };
  page.on("popup", countUnexpectedPopup);
  await page.getByLabel(/TokHub 登录密码/).fill("wrong-password");
  await page.locator(".ai-experimental-confirm input").check();
  await setup.locator('button[type="submit"]').click();
  await expect(setup.getByRole("alert")).toContainText("当前账号密码验证失败");
  await expect.poll(() => unexpectedPopupCount).toBe(0);
  page.off("popup", countUnexpectedPopup);

  await page.getByLabel(/TokHub 登录密码/).fill("local-password");
  const deepSeekLoginLink = page.getByRole("link", { name: "1. 打开 DeepSeek 登录", exact: true });
  await expect(deepSeekLoginLink).toHaveAttribute("href", "https://chat.deepseek.com");
  const deepSeekLoginPagePromise = page.waitForEvent("popup");
  await deepSeekLoginLink.click();
  const deepSeekLoginPage = await deepSeekLoginPagePromise;
  await expect(deepSeekLoginPage).toHaveURL("https://chat.deepseek.com/");
  await deepSeekLoginPage.close();
  await page.getByRole("button", { name: "2. 我已登录，继续识别", exact: true }).click();
  await expect(page.getByText("登录 DeepSeek 并导入当前登录态")).toBeVisible();
  await expect(page.getByText("浏览器阻止了授权窗口，请使用下方按钮继续。")).toHaveCount(0);
  await expect(page.getByText("复制当前账号的 userToken")).toBeVisible();
  await expect(page.getByText('copy(JSON.parse(localStorage.getItem("userToken")).value)')).toBeVisible();
  const tokenInput = page.getByPlaceholder("粘贴 userToken 的 value");
  await expect(tokenInput).toHaveAttribute("type", "password");
  const recognizeButton = page.getByRole("button", { name: "识别登录态并连接", exact: true });
  await expect(recognizeButton).toBeDisabled();
  await tokenInput.fill("eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ0ZXN0In0.signature");
  await expect(recognizeButton).toBeEnabled();
  await recognizeButton.click();
  await expect(page.getByText(/本次识别已结束，请重新点击“打开 DeepSeek 并开始”/)).toBeVisible();
  await expect(page.getByText("登录 DeepSeek 并导入当前登录态")).toHaveCount(0);
  await page.waitForTimeout(1_800);
  expect(deepSeekStatusPolls).toBe(0);

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

function authMethod(
  code: string,
  label: string,
  release: string,
  completionMode = "api_key",
  enabled = true,
  unavailableReason?: string,
  riskNotice?: string
) {
  return {
    code,
    label,
    release,
    sharingScope: "personal",
    completionMode,
    enabled,
    description: `${label} 测试说明`,
    ...(unavailableReason ? { unavailableReason } : {}),
    ...(riskNotice ? { riskNotice } : {}),
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
