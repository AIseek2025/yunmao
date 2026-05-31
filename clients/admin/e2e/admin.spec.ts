import { expect, test } from "@playwright/test";

test("Login page renders and accepts password", async ({ page }) => {
  await page.goto("/login");
  await expect(page.getByRole("heading", { name: "yunmao 运营后台登录" })).toBeVisible();
  const input = page.locator('input[type="password"]');
  await expect(input).toBeVisible();
  await input.fill("test-admin-password");
  await expect(page.getByRole("button", { name: "登录" })).toBeEnabled();
});

test("Unauthenticated visit redirects to login", async ({ page }) => {
  await page.goto("/feature-flags");
  await page.waitForURL(/\/login/, { timeout: 5000 });
  await expect(page.getByRole("heading", { name: "yunmao 运营后台登录" })).toBeVisible();
});

test("Feature flags page renders after login", async ({ page }) => {
  await page.goto("/login");
  await page.evaluate(() => window.localStorage.setItem("yunmao.admin.token", "test-jwt-token"));
  await page.goto("/feature-flags");
  await expect(page.getByRole("heading", { name: "Feature Flags" })).toBeVisible();
});

test("WebRTC gray simulator computes percent", async ({ page }) => {
  await page.goto("/login");
  await page.evaluate(() => window.localStorage.setItem("yunmao.admin.token", "test-jwt-token"));
  await page.goto("/webrtc/gray-sim");
  await expect(page.getByRole("heading", { name: "WebRTC 灰度模拟器" })).toBeVisible();
  await expect(page.locator("text=/命中：/")).toBeVisible();
});

test("Rooms page renders after login", async ({ page }) => {
  await page.goto("/login");
  await page.evaluate(() => window.localStorage.setItem("yunmao.admin.token", "test-jwt-token"));
  await page.goto("/rooms");
  await expect(page.getByRole("heading", { name: "房间管理" })).toBeVisible();
});

test("Wallet page renders after login", async ({ page }) => {
  await page.goto("/login");
  await page.evaluate(() => window.localStorage.setItem("yunmao.admin.token", "test-jwt-token"));
  await page.goto("/wallet");
  await expect(page.getByRole("heading", { name: "钱包流水" })).toBeVisible();
});
