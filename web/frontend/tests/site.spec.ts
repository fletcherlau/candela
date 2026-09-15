import { test, expect } from "@playwright/test";
import { readFileSync } from "node:fs";

test("Access guards frontend, admin and API visits", async ({ request }) => {
  for (const path of [
    "/",
    "/market",
    "/admin",
    "/admin/data",
    "/data",
    "/research",
    "/api/catalog",
  ]) {
    expect((await request.get(path)).status()).toBe(401);
  }
});

test.describe("authenticated website", () => {
  test.beforeEach(async ({ page }) => {
    await page.setExtraHTTPHeaders({
      "Cf-Access-Jwt-Assertion": readFileSync(
        "/tmp/candela-browser-test-token",
        "utf8",
      ),
    });
  });

  test("home and market have only strategy navigation, with keyboard access", async ({
    page,
  }) => {
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    page.on("console", (message) => {
      if (message.type() === "error") errors.push(message.text());
    });
    await page.goto("/");
    await expect(page.getByRole("heading", { level: 1 })).toContainText(
      "看清市场",
    );
    await expect(
      page.locator('a[href^="/admin"], a[href="/data"], a[href^="/research"]'),
    ).toHaveCount(0);
    await page.getByRole("button", { name: "策略", exact: true }).focus();
    await page.keyboard.press("Enter");
    await expect(
      page.getByRole("link", { name: /市场状态 观察全市场/ }),
    ).toBeVisible();
    await page.keyboard.press("ArrowDown");
    await expect(
      page.getByRole("link", { name: /市场状态 观察全市场/ }),
    ).toBeFocused();
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(/\/market$/);
    await expect(
      page.getByRole("heading", { name: "市场图表准备中" }),
    ).toBeVisible();
    await expect(page.getByRole("button", { name: "刷新状态" })).toHaveCount(0);
    await expect(
      page.locator('a[href^="/admin"], a[href="/data"], a[href^="/research"]'),
    ).toHaveCount(0);
    await page.screenshot({
      path: "/tmp/candela-shadcn-market.png",
      fullPage: true,
    });
    await page.getByRole("link", { name: "Candela 首页" }).click();
    await expect(page).toHaveURL(/\/$/);
    await page.screenshot({
      path: "/tmp/candela-shadcn-home.png",
      fullPage: true,
    });
    expect(errors).toEqual([]);
  });

  test("admin retains real coverage, partial failures, search and refresh", async ({
    page,
  }) => {
    await page.goto("/admin");
    await expect(
      page.getByRole("heading", { name: "数据管理", exact: true }),
    ).toBeVisible();
    await expect(page.getByText("2024.01.02")).toBeVisible();
    await expect(page.getByText("申万行业 · 读取失败")).toBeVisible();
    await expect(page.getByText("尚未接入", { exact: true })).toBeVisible();
    await expect(page.getByText("暂无数据", { exact: true })).toBeVisible();
    await page.screenshot({
      path: "/tmp/candela-shadcn-admin.png",
      fullPage: true,
    });
    await page.getByRole("tab", { name: "ETF", exact: true }).click();
    await expect(page.getByText("申万行业 · 读取失败")).toHaveCount(0);
    await page.getByRole("searchbox").fill("510300");
    await expect(page.getByText("沪深300ETF", { exact: true })).toBeVisible();
    await expect(page.getByText("测试空数据ETF", { exact: true })).toHaveCount(
      0,
    );
    await page.getByRole("searchbox").fill("unknown");
    await expect(
      page.getByText("没有匹配的数据集。试试其他代码或名称。"),
    ).toBeVisible();
    await page.getByRole("searchbox").clear();
    await page.route("**/api/catalog", (route) =>
      route.fulfill({
        json: {
          groups: [{ id: "etf", name: "ETF", status: "available", items: [] }],
        },
      }),
    );
    await page.getByRole("button", { name: "刷新状态" }).click();
    await expect(page.getByText("ETF · 暂无数据集记录")).toBeVisible();
    await expect(page.getByText("沪深300ETF", { exact: true })).toHaveCount(0);
  });

  test("old navigation redirects to the agreed frontend and admin routes", async ({
    page,
  }) => {
    await page.goto("/data");
    await expect(page).toHaveURL(/\/admin\/data$/);
    await expect(
      page.getByRole("heading", { name: "数据管理", exact: true }),
    ).toBeVisible();
    await page.goto("/research");
    await expect(page).toHaveURL(/\/$/);
    await expect(page.getByRole("link", { name: /研究/ })).toHaveCount(0);
  });

  test("mobile navigation and data failure state remain usable", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto("/");
    await page.getByRole("button", { name: "策略", exact: true }).click();
    await page.getByRole("link", { name: /市场状态 观察全市场/ }).click();
    await expect(
      page.getByRole("heading", { name: "市场状态", exact: true }),
    ).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBeTruthy();
    await page.screenshot({
      path: "/tmp/candela-shadcn-mobile-market.png",
      fullPage: true,
    });
    await page.goto("/admin/data");
    await expect(page.getByText("2024.01.02")).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBeTruthy();
    await page.screenshot({
      path: "/tmp/candela-shadcn-mobile-admin.png",
      fullPage: true,
    });
    await page.route("**/api/catalog", (route) =>
      route.fulfill({ status: 502, body: "upstream unavailable" }),
    );
    await page.getByRole("button", { name: "刷新状态" }).click();
    await expect(page.getByRole("alert")).toContainText("暂时无法读取数据");
    await expect(page.getByText("沪深300ETF", { exact: true })).toHaveCount(0);
  });
});
