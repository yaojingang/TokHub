import { expect, Page, test } from "@playwright/test";
import { adminEmail, adminLogin, adminPassword } from "./helpers";

async function writeJSON(page: Page, path: string, method: string, body?: unknown) {
  const token = await page.evaluate(async () => {
    const response = await fetch("/api/auth/csrf", { credentials: "include" });
    return ((await response.json()) as { csrfToken: string }).csrfToken;
  });
  return page.evaluate(
    async ({ path: targetPath, method: targetMethod, body: payload, token: csrfToken }) => {
      const response = await fetch(targetPath, {
        method: targetMethod,
        credentials: "include",
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": csrfToken
        },
        body: payload === undefined ? undefined : JSON.stringify(payload)
      });
      return { ok: response.ok, status: response.status, payload: await response.json() };
    },
    { path, method, body, token }
  );
}

async function readJSON(page: Page, path: string) {
  return page.evaluate(async (targetPath) => {
    const response = await fetch(targetPath, { credentials: "include", cache: "no-store" });
    return { ok: response.ok, status: response.status, payload: await response.json() };
  }, path);
}

async function createVerifiedUser(page: Page, email: string, name: string) {
  const created = await writeJSON(page, "/api/admin/users", "POST", {
    email,
    password: "admin@tokhub.local",
    name,
    role: "user",
    status: "active",
    emailVerified: true,
    dataOrigin: "runtime"
  });
  expect(created.status).toBe(201);
}

async function loginAs(page: Page, email: string) {
  const loggedIn = await writeJSON(page, "/api/auth/login", "POST", {
    email,
    password: "admin@tokhub.local"
  });
  expect(loggedIn.ok).toBeTruthy();
}

async function createConsolePrivateChannel(page: Page, name: string) {
  const slug = name.toLowerCase().replace(/[^a-z0-9]+/g, "-");
  const created = await writeJSON(page, "/api/me/private-channels", "POST", {
    name,
    provider: "OpenAI",
    type: "openai-compatible",
    model: "gpt-4o-mini",
    endpoint: `https://${slug}.example/v1`,
    apiKey: `sk-${slug}`,
    probeDaily: 5
  });
  expect(created.status).toBe(201);
  return created.payload.channel.id as string;
}

async function createConsoleGatewayStack(page: Page, suffix: number | string, label: string) {
  const channelID = await createConsolePrivateChannel(page, `Console ${label} Usage Channel ${suffix}`);
  const probe = await writeJSON(page, `/api/me/private-channels/${channelID}/probe-now`, "POST", {});
  expect(probe.ok).toBeTruthy();
  const gateway = await writeJSON(page, "/api/console/gateways", "POST", {
    name: `Console ${label} Usage Gateway ${suffix}`,
    policy: "latency",
    upstreamIds: [channelID],
    qpsLimit: 20,
    quotaMonth: 1000
  });
  expect(gateway.status).toBe(201);
  const gatewayID = gateway.payload.gateway.id as string;

  const key = await writeJSON(page, "/api/console/gateway-keys", "POST", {
    gatewayId: gatewayID,
    name: `Console ${label} Usage Key ${suffix}`,
    quotaMonth: 100,
    qpsLimit: 10
  });
  expect(key.status).toBe(201);
  const plainKey = key.payload.key.plainKey as string;

  const call = await page.evaluate(async ({ apiKey, model }) => {
    const response = await fetch("/gateway/v1/chat/completions", {
      method: "POST",
      headers: {
        "Authorization": `Bearer ${apiKey}`,
        "Content-Type": "application/json"
      },
      body: JSON.stringify({
        model,
        messages: [{ role: "user", content: "usage governance ping" }]
      })
    });
    return { status: response.status, text: await response.text() };
  }, { apiKey: plainKey, model: "gpt-4o-mini" });
  expect(call.status).toBeGreaterThanOrEqual(200);

  return {
    channelID,
    gatewayID,
    keyID: key.payload.key.id as string,
    plainKey,
    gatewayName: gateway.payload.gateway.name as string
  };
}

test("console bulk governance endpoints reject empty selections", async ({ page }) => {
  await adminLogin(page, "/admin/users");
  const suffix = Date.now();
  const email = `console-empty-bulk-${suffix}@tokhub.run`;
  await createVerifiedUser(page, email, `Console Empty Bulk ${suffix}`);
  await loginAs(page, email);

  const emptyFavorites = await writeJSON(page, "/api/me/favorites/bulk", "POST", { action: "delete", ids: [] });
  expect(emptyFavorites.status).toBe(400);
  expect(emptyFavorites.payload.error.code).toBe("empty_bulk_selection");

  const blankFavorites = await writeJSON(page, "/api/me/favorites/bulk", "POST", { action: "delete", ids: [" "] });
  expect(blankFavorites.status).toBe(400);
  expect(blankFavorites.payload.error.code).toBe("empty_bulk_selection");

  const emptyPrivateChannels = await writeJSON(page, "/api/me/private-channels/bulk", "POST", { action: "delete", ids: [] });
  expect(emptyPrivateChannels.status).toBe(400);
  expect(emptyPrivateChannels.payload.error.code).toBe("empty_bulk_selection");

  const blankPrivateChannels = await writeJSON(page, "/api/me/private-channels/bulk", "POST", { action: "disable", ids: ["\n"] });
  expect(blankPrivateChannels.status).toBe(400);
  expect(blankPrivateChannels.payload.error.code).toBe("empty_bulk_selection");

  const blankGateways = await writeJSON(page, "/api/console/gateways/bulk", "POST", { action: "status", ids: [" "], status: "paused" });
  expect(blankGateways.status).toBe(400);
  expect(blankGateways.payload.error.code).toBe("empty_bulk_selection");

  const blankGatewayKeys = await writeJSON(page, "/api/console/gateway-keys/bulk", "POST", { action: "revoke", ids: ["\t"] });
  expect(blankGatewayKeys.status).toBe(400);
  expect(blankGatewayKeys.payload.error.code).toBe("empty_bulk_selection");

  const blankMembers = await writeJSON(page, "/api/console/members/bulk", "POST", { action: "role", ids: ["\n"], role: "operator" });
  expect(blankMembers.status).toBe(400);
  expect(blankMembers.payload.error.code).toBe("empty_bulk_selection");

  const emptyAlertRules = await writeJSON(page, "/api/console/alerts/rules/bulk", "POST", { action: "disable", ids: [] });
  expect(emptyAlertRules.status).toBe(400);
  expect(emptyAlertRules.payload.error.code).toBe("empty_bulk_selection");

  const blankAlertRules = await writeJSON(page, "/api/console/alerts/rules/bulk", "POST", { action: "enable", ids: [" "] });
  expect(blankAlertRules.status).toBe(400);
  expect(blankAlertRules.payload.error.code).toBe("empty_bulk_selection");

  const emptyNotificationChannels = await writeJSON(page, "/api/console/alerts/channels/bulk", "POST", { action: "disable", ids: [] });
  expect(emptyNotificationChannels.status).toBe(400);
  expect(emptyNotificationChannels.payload.error.code).toBe("empty_bulk_selection");

  const blankNotificationChannels = await writeJSON(page, "/api/console/alerts/channels/bulk", "POST", { action: "enable", ids: ["\t"] });
  expect(blankNotificationChannels.status).toBe(400);
  expect(blankNotificationChannels.payload.error.code).toBe("empty_bulk_selection");

  const emptyIncidents = await writeJSON(page, "/api/console/incidents/bulk", "POST", { action: "close", ids: [] });
  expect(emptyIncidents.status).toBe(400);
  expect(emptyIncidents.payload.error.code).toBe("empty_bulk_selection");

  const blankIncidents = await writeJSON(page, "/api/console/incidents/bulk", "POST", { action: "delete", ids: ["\n"] });
  expect(blankIncidents.status).toBe(400);
  expect(blankIncidents.payload.error.code).toBe("empty_bulk_selection");
});

test("console channel deep link preserves login next target", async ({ page }) => {
  await page.goto("/console/channels");
  await page.waitForURL((url) => url.pathname === "/login");
  expect(new URL(page.url()).searchParams.get("next")).toBe("/console/channels");

  await page.getByLabel("邮箱").fill(adminEmail());
  await page.getByLabel("密码").fill(adminPassword());
  await Promise.all([
    page.waitForURL((url) => url.pathname === "/console/channels"),
    page.getByRole("button", { name: "登录 →" }).click()
  ]);
  await expect(page.locator(".sb-link").filter({ hasText: "我的通道" })).toHaveClass(/active/);
  await expect(page.getByRole("heading", { name: /^我的私有通道/ }).first()).toBeVisible();
});

test("console shell preserves query and hash in login next target", async ({ page }) => {
  const target = "/console/usage?range=7d&gateway=gw_filter_probe#cost";
  await page.goto(target);
  await page.waitForURL((url) => url.pathname === "/login");
  expect(new URL(page.url()).searchParams.get("next")).toBe(target);

  await page.getByLabel("邮箱").fill(adminEmail());
  await page.getByLabel("密码").fill(adminPassword());
  await Promise.all([
    page.waitForURL((url) => `${url.pathname}${url.search}${url.hash}` === target),
    page.getByRole("button", { name: "登录 →" }).click()
  ]);
  await expect(page.locator(".sb-link").filter({ hasText: "用量数据" })).toHaveClass(/active/);
});

