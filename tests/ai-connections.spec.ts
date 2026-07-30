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
  let stepUpCalls = 0;
  const deepSeekCompleteTokens: string[] = [];
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
    stepUpCalls += 1;
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ grant: "step_test", expiresAt: new Date(Date.now() + 600_000).toISOString() })
    });
  });
  await page.route("**/api/me/ai-authorizations", async (route) => {
    const request = route.request().postDataJSON() as { method?: string; stepUpGrant?: string };
    const deepSeekWeb = request.method === "deepseek_web_token";
    if (deepSeekWeb) {
      expect(request.stepUpGrant).toBeUndefined();
    }
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
    const request = route.request().postDataJSON() as { deepSeekToken?: string };
    deepSeekCompleteTokens.push(request.deepSeekToken || "");
    if (request.deepSeekToken === "extension-token-value-for-deepseek-session") {
      await route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ connection: { id: "aic_extension_test" }, authorizationId: "authz_test" })
      });
      return;
    }
    await route.fulfill({
      status: 502,
      contentType: "application/json",
      body: JSON.stringify({ error: { message: "DeepSeek 登录态验证失败，请重新登录 DeepSeek 后复制新的 userToken" } })
    });
  });
  await page.context().route("https://chat.deepseek.com/**", async (route) => {
    await route.fulfill({ contentType: "text/html", body: "<title>DeepSeek</title><main>DeepSeek login</main>" });
  });
  await page.addInitScript(() => {
    const browserWindow = window as typeof window & {
      __TOKHUB_DEEPSEEK_EXTENSION_TEST__: { status: string; token?: string };
    };
    browserWindow.__TOKHUB_DEEPSEEK_EXTENSION_TEST__ = { status: "not_logged_in" };
    window.addEventListener("message", (event) => {
      if (
        event.source !== window ||
        event.data?.source !== "tokhub-web" ||
        event.data?.type !== "TOKHUB_DEEPSEEK_SESSION_REQUEST"
      ) {
        return;
      }
      window.postMessage({
        source: "tokhub-extension",
        type: "TOKHUB_DEEPSEEK_SESSION_RESPONSE",
        version: 1,
        requestId: event.data.requestId,
        ...browserWindow.__TOKHUB_DEEPSEEK_EXTENSION_TEST__
      }, window.location.origin);
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
  const geminiProjectID = page.getByLabel("Google Cloud Project ID");
  await expect(geminiProjectID).toBeVisible();
  await expect(geminiProjectID).toHaveAttribute("pattern", "[a-z][a-z0-9-]{4,28}[a-z0-9]");
  await expect(geminiProjectID).toHaveAttribute("maxlength", "30");
  await expect(page.getByText(/Google 账号需要拥有该项目的 Service Usage Consumer 权限/)).toBeVisible();
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
  await expect(setup.getByLabel(/TokHub 登录密码/)).toHaveCount(0);
  await page.locator(".ai-experimental-confirm input").check();

  const deepSeekLoginLink = page.getByRole("link", { name: "1. 打开 DeepSeek 登录", exact: true });
  await expect(deepSeekLoginLink).toHaveAttribute("href", "https://chat.deepseek.com");
  const deepSeekLoginPagePromise = page.waitForEvent("popup");
  await deepSeekLoginLink.click();
  const deepSeekLoginPage = await deepSeekLoginPagePromise;
  await expect(deepSeekLoginPage).toHaveURL("https://chat.deepseek.com/");
  await deepSeekLoginPage.close();
  await expect(page.getByRole("link", { name: "下载 TokHub AI 登录助手", exact: true }))
    .toHaveAttribute("href", "/downloads/tokhub-ai-login-helper.zip");
  await page.getByRole("button", { name: "2. 一键读取当前登录态", exact: true }).click();
  await expect(page.getByText("识别 DeepSeek 当前登录态")).toBeVisible();
  await expect(page.getByText(/已找到 DeepSeek 网页，但没有读取到可用登录态/)).toBeVisible();
  expect(stepUpCalls).toBe(0);
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
  await expect(page.getByText(/本次识别已结束，请重新点击“一键读取当前登录态”/)).toBeVisible();
  await expect(page.getByText("识别 DeepSeek 当前登录态")).toHaveCount(0);

  await page.evaluate(() => {
    const browserWindow = window as typeof window & {
      __TOKHUB_DEEPSEEK_EXTENSION_TEST__: { status: string; token?: string };
    };
    browserWindow.__TOKHUB_DEEPSEEK_EXTENSION_TEST__ = {
      status: "ok",
      token: "extension-token-value-for-deepseek-session"
    };
  });
  await page.getByRole("button", { name: "2. 一键读取当前登录态", exact: true }).click();
  await expect(page.getByText("DeepSeek 登录态验证通过，凭证已加密保存，个人连接已经可用。")).toBeVisible();
  expect(deepSeekCompleteTokens).toEqual([
    "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ0ZXN0In0.signature",
    "extension-token-value-for-deepseek-session"
  ]);
  expect(deepSeekStatusPolls).toBe(0);

  await page.setViewportSize({ width: 375, height: 812 });
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth)).toBe(375);
});

