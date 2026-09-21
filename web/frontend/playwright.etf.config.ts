import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./tests",
  testMatch: "etf-sync.spec.ts",
  workers: 1,
  use: { baseURL: "http://127.0.0.1:18081", headless: true },
  webServer: [
    {
      command:
        "CANDELA_ETF_BROWSER_TEST=1 go -C ../../syncer test ./internal/syncrun -run '^TestETFBrowserHarness$' -count=1 -timeout=20m",
      url: "http://127.0.0.1:18089/fixture",
      timeout: 90000,
    },
    {
      command:
        "CANDELA_BROWSER_TEST=1 CANDELA_ETF_BROWSER_TEST=1 go -C .. test ./internal/server -run '^TestBrowserHarness$' -count=1 -timeout=20m",
      url: "http://127.0.0.1:18081",
      timeout: 90000,
    },
  ],
});
