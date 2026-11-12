// The palette's ACTIONS and SKILLS beside GO TO: which actions show when, the command line each
// one runs as the mocked CLI recorded it, Rescan's one refresh line on serve's stdin, and the
// skills with their origin.
import type { Page } from "@playwright/test";

import { launch } from "./fixtures/palette-publish.ts";
import { ACCOUNT } from "./fixtures/skills.ts";
import { expect, test } from "./harness/cli.ts";
import type { MockCli } from "./harness/cli.ts";

const palette = (page: Page) => page.getByRole("dialog");
const group = (page: Page, name: string) =>
  palette(page).getByRole("group", { name });
const option = (page: Page, name: string) =>
  palette(page).getByRole("option").filter({ hasText: name });
const toasts = (page: Page) =>
  page.getByRole("region", { name: /^Notifications/u });

/** Opens the app on the fixture machine, then the palette. */
const openPalette = async (page: Page) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();
  await page.keyboard.press("Control+K");
  await expect(palette(page)).toBeVisible();
};

/** Whether the mocked CLI ran `args` (without --json). */
const ran = (cli: MockCli, args: string[]) =>
  cli.commands.some((l) => l.join(" ") === [...args, "--json"].join(" "));

/** The refresh requests written to serve's stdin so far. */
const refreshes = (cli: MockCli) =>
  cli.serveRequests.filter((line) => {
    const request: unknown = JSON.parse(line);
    return (
      typeof request === "object" &&
      request !== null &&
      "type" in request &&
      request.type === "refresh"
    );
  });

test("the groups read ACTIONS, GO TO, SKILLS", async ({ cli, page }) => {
  launch(cli);
  await openPalette(page);
  const groups = palette(page).getByRole("group");
  await expect(groups).toHaveCount(3);
  await expect(groups.nth(0)).toHaveAccessibleName("ACTIONS");
  await expect(groups.nth(1)).toHaveAccessibleName("GO TO");
  await expect(groups.nth(2)).toHaveAccessibleName("SKILLS");
});

test.describe("ACTIONS", () => {
  test("with an update and skills to publish: Update all and Publish all, with their counts", async ({
    cli,
    page,
  }) => {
    launch(cli);
    await openPalette(page);
    await expect(group(page, "ACTIONS").getByRole("option")).toHaveText([
      /^Update all/u,
      /^Publish all/u,
      "Add skills",
      "Create skill",
      "Check updates",
      "Rescan this machine",
    ]);
    await expect(option(page, "Update all")).toContainText("1 skill");
    await expect(option(page, "Publish all")).toContainText("2 skills");
  });

  test("with no update and nothing to publish: neither Update all nor Publish all", async ({
    cli,
    page,
  }) => {
    launch(cli, "published", { update: false });
    await openPalette(page);
    await expect(group(page, "ACTIONS").getByRole("option")).toHaveText([
      "Add skills",
      "Create skill",
      "Check updates",
      "Rescan this machine",
    ]);
  });

  test("with no account remote: no Publish all", async ({ cli, page }) => {
    launch(cli, "no-account-remote");
    await openPalette(page);
    await expect(option(page, "Update all")).toHaveCount(1);
    await expect(option(page, "Publish all")).toHaveCount(0);
  });

  test("Update all runs skill update --all", async ({ cli, page }) => {
    launch(cli);
    cli.register(["skill", "update", "--all"], {
      events: [{ name: "pdf", state: "current", type: "library_skill" }],
    });
    await openPalette(page);
    await option(page, "Update all").click();
    await expect(palette(page)).toBeHidden();
    await expect(toasts(page)).toContainText("Updated 1 skill");
    expect(ran(cli, ["skill", "update", "--all"])).toBe(true);
  });

  test("Publish all runs a bare skill publish", async ({ cli, page }) => {
    launch(cli);
    cli.register(["skill", "publish"], {
      events: [
        {
          name: "changelog",
          outcome: "pushed",
          source: ACCOUNT,
          type: "publish",
        },
        { name: "notes", outcome: "pushed", source: ACCOUNT, type: "publish" },
      ],
    });
    await openPalette(page);
    await option(page, "Publish all").click();
    await expect(toasts(page)).toContainText("Published 2 skills");
    expect(ran(cli, ["skill", "publish"])).toBe(true);
  });

  test("Check updates runs skill check-updates", async ({ cli, page }) => {
    launch(cli);
    cli.register(["skill", "check-updates"], { events: [] });
    await openPalette(page);
    await option(page, "Check updates").click();
    await expect(toasts(page)).toContainText("All skills are up to date");
    expect(ran(cli, ["skill", "check-updates"])).toBe(true);
  });

  test("Add skills goes to its screen", async ({ cli, page }) => {
    launch(cli);
    await openPalette(page);
    await option(page, "Add skills").click();
    await expect(
      page.getByRole("heading", { level: 1, name: "Add skills" })
    ).toBeVisible();
  });

  test("Create skill opens the Create skill dialog", async ({ cli, page }) => {
    launch(cli);
    await openPalette(page);
    await option(page, "Create skill").click();
    const dialog = page.getByRole("dialog", { name: "Create skill" });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole("textbox", { name: "Name" })).toBeFocused();
  });
});

test.describe("Rescan this machine", () => {
  test("writes one refresh line on serve's stdin and says Rescanned when it is answered", async ({
    cli,
    page,
  }) => {
    launch(cli);
    await openPalette(page);
    const before = refreshes(cli).length;
    await option(page, "Rescan this machine").click();
    await expect.poll(() => refreshes(cli).length).toBe(before + 1);
    const id = cli.requestIds.at(-1) ?? "";
    await cli.serveEmit([
      { ok: true, request_id: id, scan_counter: 1, type: "refresh_complete" },
    ]);
    await expect(toasts(page)).toContainText("Rescanned");
  });

  test("a scan that failed shows its error", async ({ cli, page }) => {
    launch(cli);
    await openPalette(page);
    await option(page, "Rescan this machine").click();
    await expect.poll(() => refreshes(cli).length).toBeGreaterThan(0);
    const id = cli.requestIds.at(-1) ?? "";
    await cli.serveEmit([
      {
        error: "cannot read /home/me/.agents/skills: permission denied",
        ok: false,
        request_id: id,
        type: "refresh_complete",
      },
    ]);
    await expect(toasts(page)).toContainText(
      "Cannot read /home/me/.agents/skills: permission denied."
    );
  });
});

test.describe("SKILLS", () => {
  test("with nothing typed, the first four skills with their origin", async ({
    cli,
    page,
  }) => {
    launch(cli);
    await openPalette(page);
    const rows = group(page, "SKILLS").getByRole("option");
    await expect(rows).toHaveText([
      "changelogyour skill",
      "draftyour skill",
      "notesyour skill",
      "pdfacme/skills",
    ]);
  });

  test("a query filters them, and Enter selects the skill in Skills", async ({
    cli,
    page,
  }) => {
    launch(cli);
    await openPalette(page);
    await page.keyboard.type("rev");
    await expect(palette(page).getByRole("option")).toHaveText([
      "reviewacme/skills",
    ]);
    await page.keyboard.press("Enter");
    await expect(palette(page)).toBeHidden();
    await expect(page).toHaveURL(/#\/skills\/review$/u);
    await expect(
      page
        .getByRole("region", { name: "Skill details" })
        .getByRole("article", { name: "review" })
    ).toBeVisible();
  });
});
