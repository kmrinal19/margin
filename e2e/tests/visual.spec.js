const { test } = require("@playwright/test");
const { selectText } = require("./helpers");
const fs = require("fs");

// Captures reference screenshots for visual review. Runs after comments.spec
// (alphabetical), so the welcome doc already has a few threads — a realistic shot.
test("capture screenshots (light + dark + index)", async ({ page }) => {
  fs.mkdirSync("shots", { recursive: true });
  await page.setViewportSize({ width: 1440, height: 1024 });

  await page.goto("/doc/welcome");
  await selectText(page, "leave an inline comment");
  await page.locator(".mg-add.show").click();
  await page.locator("#mg-composer textarea").fill("Could we say 'anchored comment' for precision?");
  await page.locator("#mg-composer").getByRole("button", { name: "Comment", exact: true }).click();
  await page.locator("mark.mg-hl").first().waitFor();
  await page.waitForTimeout(400); // let the sidebar settle

  await page.emulateMedia({ colorScheme: "light" });
  await page.screenshot({ path: "shots/doc-light.png" });

  await page.emulateMedia({ colorScheme: "dark" });
  await page.screenshot({ path: "shots/doc-dark.png" });

  await page.emulateMedia({ colorScheme: "light" });
  await page.goto("/");
  await page.screenshot({ path: "shots/index-light.png" });
});
