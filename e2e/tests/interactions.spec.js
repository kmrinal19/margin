const { test, expect } = require("@playwright/test");
const { selectText } = require("./helpers");
const fs = require("fs");
const path = require("path");

const docFile = (slug) => path.join(__dirname, "..", "..", "docs", slug + ".md");
const FIX = "## Alpha\n\nThe alpha section discusses the first concern in detail right here.\n\n## Beta\n\nThe beta section covers the second concern thoroughly as well.\n";

async function comment(page, sub, body) {
  await selectText(page, sub);
  await page.locator(".mg-add.show").click();
  await page.locator("#mg-composer textarea").fill(body);
  await page.locator("#mg-composer").getByRole("button", { name: "Comment", exact: true }).click();
}

test.beforeEach(() => fs.writeFileSync(docFile("e2e-ix"), FIX));
test.afterEach(() => fs.rmSync(docFile("e2e-ix"), { force: true }));

test("composer opens at the selection, not the top-left corner", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 860 });
  await page.goto("/doc/e2e-ix");
  await selectText(page, "second concern thoroughly"); // lower on the page
  await page.locator(".mg-add.show").click();
  const composer = page.locator("#mg-composer");
  await expect(composer).toBeVisible();
  const box = await composer.boundingBox();
  expect(box.x, "composer x").toBeGreaterThan(120);
  expect(box.y, "composer y").toBeGreaterThan(120);
});

test("a new comment becomes active, so keyboard e resolves it", async ({ page }) => {
  await page.goto("/doc/e2e-ix");
  await comment(page, "first concern in detail", "resolve me with the keyboard");
  await expect(page.locator(".mg-card.active")).toBeVisible(); // new comment is active
  await expect(page.locator('.mg-tab[data-filter="open"] .chip')).toHaveText("1");

  await page.locator("h1.doc-title").click(); // focus out of any input
  await page.keyboard.press("e");
  await expect(page.locator('.mg-tab[data-filter="resolved"] .chip')).toHaveText("1");
  await expect(page.locator('.mg-tab[data-filter="open"] .chip')).toHaveText("0");
});

test("Escape closes the composer first, then the sidebar", async ({ page }) => {
  await page.goto("/doc/e2e-ix");
  // composer open → Escape closes just the composer
  await selectText(page, "first concern in detail");
  await page.locator(".mg-add.show").click();
  await expect(page.locator("#mg-composer")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.locator("#mg-composer")).toHaveCount(0);

  // a created comment opens the sidebar → Escape closes it (different phrase)
  await comment(page, "second concern thoroughly", "esc test");
  await expect(page.locator("#mg-sidebar.open")).toBeVisible();
  await page.locator("h1.doc-title").click();
  await page.keyboard.press("Escape");
  await expect(page.locator("#mg-sidebar.open")).toHaveCount(0);
});