test("console private channels and workspace settings are fully operable from the UI", async ({ page }) => {
  await adminLogin(page, "/admin");
  await writeJSON(page, "/api/admin/settings", "PATCH", { registrationOpen: true });

  const suffix = Date.now();
  const ownerEmail = `console-ui-crud-${suffix}@tokhub.run`;
  await createVerifiedUser(page, ownerEmail, `Console UI CRUD ${suffix}`);
  await loginAs(page, ownerEmail);

  await page.goto("/console");
  await page.getByRole("button", { name: /添加私有通道/ }).click();
  await page.getByLabel("通道名称").fill(`UI Private Channel ${suffix}`);
  await page.getByLabel("服务商").selectOption("Anthropic");
  await page.getByLabel("分类").selectOption("anthropic");
  await page.getByLabel("Endpoint URL").fill(`https://ui-private-${suffix}.invalid/v1`);
  await page.getByLabel("API Key").fill(`sk-ui-private-${suffix}`);
  await page.getByLabel("探测模型").fill("claude-3-5-sonnet");
  await page.locator("#f-quota").selectOption("10");
  await page.getByRole("button", { name: "保存并开始探测" }).click();
  await expect(page.locator("body")).toContainText("私有通道已创建");
  await expect(page.getByText(`UI Private Channel ${suffix}`).first()).toBeVisible();

  const createdChannels = await readJSON(page, `/api/me/private-channels?q=${encodeURIComponent(`UI Private Channel ${suffix}`)}`);
  expect(createdChannels.ok).toBeTruthy();
  const created = (createdChannels.payload.items as Array<{ id: string; name: string; probeDaily: number; keyMask: string }>).find((item) => item.name === `UI Private Channel ${suffix}`);
  expect(created).toBeTruthy();
  expect(created?.probeDaily).toBe(10);
  expect(JSON.stringify(createdChannels.payload)).not.toContain(`sk-ui-private-${suffix}`);

  const createdRow = page.locator("tr").filter({ hasText: `UI Private Channel ${suffix}` });
  await createdRow.getByRole("button", { name: "编辑" }).click();
  await page.getByLabel("通道名称").fill(`UI Private Channel Edited ${suffix}`);
  await page.getByLabel("Endpoint URL").fill(`https://ui-private-edited-${suffix}.invalid/v1`);
  await page.getByLabel("探测模型").fill("claude-3-haiku");
  await page.locator("#f-quota").selectOption("100");
  await page.getByRole("button", { name: "保存并开始探测" }).click();
  await expect(page.locator("body")).toContainText("私有通道已更新");
  await expect(page.getByText(`UI Private Channel Edited ${suffix}`).first()).toBeVisible();

  const editedChannels = await readJSON(page, `/api/me/private-channels?q=${encodeURIComponent(`UI Private Channel Edited ${suffix}`)}`);
  expect(editedChannels.ok).toBeTruthy();
  const edited = (editedChannels.payload.items as Array<{ id: string; name: string; endpoint: string; model: string; probeDaily: number; keyMask: string }>).find((item) => item.id === created?.id);
  expect(edited?.endpoint).toBe(`https://ui-private-edited-${suffix}.invalid/v1`);
  expect(edited?.model).toBe("claude-3-haiku");
  expect(edited?.probeDaily).toBe(100);
  expect(JSON.stringify(editedChannels.payload)).not.toContain(`sk-ui-private-${suffix}`);

  const editedRow = page.locator("tr").filter({ hasText: `UI Private Channel Edited ${suffix}` });
  page.once("dialog", (dialog) => void dialog.accept());
  await editedRow.locator('button[title="移除"]').click();
  await expect(page.locator("body")).toContainText("私有通道已移除");
  const afterDelete = await readJSON(page, `/api/me/private-channels?q=${encodeURIComponent(`UI Private Channel Edited ${suffix}`)}`);
  expect((afterDelete.payload.items as Array<{ id: string }>).map((item) => item.id)).not.toContain(created?.id);

  await page.goto("/console/settings");
  const originalSettings = await readJSON(page, "/api/console/settings");
  expect(originalSettings.ok).toBeTruthy();
  const originalGatewayPolicy = originalSettings.payload.workspace.defaultGatewayPolicy;
  await page.locator("label.field").filter({ hasText: "工作区名称" }).locator("input").fill(`UI Workspace ${suffix}`);
  await page.locator("label.field").filter({ hasText: "时区" }).locator("select").selectOption("UTC");
  await page.getByRole("button", { name: "保存设置" }).click();
  await expect(page.locator("body")).toContainText("工作区设置已保存");
  const savedSettings = await readJSON(page, "/api/console/settings");
  expect(savedSettings.payload.workspace.name).toBe(`UI Workspace ${suffix}`);
  expect(savedSettings.payload.workspace.timezone).toBe("UTC");
  expect(savedSettings.payload.workspace.defaultGatewayPolicy).toBe(originalGatewayPolicy);

  page.once("dialog", (dialog) => void dialog.accept());
  await page.getByRole("button", { name: "恢复默认" }).click();
  await expect(page.locator("body")).toContainText("工作区设置已恢复默认");
  const resetSettings = await readJSON(page, "/api/console/settings");
  expect(resetSettings.payload.workspace.name).toBe(`Console UI CRUD ${suffix} 的工作区`);
  expect(resetSettings.payload.workspace.timezone).toBe("Asia/Shanghai");
  expect(resetSettings.payload.workspace.defaultGatewayPolicy).toBe("latency");
});

