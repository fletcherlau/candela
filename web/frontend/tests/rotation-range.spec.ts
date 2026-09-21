import { test, expect } from "@playwright/test";
import { readFileSync } from "node:fs";
test.beforeEach(async ({ page, request }) => {
  await request.post("http://127.0.0.1:18085/fixture", {
    headers: { "X-Api-Key": "fixture-only" },
    data: "null",
  });
  await page.setExtraHTTPHeaders({
    "Cf-Access-Jwt-Assertion": readFileSync(
      "/tmp/candela-browser-test-token",
      "utf8",
    ),
  });
});
test("page reads one-year statistics from real backend and requests new range", async ({
  page,
}) => {
  const first = page.waitForResponse(
    (r) =>
      r.url().includes("/api/rotation/backtest/range") && r.status() === 200,
  );
  await page.goto("/strategies/four-etf-rotation");
  const response = await first;
  const data = await response.json();
  expect(data.range.start).toBe("20250831");
  await expect(page.getByLabel("开始日期")).toHaveValue("2025-08-31");
  await expect(page.getByTestId("rotation-gain")).toHaveText(
    `${(data.range.gain * 100).toFixed(2)}%`,
  );
  const next = page.waitForResponse((r) => r.url().includes("start=20260801"));
  await page.getByLabel("开始日期").fill("2026-08-01");
  await next;
  await expect(page.getByLabel("开始日期")).toHaveValue("2026-08-01");
  await expect(page.getByTestId("rotation-detail")).toContainText(
    "现金 30.00%",
  );
});

test("late response never overwrites the latest selected interval", async ({
  page,
}) => {
  await page.addInitScript(() => {
    const original = window.fetch;
    window.fetch = async (input, init) => {
      if (String(input).includes("start=20260801")) {
        const response = await original(input, { ...init, signal: undefined });
        await new Promise((resolve) => setTimeout(resolve, 700));
        return response;
      }
      return original(input, init);
    };
  });
  await page.goto("/strategies/four-etf-rotation");
  await expect(page.getByLabel("开始日期")).toHaveValue("2025-08-31");
  await page.getByLabel("开始日期").fill("2026-08-01");
  const latest = page.waitForResponse((r) =>
    r.url().includes("start=20260815"),
  );
  await page.getByLabel("开始日期").fill("2026-08-15");
  const result = await (await latest).json();
  await expect(page.getByTestId("rotation-gain")).toHaveText(
    `${(result.range.gain * 100).toFixed(2)}%`,
  );
  await page.waitForTimeout(800);
  await expect(page.getByLabel("开始日期")).toHaveValue("2026-08-15");
  await expect(page.locator(".rotation-metrics")).toContainText("2026-08-15");
});

test("failed range and expired session retain dated results and recover the requested range", async ({
  page,
}) => {
  await page.goto("/strategies/four-etf-rotation");
  await expect(page.getByLabel("开始日期")).toHaveValue("2025-08-31");
  const previous = await page.getByTestId("rotation-gain").textContent();
  await page.route("**/api/rotation/backtest/range?**", (r) =>
    r.fulfill({ status: 502 }),
  );
  await page.getByLabel("开始日期").fill("2026-08-10");
  await expect(page.getByRole("alert")).toContainText(
    "2025-08-31 至 2026-08-31",
  );
  await expect(page.getByTestId("rotation-gain")).toHaveText(previous!);
  await page.unroute("**/api/rotation/backtest/range?**");
  await page.getByRole("button", { name: "刷新回测" }).click();
  await expect(page.locator(".rotation-metrics")).toContainText("2026-08-10");
  await expect(page.getByRole("alert")).toHaveCount(0);
  await page.route("**/api/rotation/backtest/range?**", (r) =>
    r.fulfill({ status: 401 }),
  );
  await page.getByRole("button", { name: "刷新回测" }).click();
  await expect(page.getByRole("alert")).toContainText("登录已过期");
});

