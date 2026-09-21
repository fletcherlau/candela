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
test("20 ETF batch exposes two failed factors and retries only those original steps", async ({
  page,
  request,
}) => {
  await page.goto("/admin/data");
  const panel = page.getByRole("region", { name: "ETF 批量同步" });
  await expect(
    panel.getByText("暂无 ETF 同步批次", { exact: true }),
  ).toBeVisible();
  await panel
    .getByRole("button", { name: "同步全部启用 ETF", exact: true })
    .click();
  await expect(panel.getByLabel("ETF 批次详情")).toContainText("成功 18 / 20", {
    timeout: 15000,
  });
  await expect(panel.getByLabel("ETF 批次详情")).toContainText("失败 2");
  const failed = panel.locator('[data-etf-code="500019.SH"]');
  await expect(failed).toContainText("2025.01.03");
  await expect(failed).toContainText("复权因子");
  await expect(failed).toContainText("source_adj");
  await failed.scrollIntoViewIfNeeded();
  await panel.screenshot({ path: "/tmp/candela-etf-partial.png" });
  const original = new URL(page.url()).searchParams.get("etfBatch");
  await page.reload();
  await expect(panel.getByLabel("ETF 批次详情")).toContainText(original!);
  await request.post("http://127.0.0.1:18089/fixture?mode=repair");
  await panel.getByRole("button", { name: "仅重试失败 2 只" }).click();
  await expect(panel.getByLabel("ETF 批次详情")).toContainText("成功 2 / 2", {
    timeout: 15000,
  });
  await expect(panel.getByLabel("ETF 批次详情")).toContainText(
    "截止 2025.01.03",
  );
  await expect(panel.getByRole("button", { name: "查看原批次" })).toBeVisible();
  const counts = await (
    await request.get("http://127.0.0.1:18089/fixture")
  ).json();
  for (let i = 0; i < 20; i++) {
    const code = `500${String(i).padStart(3, "0")}.SH`;
    expect(counts.calls[`${code}/daily`]).toBe(1);
    expect(counts.calls[`${code}/adj`]).toBe(i >= 18 ? 2 : 1);
  }
  await panel.getByRole("button", { name: "查看原批次" }).click();
  await expect(panel.getByLabel("ETF 批次详情")).toContainText(original!);
});

