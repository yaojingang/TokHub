import { expect, test } from "@playwright/test";
import { adminLogin } from "./helpers";

test("phase 1 routes render", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: /先看可用性.*再选择中转站/ })).toBeVisible();
  await page.goto("/dashboard");
  await expect(page.getByRole("heading", { name: "监控总览" })).toBeVisible();
  await page.goto("/admin/login?next=/admin");
  await expect(page.getByRole("heading", { name: "进入 TokHub 平台管理" })).toBeVisible();
  await adminLogin(page, "/admin");
  await expect(page.getByRole("heading", { name: "平台总览" })).toBeVisible();
});
