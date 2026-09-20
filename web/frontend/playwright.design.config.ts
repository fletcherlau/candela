import { defineConfig } from "@playwright/test";

const port = process.env.DESIGN_TEST_PORT || "4174";
export default defineConfig({
  testDir: "./tests",
  testMatch: "design.spec.ts",
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    headless: true,
    launchOptions: { chromiumSandbox: false },
  },
  webServer: {
    command: `npm run dev -- --port ${port} --strictPort`,
    url: `http://127.0.0.1:${port}/design-preview`,
    reuseExistingServer: !process.env.CI,
  },
});
