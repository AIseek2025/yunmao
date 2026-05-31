import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  reporter: [["list"]],
  webServer: {
    command: "pnpm dev",
    port: 3100,
    reuseExistingServer: !process.env.CI,
  },
  use: { baseURL: "http://localhost:3100" },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
