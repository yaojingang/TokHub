import { expect, test } from "@playwright/test";
import { adminLogin } from "./helpers";

test("admin web menu editors keep focus while typing editable labels", async ({ page }) => {
  await adminLogin(page, "/admin/web");

  const topNavName = page
    .locator(".card.set-card")
    .filter({ hasText: "顶部导航菜单" })
    .locator(".menu-item")
    .first()
    .locator("input.input")
    .first();

  await topNavName.click();
  await topNavName.press(process.platform === "darwin" ? "Meta+A" : "Control+A");
  await topNavName.type("Alpha Menu");
  await expect(topNavName).toHaveValue("Alpha Menu");
  await expect(topNavName).toBeFocused();

  const footerName = page
    .locator(".card.set-card")
    .filter({ hasText: "页脚链接" })
    .locator(".menu-item")
    .first()
    .locator("input.input")
    .first();

  await footerName.click();
  await footerName.press(process.platform === "darwin" ? "Meta+A" : "Control+A");
  await footerName.type("Footer Menu");
  await expect(footerName).toHaveValue("Footer Menu");
  await expect(footerName).toBeFocused();
});
