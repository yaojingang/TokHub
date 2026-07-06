import { expect, test } from "@playwright/test";
import { adminLogin } from "./helpers";

test("phase 3 admin channels render and manual probe keeps status consistent", async ({ page }) => {
  await adminLogin(page, "/admin");
  await page.goto("/admin/channels");
  await expect(page.getByRole("heading", { name: "通道接入" })).toBeVisible();

  const channel = await page.evaluate(async () => {
    const response = await fetch("/api/admin/channels", { credentials: "include" });
    const payload = await response.json();
    const items = payload.items as Array<{ id: string; name: string; publicVisible: boolean; ownerType?: string }>;
    return items.find((item) => item.publicVisible) ?? items[0];
  });
  expect(channel?.id).toBeTruthy();

  const channelCheckbox = page.getByRole("checkbox", { name: `选择 ${channel.name}` });
  await expect(channelCheckbox).toBeVisible();

  const row = channelCheckbox.locator("xpath=ancestor::tr");
  await row.locator("summary").click();
  const probeResponse = page.waitForResponse((response) => response.url().includes(`/api/admin/channels/${channel.id}/probe-now`) && response.request().method() === "POST");
  const probeButton = row.getByRole("button", { name: /立即探测|探测中/ }).first();
  await probeButton.click();
  expect((await probeResponse).ok()).toBeTruthy();

  const adminStatus = await page.evaluate(async (selected: { id: string }) => {
    const response = await fetch("/api/admin/channels", { credentials: "include" });
    const payload = await response.json();
    return payload.items.find((item: { id: string }) => item.id === selected.id)?.status;
  }, channel);
  const publicStatus = await page.evaluate(async (id) => {
    const response = await fetch(`/api/public/channels/${id}`);
    const payload = await response.json();
    return payload.channel.status;
  }, channel.id);
  expect(adminStatus).toBe(publicStatus);
});
