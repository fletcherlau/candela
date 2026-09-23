import { test, expect, type Page } from "@playwright/test";
import { readFileSync } from "node:fs";
const backend = "http://127.0.0.1:18087";
async function choose(page: Page, date: string) {
  await page.getByRole("button", { name: "选择历史日期" }).click();
  await page.getByLabel("指定交易日").fill(date);
  await page.getByRole("button", { name: "查看此日", exact: true }).click();
}
test.beforeEach(async ({ page, request }, info) => {
  expect(
    (await request.post(backend + "/test-phase?phase=before")).status(),
  ).toBe(204);
  expect(
    (await request.post(backend + "/test-phase?phase=reference")).status(),
  ).toBe(204);
  expect(
    (
      await request.post(backend + "/api/v1/rotation/reference-captures", {
        headers: { "X-Api-Key": "fixture-only" },
        data: { tradeDate: "20250103" },
      })
    ).status(),
  ).toBe(202);
  await expect
    .poll(
      async () =>
        (
          await (
            await request.get(backend + "/api/v1/rotation/daily", {
              headers: { "X-Api-Key": "fixture-only" },
            })
          ).json()
        ).reference?.tradeDate,
    )
    .toBe("20250103");
  if (!info.title.includes("receives later close"))
    expect(
      (await request.post(backend + "/test-phase?phase=close")).status(),
    ).toBe(204);
  await page.setExtraHTTPHeaders({
    "Cf-Access-Jwt-Assertion": readFileSync(
      "/tmp/candela-browser-test-token",
      "utf8",
    ),
  });
});
test("history selection preserves missing dates and stays independent from backtest range", async ({
  page,
}) => {
  await page.clock.install();
  await page.goto("/strategies/four-etf-rotation");
  const daily = page.getByRole("region", { name: "每日数据" });
  await expect(daily).toContainText("+30 bps");
  await page.getByLabel("开始日期").fill("2026-08-10");
  await expect(page.locator(".rotation-metrics")).toContainText("2026-08-10");
  await choose(page, "2025-01-02");
  await expect(daily.locator(".rotation-daily-context")).toContainText(
    "2025-01-02",
  );
  await expect(daily).toContainText("14:45 参考未留存");
  await expect(page.getByLabel("开始日期")).toHaveValue("2026-08-10");
  await page.clock.fastForward(31000);
  await page.evaluate(() =>
    document.dispatchEvent(new Event("visibilitychange")),
  );
  await expect(daily.locator(".rotation-daily-context")).toContainText(
    "2025-01-02",
  );
  await choose(page, "2024-12-31");
  await expect(daily.locator(".rotation-daily-context")).toContainText(
    "2024-12-31",
  );
  await expect(daily).toContainText("暂无完整每日结果");
  await page.getByRole("button", { name: "回到最新", exact: true }).click();
  await expect(daily).toContainText("+30 bps");
  await expect(page.getByLabel("开始日期")).toHaveValue("2026-08-10");
});

