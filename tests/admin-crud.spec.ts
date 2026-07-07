import { expect, Page, test } from "@playwright/test";
import { adminEmail, adminIdentifier, adminLogin, adminPassword } from "./helpers";

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

async function hasInputValue(page: Page, value: string) {
  return page.locator("input").evaluateAll((inputs, target) => inputs.some((input) => (input as HTMLInputElement).value === target), value);
}

async function fillInputWithValue(page: Page, value: string, nextValue: string) {
  const index = await page.locator("input").evaluateAll((inputs, target) => inputs.findIndex((input) => (input as HTMLInputElement).value === target), value);
  expect(index).toBeGreaterThanOrEqual(0);
  await page.locator("input").nth(index).fill(nextValue);
}

function sidebarCountLabel(value: number) {
  return value > 99 ? "99+" : String(value);
}

async function loginAs(page: Page, email: string) {
  const loggedIn = await writeJSON(page, "/api/auth/login", "POST", {
    email,
    password: "admin@tokhub.local"
  });
  expect(loggedIn.ok).toBeTruthy();
}

async function createPrivateChannel(page: Page, suffix: number | string) {
  const created = await writeJSON(page, "/api/me/private-channels", "POST", {
    name: `Admin Delete User Private ${suffix}`,
    provider: "OpenAI",
    type: "openai-compatible",
    model: "gpt-4o-mini",
    endpoint: `https://admin-delete-user-private-${suffix}.example/v1`,
    apiKey: `sk-admin-delete-user-private-${suffix}`,
    probeDaily: 5
  });
  expect(created.status).toBe(201);
  return created.payload.channel.id as string;
}

async function createConsoleGatewayKey(page: Page, suffix: number | string) {
  const channelID = await createPrivateChannel(page, `gateway-${suffix}`);
  const probe = await writeJSON(page, `/api/me/private-channels/${channelID}/probe-now`, "POST", {});
  expect(probe.ok).toBeTruthy();
  const gateway = await writeJSON(page, "/api/console/gateways", "POST", {
    name: `Admin Disable User Gateway ${suffix}`,
    policy: "latency",
    upstreamIds: [channelID],
    qpsLimit: 20,
    quotaMonth: 1000
  });
  expect(gateway.status).toBe(201);
  const key = await writeJSON(page, "/api/console/gateway-keys", "POST", {
    gatewayId: gateway.payload.gateway.id,
    name: `Admin Disable User Key ${suffix}`,
    quotaMonth: 100,
    qpsLimit: 10
  });
  expect(key.status).toBe(201);
  return key.payload.key.plainKey as string;
}

async function expectWorkspaceWriteBlocked(page: Page, path: string, method: string, body?: unknown) {
  const result = await writeJSON(page, path, method, body);
  expect(result.status).toBe(403);
  expect(result.payload.error.code).toBe("workspace_inactive");
}

async function expectWorkspaceReadBlocked(page: Page, path: string) {
  const result = await readJSON(page, path);
  expect(result.status).toBe(403);
  expect(result.payload.error.code).toBe("workspace_inactive");
}

test("admin home console URL copy action is functional", async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        writeText: async (value: string) => {
          (window as unknown as { __tokhubCopiedText?: string }).__tokhubCopiedText = value;
        }
      }
    });
  });
  await adminLogin(page, "/admin");
  const endpoint = page.locator(".endpoint").filter({ hasText: "Console URL" });
  const copyButton = endpoint.getByRole("button", { name: "复制" });
  await expect(copyButton).toBeVisible();
  await copyButton.click();
  await expect(copyButton).toHaveText("已复制 ✓");
  await expect.poll(() => page.evaluate(() => (window as unknown as { __tokhubCopiedText?: string }).__tokhubCopiedText)).toBe("/console");
});

test("admin shell preserves query and hash in login next target", async ({ page }) => {
  const target = "/admin/users?status=active&role=admin&origin=runtime&q=ops%40tokhub.run#bulk";
  await page.goto(target);
  await page.waitForURL((url) => url.pathname === "/admin/login");
  expect(new URL(page.url()).searchParams.get("next")).toBe(target);

  await page.getByLabel("管理员用户名").fill(adminIdentifier());
  await page.getByLabel("密码").fill(adminPassword());
  await Promise.all([
    page.waitForURL((url) => `${url.pathname}${url.search}${url.hash}` === target),
    page.getByRole("button", { name: "进入平台管理 →" }).click()
  ]);
  await expect(page.getByRole("heading", { name: "用户管理" })).toBeVisible();
});

test("login next target rejects external redirects and non-page endpoints", async ({ page }) => {
  for (const next of ["https://example.com/phish", "//example.com/phish", "/api/admin/users", "/gateway/v1/models", "/metrics"]) {
    await page.context().clearCookies();
    await page.goto(`/login?next=${encodeURIComponent(next)}`);
    await page.getByLabel("邮箱").fill(adminEmail());
    await page.getByLabel("密码").fill(adminPassword());
    await Promise.all([
      page.waitForURL((url) => url.origin === new URL(page.url()).origin && url.pathname === "/console"),
      page.getByRole("button", { name: "登录 →" }).click()
    ]);
    expect(page.url()).not.toContain("example.com");
  }
});

test("admin bulk governance endpoints reject empty selections", async ({ page }) => {
  await adminLogin(page, "/admin");

  const emptyUsers = await writeJSON(page, "/api/admin/users/bulk", "POST", { action: "status", ids: [], status: "active" });
  expect(emptyUsers.status).toBe(400);
  expect(emptyUsers.payload.error.code).toBe("empty_bulk_selection");

  const blankUsers = await writeJSON(page, "/api/admin/users/bulk", "POST", { action: "role", ids: ["  "], role: "user" });
  expect(blankUsers.status).toBe(400);
  expect(blankUsers.payload.error.code).toBe("empty_bulk_selection");

  const blankOrgs = await writeJSON(page, "/api/admin/orgs/bulk", "POST", { action: "plan", ids: ["\t"], plan: "team" });
  expect(blankOrgs.status).toBe(400);
  expect(blankOrgs.payload.error.code).toBe("empty_bulk_selection");

  const blankChannels = await writeJSON(page, "/api/admin/channels/bulk", "POST", { action: "disable", ids: [" "] });
  expect(blankChannels.status).toBe(400);
  expect(blankChannels.payload.error.code).toBe("empty_bulk_selection");

  const blankGateways = await writeJSON(page, "/api/admin/gateways/bulk", "POST", { action: "status", ids: [" "], status: "paused" });
  expect(blankGateways.status).toBe(400);
  expect(blankGateways.payload.error.code).toBe("empty_bulk_selection");

  const blankGatewayKeys = await writeJSON(page, "/api/admin/gateway-keys/bulk", "POST", { action: "revoke", ids: ["\n"] });
  expect(blankGatewayKeys.status).toBe(400);
  expect(blankGatewayKeys.payload.error.code).toBe("empty_bulk_selection");

  const blankMembers = await writeJSON(page, "/api/admin/members/bulk", "POST", { action: "role", ids: ["\t"], role: "operator" });
  expect(blankMembers.status).toBe(400);
  expect(blankMembers.payload.error.code).toBe("empty_bulk_selection");

  const emptyOpenAPI = await writeJSON(page, "/api/admin/open-api/sites/bulk", "POST", { action: "delete", ids: [] });
  expect(emptyOpenAPI.status).toBe(400);
  expect(emptyOpenAPI.payload.error.code).toBe("empty_bulk_selection");

  const blankOpenAPI = await writeJSON(page, "/api/admin/open-api/sites/bulk", "POST", { action: "revoke", ids: [" "] });
  expect(blankOpenAPI.status).toBe(400);
  expect(blankOpenAPI.payload.error.code).toBe("empty_bulk_selection");

  const emptyAlertRules = await writeJSON(page, "/api/admin/alerts/rules/bulk", "POST", { action: "disable", ids: [] });
  expect(emptyAlertRules.status).toBe(400);
  expect(emptyAlertRules.payload.error.code).toBe("empty_bulk_selection");

  const blankAlertRules = await writeJSON(page, "/api/admin/alerts/rules/bulk", "POST", { action: "enable", ids: [" "] });
  expect(blankAlertRules.status).toBe(400);
  expect(blankAlertRules.payload.error.code).toBe("empty_bulk_selection");

  const emptyNotificationChannels = await writeJSON(page, "/api/admin/alerts/channels/bulk", "POST", { action: "disable", ids: [] });
  expect(emptyNotificationChannels.status).toBe(400);
  expect(emptyNotificationChannels.payload.error.code).toBe("empty_bulk_selection");

  const blankNotificationChannels = await writeJSON(page, "/api/admin/alerts/channels/bulk", "POST", { action: "enable", ids: ["\t"] });
  expect(blankNotificationChannels.status).toBe(400);
  expect(blankNotificationChannels.payload.error.code).toBe("empty_bulk_selection");

  const emptyIncidents = await writeJSON(page, "/api/admin/incidents/bulk", "POST", { action: "close", ids: [] });
  expect(emptyIncidents.status).toBe(400);
  expect(emptyIncidents.payload.error.code).toBe("empty_bulk_selection");

  const blankIncidents = await writeJSON(page, "/api/admin/incidents/bulk", "POST", { action: "delete", ids: ["\n"] });
  expect(blankIncidents.status).toBe(400);
  expect(blankIncidents.payload.error.code).toBe("empty_bulk_selection");
});

test("admin audit defaults to governance events while retaining system event access", async ({ page }) => {
  await adminLogin(page, "/admin/audit");
  await expect(page.getByRole("button", { name: "治理事件" })).toHaveClass(/active/);
  await expect(page.locator("body")).toContainText("默认展示人工和治理事件");
  await expect(page.locator("body")).toContainText("治理事件口径");

  const governance = await readJSON(page, "/api/admin/audit?eventClass=governance&limit=500");
  expect(governance.ok).toBeTruthy();
  expect((governance.payload.items as Array<{ action: string }>).every((item) => item.action !== "probe.status.changed")).toBeTruthy();

  const system = await readJSON(page, "/api/admin/audit?eventClass=system&limit=500");
  expect(system.ok).toBeTruthy();
  expect((system.payload.items as Array<{ action: string }>).every((item) => item.action === "probe.status.changed")).toBeTruthy();

  await page.getByRole("button", { name: "系统探测" }).click();
  await expect.poll(() => new URL(page.url()).searchParams.get("eventClass")).toBe("system");
  await expect(page.locator("body")).toContainText("系统探测口径");

  await page.getByRole("button", { name: "全部事件" }).click();
  await expect.poll(() => new URL(page.url()).searchParams.get("eventClass")).toBe("all");
  await expect(page.locator("body")).toContainText("全部事件口径");

  await page.getByRole("button", { name: "治理事件" }).click();
  await expect.poll(() => new URL(page.url()).searchParams.get("eventClass")).toBeNull();
});

