import { test, expect } from "@playwright/test";
import { readFileSync } from "node:fs";
test("Access guards direct page and API visits", async ({ request }) => {
  expect((await request.get("/data")).status()).toBe(401);
  expect((await request.get("/api/catalog")).status()).toBe(401);
});
test("separate pages show honest coverage, filters and retained research", async ({
  page,
}) => {
  await page.setExtraHTTPHeaders({
    "Cf-Access-Jwt-Assertion": readFileSync(
      "/tmp/candela-browser-test-token",
      "utf8",
    ),
  });
  await page.goto("/market");
  await expect(
    page.getByRole("heading", { name: "市场视图，正在起步" }),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "刷新状态" })).toHaveCount(0);
  await page.getByRole("link", { name: "查看数据覆盖" }).click();
  await expect(page.getByText("2024.01.02")).toBeVisible();
  await expect(page.getByText("申万行业 · 读取失败")).toBeVisible();
  await expect(page.getByText("尚未接入", { exact: true })).toBeVisible();
  await expect(page.getByText("暂无数据", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "ETF", exact: true }).click();
  await expect(page.getByText("申万行业 · 读取失败")).toHaveCount(0);
  await page.getByRole("searchbox").fill("510300");
  await expect(page.getByText("沪深300ETF", { exact: true })).toBeVisible();
  await expect(page.getByText("测试空数据ETF", { exact: true })).toHaveCount(0);
  await page.getByRole("searchbox").fill("unknown");
  await expect(
    page.getByText("没有匹配的数据集。试试其他代码或名称。"),
  ).toBeVisible();
  await page.getByRole("link", { name: "研究档案", exact: true }).click();
  await page.getByRole("link", { name: /ETF 轮动研究/ }).click();
  await expect(page).toHaveURL(/research\/index.html/);
});
test("mobile page fits and unavailable service is not an empty catalog", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.setExtraHTTPHeaders({
    "Cf-Access-Jwt-Assertion": readFileSync(
      "/tmp/candela-browser-test-token",
      "utf8",
    ),
  });
  await page.route("**/api/catalog", (route) =>
    route.fulfill({ status: 502, body: "upstream unavailable" }),
  );
  await page.goto("/data");
  await expect(page.getByRole("alert")).toContainText("暂时无法读取数据");
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy();
  await page.screenshot({ path: "/tmp/candela-mobile.png", fullPage: true });
});