test("ChatGPT login helper reads the localhost OAuth callback and completes the personal connection", async ({ page }) => {
  const suffix = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  const authorizationID = "authz_123e4567-e89b-42d3-a456-426614174000";
  const callbackURL = `http://localhost:1455/auth/callback?code=chatgpt-code&state=${authorizationID}.state`;
  const completedCallbacks: string[] = [];
  const providers = [
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
    await route.fulfill({ contentType: "application/json", body: JSON.stringify({ items: [] }) });
  });
  await page.route("**/api/me/ai-auth/step-up", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ grant: "step_chatgpt", expiresAt: new Date(Date.now() + 600_000).toISOString() })
    });
  });
  await page.route("**/api/me/ai-authorizations", async (route) => {
    const request = route.request().postDataJSON() as { provider?: string; method?: string; stepUpGrant?: string };
    expect(request).toMatchObject({ provider: "openai", method: "codex_oauth", stepUpGrant: "step_chatgpt" });
    await route.fulfill({
      status: 201,
      contentType: "application/json",
      body: JSON.stringify({
        id: authorizationID,
        authorizationUrl: `${new URL(page.url()).origin}/healthz`,
        completionMode: "paste_callback",
        expiresAt: new Date(Date.now() + 600_000).toISOString(),
        pollIntervalMs: 60_000
      })
    });
  });
  await page.route(`**/api/me/ai-authorizations/${authorizationID}/complete`, async (route) => {
    const request = route.request().postDataJSON() as { callbackUrl?: string };
    completedCallbacks.push(request.callbackUrl || "");
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ connection: { id: "aic_chatgpt" }, authorizationId: authorizationID })
    });
  });
  await page.route(`**/api/me/ai-authorizations/${authorizationID}`, async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        authorization: {
          id: authorizationID,
          provider: "openai",
          authMethod: "codex_oauth",
          status: "authorization_pending",
          completionMode: "paste_callback",
          startedAt: new Date().toISOString(),
          expiresAt: new Date(Date.now() + 600_000).toISOString()
        }
      })
    });
  });
  await page.addInitScript((resultURL) => {
    window.addEventListener("message", (event) => {
      if (
        event.source !== window ||
        event.data?.source !== "tokhub-web" ||
        event.data?.type !== "TOKHUB_CHATGPT_CALLBACK_REQUEST"
      ) {
        return;
      }
      if (event.data.authorizationId !== new URL(resultURL).searchParams.get("state")?.split(".")[0]) {
        return;
      }
      window.postMessage({
        source: "tokhub-extension",
        type: "TOKHUB_CHATGPT_CALLBACK_RESPONSE",
        version: 1,
        requestId: event.data.requestId,
        status: "ok",
        callbackUrl: resultURL
      }, window.location.origin);
    });
  }, callbackURL);

  await page.goto("/login?next=%2Fconsole%2Fconnections");
  await page.getByRole("button", { name: "注册新账号", exact: true }).click();
  await page.getByLabel("邮箱").fill(`ai-chatgpt-${suffix}@example.test`);
  await page.getByLabel("设置密码").fill(`AIChatGPT-${suffix}!`);
  await page.getByRole("button", { name: "创建账号并进入控制台 →", exact: true }).click();
  await page.waitForURL((url) => url.pathname === "/console/connections");

  await page.getByRole("button", { name: /ChatGPT/ }).click();
  await expect(page.getByRole("link", { name: "安装 TokHub AI 登录助手", exact: true }))
    .toHaveAttribute("href", "/downloads/tokhub-ai-login-helper.zip");
  await page.getByLabel(/TokHub 登录密码/).fill("current-password");
  await page.locator(".ai-experimental-confirm input").check();
  const popupPromise = page.waitForEvent("popup");
  await page.getByRole("button", { name: "打开登录授权", exact: true }).click();
  const popup = await popupPromise;
  await popup.close();

  await expect(page.getByText("请在新窗口完成 ChatGPT 登录")).toBeVisible();
  await expect(page.getByPlaceholder("http://localhost:1455/auth/callback?code=…&state=…")).toBeVisible();
  await page.getByRole("button", { name: "2. 一键识别授权结果", exact: true }).click();
  await expect(page.getByText("ChatGPT 授权、凭证加密保存和模型验证已完成。")).toBeVisible();
  expect(completedCallbacks).toEqual([callbackURL]);
});

