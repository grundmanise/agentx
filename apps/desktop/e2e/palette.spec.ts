import { expect, test } from "@playwright/test";

const placeholder = "Search skills or run a command";

test.beforeEach(async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Inbox" })).toBeVisible();
});

for (const shortcut of ["Meta+K", "Control+K"]) {
  test(`${shortcut} toggles the palette, which opens empty and focused`, async ({
    page,
  }) => {
    const input = page.getByPlaceholder(placeholder);
    await page.keyboard.press(shortcut);
    await expect(input).toBeFocused();
    await expect(input).toHaveValue("");

    await page.keyboard.type("add");
    await page.keyboard.press(shortcut);
    await expect(page.getByRole("dialog")).toBeHidden();

    await page.keyboard.press(shortcut);
    await expect(input).toBeFocused();
    await expect(input).toHaveValue("");
  });
}

test("opens from the command field and closes with Escape", async ({
  page,
}) => {
  await page.getByRole("button", { name: placeholder }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).toBeHidden();
});

test("filters by substring and runs the active row with Enter", async ({
  page,
}) => {
  await page.keyboard.press("Control+K");
  await page.keyboard.type("SERV");
  await expect(page.getByRole("option")).toHaveText(["MCP servers"]);
  await page.keyboard.press("Enter");
  await expect(page.getByRole("dialog")).toBeHidden();
  await expect(
    page.getByRole("heading", { name: "MCP servers" })
  ).toBeVisible();
});

test("turns the selected row's icon to the accent", async ({ page }) => {
  await page.keyboard.press("Control+K");
  const icon = (name: string) =>
    page.getByRole("option", { name }).locator("svg");
  // palette.icon-active on the selected row, palette.icon on the others.
  const active = "rgb(212, 255, 79)";
  const rest = "rgb(134, 134, 142)";
  await expect(icon("Inbox")).toHaveCSS("stroke", active);
  await expect(icon("Skills")).toHaveCSS("stroke", rest);
  await page.keyboard.press("ArrowDown");
  await expect(icon("Inbox")).toHaveCSS("stroke", rest);
  await expect(icon("Skills")).toHaveCSS("stroke", active);
});

test("says when nothing matches", async ({ page }) => {
  await page.keyboard.press("Control+K");
  await page.keyboard.type("zzz");
  await expect(page.getByText("No matches")).toBeVisible();
  await expect(page.getByRole("option")).toHaveCount(0);
});
