import { expect, Page, test } from "@playwright/test";
import { adminLogin } from "./helpers";

test.describe.configure({ mode: "serial" });

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
    const response = await fetch(targetPath, { credentials: "include" });
    return { ok: response.ok, status: response.status, payload: await response.json() };
  }, path);
}

async function openAPI(page: Page, path: string, key: string) {
  return page.evaluate(
    async ({ targetPath, siteKey }) => {
      const response = await fetch(targetPath, { headers: { "X-Site-Key": siteKey } });
      return { ok: response.ok, status: response.status, payload: await response.json() };
    },
    { targetPath: path, siteKey: key }
  );
}

test("phase 6 recommendations are API driven, editable and tracked", async ({ page }) => {
  await adminLogin(page);
  const initial = await readJSON(page, "/api/admin/recommend");
  expect(initial.ok).toBeTruthy();
  expect(initial.payload.picks.length).toBeGreaterThanOrEqual(3);
  expect(initial.payload.rewards.length).toBeGreaterThanOrEqual(1);
  const original = JSON.parse(JSON.stringify(initial.payload));
  const draft = JSON.parse(JSON.stringify(initial.payload));
  draft.picks[0].ribbon = "Phase6 首推";
  draft.picks[0].summary = "Phase6 API 驱动推荐配置";
  draft.rewards[0].code = "PHASE6";
  const rewardLabel = `${draft.rewards[0].rewardValue} · PHASE6`;

  try {
    const saved = await writeJSON(page, "/api/admin/recommend", "PUT", {
      picks: draft.picks,
      rewards: draft.rewards,
      scenarios: draft.scenarios
    });
    expect(saved.ok).toBeTruthy();

    await page.goto("/recommend");
    await expect(page.getByRole("heading", { name: /不踩坑的/ })).toBeVisible();
    await expect(page.getByText("Phase6 首推")).toBeVisible();
    await expect(page.getByText(rewardLabel, { exact: true }).first()).toBeVisible();
    const publicOverview = await readJSON(page, "/api/public/overview");
    expect(publicOverview.ok).toBeTruthy();
    const monitoredChannelValue = new Intl.NumberFormat("zh-CN").format(publicOverview.payload.total as number);
    const stats = page.locator(".rec-stats");
    await expect(stats.locator(".rs", { hasText: "监控通道数" }).locator(".rs-v")).toHaveText(monitoredChannelValue);
    await expect(page.getByText("累计推荐点击")).toHaveCount(0);

    const click = await writeJSON(page, "/api/public/recommend/click", "POST", {
      itemType: "pick",
      itemId: draft.picks[0].id,
      channelId: draft.picks[0].channelId
    });
    expect(click.status).toBe(202);
    const afterClick = await readJSON(page, "/api/admin/recommend");
    expect(afterClick.ok).toBeTruthy();
    expect(afterClick.payload.stats.clicks).toBeGreaterThanOrEqual(initial.payload.stats.clicks);
  } finally {
    await writeJSON(page, "/api/admin/recommend", "PUT", {
      picks: original.picks,
      rewards: original.rewards,
      scenarios: original.scenarios
    });
  }
});

