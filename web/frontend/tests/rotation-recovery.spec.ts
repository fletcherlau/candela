import { test, expect } from "@playwright/test";
import { readFileSync } from "node:fs";
const token = () => readFileSync("/tmp/candela-browser-test-token", "utf8");
test.beforeEach(async ({ page }) => {
  await page.setExtraHTTPHeaders({ "Cf-Access-Jwt-Assertion": token() });
});
test("recovery management writes retain Access origin CSRF and original-scope protections", async ({
  page,
  request,
}) => {
  const state = await (
    await request.get("http://127.0.0.1:18090/test-state")
  ).json();
  await page.goto("/admin/data");
  const cases = await page.evaluate(async ({ batchId, recoveryId }) => {
    const session = await (await fetch("/api/session")).json();
    const paths = [
      "/api/rotation/reference-captures/20250103/retry",
      `/api/rotation/close-syncs/${batchId}/retry`,
      `/api/rotation/recoveries/${recoveryId}/retry`,
    ];
    const statuses = [];
    for (const path of paths) {
      statuses.push((await fetch(path, { method: "POST" })).status);
      statuses.push(
        (
          await fetch(path, {
            method: "POST",
            headers: { "X-CSRF-Token": session.csrfToken },
            body: JSON.stringify({ tradeDate: "20250110" }),
          })
        ).status,
      );
      statuses.push((await fetch(path, { method: "GET" })).status);
    }
    statuses.push(
      (
        await fetch("/api/rotation/recoveries", {
          method: "POST",
          headers: { "X-CSRF-Token": session.csrfToken },
        })
      ).status,
    );
    return statuses;
  }, state);
  expect(cases).toEqual([403, 400, 405, 403, 400, 405, 403, 400, 405, 405]);
  const cross = await request.post(
    `/api/rotation/close-syncs/${state.batchId}/retry`,
    {
      headers: {
        "Cf-Access-Jwt-Assertion": token(),
        Origin: "https://evil.invalid",
        "X-CSRF-Token": "x".repeat(43),
        Cookie: "__Host-candela-csrf=" + "x".repeat(43),
      },
    },
  );
  expect(cross.status()).toBe(403);
  expect((await request.get("/api/rotation/recoveries")).status()).toBe(401);
  const after = await (
    await request.get("http://127.0.0.1:18090/test-state")
  ).json();
  expect(after).toEqual(state);
});

test("administrator restores frozen reference and failed close step through the management page", async ({
  page,
  request,
}) => {
  await page.goto("/admin/data");
  const captures = page.getByRole("region", { name: "14:45 参考采集" });
  await expect(
    captures.getByRole("heading", { name: "2025-01-03 采集详情" }),
  ).toBeVisible();
  const reference = captures.getByRole("region", { name: "14:45 参考恢复" });
  await expect(reference).toContainText("计算或发布失败");
  await reference.getByRole("button", { name: "恢复参考计算" }).click();
  await expect(reference).toContainText("已发布");
  await expect(
    reference.getByRole("button", { name: "恢复参考计算" }),
  ).toHaveCount(0);
  await expect(reference).toContainText("父恢复任务");
  const close = page.getByRole("region", { name: "收盘结果恢复" });
  await close.getByRole("button", { name: "恢复收盘同步与发布" }).click();
  await expect(close).toContainText("四标的收盘数据已发布");
  await expect(
    close.getByRole("link", { name: "查看恢复同步批次" }),
  ).toBeVisible();
  const state = await (
    await request.get("http://127.0.0.1:18090/test-state")
  ).json();
  expect(state.referenceCalls).toBe(4);
  for (const code of ["510880.SH", "518880.SH", "159915.SZ", "513100.SH"]) {
    expect(state.calls[`${code}/daily`]).toBe(1);
    expect(state.calls[`${code}/adj`]).toBe(code === "513100.SH" ? 2 : 1);
  }
  const published = await page.evaluate(
    async () =>
      await (await fetch("/api/rotation/daily?tradeDate=20250103")).json(),
  );
  expect(published.reference.available).toBe(4);
  expect(published.close.available).toBe(4);
  await captures.getByRole("button", { name: "查看 2025-01-02 采集" }).click();
  await expect(captures).toContainText("原时点数据无法补取");
  await expect(
    captures.getByRole("button", { name: "恢复参考计算" }),
  ).toHaveCount(0);
});

for (const width of [1440, 820, 390, 320]) {
  test(`recovery records and keyboard controls fit ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    await page.goto("/admin/data");
    const panel = page.getByRole("region", { name: "14:45 参考采集" });
    const recovery = panel.getByRole("region", { name: "14:45 参考恢复" });
    await expect(recovery).toContainText("原交易日 2025-01-03");
    const refresh = recovery.getByRole("button", { name: "重新读取恢复记录" });
    await expect(refresh).toHaveAttribute("aria-disabled", "false");
    await refresh.focus();
    await refresh.press("Enter");
    await expect(refresh).toHaveAttribute("aria-disabled", "false");
    await expect(refresh).toBeFocused();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await recovery.screenshot({ path: `/tmp/candela-recovery-${width}.png` });
    await panel.getByRole("button", { name: "查看 2025-01-02 采集" }).click();
    await expect(recovery).toContainText("原时点数据无法补取");
    await expect(recovery).not.toContainText("原交易日 2025-01-03");
    if (width === 390)
      await recovery.screenshot({ path: "/tmp/candela-recovery-missing.png" });
  });
}

test("recovery read failure preserves dated state and expired login prevents further writes", async ({
  page,
}) => {
  await page.goto("/admin/data");
  const panel = page.getByRole("region", { name: "14:45 参考恢复" });
  await expect(panel).toContainText("原目标时点");
  await page.route("**/api/rotation/recoveries", (route) =>
    route.fulfill({ status: 502 }),
  );
  await panel.getByRole("button", { name: "重新读取恢复记录" }).click();
  await expect(panel.getByRole("alert")).toContainText("保留上次读取记录");
  await expect(panel).toContainText("原交易日 2025-01-03");
  await page.unroute("**/api/rotation/recoveries");
  await page.setExtraHTTPHeaders({
    "Cf-Access-Jwt-Assertion": "expired-fixture",
  });
  await panel.getByRole("button", { name: "重新读取恢复记录" }).click();
  await expect(panel.getByRole("alert")).toContainText("登录或写入验证已失效");
  await page.setExtraHTTPHeaders({ "Cf-Access-Jwt-Assertion": token() });
  await panel.getByRole("button", { name: "重新读取恢复记录" }).click();
  await expect(panel.getByRole("alert")).toHaveCount(0);
});

test("a dated recovery result link opens the requested historical day", async ({
  page,
}) => {
  await page.goto("/strategies/four-etf-rotation?tradeDate=20250102");
  const daily = page.getByRole("region", { name: "每日数据" });
  await expect(daily.locator(".rotation-daily-context")).toContainText(
    "2025-01-02",
  );
  await expect(daily).toContainText("暂无完整每日结果");
  await page.getByRole("button", { name: "回到最新", exact: true }).click();
  await expect(daily.locator(".rotation-daily-context")).toContainText(
    "2025-01-03",
  );
  expect(new URL(page.url()).searchParams.has("tradeDate")).toBe(false);
});
