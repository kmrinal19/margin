const { test, expect } = require("@playwright/test");
const { selectText } = require("./helpers");
const fs = require("fs");
const path = require("path");

const docFile = (slug) => path.join(__dirname, "..", "..", "docs", slug + ".md");
const TWO = "## Alpha\n\nThe alpha section discusses the first concern in detail right here.\n\n## Beta\n\nThe beta section covers the second concern thoroughly as well.\n";
const ONE_LINE = "## Solo\n\nQuick brown fox jumps over.\n";
// tall enough that the target paragraph can be scrolled right up under the header
const TALL =
  "## Notes\n\nThe beta section covers the second concern thoroughly as well.\n\n" +
  Array.from({ length: 60 }, (_, i) => "Filler paragraph " + (i + 1) + " adds vertical space so the page can scroll.").join("\n\n") +
  "\n";

async function comment(page, sub, body) {
  await selectText(page, sub);
  await page.locator(".mg-add.show").click();
  await page.locator("#mg-composer textarea").fill(body);
  await page.locator("#mg-composer").getByRole("button", { name: "Comment", exact: true }).click();
  await expect(page.locator("#mg-composer")).toHaveCount(0);
}

// ── modal overlay on narrow viewports ─────────────────────────────────
test.describe("narrow viewport", () => {
  test.beforeEach(() => fs.writeFileSync(docFile("e2e-ux-modal"), TWO));
  test.afterEach(() => fs.rmSync(docFile("e2e-ux-modal"), { force: true }));

  test("the sidebar is a modal overlay: scrim, scroll-lock, aria-modal, Escape closes", async ({ page }) => {
    await page.setViewportSize({ width: 900, height: 800 }); // below NARROW (1100)
    await page.goto("/doc/e2e-ux-modal");
    await comment(page, "first concern in detail", "modal please");

    await expect(page.locator("#mg-sidebar.open")).toBeVisible();
    await expect(page.locator("#mg-sidebar")).toHaveAttribute("aria-modal", "true");
    await expect(page.locator(".mg-scrim.show")).toBeVisible();
    await expect(page.locator(".mg-sb-close")).toBeVisible();
    // the background is inert and the page itself can't scroll
    await expect(page.locator("#mg-article")).toHaveAttribute("inert", "");
    expect(await page.evaluate(() => getComputedStyle(document.body).overflow)).toBe("hidden");

    await page.keyboard.press("Escape");
    await expect(page.locator("#mg-sidebar.open")).toHaveCount(0);
    expect(await page.evaluate(() => document.body.classList.contains("sidebar-modal"))).toBe(false);
    expect(await page.evaluate(() => getComputedStyle(document.body).overflow)).not.toBe("hidden");
    await expect(page.locator("#mg-article")).not.toHaveAttribute("inert", "");
  });

  test("the close button dismisses the modal sidebar", async ({ page }) => {
    await page.setViewportSize({ width: 900, height: 800 });
    await page.goto("/doc/e2e-ux-modal");
    await comment(page, "first concern in detail", "close me");
    await expect(page.locator("#mg-sidebar.open")).toBeVisible();
    await page.locator(".mg-sb-close").click();
    await expect(page.locator("#mg-sidebar.open")).toHaveCount(0);
  });
});

// ── reply drafts survive a sidebar rebuild ────────────────────────────
test.describe("reply drafts", () => {
  test.beforeEach(() => fs.writeFileSync(docFile("e2e-ux-draft"), TWO));
  test.afterEach(() => fs.rmSync(docFile("e2e-ux-draft"), { force: true }));

  test("a half-written reply is restored after the sidebar re-renders", async ({ page }) => {
    await page.goto("/doc/e2e-ux-draft");
    await comment(page, "first concern in detail", "seed comment");

    const firstCard = page.locator(".mg-card", { hasText: "first concern in detail" });
    await firstCard.locator("textarea").fill("half-written reply");

    // creating a second comment forces a full sidebar rebuild
    await comment(page, "second concern thoroughly", "second seed");

    await expect(firstCard.locator("textarea")).toHaveValue("half-written reply");
  });
});

// ── the pill flips below the line when there's no room above ───────────
test.describe("comment pill", () => {
  test.beforeEach(() => fs.writeFileSync(docFile("e2e-ux-pill"), TALL));
  test.afterEach(() => fs.rmSync(docFile("e2e-ux-pill"), { force: true }));

  test("flips below the selection when it sits under the sticky header", async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 860 });
    await page.goto("/doc/e2e-ux-pill");
    // scroll the target paragraph right up against the header so there's no room above
    await page.evaluate(() => {
      const p = [...document.querySelectorAll("article p")].find((x) =>
        x.textContent.includes("second concern thoroughly")
      );
      window.scrollTo({ top: p.getBoundingClientRect().top + window.scrollY - 64, left: 0, behavior: "instant" });
    });
    await page.waitForTimeout(60); // let the scroll settle before measuring
    await selectText(page, "second concern thoroughly");
    const pill = page.locator(".mg-add.show");
    await expect(pill).toBeVisible();
    await expect(pill).toHaveClass(/below/);
  });
});

// ── several comments on one line collapse to a single counted marker ───
test.describe("stacked gutter markers", () => {
  test.beforeEach(() => fs.writeFileSync(docFile("e2e-ux-stack"), ONE_LINE));
  test.afterEach(() => fs.rmSync(docFile("e2e-ux-stack"), { force: true }));

  test("two comments on the same line show one marker with a count, and it cycles", async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.goto("/doc/e2e-ux-stack");
    await comment(page, "Quick brown", "first note");
    await comment(page, "jumps over", "second note");

    const multi = page.locator(".mg-marker.multi");
    await expect(multi).toHaveCount(1);
    await expect(multi).toHaveText("2");
    await expect(page.locator(".mg-marker")).toHaveCount(1); // no overlapping glyphs

    // clicking cycles through the stacked threads (a different card each click).
    // the open panel covers the right-gutter marker, so close it before each click.
    const closePanel = async () => {
      await page.locator(".mg-sb-close").click();
      await expect(page.locator("#mg-sidebar.open")).toHaveCount(0);
    };
    await closePanel(); // creating the comments left the panel open
    await multi.click();
    const first = await page.locator(".mg-card.active").getAttribute("data-tid");
    await closePanel();
    await multi.click();
    const second = await page.locator(".mg-card.active").getAttribute("data-tid");
    expect(second).not.toBe(first);
  });
});
