import { expect, test } from "./harness/cli.ts";

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

test("says when nothing matches", async ({ page }) => {
  await page.keyboard.press("Control+K");
  await page.keyboard.type("zzz");
  await expect(page.getByText("No matches")).toBeVisible();
  await expect(page.getByRole("option")).toHaveCount(0);
});

test("Check for agentx updates runs the check and says what it found", async ({
  cli,
  page,
}) => {
  cli.register(["update", "--check"], {
    events: [
      {
        channel: "stable",
        current_version: "1.2.0",
        path: "/Applications/agentx.app/Contents/MacOS/agentx",
        state: "available",
        type: "update",
        version: "1.3.0",
      },
      { ok: true, type: "result" },
    ],
  });
  await page.keyboard.press("Control+K");
  await page.keyboard.type("agentx up");
  await expect(page.getByRole("option")).toHaveText([
    "Check for agentx updates",
  ]);
  await page.keyboard.press("Enter");
  await expect(
    page
      .getByRole("region", { name: /^Notifications/u })
      .getByText("agentx 1.3.0 is available, you have 1.2.0")
  ).toBeVisible();
  expect(cli.commands).toContainEqual(["update", "--check", "--json"]);
  // A check changes nothing: no refresh follows it.
  expect(cli.serveRequests).toEqual([]);
});