test("admin users and orgs support create, filter, edit, bulk update and delete", async ({ page }) => {
  await adminLogin(page, "/admin/users");

  const suffix = Date.now();
  const email = `crud-user-${suffix}@tokhub.run`;
  const createdUser = await writeJSON(page, "/api/admin/users", "POST", {
    email,
    password: "admin@tokhub.local",
    name: `CRUD User ${suffix}`,
    role: "user",
    status: "active",
    emailVerified: true,
    dataOrigin: "runtime"
  });
  expect(createdUser.status).toBe(201);
  const userID = createdUser.payload.user.id as string;

  const filteredUsers = await readJSON(page, `/api/admin/users?q=${encodeURIComponent(email)}&status=active&role=user&origin=runtime`);
  expect(filteredUsers.ok).toBeTruthy();
  expect(filteredUsers.payload.items.map((item: { id: string }) => item.id)).toContain(userID);

  await loginAs(page, email);
  const privateChannelID = await createPrivateChannel(page, suffix);
  await adminLogin(page, "/admin/users");
  const adminChannelsBeforeUserDelete = await readJSON(page, "/api/admin/channels");
  expect((adminChannelsBeforeUserDelete.payload.private as Array<{ id: string }>).map((item) => item.id)).toContain(privateChannelID);
  const settingsBeforeUserDelete = await readJSON(page, "/api/admin/settings");
  expect(settingsBeforeUserDelete.payload.summary.privateChannels).toBeGreaterThanOrEqual(1);

  const editedUser = await writeJSON(page, `/api/admin/users/${userID}`, "PATCH", {
    name: `CRUD User Edited ${suffix}`,
    role: "admin",
    status: "disabled",
    emailVerified: false,
    dataOrigin: "runtime"
  });
  expect(editedUser.ok).toBeTruthy();
  expect(editedUser.payload.user.name).toContain("Edited");
  expect(editedUser.payload.user.role).toBe("admin");
  expect(editedUser.payload.user.status).toBe("disabled");
  expect(editedUser.payload.user.emailVerified).toBeFalsy();

  const patchUserStatusDeleted = await writeJSON(page, `/api/admin/users/${userID}/status`, "PATCH", { status: "deleted" });
  expect(patchUserStatusDeleted.ok).toBeFalsy();
  expect(patchUserStatusDeleted.status).toBe(400);
  const patchUserDeleted = await writeJSON(page, `/api/admin/users/${userID}`, "PATCH", { status: "deleted" });
  expect(patchUserDeleted.ok).toBeFalsy();
  expect(patchUserDeleted.status).toBe(400);

  const batchEmails = [`crud-batch-a-${suffix}@tokhub.run`, `crud-batch-b-${suffix}@tokhub.run`];
  const batchIDs: string[] = [];
  for (const batchEmail of batchEmails) {
    const created = await writeJSON(page, "/api/admin/users", "POST", {
      email: batchEmail,
      password: "admin@tokhub.local",
      name: batchEmail.split("@")[0],
      role: "user",
      status: "active",
      emailVerified: true,
      dataOrigin: "runtime"
    });
    expect(created.status).toBe(201);
    batchIDs.push(created.payload.user.id as string);
  }
  const bulkRole = await writeJSON(page, "/api/admin/users/bulk", "POST", { action: "role", ids: batchIDs, role: "admin" });
  expect(bulkRole.ok).toBeTruthy();
  const adminRoleIDs = new Set((bulkRole.payload.items as Array<{ id: string; role: string }>).filter((item) => item.role === "admin").map((item) => item.id));
  expect(batchIDs.every((id) => adminRoleIDs.has(id))).toBeTruthy();
  const bulkDisabled = await writeJSON(page, "/api/admin/users/bulk", "POST", { action: "status", ids: batchIDs, status: "disabled" });
  expect(bulkDisabled.ok).toBeTruthy();
  const disabledIDs = new Set((bulkDisabled.payload.items as Array<{ id: string; status: string }>).filter((item) => item.status === "disabled").map((item) => item.id));
  expect(batchIDs.every((id) => disabledIDs.has(id))).toBeTruthy();
  const failedMixedUserBulk = await writeJSON(page, "/api/admin/users/bulk", "POST", { action: "status", ids: [batchIDs[0], `usr_missing_${suffix}`], status: "active" });
  expect(failedMixedUserBulk.ok).toBeFalsy();
  expect(failedMixedUserBulk.status).toBe(400);
  const firstBatchUserAfterFailedBulk = await readJSON(page, `/api/admin/users?q=${encodeURIComponent(batchEmails[0])}`);
  const firstBatchUser = (firstBatchUserAfterFailedBulk.payload.items as Array<{ id: string; role: string; status: string }>).find((item) => item.id === batchIDs[0]);
  expect(firstBatchUser?.status).toBe("disabled");
  expect(firstBatchUser?.role).toBe("admin");
  const failedMixedUserRole = await writeJSON(page, "/api/admin/users/bulk", "POST", { action: "role", ids: [batchIDs[0], `usr_missing_role_${suffix}`], role: "user" });
  expect(failedMixedUserRole.ok).toBeFalsy();
  expect(failedMixedUserRole.status).toBe(400);
  const firstBatchUserAfterFailedRole = await readJSON(page, `/api/admin/users?q=${encodeURIComponent(batchEmails[0])}`);
  const firstBatchRole = (firstBatchUserAfterFailedRole.payload.items as Array<{ id: string; role: string }>).find((item) => item.id === batchIDs[0]);
  expect(firstBatchRole?.role).toBe("admin");
  const failedMixedUserDelete = await writeJSON(page, "/api/admin/users/bulk", "POST", { action: "delete", ids: [batchIDs[0], `usr_missing_delete_${suffix}`] });
  expect(failedMixedUserDelete.ok).toBeFalsy();
  expect(failedMixedUserDelete.status).toBe(400);
  const firstBatchUserAfterFailedDelete = await readJSON(page, `/api/admin/users?q=${encodeURIComponent(batchEmails[0])}`);
  const firstBatchDeleteAttempt = (firstBatchUserAfterFailedDelete.payload.items as Array<{ id: string; status: string }>).find((item) => item.id === batchIDs[0]);
  expect(firstBatchDeleteAttempt?.status).toBe("disabled");
  const bulkStatusDeleted = await writeJSON(page, "/api/admin/users/bulk", "POST", { action: "status", ids: batchIDs, status: "deleted" });
  expect(bulkStatusDeleted.ok).toBeFalsy();
  expect(bulkStatusDeleted.status).toBe(400);
  const bulkDeleted = await writeJSON(page, "/api/admin/users/bulk", "POST", { action: "delete", ids: [...batchIDs, userID] });
  expect(bulkDeleted.ok).toBeTruthy();
  const deletedIDs = new Set((bulkDeleted.payload.items as Array<{ id: string; status: string }>).filter((item) => item.status === "deleted").map((item) => item.id));
  expect([...batchIDs, userID].every((id) => deletedIDs.has(id))).toBeTruthy();
  const adminChannelsAfterUserDelete = await readJSON(page, "/api/admin/channels");
  expect((adminChannelsAfterUserDelete.payload.private as Array<{ id: string }>).map((item) => item.id)).not.toContain(privateChannelID);
  const reactivateDeletedUser = await writeJSON(page, `/api/admin/users/${userID}/status`, "PATCH", { status: "active" });
  expect(reactivateDeletedUser.ok).toBeFalsy();
  expect(reactivateDeletedUser.status).toBe(400);
  const editDeletedUser = await writeJSON(page, `/api/admin/users/${userID}`, "PATCH", { name: `Recovered User ${suffix}`, role: "user", status: "active" });
  expect(editDeletedUser.ok).toBeFalsy();
  expect(editDeletedUser.status).toBe(400);
  const bulkEditDeletedUser = await writeJSON(page, "/api/admin/users/bulk", "POST", { action: "role", ids: [batchIDs[0]], role: "user" });
  expect(bulkEditDeletedUser.ok).toBeFalsy();
  expect(bulkEditDeletedUser.status).toBe(400);
  const deleteDeletedUserAgain = await writeJSON(page, `/api/admin/users/${userID}`, "DELETE", {});
  expect(deleteDeletedUserAgain.ok).toBeFalsy();
  expect(deleteDeletedUserAgain.status).toBe(404);

  const singleDeleteEmail = `crud-user-single-delete-${suffix}@tokhub.run`;
  const singleDeleteUser = await writeJSON(page, "/api/admin/users", "POST", {
    email: singleDeleteEmail,
    password: "admin@tokhub.local",
    name: `CRUD Single Delete User ${suffix}`,
    role: "user",
    status: "active",
    emailVerified: true,
    dataOrigin: "runtime"
  });
  expect(singleDeleteUser.status).toBe(201);
  const singleDeleteUserID = singleDeleteUser.payload.user.id as string;
  await loginAs(page, singleDeleteEmail);
  const singleDeletePrivateChannelID = await createPrivateChannel(page, `single-${suffix}`);
  await adminLogin(page, "/admin/users");
  const adminChannelsBeforeSingleUserDelete = await readJSON(page, "/api/admin/channels");
  expect((adminChannelsBeforeSingleUserDelete.payload.private as Array<{ id: string }>).map((item) => item.id)).toContain(singleDeletePrivateChannelID);
  const deletedSingleUser = await writeJSON(page, `/api/admin/users/${singleDeleteUserID}`, "DELETE", {});
  expect(deletedSingleUser.ok).toBeTruthy();
  const adminChannelsAfterSingleUserDelete = await readJSON(page, "/api/admin/channels");
  expect((adminChannelsAfterSingleUserDelete.payload.private as Array<{ id: string }>).map((item) => item.id)).not.toContain(singleDeletePrivateChannelID);

  const disabledGatewayEmail = `crud-user-disable-gateway-${suffix}@tokhub.run`;
  const disabledGatewayUser = await writeJSON(page, "/api/admin/users", "POST", {
    email: disabledGatewayEmail,
    password: "admin@tokhub.local",
    name: `CRUD Disable Gateway User ${suffix}`,
    role: "user",
    status: "active",
    emailVerified: true,
    dataOrigin: "runtime"
  });
  expect(disabledGatewayUser.status).toBe(201);
  const disabledGatewayUserID = disabledGatewayUser.payload.user.id as string;
  await loginAs(page, disabledGatewayEmail);
  const plainGatewayKey = await createConsoleGatewayKey(page, suffix);
  const gatewayBeforeUserDisable = await page.evaluate(async (key) => {
    const response = await fetch("/gateway/v1/models", { headers: { "Authorization": `Bearer ${key}` } });
    return response.status;
  }, plainGatewayKey);
  expect(gatewayBeforeUserDisable).not.toBe(401);
  await adminLogin(page, "/admin/users");
  const disabledGatewayUserStatus = await writeJSON(page, `/api/admin/users/${disabledGatewayUserID}/status`, "PATCH", { status: "disabled" });
  expect(disabledGatewayUserStatus.ok).toBeTruthy();
  const gatewayAfterUserDisable = await page.evaluate(async (key) => {
    const response = await fetch("/gateway/v1/models", { headers: { "Authorization": `Bearer ${key}` } });
    return response.status;
  }, plainGatewayKey);
  expect(gatewayAfterUserDisable).toBe(401);

  const disabledOrgEmail = `crud-org-disable-gateway-${suffix}@tokhub.run`;
  const disabledOrgUser = await writeJSON(page, "/api/admin/users", "POST", {
    email: disabledOrgEmail,
    password: "admin@tokhub.local",
    name: `CRUD Disable Org Gateway User ${suffix}`,
    role: "user",
    status: "active",
    emailVerified: true,
    dataOrigin: "runtime"
  });
  expect(disabledOrgUser.status).toBe(201);
  await loginAs(page, disabledOrgEmail);
  const disabledOrgPrivateChannelID = await createPrivateChannel(page, `org-disable-${suffix}`);
  const disabledOrgPlainKey = await createConsoleGatewayKey(page, `org-disable-${suffix}`);
  const disabledOrgSettings = await readJSON(page, "/api/console/settings");
  expect(disabledOrgSettings.ok).toBeTruthy();
  const disabledOrgID = disabledOrgSettings.payload.workspace.orgId as string;
  const gatewayBeforeOrgDisable = await page.evaluate(async (key) => {
    const response = await fetch("/gateway/v1/models", { headers: { "Authorization": `Bearer ${key}` } });
    return response.status;
  }, disabledOrgPlainKey);
  expect(gatewayBeforeOrgDisable).not.toBe(401);
  await adminLogin(page, "/admin/orgs");
  const disabledOrgStatus = await writeJSON(page, `/api/admin/orgs/${disabledOrgID}/status`, "PATCH", { status: "disabled" });
  expect(disabledOrgStatus.ok).toBeTruthy();
  const gatewayAfterOrgDisable = await page.evaluate(async (key) => {
    const response = await fetch("/gateway/v1/models", { headers: { "Authorization": `Bearer ${key}` } });
    return response.status;
  }, disabledOrgPlainKey);
  expect(gatewayAfterOrgDisable).toBe(401);
  await loginAs(page, disabledOrgEmail);
  const createGatewayAfterOrgDisable = await writeJSON(page, "/api/console/gateways", "POST", {
    name: `Blocked Disabled Org Gateway ${suffix}`,
    policy: "latency",
    upstreamIds: [disabledOrgPrivateChannelID],
    qpsLimit: 10,
    quotaMonth: 100
  });
  expect(createGatewayAfterOrgDisable.status).toBe(403);
  expect(createGatewayAfterOrgDisable.payload.error.code).toBe("workspace_inactive");
  await expectWorkspaceWriteBlocked(page, "/api/me/private-channels", "POST", {
    name: `Blocked Disabled Org Private ${suffix}`,
    provider: "OpenAI",
    type: "openai-compatible",
    model: "gpt-4o-mini",
    endpoint: `https://blocked-disabled-org-private-${suffix}.invalid/v1`,
    apiKey: `sk-blocked-disabled-org-private-${suffix}`,
    probeDaily: 5
  });
  await expectWorkspaceWriteBlocked(page, `/api/me/private-channels/${disabledOrgPrivateChannelID}`, "PATCH", {
    name: `Blocked Disabled Org Private Edited ${suffix}`,
    provider: "OpenAI",
    type: "openai-compatible",
    model: "gpt-4o-mini",
    endpoint: `https://blocked-disabled-org-private-edited-${suffix}.invalid/v1`,
    apiKey: "",
    probeDaily: 5
  });
  await expectWorkspaceWriteBlocked(page, "/api/me/private-channels/bulk", "POST", { action: "disable", ids: [disabledOrgPrivateChannelID] });
  await expectWorkspaceWriteBlocked(page, `/api/me/private-channels/${disabledOrgPrivateChannelID}/validate`, "POST", {});
  await expectWorkspaceWriteBlocked(page, `/api/me/private-channels/${disabledOrgPrivateChannelID}/probe-now`, "POST", {});
  await expectWorkspaceWriteBlocked(page, `/api/me/private-channels/${disabledOrgPrivateChannelID}`, "DELETE", {});
  await expectWorkspaceWriteBlocked(page, "/api/console/alerts/rules", "POST", {
    name: `Blocked Disabled Org Rule ${suffix}`,
    kind: "cost_threshold",
    severity: "warning",
    threshold: 10,
    windowMinutes: 60,
    dedupeMinutes: 30,
    enabled: true
  });
  await expectWorkspaceWriteBlocked(page, "/api/console/alerts/channels", "POST", {
    name: `Blocked Disabled Org Notify ${suffix}`,
    type: "email",
    target: `blocked-disabled-org-${suffix}@tokhub.run`,
    enabled: true
  });
  await expectWorkspaceWriteBlocked(page, "/api/console/incidents", "POST", {
    channelId: disabledOrgPrivateChannelID,
    status: "manual",
    title: `Blocked Disabled Org Incident ${suffix}`,
    impact: "Blocked write should not create incident",
    message: "workspace inactive"
  });
  await expectWorkspaceWriteBlocked(page, "/api/console/usage/rollup/recompute", "POST", {});
  await expectWorkspaceReadBlocked(page, "/api/console/alerts");
  await expectWorkspaceReadBlocked(page, "/api/console/governance/summary");
  await adminLogin(page, "/admin/users");

  const deletedOrgEmail = `crud-org-delete-gateway-${suffix}@tokhub.run`;
  const deletedOrgUser = await writeJSON(page, "/api/admin/users", "POST", {
    email: deletedOrgEmail,
    password: "admin@tokhub.local",
    name: `CRUD Delete Org Gateway User ${suffix}`,
    role: "user",
    status: "active",
    emailVerified: true,
    dataOrigin: "runtime"
  });
  expect(deletedOrgUser.status).toBe(201);
  await loginAs(page, deletedOrgEmail);
  const deletedOrgPlainKey = await createConsoleGatewayKey(page, `org-delete-${suffix}`);
  const deletedOrgSettings = await readJSON(page, "/api/console/settings");
  expect(deletedOrgSettings.ok).toBeTruthy();
  const deletedOrgID = deletedOrgSettings.payload.workspace.orgId as string;
  const gatewayBeforeOrgDelete = await page.evaluate(async (key) => {
    const response = await fetch("/gateway/v1/models", { headers: { "Authorization": `Bearer ${key}` } });
    return response.status;
  }, deletedOrgPlainKey);
  expect(gatewayBeforeOrgDelete).not.toBe(401);
  await adminLogin(page, "/admin/orgs");
  const deletedOrg = await writeJSON(page, `/api/admin/orgs/${deletedOrgID}`, "DELETE", {});
  expect(deletedOrg.ok).toBeTruthy();
  const gatewayAfterOrgDelete = await page.evaluate(async (key) => {
    const response = await fetch("/gateway/v1/models", { headers: { "Authorization": `Bearer ${key}` } });
    return response.status;
  }, deletedOrgPlainKey);
  expect(gatewayAfterOrgDelete).toBe(401);
  await adminLogin(page, "/admin/users");

  await page.goto("/admin/users");
  await expect(page.getByRole("link", { name: /新增用户/ })).toHaveAttribute("href", "/admin/users/new");
  await page.getByLabel("用户列表模式").selectOption("bulk");
  await expect(page.getByText("批量改状态")).toBeVisible();
  await expect(page.getByText("批量改角色")).toBeVisible();
  await page.getByPlaceholder("搜索邮箱、姓名、角色、来源").fill(email);
  await page.getByRole("button", { name: "筛选", exact: true }).click();
  await expect(page.getByLabel(`选择 ${email}`)).toBeDisabled();
  await page.goto(`/admin/users?q=${encodeURIComponent(email)}&status=deleted&role=admin&origin=runtime`);
  await page.getByLabel("用户列表模式").selectOption("bulk");
  await expect(page.getByPlaceholder("搜索邮箱、姓名、角色、来源")).toHaveValue(email);
  await expect(page.getByLabel(`选择 ${email}`)).toBeDisabled();
  await page.getByRole("button", { name: "清空筛选" }).click();
  await expect(page.getByPlaceholder("搜索邮箱、姓名、角色、来源")).toHaveValue("");
  await expect.poll(() => new URL(page.url()).search).toBe("");

  const orgSlug = `crud-org-${suffix}`;
  const createdOrg = await writeJSON(page, "/api/admin/orgs", "POST", {
    name: `CRUD Org ${suffix}`,
    slug: orgSlug,
    plan: "team",
    status: "active",
    timezone: "Asia/Shanghai",
    dataOrigin: "runtime"
  });
  expect(createdOrg.status).toBe(201);
  const orgID = createdOrg.payload.org.id as string;

  const filteredOrgs = await readJSON(page, `/api/admin/orgs?q=${encodeURIComponent(orgSlug)}&status=active&plan=team&origin=runtime`);
  expect(filteredOrgs.ok).toBeTruthy();
  expect(filteredOrgs.payload.items.map((item: { id: string }) => item.id)).toContain(orgID);

  const editedOrg = await writeJSON(page, `/api/admin/orgs/${orgID}`, "PATCH", {
    name: `CRUD Org Edited ${suffix}`,
    slug: orgSlug,
    plan: "business",
    status: "disabled",
    timezone: "Asia/Shanghai",
    dataOrigin: "runtime"
  });
  expect(editedOrg.ok).toBeTruthy();
  expect(editedOrg.payload.org.name).toContain("Edited");
  expect(editedOrg.payload.org.plan).toBe("business");
  expect(editedOrg.payload.org.status).toBe("disabled");

  const patchOrgStatusDeleted = await writeJSON(page, `/api/admin/orgs/${orgID}/status`, "PATCH", { status: "deleted" });
  expect(patchOrgStatusDeleted.ok).toBeFalsy();
  expect(patchOrgStatusDeleted.status).toBe(400);
  const patchOrgDeleted = await writeJSON(page, `/api/admin/orgs/${orgID}`, "PATCH", { status: "deleted" });
  expect(patchOrgDeleted.ok).toBeFalsy();
  expect(patchOrgDeleted.status).toBe(400);

  const protectedDelete = await writeJSON(page, "/api/admin/orgs/org_default", "DELETE", {});
  expect(protectedDelete.status).toBe(400);

  const batchOrgIDs: string[] = [];
  for (const label of ["a", "b"]) {
    const created = await writeJSON(page, "/api/admin/orgs", "POST", {
      name: `CRUD Org ${label.toUpperCase()} ${suffix}`,
      slug: `crud-org-${label}-${suffix}`,
      plan: "starter",
      status: "active",
      timezone: "Asia/Shanghai",
      dataOrigin: "runtime"
    });
    expect(created.status).toBe(201);
    batchOrgIDs.push(created.payload.org.id as string);
  }
  const bulkOrgPlan = await writeJSON(page, "/api/admin/orgs/bulk", "POST", { action: "plan", ids: batchOrgIDs, plan: "enterprise" });
  expect(bulkOrgPlan.ok).toBeTruthy();
  const enterpriseOrgIDs = new Set((bulkOrgPlan.payload.items as Array<{ id: string; plan: string }>).filter((item) => item.plan === "enterprise").map((item) => item.id));
  expect(batchOrgIDs.every((id) => enterpriseOrgIDs.has(id))).toBeTruthy();
  const bulkOrgSuspended = await writeJSON(page, "/api/admin/orgs/bulk", "POST", { action: "status", ids: batchOrgIDs, status: "suspended" });
  expect(bulkOrgSuspended.ok).toBeTruthy();
  const suspendedOrgIDs = new Set((bulkOrgSuspended.payload.items as Array<{ id: string; status: string }>).filter((item) => item.status === "suspended").map((item) => item.id));
  expect(batchOrgIDs.every((id) => suspendedOrgIDs.has(id))).toBeTruthy();
  const failedMixedOrgBulk = await writeJSON(page, "/api/admin/orgs/bulk", "POST", { action: "status", ids: [batchOrgIDs[0], `org_missing_${suffix}`], status: "active" });
  expect(failedMixedOrgBulk.ok).toBeFalsy();
  expect(failedMixedOrgBulk.status).toBe(400);
  const firstBatchOrgAfterFailedBulk = await readJSON(page, `/api/admin/orgs?q=${encodeURIComponent(`crud-org-a-${suffix}`)}`);
  const firstBatchOrg = (firstBatchOrgAfterFailedBulk.payload.items as Array<{ id: string; status: string; plan: string }>).find((item) => item.id === batchOrgIDs[0]);
  expect(firstBatchOrg?.status).toBe("suspended");
  expect(firstBatchOrg?.plan).toBe("enterprise");
  const failedMixedOrgPlan = await writeJSON(page, "/api/admin/orgs/bulk", "POST", { action: "plan", ids: [batchOrgIDs[0], `org_missing_plan_${suffix}`], plan: "business" });
  expect(failedMixedOrgPlan.ok).toBeFalsy();
  expect(failedMixedOrgPlan.status).toBe(400);
  const firstBatchOrgAfterFailedPlan = await readJSON(page, `/api/admin/orgs?q=${encodeURIComponent(`crud-org-a-${suffix}`)}`);
  const firstBatchPlan = (firstBatchOrgAfterFailedPlan.payload.items as Array<{ id: string; plan: string }>).find((item) => item.id === batchOrgIDs[0]);
  expect(firstBatchPlan?.plan).toBe("enterprise");
  const failedMixedOrgDelete = await writeJSON(page, "/api/admin/orgs/bulk", "POST", { action: "delete", ids: [batchOrgIDs[0], `org_missing_delete_${suffix}`] });
  expect(failedMixedOrgDelete.ok).toBeFalsy();
  expect(failedMixedOrgDelete.status).toBe(400);
  const firstBatchOrgAfterFailedDelete = await readJSON(page, `/api/admin/orgs?q=${encodeURIComponent(`crud-org-a-${suffix}`)}`);
  const firstBatchAfterDeleteAttempt = (firstBatchOrgAfterFailedDelete.payload.items as Array<{ id: string; status: string }>).find((item) => item.id === batchOrgIDs[0]);
  expect(firstBatchAfterDeleteAttempt?.status).toBe("suspended");
  const bulkOrgStatusDeleted = await writeJSON(page, "/api/admin/orgs/bulk", "POST", { action: "status", ids: batchOrgIDs, status: "deleted" });
  expect(bulkOrgStatusDeleted.ok).toBeFalsy();
  expect(bulkOrgStatusDeleted.status).toBe(400);
  const bulkOrgDeleted = await writeJSON(page, "/api/admin/orgs/bulk", "POST", { action: "delete", ids: [...batchOrgIDs, orgID] });
  expect(bulkOrgDeleted.ok).toBeTruthy();
  const deletedOrgIDs = new Set((bulkOrgDeleted.payload.items as Array<{ id: string; status: string }>).filter((item) => item.status === "deleted").map((item) => item.id));
  expect([...batchOrgIDs, orgID].every((id) => deletedOrgIDs.has(id))).toBeTruthy();
  const reactivateDeletedOrg = await writeJSON(page, `/api/admin/orgs/${orgID}/status`, "PATCH", { status: "active" });
  expect(reactivateDeletedOrg.ok).toBeFalsy();
  expect(reactivateDeletedOrg.status).toBe(400);
  const editDeletedOrg = await writeJSON(page, `/api/admin/orgs/${orgID}`, "PATCH", { name: `Recovered Org ${suffix}`, slug: orgSlug, plan: "team", status: "active", timezone: "Asia/Shanghai" });
  expect(editDeletedOrg.ok).toBeFalsy();
  expect(editDeletedOrg.status).toBe(400);
  const bulkEditDeletedOrg = await writeJSON(page, "/api/admin/orgs/bulk", "POST", { action: "plan", ids: [batchOrgIDs[0]], plan: "team" });
  expect(bulkEditDeletedOrg.ok).toBeFalsy();
  expect(bulkEditDeletedOrg.status).toBe(400);
  const deleteDeletedOrgAgain = await writeJSON(page, `/api/admin/orgs/${orgID}`, "DELETE", {});
  expect(deleteDeletedOrgAgain.ok).toBeFalsy();
  expect(deleteDeletedOrgAgain.status).toBe(404);

  await page.goto(`/admin/orgs?q=${encodeURIComponent(orgSlug)}`);
  await expect(page.getByRole("button", { name: "新增组织" })).toBeVisible();
  await page.getByLabel("组织列表模式").selectOption("bulk");
  await expect(page.getByText("批量改状态")).toBeVisible();
  await expect(page.getByText("批量改套餐")).toBeVisible();
  await expect(page.getByLabel(`选择 CRUD Org Edited ${suffix}`)).toBeDisabled();
  await page.goto(`/admin/orgs?q=${encodeURIComponent(orgSlug)}&status=deleted&plan=business&origin=runtime`);
  await page.getByLabel("组织列表模式").selectOption("bulk");
  await expect(page.getByPlaceholder("搜索组织名、slug、套餐、来源...")).toHaveValue(orgSlug);
  await expect(page.getByLabel(`选择 CRUD Org Edited ${suffix}`)).toBeDisabled();
  await page.getByRole("button", { name: "清空筛选" }).click();
  await expect(page.getByPlaceholder("搜索组织名、slug、套餐、来源...")).toHaveValue("");
  await expect.poll(() => new URL(page.url()).search).toBe("");
});