test("ten years clamps history, rejects an extra day and keeps the continuous model", async ({
  page,
  request,
}) => {
  const codes = ["510880.SH", "518880.SH", "159915.SZ", "513100.SH"];
  const days = [];
  for (
    let d = new Date("2014-01-02T00:00:00Z"), i = 0;
    d <= new Date("2026-08-31T00:00:00Z");
    d.setUTCDate(d.getUTCDate() + 1), i++
  ) {
    days.push({
      date: d.toISOString().slice(0, 10).replaceAll("-", ""),
      nav: 1 + i * 0.0001,
      holding: codes[0],
      weight: 0.7,
      cashWeight: 0.3,
      cost: i === 0 ? 0.001 : 0,
      turnover: 0,
      benchmarks: [1, 1, 1, 1],
    });
  }
  const saved = await request.post("http://127.0.0.1:18085/fixture", {
    headers: { "X-Api-Key": "fixture-only" },
    data: {
      version: "fixture",
      start: days[0].date,
      end: days.at(-1)!.date,
      codes,
      names: ["红利 ETF", "黄金 ETF", "创业板 ETF", "纳指 ETF"],
      costBps: 10,
      days,
    },
  });
  expect(saved.status()).toBe(204);
  await page.goto("/strategies/four-etf-rotation");
  await expect(page.getByLabel("开始日期")).toHaveValue("2025-08-31");
  await page.getByRole("radio", { name: "近十年", exact: true }).click();
  await expect(page.getByLabel("开始日期")).toHaveValue("2016-08-31");
  await expect(page.getByTestId("rotation-detail")).toContainText(
    "现金 30.00%",
  );
  await page.getByLabel("开始日期").fill("2016-08-30");
  await expect(page.getByRole("alert")).toContainText("超过十年");
  await expect(page.locator(".rotation-metrics")).toContainText("2016-08-31");
  await page.getByLabel("结束日期").fill("2026-09-22");
  await expect(page.getByRole("alert")).toContainText("日期无效");
});

test("publication changes can leave an empty interval, with a way back to available history", async ({
  page,
  request,
}) => {
  await page.goto("/strategies/four-etf-rotation");
  await expect(page.getByLabel("开始日期")).toHaveValue("2025-08-31");
  await page.getByLabel("开始日期").fill("2026-08-01");
  await expect(page.locator(".rotation-metrics")).toContainText("2026-08-01");
  await request.post("http://127.0.0.1:18085/fixture", {
    headers: { "X-Api-Key": "fixture-only" },
    data: {
      version: "fixture",
      start: "20240102",
      end: "20240102",
      codes: ["510880.SH"],
      names: ["红利 ETF"],
      costBps: 10,
      days: [
        {
          date: "20240102",
          nav: 0.999,
          holding: "510880.SH",
          weight: 1,
          cashWeight: 0,
          cost: 0.001,
          turnover: 1,
          benchmarks: [1],
        },
      ],
    },
  });
  await page.getByRole("button", { name: "刷新回测" }).click();
  await expect(
    page.getByRole("heading", { name: "所选区间暂无历史数据" }),
  ).toBeVisible();
  await expect(
    page.getByRole("img", { name: "策略与 ETF 累计收益图" }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "返回近一年" }).click();
  await expect(page.getByLabel("开始日期")).toHaveValue("2024-01-02");
  await expect(page.getByTestId("rotation-gain")).toHaveText("—");
});

for (const width of [1440, 820, 390, 320])
  test(`real range layout and keyboard controls at ${width}px`, async ({
    page,
  }) => {
    const errors: string[] = [];
    page.on("pageerror", (e) => errors.push(e.message));
    page.on("console", (e) => {
      if (e.type() === "error") errors.push(e.text());
    });
    await page.setViewportSize({ width, height: 1000 });
    await page.goto("/strategies/four-etf-rotation");
    await expect(page.getByLabel("开始日期")).toHaveValue("2025-08-31");
    await page.evaluate(() => document.fonts.ready);
    const slider = page.getByLabel("区间起点", { exact: true });
    await slider.focus();
    await slider.press("ArrowRight");
    await expect(page.getByLabel("开始日期")).toHaveValue("2025-09-01");
    await expect(page.locator(".rotation-metrics")).toContainText("2025-09-01");
    await expect(slider).toBeFocused();
    const gold = page.getByRole("checkbox", { name: "黄金 ETF" });
    await gold.focus();
    await gold.press("Space");
    await expect(gold).toBeChecked();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    expect(errors).toEqual([]);
    await page.screenshot({
      path: `/tmp/candela-r02-${width}.png`,
      fullPage: true,
    });
  });
