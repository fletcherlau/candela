import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./tests",
  testMatch: "rotation-daily.spec.ts",
  workers: 1,
  use: { baseURL: "http://127.0.0.1:18081", headless: true },
  webServer: [
    {
      command:
        "CANDELA_DAILY_BROWSER_TEST=1 go -C ../../syncer test ./internal/rotation -run '^TestDailyBrowserHarness$' -count=1 -timeout=10m",
      url: "http://127.0.0.1:18084",
      timeout: 90000,
    },
    {
      command:
        "CANDELA_BROWSER_TEST=1 CANDELA_DAILY_BROWSER_TEST=1 go -C .. test ./internal/server -run '^TestBrowserHarness$' -count=1 -timeout=10m",
      url: "http://127.0.0.1:18081",
      timeout: 90000,
    },
  ],
});
