import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./tests",
  testMatch: "rotation-capture.spec.ts",
  workers: 1,
  use: { baseURL: "http://127.0.0.1:18081", headless: true },
  webServer: [
    {
      command:
        "CANDELA_CAPTURE_BROWSER_TEST=1 go -C ../../syncer test ./internal/rotation -run '^TestCaptureBrowserHarness$' -count=1 -timeout=20m",
      url: "http://127.0.0.1:18086/test-source-count",
      timeout: 90000,
    },
    {
      command:
        "CANDELA_BROWSER_TEST=1 CANDELA_CAPTURE_BROWSER_TEST=1 go -C .. test ./internal/server -run '^TestBrowserHarness$' -count=1 -timeout=20m",
      url: "http://127.0.0.1:18081",
      timeout: 90000,
    },
  ],
});
