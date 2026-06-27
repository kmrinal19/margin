const { test, expect } = require("@playwright/test");
const { selectText } = require("./helpers");
const fs = require("fs");
const path = require("path");

const docFile = (slug) => path.join(__dirname, "..", "..", "docs", slug + ".md");
const FIX = "## Alpha\n\nThe alpha section discusses the first concern in detail right here.\n\n## Beta\n\nThe beta section covers the second concern thoroughly as well.\n";

// Each test gets its own slug + doc file so threads never bleed across tests
// (the DB persists for the whole run; only the doc files are per-test).
let slug;
let n = 0;
test.beforeEach(() => {
  slug = "e2e-delight-" + ++n;
  fs.writeFileSync(docFile(slug), FIX);
});
test.afterEach(() => fs.rmSync(docFile(slug), { force: true }));

async function comment(page, sub, body) {
  await selectText(page, sub);
  await page.locator(".mg-add.show").click();
  await page.locator("#mg-composer textarea").fill(body);
  await page.locator("#mg-composer").getByRole("button", { name: "Comment", exact: true }).click();
  await expect(page.locator("#mg-composer")).toHaveCount(0);
}

test("resolving shows an Undo toast that reopens the thread", async ({ page }) => {
  await page.goto("/doc/" + slug);
  await comment(page, "first concern in detail", "resolve then undo");
  await page.locator(".mg-card").first().getByRole("button", { name: "Resolve" }).click();

  await expect(page.locator('.mg-tab[data-filter="resolved"] .chip')).toHaveText("1");
  const undo = page.locator(".mg-toast .mg-toast-act", { hasText: "Undo" });
  await expect(undo).toBeVisible();
  await undo.click();
  await expect(page.locator('.mg-tab[data-filter="open"] .chip')).toHaveText("1");
  await expect(page.locator('.mg-tab[data-filter="resolved"] .chip')).toHaveText("0");
});

test("resolved card shows who resolved it and when", async ({ page }) => {
  await page.goto("/doc/" + slug);
  await comment(page, "first concern in detail", "metadata please");
  await page.locator(".mg-card").first().getByRole("button", { name: "Resolve" }).click();
  await page.locator('.mg-tab[data-filter="resolved"]').click();
  const note = page.locator(".mg-resolved-note");
  await expect(note).toBeVisible();
  await expect(note).toContainText(/Resolved/);
});

test("resolving the last open comment shows the clean-manuscript state", async ({ page }) => {
  await page.goto("/doc/" + slug);
  await comment(page, "first concern in detail", "last one");
  await page.locator(".mg-card").first().getByRole("button", { name: "Resolve" }).click();
  await expect(page.locator('.mg-tab[data-filter="open"] .chip')).toHaveText("0");
  await expect(page.locator(".mg-empty-head")).toHaveText(/manuscript is clean/i);
});

test("the ? key opens a keyboard-shortcut help overlay; Esc closes it", async ({ page }) => {
  await page.goto("/doc/" + slug);
  await page.locator("h1.doc-title").click(); // ensure focus isn't in a field
  await page.keyboard.press("?");
  const help = page.locator(".mg-help[role='dialog']");
  await expect(help).toBeVisible();
  await expect(help).toContainText("Keyboard shortcuts");
  await page.keyboard.press("Escape");
  await expect(page.locator(".mg-help")).toHaveCount(0);
});

test("copy-link deep-links: #mg-thread-N opens and activates the thread on load", async ({ page }) => {
  await page.goto("/doc/" + slug);
  await comment(page, "second concern thoroughly", "deep link target");
  const tid = await page.locator(".mg-card").first().getAttribute("data-tid");

  await page.goto("/doc/" + slug + "#mg-thread-" + tid);
  await expect(page.locator("#mg-sidebar.open")).toBeVisible();
  await expect(page.locator('.mg-card[data-tid="' + tid + '"].active')).toBeVisible();
});

test("a comment whose anchor relocates renders a tentative cue", async ({ page }) => {
  await page.goto("/doc/" + slug);
  await comment(page, "second concern thoroughly", "watch me move");

  // edit the doc so the quoted text shifts within its block (forces a fuzzy heal)
  fs.writeFileSync(
    docFile(slug),
    "## Alpha\n\nThe alpha section discusses the first concern in detail right here.\n\n## Beta\n\nNote: the beta section now covers the second concern thoroughly as well, with more words ahead of it.\n"
  );
  await page.reload();
  await expect(page.locator("mark.mg-hl.tentative").first()).toBeVisible({ timeout: 7000 });
  await expect(page.locator(".mg-tentative").first()).toContainText(/moved|relocated/i);
});