test("phase 6 admin recommendations retain disabled editable picks while public recommendations hide them", async ({ page }) => {
  await adminLogin(page);
  const initial = await readJSON(page, "/api/admin/recommend");
  expect(initial.ok).toBeTruthy();
  expect(initial.payload.picks.length).toBeGreaterThanOrEqual(2);
  const original = JSON.parse(JSON.stringify(initial.payload));
  const generatedMonitorPoint = "基于 TokHub 实时监控数据进入精选推荐";
  const staleGeneratedPoints = (pick: { channel: { provider: string; model: string } }) => [
    "综合评分 999，适合优先评估",
    `${pick.channel.provider} · ${pick.channel.model}`,
    generatedMonitorPoint
  ];
  const disabledPick = { ...original.picks[0], enabled: false, points: staleGeneratedPoints(original.picks[0]) };
  const enabledPick = { ...original.picks[1], enabled: true, points: staleGeneratedPoints(original.picks[1]) };

  try {
    const saved = await writeJSON(page, "/api/admin/recommend", "PUT", {
      picks: [disabledPick, enabledPick],
      rankRules: original.rankRules,
      rewards: original.rewards,
      scenarios: original.scenarios
    });
    expect(saved.ok).toBeTruthy();
    expect(saved.payload.picks.map((pick: { id: string }) => pick.id)).toContain(disabledPick.id);
    expect(saved.payload.picks.find((pick: { id: string }) => pick.id === disabledPick.id)?.enabled).toBeFalsy();
    const savedDisabledPick = saved.payload.picks.find((pick: { id: string }) => pick.id === disabledPick.id);
    const savedEnabledPick = saved.payload.picks.find((pick: { id: string }) => pick.id === enabledPick.id);
    expect(savedDisabledPick.points[0]).toBe(`综合评分 ${savedDisabledPick.channel.score}，适合优先评估`);
    expect(savedEnabledPick.points[0]).toBe(`综合评分 ${savedEnabledPick.channel.score}，适合优先评估`);

    const adminAfterSave = await readJSON(page, "/api/admin/recommend");
    expect(adminAfterSave.ok).toBeTruthy();
    expect(adminAfterSave.payload.picks.map((pick: { id: string }) => pick.id)).toContain(disabledPick.id);
    expect(adminAfterSave.payload.picks.find((pick: { id: string }) => pick.id === disabledPick.id)?.enabled).toBeFalsy();

    const publicAfterSave = await readJSON(page, "/api/public/recommend");
    expect(publicAfterSave.ok).toBeTruthy();
    expect(publicAfterSave.payload.picks.map((pick: { id: string }) => pick.id)).not.toContain(disabledPick.id);
    expect(publicAfterSave.payload.picks.map((pick: { id: string }) => pick.id)).toContain(enabledPick.id);
    const publicEnabledPick = publicAfterSave.payload.picks.find((pick: { id: string }) => pick.id === enabledPick.id);
    expect(publicEnabledPick.points[0]).toBe(`综合评分 ${publicEnabledPick.channel.score}，适合优先评估`);
  } finally {
    await writeJSON(page, "/api/admin/recommend", "PUT", {
      picks: original.picks,
      rankRules: original.rankRules,
      rewards: original.rewards,
      scenarios: original.scenarios
    });
  }
});

