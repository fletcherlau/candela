import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./tests",
  testMatch: "design.spec.ts",
  use: { baseURL: "http://127.0.0.1:4174", headless: true, launchOptions: { chromiumSandbox: false } },
  webServer: {
    command: "npm run dev -- --port 4174 --strictPort",
    url: "http://127.0.0.1:4174/design-preview",
    reuseExistingServer: !process.env.CI,
  },
});
