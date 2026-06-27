const { test, expect } = require("@playwright/test");
const { selectText } = require("./helpers");
const fs = require("fs");
const path = require("path");

// Each fixture test uses its OWN slug so the shared comment DB doesn't carry
// threads between tests (the webServer serves --docs ../docs).
const docFile = (slug) => path.join(__dirname, "..", "..", "docs", slug + ".md");

async function addComment(page, sub, body) {
  await selectText(page, sub);
  await page.locator(".mg-add.show").click();
  await page.locator("#mg-composer textarea").fill(body);
  await page.locator("#mg-composer").getByRole("button", { name: "Comment", exact: true }).click();
}

test("keyboard: j activates a thread and opens the sidebar", async ({ page }) => {
  await page.goto("/doc/welcome");
  await addComment(page, "Token-efficient", "keyboard nav check");
  await expect(page.locator("mark.mg-hl").first()).toBeVisible();

  await page.keyboard.press("Escape"); // close composer/sidebar
  await page.locator("h1.doc-title").click(); // focus outside any input
  await page.keyboard.press("j");

  await expect(page.locator("#mg-sidebar.open")).toBeVisible();
  await expect(page.locator(".mg-card.active")).toBeVisible();
});

test("orphaned comment surfaces after its quoted text is deleted", async ({ page }) => {
  const f = docFile("e2e-orphan");
  fs.writeFileSync(f, "## Section\n\nThe migration deadline is the fifteenth of June for everyone.\n");
  try {
    await page.goto("/doc/e2e-orphan");
    await addComment(page, "fifteenth of June", "Is this date right?");
    await expect(page.locator("mark.mg-hl").first()).toBeVisible();

    // delete the quoted text from the source — the thread should orphan
    fs.writeFileSync(f, "## Section\n\nThe migration schedule has been removed.\n");
    await page.reload();

    await page.locator("#mg-toggle").click(); // open sidebar
    const orphTab = page.locator('.mg-tab[data-filter="orphaned"]');
    await expect(orphTab).toBeVisible();
    await expect(orphTab.locator(".chip")).toHaveText("1");
    await orphTab.click();

    await expect(page.locator(".mg-card.orphan")).toHaveCount(1);
    await expect(page.locator(".mg-card.orphan .badge.warn")).toHaveText("orphaned");
    // an orphan is NOT painted into the body (its position is no longer trusted)
    await expect(page.locator("mark.mg-hl")).toHaveCount(0);

    await page.emulateMedia({ colorScheme: "light" });
    await page.screenshot({ path: "shots/orphan-light.png" });
  } finally {
    fs.rmSync(f, { force: true });
  }
});

test("highlight survives a doc edit (re-anchor heals + repaints on the same words)", async ({ page }) => {
  const f = docFile("e2e-heal");
  fs.writeFileSync(f, "## S\n\nThe deployment window opens on Monday and closes on Friday.\n");
  try {
    await page.goto("/doc/e2e-heal");
    await addComment(page, "opens on Monday", "confirm the window?");
    await expect(page.locator("mark.mg-hl").first()).toBeVisible();

    // Edit the commented block so its content hash (id) changes; the quote survives.
    fs.writeFileSync(f, "## S\n\nNote: the deployment window opens on Monday and closes on Friday.\n");
    await page.reload();

    // The highlight must repaint on the same words (not silently drop, not orphan).
    const hl = page.locator("mark.mg-hl");
    await expect(hl.first()).toBeVisible();
    await expect(hl.first()).toContainText("opens on Monday");
    await page.locator("#mg-toggle").click();
    await expect(page.locator('.mg-tab[data-filter="orphaned"]')).toBeHidden();
  } finally {
    fs.rmSync(f, { force: true });
  }
});

