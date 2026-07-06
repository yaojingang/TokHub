import { expect, test } from "@playwright/test";

const publicPages = [
  {
    path: "/",
    title: "AI API 中转站监控、精选推荐和专属网关",
    description: "首页入口",
    body: "AI 摘要要点",
    modules: ["首页主视觉", "实时监控快照", "首页指标条", "桌面工作台入口", "监控亮点", "精选推荐预览", "使用流程", "常见问题"]
  },
  {
    path: "/dashboard",
    title: "监控总览",
    description: "真实生成 L3",
    body: "公开监控总览",
    modules: ["关键指标", "通道明细看板", "通道明细数据", "监控策略", "供应商排行", "错误分类分布", "公开状态 API"]
  },
  {
    path: "/pricing",
    title: "成本预估",
    description: "成本估算",
    body: "成本预估与 AI API 月成本估算",
    modules: ["模型成本预估", "价格数据范围", "成本估算参数", "当前成本结果", "官方价格表", "表格筛选", "价格排行", "读表口径", "官方来源"]
  },
  {
    path: "/recommend",
    title: "精选推荐",
    description: "使用场景",
    body: "精选 AI API 中转站推荐",
    modules: ["推荐首屏摘要", "推荐统计", "本周编辑精选", "多维度榜单", "创建个人网关", "接入入口说明", "看你怎么用", "凭什么靠谱？看认证逻辑", "开放 API 同步推荐状态"]
  }
];

for (const pageMeta of publicPages) {
  test(`${pageMeta.path} exposes SEO and GEO-readable metadata`, async ({ request }) => {
    const response = await request.get(pageMeta.path);
    expect(response.ok()).toBeTruthy();
    const html = await response.text();

    expect(html).toContain(pageMeta.title);
    expect(html).toContain(pageMeta.description);
    expect(html).toContain(pageMeta.body);
    for (const moduleTitle of pageMeta.modules) {
      expect(html).toContain(moduleTitle);
    }
    expect(html).toContain(`<link rel="canonical"`);
    expect(html).toContain(`<meta property="og:locale" content="zh_CN" />`);
    expect(html).toContain(`<meta name="twitter:title"`);
    expect(html).toContain(`type="application/ld+json"`);
  });
}

test("pricing page is included in sitemap and llms.txt", async ({ request }) => {
  const sitemap = await (await request.get("/sitemap.xml")).text();
  expect(sitemap).toContain("/pricing");

  const llms = await (await request.get("/llms.txt")).text();
  expect(llms).toContain("Cost estimation:");
  expect(llms).toContain("## Cost Estimation Workbench");
});

test("channel detail uses short URL and exposes intro in static HTML", async ({ request }) => {
  const listResponse = await request.get("/api/public/channels?page=1&pageSize=1");
  expect(listResponse.ok()).toBeTruthy();
  const list = await listResponse.json();
  const channel = list.items[0] as { id: string; publicSlug: string; name: string };
  expect(channel.publicSlug).toMatch(/^[a-z0-9]{6,16}$/);

  const response = await request.get(`/channels/${channel.publicSlug}`);
  expect(response.ok()).toBeTruthy();
  const html = await response.text();

  expect(html).toContain(`<link rel="canonical"`);
  expect(html).toContain(`/channels/${channel.publicSlug}`);
  expect(html).not.toContain(`/channels/${channel.id}`);
  expect(html).toContain(`${channel.name} 官方介绍`);
  expect(html).toContain("官方注册 / 体验");
  expect(html).toContain("综合健康指数");
});
