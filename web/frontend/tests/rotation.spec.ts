import { test, expect } from "@playwright/test";
import { readFileSync } from "node:fs";
test("rotation page and API require Access", async ({ request }) => {
  for (const path of [
    "/strategies/four-etf-rotation",
    "/api/rotation/backtest",
  ])
    expect((await request.get(path)).status()).toBe(401);
});
test.describe("rotation history", () => {
  test.beforeEach(async ({ page }) => {
    await page.setExtraHTTPHeaders({
      "Cf-Access-Jwt-Assertion": readFileSync(
        "/tmp/candela-browser-test-token",
        "utf8",
      ),
    });
  });
  test("one year default, zoom, benchmarks, dates and holdings", async ({
    page,
  }) => {
    const errors: string[] = [];
    page.on("pageerror", (e) => errors.push(e.message));
    page.on("console", (e) => {
      if (e.type() === "error") errors.push(e.text());
    });
    await page.goto("/");
    await page.getByRole("button", { name: "策略", exact: true }).click();
    await page.getByRole("link", { name: /四标的轮动/ }).click();
    await expect(
      page.getByRole("heading", { name: "四标的轮动" }),
    ).toBeVisible();
    const dateStart = page.getByLabel("开始日期");
    const dateEnd = page.getByLabel("结束日期");
    const initialStart = await dateStart.inputValue();
    const initialEnd = await dateEnd.inputValue();
    expect(
      new Date(initialEnd).getFullYear() - new Date(initialStart).getFullYear(),
    ).toBe(1);
    await expect(
      page.getByRole("checkbox", { name: "黄金 ETF" }),
    ).not.toBeChecked();
    await page.getByRole("checkbox", { name: "黄金 ETF" }).check();
    await expect(
      page.getByRole("checkbox", { name: "黄金 ETF" }),
    ).toBeChecked();
    await expect(
      page.getByRole("img", { name: "策略区间回撤图" }),
    ).toBeVisible();
    await expect(page.getByTestId("rotation-detail")).toContainText(
      "现金 30.00%",
    );
    await page.getByRole("radio", { name: "全部", exact: true }).click();
    await expect(dateStart).toHaveValue("2024-10-01");
    const chart = page.getByRole("img", { name: "策略与 ETF 累计收益图" });
    const box = await chart.boundingBox();
    if (!box) throw new Error("missing chart");
    await page.mouse.move(box.x + box.width * 0.3, box.y + 80);
    await page.mouse.down();
    await page.mouse.move(box.x + box.width * 0.65, box.y + 80);
    await page.mouse.up();
    expect(await dateStart.inputValue()).not.toBe("2024-10-01");
    await page.getByRole("radio", { name: "近一年", exact: true }).click();
    await expect(dateStart).toHaveValue(initialStart);
    await expect(dateEnd).toHaveValue(initialEnd);
    await page.screenshot({
      path: "/tmp/candela-rotation-desktop.png",
      fullPage: true,
    });
    expect(errors).toEqual([]);
  });
  test("mobile, unavailable data and refresh failure retain complete results", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto("/strategies/four-etf-rotation");
    await expect(page.getByTestId("rotation-detail")).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({
      path: "/tmp/candela-rotation-mobile.png",
      fullPage: true,
    });
    await page.route("**/api/rotation/backtest", (r) =>
      r.fulfill({ status: 502, body: "unavailable" }),
    );
    await page.getByRole("button", { name: "刷新回测" }).click();
    await expect(page.getByRole("alert")).toContainText("暂时无法读取回测");
    await expect(
      page.getByRole("img", { name: "策略与 ETF 累计收益图" }),
    ).toBeVisible();
    await page.unroute("**/api/rotation/backtest");
    await page.route("**/api/rotation/backtest", (r) =>
      r.fulfill({
        json: { status: "failed", message: "行情缺失，等待同步", result: null },
      }),
    );
    await page.getByRole("button", { name: "刷新回测" }).click();
    await expect(
      page.getByRole("heading", { name: "暂无完整回测结果" }),
    ).toBeVisible();
    await expect(page.getByRole("status")).toContainText("行情缺失");
  });
});