test("admin platform channels support create, edit, filter, single enable, bulk enable, bulk disable and bulk delete", async ({ page }) => {
  await adminLogin(page, "/admin/channels");

  const suffix = Date.now();
  const channelNames = [`CRUD Channel A ${suffix}`, `CRUD Channel B ${suffix}`];
  const channelIDs: string[] = [];
  for (const name of channelNames) {
    const created = await writeJSON(page, "/api/admin/channels", "POST", {
      name,
      provider: "OpenAI",
      type: "openai-compatible",
      model: "gpt-4o-mini",
      upstreamModel: "gpt-4o-mini",
      endpoint: `https://${name.toLowerCase().replaceAll(" ", "-")}.invalid/v1`,
      apiKey: `sk-crud-channel-${suffix}`,
      probeDaily: 1440,
      publicVisible: true,
      gatewayEnabled: true,
      enabled: true,
      inputPerMtok: 0.15,
      outputPerMtok: 0.6,
      providerConfig: { temperature: 0.1, maxTokens: 32 }
    });
    expect(created.status).toBe(201);
    channelIDs.push(created.payload.channel.id as string);
  }

  const allChannels = await readJSON(page, "/api/admin/channels");
  expect(allChannels.ok).toBeTruthy();
  const createdIDs = (allChannels.payload.items as Array<{ id: string; name: string }>).filter((item) => item.name.includes(String(suffix))).map((item) => item.id);
  expect(channelIDs.every((id) => createdIDs.includes(id))).toBeTruthy();

  const edited = await writeJSON(page, `/api/admin/channels/${channelIDs[0]}`, "PATCH", {
    name: `CRUD Channel Edited ${suffix}`,
    provider: "Anthropic",
    type: "anthropic",
    model: "claude-3-5-sonnet",
    upstreamModel: "claude-3-5-sonnet-20241022",
    endpoint: `https://crud-channel-edited-${suffix}.invalid/v1`,
    probeDaily: 720,
    publicVisible: false,
    gatewayEnabled: false,
    enabled: false,
    inputPerMtok: 3,
    outputPerMtok: 15,
    providerConfig: { temperature: 0.2, maxTokens: 64 }
  });
  expect(edited.ok).toBeTruthy();
  expect(edited.payload.channel.name).toContain("Edited");
  expect(edited.payload.channel.status).toBe("disabled");
  expect(edited.payload.channel.publicVisible).toBeFalsy();
  expect(edited.payload.channel.gatewayEnabled).toBeFalsy();

  await page.goto("/admin/channels");
  await page.getByPlaceholder("搜索通道 / 上游模型…").fill(`CRUD Channel Edited ${suffix}`);
  await expect(page.getByText(`CRUD Channel Edited ${suffix}`)).toBeVisible();
  page.once("dialog", (dialog) => void dialog.accept());
  const singleEnableResponse = page.waitForResponse((response) =>
    response.request().method() === "POST" &&
    (response.url().includes(`/api/admin/channels/${channelIDs[0]}/enable`) || response.url().includes("/api/admin/channels/bulk"))
  );
  const editedChannelRow = page.locator("tr", { hasText: `CRUD Channel Edited ${suffix}` }).first();
  await editedChannelRow.locator("summary").first().click();
  await editedChannelRow.getByRole("button", { name: "启用通道" }).click();
  expect((await singleEnableResponse).ok()).toBeTruthy();
  const afterSingleEnable = await readJSON(page, "/api/admin/channels");
  const singleEnabled = (afterSingleEnable.payload.items as Array<{ id: string; status: string; publicVisible: boolean; gatewayEnabled: boolean }>).find((item) => item.id === channelIDs[0]);
  expect(singleEnabled?.status).toBe("unknown");
  expect(singleEnabled?.publicVisible).toBeTruthy();
  expect(singleEnabled?.gatewayEnabled).toBeTruthy();

  const bulkDisabled = await writeJSON(page, "/api/admin/channels/bulk", "POST", { action: "status", status: "disabled", ids: channelIDs });
  expect(bulkDisabled.ok).toBeTruthy();
  const disabledIDs = new Set((bulkDisabled.payload.items as Array<{ id: string; status: string }>).filter((item) => item.status === "disabled").map((item) => item.id));
  expect(channelIDs.every((id) => disabledIDs.has(id))).toBeTruthy();

  await page.goto("/admin/channels");
  await page.getByPlaceholder("搜索通道 / 上游模型…").fill(String(suffix));
  await expect(page.getByText(`CRUD Channel Edited ${suffix}`)).toBeVisible();
  await expect(page.getByText(`CRUD Channel B ${suffix}`)).toBeVisible();
  await page.locator(".admin-channel-table thead input[type='checkbox']").check();
  await expect(page.getByRole("button", { name: "批量启用" })).toBeVisible();
  await expect(page.getByRole("button", { name: "批量禁用" })).toBeVisible();
  await expect(page.getByRole("button", { name: "批量删除" })).toBeVisible();

  page.once("dialog", (dialog) => void dialog.accept());
  const bulkEnableResponse = page.waitForResponse((response) => response.url().includes("/api/admin/channels/bulk") && response.request().method() === "POST");
  await page.getByRole("button", { name: "批量启用" }).click();
  expect((await bulkEnableResponse).ok()).toBeTruthy();

  const afterEnable = await readJSON(page, "/api/admin/channels");
  const enabledChannels = new Map(
    (afterEnable.payload.items as Array<{ id: string; status: string; publicVisible: boolean; gatewayEnabled: boolean }>).map((item) => [item.id, item])
  );
  for (const id of channelIDs) {
    const item = enabledChannels.get(id);
    expect(item?.status).toBe("unknown");
    expect(item?.publicVisible).toBeTruthy();
    expect(item?.gatewayEnabled).toBeTruthy();
  }

  await page.locator(".admin-channel-table thead input[type='checkbox']").check();
  page.once("dialog", (dialog) => void dialog.accept());
  const bulkDeleteResponse = page.waitForResponse((response) => response.url().includes("/api/admin/channels/bulk") && response.request().method() === "POST");
  await page.getByRole("button", { name: "批量删除" }).click();
  expect((await bulkDeleteResponse).ok()).toBeTruthy();

  const afterDelete = await readJSON(page, "/api/admin/channels");
  const remainingIDs = new Set((afterDelete.payload.items as Array<{ id: string }>).map((item) => item.id));
  expect(channelIDs.some((id) => remainingIDs.has(id))).toBeFalsy();
  const patchDeletedChannel = await writeJSON(page, `/api/admin/channels/${channelIDs[0]}`, "PATCH", {
    name: `Recovered Channel ${suffix}`,
    provider: "OpenAI",
    type: "openai-compatible",
    model: "gpt-4o-mini",
    upstreamModel: "gpt-4o-mini",
    endpoint: `https://recovered-channel-${suffix}.invalid/v1`,
    probeDaily: 1440,
    publicVisible: true,
    gatewayEnabled: true,
    enabled: true
  });
  expect(patchDeletedChannel.ok).toBeFalsy();
  expect(patchDeletedChannel.status).toBe(404);
  const rotateDeletedChannelKey = await writeJSON(page, `/api/admin/channels/${channelIDs[0]}/credentials`, "POST", { apiKey: `sk-recovered-${suffix}` });
  expect(rotateDeletedChannelKey.ok).toBeFalsy();
  expect(rotateDeletedChannelKey.status).toBe(404);
  const validateDeletedChannel = await writeJSON(page, `/api/admin/channels/${channelIDs[0]}/validate`, "POST", {});
  expect(validateDeletedChannel.ok).toBeFalsy();
  expect(validateDeletedChannel.status).toBe(404);
  const probeDeletedChannel = await writeJSON(page, `/api/admin/channels/${channelIDs[0]}/probe-now`, "POST", {});
  expect(probeDeletedChannel.ok).toBeFalsy();
  expect(probeDeletedChannel.status).toBe(404);
  const disableDeletedChannel = await writeJSON(page, `/api/admin/channels/${channelIDs[0]}/disable`, "POST", {});
  expect(disableDeletedChannel.ok).toBeFalsy();
  expect(disableDeletedChannel.status).toBe(404);
  const bulkEnableDeletedChannel = await writeJSON(page, "/api/admin/channels/bulk", "POST", { action: "status", status: "active", ids: [channelIDs[0]] });
  expect(bulkEnableDeletedChannel.ok).toBeFalsy();
  expect(bulkEnableDeletedChannel.status).toBe(404);
});

test("admin platform channel list displays provider name without model suffix", async ({ page }) => {
  await adminLogin(page, "/admin/channels");

  const suffix = Date.now();
  const providerName = `List Provider ${suffix}`;
  const model = `claude-list-${suffix}`;
  const fullName = `${providerName} · ${model}`;
  let channelID = "";

  try {
    const created = await writeJSON(page, "/api/admin/channels", "POST", {
      name: fullName,
      provider: providerName,
      type: "anthropic",
      model,
      upstreamModel: model,
      endpoint: `https://list-provider-${suffix}.invalid/v1`,
      apiKey: `sk-list-provider-${suffix}`,
      probeDaily: 1440,
      publicVisible: true,
      gatewayEnabled: true,
      enabled: true,
      inputPerMtok: 3,
      outputPerMtok: 15,
      providerConfig: { temperature: 0.2, maxTokens: 64 }
    });
    expect(created.status).toBe(201);
    channelID = created.payload.channel.id as string;

    await page.goto("/admin/channels");
    await page.getByPlaceholder("搜索通道 / 上游模型…").fill(providerName);
    const row = page.locator(".admin-channel-table tbody tr", { hasText: providerName }).first();
    await expect(row.locator(".channel-identity b")).toHaveText(providerName);
    await expect(row.locator(".channel-model-cell b")).toHaveText(model);
  } finally {
    if (channelID) {
      await writeJSON(page, `/api/admin/channels/${channelID}`, "DELETE", {});
    }
  }
});

test("channel detail admin actions trigger real probe and pause governance", async ({ page }) => {
  await adminLogin(page, "/admin/channels");

  const suffix = Date.now();
  const created = await writeJSON(page, "/api/admin/channels", "POST", {
    name: `Detail Action Channel ${suffix}`,
    provider: "OpenAI",
    type: "openai-compatible",
    model: "gpt-4o-mini",
    upstreamModel: "gpt-4o-mini",
    endpoint: `https://detail-action-${suffix}.invalid/v1`,
    apiKey: `sk-detail-action-${suffix}`,
    probeDaily: 1440,
    publicVisible: true,
    gatewayEnabled: true,
    enabled: true,
    inputPerMtok: 0.15,
    outputPerMtok: 0.6
  });
  expect(created.status).toBe(201);
  const channelID = created.payload.channel.id as string;

  await page.goto(`/channels/${channelID}`);
  await expect(page.getByRole("heading", { name: /Detail Action Channel/ })).toBeVisible();
  const probeResponse = page.waitForResponse((response) => response.url().includes(`/api/admin/channels/${channelID}/probe-now`) && response.request().method() === "POST");
  await page.getByRole("button", { name: "⚡ 立即探测" }).click();
  expect((await probeResponse).ok()).toBeTruthy();
  await expect(page.locator(".form-notice")).toContainText("已触发真实探测");

  page.once("dialog", (dialog) => void dialog.accept());
  const disableResponse = page.waitForResponse((response) => response.url().includes(`/api/admin/channels/${channelID}/disable`) && response.request().method() === "POST");
  await page.getByRole("button", { name: "⏸ 暂停真实探测" }).click();
  expect((await disableResponse).ok()).toBeTruthy();
  await expect(page.locator(".form-notice")).toContainText("平台通道已暂停");
  await expect(page.getByRole("button", { name: "⏸ 暂停真实探测" })).toBeDisabled();

  const adminChannelsAfterPause = await readJSON(page, "/api/admin/channels");
  const paused = (adminChannelsAfterPause.payload.items as Array<{ id: string; status: string; publicVisible: boolean; gatewayEnabled: boolean }>).find((item) => item.id === channelID);
  expect(paused?.status).toBe("disabled");
  expect(paused?.publicVisible).toBeFalsy();
  expect(paused?.gatewayEnabled).toBeFalsy();
});

test("public home report export downloads filtered real channel CSV", async ({ page }) => {
  await adminLogin(page, "/admin/channels");

  const suffix = Date.now();
  const apiKey = `sk-public-export-${suffix}`;
  const created = await writeJSON(page, "/api/admin/channels", "POST", {
    name: `Public Export Channel ${suffix}`,
    provider: "OpenAI",
    type: "openai-compatible",
    model: "gpt-4o-mini",
    upstreamModel: "gpt-4o-mini",
    endpoint: `https://public-export-${suffix}.invalid/v1`,
    apiKey,
    probeDaily: 1440,
    publicVisible: true,
    gatewayEnabled: true,
    enabled: true,
    inputPerMtok: 0.15,
    outputPerMtok: 0.6
  });
  expect(created.status).toBe(201);
  const channelID = created.payload.channel.id as string;

  try {
    const exported = await page.evaluate(async (term) => {
      const response = await fetch(`/api/public/channels/export?query=${encodeURIComponent(term)}`);
      return {
        ok: response.ok,
        contentType: response.headers.get("content-type") ?? "",
        disposition: response.headers.get("content-disposition") ?? "",
        text: await response.text()
      };
    }, `Public Export Channel ${suffix}`);
    expect(exported.ok).toBeTruthy();
    expect(exported.contentType).toContain("text/csv");
    expect(exported.disposition).toContain("tokhub-public-channels.csv");
    expect(exported.text).toContain("Public Export Channel");
    expect(exported.text).toContain(channelID);
    expect(exported.text).not.toContain(apiKey);

    await page.goto("/dashboard");
    await page.getByPlaceholder("搜索服务商 / 模型…").fill(`Public Export Channel ${suffix}`);
    await expect(page.getByRole("link", { name: "导出报表" })).toHaveAttribute("href", `/api/public/channels/export?query=Public+Export+Channel+${suffix}`);
    await page.getByRole("button", { name: /全部通道/ }).click();
    await expect(page.getByPlaceholder("搜索服务商 / 模型…")).toHaveValue("");
    await expect(page.getByRole("link", { name: "导出报表" })).toHaveAttribute("href", "/api/public/channels/export");
  } finally {
    await writeJSON(page, `/api/admin/channels/${channelID}`, "DELETE", {});
  }
});