test("console workspace gateways, keys and members support edit, filters, bulk update and delete", async ({ page }) => {
  await adminLogin(page, "/admin");
  await writeJSON(page, "/api/admin/settings", "PATCH", { registrationOpen: true });

  const suffix = Date.now();
  const ownerEmail = `console-crud-owner-${suffix}@tokhub.run`;
  const memberA = `console-crud-a-${suffix}@tokhub.run`;
  const memberB = `console-crud-b-${suffix}@tokhub.run`;
  await createVerifiedUser(page, ownerEmail, `Console Owner ${suffix}`);
  await createVerifiedUser(page, memberA, `Console Member A ${suffix}`);
  await createVerifiedUser(page, memberB, `Console Member B ${suffix}`);

  const favoriteChannelA = await writeJSON(page, "/api/admin/channels", "POST", {
    name: `Console Favorite A ${suffix}`,
    provider: "OpenAI",
    type: "openai-compatible",
    model: "gpt-4o-mini",
    upstreamModel: "gpt-4o-mini",
    endpoint: `https://console-favorite-a-${suffix}.invalid/v1`,
    apiKey: `sk-console-favorite-a-${suffix}`,
    probeDaily: 20,
    publicVisible: true,
    gatewayEnabled: true,
    enabled: true,
    inputPerMtok: 0.15,
    outputPerMtok: 0.6
  });
  expect(favoriteChannelA.status).toBe(201);
  const favoriteChannelB = await writeJSON(page, "/api/admin/channels", "POST", {
    name: `Console Favorite B ${suffix}`,
    provider: "Anthropic",
    type: "anthropic",
    model: "claude-3-5-sonnet",
    upstreamModel: "claude-3-5-sonnet-20241022",
    endpoint: `https://console-favorite-b-${suffix}.invalid/v1`,
    apiKey: `sk-console-favorite-b-${suffix}`,
    probeDaily: 20,
    publicVisible: true,
    gatewayEnabled: true,
    enabled: true,
    inputPerMtok: 3,
    outputPerMtok: 15
  });
  expect(favoriteChannelB.status).toBe(201);
  const favoriteIDs = [favoriteChannelA.payload.channel.id, favoriteChannelB.payload.channel.id] as string[];

  await loginAs(page, ownerEmail);

  const updatedWorkspace = await writeJSON(page, "/api/console/settings", "PATCH", {
    name: `Console Workspace Custom ${suffix}`,
    timezone: "UTC",
    defaultGatewayPolicy: "cost",
    defaultNotificationChannelId: ""
  });
  expect(updatedWorkspace.ok).toBeTruthy();
  expect(updatedWorkspace.payload.workspace.name).toBe(`Console Workspace Custom ${suffix}`);
  expect(updatedWorkspace.payload.workspace.timezone).toBe("UTC");
  expect(updatedWorkspace.payload.workspace.defaultGatewayPolicy).toBe("cost");

  await page.goto("/console/settings");
  await expect(page.locator("label.field").filter({ hasText: "工作区名称" }).locator("input")).toHaveValue(`Console Workspace Custom ${suffix}`);
  await expect(page.getByRole("button", { name: "恢复默认" })).toBeVisible();
  await expect(page.locator(".org-pick")).toContainText(`Console Owner ${suffix}`);
  await expect(page.locator(".org-pick")).toHaveAttribute("href", "/console/settings#profile");
  await expect(page.locator(".sb-link").filter({ hasText: "专属中转站" }).locator(".mini")).toHaveText(String(updatedWorkspace.payload.workspace.gateways));

  const resetWorkspace = await writeJSON(page, "/api/console/settings/reset", "POST", {});
  expect(resetWorkspace.ok).toBeTruthy();
  expect(resetWorkspace.payload.workspace.name).toBe(`Console Owner ${suffix} 的工作区`);
  expect(resetWorkspace.payload.workspace.timezone).toBe("Asia/Shanghai");
  expect(resetWorkspace.payload.workspace.defaultGatewayPolicy).toBe("latency");
  expect(resetWorkspace.payload.workspace.defaultNotificationChannelId).toBe("");
  const resetAudit = await readJSON(page, "/api/console/audit?action=workspace.settings.reset&objectType=org&limit=10");
  expect(resetAudit.ok).toBeTruthy();
  expect((resetAudit.payload.items as Array<{ objectId: string }>).some((item) => item.objectId === resetWorkspace.payload.workspace.orgId)).toBeTruthy();

  for (const id of favoriteIDs) {
    const added = await writeJSON(page, `/api/me/favorites/${id}`, "PUT", {});
    expect(added.ok).toBeTruthy();
  }
  const missingFavoriteTarget = await writeJSON(page, `/api/me/favorites/ch_missing_${suffix}`, "PUT", {});
  expect(missingFavoriteTarget.ok).toBeFalsy();
  expect(missingFavoriteTarget.status).toBe(404);

  const failedMixedFavoriteBulk = await writeJSON(page, "/api/me/favorites/bulk", "POST", {
    action: "delete",
    ids: [favoriteIDs[0], `ch_missing_favorite_${suffix}`]
  });
  expect(failedMixedFavoriteBulk.ok).toBeFalsy();
  expect(failedMixedFavoriteBulk.status).toBe(404);
  const favoritesAfterFailedBulk = await readJSON(page, "/api/me/favorites");
  expect((favoritesAfterFailedBulk.payload.items as Array<{ id: string }>).map((item) => item.id)).toEqual(expect.arrayContaining(favoriteIDs));

  await page.goto("/console");
  await page.getByPlaceholder("搜索关注通道 / 服务商 / 模型...").fill(`Console Favorite`);
  await expect(page.getByText(`Console Favorite A ${suffix}`)).toBeVisible();
  await expect(page.getByText(`Console Favorite B ${suffix}`)).toBeVisible();
  await page.getByLabel("选择当前筛选的关注通道").check();
  await expect(page.getByRole("button", { name: "批量取消关注" })).toBeVisible();
  const bulkRemovedFavorites = await writeJSON(page, "/api/me/favorites/bulk", "POST", {
    action: "delete",
    ids: favoriteIDs
  });
  expect(bulkRemovedFavorites.ok).toBeTruthy();
  expect((bulkRemovedFavorites.payload.ids as string[]).some((id) => favoriteIDs.includes(id))).toBeFalsy();
  const missingFavoriteDelete = await writeJSON(page, `/api/me/favorites/${favoriteIDs[0]}`, "DELETE", {});
  expect(missingFavoriteDelete.ok).toBeFalsy();
  expect(missingFavoriteDelete.status).toBe(404);

  await page.goto("/console");
  await page.getByRole("button", { name: /添加私有通道/ }).click();
  await page.getByLabel("通道名称").fill(`Console Draft Validate ${suffix}`);
  await page.getByLabel("Endpoint URL").fill("http://127.0.0.1:1/v1");
  await page.getByLabel("API Key").fill(`sk-console-draft-validate-${suffix}`);
  await page.getByLabel("探测模型").fill("gpt-4o-mini");
  await page.getByRole("button", { name: "测试连接" }).click();
  await expect(page.locator("body")).toContainText(/连接测试(失败|通过)/);
  await page.getByRole("button", { name: "知道了" }).click();
  await page.locator(".drawer.open").getByRole("button", { name: "×" }).click();

  const privateChannel = await writeJSON(page, "/api/me/private-channels", "POST", {
    name: `Console CRUD Private ${suffix}`,
    provider: "OpenAI",
    type: "openai-compatible",
    model: "gpt-4o-mini",
    endpoint: `https://console-crud-${suffix}.example/v1`,
    apiKey: `sk-console-crud-${suffix}`,
    probeDaily: 5
  });
  expect(privateChannel.ok).toBeTruthy();
  const channelID = privateChannel.payload.channel.id as string;

  const privateChannelB = await writeJSON(page, "/api/me/private-channels", "POST", {
    name: `Console CRUD Private Bulk ${suffix}`,
    provider: "OpenAI",
    type: "openai-compatible",
    model: "gpt-4o-mini",
    endpoint: `https://console-crud-bulk-${suffix}.example/v1`,
    apiKey: `sk-console-crud-bulk-${suffix}`,
    probeDaily: 5
  });
  expect(privateChannelB.ok).toBeTruthy();
  const channelBID = privateChannelB.payload.channel.id as string;

  const bulkDisabledPrivate = await writeJSON(page, "/api/me/private-channels/bulk", "POST", {
    action: "disable",
    ids: [channelID, channelBID]
  });
  expect(bulkDisabledPrivate.ok).toBeTruthy();
  expect((bulkDisabledPrivate.payload.items as Array<{ id: string; status: string }>).filter((item) => [channelID, channelBID].includes(item.id)).every((item) => item.status === "disabled")).toBeTruthy();

  const bulkEnabledPrivate = await writeJSON(page, "/api/me/private-channels/bulk", "POST", {
    action: "enable",
    ids: [channelID, channelBID]
  });
  expect(bulkEnabledPrivate.ok).toBeTruthy();
  expect((bulkEnabledPrivate.payload.items as Array<{ id: string; status: string }>).filter((item) => [channelID, channelBID].includes(item.id)).every((item) => item.status === "unknown")).toBeTruthy();
  const privateProbe = await writeJSON(page, `/api/me/private-channels/${channelID}/probe-now`, "POST", {});
  expect(privateProbe.ok).toBeTruthy();

  const failedMixedPrivateDisable = await writeJSON(page, "/api/me/private-channels/bulk", "POST", {
    action: "disable",
    ids: [channelID, `pch_missing_${suffix}`]
  });
  expect(failedMixedPrivateDisable.ok).toBeFalsy();
  expect(failedMixedPrivateDisable.status).toBe(404);
  const privateAfterFailedDisable = await readJSON(page, "/api/me/private-channels");
  expect((privateAfterFailedDisable.payload.items as Array<{ id: string; status: string }>).find((item) => item.id === channelID)?.status).toBe("unknown");

  const failedMixedPrivateDelete = await writeJSON(page, "/api/me/private-channels/bulk", "POST", {
    action: "delete",
    ids: [channelID, `pch_missing_delete_${suffix}`]
  });
  expect(failedMixedPrivateDelete.ok).toBeFalsy();
  expect(failedMixedPrivateDelete.status).toBe(404);
  const privateAfterFailedDelete = await readJSON(page, "/api/me/private-channels");
  expect((privateAfterFailedDelete.payload.items as Array<{ id: string }>).map((item) => item.id)).toEqual(expect.arrayContaining([channelID, channelBID]));

  const workspaceGatewayDefault = await writeJSON(page, "/api/console/settings", "PATCH", {
    name: resetWorkspace.payload.workspace.name,
    timezone: "Asia/Shanghai",
    defaultGatewayPolicy: "cost",
    defaultNotificationChannelId: ""
  });
  expect(workspaceGatewayDefault.ok).toBeTruthy();
  expect(workspaceGatewayDefault.payload.workspace.defaultGatewayPolicy).toBe("cost");
  await page.goto("/console/gateways");
  await page.getByPlaceholder("例如：核心生产网关").fill(`Console Wizard Gateway ${suffix}`);
  await page.getByRole("button", { name: /下一步/ }).click();
  await expect(page.getByText(`Console CRUD Private ${suffix}`).first()).toBeVisible();
  await page.getByRole("button", { name: /下一步/ }).click();
  await expect(page.locator("button.opt.sel").filter({ hasText: "成本优先" })).toBeVisible();

  await page.goto("/console");
  await expect(page.getByPlaceholder("搜索私有通道 / 端点 / 模型...")).toBeVisible();
  await page.getByPlaceholder("搜索私有通道 / 端点 / 模型...").fill(`Console CRUD Private ${suffix}`);
  await expect(page.getByText(`Console CRUD Private ${suffix}`).first()).toBeVisible();
  await expect(page.getByRole("button", { name: "批量禁用" })).toBeVisible();
  await expect(page.getByRole("button", { name: "批量启用" })).toBeVisible();
  await expect(page.getByRole("button", { name: "批量删除" })).toBeVisible();
  await expect(page.getByLabel(`选择 Console CRUD Private ${suffix}`, { exact: true })).toBeVisible();

  const gateway = await writeJSON(page, "/api/console/gateways", "POST", {
    name: `Console CRUD Gateway ${suffix}`,
    policy: "latency",
    upstreamIds: [channelID],
    qpsLimit: 10,
    quotaMonth: 1000
  });
  expect(gateway.status).toBe(201);
  const gatewayID = gateway.payload.gateway.id as string;

  const editedGateway = await writeJSON(page, `/api/console/gateways/${gatewayID}`, "PATCH", {
    name: `Console CRUD Gateway Edited ${suffix}`,
    policy: "success",
    status: "paused",
    upstreamIds: [channelID],
    qpsLimit: 20,
    quotaMonth: 2000
  });
  expect(editedGateway.ok).toBeTruthy();
  expect(editedGateway.payload.gateway.name).toContain("Edited");
  expect(editedGateway.payload.gateway.policy).toBe("success");
  expect(editedGateway.payload.gateway.status).toBe("paused");

  const secondGateway = await writeJSON(page, "/api/console/gateways", "POST", {
    name: `Console CRUD Gateway Bulk ${suffix}`,
    policy: "latency",
    upstreamIds: [channelID],
    qpsLimit: 10,
    quotaMonth: 1000
  });
  expect(secondGateway.status).toBe(201);
  const secondGatewayID = secondGateway.payload.gateway.id as string;
  const bulkGateways = await writeJSON(page, "/api/console/gateways/bulk", "POST", {
    action: "status",
    ids: [gatewayID, secondGatewayID],
    status: "active"
  });
  expect(bulkGateways.ok).toBeTruthy();
  expect((bulkGateways.payload.items as Array<{ id: string; status: string }>).filter((item) => [gatewayID, secondGatewayID].includes(item.id)).every((item) => item.status === "active")).toBeTruthy();

  const patchGatewayDeleted = await writeJSON(page, `/api/console/gateways/${gatewayID}`, "PATCH", { status: "deleted" });
  expect(patchGatewayDeleted.ok).toBeFalsy();
  expect(patchGatewayDeleted.status).toBe(400);
  const bulkStatusDeleted = await writeJSON(page, "/api/console/gateways/bulk", "POST", {
    action: "status",
    ids: [gatewayID],
    status: "deleted"
  });
  expect(bulkStatusDeleted.ok).toBeFalsy();
  expect(bulkStatusDeleted.status).toBe(400);
  const gatewayStillVisible = await readJSON(page, "/api/console/gateways");
  expect((gatewayStillVisible.payload.items as Array<{ id: string }>).map((item) => item.id)).toContain(gatewayID);

  const workspaceAfterGateways = await readJSON(page, "/api/console/settings");
  expect(workspaceAfterGateways.ok).toBeTruthy();
  await page.goto("/console/gateways");
  await expect(page.locator(".sb-link").filter({ hasText: "我的通道" }).locator(".mini")).toHaveText(String(workspaceAfterGateways.payload.workspace.privateChannels));
  await expect(page.locator(".sb-link").filter({ hasText: "专属中转站" }).locator(".mini")).toHaveText(String(workspaceAfterGateways.payload.workspace.gateways));
  await expect(page.getByText(`Console CRUD Gateway Edited ${suffix}`)).toBeVisible();
  await expect(page.getByRole("button", { name: "批量暂停" })).toBeVisible();
  await expect(page.getByRole("button", { name: "编辑" }).first()).toBeVisible();
  await page.getByRole("button", { name: "编辑" }).first().click();
  const gatewayStatusOptions = await page.locator("form.drawer.open label").filter({ hasText: "状态" }).locator("select option").allTextContents();
  expect(gatewayStatusOptions).toEqual(["Active", "Paused"]);
  await page.locator("form.drawer.open").getByRole("button", { name: "取消" }).click();

  const key = await writeJSON(page, "/api/console/gateway-keys", "POST", {
    gatewayId: gatewayID,
    name: `Console CRUD Key ${suffix}`,
    quotaMonth: 100,
    qpsLimit: 5
  });
  expect(key.status).toBe(201);
  const keyID = key.payload.key.id as string;
  expect(key.payload.key.plainKey).toContain("sk-th-");

  const editedKey = await writeJSON(page, `/api/console/gateway-keys/${keyID}`, "PATCH", {
    name: `Console CRUD Key Edited ${suffix}`,
    quotaMonth: 200,
    qpsLimit: 10,
    status: "active"
  });
  expect(editedKey.ok).toBeTruthy();
  expect(editedKey.payload.key.name).toContain("Edited");
  expect(JSON.stringify(editedKey.payload)).not.toContain(key.payload.key.plainKey);
  const patchKeyRevoked = await writeJSON(page, `/api/console/gateway-keys/${keyID}`, "PATCH", { status: "revoked" });
  expect(patchKeyRevoked.ok).toBeFalsy();
  expect(patchKeyRevoked.status).toBe(400);
  const bulkKeyStatusRevoked = await writeJSON(page, "/api/console/gateway-keys/bulk", "POST", {
    action: "status",
    ids: [keyID],
    status: "revoked"
  });
  expect(bulkKeyStatusRevoked.ok).toBeFalsy();
  expect(bulkKeyStatusRevoked.status).toBe(400);

  const keyB = await writeJSON(page, "/api/console/gateway-keys", "POST", {
    gatewayId: gatewayID,
    name: `Console CRUD Key Bulk ${suffix}`,
    quotaMonth: 100,
    qpsLimit: 5
  });
  expect(keyB.status).toBe(201);
  const bulkKeys = await writeJSON(page, "/api/console/gateway-keys/bulk", "POST", {
    action: "status",
    ids: [keyID, keyB.payload.key.id],
    status: "expired"
  });
  expect(bulkKeys.ok).toBeTruthy();
  expect((bulkKeys.payload.items as Array<{ id: string; status: string }>).filter((item) => [keyID, keyB.payload.key.id].includes(item.id)).every((item) => item.status === "expired")).toBeTruthy();
  const bulkRevokedKeys = await writeJSON(page, "/api/console/gateway-keys/bulk", "POST", {
    action: "revoke",
    ids: [keyID, keyB.payload.key.id]
  });
  expect(bulkRevokedKeys.ok).toBeTruthy();
  expect((bulkRevokedKeys.payload.items as Array<{ id: string; status: string }>).filter((item) => [keyID, keyB.payload.key.id].includes(item.id)).every((item) => item.status === "revoked")).toBeTruthy();
  const reactivateBulkRevokedKey = await writeJSON(page, `/api/console/gateway-keys/${keyID}`, "PATCH", { status: "active" });
  expect(reactivateBulkRevokedKey.ok).toBeFalsy();
  expect(reactivateBulkRevokedKey.status).toBe(400);

  const workspaceAfterKeys = await readJSON(page, "/api/console/settings");
  expect(workspaceAfterKeys.ok).toBeTruthy();
  await page.goto("/console/keys");
  await expect(page.locator(".sb-link").filter({ hasText: "成员与密钥" }).locator(".mini")).toHaveText(String(workspaceAfterKeys.payload.workspace.activeKeys));
  await expect(page.locator(".sb-link").filter({ hasText: "成员与密钥" })).toHaveClass(/active/);
  await expect(page.getByRole("button", { name: /API Key ·/ })).toHaveClass(/active/);
  await expect(page.getByRole("button", { name: /工作区成员 ·/ })).not.toHaveClass(/active/);
  await expect(page.getByRole("button", { name: "批量改状态" })).toBeVisible();
  await expect(page.getByRole("button", { name: "批量吊销" })).toBeVisible();
  await expect(page.getByRole("button", { name: "批量删除" })).toBeVisible();
  const consoleBulkKeyStatusValues = await page.getByLabel("批量 Key 状态").locator("option").evaluateAll((options) => options.map((option) => (option as HTMLOptionElement).value));
  expect(consoleBulkKeyStatusValues).toEqual(["active", "expired"]);
  await expect(page.getByLabel("批量 Key 状态")).toHaveValue("active");
  await expect(page.getByRole("button", { name: "编辑" }).first()).toBeVisible();
  await page.getByRole("button", { name: "编辑" }).first().click();
  await expect(page.getByLabel("Key 编辑状态")).toBeDisabled();
  await expect(page.locator("form.drawer.open").getByRole("button", { name: "保存 Key" })).toBeDisabled();
  await page.locator("form.drawer.open").getByRole("button", { name: "取消" }).click();

  const invitedA = await writeJSON(page, "/api/console/members", "POST", {
    email: memberA,
    role: "viewer",
    groupName: "Console CRUD"
  });
  expect(invitedA.status).toBe(201);
  const invitedB = await writeJSON(page, "/api/console/members", "POST", {
    email: memberB,
    role: "viewer",
    groupName: "Console CRUD"
  });
  expect(invitedB.status).toBe(201);
  const memberIDs = [invitedA.payload.member.userId, invitedB.payload.member.userId];

  const bulkMembers = await writeJSON(page, "/api/console/members/bulk", "POST", {
    action: "role",
    ids: memberIDs,
    role: "operator"
  });
  expect(bulkMembers.ok).toBeTruthy();
  expect((bulkMembers.payload.members as Array<{ userId: string; role: string }>).filter((item) => memberIDs.includes(item.userId)).every((item) => item.role === "operator")).toBeTruthy();
  const failedMixedMemberRole = await writeJSON(page, "/api/console/members/bulk", "POST", {
    action: "role",
    ids: [memberIDs[0], `usr_missing_member_${suffix}`],
    role: "viewer"
  });
  expect(failedMixedMemberRole.ok).toBeFalsy();
  expect(failedMixedMemberRole.status).toBe(404);
  const consoleMembersAfterFailedBulk = await readJSON(page, "/api/console/members");
  expect((consoleMembersAfterFailedBulk.payload.members as Array<{ userId: string; role: string }>).find((item) => item.userId === memberIDs[0])?.role).toBe("operator");

  await page.goto("/console/members");
  await expect(page.locator(".sb-link").filter({ hasText: "成员与密钥" })).toHaveClass(/active/);
  await expect(page.getByRole("button", { name: /工作区成员 ·/ })).toHaveClass(/active/);
  await expect(page.getByRole("button", { name: /API Key ·/ })).not.toHaveClass(/active/);
  await expect(page.getByRole("button", { name: "批量改角色" })).toBeVisible();
  await expect(page.getByRole("cell", { name: "Console CRUD" }).first()).toBeVisible();
  await page.getByLabel(`分组 ${memberA}`).fill("Console 分组治理");
  await page.getByLabel(`分组 ${memberA}`).blur();
  await expect(page.locator("body")).toContainText("成员分组已更新");
  const consoleMembersAfterGroupEdit = await readJSON(page, "/api/console/members");
  expect((consoleMembersAfterGroupEdit.payload.members as Array<{ userId: string; groupName: string }>).find((item) => item.userId === invitedA.payload.member.userId)?.groupName).toBe("Console 分组治理");

  const removedMembers = await writeJSON(page, "/api/console/members/bulk", "POST", {
    action: "delete",
    ids: memberIDs
  });
  expect(removedMembers.ok).toBeTruthy();
  expect((removedMembers.payload.members as Array<{ userId: string }>).some((item) => memberIDs.includes(item.userId))).toBeFalsy();

  const deletedKeys = await writeJSON(page, "/api/console/gateway-keys/bulk", "POST", {
    action: "delete",
    ids: [keyID, keyB.payload.key.id]
  });
  expect(deletedKeys.ok).toBeTruthy();
  const remainingKeyIDs = new Set((deletedKeys.payload.items as Array<{ id: string }>).map((item) => item.id));
  expect([keyID, keyB.payload.key.id].some((id) => remainingKeyIDs.has(id))).toBeFalsy();
	  const patchDeletedKey = await writeJSON(page, `/api/console/gateway-keys/${keyID}`, "PATCH", { status: "active" });
	  expect(patchDeletedKey.ok).toBeFalsy();
	  expect(patchDeletedKey.status).toBe(404);

	  const keyUnderDeletedGateway = await writeJSON(page, "/api/console/gateway-keys", "POST", {
	    gatewayId: gatewayID,
	    name: `Console Deleted Gateway Key ${suffix}`,
	    quotaMonth: 100,
	    qpsLimit: 5
	  });
	  expect(keyUnderDeletedGateway.status).toBe(201);
	  const keyUnderDeletedGatewayID = keyUnderDeletedGateway.payload.key.id as string;
	  const expiredUnderDeletedGateway = await writeJSON(page, `/api/console/gateway-keys/${keyUnderDeletedGatewayID}`, "PATCH", { status: "expired" });
	  expect(expiredUnderDeletedGateway.ok).toBeTruthy();

	  const deletedGateways = await writeJSON(page, "/api/console/gateways/bulk", "POST", {
	    action: "delete",
	    ids: [gatewayID, secondGatewayID]
	  });
	  expect(deletedGateways.ok).toBeTruthy();
	  expect((deletedGateways.payload.items as Array<{ id: string }>).some((item) => [gatewayID, secondGatewayID].includes(item.id))).toBeFalsy();
	  const keysAfterGatewayDelete = await readJSON(page, "/api/console/gateway-keys");
	  expect((keysAfterGatewayDelete.payload.items as Array<{ id: string }>).map((item) => item.id)).not.toContain(keyUnderDeletedGatewayID);
	  const createKeyForDeletedGateway = await writeJSON(page, "/api/console/gateway-keys", "POST", {
	    gatewayId: gatewayID,
	    name: `Console Blocked Key ${suffix}`,
	    quotaMonth: 100,
	    qpsLimit: 5
	  });
	  expect(createKeyForDeletedGateway.ok).toBeFalsy();
	  expect(createKeyForDeletedGateway.status).toBe(404);
	  const patchKeyUnderDeletedGateway = await writeJSON(page, `/api/console/gateway-keys/${keyUnderDeletedGatewayID}`, "PATCH", { status: "active" });
	  expect(patchKeyUnderDeletedGateway.ok).toBeFalsy();
	  expect(patchKeyUnderDeletedGateway.status).toBe(404);
	  const bulkStatusKeyUnderDeletedGateway = await writeJSON(page, "/api/console/gateway-keys/bulk", "POST", { action: "status", ids: [keyUnderDeletedGatewayID], status: "active" });
	  expect(bulkStatusKeyUnderDeletedGateway.ok).toBeFalsy();
	  expect(bulkStatusKeyUnderDeletedGateway.status).toBe(404);
	  const revokeKeyUnderDeletedGateway = await writeJSON(page, `/api/console/gateway-keys/${keyUnderDeletedGatewayID}/revoke`, "POST", {});
	  expect(revokeKeyUnderDeletedGateway.ok).toBeFalsy();
	  expect(revokeKeyUnderDeletedGateway.status).toBe(404);
	  const bulkRevokeKeyUnderDeletedGateway = await writeJSON(page, "/api/console/gateway-keys/bulk", "POST", { action: "revoke", ids: [keyUnderDeletedGatewayID] });
	  expect(bulkRevokeKeyUnderDeletedGateway.ok).toBeFalsy();
	  expect(bulkRevokeKeyUnderDeletedGateway.status).toBe(404);
	  const cleanupKeyUnderDeletedGateway = await writeJSON(page, `/api/console/gateway-keys/${keyUnderDeletedGatewayID}`, "DELETE", {});
	  expect(cleanupKeyUnderDeletedGateway.ok).toBeTruthy();

  const bulkDeletedPrivate = await writeJSON(page, "/api/me/private-channels/bulk", "POST", {
    action: "delete",
    ids: [channelID, channelBID]
  });
  expect(bulkDeletedPrivate.ok).toBeTruthy();
  expect((bulkDeletedPrivate.payload.items as Array<{ id: string }>).some((item) => [channelID, channelBID].includes(item.id))).toBeFalsy();

  const patchDeletedPrivate = await writeJSON(page, `/api/me/private-channels/${channelID}`, "PATCH", {
    name: `Console CRUD Private Deleted ${suffix}`,
    provider: "OpenAI",
    type: "openai-compatible",
    model: "gpt-4o-mini",
    endpoint: `https://console-crud-deleted-${suffix}.invalid/v1`,
    probeDaily: 5
  });
  expect(patchDeletedPrivate.ok).toBeFalsy();
  expect(patchDeletedPrivate.status).toBe(404);
  const validateDeletedPrivate = await writeJSON(page, `/api/me/private-channels/${channelID}/validate`, "POST", {});
  expect(validateDeletedPrivate.ok).toBeFalsy();
  expect(validateDeletedPrivate.status).toBe(404);
  const probeDeletedPrivate = await writeJSON(page, `/api/me/private-channels/${channelID}/probe-now`, "POST", {});
  expect(probeDeletedPrivate.ok).toBeFalsy();
  expect(probeDeletedPrivate.status).toBe(404);

  const adminBoundary = await readJSON(page, "/api/admin/gateways");
  expect(adminBoundary.status).toBe(403);

  await adminLogin(page, "/admin/channels");
  const adminChannelsAfterPrivateDelete = await readJSON(page, "/api/admin/channels");
  expect((adminChannelsAfterPrivateDelete.payload.private as Array<{ id: string }>).map((item) => item.id)).not.toEqual(expect.arrayContaining([channelID, channelBID]));

  for (const id of favoriteIDs) {
    const deletedFavorite = await writeJSON(page, `/api/admin/channels/${id}`, "DELETE", {});
    expect(deletedFavorite.ok).toBeTruthy();
  }
});

