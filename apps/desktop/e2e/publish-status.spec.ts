// The sync status at the right of the title bar: its states from the inventory, the sources it
// lists and what its library says another machine published, the popover it opens with what
// there is to publish and what is incoming, Publish all from that popover, and when the hub was
// last checked. Every click's command line is asserted as the mocked CLI recorded it.
import type { Page } from "@playwright/test";

import type { PublishOutcome } from "#/cli/events.gen.ts";

import {
  fromAnotherMachine,
  hub,
  launch,
  sources,
  syncedLibrary,
} from "./fixtures/palette-publish.ts";
import { HUB, configurations } from "./fixtures/skills.ts";
import { expect, test } from "./harness/cli.ts";
import type { InlineEvent, MockCli } from "./harness/cli.ts";

const status = (page: Page) =>
  page
    .getByRole("banner")
    .getByRole("button", { name: /publish|sync|incoming/iu });
const popover = (page: Page) => page.getByRole("dialog", { name: "Sync" });
const group = (page: Page, name: string) =>
  popover(page).getByRole("region", { name });
const toasts = (page: Page) =>
  page.getByRole("region", { name: /^Notifications/u });
const pane = (page: Page, name: string) =>
  page
    .getByRole("region", { name: "Skill details" })
    .getByRole("article", { name });

/** A hub whose short form is longer than the popover's least width leaves room for. */
const LONG_REMOTE =
  "git@github.com:example-org/a-rather-long-skill-remote-name.git";

/** Every skill to publish on the fixture machine, as Publish all names them. */
const OUTGOING = ["changelog", "draft", "notes"];

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

/** Opens the popover and runs its Publish all. */
const publishAll = async (page: Page) => {
  await status(page).click();
  await popover(page).getByRole("button", { name: "Publish all" }).click();
};

const progress = (current: number, subject: string): InlineEvent => ({
  current,
  phase: "publish",
  subject,
  total: 3,
  type: "progress",
});

const published = (name: string, outcome: PublishOutcome): InlineEvent => ({
  name,
  outcome,
  source: HUB,
  type: "publish",
});

test.describe("states", () => {
  test("edited and new skills: 3 to publish, its tooltip counts each kind", async ({
    cli,
    page,
  }) => {
    launch(cli);
    await page.goto("/");
    await expect(status(page)).toHaveText("3 to publish");
    await status(page).hover();
    await expect(page.getByRole("tooltip")).toHaveText("2 edited · 1 new");
  });

  test("changed on another machine: 1 incoming, which a screen reader is told", async ({
    cli,
    page,
  }) => {
    launch(cli, "synced", { incoming: true });
    await page.goto("/");
    await expect(status(page)).toHaveText("1 incoming");
    // The Skills screen may still show its own status while it reads the first skill's files.
    await expect(
      page.getByRole("status").filter({ hasText: "incoming" })
    ).toHaveText("1 incoming");
  });

  test("what another machine published to a skill of another remote is not incoming", async ({
    cli,
    page,
  }) => {
    launch(cli, "synced");
    await page.goto("/");
    await expect(status(page)).toHaveText("Synced");
    await cli.serveEmit([
      {
        configurations,
        library: syncedLibrary.map((entry) =>
          entry.name === "notes"
            ? {
                ...entry,
                ...fromAnotherMachine,
                source: "https://github.com/me/old-skills.git",
              }
            : entry
        ),
        scan_counter: 2,
        sources,
        type: "inventory",
      },
    ]);
    await expect(status(page)).toHaveText("Synced");
  });

  test("both: 3 to publish · 1 incoming", async ({ cli, page }) => {
    launch(cli, "edited", { incoming: true });
    await page.goto("/");
    await expect(status(page)).toHaveText("3 to publish · 1 incoming");
  });

  test("nothing pending: Synced", async ({ cli, page }) => {
    launch(cli, "synced");
    await page.goto("/");
    await expect(status(page)).toHaveText("Synced");
  });

  test("no hub: Set up sync, and a click goes to Settings, Hub", async ({
    cli,
    page,
  }) => {
    launch(cli, "no-hub");
    await page.goto("/");
    await expect(status(page)).toHaveText("Set up sync");
    await status(page).click();
    await expect(
      page.getByRole("heading", { level: 1, name: "Settings" })
    ).toBeVisible();
    await expect(page.getByRole("region", { name: "Hub" })).toBeInViewport();
  });

  test("is hidden while the startup layer shows", async ({ cli, page }) => {
    launch(cli);
    await page.goto("/");
    await expect(status(page)).toHaveText("3 to publish");
    // A store error after launch runs the checks again, and a failed one shows the layer.
    cli.register(["doctor"], {
      events: [
        {
          check: "store",
          detail: "/home/me/.agentx/store.git: not a git repository",
          status: "fail",
          type: "doctor",
        },
      ],
      exitCode: 8,
    });
    await cli.serveEmit([
      {
        code: "store",
        message: "the store is not a git repository",
        type: "error",
      },
    ]);
    await expect(
      page.getByRole("region", { name: "Startup checks" })
    ).toBeVisible();
    await expect(status(page)).toHaveCount(0);
  });
});

