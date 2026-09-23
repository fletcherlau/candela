import { test, expect, type Page } from "@playwright/test";
import { readFileSync } from "node:fs";
test.describe.configure({ mode: "serial" });
const source = "http://127.0.0.1:18091/test-source";
test.beforeEach(async ({ context }) => {
  await context.setExtraHTTPHeaders({
    "Cf-Access-Jwt-Assertion": readFileSync(
      "/tmp/candela-browser-test-token",
      "utf8",
    ),
  });
  await context.addInitScript(() =>
    document.addEventListener("DOMContentLoaded", () => {
      const note = document.createElement("p");
      note.textContent = "验收预览 · 隔离测试行情，非真实市场数据";
      document.body.prepend(note);
    }),
  );
});
async function submitHistory(
  page: Page,
  codes: string,
  start: string,
  end: string,
) {
  await page.goto("/admin/data");
  const panel = page.getByRole("region", { name: "ETF 批量同步" });
  await panel.getByRole("radio", { name: "历史重同步", exact: true }).click();
  await panel.getByLabel("开始日期", { exact: true }).fill(start);
  await panel.getByLabel("结束日期", { exact: true }).fill(end);
  await panel.getByLabel("指定 ETF 代码", { exact: true }).fill(codes);
  const accepted = page.waitForResponse(
    (r) =>
      r.url().endsWith("/api/etf-syncs") && r.request().method() === "POST",
  );
  await panel
    .getByRole("button", { name: "重同步指定 ETF", exact: true })
    .click();
  const response = await accepted;
  expect(response.status()).toBe(202);
  const id = (await response.json()).batch.id as string;
  await expect(page).toHaveURL(new RegExp(`etfBatch=${id}`));
  return id;
}
const dailyRead = (page: Page, date = "20250107") =>
  page.evaluate(
    async (d) => (await fetch(`/api/rotation/daily?tradeDate=${d}`)).json(),
    date,
  );
const rangeRead = (page: Page) =>
  page.evaluate(async () =>
    (
      await fetch("/api/rotation/backtest/range?start=20250106&end=20250110")
    ).json(),
  );