test("resolved highlight recedes (no underline) and reopen restores it", async ({ page }) => {
  const f = docFile("e2e-resolved");
  fs.writeFileSync(f, "## R\n\nThe quarterly report ships on Friday afternoon.\n");
  try {
    await page.goto("/doc/e2e-resolved");
    await addComment(page, "quarterly report", "confirm cadence");
    const card = page.locator(".mg-card").first();
    await card.getByRole("button", { name: "Resolve" }).click();
    // highlight gains the resolved class
    await expect(page.locator("mark.mg-hl.resolved").first()).toBeVisible();
  } finally {
    fs.rmSync(f, { force: true });
  }
});

test("TOC scrollspy tracks the section being read (not just the next heading)", async ({ page }) => {
  const f = docFile("e2e-spy");
  // three tall sections so each fills more than a viewport
  const filler = (w) => Array.from({ length: 18 }, (_, i) => `${w} paragraph ${i + 1} with enough text to add vertical height to the section.`).join("\n\n");
  fs.writeFileSync(
    f,
    `## Alpha\n\n${filler("Alpha")}\n\n## Beta\n\n${filler("Beta")}\n\n## Gamma\n\n${filler("Gamma")}\n`
  );
  try {
    await page.setViewportSize({ width: 1280, height: 860 });
    await page.goto("/doc/e2e-spy");

    // read into the MIDDLE of Beta — the active TOC entry must be Beta, even though
    // we're nowhere near the Gamma heading (the old band-based spy lagged here)
    await page.evaluate(() => {
      const h = document.getElementById("beta");
      window.scrollTo({ top: h.getBoundingClientRect().top + window.scrollY + 200, behavior: "instant" });
    });
    await page.waitForTimeout(150);
    await expect(page.locator(".toc a.active")).toHaveAttribute("href", "#beta");

    // scrolling to the very bottom activates the last section
    await page.evaluate(() => window.scrollTo({ top: 1e7, behavior: "instant" }));
    await page.waitForTimeout(150);
    await expect(page.locator(".toc a.active")).toHaveAttribute("href", "#gamma");
  } finally {
    fs.rmSync(f, { force: true });
  }
});

test("nested docs: the desk shows a collapsible folder tree, collapse persists", async ({ page }) => {
  const dir = path.join(__dirname, "..", "..", "docs", "e2e-grp");
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(path.join(dir, "alpha.md"), "---\ntitle: Group Alpha\n---\n\nFirst grouped doc.\n");
  fs.writeFileSync(path.join(dir, "beta.md"), "---\ntitle: Group Beta\n---\n\nSecond grouped doc.\n");
  try {
    await page.goto("/");
    const folder = page.locator('details.dir[data-dir="e2e-grp"]');
    await expect(folder).toBeVisible();
    // both grouped docs are listed under the folder, open by default
    await expect(folder.locator('a[href="/doc/e2e-grp/alpha"]')).toBeVisible();
    await expect(folder.locator('a[href="/doc/e2e-grp/beta"]')).toBeVisible();

    // collapse it, reload — the folder stays collapsed (localStorage)
    await folder.locator("summary.dir-head").click();
    await expect(folder).not.toHaveAttribute("open", /.*/);
    await page.reload();
    await expect(page.locator('details.dir[data-dir="e2e-grp"]')).not.toHaveAttribute("open", /.*/);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test("nested docs: a comment round-trips on a nested-slug doc", async ({ page }) => {
  const dir = path.join(__dirname, "..", "..", "docs", "e2e-nest");
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(path.join(dir, "deep.md"), "## Deep\n\nThe nested document anchors comments just like a flat one.\n");
  try {
    await page.goto("/doc/e2e-nest/deep");
    await addComment(page, "nested document anchors", "does this work nested?");
    await expect(page.locator("mark.mg-hl").first()).toBeVisible();
    await expect(page.locator('.mg-tab[data-filter="open"] .chip')).toHaveText("1");
    // survives a reload (persisted under the nested slug)
    await page.reload();
    await expect(page.locator("mark.mg-hl").first()).toBeVisible();
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});
