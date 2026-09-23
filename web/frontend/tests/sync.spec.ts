import { test, expect } from "@playwright/test";
import { readFileSync } from "node:fs";

test("persistent backfill, duplicate submission, incremental and source failure", async ({
  page,
  request,
}) => {
  test.setTimeout(90000);
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
  expect(
    (
      await request.post("http://127.0.0.1:18082/fixture/slow", {
        headers: { "X-Api-Key": "fixture-only" },
      })
    ).status(),
  ).toBe(204);
  await page.getByRole("button", { name: "历史回填", exact: true }).click();
  await expect(panel.getByText("执行中", { exact: true })).toBeVisible({
    timeout: 15000,
  });
  const cancelledURL = page.url();
  const cancel = panel.getByRole("button", { name: "取消任务", exact: true });
  await cancel.focus();
  await page.keyboard.press("Enter");
  await expect(panel.getByText("取消中", { exact: true })).toBeVisible();
  await expect(panel.getByText("已取消", { exact: true })).toBeVisible({
    timeout: 15000,
  });
  await expect(panel.getByRole("list", { name: "执行记录" })).toContainText(
    "已记录取消意图",
  );
  await expect(cancel).toBeFocused();
  await expect(cancel).toHaveAttribute("aria-disabled", "true");
  await expect(cancel).toHaveCSS("opacity", "0.5");
  await page.reload();
  expect(page.url()).toBe(cancelledURL);
  await expect(panel.getByText("已取消", { exact: true })).toBeVisible();
  for (const width of [1440, 820, 390, 320]) {
    await page.setViewportSize({ width, height: 900 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBeTruthy();
    await page.screenshot({
      path: `/tmp/candela-t03-${width}.png`,
      fullPage: true,
    });
  }
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.getByRole("button", { name: "历史回填", exact: true }).click();
  await expect(panel.getByText("执行中", { exact: true })).toBeVisible({
    timeout: 15000,
  });
  const recoveredURL = page.url();
  expect(
    (
      await request.post("http://127.0.0.1:18082/fixture/restart", {
        headers: { "X-Api-Key": "fixture-only" },
      })
    ).status(),
  ).toBe(204);
  await expect(panel.getByRole("list", { name: "执行记录" })).toContainText(
    "服务执行中断",
    { timeout: 15000 },
  );
  await expect(panel.getByRole("list", { name: "执行记录" })).toContainText(
    "已取得新的执行权",
  );
  await page.reload();
  expect(page.url()).toBe(recoveredURL);
  await expect(panel.getByRole("list", { name: "执行记录" })).toContainText(
    "从原检查点恢复固定范围",
  );
  await page.screenshot({
    path: "/tmp/candela-t03-recovery.png",
    fullPage: true,
  });
  await panel.getByRole("button", { name: "取消任务", exact: true }).click();
  await expect(panel.getByText("已取消", { exact: true })).toBeVisible({
    timeout: 15000,
  });
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
