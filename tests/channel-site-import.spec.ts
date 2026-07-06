import { expect, test } from "@playwright/test";
import { writeFileSync } from "node:fs";
import { adminLogin } from "./helpers";

test("channel site package import supports template download and CSV preview", async ({ page }, testInfo) => {
  await adminLogin(page, "/admin/open-api");
  await page.getByRole("button", { name: "渠道站点包" }).click();

  await expect(page.getByText("批量导入站点包")).toBeVisible();

  const downloadPromise = page.waitForEvent("download");
  await page.getByRole("button", { name: "下载导入模板" }).click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toBe("tokhub-channel-site-import-template.csv");

  const csvPath = testInfo.outputPath("channel-site-import.csv");
  writeFileSync(
    csvPath,
    [
      "site_name,domain,public_url,title,description,overview_label,recommend_label,logo_mark,qps_limit,status,modules,home_intro,recommend_intro,footer_text,canonical_url,nav1_label,nav1_href,nav2_label,nav2_href,nav3_label,nav3_href",
      "Example Status,status.example.com,https://status.example.com,Example API 中转站监控,实时查看 Example 渠道的 API 可用性和精选推荐,监控总览,精选推荐,E,30,active,overview;channelBoard;recommend,查看 API 中转站实时可用性,精选稳定中转站与福利入口,Powered by TokHub,https://status.example.com,官网,https://example.com,控制台,https://tokhub.run/console,帮助,https://example.com/docs"
    ].join("\n"),
    "utf8"
  );

  await page.locator("input[type=file]").setInputFiles(csvPath);
  await expect(page.getByText("已解析 1 行，1 行可导入")).toBeVisible();
  await expect(page.getByRole("button", { name: "导入并生成下载" })).toBeEnabled();
});