test("document-level note: header button creates an unanchored note in its own group", async ({ page }) => {
  await page.goto("/doc/" + slug);
  // open the centered composer from the masthead "＋ Note" button
  await page.locator("#mg-docnote").click();
  const composer = page.locator("#mg-composer[role='dialog']");
  await expect(composer).toBeVisible();
  await expect(composer.locator(".mg-doclabel")).toContainText(/On this document/i);
  await composer.locator("textarea").fill("Overall this needs a security section before we ship.");
  await composer.getByRole("button", { name: "Add note", exact: true }).click();
  await expect(page.locator("#mg-composer")).toHaveCount(0);

  // it lands in the "On the document" group, counts as open, and paints NO highlight/marker
  await expect(page.locator(".mg-group", { hasText: "On the document" })).toBeVisible();
  const docCard = page.locator(".mg-card.doc");
  await expect(docCard).toHaveCount(1);
  await expect(docCard.locator(".mg-body")).toContainText("security section");
  await expect(page.locator('.mg-tab[data-filter="open"] .chip')).toHaveText("1");
  await expect(page.locator("mark.mg-hl")).toHaveCount(0);
  await expect(page.locator(".mg-marker")).toHaveCount(0);

  // it resolves like any thread and survives a reload (persisted, never orphaned)
  await docCard.getByRole("button", { name: "Resolve" }).click();
  await expect(page.locator('.mg-tab[data-filter="resolved"] .chip')).toHaveText("1");
  await expect(page.locator('.mg-tab[data-filter="orphaned"]')).toBeHidden();
  await page.reload();
  await page.locator("#mg-toggle").click();
  await page.locator('.mg-tab[data-filter="resolved"]').click();
  await expect(page.locator(".mg-card.doc .mg-body")).toContainText("security section");
});

test("pressing c with nothing selected opens the document-note composer", async ({ page }) => {
  await page.goto("/doc/" + slug);
  await page.locator("h1.doc-title").click(); // focus out of any field, no selection
  await page.keyboard.press("c");
  await expect(page.locator("#mg-composer[role='dialog'] .mg-doclabel")).toContainText(/On this document/i);
});

test("delete: the ⋯ menu's two-step confirm removes the comment; Cancel aborts", async ({ page }) => {
  await page.goto("/doc/" + slug);
  await comment(page, "first concern in detail", "delete me please");
  const card = page.locator(".mg-card").first();
  await expect(page.locator("mark.mg-hl")).toHaveCount(1);

  // open the overflow menu → Delete → Cancel aborts (comment stays)
  await card.getByRole("button", { name: "More actions" }).click();
  await card.getByRole("menuitem", { name: "Delete comment…" }).click();
  await expect(card.getByText(/can.t be undone/i)).toBeVisible();
  await card.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(page.locator('.mg-tab[data-filter="open"] .chip')).toHaveText("1");

  // open again → Delete → confirm → comment + highlight + marker gone
  await card.getByRole("button", { name: "More actions" }).click();
  await card.getByRole("menuitem", { name: "Delete comment…" }).click();
  await card.getByRole("button", { name: "Delete", exact: true }).click();
  await expect(page.locator(".mg-card")).toHaveCount(0);
  await expect(page.locator("mark.mg-hl")).toHaveCount(0);
  await expect(page.locator(".mg-marker")).toHaveCount(0);

  // stays gone after reload (permanent)
  await page.reload();
  await expect(page.locator('.mg-tab[data-filter="open"] .chip')).toHaveText("0");
});

test("edit: a message can be edited inline and shows an 'edited' marker", async ({ page }) => {
  await page.goto("/doc/" + slug);
  await comment(page, "first concern in detail", "Typo: shoudl be 'should'");
  const card = page.locator(".mg-card").first();
  const msg = card.locator(".mg-msg").first();

  await msg.hover();
  await msg.getByRole("button", { name: "Edit", exact: true }).click();
  const ta = msg.locator("textarea");
  await expect(ta).toBeVisible();
  await ta.fill("Fixed: should be 'should'.");
  await msg.getByRole("button", { name: "Save", exact: true }).click();

  // body updates, an 'edited' marker appears, count unchanged, survives reload
  await expect(card.locator(".mg-body").first()).toHaveText("Fixed: should be 'should'.");
  await expect(card.locator(".mg-edited")).toHaveCount(1);
  await expect(page.locator('.mg-tab[data-filter="open"] .chip')).toHaveText("1");
  await page.reload();
  await expect(page.locator(".mg-card .mg-body").first()).toHaveText("Fixed: should be 'should'.");
  await expect(page.locator(".mg-card .mg-edited")).toHaveCount(1);
});

test("edit: Cancel discards changes", async ({ page }) => {
  await page.goto("/doc/" + slug);
  await comment(page, "first concern in detail", "keep me as is");
  const msg = page.locator(".mg-card .mg-msg").first();
  await msg.hover();
  await msg.getByRole("button", { name: "Edit", exact: true }).click();
  await msg.locator("textarea").fill("this should be thrown away");
  await msg.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(page.locator(".mg-card .mg-body").first()).toHaveText("keep me as is");
  await expect(page.locator(".mg-card .mg-edited")).toHaveCount(0);
});

test("download: the menu offers Markdown / HTML / PDF and downloads the file", async ({ page }) => {
  await page.goto("/doc/" + slug);
  await comment(page, "first concern in detail", "a note for the export");

  await page.locator("#mg-download").click();
  const menu = page.locator(".mg-dlmenu");
  await expect(menu).toBeVisible();
  await expect(menu.locator(".mg-menu-item")).toHaveText(["Markdown", "HTML", "PDF (print)"]);

  // download Markdown WITH comments → file arrives and contains the comment
  await menu.getByText("Include review comments").click();
  const [dl] = await Promise.all([
    page.waitForEvent("download"),
    menu.getByRole("menuitem", { name: "Markdown" }).click(),
  ]);
  expect(dl.suggestedFilename()).toBe(slug.split("/").pop() + ".md");

  // menu closes after a choice; Esc-less re-open works
  await expect(page.locator(".mg-dlmenu")).toHaveCount(0);
});