test("admin gateways, gateway keys and platform members support CRUD governance", async ({ page }) => {
  await adminLogin(page, "/admin/gateways");

  const suffix = Date.now();
  const createdChannel = await writeJSON(page, "/api/admin/channels", "POST", {
    name: `CRUD Gateway Upstream ${suffix}`,
    provider: "OpenAI",
    type: "openai-compatible",
    model: "gpt-4o-mini",
    upstreamModel: "gpt-4o-mini",
    endpoint: `https://crud-upstream-${suffix}.example/v1`,
    apiKey: `sk-crud-${suffix}`,
    probeDaily: 1440,
    publicVisible: true,
    gatewayEnabled: true,
    enabled: true,
    inputPerMtok: 0.15,
    outputPerMtok: 0.6
  });
  expect(createdChannel.status).toBe(201);
  const channelID = createdChannel.payload.channel.id as string;
  const channelProbe = await writeJSON(page, `/api/admin/channels/${channelID}/probe-now`, "POST", {});
  expect(channelProbe.ok).toBeTruthy();

  const defaultGatewaySettings = await writeJSON(page, "/api/admin/settings", "PATCH", { defaultGatewayPolicy: "cost" });
  expect(defaultGatewaySettings.ok).toBeTruthy();
  expect(defaultGatewaySettings.payload.site.defaultGatewayPolicy).toBe("cost");
  await page.goto("/admin/gateways");
  await page.getByPlaceholder("例如：平台生产网关").fill(`CRUD Wizard Gateway ${suffix}`);
  await page.getByRole("button", { name: /下一步/ }).click();
  await expect(page.locator("button.pick").filter({ hasText: `CRUD Gateway Upstream ${suffix}` }).first()).toBeVisible();
  await page.getByRole("button", { name: /下一步/ }).click();
  await expect(page.locator("button.opt.sel").filter({ hasText: "成本优先" })).toBeVisible();

  const createdGateway = await writeJSON(page, "/api/admin/gateways", "POST", {
    name: `CRUD Gateway ${suffix}`,
    policy: "latency",
    upstreamIds: [channelID],
    qpsLimit: 30,
    quotaMonth: 50000
  });
  expect(createdGateway.status).toBe(201);
  const gatewayID = createdGateway.payload.gateway.id as string;
  expect(createdGateway.payload.gateway.upstreams.map((item: { channelId: string }) => item.channelId)).toContain(channelID);

  const patchedGateway = await writeJSON(page, `/api/admin/gateways/${gatewayID}`, "PATCH", {
    name: `CRUD Gateway Edited ${suffix}`,
    policy: "success",
    status: "paused",
    upstreamIds: [channelID],
    qpsLimit: 45,
    quotaMonth: 60000
  });
  expect(patchedGateway.ok).toBeTruthy();
  expect(patchedGateway.payload.gateway.name).toContain("Edited");
  expect(patchedGateway.payload.gateway.policy).toBe("success");
  expect(patchedGateway.payload.gateway.status).toBe("paused");
  expect(patchedGateway.payload.gateway.qpsLimit).toBe(45);

  const activeGateway = await writeJSON(page, `/api/admin/gateways/${gatewayID}`, "PATCH", { status: "active" });
  expect(activeGateway.ok).toBeTruthy();
  expect(activeGateway.payload.gateway.status).toBe("active");

  const batchGatewayIDs: string[] = [];
  for (const label of ["A", "B"]) {
    const created = await writeJSON(page, "/api/admin/gateways", "POST", {
      name: `CRUD Batch Gateway ${label} ${suffix}`,
      policy: "latency",
      upstreamIds: [channelID],
      qpsLimit: 20,
      quotaMonth: 20000
    });
    expect(created.status).toBe(201);
    batchGatewayIDs.push(created.payload.gateway.id as string);
  }
  const bulkPaused = await writeJSON(page, "/api/admin/gateways/bulk", "POST", { action: "status", ids: batchGatewayIDs, status: "paused" });
  expect(bulkPaused.ok).toBeTruthy();
  const pausedGateways = new Map((bulkPaused.payload.items as Array<{ id: string; status: string }>).map((gateway) => [gateway.id, gateway.status]));
  expect(batchGatewayIDs.every((id) => pausedGateways.get(id) === "paused")).toBeTruthy();
  const failedMixedGatewayStatus = await writeJSON(page, "/api/admin/gateways/bulk", "POST", { action: "status", ids: [batchGatewayIDs[0], `gw_missing_${suffix}`], status: "active" });
  expect(failedMixedGatewayStatus.ok).toBeFalsy();
  expect(failedMixedGatewayStatus.status).toBe(404);
  const gatewaysAfterFailedStatus = await readJSON(page, "/api/admin/gateways");
  const firstBatchGatewayAfterFailedStatus = (gatewaysAfterFailedStatus.payload.items as Array<{ id: string; status: string }>).find((gateway) => gateway.id === batchGatewayIDs[0]);
  expect(firstBatchGatewayAfterFailedStatus?.status).toBe("paused");
  const failedMixedGatewayDelete = await writeJSON(page, "/api/admin/gateways/bulk", "POST", { action: "delete", ids: [batchGatewayIDs[0], `gw_missing_delete_${suffix}`] });
  expect(failedMixedGatewayDelete.ok).toBeFalsy();
  expect(failedMixedGatewayDelete.status).toBe(404);
  const gatewaysAfterFailedDelete = await readJSON(page, "/api/admin/gateways");
  const firstBatchGatewayAfterFailedDelete = (gatewaysAfterFailedDelete.payload.items as Array<{ id: string; status: string }>).find((gateway) => gateway.id === batchGatewayIDs[0]);
  expect(firstBatchGatewayAfterFailedDelete?.status).toBe("paused");
  const bulkGatewayDeleted = await writeJSON(page, "/api/admin/gateways/bulk", "POST", { action: "delete", ids: batchGatewayIDs });
  expect(bulkGatewayDeleted.ok).toBeTruthy();
  const remainingGatewayIDs = new Set((bulkGatewayDeleted.payload.items as Array<{ id: string }>).map((gateway) => gateway.id));
  expect(batchGatewayIDs.some((id) => remainingGatewayIDs.has(id))).toBeFalsy();

  const createdKey = await writeJSON(page, "/api/admin/gateway-keys", "POST", {
    gatewayId: gatewayID,
    name: `CRUD Gateway Key ${suffix}`,
    quotaMonth: 1000,
    qpsLimit: 10
  });
  expect(createdKey.status).toBe(201);
  expect(createdKey.payload.key.plainKey).toContain("sk-th-");
  const keyID = createdKey.payload.key.id as string;

  const patchedKey = await writeJSON(page, `/api/admin/gateway-keys/${keyID}`, "PATCH", {
    name: `CRUD Gateway Key Edited ${suffix}`,
    quotaMonth: 2000,
    qpsLimit: 20,
    status: "active"
  });
  expect(patchedKey.ok).toBeTruthy();
  expect(patchedKey.payload.key.name).toContain("Edited");
  expect(patchedKey.payload.key.quotaMonth).toBe(2000);
  expect(patchedKey.payload.key.qpsLimit).toBe(20);
  expect(JSON.stringify(patchedKey.payload)).not.toContain(createdKey.payload.key.plainKey);

  const patchGatewayDeleted = await writeJSON(page, `/api/admin/gateways/${gatewayID}`, "PATCH", { status: "deleted" });
  expect(patchGatewayDeleted.ok).toBeFalsy();
  expect(patchGatewayDeleted.status).toBe(400);
  const bulkStatusDeleted = await writeJSON(page, "/api/admin/gateways/bulk", "POST", { action: "status", ids: [gatewayID], status: "deleted" });
  expect(bulkStatusDeleted.ok).toBeFalsy();
  expect(bulkStatusDeleted.status).toBe(400);
  const gatewayStillVisible = await readJSON(page, "/api/admin/gateways");
  expect((gatewayStillVisible.payload.items as Array<{ id: string }>).map((item) => item.id)).toContain(gatewayID);
  const keyStillEditable = await writeJSON(page, `/api/admin/gateway-keys/${keyID}`, "PATCH", { status: "active" });
  expect(keyStillEditable.ok).toBeTruthy();
  const patchKeyRevoked = await writeJSON(page, `/api/admin/gateway-keys/${keyID}`, "PATCH", { status: "revoked" });
  expect(patchKeyRevoked.ok).toBeFalsy();
  expect(patchKeyRevoked.status).toBe(400);
  const bulkKeyStatusRevoked = await writeJSON(page, "/api/admin/gateway-keys/bulk", "POST", { action: "status", ids: [keyID], status: "revoked" });
  expect(bulkKeyStatusRevoked.ok).toBeFalsy();
  expect(bulkKeyStatusRevoked.status).toBe(400);

  const deletableKey = await writeJSON(page, "/api/admin/gateway-keys", "POST", {
    gatewayId: gatewayID,
    name: `CRUD Deletable Key ${suffix}`,
    quotaMonth: 1000,
    qpsLimit: 10
  });
  expect(deletableKey.status).toBe(201);
  const deletableKeyID = deletableKey.payload.key.id as string;
  const deletablePlainKey = deletableKey.payload.key.plainKey as string;
  const gatewayAuthBeforeDelete = await page.evaluate(async (key) => {
    const response = await fetch("/gateway/v1/models", { headers: { "Authorization": `Bearer ${key}` } });
    return { status: response.status };
  }, deletablePlainKey);
  expect(gatewayAuthBeforeDelete.status).not.toBe(401);
  const deletedKey = await writeJSON(page, `/api/admin/gateway-keys/${deletableKeyID}`, "DELETE", {});
  expect(deletedKey.ok).toBeTruthy();
  const keysAfterSingleDelete = await readJSON(page, "/api/admin/gateway-keys");
  expect((keysAfterSingleDelete.payload.items as Array<{ id: string }>).map((key) => key.id)).not.toContain(deletableKeyID);
  const gatewayAuthAfterDelete = await page.evaluate(async (key) => {
    const response = await fetch("/gateway/v1/models", { headers: { "Authorization": `Bearer ${key}` } });
    return { status: response.status };
  }, deletablePlainKey);
  expect(gatewayAuthAfterDelete.status).toBe(401);
  const patchDeletedKey = await writeJSON(page, `/api/admin/gateway-keys/${deletableKeyID}`, "PATCH", { status: "active" });
  expect(patchDeletedKey.ok).toBeFalsy();
  expect(patchDeletedKey.status).toBe(404);

  const batchKeyIDs: string[] = [];
  for (const label of ["A", "B"]) {
    const created = await writeJSON(page, "/api/admin/gateway-keys", "POST", {
      gatewayId: gatewayID,
      name: `CRUD Batch Key ${label} ${suffix}`,
      quotaMonth: 1000,
      qpsLimit: 10
    });
    expect(created.status).toBe(201);
    batchKeyIDs.push(created.payload.key.id as string);
  }
  const bulkExpiredKeys = await writeJSON(page, "/api/admin/gateway-keys/bulk", "POST", { action: "status", ids: batchKeyIDs, status: "expired" });
  expect(bulkExpiredKeys.ok).toBeTruthy();
  const expiredKeys = new Map((bulkExpiredKeys.payload.items as Array<{ id: string; status: string }>).map((key) => [key.id, key.status]));
  expect(batchKeyIDs.every((id) => expiredKeys.get(id) === "expired")).toBeTruthy();
  const failedMixedKeyStatus = await writeJSON(page, "/api/admin/gateway-keys/bulk", "POST", { action: "status", ids: [batchKeyIDs[0], `gkey_missing_${suffix}`], status: "active" });
  expect(failedMixedKeyStatus.ok).toBeFalsy();
  expect(failedMixedKeyStatus.status).toBe(404);
  const keysAfterFailedStatus = await readJSON(page, "/api/admin/gateway-keys");
  const firstBatchKeyAfterFailedStatus = (keysAfterFailedStatus.payload.items as Array<{ id: string; status: string }>).find((key) => key.id === batchKeyIDs[0]);
  expect(firstBatchKeyAfterFailedStatus?.status).toBe("expired");
  const failedMixedKeyRevoke = await writeJSON(page, "/api/admin/gateway-keys/bulk", "POST", { action: "revoke", ids: [batchKeyIDs[0], `gkey_missing_revoke_${suffix}`] });
  expect(failedMixedKeyRevoke.ok).toBeFalsy();
  expect(failedMixedKeyRevoke.status).toBe(404);
  const keysAfterFailedRevoke = await readJSON(page, "/api/admin/gateway-keys");
  const firstBatchKeyAfterFailedRevoke = (keysAfterFailedRevoke.payload.items as Array<{ id: string; status: string }>).find((key) => key.id === batchKeyIDs[0]);
  expect(firstBatchKeyAfterFailedRevoke?.status).toBe("expired");
  const failedMixedKeyDelete = await writeJSON(page, "/api/admin/gateway-keys/bulk", "POST", { action: "delete", ids: [batchKeyIDs[0], `gkey_missing_delete_${suffix}`] });
  expect(failedMixedKeyDelete.ok).toBeFalsy();
  expect(failedMixedKeyDelete.status).toBe(404);
  const keysAfterFailedDelete = await readJSON(page, "/api/admin/gateway-keys");
  const firstBatchKeyAfterFailedDelete = (keysAfterFailedDelete.payload.items as Array<{ id: string; status: string }>).find((key) => key.id === batchKeyIDs[0]);
  expect(firstBatchKeyAfterFailedDelete?.status).toBe("expired");
  const bulkRevokedKeys = await writeJSON(page, "/api/admin/gateway-keys/bulk", "POST", { action: "revoke", ids: batchKeyIDs });
  expect(bulkRevokedKeys.ok).toBeTruthy();
  const revokedKeys = new Map((bulkRevokedKeys.payload.items as Array<{ id: string; status: string }>).map((key) => [key.id, key.status]));
  expect(batchKeyIDs.every((id) => revokedKeys.get(id) === "revoked")).toBeTruthy();
  const reactivateBulkRevokedKey = await writeJSON(page, `/api/admin/gateway-keys/${batchKeyIDs[0]}`, "PATCH", { status: "active" });
  expect(reactivateBulkRevokedKey.ok).toBeFalsy();
  expect(reactivateBulkRevokedKey.status).toBe(400);
  const bulkDeletedKeys = await writeJSON(page, "/api/admin/gateway-keys/bulk", "POST", { action: "delete", ids: batchKeyIDs });
  expect(bulkDeletedKeys.ok).toBeTruthy();
  const remainingKeyIDs = new Set((bulkDeletedKeys.payload.items as Array<{ id: string }>).map((key) => key.id));
  expect(batchKeyIDs.some((id) => remainingKeyIDs.has(id))).toBeFalsy();
  const patchBulkDeletedKey = await writeJSON(page, `/api/admin/gateway-keys/${batchKeyIDs[0]}`, "PATCH", { status: "active" });
  expect(patchBulkDeletedKey.ok).toBeFalsy();
  expect(patchBulkDeletedKey.status).toBe(404);

  const memberEmail = `crud-member-${suffix}@tokhub.run`;
  const createdMemberUser = await writeJSON(page, "/api/admin/users", "POST", {
    email: memberEmail,
    password: "admin@tokhub.local",
    name: `CRUD Member ${suffix}`,
    role: "user",
    status: "active",
    emailVerified: true,
    dataOrigin: "runtime"
  });
  expect(createdMemberUser.status).toBe(201);
  const memberUserID = createdMemberUser.payload.user.id as string;

  const invited = await writeJSON(page, "/api/admin/members", "POST", {
    email: memberEmail,
    role: "viewer",
    groupName: "平台运营"
  });
  expect(invited.status).toBe(201);
  expect(invited.payload.member.userId).toBe(memberUserID);

  const patchedMember = await writeJSON(page, `/api/admin/members/${memberUserID}`, "PATCH", {
    role: "operator",
    groupName: "平台治理"
  });
  expect(patchedMember.ok).toBeTruthy();
  expect(patchedMember.payload.member.role).toBe("operator");
  expect(patchedMember.payload.member.groupName).toBe("平台治理");

  const batchMemberIDs: string[] = [];
  for (const label of ["a", "b"]) {
    const email = `crud-batch-member-${label}-${suffix}@tokhub.run`;
    const created = await writeJSON(page, "/api/admin/users", "POST", {
      email,
      password: "admin@tokhub.local",
      name: `CRUD Batch Member ${label.toUpperCase()} ${suffix}`,
      role: "user",
      status: "active",
      emailVerified: true,
      dataOrigin: "runtime"
    });
    expect(created.status).toBe(201);
    const userID = created.payload.user.id as string;
    const invitedMember = await writeJSON(page, "/api/admin/members", "POST", {
      email,
      role: "viewer",
      groupName: "批量治理"
    });
    expect(invitedMember.status).toBe(201);
    batchMemberIDs.push(userID);
  }
  const bulkMemberRole = await writeJSON(page, "/api/admin/members/bulk", "POST", { action: "role", ids: batchMemberIDs, role: "operator" });
  expect(bulkMemberRole.ok).toBeTruthy();
  const roleMap = new Map((bulkMemberRole.payload.members as Array<{ userId: string; role: string }>).map((member) => [member.userId, member.role]));
  expect(batchMemberIDs.every((id) => roleMap.get(id) === "operator")).toBeTruthy();
  const failedMixedMemberRole = await writeJSON(page, "/api/admin/members/bulk", "POST", {
    action: "role",
    ids: [batchMemberIDs[0], `usr_missing_member_${suffix}`],
    role: "viewer"
  });
  expect(failedMixedMemberRole.ok).toBeFalsy();
  expect(failedMixedMemberRole.status).toBe(404);
  const membersAfterFailedBulk = await readJSON(page, "/api/admin/members");
  const firstBatchMemberAfterFailedBulk = (membersAfterFailedBulk.payload.members as Array<{ userId: string; role: string }>).find((member) => member.userId === batchMemberIDs[0]);
  expect(firstBatchMemberAfterFailedBulk?.role).toBe("operator");
  const bulkMemberRemoved = await writeJSON(page, "/api/admin/members/bulk", "POST", { action: "delete", ids: batchMemberIDs });
  expect(bulkMemberRemoved.ok).toBeTruthy();
  const removedRoleMap = new Map((bulkMemberRemoved.payload.members as Array<{ userId: string; role: string }>).map((member) => [member.userId, member.role]));
  expect(batchMemberIDs.every((id) => removedRoleMap.get(id) === "user")).toBeTruthy();

  await page.goto("/admin/gateways");
  await expect(page.getByText("企业网关治理")).toBeVisible();
  await expect(page.getByRole("button", { name: "批量暂停" })).toBeVisible();
  await expect(page.getByRole("button", { name: "批量删除" })).toBeVisible();
  await expect(page.getByRole("button", { name: "编辑" }).first()).toBeVisible();
  await page.getByRole("button", { name: "编辑" }).first().click();
  const adminGatewayStatusOptions = await page.locator("form.drawer.open label").filter({ hasText: "状态" }).locator("select option").allTextContents();
  expect(adminGatewayStatusOptions).toEqual(["Active", "Paused"]);
  await page.locator("form.drawer.open").getByRole("button", { name: "取消" }).click();
  await page.goto("/admin/members");
  await expect(page.getByText("平台成员与密钥")).toBeVisible();
  await expect(page.getByRole("button", { name: "批量改角色" })).toBeVisible();
  await page.getByPlaceholder("搜索成员 / 邮箱 / 分组...").fill(memberEmail);
  await page.getByLabel(`分组 ${memberEmail}`).fill("页面分组治理");
  await page.getByLabel(`分组 ${memberEmail}`).blur();
  await expect(page.locator("body")).toContainText("成员分组已更新");
  const memberGroupAfterUIEdit = await readJSON(page, "/api/admin/members");
  expect((memberGroupAfterUIEdit.payload.members as Array<{ userId: string; groupName: string }>).find((member) => member.userId === memberUserID)?.groupName).toBe("页面分组治理");
  await page.getByRole("button", { name: /API Key ·/ }).click();
  await expect(page.getByRole("button", { name: "批量吊销" })).toBeVisible();
  await expect(page.getByRole("button", { name: "批量删除" })).toBeVisible();
  await expect(page.getByRole("button", { name: "签发 API Key" })).toBeVisible();
  const adminBulkKeyStatusValues = await page.getByLabel("批量 Key 状态").locator("option").evaluateAll((options) => options.map((option) => (option as HTMLOptionElement).value));
  expect(adminBulkKeyStatusValues).toEqual(["active", "expired"]);
  await expect(page.getByLabel("批量 Key 状态")).toHaveValue("active");
  await expect(page.getByRole("button", { name: "删除" }).first()).toBeVisible();
  await page.getByRole("button", { name: "编辑" }).first().click();
  const adminKeyEditStatusValues = await page.getByLabel("Key 编辑状态").locator("option").evaluateAll((options) => options.map((option) => (option as HTMLOptionElement).value));
  expect(adminKeyEditStatusValues).toEqual(["active", "expired"]);
  await page.locator("form.drawer.open").getByRole("button", { name: "取消" }).click();

  const removedMember = await writeJSON(page, `/api/admin/members/${memberUserID}`, "DELETE", {});
  expect(removedMember.ok).toBeTruthy();
  const membersAfterSingleRemove = await readJSON(page, "/api/admin/members");
  const removedAsUser = (membersAfterSingleRemove.payload.members as Array<{ userId: string; role: string; email: string }>).find((member) => member.userId === memberUserID);
  expect(removedAsUser?.role).toBe("user");

  await page.goto("/admin/members");
  await page.getByRole("button", { name: /注册用户/ }).click();
  await page.getByPlaceholder("搜索成员 / 邮箱 / 分组...").fill(memberEmail);
  await expect(page.getByRole("link", { name: "查看", exact: true })).toHaveAttribute("href", `/admin/users?q=${encodeURIComponent(memberEmail)}`);

	  const revokedKey = await writeJSON(page, `/api/admin/gateway-keys/${keyID}/revoke`, "POST", {});
	  expect(revokedKey.ok).toBeTruthy();
	  const reactivatedKey = await writeJSON(page, `/api/admin/gateway-keys/${keyID}`, "PATCH", { status: "active" });
	  expect(reactivatedKey.ok).toBeFalsy();
	  expect(reactivatedKey.status).toBe(400);

	  const keyUnderDeletedGateway = await writeJSON(page, "/api/admin/gateway-keys", "POST", {
	    gatewayId: gatewayID,
	    name: `CRUD Deleted Gateway Key ${suffix}`,
	    quotaMonth: 1000,
	    qpsLimit: 10
	  });
	  expect(keyUnderDeletedGateway.status).toBe(201);
	  const keyUnderDeletedGatewayID = keyUnderDeletedGateway.payload.key.id as string;
	  const expiredUnderDeletedGateway = await writeJSON(page, `/api/admin/gateway-keys/${keyUnderDeletedGatewayID}`, "PATCH", { status: "expired" });
	  expect(expiredUnderDeletedGateway.ok).toBeTruthy();

	  const deletedGateway = await writeJSON(page, `/api/admin/gateways/${gatewayID}`, "DELETE", {});
	  expect(deletedGateway.ok).toBeTruthy();
	  const gatewaysAfterDelete = await readJSON(page, "/api/admin/gateways");
	  expect((gatewaysAfterDelete.payload.items as Array<{ id: string }>).map((item) => item.id)).not.toContain(gatewayID);
	  const keysAfterGatewayDelete = await readJSON(page, "/api/admin/gateway-keys");
	  expect((keysAfterGatewayDelete.payload.items as Array<{ id: string }>).map((key) => key.id)).not.toContain(keyUnderDeletedGatewayID);
	  const createKeyForDeletedGateway = await writeJSON(page, "/api/admin/gateway-keys", "POST", {
	    gatewayId: gatewayID,
	    name: `CRUD Blocked Key ${suffix}`,
	    quotaMonth: 1000,
	    qpsLimit: 10
	  });
	  expect(createKeyForDeletedGateway.ok).toBeFalsy();
	  expect(createKeyForDeletedGateway.status).toBe(404);
	  const patchKeyUnderDeletedGateway = await writeJSON(page, `/api/admin/gateway-keys/${keyUnderDeletedGatewayID}`, "PATCH", { status: "active" });
	  expect(patchKeyUnderDeletedGateway.ok).toBeFalsy();
	  expect(patchKeyUnderDeletedGateway.status).toBe(404);
	  const bulkStatusKeyUnderDeletedGateway = await writeJSON(page, "/api/admin/gateway-keys/bulk", "POST", { action: "status", ids: [keyUnderDeletedGatewayID], status: "active" });
	  expect(bulkStatusKeyUnderDeletedGateway.ok).toBeFalsy();
	  expect(bulkStatusKeyUnderDeletedGateway.status).toBe(404);
	  const revokeKeyUnderDeletedGateway = await writeJSON(page, `/api/admin/gateway-keys/${keyUnderDeletedGatewayID}/revoke`, "POST", {});
	  expect(revokeKeyUnderDeletedGateway.ok).toBeFalsy();
	  expect(revokeKeyUnderDeletedGateway.status).toBe(404);
	  const bulkRevokeKeyUnderDeletedGateway = await writeJSON(page, "/api/admin/gateway-keys/bulk", "POST", { action: "revoke", ids: [keyUnderDeletedGatewayID] });
	  expect(bulkRevokeKeyUnderDeletedGateway.ok).toBeFalsy();
	  expect(bulkRevokeKeyUnderDeletedGateway.status).toBe(404);
	  const cleanupKeyUnderDeletedGateway = await writeJSON(page, `/api/admin/gateway-keys/${keyUnderDeletedGatewayID}`, "DELETE", {});
	  expect(cleanupKeyUnderDeletedGateway.ok).toBeTruthy();

  const deletedUpstream = await writeJSON(page, `/api/admin/channels/${channelID}`, "DELETE", {});
  expect(deletedUpstream.ok).toBeTruthy();
	});

