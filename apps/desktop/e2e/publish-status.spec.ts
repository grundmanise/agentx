// The publish status at the right of the title bar: its three states from the inventory and the
// Sources slice, Publish all from "{n} to publish", and the check of what your other machines
// published from "Published". Every click's command line is asserted as the mocked CLI recorded it.
import type { Page } from "@playwright/test";

import {
  draftNotPublished,
  launch,
  notesFromAnotherMachine,
  pdfUpdate,
} from "./fixtures/palette-publish.ts";
import { ACCOUNT } from "./fixtures/skills.ts";
import { expect, test } from "./harness/cli.ts";
import type { InlineEvent, MockCli } from "./harness/cli.ts";

const status = (page: Page) =>
  page.getByRole("banner").getByRole("button", { name: /publish|remote/iu });
const toasts = (page: Page) =>
  page.getByRole("region", { name: /^Notifications/u });
const pane = (page: Page, name: string) =>
  page
    .getByRole("region", { name: "Skill details" })
    .getByRole("article", { name });

/** Answers every refresh request so far, as serve does after its scan. */
const answerRefreshes = async (cli: MockCli) => {
  await cli.serveEmit(
    cli.requestIds.map((id) => ({
      ok: true,
      request_id: id,
      scan_counter: 1,
      type: "refresh_complete",
    }))
  );
};

/** Whether the mocked CLI ran `args` (without --json). */
const ran = (cli: MockCli, args: string[]) =>
  cli.commands.some((l) => l.join(" ") === [...args, "--json"].join(" "));

const progress = (current: number, subject: string): InlineEvent => ({
  current,
  phase: "publish",
  subject,
  total: 2,
  type: "progress",
});

const published = (name: string, outcome: "pushed" | "moved"): InlineEvent => ({
  name,
  outcome,
  source: ACCOUNT,
  type: "publish",
});

test.describe("states", () => {
  test("two of your skills edited since they were published: 2 to publish", async ({
    cli,
    page,
  }) => {
    launch(cli);
    await page.goto("/");
    await expect(status(page)).toHaveText("2 to publish");
  });

  test("nothing to publish: Published", async ({ cli, page }) => {
    launch(cli, "published");
    await page.goto("/");
    await expect(status(page)).toHaveText("Published");
  });

  test("no account remote: says so, and a click goes to Settings, Account remote", async ({
    cli,
    page,
  }) => {
    launch(cli, "no-account-remote");
    await page.goto("/");
    await expect(status(page)).toHaveText("No account remote");
    await status(page).click();
    await expect(
      page.getByRole("heading", { level: 1, name: "Settings" })
    ).toBeVisible();
    await expect(
      page.getByRole("region", { name: "Account remote" })
    ).toBeInViewport();
  });

  test("is hidden while the startup layer shows", async ({ cli, page }) => {
    launch(cli);
    await page.goto("/");
    await expect(status(page)).toHaveText("2 to publish");
    await page.addScriptTag({
      type: "module",
      url: "/e2e/harness/run-checks.ts",
    });
    await expect(
      page.getByRole("region", { name: "Startup checks" })
    ).toBeVisible();
    await expect(status(page)).toHaveCount(0);
  });
});