test("console usage and audit support filters, export and workspace isolation", async ({ page }) => {
  await adminLogin(page, "/admin");

  const suffix = Date.now();
  const ownerAEmail = `console-usage-a-${suffix}@tokhub.run`;
  const ownerBEmail = `console-usage-b-${suffix}@tokhub.run`;
  await createVerifiedUser(page, ownerAEmail, `Console Usage A ${suffix}`);
  await createVerifiedUser(page, ownerBEmail, `Console Usage B ${suffix}`);

  await loginAs(page, ownerAEmail);
  const stackA = await createConsoleGatewayStack(page, suffix, "A");
  await writeJSON(page, "/api/console/settings", "PATCH", {
    name: `Console Usage Workspace A ${suffix}`,
    timezone: "UTC",
    defaultGatewayPolicy: "latency",
    defaultNotificationChannelId: ""
  });

  await loginAs(page, ownerBEmail);
  const stackB = await createConsoleGatewayStack(page, suffix, "B");

  await loginAs(page, ownerAEmail);
  const usageByGateway = await readJSON(page, `/api/console/usage?days=7&source=gateway&gatewayId=${encodeURIComponent(stackA.gatewayID)}`);
  expect(usageByGateway.ok).toBeTruthy();
  expect(usageByGateway.payload.totals.requests).toBeGreaterThanOrEqual(1);
  expect((usageByGateway.payload.recent as Array<{ gateway: string }>).some((item) => item.gateway === stackA.gatewayName)).toBeTruthy();
  expect(JSON.stringify(usageByGateway.payload)).not.toContain(stackA.plainKey);
  expect(JSON.stringify(usageByGateway.payload)).not.toContain(stackB.gatewayID);
  expect(JSON.stringify(usageByGateway.payload)).not.toContain(stackB.plainKey);

  const usageByOtherWorkspaceGateway = await readJSON(page, `/api/console/usage?days=7&source=gateway&gatewayId=${encodeURIComponent(stackB.gatewayID)}`);
  expect(usageByOtherWorkspaceGateway.ok).toBeTruthy();
  expect(usageByOtherWorkspaceGateway.payload.totals.requests).toBe(0);
  expect(JSON.stringify(usageByOtherWorkspaceGateway.payload)).not.toContain(stackB.gatewayName);

  const usageByModel = await readJSON(page, `/api/console/usage?days=7&source=gateway&gatewayId=${encodeURIComponent(stackA.gatewayID)}&model=gpt-4o-mini`);
  expect(usageByModel.ok).toBeTruthy();
  expect(usageByModel.payload.totals.requests).toBeGreaterThanOrEqual(1);

  const usageNoMatch = await readJSON(page, `/api/console/usage?days=7&source=gateway&gatewayId=${encodeURIComponent(stackA.gatewayID)}&model=not-a-real-model`);
  expect(usageNoMatch.ok).toBeTruthy();
  expect(usageNoMatch.payload.totals.requests).toBe(0);

  const usageCSV = await page.evaluate(async (url) => {
    const response = await fetch(url, { credentials: "include" });
    return { ok: response.ok, text: await response.text(), contentType: response.headers.get("content-type") };
  }, `/api/console/usage/export?days=7&source=gateway&gatewayId=${encodeURIComponent(stackA.gatewayID)}`);
  expect(usageCSV.ok).toBeTruthy();
  expect(usageCSV.contentType).toContain("text/csv");
  expect(usageCSV.text).toContain("day,org_id,source,gateway_id");
  expect(usageCSV.text).toContain(stackA.gatewayID);
  expect(usageCSV.text).not.toContain(stackA.plainKey);
  expect(usageCSV.text).not.toContain(stackB.gatewayID);
  expect(usageCSV.text).not.toContain(stackB.plainKey);

  await page.goto("/console/usage");
  await page.getByLabel("用量时间范围").selectOption("7");
  await page.getByLabel("来源筛选").selectOption("gateway");
  await page.getByLabel("网关筛选").selectOption(stackA.gatewayID);
  await page.getByRole("button", { name: "应用筛选" }).click();
  await expect(page.locator("body")).toContainText(stackA.gatewayName);
  await expect(page.locator("body")).not.toContainText(stackB.gatewayName);
  await expect(page.getByRole("link", { name: "导出 CSV" })).toHaveAttribute("href", `/api/console/usage/export?days=7&source=gateway&gatewayId=${encodeURIComponent(stackA.gatewayID)}`);
  await expect(page.getByRole("button", { name: "重算 rollup" })).toHaveCount(0);
  await expect.poll(() => new URL(page.url()).searchParams.get("gatewayId")).toBe(stackA.gatewayID);

  await page.goto(`/console/usage?days=7&source=gateway&gatewayId=${encodeURIComponent(stackA.gatewayID)}&model=gpt-4o-mini`);
  await expect(page.locator("body")).toContainText(stackA.gatewayName);
  await expect(page.locator("body")).not.toContainText(stackB.gatewayName);
  await expect(page.getByLabel("用量时间范围")).toHaveValue("7");
  await expect(page.getByLabel("来源筛选")).toHaveValue("gateway");
  await expect(page.getByLabel("网关筛选")).toHaveValue(stackA.gatewayID);
  await expect(page.getByLabel("模型筛选")).toHaveValue("gpt-4o-mini");
  await expect(page.getByRole("link", { name: "导出 CSV" })).toHaveAttribute("href", `/api/console/usage/export?days=7&source=gateway&gatewayId=${encodeURIComponent(stackA.gatewayID)}&model=gpt-4o-mini`);
  await page.getByRole("button", { name: "重置" }).click();
  await expect(page.getByLabel("用量时间范围")).toHaveValue("30");
  await expect(page.getByLabel("来源筛选")).toHaveValue("");
  await expect.poll(() => new URL(page.url()).search).toBe("");

  const auditByObject = await readJSON(page, `/api/console/audit?action=${encodeURIComponent("gateway.created")}&objectType=gateway&objectId=${encodeURIComponent(stackA.gatewayID)}&limit=50`);
  expect(auditByObject.ok).toBeTruthy();
  expect((auditByObject.payload.items as Array<{ objectId: string; action: string }>).some((item) => item.objectId === stackA.gatewayID && item.action === "gateway.created")).toBeTruthy();
  expect(JSON.stringify(auditByObject.payload)).not.toContain(stackA.plainKey);
  expect(JSON.stringify(auditByObject.payload)).not.toContain(stackB.gatewayID);

  const otherWorkspaceAudit = await readJSON(page, `/api/console/audit?action=${encodeURIComponent("gateway.created")}&objectType=gateway&objectId=${encodeURIComponent(stackB.gatewayID)}&limit=50`);
  expect(otherWorkspaceAudit.ok).toBeTruthy();
  expect(otherWorkspaceAudit.payload.total).toBe(0);
  expect(JSON.stringify(otherWorkspaceAudit.payload)).not.toContain(stackB.gatewayID);

  const auditCSV = await page.evaluate(async (url) => {
    const response = await fetch(url, { credentials: "include" });
    return { ok: response.ok, text: await response.text(), contentType: response.headers.get("content-type") };
  }, `/api/console/audit/export?action=${encodeURIComponent("gateway.created")}&objectType=gateway&objectId=${encodeURIComponent(stackA.gatewayID)}&limit=500`);
  expect(auditCSV.ok).toBeTruthy();
  expect(auditCSV.contentType).toContain("text/csv");
  expect(auditCSV.text).toContain("created_at,actor_email,action");
  expect(auditCSV.text).toContain(stackA.gatewayID);
  expect(auditCSV.text).not.toContain(stackA.plainKey);
  expect(auditCSV.text).not.toContain(stackB.gatewayID);
  expect(auditCSV.text).not.toContain(stackB.plainKey);

  await page.goto(`/console/audit?action=${encodeURIComponent("gateway.created")}&objectType=gateway&objectId=${encodeURIComponent(stackA.gatewayID)}&limit=50`);
  await expect(page.getByPlaceholder("对象 ID")).toHaveValue(stackA.gatewayID);
  await expect(page.getByPlaceholder("精确 action")).toHaveValue("gateway.created");
  await expect(page.getByRole("row").filter({ hasText: stackA.gatewayID })).toBeVisible();
  await expect(page.locator("body")).not.toContainText(stackB.gatewayID);
  await expect(page.getByRole("button", { name: "导出 CSV" })).toBeVisible();
  await page.getByRole("row").filter({ hasText: stackA.gatewayID }).getByRole("button", { name: "查看" }).click();
  await expect(page.getByRole("dialog")).toContainText("gateway.created");
  await expect(page.getByRole("dialog")).toContainText(stackA.gatewayID);
  await expect(page.getByRole("dialog")).not.toContainText(stackA.plainKey);
  await page.getByRole("button", { name: "关闭" }).click();
  await page.getByRole("button", { name: "重置" }).click();
  await expect(page.getByPlaceholder("对象 ID")).toHaveValue("");
  await expect(page.getByPlaceholder("精确 action")).toHaveValue("");
  await expect.poll(() => new URL(page.url()).search).toBe("");

  const adminBoundary = await readJSON(page, "/api/admin/usage");
  expect(adminBoundary.status).toBe(403);
});