test("admin usage supports real filters, rollup recompute and CSV export", async ({ page }) => {
  await adminLogin(page, "/admin/usage");

  const suffix = Date.now();
  const createdChannel = await writeJSON(page, "/api/admin/channels", "POST", {
    name: `CRUD Usage Upstream ${suffix}`,
    provider: "OpenAI",
    type: "openai-compatible",
    model: "gpt-4o-mini",
    upstreamModel: "gpt-4o-mini",
    endpoint: `https://crud-usage-${suffix}.example/v1`,
    apiKey: `sk-crud-usage-${suffix}`,
    probeDaily: 1440,
    publicVisible: false,
    gatewayEnabled: true,
    enabled: true,
    inputPerMtok: 0.15,
    outputPerMtok: 0.6
  });
  expect(createdChannel.status).toBe(201);
  const channelID = createdChannel.payload.channel.id as string;
  const channelProbe = await writeJSON(page, `/api/admin/channels/${channelID}/probe-now`, "POST", {});
  expect(channelProbe.ok).toBeTruthy();

  const createdGateway = await writeJSON(page, "/api/admin/gateways", "POST", {
    name: `CRUD Usage Gateway ${suffix}`,
    policy: "latency",
    upstreamIds: [channelID],
    qpsLimit: 30,
    quotaMonth: 50000
  });
  expect(createdGateway.status).toBe(201);
  const gatewayID = createdGateway.payload.gateway.id as string;

  const createdKey = await writeJSON(page, "/api/admin/gateway-keys", "POST", {
    gatewayId: gatewayID,
    name: `CRUD Usage Key ${suffix}`,
    quotaMonth: 1000,
    qpsLimit: 10
  });
  expect(createdKey.status).toBe(201);
  const plainKey = createdKey.payload.key.plainKey as string;

  const gatewayCall = await page.evaluate(async ({ key }) => {
    const response = await fetch("/gateway/v1/chat/completions", {
      method: "POST",
      headers: {
        "Authorization": `Bearer ${key}`,
        "Content-Type": "application/json"
      },
      body: JSON.stringify({
        model: "gpt-4o-mini",
        messages: [{ role: "user", content: "ping" }]
      })
    });
    return { status: response.status, text: await response.text() };
  }, { key: plainKey });
  expect(gatewayCall.status).toBeGreaterThanOrEqual(200);

  const usageByGateway = await readJSON(page, `/api/admin/usage?days=7&source=gateway&gatewayId=${encodeURIComponent(gatewayID)}`);
  expect(usageByGateway.ok).toBeTruthy();
  expect(usageByGateway.payload.totals.requests).toBeGreaterThanOrEqual(1);
  expect((usageByGateway.payload.recent as Array<{ gateway: string }>).some((item) => item.gateway.includes(`CRUD Usage Gateway ${suffix}`))).toBeTruthy();

  const usageByModel = await readJSON(page, `/api/admin/usage?days=7&source=gateway&gatewayId=${encodeURIComponent(gatewayID)}&model=gpt-4o-mini`);
  expect(usageByModel.ok).toBeTruthy();
  expect(usageByModel.payload.totals.requests).toBeGreaterThanOrEqual(1);

  const memberUserID = (usageByModel.payload.rollups as Array<{ memberUserId: string }>).find((item) => item.memberUserId)?.memberUserId;
  expect(memberUserID).toBeTruthy();
  const usageByMember = await readJSON(page, `/api/admin/usage?days=7&source=gateway&gatewayId=${encodeURIComponent(gatewayID)}&memberUserId=${encodeURIComponent(memberUserID as string)}`);
  expect(usageByMember.ok).toBeTruthy();
  expect(usageByMember.payload.totals.requests).toBeGreaterThanOrEqual(1);

  const usageNoMatch = await readJSON(page, `/api/admin/usage?days=7&source=gateway&gatewayId=${encodeURIComponent(gatewayID)}&model=not-a-real-model`);
  expect(usageNoMatch.ok).toBeTruthy();
  expect(usageNoMatch.payload.totals.requests).toBe(0);

  const probeSource = await readJSON(page, `/api/admin/usage?days=7&source=probe&gatewayId=${encodeURIComponent(gatewayID)}`);
  expect(probeSource.ok).toBeTruthy();
  expect(probeSource.payload.totals.requests).toBe(0);
  expect(probeSource.payload.recent.length).toBe(0);

  const csv = await page.evaluate(async (url) => {
    const response = await fetch(url, { credentials: "include" });
    return { ok: response.ok, text: await response.text() };
  }, `/api/admin/usage/export?days=7&source=gateway&gatewayId=${encodeURIComponent(gatewayID)}`);
  expect(csv.ok).toBeTruthy();
  expect(csv.text).toContain("day,org_id,source,gateway_id");
  expect(csv.text).toContain(gatewayID);
  expect(csv.text).not.toContain(plainKey);

  await page.goto("/admin/usage");
  await page.getByLabel("用量时间范围").selectOption("7");
  await page.getByLabel("来源筛选").selectOption("gateway");
  await page.getByLabel("网关筛选").selectOption(gatewayID);
  await page.getByRole("button", { name: "应用筛选" }).click();
  await expect(page.locator("body")).toContainText(`CRUD Usage Gateway ${suffix}`);
  await expect(page.getByRole("link", { name: "导出 CSV" })).toBeVisible();
  await expect(page.getByRole("link", { name: "导出 CSV" })).toHaveAttribute("href", `/api/admin/usage/export?days=7&source=gateway&gatewayId=${encodeURIComponent(gatewayID)}`);
  await expect.poll(() => new URL(page.url()).searchParams.get("gatewayId")).toBe(gatewayID);
  await expect(page.getByRole("button", { name: "重新聚合" })).toBeVisible();
  page.once("dialog", (dialog) => void dialog.accept());
  await page.getByRole("button", { name: "重新聚合" }).click();
  await expect(page.locator("body")).toContainText("Daily rollup 已重算。");

  await page.goto(`/admin/usage?days=7&source=gateway&gatewayId=${encodeURIComponent(gatewayID)}&model=gpt-4o-mini`);
  await expect(page.locator("body")).toContainText(`CRUD Usage Gateway ${suffix}`);
  await expect(page.getByLabel("用量时间范围")).toHaveValue("7");
  await expect(page.getByLabel("来源筛选")).toHaveValue("gateway");
  await expect(page.getByLabel("网关筛选")).toHaveValue(gatewayID);
  await expect(page.getByLabel("模型筛选")).toHaveValue("gpt-4o-mini");
  await expect(page.getByRole("link", { name: "导出 CSV" })).toHaveAttribute("href", `/api/admin/usage/export?days=7&source=gateway&gatewayId=${encodeURIComponent(gatewayID)}&model=gpt-4o-mini`);
  await page.getByRole("button", { name: "重置" }).click();
  await expect(page.getByLabel("用量时间范围")).toHaveValue("30");
  await expect(page.getByLabel("来源筛选")).toHaveValue("");
  await expect.poll(() => new URL(page.url()).search).toBe("");
});

test("admin Open API sites support filterable CRUD and bulk governance", async ({ page }) => {
  await adminLogin(page, "/admin/open-api");

  const suffix = Date.now();
  const siteA = await writeJSON(page, "/api/admin/open-api/sites", "POST", {
    name: `CRUD Status Portal A ${suffix}`,
    scopes: ["overview", "channels"],
    qpsLimit: 11
  });
  expect(siteA.status).toBe(201);
  const siteAID = siteA.payload.site.id as string;
  const siteAKey = siteA.payload.site.plainKey as string;

  const siteB = await writeJSON(page, "/api/admin/open-api/sites", "POST", {
    name: `CRUD Status Portal B ${suffix}`,
    scopes: ["incidents"],
    qpsLimit: 12
  });
  expect(siteB.status).toBe(201);
  const siteBID = siteB.payload.site.id as string;
  const siteBKey = siteB.payload.site.plainKey as string;

  const edited = await writeJSON(page, `/api/admin/open-api/sites/${siteAID}`, "PATCH", {
    name: `CRUD Status Portal A Edited ${suffix}`,
    scopes: ["overview"],
    qpsLimit: 21,
    status: "paused"
  });
  expect(edited.ok).toBeTruthy();
  expect(edited.payload.site.name).toContain("Edited");
  expect(edited.payload.site.scopes).toEqual(["overview"]);
  expect(edited.payload.site.qpsLimit).toBe(21);
  expect(edited.payload.site.status).toBe("paused");
  expect(JSON.stringify(edited.payload)).not.toContain(siteAKey);

  const failedMixedOpenAPIStatus = await writeJSON(page, "/api/admin/open-api/sites/bulk", "POST", {
    action: "status",
    ids: [siteAID, `oas_missing_${suffix}`],
    status: "active"
  });
  expect(failedMixedOpenAPIStatus.ok).toBeFalsy();
  expect(failedMixedOpenAPIStatus.status).toBe(404);
  const openAPIAfterFailedBulk = await readJSON(page, "/api/admin/open-api");
  const siteAAfterFailedBulk = (openAPIAfterFailedBulk.payload.sites as Array<{ id: string; status: string }>).find((site) => site.id === siteAID);
  expect(siteAAfterFailedBulk?.status).toBe("paused");

  const failedMixedOpenAPIRevoke = await writeJSON(page, "/api/admin/open-api/sites/bulk", "POST", {
    action: "revoke",
    ids: [siteAID, `oas_missing_revoke_${suffix}`]
  });
  expect(failedMixedOpenAPIRevoke.ok).toBeFalsy();
  expect(failedMixedOpenAPIRevoke.status).toBe(404);
  const openAPIAfterFailedRevoke = await readJSON(page, "/api/admin/open-api");
  const siteAAfterFailedRevoke = (openAPIAfterFailedRevoke.payload.sites as Array<{ id: string; status: string }>).find((site) => site.id === siteAID);
  expect(siteAAfterFailedRevoke?.status).toBe("paused");

  const failedMixedOpenAPIDelete = await writeJSON(page, "/api/admin/open-api/sites/bulk", "POST", {
    action: "delete",
    ids: [siteAID, `oas_missing_delete_${suffix}`]
  });
  expect(failedMixedOpenAPIDelete.ok).toBeFalsy();
  expect(failedMixedOpenAPIDelete.status).toBe(404);
  const openAPIAfterFailedDelete = await readJSON(page, "/api/admin/open-api");
  const siteAAfterFailedDelete = (openAPIAfterFailedDelete.payload.sites as Array<{ id: string; status: string }>).find((site) => site.id === siteAID);
  expect(siteAAfterFailedDelete?.status).toBe("paused");

  const bulkActive = await writeJSON(page, "/api/admin/open-api/sites/bulk", "POST", {
    action: "status",
    ids: [siteAID, siteBID],
    status: "active"
  });
  expect(bulkActive.ok).toBeTruthy();
  const activeSites = new Map((bulkActive.payload.sites as Array<{ id: string; status: string }>).map((site) => [site.id, site.status]));
  expect(activeSites.get(siteAID)).toBe("active");
  expect(activeSites.get(siteBID)).toBe("active");

  const patchRevoked = await writeJSON(page, `/api/admin/open-api/sites/${siteAID}`, "PATCH", {
    status: "revoked"
  });
  expect(patchRevoked.ok).toBeFalsy();
  expect(patchRevoked.status).toBe(400);

  const bulkRevoked = await writeJSON(page, "/api/admin/open-api/sites/bulk", "POST", {
    action: "status",
    ids: [siteAID],
    status: "revoked"
  });
  expect(bulkRevoked.ok).toBeFalsy();
  expect(bulkRevoked.status).toBe(400);

  const openAPIBeforeDelete = await page.evaluate(async (siteKey) => {
    const response = await fetch("/v1/status/overview", { headers: { "X-Site-Key": siteKey } });
    return { ok: response.ok, status: response.status };
  }, siteAKey);
  expect(openAPIBeforeDelete.status).toBe(200);

  await page.goto(`/admin/open-api?q=${encodeURIComponent(`Portal A Edited ${suffix}`)}&status=active&scope=overview`);
  await expect(page.getByRole("heading", { name: "授权站点" })).toBeVisible();
  await expect(page.getByPlaceholder("搜索站点名、Key mask、scope...")).toHaveValue(`Portal A Edited ${suffix}`);
  await expect(page.getByLabel("授权站点状态筛选")).toHaveValue("active");
  await expect(page.getByLabel("授权站点 Scope 筛选")).toHaveValue("overview");
  await expect(page.locator("input.compact-name")).toHaveCount(1);
  await page.getByRole("button", { name: "清空筛选" }).click();
  await expect(page.getByPlaceholder("搜索站点名、Key mask、scope...")).toHaveValue("");
  await expect(page.getByLabel("授权站点状态筛选")).toHaveValue("all");
  await expect(page.getByLabel("授权站点 Scope 筛选")).toHaveValue("all");
  await expect.poll(() => new URL(page.url()).search).toBe("");
  await page.getByPlaceholder("搜索站点名、Key mask、scope...").fill(`Portal A Edited ${suffix}`);
  await expect.poll(() => new URL(page.url()).searchParams.get("q")).toBe(`Portal A Edited ${suffix}`);
  await expect(page.locator("input.compact-name")).toHaveCount(1);
  await expect(page.getByText("批量改状态")).toBeVisible();
  await expect(page.getByText("批量吊销")).toBeVisible();

  await page.getByPlaceholder("搜索站点名、Key mask、scope...").fill(`Portal B ${suffix}`);
  await expect(page.locator("input.compact-name")).toHaveCount(1);
  await page.getByLabel(`选择 CRUD Status Portal B ${suffix}`).check();
  page.once("dialog", (dialog) => void dialog.accept());
  await page.getByRole("button", { name: "批量吊销" }).click();
  await expect(page.locator("body")).toContainText("批量吊销完成。");
  const afterBulkRevoke = await readJSON(page, "/api/admin/open-api");
  const revokedSiteB = (afterBulkRevoke.payload.sites as Array<{ id: string; status: string }>).find((site) => site.id === siteBID);
  expect(revokedSiteB?.status).toBe("revoked");
  const openAPIAfterRevoke = await page.evaluate(async (siteKey) => {
    const response = await fetch("/v1/status/overview", { headers: { "X-Site-Key": siteKey } });
    return { ok: response.ok, status: response.status };
  }, siteBKey);
  expect(openAPIAfterRevoke.status).toBe(403);

  await page.goto("/admin/open-api");
  await page.getByPlaceholder("搜索站点名、Key mask、scope...").fill(`Portal B ${suffix}`);
  await expect(page.locator("input.compact-name")).toHaveCount(1);
  const revokedRow = page.locator("tr").filter({ has: page.getByLabel(`选择 CRUD Status Portal B ${suffix}`) });
  await expect(revokedRow.locator("input.compact-name")).toBeDisabled();
  await expect(revokedRow.locator("select.compact-select")).toBeDisabled();
  await expect(revokedRow.locator("select.compact-select")).toHaveValue("revoked");
  await expect(revokedRow.getByRole("button", { name: "删除" })).toBeEnabled();
  const patchRevokedName = await writeJSON(page, `/api/admin/open-api/sites/${siteBID}`, "PATCH", {
    name: `Should Not Edit ${suffix}`
  });
  expect(patchRevokedName.ok).toBeFalsy();
  expect(patchRevokedName.status).toBe(400);
  page.once("dialog", (dialog) => void dialog.accept());
  await revokedRow.getByRole("button", { name: "删除" }).click();
  await expect(page.locator("body")).toContainText("授权站点已删除。");
  const afterRevokedSingleDelete = await readJSON(page, "/api/admin/open-api");
  expect((afterRevokedSingleDelete.payload.sites as Array<{ id: string }>).some((site) => site.id === siteBID)).toBeFalsy();

  const bulkDeleted = await writeJSON(page, "/api/admin/open-api/sites/bulk", "POST", {
    action: "delete",
    ids: [siteAID]
  });
  expect(bulkDeleted.ok).toBeTruthy();
  const remainingSites = new Map((bulkDeleted.payload.sites as Array<{ id: string; status: string }>).map((site) => [site.id, site.status]));
  expect(remainingSites.has(siteAID)).toBeFalsy();

  const afterDeleteSummary = await readJSON(page, "/api/admin/open-api");
  expect(afterDeleteSummary.ok).toBeTruthy();
  const deletedVisible = (afterDeleteSummary.payload.sites as Array<{ id: string }>).some((site) => site.id === siteAID || site.id === siteBID);
  expect(deletedVisible).toBeFalsy();

  const openAPIAfterDelete = await page.evaluate(async (siteKey) => {
    const response = await fetch("/v1/status/overview", { headers: { "X-Site-Key": siteKey } });
    return { ok: response.ok, status: response.status };
  }, siteAKey);
  expect(openAPIAfterDelete.status).toBe(403);

  const reactivated = await writeJSON(page, `/api/admin/open-api/sites/${siteAID}`, "PATCH", {
    status: "active"
  });
  expect(reactivated.ok).toBeFalsy();
  expect(reactivated.status).toBe(404);
});

