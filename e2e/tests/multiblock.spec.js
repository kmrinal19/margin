const { test, expect } = require("@playwright/test");
const { selectAcross } = require("./helpers");
const fs = require("fs");
const path = require("path");

const docFile = (slug) => path.join(__dirname, "..", "..", "docs", slug + ".md");

test("a selection across two bullets highlights both and orphans when one is deleted", async ({ page }) => {
  const f = docFile("e2e-multi");
  // Deliberately dissimilar bullets so a deleted endpoint can't fuzzy-match the other.
  fs.writeFileSync(f, "## Section\n\n- Authentication uses short-lived bearer tokens.\n- Storage relies on a replicated append-only ledger.\n");
  try {
    await page.goto("/doc/e2e-multi");

    // drag from the first bullet into the second
    await selectAcross(page, "Authentication uses", "replicated append-only ledger");
    await page.locator(".mg-add.show").click();
    await page.locator("#mg-composer textarea").fill("this spans two bullets");
    await page.locator("#mg-composer").getByRole("button", { name: "Comment", exact: true }).click();

    // both list items carry a highlight
    await expect(
      page.locator("#mg-article li").filter({ has: page.locator("mark.mg-hl") })
    ).toHaveCount(2);
    // sidebar shows the head … tail span
    await expect(page.locator(".mg-card .mg-quote").first()).toContainText("…");

    // delete the SECOND bullet's text → the tail can't resolve → orphan
    fs.writeFileSync(f, "## Section\n\n- Authentication uses short-lived bearer tokens.\n- Removed entirely.\n");
    await page.reload();
    await page.locator("#mg-toggle").click();
    await expect(page.locator('.mg-tab[data-filter="orphaned"]')).toBeVisible();
    await expect(page.locator('.mg-tab[data-filter="orphaned"] .chip')).toHaveText("1");
  } finally {
    fs.rmSync(f, { force: true });
  }
});

test("multi-block highlight survives an edit to a non-anchor part", async ({ page }) => {
  const f = docFile("e2e-multi2");
  fs.writeFileSync(f, "## S\n\nThe alpha paragraph leads things off here.\n\nThe omega paragraph closes it all out.\n");
  try {
    await page.goto("/doc/e2e-multi2");
    await selectAcross(page, "alpha paragraph", "omega paragraph");
    await page.locator(".mg-add.show").click();
    await page.locator("#mg-composer textarea").fill("alpha→omega");
    await page.locator("#mg-composer").getByRole("button", { name: "Comment", exact: true }).click();
    await expect(page.locator("mark.mg-hl").first()).toBeVisible();

    // edit text that's NOT part of either endpoint quote → stays anchored
    fs.writeFileSync(f, "## S\n\nThe alpha paragraph leads things off here, truly.\n\nThe omega paragraph closes it all out.\n");
    await page.reload();
    await page.locator("#mg-toggle").click();
    await expect(page.locator('.mg-tab[data-filter="orphaned"]')).toBeHidden();
    await expect(page.locator("mark.mg-hl").first()).toBeVisible();
  } finally {
    fs.rmSync(f, { force: true });
  }
});