test.describe("popover", () => {
  test("lists what to publish and what is incoming; a row opens the skill and the popover stays open", async ({
    cli,
    page,
  }) => {
    launch(cli, "edited", { incoming: true });
    await page.goto("/");
    await status(page).click();
    await expect(group(page, "To publish").getByRole("listitem")).toHaveText([
      /^changelog\s*edited$/u,
      /^draft\s*new$/u,
      /^notes\s*edited$/u,
    ]);
    await expect(
      group(page, "To publish").getByRole("button", { name: "Publish all" })
    ).toBeVisible();
    await expect(popover(page)).toContainText("me/hub");
    await expect(popover(page)).toContainText("Not checked yet");

    await group(page, "To publish")
      .getByRole("button", { name: "changelog" })
      .click();
    await expect(page).toHaveURL(/#\/skills\/changelog$/u);
    await expect(pane(page, "changelog")).toBeVisible();
    await expect(popover(page)).toBeVisible();

    await group(page, "Incoming")
      .getByRole("button", { name: "notes" })
      .click();
    await expect(page).toHaveURL(/#\/skills\/notes$/u);
    await expect(pane(page, "notes")).toBeVisible();
    await expect(popover(page)).toBeVisible();
  });

  test("shows the hub in full, Publish all at the right", async ({
    cli,
    page,
  }) => {
    launch(cli, "edited", { hub: { ...hub, url: LONG_REMOTE } });
    await page.goto("/");
    await expect(status(page)).toHaveText("3 to publish");
    // Checked a while ago, so the time takes the most room it can.
    await cli.serveEmit([
      {
        checked_at: new Date(Date.now() - 3_000_000).toISOString(),
        ok: true,
        type: "update_check",
      },
    ]);
    await status(page).click();
    const remote = popover(page).getByText(
      "example-org/a-rather-long-skill-remote-name",
      { exact: true }
    );
    await expect(remote).toBeVisible();
    const clipped = await remote.evaluate(
      (el) => el.scrollWidth > el.clientWidth
    );
    expect(clipped).toBe(false);
    // Once its open animation ends, so the sizes are its own.
    await popover(page).evaluate(async (el) => {
      await Promise.all(
        el.getAnimations().map(async (a) => {
          await a.finished;
        })
      );
    });
    const button = await popover(page)
      .getByRole("button", { name: "Publish all" })
      .boundingBox();
    const checked = await popover(page)
      .getByText(/^Checked /u)
      .boundingBox();
    // The time keeps its place at the right of the remote, on the same line.
    const remoteBox = await remote.boundingBox();
    expect(remoteBox?.height ?? 0).toBeCloseTo(checked?.height ?? 0, -1);
    // Its right edge lines up with the footer's, which ends at the popover's right.
    expect((button?.x ?? 0) + (button?.width ?? 0)).toBeCloseTo(
      (checked?.x ?? 0) + (checked?.width ?? 0),
      0
    );
  });

  test("synced: says so, with when serve last checked; a failed check leaves that time", async ({
    cli,
    page,
  }) => {
    launch(cli, "synced");
    await page.goto("/");
    await expect(status(page)).toHaveText("Synced");
    // Five and a half minutes ago: the status reads the clock once a minute.
    const checkedAt = new Date(Date.now() - 330_000).toISOString();
    await cli.serveEmit([
      {
        checked_at: checkedAt,
        ok: true,
        type: "update_check",
      },
      {
        checked_at: new Date().toISOString(),
        ok: false,
        type: "update_check",
      },
    ]);
    await status(page).hover();
    await expect(page.getByRole("tooltip")).toHaveText(
      "Synced with your hub · checked 5 minutes ago"
    );
    await status(page).click();
    await expect(popover(page)).toContainText("Everything is synced");
    await expect(popover(page)).toContainText("Checked 5 minutes ago");
  });
});

test.describe("publish", () => {
  test("Publish all runs one skill publish naming every skill to publish, shows its progress, then names the skill with a newer version on the hub, with Update", async ({
    cli,
    page,
  }) => {
    launch(cli);
    const run = cli.register(["skill", "publish", ...OUTGOING], {
      events: [
        progress(1, "changelog"),
        published("changelog", "pushed"),
        progress(2, "draft"),
        published("draft", "pushed"),
        progress(3, "notes"),
        published("notes", "moved"),
        {
          code: "refused",
          message:
            "1 of 3 skills could not be published: the hub holds changes to notes that this machine lacks, published from another machine",
          type: "error",
        },
      ],
      exitCode: 6,
      holdAfter: 1,
    });
    cli.register(["skill", "update", "notes"], {
      events: [{ name: "notes", state: "current", type: "library_skill" }],
    });
    await page.goto("/");
    await publishAll(page);
    await run.started;
    await expect(status(page)).toHaveText("Publishing 1 of 3");
    await expect(status(page)).toHaveAttribute("aria-busy", "true");
    // Its progress runs on the pill's rim.
    await expect(status(page).locator("canvas[data-progress]")).toBeAttached();
    run.release();

    const toast = toasts(page);
    await expect(toast).toContainText(
      "There is a newer version of notes skill on the hub. Update it first, then publish your changes."
    );
    expect(ran(cli, ["skill", "publish", ...OUTGOING])).toBe(true);
    // Until the inventory that follows arrives, the pill keeps the last count.
    await expect(status(page)).toHaveText("Publishing 3 of 3");
    await answerRefreshes(cli);
    await expect(status(page)).toHaveText("3 to publish");

    await toast.getByRole("button", { name: "Update" }).click();
    await expect.poll(() => ran(cli, ["skill", "update", "notes"])).toBe(true);
  });

  test("every skill pushed: Published {n} skills", async ({ cli, page }) => {
    launch(cli);
    cli.register(["skill", "publish", ...OUTGOING], {
      events: OUTGOING.map((name) => published(name, "pushed")),
    });
    await page.goto("/");
    await publishAll(page);
    await expect(toasts(page)).toContainText("Published 3 skills");
  });

  test("every skill already matches: no skills were published, and why", async ({
    cli,
    page,
  }) => {
    launch(cli);
    cli.register(["skill", "publish", ...OUTGOING], {
      events: OUTGOING.map((name) => published(name, "up to date")),
    });
    await page.goto("/");
    await publishAll(page);
    await expect(toasts(page)).toContainText(
      "No skills were published. They already match the published versions."
    );
  });
});

test.describe("publish, refused: the hint in the app's words", () => {
  const cases: {
    name: string;
    outcomes: ["changelog" | "notes", "declined" | "denied"][];
    exitCode: number;
    message: string;
    hint: string;
    shown: string;
  }[] = [
    {
      exitCode: 6,
      hint: "once the hub takes it, run 'agentx skill publish changelog draft notes' again",
      message: "the hub declined skills/notes: protected branch",
      name: "declined",
      outcomes: [["notes", "declined"]],
      shown:
        "The hub declined skills/notes: protected branch. Publish again once the hub takes it.",
    },
    {
      exitCode: 3,
      hint: "check that the git credential helper (git config credential.helper) or SSH key you use for it may push there, then run 'agentx skill publish changelog draft notes' again",
      message: `the hub ${HUB} did not let this machine push skills/notes: permission denied`,
      name: "denied",
      outcomes: [["notes", "denied"]],
      shown: `The hub ${HUB} did not let this machine push skills/notes: permission denied. Check that the git credential helper (git config credential.helper) or SSH key you use for it may push there, then publish again.`,
    },
    {
      exitCode: 6,
      hint: "run 'agentx skill list' to see the state of each skill, then publish the rest one at a time",
      message:
        "2 of 3 skills could not be published: the hub declined skills/changelog: protected branch; notes: the hub did not let this machine push skills/notes: permission denied",
      name: "several causes",
      outcomes: [
        ["changelog", "declined"],
        ["notes", "denied"],
      ],
      shown:
        "2 of 3 skills could not be published: the hub declined skills/changelog: protected branch; notes: the hub did not let this machine push skills/notes: permission denied. See the state of each skill in Skills, then publish the rest one at a time.",
    },
    {
      exitCode: 3,
      hint: "check your network; the edits were recorded, so run 'agentx skill publish changelog draft notes' again once the hub can be reached",
      message: `cannot reach ${HUB}: connection refused`,
      name: "the hub cannot be reached",
      outcomes: [],
      shown: `Cannot reach ${HUB}: connection refused. Check your network; the edits were recorded, so publish again once the hub can be reached.`,
    },
  ];
  for (const c of cases) {
    test(c.name, async ({ cli, page }) => {
      launch(cli);
      cli.register(["skill", "publish", ...OUTGOING], {
        events: [
          ...c.outcomes.map(([name, outcome]) => published(name, outcome)),
          {
            code: c.exitCode === 3 ? "source" : "refused",
            hint: c.hint,
            message: c.message,
            type: "error",
          },
        ],
        exitCode: c.exitCode,
      });
      await page.goto("/");
      await publishAll(page);
      const toast = toasts(page).getByRole("listitem");
      await expect(toast.getByText(c.shown, { exact: true })).toBeVisible();
      expect(ran(cli, ["skill", "publish", ...OUTGOING])).toBe(true);
    });
  }
});
