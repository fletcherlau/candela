import { test, expect } from "@playwright/test";
import { readFileSync } from "node:fs";

test("persistent backfill, duplicate submission, incremental and source failure", async ({
  page,
  request,
}) => {
  await page.setExtraHTTPHeaders({
    "Cf-Access-Jwt-Assertion": readFileSync(
      "/tmp/candela-browser-test-token",
      "utf8",
    ),
  });
  await page.goto("/admin/data");
  const panel = page.getByRole("region", { name: "同步任务详情" });
  await page.getByRole("button", { name: "历史回填", exact: true }).click();
  await expect(page).toHaveURL(/\?run=[a-f0-9]{32}$/);
  const originalURL = page.url();
  await expect(panel).toContainText("全部可用历史");
  // A second browser submission reuses the same persistent execution.
  await page.getByRole("button", { name: "历史回填", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("已有相同任务");
  expect(page.url()).toBe(originalURL);
  await page.reload();
  expect(page.url()).toBe(originalURL);
  await expect(panel).toContainText("2004.12.31", { timeout: 30000 });
  await expect(panel.getByText("已完成", { exact: true })).toBeVisible({
    timeout: 60000,
  });
  await expect(panel).toContainText("源端最早记录 20041231");
  await page.screenshot({
    path: "/tmp/candela-t02-completed.png",
    fullPage: true,
  });
  await page.getByRole("button", { name: "增量同步", exact: true }).click();
  await expect(page).not.toHaveURL(originalURL);
  await expect(panel.getByText("已完成", { exact: true })).toBeVisible({
    timeout: 15000,
  });
  await expect(panel).toContainText("1 条");
  await expect(page.getByLabel("最近同步任务").getByRole("button")).toHaveCount(
    2,
  );
  // Trigger a controlled permission failure; the browser displays a persisted result.
  expect(
    (
      await request.post("http://127.0.0.1:18082/fixture/permission", {
        headers: { "X-Api-Key": "fixture-only" },
      })
    ).status(),
  ).toBe(204);
  await page.getByRole("button", { name: "增量同步", exact: true }).click();
  await expect(panel.getByText("失败", { exact: true })).toBeVisible({
    timeout: 15000,
  });
  await expect(panel).toContainText("permission_denied");
  await page.reload();
  await expect(panel).toContainText("permission_denied");
  await page.setViewportSize({ width: 390, height: 844 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBeTruthy();
  await page.screenshot({
    path: "/tmp/candela-t02-mobile.png",
    fullPage: true,
  });
});
