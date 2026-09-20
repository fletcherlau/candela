import { test, expect } from "@playwright/test";

test("shared primitives work outside the sample; portals inherit without changing legacy UI", async ({ page }) => {
  await page.goto("/tests/fixtures/research-theme.html");
  const scope = page.getByTestId("research");
  await expect(scope.getByRole("button", { name: "研究按钮", exact: true })).toHaveCSS("background-color", "rgb(36, 36, 32)");
  await expect(page.getByTestId("legacy").getByRole("button", { name: "原有按钮", exact: true })).toHaveCSS("background-color", "rgb(42, 113, 87)");
  await expect(scope.getByLabel("研究输入")).toHaveCSS("border-radius", "4px");
  await expect(scope.getByLabel("研究输入")).toHaveCSS("height", "44px");
  await expect(scope.locator('[data-slot="card"]')).toHaveCSS("box-shadow", "none");
  await expect(scope.getByRole("button", { name: "中性按钮" })).toHaveCSS("background-color", "rgb(243, 243, 243)");
  await expect(scope.getByRole("button", { name: "错误按钮" })).toHaveCSS("background-color", "rgb(163, 51, 67)");
  const checkbox = scope.getByRole("checkbox");
  await checkbox.focus();
  await page.keyboard.press("Space");
  await expect(checkbox).toBeChecked();
  await expect(checkbox).toHaveCSS("background-color", "rgb(36, 36, 32)");
  const trigger = scope.getByRole("button", { name: "研究弹窗" });
  await trigger.click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toHaveCSS("background-color", "rgb(255, 255, 255)");
  await expect(dialog).toHaveCSS("border-radius", "12px");
  await expect(dialog).toHaveCSS("box-shadow", "none");
  await expect(dialog.getByRole("heading")).toHaveCSS("font-weight", "300");
  await expect(dialog.locator(".lucide")).toHaveCSS("stroke-width", "1.5px");
  await expect(dialog.getByLabel("弹窗输入")).toHaveCSS("border-radius", "4px");
  const close = dialog.getByRole("button", { name: "Close" });
  await close.focus();
  await expect(close).toHaveCSS("outline-color", "rgb(36, 36, 32)");
  await expect(close).toHaveCSS("box-shadow", "none");
  // Wait for the modal scale transition before checking physical target size.
  await expect.poll(async () => (await close.boundingBox())!.width).toBeGreaterThanOrEqual(44);
  await expect.poll(async () => (await close.boundingBox())!.height).toBeGreaterThanOrEqual(44);
  for (let i = 0; i < 5; i++) {
    await page.keyboard.press("Tab");
    expect(await dialog.evaluate(el => el.contains(document.activeElement))).toBe(true);
  }
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  await expect(trigger).toBeFocused();
  await page.getByRole("button", { name: "原有弹窗" }).click();
  await expect(page.getByRole("dialog")).not.toHaveClass(/research-theme/);
});

for (const width of [1440, 820, 390, 320]) {
  test(`sample remains readable and usable at ${width}px`, async ({ page }) => {
    const errors: string[] = [];
    page.on("pageerror", error => errors.push(error.message));
    await page.setViewportSize({ width, height: 1000 });
    await page.goto("/design-preview");
    await expect(page.locator(".research-intro h1")).toHaveCSS("font-weight", "300");
    await page.evaluate(() => document.fonts.ready);
    await expect(page.locator(".research-intro-copy")).toHaveCSS("font-size", "16px");
    await expect(page.locator(".metric-number").first()).toHaveCSS("font-family", /Source Serif 4/);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.getByRole("button", { name: "数据与方法" }).click();
    await expect(page.getByRole("dialog")).toHaveCSS("border-radius", "12px");
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).toBeHidden();
    expect(errors).toEqual([]);
  });
}
