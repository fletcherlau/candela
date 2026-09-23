import {
  test,
  expect,
  type APIRequestContext,
  type Page,
} from "@playwright/test";
import { readFileSync } from "node:fs";
const backend = "http://127.0.0.1:18087";
async function phase(request: APIRequestContext, name: string) {
  expect(
    (await request.post(`${backend}/test-phase?phase=${name}`)).status(),
  ).toBe(204);
}
async function reference(request: APIRequestContext) {
  await phase(request, "reference");
  expect(
    (
      await request.post(`${backend}/api/v1/rotation/reference-captures`, {
        headers: { "X-Api-Key": "fixture-only" },
        data: { tradeDate: "20250103" },
      })
    ).status(),
  ).toBe(202);
  await expect
    .poll(async () => {
      const r = await request.get(`${backend}/api/v1/rotation/daily`, {
        headers: { "X-Api-Key": "fixture-only" },
      });
      return (await r.json()).reference?.tradeDate;
    })
    .toBe("20250103");
}
async function refresh(page: Page) {
  const button = page.getByRole("button", { name: "重新读取每日数据" });
  await expect(button).toHaveAttribute("aria-disabled", "false");
  await button.click();
  await expect(button).toHaveAttribute("aria-disabled", "false");
}
test.beforeEach(async ({ page, request }) => {
  await phase(request, "before");
  await page.setExtraHTTPHeaders({
    "Cf-Access-Jwt-Assertion": readFileSync(
      "/tmp/candela-browser-test-token",
      "utf8",
    ),
  });
});
test("real publications move from previous session through reference and partial close into paired metrics", async ({
  page,
  request,
}) => {
  await page.goto("/strategies/four-etf-rotation");
  const daily = page.getByRole("region", { name: "每日数据" });
  await expect(daily).toContainText("2025-01-02");
  await expect(daily).toContainText("尚未到今日 14:45");
  await reference(request);
  await refresh(page);
  await expect(daily).toContainText("2025-01-03");
  await expect(
    daily.getByRole("table").getByRole("cell", { name: "14:45", exact: true }),
  ).toHaveCount(4);
  await phase(request, "partial");
  await refresh(page);
  await expect(daily).toContainText("3/4");
  await expect(daily).toContainText("513100.SH");
  await phase(request, "close");
  await refresh(page);
  await expect(daily).toContainText("+30 bps");
  await expect(daily).toContainText("−12.5 bps");
  await expect(daily).toContainText("0 bps");
  await expect(daily).toContainText("不是实际成交滑点");
  await expect(daily).not.toContainText(/买入|卖出|建议仓位|换仓/);
  await expect(page.getByRole("heading", { name: "收益与风险" })).toBeVisible();
});

for (const width of [1440, 820, 390, 320]) {
  test(`paired fields and bps remain readable and keyboard usable at ${width}px`, async ({
    page,
    request,
  }) => {
    await reference(request);
    await phase(request, "close");
    await page.setViewportSize({ width, height: 1000 });
    await page.goto("/strategies/four-etf-rotation");
    const daily = page.getByRole("region", { name: "每日数据" });
    await expect(daily).toContainText("+30 bps");
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    if (width <= 900) {
      const cards = daily.locator("[data-daily-code]");
      await expect(cards).toHaveCount(4);
      for (const card of await cards.all()) {
        for (const field of ["指标", "14:45", "收盘"])
          await expect(
            card.getByRole("columnheader", { name: field, exact: true }),
          ).toBeVisible();
        for (const field of [
          "价格",
          "20 日动量",
          "排名",
          "年化波动率",
          "波动率分位",
          "计算仓位",
        ])
          await expect(
            card.getByRole("cell", { name: field, exact: true }),
          ).toBeVisible();
        await expect(card).toContainText("14:45:02");
        await expect(card).toContainText("14:45:03");
      }
    } else {
      await expect(daily.getByRole("table").getByRole("row")).toHaveCount(9);
    }
    const button = daily.getByRole("button", { name: "重新读取每日数据" });
    await button.focus();
    await button.press("Enter");
    await expect(button).toHaveAttribute("aria-disabled", "false");
    await expect(button).toBeFocused();
    await expect(daily).not.toContainText(/\+0 bps|30\.0 bps|−12\.50 bps/);
    await daily.screenshot({ path: `/tmp/candela-reference-${width}.png` });
  });
}

