import { test, expect } from "@playwright/test";
import { readFileSync } from "node:fs";

test.beforeEach(async ({ page }) => {
  await page.setExtraHTTPHeaders({
    "Cf-Access-Jwt-Assertion": readFileSync(
      "/tmp/candela-browser-test-token",
      "utf8",
    ),
  });
});

test("real database publication shows daily six fields independently of backtest", async ({
  page,
}) => {
  await page.goto("/strategies/four-etf-rotation");
  const daily = page.getByRole("region", { name: "每日数据" });
  await expect(daily).toBeVisible();
  await expect(daily).toContainText("2025-01-02");
  const table = daily.getByRole("table");
  for (const field of [
    "收盘价",
    "20 日动量",
    "排名",
    "年化波动率",
    "波动率分位",
    "计算仓位",
  ]) {
    await expect(
      table.getByRole("columnheader", { name: field, exact: true }),
    ).toBeVisible();
  }
  await expect(table.getByRole("row")).toHaveCount(5);
  await expect(table).toContainText("100.000");
  await expect(table).toContainText("50.00%");
  await expect(table).toContainText("100.00%");
  await expect(daily).toContainText("14:45 参考未留存");
  await expect(daily).not.toContainText(/建议|买入|卖出|加仓|减仓/);
  await expect(page.getByRole("heading", { name: "收益与风险" })).toBeVisible();
});

for (const width of [1440, 820, 390, 320]) {
  test(`daily fields remain accessible at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1000 });
    await page.goto("/strategies/four-etf-rotation");
    const daily = page.getByRole("region", { name: "每日数据" });
    await expect(daily).toContainText("100.000");
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    if (width <= 900) {
      const cards = daily.locator('[data-slot="card"]');
      await expect(cards).toHaveCount(4);
      for (const card of await cards.all()) {
        await expect(card).toBeVisible();
        for (const field of [
          "收盘价",
          "20 日动量",
          "排名",
          "年化波动率",
          "波动率分位",
          "计算仓位",
        ])
          await expect(card.getByText(field, { exact: true })).toBeVisible();
      }
    }
    const refresh = daily.getByRole("button", { name: "重新读取每日数据" });
    await refresh.focus();
    await expect(refresh).toBeFocused();
    await refresh.press("Enter");
    await expect(refresh).toBeEnabled();
    await expect(refresh).toBeFocused();
    await page.screenshot({
      path: `/tmp/candela-daily-${width}.png`,
      fullPage: true,
    });
  });
}

test("daily failure keeps its dated publication while backtest remains usable", async ({
  page,
}) => {
  await page.goto("/strategies/four-etf-rotation");
  const daily = page.getByRole("region", { name: "每日数据" });
  await expect(daily).toContainText("100.000");
  await page.route("**/api/rotation/daily", (r) =>
    r.fulfill({ status: 502, body: "unavailable" }),
  );
  await daily.getByRole("button", { name: "重新读取每日数据" }).click();
  await expect(daily.getByRole("alert")).toContainText(
    "当前保留 2025-01-02 已发布结果",
  );
  await expect(daily.getByRole("table")).toContainText("100.000");
  await expect(
    page.getByRole("img", { name: "策略与 ETF 累计收益图" }),
  ).toBeVisible();
  await page.unroute("**/api/rotation/daily");
  await daily.getByRole("button", { name: "重新读取每日数据" }).click();
  await expect(daily.getByRole("alert")).toHaveCount(0);
  await page.route("**/api/rotation/backtest/range**", (r) =>
    r.fulfill({ status: 502, body: "unavailable" }),
  );
  await page.getByRole("button", { name: "刷新回测" }).click();
  await expect(daily.getByRole("table")).toContainText("100.000");
});

test("loading, partial close, unknown fields and expired login are explicit", async ({
  page,
}) => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/api/rotation/daily", async (r) => {
    await gate;
    await r.fulfill({
      json: {
        status: "pending",
        tradeDate: "20250102",
        available: 3,
        message: "收盘数据已到齐 3/4；513100.SH：官方行情未到齐",
        close: null,
      },
    });
  });
  await page.goto("/strategies/four-etf-rotation");
  const daily = page.getByRole("region", { name: "每日数据" });
  await expect(daily.getByLabel("正在读取每日数据")).toBeVisible();
  release();
  await expect(daily).toContainText("3/4");
  await expect(daily).toContainText("513100.SH");
  await expect(daily.getByRole("table")).toHaveCount(0);
  await page.unroute("**/api/rotation/daily");
  const response = await page.request.get("/api/rotation/daily", {
    headers: {
      "Cf-Access-Jwt-Assertion": readFileSync(
        "/tmp/candela-browser-test-token",
        "utf8",
      ),
    },
  });
  const publication = await response.json();
  for (const card of publication.close.cards) {
    card.quantile = null;
    card.weight = null;
    card.reasons = {
      quantile: "所需历史数据不足，无法计算",
      weight: "所需历史数据不足，无法计算",
    };
  }
  await page.route("**/api/rotation/daily", (r) =>
    r.fulfill({ json: publication }),
  );
  await page.setViewportSize({ width: 320, height: 844 });
  await daily.getByRole("button", { name: "重新读取每日数据" }).click();
  await expect(daily.locator('[data-slot="card"]').first()).toContainText(
    "所需历史数据不足",
  );
  await expect(daily.locator('[data-slot="card"]').first()).not.toContainText(
    "100.00%",
  );
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: "/tmp/candela-daily-missing-320.png",
    fullPage: true,
  });
  await page.unroute("**/api/rotation/daily");
  await page.route("**/api/rotation/daily", (r) => r.fulfill({ status: 401 }));
  await daily.getByRole("button", { name: "重新读取每日数据" }).click();
  await expect(daily.getByRole("alert")).toContainText("登录已过期");
});