test("phase 6 Open API Site Key, scope and QPS controls work", async ({ page }) => {
  await adminLogin(page);
  const created = await writeJSON(page, "/api/admin/open-api/sites", "POST", {
    name: `Phase6 Site ${Date.now()}`,
    scopes: ["overview", "channels", "uptime", "incidents"],
    qpsLimit: 30
  });
  expect(created.status).toBe(201);
  const plainKey = created.payload.site.plainKey as string;
  expect(plainKey).toMatch(/^site-th-/);

  const listAfterCreate = await readJSON(page, "/api/admin/open-api");
  expect(listAfterCreate.ok).toBeTruthy();
  expect(JSON.stringify(listAfterCreate.payload)).not.toContain(plainKey);
  expect(JSON.stringify(listAfterCreate.payload)).toContain(created.payload.site.siteKeyMask);
  expect(listAfterCreate.payload.endpoints.map((item: { path: string }) => item.path).join(" ")).toContain("/v1/status/overview");

  const overview = await openAPI(page, "/v1/status/overview", plainKey);
  expect(overview.ok).toBeTruthy();
  expect(overview.payload.total).toBeGreaterThan(0);

  const channels = await openAPI(page, "/v1/status/channels", plainKey);
  expect(channels.ok).toBeTruthy();
  expect(channels.payload.items.length).toBeGreaterThan(0);
  const channelID = channels.payload.items[0].id as string;
  const channel = await openAPI(page, `/v1/status/channels/${channelID}`, plainKey);
  expect(channel.ok).toBeTruthy();
  expect(channel.payload.channel.id).toBe(channelID);

  const uptime = await openAPI(page, "/v1/status/uptime", plainKey);
  expect(uptime.ok).toBeTruthy();
  const incidents = await openAPI(page, "/v1/status/incidents", plainKey);
  expect(incidents.ok).toBeTruthy();

  const limited = await writeJSON(page, "/api/admin/open-api/sites", "POST", {
    name: `Phase6 Limited ${Date.now()}`,
    scopes: ["overview"],
    qpsLimit: 30
  });
  const limitedKey = limited.payload.site.plainKey as string;
  const forbidden = await openAPI(page, "/v1/status/incidents", limitedKey);
  expect(forbidden.status).toBe(403);

  const qpsSite = await writeJSON(page, "/api/admin/open-api/sites", "POST", {
    name: `Phase6 QPS ${Date.now()}`,
    scopes: ["overview"],
    qpsLimit: 1
  });
  const qpsKey = qpsSite.payload.site.plainKey as string;
  const statuses = await page.evaluate(async (siteKey) => {
    return Promise.all([0, 1].map(async () => {
      const response = await fetch("/v1/status/overview", { headers: { "X-Site-Key": siteKey } });
      return response.status;
    }));
  }, qpsKey);
  expect(statuses).toContain(429);
});

test("phase 6 Open API CORS preflight allows Site Key header", async ({ request, baseURL }) => {
  const origin = new URL(baseURL ?? "http://localhost:8080").origin;
  const response = await request.fetch("/v1/status/overview", {
    method: "OPTIONS",
    headers: {
      "Origin": origin,
      "Access-Control-Request-Method": "GET",
      "Access-Control-Request-Headers": "x-site-key"
    }
  });
  expect(response.status()).toBe(204);
  expect((response.headers()["access-control-allow-headers"] ?? "").toLowerCase()).toContain("x-site-key");
});

test("phase 6 web config drives public site config and admin pages render", async ({ page }) => {
  await adminLogin(page);
  const current = await readJSON(page, "/api/admin/web");
  expect(current.ok).toBeTruthy();
  const original = JSON.parse(JSON.stringify(current.payload.site));
  const draft = {
    ...original,
    brandName: "TokHub Phase6",
    logoMark: "P6",
    subtitle: "Phase6 Open API",
    showRegisterCta: true,
    navItems: [
      { label: "首页", href: "/" },
      { label: "监控总览", href: "/dashboard" },
      { label: "精选推荐", href: "/recommend" },
      { label: "开放 API", href: "/admin/open-api" }
    ],
    footerLinks: [
      { label: "精选推荐", href: "/recommend" },
      { label: "开放 API", href: "/admin/open-api" }
    ]
  };

  try {
    const saved = await writeJSON(page, "/api/admin/web", "PATCH", draft);
    expect(saved.ok).toBeTruthy();
    const publicConfig = await readJSON(page, "/api/public/site-config");
    expect(publicConfig.payload.brandName).toBe("TokHub Phase6");
    expect(publicConfig.payload.navItems.map((item: { label: string }) => item.label)).toContain("开放 API");

    await page.goto("/");
    await expect(page.getByText("TokHub Phase6").first()).toBeVisible();
    await page.goto("/admin/recommend");
    await expect(page.getByRole("heading", { name: "精选推荐管理" })).toBeVisible();
    await page.goto("/admin/open-api");
    await expect(page.getByRole("heading", { name: "开放 API" })).toBeVisible();
    await expect(page.getByText("/v1/status/overview").first()).toBeVisible();
    await page.goto("/admin/web");
    await expect(page.getByRole("heading", { name: "网站设置" })).toBeVisible();
  } finally {
    await writeJSON(page, "/api/admin/web", "PATCH", original);
  }
});