for (const width of [1440, 820, 390, 320]) {
  test(`archive dialog and historical daily data work at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    await page.goto("/strategies/four-etf-rotation");
    const trigger = page.getByRole("button", { name: "选择历史日期" });
    await trigger.focus();
    await trigger.press("Enter");
    const dialog = page.getByRole("dialog", { name: "每日数据档案" });
    await expect(dialog).toContainText("档案始于 2025-01-02");
    await expect(
      dialog.getByRole("button", {
        name: /2025-01-03.*14:45 已发布.*收盘 已发布/,
      }),
    ).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    expect(
      await dialog.evaluate((el) => el.scrollWidth <= el.clientWidth),
    ).toBe(true);
    await dialog.screenshot({
      path: `/tmp/candela-history-dialog-${width}.png`,
    });
    await dialog.getByRole("button", { name: /2025-01-02/ }).click();
    const daily = page.getByRole("region", { name: "每日数据" });
    await expect(daily.locator(".rotation-daily-context")).toContainText(
      "2025-01-02",
    );
    await expect(trigger).toBeFocused();
    await expect(daily).toContainText("14:45 参考未留存");
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await daily.screenshot({ path: `/tmp/candela-history-${width}.png` });
  });
}

test("late historical response cannot overwrite another date or returning to latest", async ({
  page,
}) => {
  await page.goto("/strategies/four-etf-rotation");
  const daily = page.getByRole("region", { name: "每日数据" });
  await expect(daily).toContainText("+30 bps");
  let release!: () => void;
  const held = new Promise<void>((resolve) => {
    release = resolve;
  });
  let started!: () => void;
  const pending = new Promise<void>((resolve) => {
    started = resolve;
  });
  await page.route(
    "**/api/rotation/daily?tradeDate=20250102",
    async (route) => {
      const response = await route.fetch();
      started();
      await held;
      await route.fulfill({ response }).catch(() => {});
    },
  );
  await choose(page, "2025-01-02");
  await pending;
  await expect(daily).toContainText("等待读取该日数据");
  await expect(daily).not.toContainText("+30 bps");
  await choose(page, "2024-12-31");
  await expect(daily).toContainText("暂无完整每日结果");
  release();
  await expect(daily.locator(".rotation-daily-context")).toContainText(
    "2024-12-31",
  );
  await page.getByRole("button", { name: "回到最新", exact: true }).click();
  await expect(daily).toContainText("+30 bps");
});

test("archive failure and mismatched selected day never replace daily results", async ({
  page,
}) => {
  await page.goto("/strategies/four-etf-rotation");
  const daily = page.getByRole("region", { name: "每日数据" });
  await expect(daily).toContainText("+30 bps");
  await page.route("**/api/rotation/daily/dates", (route) =>
    route.fulfill({ status: 502 }),
  );
  await page.getByRole("button", { name: "选择历史日期" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("alert")).toContainText("档案列表暂不可用");
  await page.unroute("**/api/rotation/daily/dates");
  await dialog.getByRole("button", { name: "重新读取档案" }).click();
  await expect(
    dialog.getByRole("button", { name: /2025-01-02/ }),
  ).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(daily).toContainText("+30 bps");
  await page.route(
    "**/api/rotation/daily?tradeDate=20250102",
    async (route) => {
      const response = await route.fetch({
        url: "http://127.0.0.1:18081/api/rotation/daily",
      });
      await route.fulfill({ response });
    },
  );
  await choose(page, "2025-01-02");
  await expect(daily.getByRole("alert")).toContainText("每日数据返回不完整");
  await expect(daily).not.toContainText("+30 bps");
  await page.unroute("**/api/rotation/daily?tradeDate=20250102");
  await daily.getByRole("button", { name: "重新读取每日数据" }).click();
  await expect(daily).toContainText("14:45 参考未留存");
});

test("archive paginates real capture attempts without requesting later quotes", async ({
  page,
  request,
}) => {
  const before = (
    await (await request.get(backend + "/test-source-count")).json()
  ).calls;
  // The harness caches this controlled trading calendar; submissions are the
  // real API and must remain missing rather than fetch today's quotes.
  const dates: string[] = [];
  for (
    let d = new Date("2024-11-04T00:00:00Z");
    dates.length < 33;
    d.setUTCDate(d.getUTCDate() + 1)
  ) {
    if ([0, 6].includes(d.getUTCDay())) continue;
    dates.push(d.toISOString().slice(0, 10).replaceAll("-", ""));
  }
  for (const tradeDate of dates)
    expect(
      (
        await request.post(backend + "/api/v1/rotation/reference-captures", {
          headers: { "X-Api-Key": "fixture-only" },
          data: { tradeDate },
        })
      ).status(),
    ).toBe(202);
  await page.goto("/strategies/four-etf-rotation");
  await page.getByRole("button", { name: "选择历史日期" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toContainText("档案始于 2024-11-04");
  await expect(
    dialog.getByRole("list", { name: "已留存日期" }).getByRole("button"),
  ).toHaveCount(30);
  await dialog.getByRole("button", { name: "加载更早日期" }).click();
  await expect(
    dialog.getByRole("list", { name: "已留存日期" }).getByRole("button"),
  ).toHaveCount(35);
  await expect(
    dialog.getByRole("button", { name: "加载更早日期" }),
  ).toHaveCount(0);
  await dialog.getByRole("button", { name: /2024-11-04/ }).click();
  const daily = page.getByRole("region", { name: "每日数据" });
  await expect(daily.locator(".rotation-daily-context")).toContainText(
    "2024-11-04",
  );
  await expect(daily).toContainText("暂无完整每日结果");
  expect(
    (await (await request.get(backend + "/test-source-count")).json()).calls,
  ).toBe(before);
});

test("selected historical day receives later close without changing its fixed reference", async ({
  page,
  request,
}) => {
  await page.goto("/strategies/four-etf-rotation");
  await choose(page, "2025-01-03");
  const daily = page.getByRole("region", { name: "每日数据" });
  await expect(daily.locator(".rotation-daily-context")).toContainText(
    "14:45 固定参考",
  );
  const read = async () =>
    await (
      await request.get(backend + "/api/v1/rotation/daily?tradeDate=20250103", {
        headers: { "X-Api-Key": "fixture-only" },
      })
    ).json();
  const before = await read();
  expect(
    (await request.post(backend + "/test-phase?phase=close")).status(),
  ).toBe(204);
  await daily.getByRole("button", { name: "重新读取每日数据" }).click();
  await expect(daily).toContainText("+30 bps");
  await expect(daily).toContainText("已选择 2025-01-03");
  expect((await read()).reference).toEqual(before.reference);
});

test("empty archive and future date stay explicit without affecting the backtest", async ({
  page,
  request,
}) => {
  expect(
    (await request.post(backend + "/test-phase?phase=empty")).status(),
  ).toBe(204);
  await page.goto("/strategies/four-etf-rotation");
  await page.getByRole("button", { name: "选择历史日期" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toContainText("尚无留存档案");
  await expect(
    dialog.getByRole("list", { name: "已留存日期" }).getByRole("button"),
  ).toHaveCount(0);
  await page.getByLabel("指定交易日").fill("2025-01-04");
  await page.getByRole("button", { name: "查看此日", exact: true }).click();
  await expect(dialog).toBeVisible();
  expect(
    await page
      .getByLabel("指定交易日")
      .evaluate((el: HTMLInputElement) => el.validity.rangeOverflow),
  ).toBe(true);
  await page.getByLabel("指定交易日").fill("2024-12-31");
  await page.getByRole("button", { name: "查看此日", exact: true }).click();
  await expect(page.getByRole("region", { name: "每日数据" })).toContainText(
    "暂无完整每日结果",
  );
  await expect(page.getByRole("heading", { name: "收益与风险" })).toBeVisible();
});
