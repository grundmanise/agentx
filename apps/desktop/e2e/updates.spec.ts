// Updates on the Skills screen: Check updates, the banners of an update and their Update button,
// and Update all on the toast of a check. No row carries an Update button. Each button runs exactly one
// command line, which the mocked CLI records, and its outcome is the toast of the results table.
import type { Page } from "@playwright/test";

import {
  fileReads,
  history,
  releaseNotesDiffs,
  reviewDiffs,
} from "./fixtures/skills.ts";
import {
  ACME,
  allUpdates,
  lettersUpdate,
  pdfUpdate,
  releaseNotesUpdate,
  reviewUpdate,
  updatesInventory,
  updatesLibrary,
  ZETA,
} from "./fixtures/updates.ts";
import { expect, loadLines, test } from "./harness/cli.ts";
import type { InlineEvent, MockCli } from "./harness/cli.ts";

const launchLines = [
  ["version", "--json"],
  ["doctor", "--json"],
];

/** The reads the detail pane runs on its own when a skill is selected. */
const detailReads = new Set(["skill history", "skill files", "skill diff"]);

/** The command lines the buttons ran: everything but the launch, the store's and the pane's reads. */
const later = (cli: MockCli) =>
  cli.commands.filter(
    (l) =>
      ![...launchLines, ...loadLines].some(
        (k) => k.join(" ") === l.join(" ")
      ) && !detailReads.has(l.slice(0, 2).join(" "))
  );

const list = (page: Page) => page.getByRole("navigation", { name: "Skills" });
const row = (page: Page, name: string) =>
  list(page).getByRole("button", { exact: true, name });
const pane = (page: Page, name: string) =>
  page
    .getByRole("region", { name: "Skill details" })
    .getByRole("article", { name });
/** The Update of `name`'s banner, in the detail pane while `name` is selected. */
const update = (page: Page, name: string) =>
  pane(page, name).getByRole("button", { exact: true, name: "Update" });
const toasts = (page: Page) =>
  page.getByRole("region", { name: /^Notifications/u });
/** Selects `name` and expects its banner to offer Update, or not. */
const expectOffered = async (page: Page, name: string, offers: boolean) => {
  await row(page, name).click();
  await expect(pane(page, name)).toBeVisible();
  await expect(update(page, name)).toHaveCount(offers ? 1 : 0);
};
const checkButton = (page: Page) =>
  page.getByRole("button", { exact: true, name: "Check updates" });

/** The library entry of `name` as `skill update` emits it once it updated the skill. */
const updated = (name: string, state: "current" | "modified"): InlineEvent => {
  const entry = updatesLibrary.find((e) => e.name === name);
  return { ...entry, state, type: "library_skill" };
};

/**
 * Opens the Skills screen on the fixture machine, on `name`, with serve's start check having
 * found `updates`.
 */
const open = async (
  cli: MockCli,
  page: Page,
  name: string,
  updates: InlineEvent[] = allUpdates
) => {
  cli.onServeStart({ events: [updatesInventory, ...updates] });
  cli.register(["skill", "history", "pdf-letters"], { events: history });
  cli.register(["skill", "history", "release-notes"], { events: [] });
  cli.register(["skill", "history", "draft"], { events: [] });
  for (const read of fileReads) {
    cli.register(["skill", "files", ...read.args], { events: read.events });
  }
  cli.register(["skill", "diff", "review"], { events: reviewDiffs });
  cli.register(["skill", "diff", "release-notes"], {
    events: releaseNotesDiffs,
  });
  cli.register(["skill", "diff", "pdf-letters"], { events: [] });
  await page.goto(`/#/skills/${encodeURIComponent(name)}`);
  await expect(pane(page, name)).toBeVisible();
};

test.describe("the rows", () => {
  test("carry no Update button: Update is on the banner", async ({
    cli,
    page,
  }) => {
    await open(cli, page, "pdf");
    await expect(row(page, "pdf")).toBeVisible();
    await expect(
      list(page).getByRole("button", { name: /^Update /u })
    ).toHaveCount(0);
    await expect(update(page, "pdf")).toBeVisible();
  });

  test("the banner's Update records skill update <name>, is pending, and leaves once it updated", async ({
    cli,
    page,
  }) => {
    await open(cli, page, "pdf");
    const run = cli.register(["skill", "update", "pdf"], {
      events: [updated("pdf", "current")],
      holdAfter: 0,
    });
    await update(page, "pdf").click();
    await run.started;
    await expect(update(page, "pdf")).toHaveAttribute("aria-busy", "true");
    run.release();
    await expect(toasts(page).getByText("Updated pdf")).toBeVisible();
    await expect(update(page, "pdf")).toHaveCount(0);
    expect(later(cli)).toEqual([["skill", "update", "pdf", "--json"]]);
  });
});