test("admin recommend operations support create, edit, publish and delete", async ({ page }) => {
  await adminLogin(page, "/admin/recommend");

  const original = await readJSON(page, "/api/admin/recommend");
  expect(original.ok).toBeTruthy();

  const suffix = Date.now();
  const publicChannels = await readJSON(page, "/api/public/channels?pageSize=20");
  expect(publicChannels.ok).toBeTruthy();
  const channel = (publicChannels.payload.items as Array<{ id: string }>)[0];
  expect(channel).toBeTruthy();
  const channelID = channel.id as string;

  try {
    const created = await writeJSON(page, "/api/admin/recommend", "PUT", {
      picks: [{
        id: "",
        channelId: channelID,
        position: 1,
        title: `CRUD Pick ${suffix}`,
        ribbon: "编辑精选",
        summary: "通过 CRUD 回归创建的推荐位",
        points: ["真实配置保存", "前台实时读取"],
        ctaLabel: "去官方体验",
        ctaUrl: `https://official-${suffix}.example.com/register`,
        enabled: true,
        channel
      }],
      rankRules: original.payload.rankRules,
      rewards: [{
        id: "",
        channelId: channelID,
        providerName: `CRUD Reward ${suffix}`,
        rewardType: "新人福利",
        rewardValue: "$5",
        code: "TOKHUB",
        expiresAtText: "长期有效",
        enabled: true
      }],
      scenarios: [{
        id: "",
        title: `CRUD Scenario ${suffix}`,
        icon: "AI",
        channelId: channelID,
        summary: "通过 CRUD 回归创建的场景推荐",
        position: 1,
        enabled: true,
        channel
      }]
    });
    expect(created.ok).toBeTruthy();
    expect(created.payload.picks[0].title).toBe(`CRUD Pick ${suffix}`);
    expect(created.payload.rewards[0].providerName).toBe(`CRUD Reward ${suffix}`);
    expect(created.payload.scenarios[0].title).toBe(`CRUD Scenario ${suffix}`);

    const publicCreated = await readJSON(page, "/api/public/recommend");
    expect(publicCreated.ok).toBeTruthy();
    expect(publicCreated.payload.picks.map((item: { title: string }) => item.title)).toContain(`CRUD Pick ${suffix}`);
    expect(publicCreated.payload.picks).toHaveLength(1);
    expect((publicCreated.payload.ranks as Array<{ channelId: string; title: string }>).map((item) => item.channelId)).toEqual([channelID]);
    expect((publicCreated.payload.ranks as Array<{ channelId: string; title: string }>).map((item) => item.title)).toEqual([`CRUD Pick ${suffix}`]);
    expect((publicCreated.payload.rankRules as Array<{ enabled: boolean }>).some((rule) => rule.enabled)).toBeTruthy();

    const pickID = created.payload.picks[0].id as string;
    const rewardID = created.payload.rewards[0].id as string;
    const scenarioID = created.payload.scenarios[0].id as string;
    const edited = await writeJSON(page, "/api/admin/recommend", "PUT", {
      picks: [{
        ...created.payload.picks[0],
        title: `CRUD Pick Edited ${suffix}`,
        summary: "编辑后的推荐摘要",
        points: ["编辑保存", "删除可回滚"],
        ctaLabel: "进入详情"
      }],
      rankRules: [{
        ...(created.payload.rankRules?.[0] ?? { id: "", position: 1 }),
        label: `CRUD Rank Rule ${suffix}`,
        description: "通过 CRUD 回归编辑的榜单规则",
        metric: "speed",
        enabled: true,
        position: 1
      }],
      rewards: [{
        ...created.payload.rewards[0],
        rewardValue: "$10",
        code: "TOKHUB10"
      }],
      scenarios: [{
        ...created.payload.scenarios[0],
        title: `CRUD Scenario Edited ${suffix}`,
        position: 1
      }]
    });
    expect(edited.ok).toBeTruthy();
    expect(edited.payload.picks[0].title).toBe(`CRUD Pick Edited ${suffix}`);
    expect(edited.payload.rankRules[0].label).toBe(`CRUD Rank Rule ${suffix}`);
    expect(edited.payload.rewards[0].rewardValue).toBe("$10");
    expect(edited.payload.scenarios[0].title).toBe(`CRUD Scenario Edited ${suffix}`);

    const invalidSave = await writeJSON(page, "/api/admin/recommend", "PUT", {
      picks: [{
        ...edited.payload.picks[0],
        channelId: `ch_missing_${suffix}`
      }],
      rankRules: edited.payload.rankRules,
      rewards: edited.payload.rewards,
      scenarios: edited.payload.scenarios
    });
    expect(invalidSave.ok).toBeFalsy();
    expect(invalidSave.status).toBe(400);
    expect(invalidSave.payload.error.code).toBe("recommend_config_invalid");
    const afterInvalidSave = await readJSON(page, "/api/admin/recommend");
    expect(afterInvalidSave.ok).toBeTruthy();
    expect(afterInvalidSave.payload.picks[0].title).toBe(`CRUD Pick Edited ${suffix}`);
    expect(afterInvalidSave.payload.scenarios[0].title).toBe(`CRUD Scenario Edited ${suffix}`);

    const click = await writeJSON(page, "/api/public/recommend/click", "POST", { itemType: "pick", itemId: pickID, channelId: channelID });
    expect(click.ok).toBeTruthy();

    await page.goto("/admin/recommend");
    await expect(page.getByRole("link", { name: /添加推荐位/ })).toHaveAttribute("href", "/admin/recommend/new");
    await expect(page.getByRole("button", { name: "复制" }).first()).toBeVisible();
    await expect(page.getByRole("button", { name: "删除" }).first()).toBeVisible();

    await page.getByPlaceholder("搜索推荐标题 / 摘要 / 模型...").fill(`CRUD Pick Edited ${suffix}`);
    await expect(page.getByText(`CRUD Pick Edited ${suffix}`).first()).toBeVisible();
    await page.getByLabel(`选择推荐位 CRUD Pick Edited ${suffix}`).check();
    await page.getByRole("button", { name: "批量停用" }).click();
    await page.locator("select.input.compact-select").selectOption("disabled");
    await expect(page.getByText(`CRUD Pick Edited ${suffix}`).first()).toBeVisible();
    await page.getByRole("button", { name: "批量启用" }).click();
    await page.locator("select.input.compact-select").selectOption("enabled");
    await expect(page.getByText(`CRUD Pick Edited ${suffix}`).first()).toBeVisible();
    await page.getByRole("button", { name: "批量复制" }).click();
    await expect(page.getByText(`CRUD Pick Edited ${suffix} 副本`).first()).toBeVisible();
    await page.getByLabel(`选择推荐位 CRUD Pick Edited ${suffix} 副本`).check();
    await page.getByRole("button", { name: "批量删除" }).click();
    await expect(page.locator("body")).not.toContainText(`CRUD Pick Edited ${suffix} 副本`);

    await page.getByRole("button", { name: "多维度榜单" }).click();
    expect(await hasInputValue(page, `CRUD Rank Rule ${suffix}`)).toBeTruthy();
    await expect(page.getByRole("button", { name: "＋ 新增规则" })).toBeVisible();
    await page.getByRole("button", { name: "＋ 新增规则" }).click();
    expect(await hasInputValue(page, "自定义榜单")).toBeTruthy();
    await fillInputWithValue(page, "自定义榜单", `UI Rank Rule ${suffix}`);
    await page.getByPlaceholder("搜索榜单名称 / 指标 / 说明...").fill(`UI Rank Rule ${suffix}`);
    await page.getByLabel(`选择榜单规则 UI Rank Rule ${suffix}`).check();
    await page.getByRole("button", { name: "批量停用" }).click();
    await page.locator("select.input.compact-select").selectOption("disabled");
    expect(await hasInputValue(page, `UI Rank Rule ${suffix}`)).toBeTruthy();
    await page.getByRole("button", { name: "批量启用" }).click();
    await page.locator("select.input.compact-select").selectOption("enabled");
    expect(await hasInputValue(page, `UI Rank Rule ${suffix}`)).toBeTruthy();
    await page.getByRole("button", { name: "批量复制" }).click();
    expect(await hasInputValue(page, `UI Rank Rule ${suffix} 副本`)).toBeTruthy();
    await page.getByPlaceholder("搜索榜单名称 / 指标 / 说明...").fill(`UI Rank Rule ${suffix} 副本`);
    await page.getByLabel(`选择榜单规则 UI Rank Rule ${suffix} 副本`).check();
    await page.getByRole("button", { name: "批量删除" }).click();
    await expect(page.locator("body")).not.toContainText(`UI Rank Rule ${suffix} 副本`);
    await page.getByPlaceholder("搜索榜单名称 / 指标 / 说明...").fill("");

    await page.getByRole("button", { name: "新人福利" }).click();
    await page.getByPlaceholder("搜索福利服务商 / 类型 / 优惠码...").fill(`CRUD Reward ${suffix}`);
    await expect(page.getByText(`CRUD Reward ${suffix}`).first()).toBeVisible();
    await page.getByLabel(`选择福利模板 CRUD Reward ${suffix}`).check();
    await page.getByRole("button", { name: "批量停用" }).click();
    await page.locator("select.input.compact-select").selectOption("disabled");
    await expect(page.getByText(`CRUD Reward ${suffix}`).first()).toBeVisible();
    await page.getByRole("button", { name: "批量启用" }).click();
    await page.locator("select.input.compact-select").selectOption("enabled");
    await expect(page.getByText(`CRUD Reward ${suffix}`).first()).toBeVisible();
    await page.getByRole("button", { name: "批量复制" }).click();
    await expect(page.getByText(`CRUD Reward ${suffix} 副本`).first()).toBeVisible();
    await page.getByLabel(`选择福利模板 CRUD Reward ${suffix} 副本`).check();
    await page.getByRole("button", { name: "批量删除" }).click();
    await expect(page.locator("body")).not.toContainText(`CRUD Reward ${suffix} 副本`);

    await page.getByRole("button", { name: "场景化推荐" }).click();
    await page.getByPlaceholder("搜索场景标题 / 摘要 / 模型...").fill(`CRUD Scenario Edited ${suffix}`);
    expect(await hasInputValue(page, `CRUD Scenario Edited ${suffix}`)).toBeTruthy();
    await page.getByLabel(`选择场景推荐 CRUD Scenario Edited ${suffix}`).check();
    await page.getByRole("button", { name: "批量停用" }).click();
    await page.locator("select.input.compact-select").selectOption("disabled");
    expect(await hasInputValue(page, `CRUD Scenario Edited ${suffix}`)).toBeTruthy();
    await page.getByRole("button", { name: "批量启用" }).click();
    await page.locator("select.input.compact-select").selectOption("enabled");
    expect(await hasInputValue(page, `CRUD Scenario Edited ${suffix}`)).toBeTruthy();
    await page.getByRole("button", { name: "批量复制" }).click();
    expect(await hasInputValue(page, `CRUD Scenario Edited ${suffix} 副本`)).toBeTruthy();
    await page.getByLabel(`选择场景推荐 CRUD Scenario Edited ${suffix} 副本`).check();
    await page.getByRole("button", { name: "批量删除" }).click();
    await expect(page.locator("body")).not.toContainText(`CRUD Scenario Edited ${suffix} 副本`);

    await page.getByRole("button", { name: "保存全部配置" }).click();
    await expect(page.locator("body")).toContainText("推荐配置已保存");
    const pageSaved = await readJSON(page, "/api/admin/recommend");
    expect(pageSaved.ok).toBeTruthy();
    expect((pageSaved.payload.rankRules as Array<{ label: string }>).map((rule) => rule.label)).toContain(`UI Rank Rule ${suffix}`);

    const deleted = await writeJSON(page, "/api/admin/recommend", "PUT", {
      picks: [],
      rankRules: [],
      rewards: [],
      scenarios: []
    });
    expect(deleted.ok).toBeTruthy();
    expect(deleted.payload.picks).toHaveLength(0);
    expect(deleted.payload.rankRules).toHaveLength(0);
    expect(deleted.payload.rewards).toHaveLength(0);
    expect(deleted.payload.scenarios).toHaveLength(0);

    const publicDeleted = await readJSON(page, "/api/public/recommend");
    expect(publicDeleted.payload.picks.map((item: { id: string }) => item.id)).not.toContain(pickID);
    expect(publicDeleted.payload.rewards.map((item: { id: string }) => item.id)).not.toContain(rewardID);
    expect(publicDeleted.payload.scenarios.map((item: { id: string }) => item.id)).not.toContain(scenarioID);
  } finally {
    await writeJSON(page, "/api/admin/recommend", "PUT", {
      picks: original.payload.picks,
      rankRules: original.payload.rankRules,
      rewards: original.payload.rewards,
      scenarios: original.payload.scenarios
    });
  }
});

test("admin platform channel list can add and remove featured recommendations", async ({ page }) => {
  await adminLogin(page, "/admin/channels");

  const original = await readJSON(page, "/api/admin/recommend");
  expect(original.ok).toBeTruthy();
  const adminChannelsData = await readJSON(page, "/api/admin/channels");
  expect(adminChannelsData.ok).toBeTruthy();
  const channels = adminChannelsData.payload.items as Array<{ id: string; name: string; publicVisible: boolean; status: string; recommended: boolean }>;
  const target = channels.find((channel) => channel.publicVisible && channel.status !== "disabled");
  expect(target).toBeTruthy();

  const originalPicks = original.payload.picks as Array<{ channelId: string }>;
  const targetWasRecommended = originalPicks.some((pick) => pick.channelId === target!.id);
  const slotToFree = !targetWasRecommended && originalPicks.length >= 3 ? originalPicks[0].channelId : "";

  try {
    if (targetWasRecommended) {
      const removed = await writeJSON(page, `/api/admin/channels/${target!.id}/recommend`, "DELETE");
      expect(removed.ok).toBeTruthy();
      expect(removed.payload.channel.recommended).toBeFalsy();
    } else if (slotToFree) {
      const removed = await writeJSON(page, `/api/admin/channels/${slotToFree}/recommend`, "DELETE");
      expect(removed.ok).toBeTruthy();
    }

    await page.goto("/admin/channels");
    await page.locator(".channel-filter-toolbar .tb-search input").fill(target!.name);
    const row = page.locator(".admin-channel-table tbody tr").filter({ hasText: target!.name }).first();
    await expect(row.getByRole("button", { name: "加入推荐" })).toBeVisible();

    await row.getByRole("button", { name: "加入推荐" }).click();
    await expect(row.getByRole("button", { name: "取消推荐" })).toBeVisible();
    const afterAdd = await readJSON(page, "/api/admin/channels");
    const addedChannel = (afterAdd.payload.items as Array<{ id: string; recommended: boolean }>).find((channel) => channel.id === target!.id);
    expect(addedChannel?.recommended).toBeTruthy();

    await row.getByRole("button", { name: "取消推荐" }).click();
    await expect(row.getByRole("button", { name: "加入推荐" })).toBeVisible();
    const afterRemove = await readJSON(page, "/api/admin/channels");
    const removedChannel = (afterRemove.payload.items as Array<{ id: string; recommended: boolean }>).find((channel) => channel.id === target!.id);
    expect(removedChannel?.recommended).toBeFalsy();
  } finally {
    await writeJSON(page, "/api/admin/recommend", "PUT", {
      picks: original.payload.picks,
      rankRules: original.payload.rankRules,
      rewards: original.payload.rewards,
      scenarios: original.payload.scenarios
    });
  }
});

