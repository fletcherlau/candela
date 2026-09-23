import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./tests",
  testIgnore: [
    "etf-sync.spec.ts",
    "rotation-recovery.spec.ts",
    "rotation-corrections.spec.ts",
    "sync.spec.ts",
    "design.spec.ts",
    "rotation-daily.spec.ts",
    "rotation-range.spec.ts",
    "rotation-capture.spec.ts",
    "rotation-reference.spec.ts",
    "rotation-history.spec.ts",
  ],
  use: { baseURL: "http://127.0.0.1:18081", headless: true },
  webServer: [
    {
      command:
        "CANDELA_RANGE_BROWSER_TEST=1 go -C ../../syncer test ./internal/rotation -run '^TestRangeBrowserHarness$' -count=1 -timeout=10m",
      url: "http://127.0.0.1:18085",
      timeout: 90000,
    },
    {
      command:
        "CANDELA_BROWSER_TEST=1 go -C .. test ./internal/server -run TestBrowserHarness -count=1 -timeout=10m",
      url: "http://127.0.0.1:18081",
      reuseExistingServer: false,
      timeout: 90000,
    },
  ],
});