test.describe("Update", () => {
  const cases: {
    name: string;
    events: InlineEvent[];
    toast: string;
  }[] = [
    {
      events: [updated("pdf", "current")],
      name: "pdf",
      toast: "Updated pdf",
    },
    {
      events: [updated("review", "modified")],
      name: "review",
      toast: "Updated review and kept your edits",
    },
    {
      events: [updated("pdf-letters", "modified")],
      name: "pdf-letters",
      toast: "Updated pdf-letters. Publish when ready.",
    },
    {
      events: [
        {
          ok: true,
          summary: "pdf is up to date as of the last update check",
          type: "result",
        },
      ],
      name: "pdf",
      toast: "pdf is already up to date",
    },
  ];
  for (const c of cases) {
    test(`${c.name}: "${c.toast}"`, async ({ cli, page }) => {
      await open(cli, page, c.name);
      cli.register(["skill", "update", c.name], { events: c.events });
      await pane(page, c.name)
        .getByRole("button", { exact: true, name: "Update" })
        .click();
      await expect(
        toasts(page).getByText(c.toast, { exact: true })
      ).toBeVisible();
      expect(later(cli)).toEqual([["skill", "update", c.name, "--json"]]);
    });
  }

  test("exit 4: the merge waits for the user's choices", async ({
    cli,
    page,
  }) => {
    await open(cli, page, "review");
    cli.register(["skill", "update", "review"], {
      events: [
        {
          files: [],
          kind: "managed",
          name: "review",
          type: "conflict",
        },
        {
          code: "pending_merge",
          message: "the update of review conflicts with its edits",
          type: "error",
        },
      ],
      exitCode: 4,
    });
    await update(page, "review").click();
    await expect(
      toasts(page).getByText("Updated review. It needs your choices")
    ).toBeVisible();
    // The runner opens the skill whose merge waits.
    await expect(pane(page, "review")).toBeVisible();
    expect(later(cli)).toEqual([["skill", "update", "review", "--json"]]);
  });

  test("exit 5: the CLI's message, and Add source records source add <source>", async ({
    cli,
    page,
  }) => {
    await open(cli, page, "pdf");
    const message = `pdf was installed from ${ACME}, which was removed from this machine, so it is not updated`;
    cli.register(["skill", "update", "pdf"], {
      events: [
        {
          code: "not_found",
          hint: `run 'agentx source add ${ACME}' to add it again`,
          message,
          type: "error",
        },
      ],
      exitCode: 5,
    });
    await update(page, "pdf").click();
    await expect(toasts(page).getByText(message)).toBeVisible();
    cli.register(["source", "add", ACME]);
    await toasts(page)
      .getByRole("button", { exact: true, name: "Add source" })
      .click();
    await expect(
      toasts(page).getByText("Added acme/skills source")
    ).toBeVisible();
    expect(later(cli)).toEqual([
      ["skill", "update", "pdf", "--json"],
      ["source", "add", ACME, "--json"],
    ]);
  });

  test("another failure: the runner's error toast", async ({ cli, page }) => {
    await open(cli, page, "pdf");
    cli.register(["skill", "update", "pdf"], {
      events: [
        {
          code: "refused",
          hint: "move it out of the skill, then run 'agentx skill update pdf' again",
          message: "pdf holds .venv, which git cannot record",
          type: "error",
        },
      ],
      exitCode: 6,
    });
    await update(page, "pdf").click();
    await expect(
      toasts(page).getByText("pdf holds .venv, which git cannot record")
    ).toBeVisible();
    await expect(
      toasts(page).getByText(
        "move it out of the skill, then run 'agentx skill update pdf' again"
      )
    ).toBeVisible();
  });
});

test.describe("banners", () => {
  const cases = [
    {
      name: "pdf",
      text: "SKILL.md · scripts/convert.py",
      title: "A newer version from acme/skills",
    },
    {
      name: "review",
      text: "SKILL.md · your edits are kept",
      title: "A newer version from acme/skills",
    },
    {
      name: "pdf-letters",
      text: "SKILL.md · examples.md · edits you have not published are kept",
      title: "The original has a new version",
    },
    {
      name: "release-notes",
      text: "template.md",
      title: "Changes from your other machines",
    },
  ];
  for (const c of cases) {
    test(`${c.name}: "${c.title}"`, async ({ cli, page }) => {
      await open(cli, page, c.name);
      const banner = pane(page, c.name).getByRole("note", { name: c.title });
      await expect(banner).toBeVisible();
      await expect(banner).toContainText(c.text);
      await expect(
        banner.getByRole("button", { exact: true, name: "Update" })
      ).toBeVisible();
    });
  }

  test("no update banner for a skill with a pending merge, or without an update", async ({
    cli,
    page,
  }) => {
    await open(cli, page, "draft");
    await expect(
      pane(page, "draft").getByRole("button", { exact: true, name: "Update" })
    ).toHaveCount(0);
    await page.goto("/#/skills/gone");
    await expect(
      pane(page, "gone").getByRole("note", { name: "Removed from source" })
    ).toBeVisible();
  });
});

