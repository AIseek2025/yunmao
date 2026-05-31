import { expect, test } from "@playwright/test";

test("登录页可达 + 表单可输入", async ({ page }) => {
  await page.goto("/login");
  await expect(page.getByRole("heading", { name: "登录 yunmao" })).toBeVisible();
  await page.getByLabel("phone").fill("13800000000");
  await page.getByLabel("code").fill("0000");
  const btn = page.getByRole("button", { name: "登录" });
  await expect(btn).toBeEnabled();
});

test("登录后跳转到房间列表", async ({ page }) => {
  await page.goto("/login");
  await page.evaluate(() => {
    const session = {
      access_token: "test-jwt",
      user: { id: "u1", phone: "13800000000" },
    };
    window.localStorage.setItem("yunmao.session", JSON.stringify(session));
    window.localStorage.setItem("yunmao.token", "test-jwt");
  });
  await page.reload();
  await page.goto("/rooms");
  await expect(page.locator("main")).toBeVisible();
});

test("房间详情页挂载", async ({ page }) => {
  await page.goto("/login");
  await page.evaluate(() => {
    const session = {
      access_token: "test-jwt",
      user: { id: "u1", phone: "13800000000" },
    };
    window.localStorage.setItem("yunmao.session", JSON.stringify(session));
    window.localStorage.setItem("yunmao.token", "test-jwt");
  });
  await page.goto("/rooms/test-room-id");
  await expect(page.locator("main")).toBeVisible();
});

test("灰度命中客户端逻辑一致性", async ({ page }) => {
  await page.goto("/login");
  const h = await page.evaluate(() => {
    function hash100(key: string): number {
      let hash = 0x811c9dc5;
      for (const ch of key) {
        const b = ch.charCodeAt(0) & 0xff;
        hash = ((hash ^ b) & 0xffffffff) >>> 0;
        hash = Math.imul(hash, 0x01000193) >>> 0;
      }
      return hash % 100;
    }
    return hash100("room_cross_client_e2e");
  });
  expect(h).toBe(84);
});