for (const width of [1440, 820, 390, 320]) {
  test(`ETF details, keyboard and long identifiers fit ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    await page.goto("/admin/data");
    const panel = page.getByRole("region", { name: "ETF 批量同步" });
    await panel.getByLabel("指定 ETF 代码", { exact: true }).fill("510880.SH");
    const accepted = page.waitForResponse(
      (response) =>
        response.url().endsWith("/api/etf-syncs") &&
        response.request().method() === "POST",
    );
    await panel
      .getByRole("button", { name: "同步指定 ETF", exact: true })
      .click();
    const submitted = (await (await accepted).json()).batch.id;
    await expect(page).toHaveURL(new RegExp(`etfBatch=${submitted}`));
    const detail = panel.getByLabel("ETF 批次详情");
    await expect(detail).toContainText("成功 1 / 1", { timeout: 15000 });
    await expect(detail).toContainText("复权因子");
    await expect(panel.getByRole("button", { name: /仅重试失败/ })).toHaveCount(
      0,
    );
    const id = new URL(page.url()).searchParams.get("etfBatch")!;
    await panel
      .getByRole("button", { name: `查看 ETF 批次 ${id.slice(0, 8)}` })
      .click();
    await expect(detail).toContainText(id);
    const refresh = panel.getByRole("button", { name: "刷新 ETF 批次" });
    await refresh.focus();
    await refresh.press("Enter");
    await expect(refresh).toHaveAttribute("aria-disabled", "false");
    await expect(refresh).toBeFocused();
    await page.evaluate(() => document.fonts.ready);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    expect(
      await panel
        .getByText("ETF 批量同步", { exact: true })
        .evaluate((el) => getComputedStyle(el).fontFamily),
    ).toContain("Noto Serif SC");
    await panel.screenshot({ path: `/tmp/candela-etf-${width}.png` });
  });
}

test("cancelled work remains cancelled after a browser reload", async ({
  page,
  request,
}) => {
  await request.post("http://127.0.0.1:18089/fixture?mode=hold");
  try {
    await page.goto("/admin/data");
    const panel = page.getByRole("region", { name: "ETF 批量同步" });
    await panel
      .getByLabel("指定 ETF 代码", { exact: true })
      .fill("510999.SH 511000.SH");
    const accepted = page.waitForResponse(
      (response) =>
        response.url().endsWith("/api/etf-syncs") &&
        response.request().method() === "POST",
    );
    await panel
      .getByRole("button", { name: "同步指定 ETF", exact: true })
      .click();
    const submitted = (await (await accepted).json()).batch.id;
    await expect(page).toHaveURL(new RegExp(`etfBatch=${submitted}`));
    await expect(panel.getByLabel("ETF 批次详情")).toContainText("成功 0 / 2");
    const id = new URL(page.url()).searchParams.get("etfBatch")!;
    await page.reload();
    await expect(panel.getByLabel("ETF 批次详情")).toContainText(id);
    await panel.getByRole("button", { name: "取消未完成对象" }).click();
    await request.post("http://127.0.0.1:18089/fixture?mode=release");
    await expect(panel.getByLabel("ETF 批次详情")).toContainText("取消 2", {
      timeout: 15000,
    });
    await expect(panel.getByRole("button", { name: /仅重试失败/ })).toHaveCount(
      0,
    );
    await panel.getByText("执行记录（最近 200 条）", { exact: true }).click();
    await expect(panel.getByLabel("ETF 执行记录")).toContainText("已确认取消");
    await page.reload();
    await expect(panel.getByLabel("ETF 批次详情")).toContainText("取消 2");
  } finally {
    await request.post("http://127.0.0.1:18089/fixture?mode=release");
  }
});

test("read failures preserve dated results and expired Access is explicit", async ({
  page,
}) => {
  await page.goto("/admin/data");
  const panel = page.getByRole("region", { name: "ETF 批量同步" });
  await panel.getByLabel("指定 ETF 代码", { exact: true }).fill("510880.SH");
  await panel
    .getByRole("button", { name: "同步指定 ETF", exact: true })
    .click();
  await expect(panel.getByLabel("ETF 批次详情")).toContainText("成功 1 / 1", {
    timeout: 15000,
  });
  await page.route("**/api/etf-syncs", (route) =>
    route.fulfill({ status: 502 }),
  );
  await panel.getByRole("button", { name: "刷新 ETF 批次" }).click();
  await expect(panel.getByRole("alert")).toContainText("上次读取结果");
  await expect(panel.getByLabel("ETF 批次详情")).toContainText(
    "截止 2025.01.03",
  );
  await page.unroute("**/api/etf-syncs");
  await page.setExtraHTTPHeaders({
    "Cf-Access-Jwt-Assertion": "expired-fixture",
  });
  await panel.getByRole("button", { name: "刷新 ETF 批次" }).click();
  await expect(panel.getByRole("alert")).toContainText("登录已过期或无权访问");
});

test("loading, timeout and malformed results stay distinct from an empty archive", async ({
  page,
}) => {
  await page.clock.install();
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/api/etf-syncs", async (route) => {
    await gate;
    await route.abort().catch(() => {});
  });
  await page.goto("/admin/data");
  const panel = page.getByRole("region", { name: "ETF 批量同步" });
  await expect(
    panel.getByLabel("正在读取 ETF 批次", { exact: true }),
  ).toBeVisible();
  await expect(
    panel.getByText("暂无 ETF 同步批次", { exact: true }),
  ).toHaveCount(0);
  await page.clock.fastForward(16000);
  try {
    await expect(panel.getByRole("alert")).toContainText("读取超时");
    await expect(
      panel.getByRole("button", { name: "刷新 ETF 批次" }),
    ).toHaveAttribute("aria-disabled", "false");
  } finally {
    release();
  }
  await page.unroute("**/api/etf-syncs");
  await page.route("**/api/etf-syncs", (route) =>
    route.fulfill({ json: { batches: [{ id: "bad" }] } }),
  );
  await panel.getByRole("button", { name: "刷新 ETF 批次" }).click();
  await expect(panel.getByRole("alert")).toContainText("批次服务返回异常");
  await expect(
    panel.getByText("暂无 ETF 同步批次", { exact: true }),
  ).toHaveCount(0);
});

test("invalid ETF codes are associated with the field and never submitted", async ({
  page,
}) => {
  await page.goto("/admin/data");
  const panel = page.getByRole("region", { name: "ETF 批量同步" });
  const input = panel.getByLabel("指定 ETF 代码", { exact: true });
  let writes = 0;
  page.on("request", (r) => {
    if (r.url().endsWith("/api/etf-syncs") && r.method() === "POST") writes++;
  });
  await input.fill("invalid");
  await panel
    .getByRole("button", { name: "同步指定 ETF", exact: true })
    .click();
  await expect(input).toHaveAttribute("aria-invalid", "true");
  await expect(input).toHaveAccessibleDescription(/请输入 1 至 500/);
  expect(writes).toBe(0);
  await input.fill("510880.SH");
  await expect(input).not.toHaveAttribute("aria-invalid", "true");
});
