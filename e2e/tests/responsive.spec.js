const { test, expect } = require("@playwright/test");
const { selectText } = require("./helpers");

// Regression guard: the page must never scroll horizontally — including with a
// comment present (which adds the gutter markers that previously overflowed).
test("no horizontal scroll across widths, with a comment present", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/doc/welcome");

  await selectText(page, "leave an inline comment");
  await page.locator(".mg-add.show").click();
  await page.locator("#mg-composer textarea").fill("overflow guard");
  await page.locator("#mg-composer").getByRole("button", { name: "Comment", exact: true }).click();
  await page.locator("mark.mg-hl").first().waitFor();

  for (const w of [1440, 1280, 1024, 992, 900, 768, 414, 360]) {
    await page.setViewportSize({ width: w, height: 900 });
    await page.waitForTimeout(220); // let the gutter re-lay-out (debounced on resize)
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth
    );
    expect(overflow, `horizontal overflow at ${w}px`).toBeLessThanOrEqual(0);
  }
});