test("Gemini explains the deployment dependency when Google OAuth is not ready", async ({ page }) => {
  const suffix = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  const providers = [
    provider("gemini", "Gemini", [
      authMethod("api_key", "官方 API Key", "stable"),
      authMethod(
        "oauth",
        "使用 Google 账号授权",
        "stable",
        "redirect_callback",
        false,
        "部署端需要配置 Google OAuth Client ID 与 Secret。"
      )
    ])
  ];
  await page.route("**/api/me/ai-connection-providers", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ items: providers, policyVersion: "ai-authorization-v2", credentialPolicy: { accepted: [], rejected: [] } })
    });
  });
  await page.route("**/api/me/ai-connections", async (route) => {
    await route.fulfill({ contentType: "application/json", body: JSON.stringify({ items: [] }) });
  });

  await page.goto("/login?next=%2Fconsole%2Fconnections");
  await page.getByRole("button", { name: "注册新账号", exact: true }).click();
  await page.getByLabel("邮箱").fill(`ai-gemini-readiness-${suffix}@example.test`);
  await page.getByLabel("设置密码").fill(`AIGemini-${suffix}!`);
  await page.getByRole("button", { name: "创建账号并进入控制台 →", exact: true }).click();
  await page.waitForURL((url) => url.pathname === "/console/connections");

  await page.getByRole("button", { name: /Gemini/ }).click();
  await expect(page.getByRole("radio", { name: /使用 Google 账号授权/ })).toBeDisabled();
  const readiness = page.getByRole("region", { name: "Gemini OAuth 配置状态" });
  await expect(readiness.getByText("Gemini Google OAuth 等待部署配置")).toBeVisible();
  await expect(readiness).toContainText("Google OAuth Client ID 与 Secret");
  await expect(page.getByLabel("Gemini API Key")).toBeVisible();
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

