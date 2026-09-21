import { test, expect, type Page } from "@playwright/test";
import { readFileSync } from "node:fs";
const fixture = () => ({
  status: "ready",
  message: "",
  updatedAt: "2026-09-18T12:30:00Z",
  result: {
    version: "v1.4-close-5pp-cash0-cost10-v1",
    start: "20240102",
    end: "20260917",
    costBps: 10,
    codes: ["510880.SH", "518880.SH", "159915.SZ", "513100.SH"],
    names: ["红利 ETF", "黄金 ETF", "创业板 ETF", "纳指 ETF"],
    days: [
      ["20240102", 0.999],
      ["20250917", 1.1],
      ["20260916", 1.32],
      ["20260917", 1.21],
    ].map(([date, nav]) => ({
      date,
      nav,
      holding: "510880.SH",
      weight: 0.7,
      cashWeight: 0.3,
      benchmarks: [1, 1, 1, 1],
      cost: 0,
      turnover: 0,
    })),
  },
});
async function reply(page: Page, data: unknown) {
  await page.route("**/api/rotation/backtest", (r) =>
    r.fulfill({ json: data }),
  );
  await page.goto("/strategies/four-etf-rotation");
}
test.beforeEach(async ({ page }) => {
  await page.setExtraHTTPHeaders({
    "Cf-Access-Jwt-Assertion": readFileSync(
      "/tmp/candela-browser-test-token",
      "utf8",
    ),
  });
});
test("date semantics, full-history fee basis, linked metrics and keyboard cursor", async ({
  page,
}) => {
  await reply(page, fixture());
  await expect(page.getByTestId("rotation-gain")).toHaveText("10.00%");
  await expect(page.getByTestId("rotation-maxdd")).toHaveText("-8.33%");
  await expect(page.locator("#data-status")).toContainText("2026-09-17");
  await expect(page.locator("#data-status")).toContainText("2026/09/18 20:30");
  await expect(page.locator("#data-status")).toContainText(
    "标的与收盘权重相同",
  );
  await page.getByRole("radio", { name: "全部", exact: true }).click();
  await expect(page.getByTestId("rotation-gain")).toHaveText("21.00%");
  await expect(page.getByLabel("开始日期")).toHaveValue("2024-01-02");
  const cursor = page.getByLabel("查看交易日（方向键逐日移动）");
  await cursor.focus();
  await page.keyboard.press("ArrowLeft");
  await expect(page.getByTestId("rotation-detail")).toContainText("2026-09-16");
  await expect(page.getByTestId("rotation-detail")).toContainText("1.3200");
  const cb = page.getByRole("checkbox", { name: "黄金 ETF" });
  await cb.focus();
  await page.keyboard.press("Space");
  await expect(cb).toBeChecked();
  await expect(page.getByTestId("rotation-detail")).toContainText(
    "黄金 ETF · 区间收益",
  );
  await page.getByLabel("开始日期").fill("2026-09-17");
  await expect(page.getByTestId("rotation-gain")).toHaveText("—");
  await expect(page.getByTestId("rotation-maxdd")).toHaveText("—");
});
for (const [status, label] of Object.entries({
  pending: "等待更新",
  computing: "回测计算中",
  syncing: "行情同步中",
  failed: "更新失败",
  stale: "数据滞后",
  unexpected: "状态未知",
})) {
  test(`${status} retains historical result and reports state`, async ({
    page,
  }) => {
    await reply(page, {
      ...fixture(),
      status,
      message: "发布服务返回的状态说明",
    });
    await expect(page.locator("#data-status")).toContainText(label);
    await expect(page.getByRole("status")).toContainText("保留的历史结果");
    await expect(
      page.getByRole("img", { name: "策略与 ETF 累计收益图" }),
    ).toBeVisible();
  });
}
test("loading, empty array, null result and initial failure", async ({
  page,
}) => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/api/rotation/backtest", async (r) => {
    await gate;
    await r.fulfill({ json: { ...fixture(), result: null } });
  });
  await page.goto("/strategies/four-etf-rotation");
  await expect(page.getByRole("status")).toContainText("正在读取回测");
  release();
  await expect(
    page.getByRole("heading", { name: "暂无完整回测结果" }),
  ).toBeVisible();
  await page.unroute("**/api/rotation/backtest");
  await reply(page, {
    ...fixture(),
    result: { ...fixture().result, days: [] },
  });
  await expect(
    page.getByRole("heading", { name: "暂无完整回测结果" }),
  ).toBeVisible();
  await page.unroute("**/api/rotation/backtest");
  await page.route("**/api/rotation/backtest", (r) =>
    r.fulfill({ status: 502, body: "down" }),
  );
  await page.reload();
  await expect(page.getByRole("alert")).toContainText("暂时无法读取回测");
  await expect(page.getByRole("button", { name: "刷新回测" })).toBeEnabled();
});
test("missing nav, weights, benchmarks and timestamp stay unknown", async ({
  page,
}) => {
  const data = fixture();
  Object.assign(data, { updatedAt: null });
  Object.assign(data.result.days[2], { nav: null });
  Object.assign(data.result.days[3], {
    weight: null,
    cashWeight: null,
    holding: null,
    benchmarks: [1, null, 1, 1],
  });
  await reply(page, data);
  await expect(page.getByRole("alert")).toContainText("部分历史字段缺失");
  await expect(page.getByTestId("rotation-maxdd")).toHaveText("—");
  await expect(page.locator("#data-status")).toContainText("无法比较");
  await expect(page.locator("#data-status")).not.toContainText(
    "标的与收盘权重相同",
  );
  await expect(page.locator("#data-status")).toContainText("发布更新时间：—");
  await expect(page.getByTestId("rotation-detail")).toContainText("现金 —");
  await page.getByRole("checkbox", { name: "黄金 ETF" }).check();
  await expect(
    page
      .getByTestId("rotation-detail")
      .locator("div")
      .filter({ has: page.locator("dt", { hasText: "黄金 ETF · 区间收益" }) })
      .last(),
  ).toContainText("—");
  await expect(page.locator("body")).not.toContainText("NaN");
});
test("unknown version and absent fee do not assert current strategy parameters", async ({
  page,
}) => {
  const data = fixture();
  Object.assign(data.result, { version: "future-version", costBps: null });
  await reply(page, data);
  await page.getByRole("button", { name: "阅读策略与回测口径" }).click();
  await expect(page.getByRole("dialog")).toContainText("尚未在本页核对");
  await expect(page.getByRole("dialog")).toContainText("—（未提供）");
  await expect(page.getByRole("dialog")).not.toContainText("0.005");
});
for (const width of [1440, 820, 390, 320])
  test(`layout, fonts, dialog and focus at ${width}px`, async ({ page }) => {
    const errors: string[] = [];
    page.on("pageerror", (e) => errors.push(e.message));
    page.on("console", (e) => {
      if (e.type() === "error") errors.push(e.text());
    });
    await page.setViewportSize({ width, height: 900 });
    await reply(page, fixture());
    await page.evaluate(() => document.fonts.ready);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    const title = await page
      .getByRole("heading", { name: "四标的轮动", exact: true })
      .evaluate((el) => ({
        family: getComputedStyle(el).fontFamily,
        weight: getComputedStyle(el).fontWeight,
      }));
    expect(title.family).toContain("Noto Serif SC");
    expect(title.weight).toBe("300");
    expect(
      await page.evaluate(() =>
        document.fonts.check('300 26px "Noto Serif SC Variable"', "四标的轮动"),
      ),
    ).toBe(true);
    expect(
      await page.evaluate(() =>
        document.fonts.check('400 36px "Source Serif 4"', "1234567890"),
      ),
    ).toBe(true);
    const trigger = page.getByRole("button", { name: "阅读策略与回测口径" });
    await trigger.click();
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    expect(
      await dialog.evaluate((el) => getComputedStyle(el).borderRadius),
    ).toBe("12px");
    const box = await dialog.boundingBox();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(width);
    await page.keyboard.press("Tab");
    expect(
      await dialog.evaluate((el) => el.contains(document.activeElement)),
    ).toBe(true);
    await page.keyboard.press("Escape");
    await expect(trigger).toBeFocused();
    expect(errors).toEqual([]);
    await page.screenshot({
      path: `/tmp/candela-rotation-fixture-${width}.png`,
      fullPage: true,
    });
  });
