// The palette's ACTIONS and SKILLS beside GO TO: which actions show when, the command line each
// one runs as the mocked CLI recorded it, Rescan's one refresh line on serve's stdin, and the
// skills with their origin.
import type { Page } from "@playwright/test";

import { launch } from "./fixtures/palette-publish.ts";
import { HUB } from "./fixtures/skills.ts";
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
  test("with skills to publish: Publish all, with its count", async ({
    cli,
    page,
  }) => {
    launch(cli);
    await openPalette(page);
    await expect(group(page, "ACTIONS").getByRole("option")).toHaveText([
      /^Publish all/u,
      "Add skill",
      "Create skill",
      "Check skills for updates",
      "Rescan this machine",
      "Show activity log",
    ]);
    await expect(option(page, "Publish all")).toContainText("3 skills");
  });

  test("with nothing to publish: no Publish all", async ({ cli, page }) => {
    launch(cli, "synced", { update: false });
    await openPalette(page);
    await expect(group(page, "ACTIONS").getByRole("option")).toHaveText([
      "Add skill",
      "Create skill",
      "Check skills for updates",
      "Rescan this machine",
      "Show activity log",
    ]);
  });

  test("with no hub: no Publish all", async ({ cli, page }) => {
    launch(cli, "no-hub");
    await openPalette(page);
    // Once the skills show, the inventory is in.
    await expect(
      group(page, "SKILLS").getByRole("option").first()
    ).toBeVisible();
    await expect(option(page, "Publish all")).toHaveCount(0);
  });

  test("Publish all runs skill publish with every skill the title bar counts", async ({
    cli,
    page,
  }) => {
    launch(cli);
    const names = ["changelog", "draft", "notes"];
    cli.register(["skill", "publish", ...names], {
      events: names.map((name) => ({
        name,
        outcome: "pushed" as const,
        source: HUB,
        type: "publish" as const,
      })),
    });
    await openPalette(page);
    await option(page, "Publish all").click();
    await expect(toasts(page)).toContainText("Published 3 skills");
    expect(ran(cli, ["skill", "publish", ...names])).toBe(true);
  });

  test("Check skills for updates runs skill check-updates", async ({
    cli,
    page,
  }) => {
    launch(cli);
    cli.register(["skill", "check-updates"], { events: [] });
    await openPalette(page);
    await option(page, "Check skills for updates").click();
    await expect(toasts(page)).toContainText("All skills are up to date");
    expect(ran(cli, ["skill", "check-updates"])).toBe(true);
  });

  test("Add skill opens the Add skill dialog", async ({ cli, page }) => {
    launch(cli);
    await openPalette(page);
    await option(page, "Add skill").click();
    const dialog = page.getByRole("dialog", { name: "Add skill" });
    await expect(dialog).toBeVisible();
    await expect(
      dialog.getByRole("textbox", { name: "Link to skills" })
    ).toBeFocused();
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
      "changeloghub skill",
      "drafthub skill",
      "noteshub skill",
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