test("OpenCLI browser entry guides pairing and creates a personal DeepSeek connection", async ({ page }) => {
  const suffix = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  const now = new Date().toISOString();
  let connectionRequest: Record<string, unknown> | null = null;
  let browserRiskState = "normal";
  const providers = ["openai", "gemini", "deepseek"].map((code) => provider(
    code,
    code === "openai" ? "ChatGPT" : code === "gemini" ? "Gemini" : "DeepSeek",
    [
      authMethod("api_key", "官方 API Key", "stable"),
      authMethod(
        "opencli_browser",
        "连接本机已登录网页",
        "experimental",
        "local_connector",
        true,
        undefined,
        "网页登录态保留在本机，连接仅限本人低频使用。"
      )
    ]
  ));
  const connector = {
    id: "aibc_test",
    orgId: "org_test",
    displayName: "我的 Chrome",
    status: "active",
    online: true,
    opencliVersion: "1.8.6",
    extensionVersion: "1.8.6",
    capabilities: ["openai", "gemini", "deepseek"],
    lastSeenAt: now,
    pairedAt: now,
    createdAt: now,
    updatedAt: now
  };
  await page.route("**/api/me/ai-connection-providers", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        items: providers,
        policyVersion: "ai-authorization-v2",
        credentialPolicy: { accepted: [], rejected: [] }
      })
    });
  });
  await page.route("**/api/me/ai-browser-connectors", async (route) => {
    if (route.request().method() === "GET") {
      await route.fulfill({ contentType: "application/json", body: JSON.stringify({ items: [connector] }) });
      return;
    }
    await route.fulfill({
      status: 201,
      contentType: "application/json",
      body: JSON.stringify({
        connector: { ...connector, id: "aibc_pending", status: "pending", online: false },
        pairingCode: "review-pairing-code",
        pairCommand: "tokhub-opencli-connector pair --server 'https://tokhub.example.test' --code 'review-pairing-code'"
      })
    });
  });
  await page.route("**/api/me/ai-connections", async (route) => {
    await route.fulfill({ contentType: "application/json", body: JSON.stringify({ items: [] }) });
  });
  await page.route("**/api/me/ai-browser-connections", async (route) => {
    connectionRequest = route.request().postDataJSON() as Record<string, unknown>;
    await route.fulfill({
      status: 201,
      contentType: "application/json",
      body: JSON.stringify({
        connection: {
          id: "aic_browser_test",
          orgId: "org_test",
          provider: "deepseek",
          productLine: "DeepSeek Web",
          region: "global",
          authMethod: "opencli_browser",
          protocol: "openai_compatible",
          adapterType: "openai-compatible",
          endpoint: "browser+opencli://aibc_test/deepseek",
          providerConfig: { connectorId: "aibc_test" },
          displayName: "我的 DeepSeek 网页",
          status: "active",
          authStatus: "active",
          sharingScope: "personal",
          riskLevel: "experimental",
          providerAdapterVersion: "opencli-browser-v1",
          accountMask: "d***@example.test",
          validationStage: "browser_login",
          validationLatencyMs: 80,
          modelCount: 1,
          policyVersion: "ai-authorization-v2",
          secretMask: "Local Browser · d***@example.test",
          models: [{
            id: "aicm_browser_test",
            connectionId: "aic_browser_test",
            providerModelId: "deepseek-web",
            displayName: "deepseek-web",
            enabled: true,
            verificationStatus: "verified",
            validationLatencyMs: 80,
            capabilities: {},
            createdAt: now,
            updatedAt: now
          }],
          createdAt: now,
          updatedAt: now
        }
      })
    });
  });
  await page.route("**/api/me/ai-connections/aic_browser_test/browser-risk**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith("/pause")) browserRiskState = "paused";
    if (path.endsWith("/resume")) browserRiskState = "normal";
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        risk: {
          provider: "deepseek",
          state: browserRiskState,
          requestsHour: 2,
          requestsDay: 7,
          rateLimitEvents: 0,
          consecutiveFailures: 0,
          hourWindowStartedAt: now,
          dayWindowStartedAt: now,
          lastSuccessAt: now,
          updatedAt: now,
          hourlyLimit: 20,
          dailyLimit: 80,
          minimumIntervalSeconds: 15
        }
      })
    });
  });

  await page.goto("/login?next=%2Fconsole%2Fconnections");
  await page.getByRole("button", { name: "注册新账号", exact: true }).click();
  await page.getByLabel("邮箱").fill(`ai-opencli-${suffix}@example.test`);
  await page.getByLabel("设置密码").fill(`AIOpenCLI-${suffix}!`);
  await page.getByRole("button", { name: "创建账号并进入控制台 →", exact: true }).click();
  await page.waitForURL((url) => url.pathname === "/console/connections");

  const connectorPanel = page.getByRole("region", { name: "连接本机已登录的 AI 网页" });
  await expect(connectorPanel).toBeVisible();
  await expect(connectorPanel).toContainText("我的 Chrome");
  await expect(connectorPanel.getByText("OpenCLI 1.8.6")).toBeVisible();
  await connectorPanel.getByRole("button", { name: "＋ 添加本机连接器", exact: true }).click();
  await expect(connectorPanel.getByText("一次性配对命令")).toBeVisible();
  await expect(connectorPanel.getByRole("link", { name: "安装 OpenCLI ↗", exact: true }))
    .toHaveAttribute("href", "https://github.com/jackwener/OpenCLI");

  await page.getByRole("button", { name: /ChatGPT/ }).click();
  await page.getByRole("radio", { name: /连接本机已登录网页/ }).click();
  await expect(page.getByRole("link", { name: "1. 打开 ChatGPT 登录 ↗", exact: true }))
    .toHaveAttribute("href", "https://auth.openai.com/log-in");
  await expect(page.getByLabel("本机连接器")).toHaveValue("aibc_test");
  await expect(page.locator(".ai-model-input")).toHaveValue("chatgpt-web");

  await page.getByRole("button", { name: /Gemini/ }).click();
  await page.getByRole("radio", { name: /连接本机已登录网页/ }).click();
  await expect(page.getByRole("link", { name: "1. 打开 Gemini 登录 ↗", exact: true }))
    .toHaveAttribute("href", "https://accounts.google.com/ServiceLogin?continue=https%3A%2F%2Fgemini.google.com%2F");
  await expect(page.getByLabel("本机连接器")).toHaveValue("aibc_test");
  await expect(page.locator(".ai-model-input")).toHaveValue("gemini-web");

  await page.getByRole("button", { name: /DeepSeek/ }).click();
  await page.getByRole("radio", { name: /连接本机已登录网页/ }).click();
  await expect(page.getByRole("link", { name: "1. 打开 DeepSeek 登录 ↗", exact: true }))
    .toHaveAttribute("href", "https://chat.deepseek.com/sign_in");
  await expect(page.getByLabel("本机连接器")).toHaveValue("aibc_test");
  await page.locator(".ai-experimental-confirm input").check();
  await page.getByRole("button", { name: "2. 已登录，识别并连接", exact: true }).click();
  await expect(page.getByText(/已通过本机浏览器识别账号/)).toBeVisible();
  expect(connectionRequest).toEqual({
    connectorId: "aibc_test",
    provider: "deepseek",
    displayName: "我的 DeepSeek",
    models: ["deepseek-web"],
    termsAckVersion: "opencli-personal-browser-experimental-v1"
  });
  const safetyGovernor = page.getByRole("region", { name: "个人浏览器账号保护状态" });
  await expect(safetyGovernor).toContainText("账号保护正常");
  await expect(safetyGovernor).toContainText("2 / 20");
  await expect(safetyGovernor).toContainText("7 / 80");
  page.once("dialog", (dialog) => void dialog.accept());
  await safetyGovernor.getByRole("button", { name: "立即暂停", exact: true }).click();
  await expect(safetyGovernor).toContainText("已手动暂停");
  page.once("dialog", (dialog) => void dialog.accept());
  await safetyGovernor.getByRole("button", { name: "恢复中转", exact: true }).click();
  await expect(safetyGovernor).toContainText("账号保护正常");

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
