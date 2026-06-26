const { test, expect } = require("@playwright/test");
const { selectText } = require("./helpers");

test("full comment lifecycle: create, reply, resolve, reopen", async ({ page }) => {
  await page.goto("/doc/welcome");
  await expect(page).toHaveTitle(/Welcome to margin/);

  // 1. select text and open the comment pill
  await selectText(page, "leave an inline comment");
  const pill = page.locator(".mg-add.show");
  await expect(pill).toBeVisible();
  await pill.click();

  // 2. composer -> create thread
  const composer = page.locator("#mg-composer");
  await expect(composer).toBeVisible();
  await composer.locator("textarea").fill('Is "anchored" clearer here?');
  await composer.getByRole("button", { name: "Comment", exact: true }).click();

  // 3. highlight painted + sidebar shows the thread
  await expect(page.locator("mark.mg-hl").first()).toBeVisible();
  await expect(page.locator("#mg-sidebar.open")).toBeVisible();
  const card = page.locator(".mg-card").first();
  await expect(card.locator(".mg-body")).toContainText("anchored");
  await expect(page.locator('.mg-tab[data-filter="open"] .chip')).toHaveText("1");

  // 4. reply
  await card.locator("textarea").fill("Agreed, updating.");
  await card.getByRole("button", { name: "Reply", exact: true }).click();
  await expect(page.locator(".mg-card .mg-msg")).toHaveCount(2);

  // 5. resolve -> leaves the open tab, appears under resolved
  await page.locator(".mg-card").first().getByRole("button", { name: "Resolve" }).click();
  await expect(page.locator('.mg-tab[data-filter="open"] .chip')).toHaveText("0");
  await page.locator('.mg-tab[data-filter="resolved"]').click();
  await expect(page.locator(".mg-card .badge.good")).toHaveText("resolved");

  // 6. reopen
  await page.locator(".mg-card").first().getByRole("button", { name: "Reopen" }).click();
  await page.locator('.mg-tab[data-filter="open"]').click();
  await expect(page.locator(".mg-card")).toHaveCount(1);
});

test("gutter marker activates its thread", async ({ page }) => {
  await page.goto("/doc/welcome");
  // create a comment so a marker exists
  await selectText(page, "single static binary");
  await page.locator(".mg-add.show").click();
  await page.locator("#mg-composer textarea").fill("Worth emphasising.");
  await page.locator("#mg-composer").getByRole("button", { name: "Comment", exact: true }).click();
  await expect(page.locator("mark.mg-hl").first()).toBeVisible();

  // close the sidebar deterministically and wait for it to settle…
  await page.locator("#mg-toggle").click();
  await expect(page.locator("#mg-sidebar.open")).toHaveCount(0);
  // …then a gutter marker reopens + activates its thread
  const marker = page.locator(".mg-marker").first();
  await expect(marker).toBeVisible();
  await marker.scrollIntoViewIfNeeded();
  await marker.click();
  await expect(page.locator("#mg-sidebar.open")).toBeVisible();
});

test("comment body is escaped (no XSS)", async ({ page }) => {
  let dialogFired = false;
  page.on("dialog", (d) => {
    dialogFired = true;
    d.dismiss();
  });
  await page.goto("/doc/welcome");
  await selectText(page, "Local and offline");
  await page.locator(".mg-add.show").click();
  await page.locator("#mg-composer textarea").fill("<img src=x onerror=alert(1)>");
  await page.locator("#mg-composer").getByRole("button", { name: "Comment", exact: true }).click();
  // the payload is shown as literal text in its card…
  const body = page.locator(".mg-card .mg-body").filter({ hasText: "onerror" });
  await expect(body).toBeVisible();
  // …and never as a real <img> element (would be XSS)
  await expect(page.locator(".mg-card .mg-body img")).toHaveCount(0);
  expect(dialogFired).toBe(false);
});
