import { test, expect } from "@playwright/test";
import { readFileSync } from "node:fs";
test.beforeEach(async ({ page }) => {
  await page.setExtraHTTPHeaders({
    "Cf-Access-Jwt-Assertion": readFileSync(
      "/tmp/candela-browser-test-token",
      "utf8",
    ),
  });
});
test("saved captures show source evidence and missing records without fetching quotes", async ({
  page,
  request,
}) => {
  const before = await (
    await request.get("http://127.0.0.1:18086/test-source-count")
  ).json();
  await page.goto("/admin/data");
  const panel = page.getByRole("region", { name: "14:45 参考采集" });
  await expect(panel).toBeVisible();
  await expect(panel).toContainText("原始数据采集完成／参考待计算");
  await expect(
    panel.getByRole("heading", { name: "2025-01-07 采集详情" }),
  ).toBeVisible();
  await expect(panel).toContainText("14:45:02");
  await expect(panel).toContainText("14:45:03");
  await expect(panel).toContainText("4/4");
  await panel.getByRole("button", { name: "查看 2025-01-06 采集" }).click();
  await expect(
    panel.getByRole("heading", { name: "2025-01-06 采集详情" }),
  ).toBeVisible();
  await expect(panel).toContainText("原始数据到齐 3/4");
  await expect(panel).toContainText("513100.SH");
  await panel.getByRole("button", { name: "查看 2025-01-03 采集" }).click();
  await expect(panel).toContainText("无法补取");
  await panel.getByRole("button", { name: "重新读取采集记录" }).click();
  await expect(
    panel.getByRole("heading", { name: "2025-01-03 采集详情" }),
  ).toBeVisible();
  const after = await (
    await request.get("http://127.0.0.1:18086/test-source-count")
  ).json();
  expect(before.calls).toBe(8);
  expect(after.calls).toBe(before.calls);
});

for (const width of [1440, 820, 390, 320]) {
  test(`saved capture evidence and keyboard controls remain usable at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    await page.goto("/admin/data");
    const panel = page.getByRole("region", { name: "14:45 参考采集" });
    await expect(
      panel.getByRole("heading", { name: "2025-01-07 采集详情" }),
    ).toBeVisible();
    for (const code of ["510880.SH", "518880.SH", "159915.SZ", "513100.SH"]) {
      const card = panel.locator(`[data-capture-code="${code}"]`);
      for (const field of ["时点价格", "来源时间", "请求时间", "采集时间"])
        await expect(card.getByText(field, { exact: true })).toBeVisible();
    }
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    expect(
      await panel
        .getByRole("heading", { name: "14:45 参考采集", exact: true })
        .evaluate((el) => getComputedStyle(el).fontFamily),
    ).toContain("Noto Serif SC");
    const refresh = panel.getByRole("button", { name: "重新读取采集记录" });
    await refresh.focus();
    await refresh.press("Enter");
    await expect(refresh).toHaveAttribute("aria-disabled", "false");
    await expect(refresh).toBeFocused();
    await panel.screenshot({ path: `/tmp/candela-capture-${width}.png` });
    await panel.getByRole("button", { name: "查看 2025-01-06 采集" }).focus();
    await page.keyboard.press("Enter");
    await expect(panel).toContainText("原始数据到齐 3/4");
    await expect(
      panel.locator('[data-capture-code="513100.SH"]'),
    ).toContainText("历史时点补取");
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    if (width === 390)
      await panel.screenshot({ path: "/tmp/candela-capture-partial-390.png" });
  });
}

test("failed refresh retains dated evidence and real expired authentication is explicit", async ({
  page,
}) => {
  await page.goto("/admin/data");
  const panel = page.getByRole("region", { name: "14:45 参考采集" });
  await expect(
    panel.getByRole("heading", { name: "2025-01-07 采集详情" }),
  ).toBeVisible();
  await page.route("**/api/rotation/reference-captures", (route) =>
    route.fulfill({ status: 502 }),
  );
  await panel.getByRole("button", { name: "重新读取采集记录" }).click();
  await expect(panel.getByRole("alert")).toContainText(
    "当前保留 2025-01-07 已读取记录",
  );
  await expect(panel.locator('[data-capture-code="510880.SH"]')).toContainText(
    "101.000",
  );
  await page.unroute("**/api/rotation/reference-captures");
  await page.setExtraHTTPHeaders({
    "Cf-Access-Jwt-Assertion": "expired-fixture",
  });
  await panel.getByRole("button", { name: "重新读取采集记录" }).click();
  await expect(panel.getByRole("alert")).toContainText("登录已过期或无权访问");
  await page.setExtraHTTPHeaders({
    "Cf-Access-Jwt-Assertion": readFileSync(
      "/tmp/candela-browser-test-token",
      "utf8",
    ),
  });
  await panel.getByRole("button", { name: "重新读取采集记录" }).click();
  await expect(panel.getByRole("alert")).toHaveCount(0);
});

test("loading and late date responses never relabel a saved record", async ({
  page,
}) => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/api/rotation/reference-captures", async (route) => {
    const response = await route.fetch();
    await gate;
    await route.fulfill({ response });
  });
  await page.goto("/admin/data");
  const panel = page.getByRole("region", { name: "14:45 参考采集" });
  await expect(
    panel.getByLabel("正在读取采集记录", { exact: true }),
  ).toBeVisible();
  release();
  await expect(
    panel.getByRole("heading", { name: "2025-01-07 采集详情" }),
  ).toBeVisible();
  await page.unroute("**/api/rotation/reference-captures");
  let late!: () => void;
  let started!: () => void;
  const held = new Promise<void>((resolve) => {
    late = resolve;
  });
  const requested = new Promise<void>((resolve) => {
    started = resolve;
  });
  await page.route(
    "**/api/rotation/reference-captures/20250106",
    async (route) => {
      const response = await route.fetch();
      started();
      await held;
      await route.fulfill({ response }).catch(() => {});
    },
  );
  await panel.getByRole("button", { name: "查看 2025-01-06 采集" }).click();
  await requested;
  await expect(
    panel.getByRole("heading", { name: "2025-01-07 采集详情" }),
  ).toHaveCount(0);
  await panel.getByRole("button", { name: "查看 2025-01-03 采集" }).click();
  await expect(
    panel.getByRole("heading", { name: "2025-01-03 采集详情" }),
  ).toBeVisible();
  late();
  await expect(
    panel.getByRole("heading", { name: "2025-01-03 采集详情" }),
  ).toBeVisible();
  await expect(
    panel.getByRole("heading", { name: "2025-01-06 采集详情" }),
  ).toHaveCount(0);
});

test("empty capture archives remain distinct from read failures", async ({
  page,
}) => {
  await page.route("**/api/rotation/reference-captures", (route) =>
    route.fulfill({ json: { runs: [] } }),
  );
  await page.goto("/admin/data");
  const panel = page.getByRole("region", { name: "14:45 参考采集" });
  await expect(panel).toContainText("暂无固定参考采集记录");
  await expect(panel).toContainText("历史日线和旧盘中信号不等于");
  await expect(panel.getByRole("alert")).toHaveCount(0);
});