test("missed original reference falls back then shows close with no fabricated price difference", async ({
  page,
  request,
}) => {
  await phase(request, "missed");
  const before = await (
    await request.get(`${backend}/test-source-count`)
  ).json();
  expect(
    (
      await request.post(`${backend}/api/v1/rotation/reference-captures`, {
        headers: { "X-Api-Key": "fixture-only" },
        data: { tradeDate: "20250103" },
      })
    ).status(),
  ).toBe(202);
  await expect
    .poll(async () => {
      const r = await request.get(
        `${backend}/api/v1/rotation/reference-captures/20250103`,
        { headers: { "X-Api-Key": "fixture-only" } },
      );
      return (await r.json()).run.state;
    })
    .toBe("missing");
  await page.goto("/strategies/four-etf-rotation");
  const daily = page.getByRole("region", { name: "每日数据" });
  await expect(daily).toContainText("2025-01-02");
  await expect(daily).toContainText("今日 14:45 参考缺失");
  await expect(daily).toContainText("无法补取");
  await phase(request, "close");
  await refresh(page);
  await expect(daily).toContainText("2025-01-03 · 收盘");
  await expect(daily).toContainText("100.300");
  await expect(daily).not.toContainText(/[+−]\d+(\.\d+)? bps|0 bps/);
  const after = await (
    await request.get(`${backend}/test-source-count`)
  ).json();
  expect(after.calls).toBe(before.calls);
  await page.setViewportSize({ width: 390, height: 1000 });
  await daily.screenshot({ path: "/tmp/candela-reference-missing-390.png" });
});

test("read failures and actual expired login retain both dated publications without affecting backtest", async ({
  page,
  request,
}) => {
  await reference(request);
  await phase(request, "close");
  await page.goto("/strategies/four-etf-rotation");
  const daily = page.getByRole("region", { name: "每日数据" });
  await expect(daily).toContainText("+30 bps");
  await page.route("**/api/rotation/daily", (route) =>
    route.fulfill({ status: 502 }),
  );
  await refresh(page);
  await expect(daily.getByRole("alert")).toContainText(
    "当前保留 2025-01-03 已发布结果",
  );
  await expect(daily).toContainText("+30 bps");
  await expect(
    page.getByRole("img", { name: "策略与 ETF 累计收益图" }),
  ).toBeVisible();
  await page.unroute("**/api/rotation/daily");
  await page.setExtraHTTPHeaders({
    "Cf-Access-Jwt-Assertion": "expired-fixture",
  });
  await refresh(page);
  await expect(daily.getByRole("alert")).toContainText("登录已过期");
  await page.setExtraHTTPHeaders({
    "Cf-Access-Jwt-Assertion": readFileSync(
      "/tmp/candela-browser-test-token",
      "utf8",
    ),
  });
  await refresh(page);
  await expect(daily.getByRole("alert")).toHaveCount(0);
  const before = await (
    await request.get(`${backend}/test-source-count`)
  ).json();
  await refresh(page);
  await page.evaluate(() =>
    document.dispatchEvent(new Event("visibilitychange")),
  );
  await expect(daily).toContainText("+30 bps");
  const after = await (
    await request.get(`${backend}/test-source-count`)
  ).json();
  expect(after.calls).toBe(before.calls);
});

test("late read and mismatched publication dates cannot replace the current date", async ({
  page,
  request,
}) => {
  await reference(request);
  await page.goto("/strategies/four-etf-rotation");
  const daily = page.getByRole("region", { name: "每日数据" });
  await expect(daily).toContainText("2025-01-03 · 14:45 固定参考");
  let release!: () => void;
  let started!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  const entered = new Promise<void>((resolve) => {
    started = resolve;
  });
  let calls = 0;
  await page.route("**/api/rotation/daily", async (route) => {
    const response = await route.fetch();
    if (++calls === 1) {
      started();
      await gate;
    }
    await route.fulfill({ response }).catch(() => {});
  });
  await daily.getByRole("button", { name: "重新读取每日数据" }).click();
  await entered;
  await phase(request, "close");
  await page.evaluate(() =>
    document.dispatchEvent(new Event("visibilitychange")),
  );
  await expect(daily).toContainText("+30 bps");
  release();
  await page.unroute("**/api/rotation/daily");
  await expect(daily).toContainText("+30 bps");
  await page.route("**/api/rotation/daily", async (route) => {
    const response = await route.fetch();
    const data = await response.json();
    data.reference.tradeDate = "20250102";
    await route.fulfill({ json: data });
  });
  await refresh(page);
  await expect(daily.getByRole("alert")).toContainText("每日数据返回不完整");
  await expect(daily).toContainText("+30 bps");
});

test("management reports reference publication instead of waiting for calculation", async ({
  page,
  request,
}) => {
  await reference(request);
  await page.goto("/admin/data");
  const panel = page.getByRole("region", { name: "14:45 参考采集" });
  await expect(panel).toContainText("原始数据采集完成／参考已发布");
  await expect(panel).not.toContainText("参考待计算");
});
