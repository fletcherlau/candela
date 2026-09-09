import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./tests",
  use: { baseURL: "http://127.0.0.1:18081", headless: true },
  webServer: {
    command:
      "CANDELA_BROWSER_TEST=1 go -C .. test ./internal/server -run TestBrowserHarness -count=1 -timeout=10m",
    url: "http://127.0.0.1:18081",
    reuseExistingServer: false,
    timeout: 90000,
  },
});
