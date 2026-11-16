// Updates on the Skills screen: Check updates, which opens the Update skills dialog when it finds
// an update, the banners of an update and their Update button, the banner of a pending merge with
// Show folder, Abort and Update, and the Update skills dialog the sidebar's update pill opens.
// Each button runs exactly one command line, which the mocked CLI records, and its outcome is the
// toast of the results table; the dialog runs one per skill selected and shows the outcome itself.
import type { Page } from "@playwright/test";

import {
  fileReads,
  history,
  releaseNotesDiffs,
  reviewDiffs,
} from "./fixtures/skills.ts";
import { pdfUpdateDiffs } from "./fixtures/update-diffs.ts";
import {
  ACME,
  allUpdates,
  pdfUpdate,
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
const updateDialog = (page: Page) =>
  page.getByRole("dialog", { name: "Update skills" });
/** The row of `name` in the Update skills dialog, before its run. */
const dialogRow = (page: Page, name: string) =>
  updateDialog(page)
    .getByRole("listitem")
    .filter({ has: page.getByRole("checkbox", { exact: true, name }) });

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
      // Updated or up to date already, the skill has no update left.
      await expect(update(page, c.name)).toHaveCount(0);
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
  test("records skill check-updates, turns while it fetches, and opens the Update skills dialog with what it found", async ({
    cli,
    page,
  }) => {
    await open(cli, page, "pdf", []);
    const run = cli.register(["skill", "check-updates"], {
      events: [
        // Before the first fetch: the total at once.
        { current: 0, phase: "fetch", total: 2, type: "progress" },
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
      toasts(page).getByText("Checking for updates · 0 of 2 sources")
    ).toBeVisible();
    run.release();
    // draft's pending merge comes first: four skills are offered an update, in the dialog.
    await expect(updateDialog(page)).toBeVisible();
    await expect(updateDialog(page).getByRole("listitem")).toHaveCount(4);
    await expect(dialogRow(page, "pdf-letters")).toBeVisible();
    await expect(dialogRow(page, "release-notes")).toBeVisible();
    // The dialog says what the check found: its progress toast is gone.
    await expect(toasts(page).getByText(/^Checking for updates/u)).toHaveCount(
      0
    );
    expect(later(cli)).toEqual([["skill", "check-updates", "--json"]]);
  });

  test("is an icon button that shows its name in a tooltip, and is busy while the check runs", async ({
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
    // Still named Check updates while it runs, and busy.
    await expect(checkButton(page)).toHaveAttribute("aria-busy", "true");
    run.release();
    await expect(
      toasts(page).getByText("All skills are up to date")
    ).toBeVisible();
    await expect(checkButton(page)).toHaveAttribute("aria-busy", "false");
  });

  test("nothing new: All skills are up to date, and the updates serve found are gone", async ({
    cli,
    page,
  }) => {
    await open(cli, page, "pdf");
    await expect(update(page, "pdf")).toBeVisible();
    cli.register(["skill", "check-updates"]);
    await checkButton(page).click();
    await expect(
      toasts(page).getByText("All skills are up to date")
    ).toBeVisible();
    await expect(update(page, "pdf")).toHaveCount(0);
  });

  test("exit 3: names the source it could not reach, with a line of the app's own, and opens the dialog with the updates found", async ({
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
    await expect(dialogRow(page, "pdf")).toBeVisible();
    await expect(dialogRow(page, "review")).toBeVisible();
  });
});

test.describe("the Update skills dialog", () => {
  test("the sidebar's pill opens it with every update offered selected, by source, and it updates the selection one skill at a time", async ({
    cli,
    page,
  }) => {
    await open(cli, page, "pdf");
    const nav = page.getByRole("navigation", { name: "Main" });
    // draft's pending merge comes first: its update is not counted.
    await nav.getByRole("button", { exact: true, name: "4 updates" }).click();
    await expect(page).toHaveURL(/#\/skills\/pdf$/u);
    const dialog = updateDialog(page);
    const skillRow = (name: string) => dialogRow(page, name);
    // A group per source, A to Z, each skill under the source its update comes from: a fork's
    // upstream for pdf-letters.
    await expect(dialog.getByRole("heading")).toHaveText([
      "Update skills",
      "acme/skills",
      "me/account",
    ]);
    const acme = dialog.getByRole("region", { name: "acme/skills" });
    await expect(acme.getByRole("listitem")).toHaveCount(3);
    await expect(
      dialog.getByRole("region", { name: "me/account" }).getByRole("checkbox")
    ).toHaveCount(1);
    // Every skill and the select-all row, all checked.
    await expect(dialog.getByRole("checkbox", { checked: true })).toHaveCount(
      5
    );
    await expect(dialog.getByText("All 4 selected")).toBeVisible();
    await expect(skillRow("review")).toContainText(
      "Skill has local changes; update merges with edits."
    );

    // See changes opens What's new above the dialog; Escape closes the sheet alone and puts the
    // focus back on See changes.
    cli.register(["skill", "diff", "pdf", "--update"], {
      events: pdfUpdateDiffs,
    });
    const seeChanges = skillRow("pdf").getByRole("button", {
      exact: true,
      name: "See changes",
    });
    await seeChanges.click();
    const sheet = page.getByRole("dialog", { name: "What's new" });
    await expect(sheet).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(sheet).toHaveCount(0);
    await expect(dialog).toBeVisible();
    await expect(seeChanges).toBeFocused();
    await expect(
      dialog.getByRole("checkbox", { exact: true, name: "pdf" })
    ).toBeChecked();

    // A click on the row's name leaves the skill out.
    await dialog.getByText("release-notes", { exact: true }).click();
    await expect(dialog.getByText("3 of 4 selected")).toBeVisible();

    // A group's button reads Select all while some of its skills are left out and selects them
    // all, then reads Deselect all and clears them all.
    const groupButton = (name: string) =>
      acme.getByRole("button", { exact: true, name });
    await expect(groupButton("Deselect all")).toBeVisible();
    await dialog.getByText("pdf-letters", { exact: true }).click();
    await groupButton("Select all").click();
    await expect(dialog.getByText("3 of 4 selected")).toBeVisible();
    await groupButton("Deselect all").click();
    await expect(dialog.getByText("Select all 4")).toBeVisible();
    await groupButton("Select all").click();
    await expect(dialog.getByText("3 of 4 selected")).toBeVisible();

    cli.register(["skill", "update", "pdf"], {
      events: [updated("pdf", "current")],
    });
    const letters = cli.register(["skill", "update", "pdf-letters"], {
      events: [updated("pdf-letters", "modified")],
      holdAfter: 0,
    });
    cli.register(["skill", "update", "review"], {
      events: [
        { files: [], kind: "managed", name: "review", type: "conflict" },
        {
          code: "pending_merge",
          message: "the update of review conflicts with its edits",
          type: "error",
        },
      ],
      exitCode: 4,
    });
    await dialog
      .getByRole("button", { exact: true, name: "Update 3 skills" })
      .click();
    await letters.started;
    await expect(dialog.getByRole("status")).toHaveText("Updating 2 of 3…");
    letters.release();

    await expect(dialog.getByRole("status")).toHaveText(
      "2 of 3 skills updated"
    );
    // A conflict says where to resolve it, in place of the note on local changes.
    const reviewRow = dialog
      .getByRole("listitem")
      .filter({ hasText: "review" });
    await expect(reviewRow).toContainText(
      "Merge conflict. Open the skill to resolve it."
    );
    await expect(reviewRow).not.toContainText(
      "Skill has local changes; update merges with edits."
    );
    // The merge waits on its row: the dialog opens no skill.
    await expect(page).toHaveURL(/#\/skills\/pdf$/u);
    expect(later(cli)).toEqual([
      ["skill", "update", "pdf", "--json"],
      ["skill", "update", "pdf-letters", "--json"],
      ["skill", "update", "review", "--json"],
    ]);

    await dialog.getByRole("button", { exact: true, name: "Done" }).click();
    await expect(dialog).toHaveCount(0);
    // The inventory that follows has review's merge pending: release-notes is left.
    await cli.serveEmit([
      {
        ...updatesInventory,
        library: updatesLibrary.map((entry) =>
          entry.name === "review" ? { ...entry, pending_merge: true } : entry
        ),
        scan_counter: 2,
      } satisfies InlineEvent,
    ]);
    await expect(
      nav.getByRole("button", { exact: true, name: "1 update" })
    ).toBeVisible();
  });
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
