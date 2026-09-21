import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./tests",
  testMatch: "rotation-corrections.spec.ts",
  workers: 1,
  timeout: 60000,
  use: { baseURL: "http://127.0.0.1:18081", headless: true },
  webServer: [
    {
      command:
        "CANDELA_CORRECTION_BROWSER_TEST=1 go -C ../../syncer test ./internal/rotation -run '^TestCorrectionBrowserHarness$' -count=1 -timeout=20m",
      url: "http://127.0.0.1:18091/test-source",
      timeout: 90000,
    },
    {
      command:
        "CANDELA_BROWSER_TEST=1 CANDELA_CORRECTION_BROWSER_TEST=1 go -C .. test ./internal/server -run '^TestBrowserHarness$' -count=1 -timeout=20m",
      url: "http://127.0.0.1:18081",
      timeout: 90000,
    },
  ],
});