test.describe("Check updates", () => {
  test("records skill check-updates, turns while it fetches, and toasts what it found with Update all", async ({
    cli,
    page,
  }) => {
    await open(cli, page, "pdf", []);
    await expect(update(page, "pdf")).toHaveCount(0);
    const run = cli.register(["skill", "check-updates"], {
      events: [
        {
          current: 1,
          phase: "fetch",
          subject: ACME,
          total: 2,
          type: "progress",
        },
        {
          current: 2,
          phase: "fetch",
          subject: ZETA,
          total: 2,
          type: "progress",
        },
        ...allUpdates,
      ],
      holdAfter: 1,
    });
    await checkButton(page).click();
    await run.started;
    await expect(checkButton(page)).toHaveAttribute("aria-busy", "true");
    await expect(
      toasts(page).getByText("Checking for updates · 1 of 2 sources")
    ).toBeVisible();
    run.release();
    // draft's pending merge comes first: four skills are offered an update.
    await expect(toasts(page).getByText("4 skills have updates")).toBeVisible();
    await expect(checkButton(page)).toHaveAttribute("aria-busy", "false");
    // One after the other: each selects its row.
    await expectOffered(page, "pdf", true);
    await expectOffered(page, "pdf-letters", true);
    await expectOffered(page, "release-notes", true);
    await expectOffered(page, "review", true);
    await expectOffered(page, "draft", false);
    expect(later(cli)).toEqual([["skill", "check-updates", "--json"]]);

    cli.register(["skill", "update", "--all"], {
      events: [
        updated("pdf", "current"),
        updated("pdf-letters", "modified"),
        updated("release-notes", "current"),
        updated("review", "modified"),
      ],
    });
    await toasts(page)
      .getByRole("button", { exact: true, name: "Update all" })
      .click();
    await expect(toasts(page).getByText("Updated 4 skills")).toBeVisible();
    expect(later(cli)).toEqual([
      ["skill", "check-updates", "--json"],
      ["skill", "update", "--all", "--json"],
    ]);
  });

  test("one update: no Update all", async ({ cli, page }) => {
    await open(cli, page, "pdf", []);
    cli.register(["skill", "check-updates"], { events: [pdfUpdate] });
    await checkButton(page).click();
    await expect(toasts(page).getByText("1 skill has an update")).toBeVisible();
    await expect(
      toasts(page).getByRole("button", { name: "Update all" })
    ).toHaveCount(0);
  });

  test("nothing new: Everything is up to date, and the updates serve found are gone", async ({
    cli,
    page,
  }) => {
    await open(cli, page, "pdf");
    await expect(update(page, "pdf")).toBeVisible();
    cli.register(["skill", "check-updates"]);
    await checkButton(page).click();
    await expect(
      toasts(page).getByText("Everything is up to date")
    ).toBeVisible();
    await expect(update(page, "pdf")).toHaveCount(0);
  });

  test("exit 3: names the source it could not reach, with the hint, and lists the updates found", async ({
    cli,
    page,
  }) => {
    await open(cli, page, "pdf", []);
    const hint =
      "check that you can reach the source and that a private one has a git credential helper";
    cli.register(["skill", "check-updates"], {
      events: [
        {
          current: 1,
          phase: "fetch",
          subject: ACME,
          total: 2,
          type: "progress",
        },
        {
          current: 2,
          phase: "fetch",
          subject: ZETA,
          total: 2,
          type: "progress",
        },
        pdfUpdate,
        reviewUpdate,
        {
          code: "source",
          hint,
          message: `could not check ${ZETA}: git fetch: could not resolve host; not checked: gone`,
          type: "error",
        },
      ],
      exitCode: 3,
      logs: [
        {
          level: "warn",
          message: `${ZETA}: git fetch: could not resolve host; not checked: gone`,
        },
      ],
    });
    await checkButton(page).click();
    await expect(
      toasts(page).getByText("Checked. Couldn't reach zeta/tools")
    ).toBeVisible();
    await expect(toasts(page).getByText(hint)).toBeVisible();
    await expectOffered(page, "pdf", true);
    await expectOffered(page, "review", true);
    await expectOffered(page, "pdf-letters", false);
  });
});

test.describe("Update all", () => {
  test("exit 4: Updated {k} skills. {m} need your choices", async ({
    cli,
    page,
  }) => {
    await open(cli, page, "pdf", []);
    cli.register(["skill", "check-updates"], {
      events: [pdfUpdate, lettersUpdate, releaseNotesUpdate, reviewUpdate],
    });
    await checkButton(page).click();
    cli.register(["skill", "update", "--all"], {
      events: [
        updated("pdf", "current"),
        updated("pdf-letters", "modified"),
        updated("release-notes", "current"),
        { files: [], kind: "managed", name: "review", type: "conflict" },
        {
          code: "pending_merge",
          message:
            "1 of 4 skills could not be updated: review: the update conflicts with its edits",
          type: "error",
        },
      ],
      exitCode: 4,
    });
    await toasts(page)
      .getByRole("button", { exact: true, name: "Update all" })
      .click();
    await expect(
      toasts(page).getByText("Updated 3 skills. 1 needs your choices")
    ).toBeVisible();
    expect(later(cli)).toEqual([
      ["skill", "check-updates", "--json"],
      ["skill", "update", "--all", "--json"],
    ]);
  });
});
