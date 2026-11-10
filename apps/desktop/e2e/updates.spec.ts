// Updates on the Skills screen: Check updates, the banners of an update and their Update button,
// the banner of a pending merge with Show folder, Abort and Update, and Update all on the toast of
// a check. Each button runs exactly one
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
/** The banner of an update, whichever of the two it is. */
const updateBanner = (page: Page, name: string) =>
  pane(page, name).getByRole("note", {
    name: /^(?:New version from |Published from another machine$)/u,
  });
/** Selects `name` and expects the banner of an update, with its Update. */
const expectOffered = async (page: Page, name: string) => {
  await row(page, name).click();
  await expect(pane(page, name)).toBeVisible();
  await expect(updateBanner(page, name)).toHaveCount(1);
  await expect(
    updateBanner(page, name).getByRole("button", {
      exact: true,
      name: "Update",
    })
  ).toBeVisible();
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

test.describe("Update", () => {
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
    await expect(toasts(page).getByText("Updated pdf skill")).toBeVisible();
    await expect(update(page, "pdf")).toHaveCount(0);
    expect(later(cli)).toEqual([["skill", "update", "pdf", "--json"]]);
  });

  const cases: {
    name: string;
    events: InlineEvent[];
    toast: string;
  }[] = [
    {
      events: [updated("pdf", "current")],
      name: "pdf",
      toast: "Updated pdf skill",
    },
    {
      events: [updated("review", "modified")],
      name: "review",
      toast: "Updated review skill. Merged with your local edits",
    },
    {
      events: [updated("pdf-letters", "modified")],
      name: "pdf-letters",
      toast: "Updated pdf-letters skill. Publish when ready.",
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
      toast: "pdf skill is already up to date",
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

  test("exit 4: the merge waits for the user to resolve its conflicts", async ({
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
      toasts(page).getByText(
        "review skill has conflicts. Resolve them to finish the update",
        { exact: true }
      )
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
      title: "New version from acme/skills",
    },
    {
      name: "review",
      text: "SKILL.md · Your local edits are kept.",
      title: "New version from acme/skills",
    },
    // One of your own skills: the update comes from the upstream it was forked from.
    {
      name: "pdf-letters",
      text: "SKILL.md · examples.md · Your local edits are kept.",
      title: "New version from acme/skills",
    },
    {
      name: "release-notes",
      text: "template.md",
      title: "Published from another machine",
    },
  ];
  for (const c of cases) {
    test(`${c.name}: "${c.title}"`, async ({ cli, page }) => {
      await open(cli, page, c.name);
      const banner = pane(page, c.name).getByRole("note", { name: c.title });
      await expect(banner).toBeVisible();
      await expect(banner.getByText(c.text, { exact: true })).toBeVisible();
      await expect(
        banner.getByRole("button", { exact: true, name: "Update" })
      ).toBeVisible();
    });
  }

  test("a pending merge comes first: its banner in place of the update's", async ({
    cli,
    page,
  }) => {
    // draft has an update too: its pending merge comes first.
    await open(cli, page, "draft");
    await expect(pane(page, "draft").getByRole("note")).toHaveCount(1);
    await expect(
      pane(page, "draft").getByRole("note", { name: "Update has conflicts" })
    ).toBeVisible();
  });
});

test.describe("Check updates", () => {
  test("records skill check-updates, turns while it fetches, and toasts what it found with Update all", async ({
    cli,
    page,
  }) => {
    await open(cli, page, "pdf", []);
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
    await expectOffered(page, "pdf");
    await expectOffered(page, "pdf-letters");
    await expectOffered(page, "release-notes");
    await expectOffered(page, "review");
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

  test("is an icon button that shows its name in a tooltip, and stays busy while the check runs", async ({
    cli,
    page,
  }) => {
    await open(cli, page, "pdf", []);
    await expect(checkButton(page)).toHaveText("");
    await checkButton(page).hover();
    await expect(page.getByRole("tooltip")).toHaveText("Check updates");
    // Away in a few steps, as a pointer moves: the tooltip closes once it is past the grace area
    // between the button and the tooltip.
    await page.mouse.move(1, 1, { steps: 5 });
    await expect(page.getByRole("tooltip")).toHaveCount(0);

    // From the keyboard too: the focus shows the tooltip.
    await checkButton(page).focus();
    await expect(page.getByRole("tooltip")).toHaveText("Check updates");
    await page.keyboard.press("Escape");
    await expect(page.getByRole("tooltip")).toHaveCount(0);

    const run = cli.register(["skill", "check-updates"], { holdAfter: 0 });
    await checkButton(page).click();
    await run.started;
    // Still named Check updates while it runs; busy, its icon turning.
    await expect(checkButton(page)).toHaveAttribute("aria-busy", "true");
    await expect(checkButton(page).locator(".animate-turn-once")).toHaveCount(
      1
    );
    run.release();
    await expect(
      toasts(page).getByText("Everything is up to date")
    ).toBeVisible();
    await expect(checkButton(page)).toHaveAttribute("aria-busy", "false");
    await expect(checkButton(page).locator(".animate-turn-once")).toHaveCount(
      0
    );
  });

  test("one update: 1 skill has an update", async ({ cli, page }) => {
    await open(cli, page, "pdf", []);
    cli.register(["skill", "check-updates"], { events: [pdfUpdate] });
    await checkButton(page).click();
    await expect(toasts(page).getByText("1 skill has an update")).toBeVisible();
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

  test("exit 3: names the source it could not reach, with a line of the app's own, and lists the updates found", async ({
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
    // The app's own hint, in place of the CLI's, in one sentence-cased paragraph.
    await expect(
      toasts(page).getByText(
        "Couldn't reach zeta/tools. Verify that you can reach the source.",
        { exact: true }
      )
    ).toBeVisible();
    await expectOffered(page, "pdf");
    await expectOffered(page, "review");
  });
});

test.describe("Update all", () => {
  const cases = [
    { conflicted: ["review"], toast: "Updated 3 skills. 1 has conflicts" },
    {
      conflicted: ["review", "pdf-letters"],
      toast: "Updated 2 skills. 2 have conflicts",
    },
  ];
  for (const c of cases) {
    test(`exit 4: "${c.toast}"`, async ({ cli, page }) => {
      await open(cli, page, "pdf", []);
      cli.register(["skill", "check-updates"], {
        events: [pdfUpdate, lettersUpdate, releaseNotesUpdate, reviewUpdate],
      });
      await checkButton(page).click();
      const names = ["pdf", "pdf-letters", "release-notes", "review"];
      cli.register(["skill", "update", "--all"], {
        events: [
          ...names
            .filter((name) => !c.conflicted.includes(name))
            .map((name) => updated(name, "current")),
          ...c.conflicted.map((name): InlineEvent => ({
            files: [],
            kind: "managed",
            name,
            type: "conflict",
          })),
          {
            code: "pending_merge",
            message: `${c.conflicted.length} of 4 skills could not be updated: the update conflicts with their edits`,
            type: "error",
          },
        ],
        exitCode: 4,
      });
      await toasts(page)
        .getByRole("button", { exact: true, name: "Update all" })
        .click();
      await expect(
        toasts(page).getByText(c.toast, { exact: true })
      ).toBeVisible();
      expect(later(cli)).toEqual([
        ["skill", "check-updates", "--json"],
        ["skill", "update", "--all", "--json"],
      ]);
    });
  }
});

/** The banner of draft's pending merge. */
const conflictBanner = (page: Page) =>
  pane(page, "draft").getByRole("note", { name: "Update has conflicts" });
const abortDialog = (page: Page) =>
  page.getByRole("dialog", { name: "Abort the update of draft?" });

test.describe("a pending merge", () => {
  const banner = conflictBanner;
  const button = (page: Page, name: string) =>
    banner(page).getByRole("button", { exact: true, name });

  test("says so in attention, with Show folder, Abort and Update in that order", async ({
    cli,
    page,
  }) => {
    await open(cli, page, "draft");
    await expect(banner(page)).toBeVisible();
    // No conflict event this session: the sentence without the count.
    await expect(
      banner(page).getByText(
        "Your edits and the update changed the same lines. Resolve them in the merge folder, then update again.",
        { exact: true }
      )
    ).toBeVisible();
    expect(
      await banner(page)
        .getByRole("button")
        .evaluateAll((buttons) => buttons.map((b) => b.textContent))
    ).toEqual(["Show folder", "Abort", "Update"]);
  });

  test("Show folder reveals the merge folder in agentx home", async ({
    cli,
    page,
  }) => {
    await open(cli, page, "draft");
    await button(page, "Show folder").click();
    await expect
      .poll(() => cli.reveals)
      .toEqual([["/home/me/.agentx/merges/draft/draft"]]);
  });

  test("Abort asks first: Cancel closes the dialog, Abort records skill update <name> --abort and keeps the update", async ({
    cli,
    page,
  }) => {
    await open(cli, page, "draft");
    await button(page, "Abort").click();
    await expect(abortDialog(page)).toBeVisible();
    await expect(
      abortDialog(page).getByText(
        "Your resolved files in the merge folder are discarded. The skill and the update stay as they were.",
        { exact: true }
      )
    ).toBeVisible();
    await abortDialog(page)
      .getByRole("button", { exact: true, name: "Cancel" })
      .click();
    await expect(abortDialog(page)).toBeHidden();

    cli.register(["skill", "update", "draft", "--abort"], {
      events: [
        {
          ...updatesLibrary.find((e) => e.name === "draft"),
          pending_merge: false,
          type: "library_skill",
        },
        {
          ok: true,
          summary:
            "gave up the merge of draft; its worktree and branch are as they were",
          type: "result",
        },
      ],
    });
    await button(page, "Abort").click();
    await abortDialog(page)
      .getByRole("button", { exact: true, name: "Abort" })
      .click();
    await expect(abortDialog(page)).toBeHidden();
    await expect(
      toasts(page).getByText("Aborted the update of draft skill", {
        exact: true,
      })
    ).toBeVisible();
    expect(later(cli)).toEqual([
      ["skill", "update", "draft", "--abort", "--json"],
    ]);

    // The merge is given up, not the update: the inventory that follows offers it again.
    await cli.serveEmit([
      {
        ...updatesInventory,
        library: updatesLibrary.map((e) =>
          e.name === "draft" ? { ...e, pending_merge: false } : e
        ),
        scan_counter: 2,
      },
    ]);
    await expect(banner(page)).toHaveCount(0);
    await expectOffered(page, "draft");
    await page.getByRole("button", { name: "Filter" }).click();
    await expect(
      page
        .getByRole("dialog", { name: "Filters" })
        .getByRole("radio", { name: "Updates 5" })
    ).toBeVisible();
  });

  test("Update tries again: still conflicting, the banner counts the files", async ({
    cli,
    page,
  }) => {
    await open(cli, page, "draft");
    cli.register(["skill", "update", "draft"], {
      events: [
        {
          files: [
            { base: "b1", mine: "m1", path: "SKILL.md", theirs: "t1" },
            { base: null, mine: "m2", path: "notes.md", theirs: "t2" },
          ],
          kind: "managed",
          name: "draft",
          type: "conflict",
        },
        {
          code: "pending_merge",
          message: "draft conflicts with its update in 2 files",
          type: "error",
        },
      ],
      exitCode: 4,
    });
    await button(page, "Update").click();
    await expect(
      toasts(page).getByText(
        "draft skill has conflicts. Resolve them to finish the update",
        { exact: true }
      )
    ).toBeVisible();
    await expect(
      banner(page).getByText(
        "Your edits and the update changed the same lines in 2 files. Resolve them in the merge folder, then update again.",
        { exact: true }
      )
    ).toBeVisible();
    expect(later(cli)).toEqual([["skill", "update", "draft", "--json"]]);
  });
});