test("admin alert rules and notification channels support edit, filter, bulk update and delete", async ({ page }) => {
  await adminLogin(page, "/admin/alerts");

  const suffix = Date.now();
  const createdRule = await writeJSON(page, "/api/admin/alerts/rules", "POST", {
    name: `CRUD Alert Rule ${suffix}`,
    kind: "cost_threshold",
    severity: "warning",
    threshold: 3,
    windowMinutes: 60,
    dedupeMinutes: 30,
    enabled: true
  });
  expect(createdRule.status).toBe(201);
  const ruleID = createdRule.payload.rule.id as string;

  const editedRule = await writeJSON(page, `/api/admin/alerts/rules/${ruleID}`, "PATCH", {
    name: `CRUD Alert Rule Edited ${suffix}`,
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
    const created = await writeJSON(page, "/api/admin/alerts/rules", "POST", {
      name: `CRUD Bulk Alert Rule ${index} ${suffix}`,
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

  const disabledRules = await writeJSON(page, "/api/admin/alerts/rules/bulk", "POST", { action: "disable", ids: batchRuleIDs });
  expect(disabledRules.ok).toBeTruthy();
  expect((disabledRules.payload.items as Array<{ id: string; enabled: boolean }>).filter((rule) => batchRuleIDs.includes(rule.id)).every((rule) => !rule.enabled)).toBeTruthy();

  const enabledRules = await writeJSON(page, "/api/admin/alerts/rules/bulk", "POST", { action: "enable", ids: batchRuleIDs });
  expect(enabledRules.ok).toBeTruthy();
  expect((enabledRules.payload.items as Array<{ id: string; enabled: boolean }>).filter((rule) => batchRuleIDs.includes(rule.id)).every((rule) => rule.enabled)).toBeTruthy();

  const failedMixedRuleDisable = await writeJSON(page, "/api/admin/alerts/rules/bulk", "POST", {
    action: "disable",
    ids: [batchRuleIDs[0], `alr_missing_${suffix}`]
  });
  expect(failedMixedRuleDisable.ok).toBeFalsy();
  expect(failedMixedRuleDisable.status).toBe(404);
  const rulesAfterFailedDisable = await readJSON(page, "/api/admin/alerts");
  expect((rulesAfterFailedDisable.payload.rules as Array<{ id: string; enabled: boolean }>).find((rule) => rule.id === batchRuleIDs[0])?.enabled).toBeTruthy();

  const failedMixedRuleDelete = await writeJSON(page, "/api/admin/alerts/rules/bulk", "POST", {
    action: "delete",
    ids: [batchRuleIDs[0], `alr_missing_delete_${suffix}`]
  });
  expect(failedMixedRuleDelete.ok).toBeFalsy();
  expect(failedMixedRuleDelete.status).toBe(404);
  const rulesAfterFailedDelete = await readJSON(page, "/api/admin/alerts");
  expect((rulesAfterFailedDelete.payload.rules as Array<{ id: string }>).map((rule) => rule.id)).toContain(batchRuleIDs[0]);

  const createdChannel = await writeJSON(page, "/api/admin/alerts/channels", "POST", {
    name: `CRUD Notify ${suffix}`,
    type: "email",
    target: `ops-${suffix}@tokhub.run`,
    enabled: true
  });
  expect(createdChannel.status).toBe(201);
  const channelID = createdChannel.payload.channel.id as string;

  const webhookSecret = `notify-secret-${suffix}`;
  const editedChannel = await writeJSON(page, `/api/admin/alerts/channels/${channelID}`, "PATCH", {
    name: `CRUD Notify Edited ${suffix}`,
    type: "webhook",
    target: `https://notify.invalid/hook/${webhookSecret}?token=${webhookSecret}`,
    enabled: false
  });
  expect(editedChannel.ok).toBeTruthy();
  expect(editedChannel.payload.channel.name).toContain("Edited");
  expect(editedChannel.payload.channel.type).toBe("webhook");
  expect(editedChannel.payload.channel.enabled).toBeFalsy();
  expect(JSON.stringify(editedChannel.payload.channel)).not.toContain(webhookSecret);
  expect(editedChannel.payload.channel.targetMask).toBe("https://notify.invalid/***");

  const preservedChannel = await writeJSON(page, `/api/admin/alerts/channels/${channelID}`, "PATCH", {
    name: `CRUD Notify Edited ${suffix}`,
    type: "webhook",
    target: "",
    enabled: false
  });
  expect(preservedChannel.ok).toBeTruthy();
  expect(preservedChannel.payload.channel.targetMask).toBe("https://notify.invalid/***");
  expect(JSON.stringify(preservedChannel.payload.channel)).not.toContain(webhookSecret);

  const batchChannelIDs: string[] = [];
  for (const index of [1, 2]) {
    const created = await writeJSON(page, "/api/admin/alerts/channels", "POST", {
      name: `CRUD Bulk Notify ${index} ${suffix}`,
      type: "email",
      target: `ops-bulk-${index}-${suffix}@tokhub.run`,
      enabled: true
    });
    expect(created.status).toBe(201);
    batchChannelIDs.push(created.payload.channel.id as string);
  }

  const disabledChannels = await writeJSON(page, "/api/admin/alerts/channels/bulk", "POST", { action: "disable", ids: batchChannelIDs });
  expect(disabledChannels.ok).toBeTruthy();
  expect((disabledChannels.payload.items as Array<{ id: string; enabled: boolean }>).filter((channel) => batchChannelIDs.includes(channel.id)).every((channel) => !channel.enabled)).toBeTruthy();

  const enabledChannels = await writeJSON(page, "/api/admin/alerts/channels/bulk", "POST", { action: "enable", ids: batchChannelIDs });
  expect(enabledChannels.ok).toBeTruthy();
  expect((enabledChannels.payload.items as Array<{ id: string; enabled: boolean }>).filter((channel) => batchChannelIDs.includes(channel.id)).every((channel) => channel.enabled)).toBeTruthy();

  const failedMixedChannelDisable = await writeJSON(page, "/api/admin/alerts/channels/bulk", "POST", {
    action: "disable",
    ids: [batchChannelIDs[0], `ntc_missing_${suffix}`]
  });
  expect(failedMixedChannelDisable.ok).toBeFalsy();
  expect(failedMixedChannelDisable.status).toBe(404);
  const channelsAfterFailedDisable = await readJSON(page, "/api/admin/alerts");
  expect((channelsAfterFailedDisable.payload.channels as Array<{ id: string; enabled: boolean }>).find((channel) => channel.id === batchChannelIDs[0])?.enabled).toBeTruthy();

  const failedMixedChannelDelete = await writeJSON(page, "/api/admin/alerts/channels/bulk", "POST", {
    action: "delete",
    ids: [batchChannelIDs[0], `ntc_missing_delete_${suffix}`]
  });
  expect(failedMixedChannelDelete.ok).toBeFalsy();
  expect(failedMixedChannelDelete.status).toBe(404);
  const channelsAfterFailedDelete = await readJSON(page, "/api/admin/alerts");
  expect((channelsAfterFailedDelete.payload.channels as Array<{ id: string }>).map((channel) => channel.id)).toContain(batchChannelIDs[0]);

  await page.goto("/admin/alerts");
  await expect(page.locator("body")).toContainText(`CRUD Alert Rule Edited ${suffix}`);
  await expect(page.getByRole("button", { name: "编辑" }).first()).toBeVisible();
  await expect(page.getByRole("button", { name: "删除" }).first()).toBeVisible();
  await expect(page.getByPlaceholder("筛选规则名称 / 类型 / 级别")).toBeVisible();
  await page.getByPlaceholder("筛选规则名称 / 类型 / 级别").fill(`CRUD Alert Rule Edited ${suffix}`);
  await page.getByLabel(`选择告警规则 CRUD Alert Rule Edited ${suffix}`).check();
  await page.getByRole("button", { name: "批量启用" }).first().click();
  await expect(page.locator("body")).toContainText("选中告警规则已启用。");

  await page.getByRole("tab", { name: /通知渠道/ }).click();
  await expect(page.locator("body")).toContainText(`CRUD Notify Edited ${suffix}`);
  await expect(page.locator("body")).toContainText("https://notify.invalid/***");
  await expect(page.locator("body")).not.toContainText(webhookSecret);
  await expect(page.getByPlaceholder("筛选渠道名称 / 类型 / 目标")).toBeVisible();
  await page.getByPlaceholder("筛选渠道名称 / 类型 / 目标").fill(`CRUD Notify Edited ${suffix}`);
  await page.getByLabel(`选择通知渠道 CRUD Notify Edited ${suffix}`).check();
  await page.getByRole("button", { name: "批量启用" }).click();
  await expect(page.locator("body")).toContainText("选中通知渠道已启用。");

  const incidentChannel = await writeJSON(page, "/api/admin/channels", "POST", {
    name: `CRUD Incident Channel ${suffix}`,
    provider: "OpenAI",
    type: "openai-compatible",
    model: "gpt-4o-mini",
    upstreamModel: "gpt-4o-mini",
    endpoint: `https://crud-incident-${suffix}.invalid/v1`,
    apiKey: `sk-crud-incident-${suffix}`,
    probeDaily: 1440,
    publicVisible: true,
    gatewayEnabled: false,
    enabled: true,
    inputPerMtok: 0.15,
    outputPerMtok: 0.6
  });
  expect(incidentChannel.status).toBe(201);
  const incidentChannelID = incidentChannel.payload.channel.id as string;
  const incidentTitle = `CRUD Incident ${suffix}`;

  await page.goto("/admin/alerts");
  await page.getByRole("tab", { name: /Incident 治理/ }).click();
  const incidentCreateForm = page.locator("form.incident-form");
  const incidentChannelSelect = incidentCreateForm.locator("select").filter({ hasText: "选择要登记的通道" }).first();
  if (await incidentChannelSelect.isVisible().catch(() => false)) {
    const selectedIncidentChannelID = await incidentChannelSelect.evaluate((select, channelID) => {
      const element = select as HTMLSelectElement;
      const option = Array.from(element.options).find((item) => item.value === channelID);
      if (!option) return "";
      element.value = channelID;
      element.dispatchEvent(new Event("change", { bubbles: true }));
      return element.value;
    }, incidentChannelID);
    expect(selectedIncidentChannelID).toBe(incidentChannelID);
    await expect(incidentChannelSelect).toHaveValue(incidentChannelID);
  } else {
    await incidentCreateForm.locator("input.mono").first().fill(incidentChannelID);
  }
  await page.getByPlaceholder("例如：供应商模型不可用").fill(incidentTitle);
  await page.getByPlaceholder("记录排查背景、影响范围或处置动作").fill("E2E 手动登记 Incident");
  await page.getByRole("button", { name: "登记事件" }).click();
  await expect(page.locator("body")).toContainText("Incident 已登记。");
  await expect(page.getByRole("row").filter({ hasText: incidentTitle })).toBeVisible();

  await page.getByPlaceholder("筛选标题 / 通道 / 状态").fill(incidentTitle);
  await page.getByRole("button", { name: "筛选" }).click();
  await expect(page.getByRole("row").filter({ hasText: incidentTitle })).toBeVisible();
  const createdIncident = await readJSON(page, `/api/admin/incidents?q=${encodeURIComponent(incidentTitle)}&status=open`);
  expect(createdIncident.ok).toBeTruthy();
  const incidentID = createdIncident.payload.items[0].id as string;
  const editedIncidentTitle = `CRUD Incident Edited ${suffix}`;
  await page.getByRole("row").filter({ hasText: incidentTitle }).getByRole("button", { name: "编辑" }).click();
  const incidentForm = page.locator("form").filter({ hasText: "编辑 Incident" });
  await expect(incidentForm).toBeVisible();
  await incidentForm.getByPlaceholder("例如：供应商模型不可用").fill(editedIncidentTitle);
  await incidentForm.locator("select.input").selectOption("auth_error");
  await incidentForm.getByPlaceholder("记录排查背景、影响范围或处置动作").fill("E2E 编辑 Incident");
  await incidentForm.getByRole("button", { name: "更新事件" }).click();
  await expect(page.locator("body")).toContainText("Incident 已更新。");
  const editedIncident = await readJSON(page, `/api/admin/incidents?q=${encodeURIComponent(editedIncidentTitle)}&status=auth_error`);
  expect(editedIncident.ok).toBeTruthy();
  expect((editedIncident.payload.items as Array<{ id: string; status: string; title: string }>).some((incident) => incident.id === incidentID && incident.status === "auth_error" && incident.title === editedIncidentTitle)).toBeTruthy();
  await page.getByPlaceholder("筛选标题 / 通道 / 状态").fill(editedIncidentTitle);
  await page.getByRole("button", { name: "筛选" }).click();
  await expect(page.getByRole("row").filter({ hasText: editedIncidentTitle })).toBeVisible();

  await page.getByRole("row").filter({ hasText: editedIncidentTitle }).getByRole("button", { name: "关闭" }).click();
  await expect(page.locator("body")).toContainText("Incident 已关闭。");
  const resolvedIncident = await readJSON(page, `/api/admin/incidents?q=${encodeURIComponent(editedIncidentTitle)}&status=resolved`);
  expect((resolvedIncident.payload.items as Array<{ id: string }>).map((incident) => incident.id)).toContain(incidentID);

  await page.getByRole("row").filter({ hasText: editedIncidentTitle }).getByRole("button", { name: "重开" }).click();
  await expect(page.locator("body")).toContainText("Incident 已重开。");
  const reopenedIncident = await readJSON(page, `/api/admin/incidents?q=${encodeURIComponent(editedIncidentTitle)}&status=open`);
  expect((reopenedIncident.payload.items as Array<{ id: string }>).map((incident) => incident.id)).toContain(incidentID);

  page.once("dialog", (dialog) => void dialog.accept());
  await page.getByRole("row").filter({ hasText: editedIncidentTitle }).getByRole("button", { name: "删除" }).click();
  await expect(page.locator("body")).toContainText("Incident 已删除。");
  const deletedIncident = await readJSON(page, `/api/admin/incidents?q=${encodeURIComponent(editedIncidentTitle)}`);
  expect((deletedIncident.payload.items as Array<{ id: string }>).map((incident) => incident.id)).not.toContain(incidentID);

  const bulkIncidentIDs: string[] = [];
  for (const index of [1, 2]) {
    const bulkChannel = await writeJSON(page, "/api/admin/channels", "POST", {
      name: `CRUD Batch Channel ${index} ${suffix}`,
      provider: "OpenAI",
      type: "openai-compatible",
      model: "gpt-4o-mini",
      upstreamModel: "gpt-4o-mini",
      endpoint: `https://crud-bulk-incident-${index}-${suffix}.invalid/v1`,
      apiKey: `sk-crud-bulk-incident-${index}-${suffix}`,
      probeDaily: 1440,
      publicVisible: false,
      gatewayEnabled: false,
      enabled: false,
      inputPerMtok: 0.15,
      outputPerMtok: 0.6
    });
    expect(bulkChannel.status).toBe(201);
    const createdIncident = await writeJSON(page, "/api/admin/incidents", "POST", {
      channelId: bulkChannel.payload.channel.id,
      status: "manual",
      title: `CRUD Bulk Incident ${index} ${suffix}`,
      message: "E2E 批量治理 Incident"
    });
    expect(createdIncident.status).toBe(201);
    bulkIncidentIDs.push(createdIncident.payload.id as string);
  }

  const failedMixedIncidentBulk = await writeJSON(page, "/api/admin/incidents/bulk", "POST", {
    action: "close",
    ids: [bulkIncidentIDs[0], `inc_missing_${suffix}`],
    message: "mixed id should not partially close"
  });
  expect(failedMixedIncidentBulk.ok).toBeFalsy();
  expect(failedMixedIncidentBulk.status).toBe(404);
  const incidentsAfterFailedBulk = await readJSON(page, `/api/admin/incidents?q=${encodeURIComponent("CRUD Bulk Incident")}&status=open`);
  expect((incidentsAfterFailedBulk.payload.items as Array<{ id: string; open: boolean }>).filter((incident) => bulkIncidentIDs.includes(incident.id)).every((incident) => incident.open)).toBeTruthy();

  await page.goto("/admin/alerts");
  await page.getByRole("tab", { name: /Incident 治理/ }).click();
  await page.getByPlaceholder("筛选标题 / 通道 / 状态").fill(`CRUD Bulk Incident`);
  await page.getByRole("button", { name: "筛选" }).click();
  const incidentBoard = page.locator(".card.board").filter({ has: page.getByPlaceholder("筛选标题 / 通道 / 状态") });
  await expect(page.getByRole("row").filter({ hasText: `CRUD Bulk Incident 1 ${suffix}` })).toBeVisible();
  await expect(page.getByRole("row").filter({ hasText: `CRUD Bulk Incident 2 ${suffix}` })).toBeVisible();
  await incidentBoard.locator("select.incident-mode-select").selectOption("bulk");
  await page.getByLabel("选择当前页 Incident").check();
  const incidentBulkActions = incidentBoard.locator(".incident-bulk-actions");
  await expect(incidentBulkActions.getByRole("button", { name: "关闭" })).toBeVisible();
  await expect(incidentBulkActions.getByRole("button", { name: "重开" })).toBeVisible();
  await expect(incidentBulkActions.getByRole("button", { name: "删除" })).toBeVisible();

  page.once("dialog", (dialog) => void dialog.accept());
  await incidentBulkActions.getByRole("button", { name: "关闭" }).click();
  await expect(page.locator("body")).toContainText("Incident 已批量关闭。");
  const bulkResolved = await readJSON(page, `/api/admin/incidents?q=${encodeURIComponent("CRUD Bulk Incident")}&status=resolved`);
  expect((bulkResolved.payload.items as Array<{ id: string; open: boolean }>).filter((incident) => bulkIncidentIDs.includes(incident.id)).every((incident) => !incident.open)).toBeTruthy();

  await page.getByLabel("选择当前页 Incident").check();
  page.once("dialog", (dialog) => void dialog.accept());
  await incidentBulkActions.getByRole("button", { name: "重开" }).click();
  await expect(page.locator("body")).toContainText("Incident 已批量重开。");
  const bulkReopened = await readJSON(page, `/api/admin/incidents?q=${encodeURIComponent("CRUD Bulk Incident")}&status=open`);
  expect((bulkReopened.payload.items as Array<{ id: string; open: boolean }>).filter((incident) => bulkIncidentIDs.includes(incident.id)).every((incident) => incident.open)).toBeTruthy();

  await page.getByLabel("选择当前页 Incident").check();
  page.once("dialog", (dialog) => void dialog.accept());
  await incidentBulkActions.getByRole("button", { name: "删除" }).click();
  await expect(page.locator("body")).toContainText("Incident 已批量删除。");
  const bulkDeletedIncidents = await readJSON(page, `/api/admin/incidents?q=${encodeURIComponent("CRUD Bulk Incident")}`);
  expect((bulkDeletedIncidents.payload.items as Array<{ id: string }>).some((incident) => bulkIncidentIDs.includes(incident.id))).toBeFalsy();

  const deletedRule = await writeJSON(page, `/api/admin/alerts/rules/${ruleID}`, "DELETE", {});
  expect(deletedRule.ok).toBeTruthy();
  const deletedChannel = await writeJSON(page, `/api/admin/alerts/channels/${channelID}`, "DELETE", {});
  expect(deletedChannel.ok).toBeTruthy();
  const deletedBatchRules = await writeJSON(page, "/api/admin/alerts/rules/bulk", "POST", { action: "delete", ids: batchRuleIDs });
  expect(deletedBatchRules.ok).toBeTruthy();
  const deletedBatchChannels = await writeJSON(page, "/api/admin/alerts/channels/bulk", "POST", { action: "delete", ids: batchChannelIDs });
  expect(deletedBatchChannels.ok).toBeTruthy();

  const center = await readJSON(page, "/api/admin/alerts");
  expect((center.payload.rules as Array<{ id: string }>).map((rule) => rule.id)).not.toContain(ruleID);
  expect((center.payload.channels as Array<{ id: string }>).map((channel) => channel.id)).not.toContain(channelID);
  for (const id of batchRuleIDs) {
    expect((center.payload.rules as Array<{ id: string }>).map((rule) => rule.id)).not.toContain(id);
  }
  for (const id of batchChannelIDs) {
    expect((center.payload.channels as Array<{ id: string }>).map((channel) => channel.id)).not.toContain(id);
  }

  const defaultRuleID = (center.payload.rules as Array<{ id: string; name: string }>)
    .find((rule) => rule.id.startsWith("alr_admin_") && rule.name.includes("超过"))?.id;
  const defaultChannelID = (center.payload.channels as Array<{ id: string; name: string }>)
    .find((channel) => channel.id.endsWith("_email") && channel.name === "默认邮件通知")?.id;

  if (defaultRuleID) {
    const deletedDefaultRule = await writeJSON(page, `/api/admin/alerts/rules/${defaultRuleID}`, "DELETE", {});
    expect(deletedDefaultRule.ok).toBeTruthy();
  }
  if (defaultChannelID) {
    const deletedDefaultChannel = await writeJSON(page, `/api/admin/alerts/channels/${defaultChannelID}`, "DELETE", {});
    expect(deletedDefaultChannel.ok).toBeTruthy();
  }

  const afterDefaultDelete = await readJSON(page, "/api/admin/alerts");
  if (defaultRuleID) {
    expect((afterDefaultDelete.payload.rules as Array<{ id: string }>).map((rule) => rule.id)).not.toContain(defaultRuleID);
  }
  if (defaultChannelID) {
    expect((afterDefaultDelete.payload.channels as Array<{ id: string }>).map((channel) => channel.id)).not.toContain(defaultChannelID);
  }
  expect((afterDefaultDelete.payload.channels as Array<{ id: string; name: string }>).some((channel) => channel.id.endsWith("_email") && channel.name === "默认邮件通知")).toBeFalsy();

  const evaluated = await writeJSON(page, "/api/admin/alerts/evaluate", "POST", {});
  expect(evaluated.ok).toBeTruthy();
  const afterEvaluate = await readJSON(page, "/api/admin/alerts");
  if (defaultRuleID) {
    expect((afterEvaluate.payload.rules as Array<{ id: string }>).map((rule) => rule.id)).not.toContain(defaultRuleID);
  }
  if (defaultChannelID) {
    expect((afterEvaluate.payload.channels as Array<{ id: string }>).map((channel) => channel.id)).not.toContain(defaultChannelID);
  }
  expect((afterEvaluate.payload.channels as Array<{ id: string; name: string }>).some((channel) => channel.id.endsWith("_email") && channel.name === "默认邮件通知")).toBeFalsy();
});

test("admin web settings, system settings and audit filters are governable", async ({ page }) => {
  await adminLogin(page, "/admin/web");

  const suffix = Date.now();
  const original = await readJSON(page, "/api/admin/web");
  expect(original.ok).toBeTruthy();
  const originalOrgs = await readJSON(page, "/api/admin/orgs?q=tokhub");
  expect(originalOrgs.ok).toBeTruthy();
  const originalPlatformOrg = (originalOrgs.payload.items as Array<{ id: string; name: string; slug: string; plan: string; status: string; timezone: string; dataOrigin: string }>).find((item) => item.id === "org_default");
  expect(originalPlatformOrg).toBeTruthy();

  try {
    const invalid = await writeJSON(page, "/api/admin/web", "PATCH", {
      ...original.payload.site,
      navItems: [{ label: "Bad", href: "javascript:alert(1)" }],
      footerLinks: original.payload.site.footerLinks
    });
    expect(invalid.status).toBe(400);

    const savedWeb = await writeJSON(page, "/api/admin/web", "PATCH", {
      ...original.payload.site,
      brandName: `TokHub CRUD ${suffix}`,
      logoMark: "TC",
      subtitle: "CRUD 后台治理验证",
      footerText: `CRUD Footer ${suffix}`,
      navItems: [
        { label: "首页", href: "/" },
        { label: "监控总览", href: "/dashboard" },
        { label: "CRUD 页面", href: `/crud-${suffix}` }
      ],
      footerLinks: [
        { label: "控制台", href: "/console" },
        { label: "外部文档", href: "https://example.com/docs" }
      ]
    });
    expect(savedWeb.ok).toBeTruthy();
    expect(savedWeb.payload.site.brandName).toBe(`TokHub CRUD ${suffix}`);
    expect(savedWeb.payload.site.navItems.map((item: { label: string }) => item.label)).toContain("CRUD 页面");

    const publicSite = await readJSON(page, "/api/public/site-config");
    expect(publicSite.payload.brandName).toBe(`TokHub CRUD ${suffix}`);

    await page.goto("/admin/web");
    await expect(page.getByRole("button", { name: "恢复默认" })).toBeVisible();
    await expect(page.locator("body")).toContainText(`TokHub CRUD ${suffix}`);
    await page.getByLabel("选择顶部导航菜单CRUD 页面").check();
    await page.getByRole("button", { name: "批量复制" }).first().click();
    expect(await hasInputValue(page, "CRUD 页面 副本")).toBeTruthy();
    await page.getByLabel("选择顶部导航菜单CRUD 页面 副本").check();
    await page.getByRole("button", { name: "批量删除" }).first().click();
    expect(await hasInputValue(page, "CRUD 页面 副本")).toBeFalsy();
    await page.getByLabel("选择页脚链接外部文档").check();
    await page.getByRole("button", { name: "批量复制" }).nth(1).click();
    expect(await hasInputValue(page, "外部文档 副本")).toBeTruthy();
    await page.getByRole("button", { name: "保存更改" }).click();
    await expect(page.locator("body")).toContainText("网站配置已保存");
    const bulkSavedWeb = await readJSON(page, "/api/admin/web");
    expect(bulkSavedWeb.ok).toBeTruthy();
    expect(bulkSavedWeb.payload.site.navItems.map((item: { label: string }) => item.label)).not.toContain("CRUD 页面 副本");
    expect(bulkSavedWeb.payload.site.footerLinks.map((item: { label: string }) => item.label)).toContain("外部文档 副本");

    const webResetBaselineSettings = await writeJSON(page, "/api/admin/settings", "PATCH", {
      defaultGatewayPolicy: "cost",
      timezone: "America/Los_Angeles"
    });
    expect(webResetBaselineSettings.ok).toBeTruthy();
    expect(webResetBaselineSettings.payload.site.defaultGatewayPolicy).toBe("cost");
    expect(webResetBaselineSettings.payload.site.timezone).toBe("America/Los_Angeles");

    const reset = await writeJSON(page, "/api/admin/web/reset", "POST", {});
    expect(reset.ok).toBeTruthy();
    expect(reset.payload.site.brandName).toBe("TokHub");
    expect(reset.payload.site.navItems.map((item: { label: string }) => item.label)).toContain("监控总览");
    expect(reset.payload.site.defaultGatewayPolicy).toBe("cost");
    expect(reset.payload.site.timezone).toBe("America/Los_Angeles");

    const invalidAdminPath = await writeJSON(page, "/api/admin/settings", "PATCH", {
      adminPath: "/api/admin-ui"
    });
    expect(invalidAdminPath.status).toBe(400);

    const customAdminPath = `/ops-admin-${suffix}`;
    const savedSettings = await writeJSON(page, "/api/admin/settings", "PATCH", {
      brandName: `TokHub Settings ${suffix}`,
    logoMark: "TS",
    subtitle: "设置页保存验证",
    footerText: `Settings Footer ${suffix}`,
    adminPath: customAdminPath,
    showRegisterCta: false,
    defaultGatewayPolicy: "success",
    timezone: "UTC"
  });
  expect(savedSettings.ok).toBeTruthy();
  expect(savedSettings.payload.site.brandName).toBe(`TokHub Settings ${suffix}`);
	  expect(savedSettings.payload.site.adminPath).toBe(customAdminPath);
	  expect(savedSettings.payload.site.showRegisterCta).toBeFalsy();
	  expect(savedSettings.payload.site.defaultGatewayPolicy).toBe("success");
	  expect(savedSettings.payload.site.timezone).toBe("UTC");

		  const clearedOptionalText = await writeJSON(page, "/api/admin/settings", "PATCH", {
		    subtitle: "",
		    footerText: ""
		  });
		  expect(clearedOptionalText.ok).toBeTruthy();
		  expect(clearedOptionalText.payload.site.subtitle).toBe("");
		  expect(clearedOptionalText.payload.site.footerText).toBe("");
		  const adminWebAfterClear = await readJSON(page, "/api/admin/web");
		  expect(adminWebAfterClear.payload.site.subtitle).toBe("");
		  expect(adminWebAfterClear.payload.site.footerText).toBe("");
		  const publicSiteAfterClear = await readJSON(page, "/api/public/site-config");
		  expect(publicSiteAfterClear.payload.subtitle).toBe("");
		  expect(publicSiteAfterClear.payload.footerText).toBe("");

	  const platformOrgName = `TokHub Platform ${suffix}`;
  const platformOrgSlug = `tokhub-platform-${suffix}`;
  const savedPlatformOrg = await writeJSON(page, "/api/admin/orgs/org_default", "PATCH", {
    name: platformOrgName,
    slug: platformOrgSlug,
    plan: "enterprise",
    status: "active",
    timezone: "UTC",
    dataOrigin: "system"
  });
  expect(savedPlatformOrg.ok).toBeTruthy();

  const settingsSummary = await readJSON(page, "/api/admin/settings");
  expect(settingsSummary.ok).toBeTruthy();
  expect(settingsSummary.payload.site.adminPath).toBe(customAdminPath);
  expect(settingsSummary.payload.site.defaultGatewayPolicy).toBe("success");
  expect(settingsSummary.payload.site.timezone).toBe("UTC");
  expect(typeof settingsSummary.payload.summary.activeSessions).toBe("number");
  expect(typeof settingsSummary.payload.summary.enabledAdminAlertRules).toBe("number");
  expect(settingsSummary.payload.summary.enabledAdminAlertRules).toBeLessThanOrEqual(settingsSummary.payload.summary.enabledAlertRules);
  expect(typeof settingsSummary.payload.summary.enabledNotificationChannels).toBe("number");
  expect(typeof settingsSummary.payload.summary.openApiSites).toBe("number");
  expect(settingsSummary.payload.summary.platformOrg.id).toBe("org_default");
  expect(settingsSummary.payload.summary.platformOrg.name).toBe(platformOrgName);
  expect(settingsSummary.payload.summary.platformOrg.slug).toBe(platformOrgSlug);
  expect(typeof settingsSummary.payload.summary.platformChannels).toBe("number");
  expect(typeof settingsSummary.payload.summary.users).toBe("number");
  expect(typeof settingsSummary.payload.summary.orgs).toBe("number");
  expect(typeof settingsSummary.payload.summary.recommendPicks).toBe("number");

  await page.goto("/admin/settings");
  await page.waitForURL((url) => url.pathname === `${customAdminPath}/settings`);
  await expect(page.getByRole("button", { name: "恢复品牌默认" })).toBeVisible();
  await expect(page.getByRole("link", { name: "进入用户管理" })).toBeVisible();
  await expect(page.locator("body")).toContainText(platformOrgName);
  await expect(page.locator("body")).toContainText(platformOrgSlug);
  await expect(page.getByLabel("后台管理员地址")).toHaveValue(customAdminPath);
  await expect(page.getByRole("link", { name: "编辑组织资料" })).toHaveAttribute("href", `${customAdminPath}/orgs?q=${platformOrgSlug}`);
  await expect(page.locator(".org-pick")).toContainText(platformOrgName);
  await expect(page.locator(".org-pick")).toHaveAttribute("href", `${customAdminPath}/orgs?q=${platformOrgSlug}`);
  await expect(page.locator(".sb-link").filter({ hasText: "平台通道" }).locator(".mini")).toHaveText(sidebarCountLabel(settingsSummary.payload.summary.platformChannels));
  await expect(page.locator(".sb-link").filter({ hasText: "用户管理" }).locator(".mini")).toHaveText(sidebarCountLabel(settingsSummary.payload.summary.users));
  await expect(page.locator(".sb-link").filter({ hasText: "组织管理" }).locator(".mini")).toHaveText(sidebarCountLabel(settingsSummary.payload.summary.orgs));
  await expect(page.locator(".sb-link").filter({ hasText: "开放 API" }).locator(".mini")).toHaveText(sidebarCountLabel(settingsSummary.payload.summary.openApiSites));
  await expect(page.locator(".sb-link").filter({ hasText: "精选推荐" }).locator(".mini")).toHaveText(sidebarCountLabel(settingsSummary.payload.summary.recommendPicks));
  await expect(page.locator(".sb-link").filter({ hasText: "告警规则" }).locator(".mini")).toHaveText(sidebarCountLabel(settingsSummary.payload.summary.enabledAdminAlertRules));
  await page.getByLabel("默认路由策略").selectOption("cost");
  await page.getByLabel("平台时区").selectOption("America/Los_Angeles");
  await page.getByRole("button", { name: "保存更改" }).click();
  await expect(page.locator("body")).toContainText("设置已保存");
  const uiSavedSettings = await readJSON(page, "/api/admin/settings");
  expect(uiSavedSettings.payload.site.adminPath).toBe(customAdminPath);
  expect(uiSavedSettings.payload.site.defaultGatewayPolicy).toBe("cost");
  expect(uiSavedSettings.payload.site.timezone).toBe("America/Los_Angeles");
  await page.getByRole("button", { name: "计费" }).click();
    await expect(page.locator("body")).toContainText("套餐与计费治理");
    await expect(page.getByRole("link", { name: "查看用量" })).toBeVisible();
    await expect(page.getByRole("link", { name: "管理网关" })).toBeVisible();
    await page.getByRole("button", { name: "安全" }).click();
    await expect(page.locator("body")).toContainText("安全策略治理");
    await expect(page.getByRole("link", { name: "管理用户" })).toBeVisible();
    await expect(page.getByRole("link", { name: "查看审计" })).toBeVisible();
    await page.getByRole("button", { name: "集成" }).click();
    await expect(page.locator("body")).toContainText("集成与通知治理");
    await expect(page.getByRole("link", { name: "管理通知" })).toBeVisible();
    await expect(page.getByRole("link", { name: "管理授权" })).toBeVisible();
    await expect(page.locator("body")).not.toContainText("Phase Backlog");
    await expect(page.locator("body")).not.toContainText("规划中");

    const auditActor = adminEmail();
    const today = new Date().toISOString().slice(0, 10);
    const audit = await readJSON(page, `/api/admin/audit?action=${encodeURIComponent("site.web_config.reset")}&objectType=site_config&actor=${encodeURIComponent(auditActor)}&from=${today}&limit=50`);
    expect(audit.ok).toBeTruthy();
    expect((audit.payload.items as Array<{ action: string; objectType: string }>).some((item) => item.action === "site.web_config.reset" && item.objectType === "site_config")).toBeTruthy();

    await page.goto(`/admin/audit?actor=${encodeURIComponent(auditActor)}&objectId=site&objectType=site_config&action=${encodeURIComponent("site.web_config.reset")}&limit=50`);
    await expect(page.getByPlaceholder("操作人邮箱 / ID")).toHaveValue(auditActor);
    await expect(page.getByPlaceholder("对象 ID")).toHaveValue("site");
    await expect(page.getByPlaceholder("精确 action")).toHaveValue("site.web_config.reset");
    await expect(page.getByRole("row").filter({ hasText: "site.web_config.reset" }).first()).toBeVisible();
    await expect(page.getByRole("button", { name: "导出 CSV" })).toBeVisible();
    await page.getByRole("button", { name: "重置" }).click();
    await expect(page.getByPlaceholder("操作人邮箱 / ID")).toHaveValue("");
    await expect(page.getByPlaceholder("对象 ID")).toHaveValue("");
    await expect.poll(() => new URL(page.url()).search).toBe("");

    const csv = await page.evaluate(async () => {
      const response = await fetch("/api/admin/audit/export?action=site.web_config.reset&objectType=site_config&limit=500", { credentials: "include" });
      return { ok: response.ok, text: await response.text() };
    });
    expect(csv.ok).toBeTruthy();
    expect(csv.text).toContain("site.web_config.reset");
    expect(csv.text).not.toContain("password");
  } finally {
    await writeJSON(page, "/api/admin/web", "PATCH", original.payload.site);
    if (originalPlatformOrg) {
      await writeJSON(page, "/api/admin/orgs/org_default", "PATCH", {
        name: originalPlatformOrg.name,
        slug: originalPlatformOrg.slug,
        plan: originalPlatformOrg.plan,
        status: "active",
        timezone: originalPlatformOrg.timezone,
        dataOrigin: originalPlatformOrg.dataOrigin
      });
    }
  }
});

test("unknown routes render public not-found instead of admin placeholder modules", async ({ page }) => {
  await page.goto("/missing-admin-module-check");
  await expect(page.getByRole("heading", { name: "页面不存在" })).toBeVisible();
  await expect(page.getByRole("link", { name: "返回首页" })).toBeVisible();
  await expect(page.getByRole("link", { name: "监控总览" })).toBeVisible();
  await expect(page.locator("body")).not.toContainText("只读发布占位状态");
  await expect(page.locator("body")).not.toContainText("该平台管理模块已接入后台壳");

  await page.goto("/admin/missing-admin-module-check");
  await expect(page.getByRole("heading", { name: "页面不存在" })).toBeVisible();
  await expect(page.locator("body")).not.toContainText("平台管理后台");
  await expect(page.locator("body")).not.toContainText("平台总览");
  await expect(page.locator("body")).not.toContainText("只读发布占位状态");

  await page.goto("/console/missing-console-module-check");
  await expect(page.getByRole("heading", { name: "页面不存在" })).toBeVisible();
  await expect(page.locator("body")).not.toContainText("用户控制台");
  await expect(page.locator("body")).not.toContainText("我的关注");
  await expect(page.locator("body")).not.toContainText("该平台管理模块已接入后台壳");
});