test.describe("publish", () => {
  test("a click runs a bare skill publish, shows its progress, then names the skill another machine changed, with Update and the skills not published", async ({
    cli,
    page,
  }) => {
    launch(cli);
    const run = cli.register(["skill", "publish"], {
      events: [
        progress(1, "changelog"),
        published("changelog", "pushed"),
        progress(2, "notes"),
        published("notes", "moved"),
        {
          code: "refused",
          message:
            "1 of 2 skills could not be published: the account remote holds changes to notes that this machine lacks, published from another machine",
          type: "error",
        },
      ],
      exitCode: 6,
      holdAfter: 1,
      logs: [draftNotPublished],
    });
    cli.register(["skill", "update", "notes"], {
      events: [{ name: "notes", state: "current", type: "library_skill" }],
    });
    await page.goto("/");
    await status(page).click();
    await run.started;
    await expect(status(page)).toHaveText("Publishing 1 of 2");
    await expect(status(page)).toHaveAttribute("aria-busy", "true");
    run.release();

    const toast = toasts(page);
    await expect(toast).toContainText(
      "notes changed somewhere else first. Update it, then publish. · 1 not published yet"
    );
    expect(ran(cli, ["skill", "publish"])).toBe(true);
    await answerRefreshes(cli);
    await expect(status(page)).toHaveText("2 to publish");

    await toast.getByRole("button", { name: "Update" }).click();
    await expect.poll(() => ran(cli, ["skill", "update", "notes"])).toBe(true);
  });

  test("every skill pushed: Published {n} skills, and Show opens the first one not published", async ({
    cli,
    page,
  }) => {
    launch(cli);
    cli.register(["skill", "publish"], {
      events: [
        progress(1, "changelog"),
        published("changelog", "pushed"),
        progress(2, "notes"),
        published("notes", "pushed"),
      ],
      logs: [draftNotPublished],
    });
    await page.goto("/");
    await status(page).click();
    const toast = toasts(page);
    await expect(toast).toContainText(
      "Published 2 skills · 1 not published yet"
    );
    await toast.getByRole("button", { name: "Show" }).click();
    await expect(page).toHaveURL(/#\/skills\/draft$/u);
    await expect(pane(page, "draft")).toBeVisible();
  });
});

test.describe("publish, nothing sent", () => {
  test("every skill already matches: no skills were published, and why", async ({
    cli,
    page,
  }) => {
    launch(cli);
    cli.register(["skill", "publish"], {
      events: [
        progress(1, "changelog"),
        {
          name: "changelog",
          outcome: "up to date",
          source: ACCOUNT,
          type: "publish",
        },
        progress(2, "notes"),
        {
          name: "notes",
          outcome: "up to date",
          source: ACCOUNT,
          type: "publish",
        },
      ],
      logs: [draftNotPublished],
    });
    await page.goto("/");
    await status(page).click();
    const toast = toasts(page);
    await expect(toast).toContainText(
      "No skills were published. They already match the published versions. · 1 not published yet"
    );
    await expect(toast.getByRole("button", { name: "Show" })).toBeVisible();
    expect(ran(cli, ["skill", "publish"])).toBe(true);
  });
});

test.describe("check", () => {
  test("Published runs skill check-updates and counts what your other machines published, with Review", async ({
    cli,
    page,
  }) => {
    launch(cli, "published");
    const run = cli.register(["skill", "check-updates"], {
      events: [notesFromAnotherMachine, pdfUpdate],
      holdAfter: 0,
    });
    await page.goto("/");
    await status(page).click();
    await run.started;
    await expect(status(page)).toHaveAttribute("aria-busy", "true");
    run.release();

    const toast = toasts(page);
    await expect(toast).toContainText(
      "1 skill changed on your other machines, 1 update from sources"
    );
    expect(ran(cli, ["skill", "check-updates"])).toBe(true);
    await toast.getByRole("button", { name: "Review" }).click();
    await expect(page).toHaveURL(/#\/skills\/notes$/u);
    await expect(pane(page, "notes")).toBeVisible();
  });

  test("with nothing from another machine: Nothing new from your other machines", async ({
    cli,
    page,
  }) => {
    launch(cli, "published");
    cli.register(["skill", "check-updates"], { events: [pdfUpdate] });
    await page.goto("/");
    await status(page).click();
    await expect(toasts(page)).toContainText(
      "Nothing new from your other machines"
    );
    await expect(status(page)).not.toHaveAttribute("aria-busy", "true");
    expect(ran(cli, ["skill", "check-updates"])).toBe(true);
  });
});