test("console alert rules, notification channels and incidents support CRUD governance", async ({ page }) => {
  await adminLogin(page, "/admin");

  const suffix = Date.now();
  const ownerEmail = `console-alert-owner-${suffix}@tokhub.run`;
  await createVerifiedUser(page, ownerEmail, `Console Alert Owner ${suffix}`);
  await loginAs(page, ownerEmail);

  async function createPrivateChannel(name: string) {
    const created = await writeJSON(page, "/api/me/private-channels", "POST", {
      name,
      provider: "OpenAI",
      type: "openai-compatible",
      endpoint: `https://${name.toLowerCase().replace(/[^a-z0-9]+/g, "-")}.invalid/v1`,
      apiKey: `sk-${name.toLowerCase().replace(/[^a-z0-9]+/g, "-")}`,
      model: "gpt-4o-mini",
      probeDaily: 5
    });
    expect(created.status).toBe(201);
    const channelID = created.payload.channel.id as string;
    const disabled = await writeJSON(page, "/api/me/private-channels/bulk", "POST", {
      action: "disable",
      ids: [channelID]
    });
    expect(disabled.ok).toBeTruthy();
    return channelID;
  }

  const channelID = await createPrivateChannel(`Console Alert Channel ${suffix}`);

  const createdRule = await writeJSON(page, "/api/console/alerts/rules", "POST", {
    name: `Console Alert Rule ${suffix}`,
    kind: "cost_threshold",
    severity: "warning",
    threshold: 3,
    windowMinutes: 60,
    dedupeMinutes: 30,
    enabled: true
  });
  expect(createdRule.status).toBe(201);
  const ruleID = createdRule.payload.rule.id as string;

  const editedRule = await writeJSON(page, `/api/console/alerts/rules/${ruleID}`, "PATCH", {
    name: `Console Alert Rule Edited ${suffix}`,
    kind: "gateway_error_rate",
    severity: "critical",
    threshold: 25,
    windowMinutes: 120,
    dedupeMinutes: 45,
    enabled: false
  });
  expect(editedRule.ok).toBeTruthy();
  expect(editedRule.payload.rule.name).toContain("Edited");
  expect(editedRule.payload.rule.kind).toBe("gateway_error_rate");
  expect(editedRule.payload.rule.enabled).toBeFalsy();

  const batchRuleIDs: string[] = [];
  for (const index of [1, 2]) {
    const created = await writeJSON(page, "/api/console/alerts/rules", "POST", {
      name: `Console Bulk Alert Rule ${index} ${suffix}`,
      kind: "cost_threshold",
      severity: "warning",
      threshold: index,
      windowMinutes: 60,
      dedupeMinutes: 30,
      enabled: true
    });
    expect(created.status).toBe(201);
    batchRuleIDs.push(created.payload.rule.id as string);
  }

  const disabledRules = await writeJSON(page, "/api/console/alerts/rules/bulk", "POST", { action: "disable", ids: batchRuleIDs });
  expect(disabledRules.ok).toBeTruthy();
  expect((disabledRules.payload.items as Array<{ id: string; enabled: boolean }>).filter((rule) => batchRuleIDs.includes(rule.id)).every((rule) => !rule.enabled)).toBeTruthy();

  const enabledRules = await writeJSON(page, "/api/console/alerts/rules/bulk", "POST", { action: "enable", ids: batchRuleIDs });
  expect(enabledRules.ok).toBeTruthy();
  expect((enabledRules.payload.items as Array<{ id: string; enabled: boolean }>).filter((rule) => batchRuleIDs.includes(rule.id)).every((rule) => rule.enabled)).toBeTruthy();

  const failedMixedRuleDisable = await writeJSON(page, "/api/console/alerts/rules/bulk", "POST", {
    action: "disable",
    ids: [batchRuleIDs[0], `alr_missing_${suffix}`]
  });
  expect(failedMixedRuleDisable.ok).toBeFalsy();
  expect(failedMixedRuleDisable.status).toBe(404);
  const rulesAfterFailedDisable = await readJSON(page, "/api/console/alerts");
  expect((rulesAfterFailedDisable.payload.rules as Array<{ id: string; enabled: boolean }>).find((rule) => rule.id === batchRuleIDs[0])?.enabled).toBeTruthy();

  const failedMixedRuleDelete = await writeJSON(page, "/api/console/alerts/rules/bulk", "POST", {
    action: "delete",
    ids: [batchRuleIDs[0], `alr_missing_delete_${suffix}`]
  });
  expect(failedMixedRuleDelete.ok).toBeFalsy();
  expect(failedMixedRuleDelete.status).toBe(404);
  const rulesAfterFailedDelete = await readJSON(page, "/api/console/alerts");
  expect((rulesAfterFailedDelete.payload.rules as Array<{ id: string }>).map((rule) => rule.id)).toContain(batchRuleIDs[0]);

  const createdChannel = await writeJSON(page, "/api/console/alerts/channels", "POST", {
    name: `Console Notify ${suffix}`,
    type: "email",
    target: `console-ops-${suffix}@tokhub.run`,
    enabled: true
  });
  expect(createdChannel.status).toBe(201);
  const notificationChannelID = createdChannel.payload.channel.id as string;

  const webhookSecret = `console-notify-secret-${suffix}`;
  const editedChannel = await writeJSON(page, `/api/console/alerts/channels/${notificationChannelID}`, "PATCH", {
    name: `Console Notify Edited ${suffix}`,
    type: "webhook",
    target: `https://console-notify.invalid/hook/${webhookSecret}?token=${webhookSecret}`,
    enabled: false
  });
  expect(editedChannel.ok).toBeTruthy();
  expect(editedChannel.payload.channel.name).toContain("Edited");
  expect(editedChannel.payload.channel.type).toBe("webhook");
  expect(editedChannel.payload.channel.enabled).toBeFalsy();
  expect(JSON.stringify(editedChannel.payload.channel)).not.toContain(webhookSecret);
  expect(editedChannel.payload.channel.targetMask).toBe("https://console-notify.invalid/***");

  const preservedChannel = await writeJSON(page, `/api/console/alerts/channels/${notificationChannelID}`, "PATCH", {
    name: `Console Notify Edited ${suffix}`,
    type: "webhook",
    target: "",
    enabled: false
  });
  expect(preservedChannel.ok).toBeTruthy();
  expect(preservedChannel.payload.channel.targetMask).toBe("https://console-notify.invalid/***");
  expect(JSON.stringify(preservedChannel.payload.channel)).not.toContain(webhookSecret);

  const batchChannelIDs: string[] = [];
  for (const index of [1, 2]) {
    const created = await writeJSON(page, "/api/console/alerts/channels", "POST", {
      name: `Console Bulk Notify ${index} ${suffix}`,
      type: "email",
      target: `console-bulk-${index}-${suffix}@tokhub.run`,
      enabled: true
    });
    expect(created.status).toBe(201);
    batchChannelIDs.push(created.payload.channel.id as string);
  }

  const disabledChannels = await writeJSON(page, "/api/console/alerts/channels/bulk", "POST", { action: "disable", ids: batchChannelIDs });
  expect(disabledChannels.ok).toBeTruthy();
  expect((disabledChannels.payload.items as Array<{ id: string; enabled: boolean }>).filter((channel) => batchChannelIDs.includes(channel.id)).every((channel) => !channel.enabled)).toBeTruthy();

  const enabledChannels = await writeJSON(page, "/api/console/alerts/channels/bulk", "POST", { action: "enable", ids: batchChannelIDs });
  expect(enabledChannels.ok).toBeTruthy();
  expect((enabledChannels.payload.items as Array<{ id: string; enabled: boolean }>).filter((channel) => batchChannelIDs.includes(channel.id)).every((channel) => channel.enabled)).toBeTruthy();

  const failedMixedChannelDisable = await writeJSON(page, "/api/console/alerts/channels/bulk", "POST", {
    action: "disable",
    ids: [batchChannelIDs[0], `ntc_missing_${suffix}`]
  });
  expect(failedMixedChannelDisable.ok).toBeFalsy();
  expect(failedMixedChannelDisable.status).toBe(404);
  const channelsAfterFailedDisable = await readJSON(page, "/api/console/alerts");
  expect((channelsAfterFailedDisable.payload.channels as Array<{ id: string; enabled: boolean }>).find((channel) => channel.id === batchChannelIDs[0])?.enabled).toBeTruthy();

  const failedMixedChannelDelete = await writeJSON(page, "/api/console/alerts/channels/bulk", "POST", {
    action: "delete",
    ids: [batchChannelIDs[0], `ntc_missing_delete_${suffix}`]
  });
  expect(failedMixedChannelDelete.ok).toBeFalsy();
  expect(failedMixedChannelDelete.status).toBe(404);
  const channelsAfterFailedDelete = await readJSON(page, "/api/console/alerts");
  expect((channelsAfterFailedDelete.payload.channels as Array<{ id: string }>).map((channel) => channel.id)).toContain(batchChannelIDs[0]);

  await page.goto("/console/alerts");
  await expect(page.locator("body")).toContainText(`Console Alert Rule Edited ${suffix}`);
  await expect(page.getByPlaceholder("筛选规则名称 / 类型 / 级别")).toBeVisible();
  await page.getByPlaceholder("筛选规则名称 / 类型 / 级别").fill(`Console Alert Rule Edited ${suffix}`);
  await page.getByLabel(`选择告警规则 Console Alert Rule Edited ${suffix}`).check();
  await page.getByRole("button", { name: "批量启用" }).first().click();
  await expect(page.locator("body")).toContainText("选中告警规则已启用。");

  await page.getByRole("tab", { name: /通知渠道/ }).click();
  await expect(page.locator("body")).toContainText(`Console Notify Edited ${suffix}`);
  await expect(page.locator("body")).toContainText("https://console-notify.invalid/***");
  await expect(page.locator("body")).not.toContainText(webhookSecret);
  await expect(page.getByPlaceholder("筛选渠道名称 / 类型 / 目标")).toBeVisible();
  await page.getByPlaceholder("筛选渠道名称 / 类型 / 目标").fill(`Console Notify Edited ${suffix}`);
  await page.getByLabel(`选择通知渠道 Console Notify Edited ${suffix}`).check();
  await page.getByRole("button", { name: "批量启用" }).click();
  await expect(page.locator("body")).toContainText("选中通知渠道已启用。");

  const incidentTitle = `Console Incident ${suffix}`;
  await page.getByRole("tab", { name: /Incident 治理/ }).click();
  await page.locator("form.incident-form select").first().selectOption(channelID);
  await page.getByPlaceholder("例如：供应商模型不可用").fill(incidentTitle);
  await page.getByPlaceholder("记录排查背景、影响范围或处置动作").fill("E2E 工作区手动登记 Incident");
  await page.getByRole("button", { name: "登记事件" }).click();
  await expect(page.locator("body")).toContainText("Incident 已登记。");
  await expect(page.getByRole("row").filter({ hasText: incidentTitle })).toBeVisible();

  await page.getByPlaceholder("筛选标题 / 通道 / 状态").fill(incidentTitle);
  await page.getByRole("button", { name: "筛选" }).click();
  await expect(page.getByRole("row").filter({ hasText: incidentTitle })).toBeVisible();
  const createdIncident = await readJSON(page, `/api/console/incidents?q=${encodeURIComponent(incidentTitle)}&status=open`);
  expect(createdIncident.ok).toBeTruthy();
  const incidentID = createdIncident.payload.items[0].id as string;
  const editedIncidentTitle = `Console Incident Edited ${suffix}`;
  await page.getByRole("row").filter({ hasText: incidentTitle }).getByRole("button", { name: "编辑" }).click();
  const incidentForm = page.locator("form").filter({ hasText: "编辑 Incident" });
  await expect(incidentForm).toBeVisible();
  await incidentForm.getByPlaceholder("例如：供应商模型不可用").fill(editedIncidentTitle);
  await incidentForm.locator("select.input").selectOption("auth_error");
  await incidentForm.getByPlaceholder("记录排查背景、影响范围或处置动作").fill("E2E 工作区编辑 Incident");
  await incidentForm.getByRole("button", { name: "更新事件" }).click();
  await expect(page.locator("body")).toContainText("Incident 已更新。");
  const editedIncident = await readJSON(page, `/api/console/incidents?q=${encodeURIComponent(editedIncidentTitle)}&status=auth_error`);
  expect(editedIncident.ok).toBeTruthy();
  expect((editedIncident.payload.items as Array<{ id: string; status: string; title: string }>).some((incident) => incident.id === incidentID && incident.status === "auth_error" && incident.title === editedIncidentTitle)).toBeTruthy();
  await page.getByPlaceholder("筛选标题 / 通道 / 状态").fill(editedIncidentTitle);
  await page.getByRole("button", { name: "筛选" }).click();
  await expect(page.getByRole("row").filter({ hasText: editedIncidentTitle })).toBeVisible();

  await page.getByRole("row").filter({ hasText: editedIncidentTitle }).getByRole("button", { name: "关闭" }).click();
  await expect(page.locator("body")).toContainText("Incident 已关闭。");
  const resolvedIncident = await readJSON(page, `/api/console/incidents?q=${encodeURIComponent(editedIncidentTitle)}&status=resolved`);
  expect((resolvedIncident.payload.items as Array<{ id: string }>).map((incident) => incident.id)).toContain(incidentID);

  await page.getByRole("row").filter({ hasText: editedIncidentTitle }).getByRole("button", { name: "重开" }).click();
  await expect(page.locator("body")).toContainText("Incident 已重开。");
  const reopenedIncident = await readJSON(page, `/api/console/incidents?q=${encodeURIComponent(editedIncidentTitle)}&status=open`);
  expect((reopenedIncident.payload.items as Array<{ id: string }>).map((incident) => incident.id)).toContain(incidentID);

  page.once("dialog", (dialog) => void dialog.accept());
  await page.getByRole("row").filter({ hasText: editedIncidentTitle }).getByRole("button", { name: "删除" }).click();
  await expect(page.locator("body")).toContainText("Incident 已删除。");
  const deletedIncident = await readJSON(page, `/api/console/incidents?q=${encodeURIComponent(editedIncidentTitle)}`);
  expect((deletedIncident.payload.items as Array<{ id: string }>).map((incident) => incident.id)).not.toContain(incidentID);

  const bulkIncidentIDs: string[] = [];
  for (const index of [1, 2]) {
    const bulkChannelID = await createPrivateChannel(`Console Batch Incident Channel ${index} ${suffix}`);
    const created = await writeJSON(page, "/api/console/incidents", "POST", {
      channelId: bulkChannelID,
      status: "manual",
      title: `Console Bulk Incident ${index} ${suffix}`,
      message: "E2E 工作区批量治理 Incident"
    });
    expect(created.status).toBe(201);
    bulkIncidentIDs.push(created.payload.id as string);
  }

  const failedMixedIncidentBulk = await writeJSON(page, "/api/console/incidents/bulk", "POST", {
    action: "close",
    ids: [bulkIncidentIDs[0], `inc_missing_${suffix}`],
    message: "mixed id should not partially close"
  });
  expect(failedMixedIncidentBulk.ok).toBeFalsy();
  expect(failedMixedIncidentBulk.status).toBe(404);
  const incidentsAfterFailedBulk = await readJSON(page, `/api/console/incidents?q=${encodeURIComponent("Console Bulk Incident")}&status=open`);
  expect((incidentsAfterFailedBulk.payload.items as Array<{ id: string; open: boolean }>).filter((incident) => bulkIncidentIDs.includes(incident.id)).every((incident) => incident.open)).toBeTruthy();

  await page.goto("/console/alerts");
  await page.getByRole("tab", { name: /Incident 治理/ }).click();
  await page.getByPlaceholder("筛选标题 / 通道 / 状态").fill("Console Bulk Incident");
  await page.getByRole("button", { name: "筛选" }).click();
  const incidentBoard = page.locator(".card.board").filter({ has: page.getByPlaceholder("筛选标题 / 通道 / 状态") });
  await expect(page.getByRole("row").filter({ hasText: `Console Bulk Incident 1 ${suffix}` })).toBeVisible();
  await expect(page.getByRole("row").filter({ hasText: `Console Bulk Incident 2 ${suffix}` })).toBeVisible();
  await incidentBoard.locator("select.incident-mode-select").selectOption("bulk");
  await page.getByLabel("选择当前页 Incident").check();
  const incidentBulkActions = incidentBoard.locator(".incident-bulk-actions");
  await expect(incidentBulkActions.getByRole("button", { name: "关闭" })).toBeVisible();
  await expect(incidentBulkActions.getByRole("button", { name: "重开" })).toBeVisible();
  await expect(incidentBulkActions.getByRole("button", { name: "删除" })).toBeVisible();

  page.once("dialog", (dialog) => void dialog.accept());
  await incidentBulkActions.getByRole("button", { name: "关闭" }).click();
  await expect(page.locator("body")).toContainText("Incident 已批量关闭。");
  const bulkResolved = await readJSON(page, `/api/console/incidents?q=${encodeURIComponent("Console Bulk Incident")}&status=resolved`);
  expect((bulkResolved.payload.items as Array<{ id: string; open: boolean }>).filter((incident) => bulkIncidentIDs.includes(incident.id)).every((incident) => !incident.open)).toBeTruthy();

  await page.getByLabel("选择当前页 Incident").check();
  page.once("dialog", (dialog) => void dialog.accept());
  await incidentBulkActions.getByRole("button", { name: "重开" }).click();
  await expect(page.locator("body")).toContainText("Incident 已批量重开。");
  const bulkReopened = await readJSON(page, `/api/console/incidents?q=${encodeURIComponent("Console Bulk Incident")}&status=open`);
  expect((bulkReopened.payload.items as Array<{ id: string; open: boolean }>).filter((incident) => bulkIncidentIDs.includes(incident.id)).every((incident) => incident.open)).toBeTruthy();

  await page.getByLabel("选择当前页 Incident").check();
  page.once("dialog", (dialog) => void dialog.accept());
  await incidentBulkActions.getByRole("button", { name: "删除" }).click();
  await expect(page.locator("body")).toContainText("Incident 已批量删除。");
  const bulkDeletedIncidents = await readJSON(page, `/api/console/incidents?q=${encodeURIComponent("Console Bulk Incident")}`);
  expect((bulkDeletedIncidents.payload.items as Array<{ id: string }>).some((incident) => bulkIncidentIDs.includes(incident.id))).toBeFalsy();

  const deletedRule = await writeJSON(page, `/api/console/alerts/rules/${ruleID}`, "DELETE", {});
  expect(deletedRule.ok).toBeTruthy();
  const deletedChannel = await writeJSON(page, `/api/console/alerts/channels/${notificationChannelID}`, "DELETE", {});
  expect(deletedChannel.ok).toBeTruthy();
  const deletedBatchRules = await writeJSON(page, "/api/console/alerts/rules/bulk", "POST", { action: "delete", ids: batchRuleIDs });
  expect(deletedBatchRules.ok).toBeTruthy();
  const deletedBatchChannels = await writeJSON(page, "/api/console/alerts/channels/bulk", "POST", { action: "delete", ids: batchChannelIDs });
  expect(deletedBatchChannels.ok).toBeTruthy();

  const center = await readJSON(page, "/api/console/alerts");
  expect((center.payload.rules as Array<{ id: string }>).map((rule) => rule.id)).not.toContain(ruleID);
  expect((center.payload.channels as Array<{ id: string }>).map((channel) => channel.id)).not.toContain(notificationChannelID);
  for (const id of batchRuleIDs) {
    expect((center.payload.rules as Array<{ id: string }>).map((rule) => rule.id)).not.toContain(id);
  }
  for (const id of batchChannelIDs) {
    expect((center.payload.channels as Array<{ id: string }>).map((channel) => channel.id)).not.toContain(id);
  }

  const defaultRuleID = (center.payload.rules as Array<{ id: string; name: string }>)
    .find((rule) => rule.name.includes("超过"))?.id;
  const defaultChannelID = (center.payload.channels as Array<{ id: string; name: string }>)
    .find((channel) => channel.name === "默认邮件通知")?.id;
  expect(defaultRuleID).toBeTruthy();
  expect(defaultChannelID).toBeTruthy();

  const deletedDefaultRule = await writeJSON(page, `/api/console/alerts/rules/${defaultRuleID}`, "DELETE", {});
  expect(deletedDefaultRule.ok).toBeTruthy();
  const deletedDefaultChannel = await writeJSON(page, `/api/console/alerts/channels/${defaultChannelID}`, "DELETE", {});
  expect(deletedDefaultChannel.ok).toBeTruthy();

  const afterDefaultDelete = await readJSON(page, "/api/console/alerts");
  expect((afterDefaultDelete.payload.rules as Array<{ id: string }>).map((rule) => rule.id)).not.toContain(defaultRuleID);
  expect((afterDefaultDelete.payload.channels as Array<{ id: string }>).map((channel) => channel.id)).not.toContain(defaultChannelID);

  const evaluated = await writeJSON(page, "/api/console/alerts/evaluate", "POST", {});
  expect(evaluated.ok).toBeTruthy();
  const afterEvaluate = await readJSON(page, "/api/console/alerts");
  expect((afterEvaluate.payload.rules as Array<{ id: string }>).map((rule) => rule.id)).not.toContain(defaultRuleID);
  expect((afterEvaluate.payload.channels as Array<{ id: string }>).map((channel) => channel.id)).not.toContain(defaultChannelID);
});