test("real source corrections update selected history and continuous range while preserving frozen reference", async ({
  page,
  context,
  request,
}) => {
  await page.goto("/strategies/four-etf-rotation?tradeDate=20250107");
  const daily = page.getByRole("region", { name: "每日数据" });
  await expect(daily).toContainText("同日 14:45 参考与收盘数据均已发布");
  await expect(page.getByLabel("开始日期")).toHaveValue("2024-01-10");
  await page.getByLabel("开始日期").fill("2025-01-06");
  await expect(page.getByTestId("rotation-gain")).toHaveText("0.00%");
  const before = await dailyRead(page);
  await request.post(source + "?mode=correct");
  const admin = await context.newPage();
  await submitHistory(admin, "510880.SH,513100.SH", "2025-01-07", "2025-01-07");
  await expect(admin.getByLabel("ETF 批次详情")).toContainText("成功 2 / 2", {
    timeout: 15000,
  });
  await expect
    .poll(
      async () => {
        const v = await dailyRead(page);
        return (
          v.closeState.status === "ready" &&
          v.close.cards[0].price === 110 &&
          v.close.cards[3].rank === 1
        );
      },
      { timeout: 20000 },
    )
    .toBe(true);
  await page.getByRole("button", { name: "重新读取每日数据" }).click();
  await expect(daily).toContainText("+1000 bps");
  await expect
    .poll(async () => (await rangeRead(page)).status, { timeout: 20000 })
    .toBe("ready");
  await page.getByRole("button", { name: "刷新回测" }).click();
  const after = await dailyRead(page);
  expect(after.reference).toEqual(before.reference);
  expect(after.priceSlippage[0].bps).toBe(1000);
  expect(after.priceSlippage[3].bps).toBe(0);
  expect(after.close.cards[3].price).toBe(103);
  const range = await rangeRead(page);
  expect(range.result.end).toBe("20250110");
  expect(range.result.costBps).toBe(10);
  expect(
    range.result.days.find((d: { date: string }) => d.date === "20250107")
      .benchmarks[3],
  ).toBe(2);
  await expect(page.getByTestId("rotation-gain")).toHaveText(
    `${(range.range.gain * 100).toFixed(2)}%`,
  );
  await expect(page.getByLabel("开始日期")).toHaveValue("2025-01-06");
  await expect(page.getByLabel("结束日期")).toHaveValue("2025-01-10");
  expect(new URL(page.url()).searchParams.get("tradeDate")).toBe("20250107");
  const later = await dailyRead(page, "20250108");
  expect(later.close.cards[0].volatility).toBeGreaterThan(0);
  expect(later.reference).toBeNull();
  expect((await (await request.get(source)).json()).referenceCalls).toBe(4);
  await admin.close();
});
test("failed factor retains complete result and weekend historical scope recovers in management", async ({
  page,
  request,
}) => {
  await page.goto("/strategies/four-etf-rotation?tradeDate=20250107");
  const before = await dailyRead(page);
  await request.post(source + "?mode=fail");
  await submitHistory(page, "510880.SH", "2025-01-02", "2025-01-05");
  await expect(page.getByLabel("ETF 批次详情")).toContainText("失败 1", {
    timeout: 15000,
  });
  const failed = await dailyRead(page);
  expect(failed.closeState.status).toBe("failed");
  expect(failed.close).toEqual(before.close);
  const recovery = page.getByRole("region", { name: "历史结果恢复" });
  await expect(recovery).toContainText("原历史范围 2025-01-02 至 2025-01-05");
  await request.post(source + "?mode=repair");
  await recovery.getByRole("button", { name: "恢复历史同步与发布" }).click();
  await expect(recovery).toContainText("受影响留存日与连续回测已发布", {
    timeout: 20000,
  });
  await expect(
    recovery.getByRole("button", { name: "恢复历史同步与发布" }),
  ).toHaveCount(0);
  await expect(
    recovery.getByRole("link", { name: "查看复盘与回测" }),
  ).toHaveAttribute("href", "/strategies/four-etf-rotation?tradeDate=20250106");
  await recovery.getByRole("link", { name: "查看复盘与回测" }).click();
  await expect(
    page
      .getByRole("region", { name: "每日数据" })
      .locator(".rotation-daily-context"),
  ).toContainText("2025-01-06");
  expect((await (await request.get(source)).json()).referenceCalls).toBe(4);
});
test("cancelled historical source remains incomplete without replacing displayed data", async ({
  page,
  context,
  request,
}) => {
  await page.goto("/strategies/four-etf-rotation?tradeDate=20250107");
  const before = await dailyRead(page);
  await request.post(source + "?mode=hold");
  const admin = await context.newPage();
  const cancelledID = await submitHistory(
    admin,
    "510880.SH",
    "2025-01-07",
    "2025-01-07",
  );
  const detail = admin.getByLabel("ETF 批次详情");
  await expect(
    detail.getByRole("button", { name: "取消未完成对象" }),
  ).toBeVisible();
  await expect
    .poll(async () => (await dailyRead(page)).closeState.status)
    .toBe("syncing");
  await page.getByRole("button", { name: "重新读取每日数据" }).click();
  const daily = page.getByRole("region", { name: "每日数据" });
  await expect(daily).toContainText("原始数据同步中");
  await detail.getByRole("button", { name: "取消未完成对象" }).click();
  await expect.poll(async () => page.evaluate(async id =>
    (await (await fetch(`/api/etf-syncs/${id}`)).json()).batch.state,
    cancelledID), { timeout: 20000 }).toBe("cancelled");
  await expect
    .poll(async () => (await dailyRead(page)).closeState.status)
    .toBe("failed");
  expect((await dailyRead(page)).close).toEqual(before.close);
  await page.getByRole("button", { name: "重新读取每日数据" }).click();
  await expect(daily).toContainText("更新未完成");
  await request.post(source + "?mode=release");
  await submitHistory(admin, "510880.SH", "2025-01-07", "2025-01-07");
  await expect
    .poll(async () => (await dailyRead(page)).closeState.status, {
      timeout: 20000,
    })
    .toBe("ready");
  const cancelled = await page.evaluate(
    async (id) => (await fetch(`/api/etf-syncs/${id}`)).json(),
    cancelledID,
  );
  expect(cancelled.batch.state).toBe("cancelled");
  await expect.poll(async () => (await rangeRead(page)).status, { timeout: 20000 }).toBe("ready");
  expect((await dailyRead(page)).reference).toEqual(before.reference);
  await admin.close();
});
for (const width of [1440, 820, 390, 320]) {
  test(`corrected data and recovery fit ${width}px with keyboard access`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    await page.goto("/strategies/four-etf-rotation?tradeDate=20250107");
    const daily = page.getByRole("region", { name: "每日数据" });
    await expect(daily).toContainText("同日 14:45 参考与收盘数据均已发布");
    const refresh = daily.getByRole("button", { name: "重新读取每日数据" });
    await expect(refresh).toHaveAttribute("aria-disabled", "false");
    await refresh.focus();
    await refresh.press("Enter");
    await expect(refresh).toHaveAttribute("aria-disabled", "false");
    await expect(refresh).toBeFocused();
    await page.evaluate(() => document.fonts.ready);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    if (width === 1440 || width === 390) {
      await page.screenshot({
        path: `/tmp/candela-corrections-${width}.png`,
        fullPage: true,
      });
      await daily.screenshot({
        path: `/tmp/candela-corrections-daily-${width}.png`,
      });
    }
    await page.goto("/admin/data");
    const recovery = page.getByRole("region", { name: "历史结果恢复" });
    await expect(recovery).toContainText("受影响留存日与连续回测已发布");
    const read = recovery.getByRole("button", { name: "重新读取恢复记录" });
    await expect(read).toHaveAttribute("aria-disabled", "false");
    await read.focus();
    await read.press("Enter");
    await expect(read).toHaveAttribute("aria-disabled", "false");
    await expect(read).toBeFocused();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    if (width === 390)
      await recovery.screenshot({
        path: "/tmp/candela-corrections-recovery-390.png",
      });
  });
}
test("read errors preserve complete corrected data and missing references stay missing", async ({
  page,
}) => {
  await page.goto("/strategies/four-etf-rotation?tradeDate=20250107");
  const daily = page.getByRole("region", { name: "每日数据" });
  await expect(daily).toContainText("+1000 bps");
  await page.route("**/api/rotation/daily?tradeDate=20250107", (route) =>
    route.fulfill({ status: 502 }),
  );
  await daily.getByRole("button", { name: "重新读取每日数据" }).click();
  await expect(daily.getByRole("alert")).toContainText(
    "当前保留 2025-01-07 已发布结果",
  );
  await expect(daily).toContainText("+1000 bps");
  await page.unroute("**/api/rotation/daily?tradeDate=20250107");
  await page.goto("/strategies/four-etf-rotation?tradeDate=20250108");
  await expect(daily).toContainText("14:45 参考缺失");
  await expect(daily.locator(".rotation-daily-context")).toContainText(
    "2025-01-08",
  );
});

test("slow reads expose loading and timeout without changing selected date", async ({
  page,
}) => {
  await page.clock.install();
  let release!: () => void;
  const held = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route(
    "**/api/rotation/daily?tradeDate=20250107",
    async (route) => {
      await held;
      await route.abort().catch(() => {});
    },
  );
  await page.goto("/strategies/four-etf-rotation?tradeDate=20250107");
  const daily = page.getByRole("region", { name: "每日数据" });
  await expect(daily.getByLabel("正在读取每日数据")).toBeVisible();
  await page.clock.fastForward(11000);
  await expect(daily.getByRole("alert")).toContainText("读取超时");
  await expect(daily.locator(".rotation-daily-context")).toContainText(
    "2025-01-07",
  );
  release();
  await page.unroute("**/api/rotation/daily?tradeDate=20250107");
  await daily.getByRole("button", { name: "重新读取每日数据" }).click();
  await expect(daily).toContainText("+1000 bps");
});
