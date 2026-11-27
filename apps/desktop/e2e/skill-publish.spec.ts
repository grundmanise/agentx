// Publishing a skill from the Skills detail pane: the banners that say a skill's edits are only on
// this machine, the Publish dialog they open, and the toast of each outcome of `skill publish`.
// Every button's command line is asserted as the mocked CLI recorded it.
import type { Locator, Page } from "@playwright/test";

import type { PublishOutcome, Source } from "#/cli/events.gen.ts";

import {
  inventoryWith,
  showReads,
  sources,
  sourcesWithoutHub,
} from "./fixtures/publish.ts";
import { HUB, ACME } from "./fixtures/skills.ts";
import { expect, loadLines, test } from "./harness/cli.ts";
import type { InlineEvent, MockCli, Reply } from "./harness/cli.ts";

const launchLines = [
  ["version", "--json"],
  ["doctor", "--json"],
];

const isLaunchOrLoad = (line: string[]) =>
  [...launchLines, ...loadLines].some(
    (known) => known.join(" ") === line.join(" ")
  );

/** The command lines run after the launch, but the detail pane's own read of the skill it shows. */
const later = (cli: MockCli) =>
  cli.commands.filter(
    (l) => !(isLaunchOrLoad(l) || l.slice(0, 2).join(" ") === "skill show")
  );

const toasts = (page: Page) =>
  page.getByRole("region", { name: /^Notifications/u });
const pane = (page: Page, name: string) =>
  page
    .getByRole("region", { name: "Skill details" })
    .getByRole("article", { name });
const banner = (article: Locator) => article.getByRole("note");
const dialog = (page: Page) =>
  page.getByRole("dialog", { name: "Publish changes" });

/** Opens a skill on the fixture machine, with every read registered. */
const openSkill = async (
  cli: MockCli,
  page: Page,
  name: string,
  listed: Source[] = sources,
  /** The skills whose read holds its answer back. */
  held: string[] = []
) => {
  cli.onServeStart({ events: [inventoryWith(listed)] });
  for (const read of showReads) {
    cli.register(["skill", "show", ...read.args], {
      events: read.events,
      ...(held.includes(read.args[0] ?? "") ? { holdAfter: 0 } : {}),
    });
  }
  await page.goto(`/#/skills/${name}`);
  await expect(pane(page, name)).toBeVisible();
};

/** Opens the Publish dialog of `name` from its banner's button. */
const openDialog = async (page: Page, name: string, button = "Publish") => {
  await banner(pane(page, name)).getByRole("button", { name: button }).click();
  const shown = dialog(page);
  await expect(shown).toBeVisible();
  return shown;
};

/** A `skill publish <name>` reply: the `publish` event, and the error and logs when there are. */
const publishReply = (
  name: string,
  outcome: PublishOutcome | undefined,
  rest: Partial<Reply> & { error?: InlineEvent } = {}
): Reply => {
  const events: InlineEvent[] = [
    { current: 1, phase: "publish", subject: name, type: "progress" },
  ];
  if (outcome !== undefined) {
    events.push({
      name,
      outcome,
      source: HUB,
      type: "publish",
    });
  }
  if (rest.error !== undefined) {
    events.push(rest.error);
  }
  const { error: _, ...reply } = rest;
  return { events, ...reply };
};

test.describe("banners", () => {
  test("your skill, edited since it was published: Edits not published, with Publish", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "changelog");
    const note = banner(pane(page, "changelog"));
    await expect(note).toHaveAccessibleName("Edits not published");
    await expect(note).toContainText("Publish to share your latest version.");
    await expect(note.getByRole("button")).toHaveText(["Publish"]);
  });

  test("your skill, never published, with a hub: Not published, with Publish", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "draft");
    const note = banner(pane(page, "draft"));
    await expect(note).toHaveAccessibleName("Not published");
    await expect(note).toContainText("Publish it to use it on other machines.");
    await expect(note.getByRole("button")).toHaveText(["Publish"]);
  });

  test("your skill, never published, with no hub: Open Settings", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "draft", sourcesWithoutHub);
    const note = banner(pane(page, "draft"));
    await expect(note).toHaveAccessibleName("Not published");
    await expect(note).toContainText("Set a hub to publish it.");
    await expect(note.getByRole("button")).toHaveText(["Open Settings"]);
    await note.getByRole("button", { name: "Open Settings" }).click();
    await expect(page).toHaveURL(/#\/settings$/u);
  });

  test("an edited skill of a writable source: Edits not published, with Publish", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "review");
    const note = banner(pane(page, "review"));
    await expect(note).toHaveAccessibleName("Edits not published");
    await expect(note).toContainText("Publish to share them on acme/skills.");
    await expect(note.getByRole("button")).toHaveText(["Publish"]);
    await expect(
      pane(page, "review").getByRole("region", { name: "Files" })
    ).toContainText("edited");
  });

  test("an edited skill of a source forks from the action row, whose Fork runs skill fork <name>", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "review");
    await pane(page, "review")
      .getByLabel("Actions", { exact: true })
      .getByRole("button", { exact: true, name: "Fork" })
      .click();
    const fork = page.getByRole("dialog", { name: "Fork review" });
    await expect(fork).toBeVisible();
    cli.register(["skill", "fork", "review"], {
      events: [{ name: "review", type: "library_skill" }],
    });
    await fork.getByRole("button", { exact: true, name: "Fork" }).click();
    await expect(
      toasts(page).getByText("Forked review skill", { exact: true })
    ).toBeVisible();
    expect(later(cli).at(-1)).toEqual(["skill", "fork", "review", "--json"]);
  });

  test("an edited skill of a read-only source: Edited locally, with the count of its edits", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "lint");
    const note = banner(pane(page, "lint"));
    await expect(note).toHaveAccessibleName("Edited locally");
    await expect(note).toContainText(
      "1 file differs from zeta/tools. Updates keep your edits."
    );
  });

  test("an edited skill of a read-only source, before its read answers: Updates keep your edits", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "lint", sources, ["lint"]);
    const note = banner(pane(page, "lint"));
    await expect(note).toHaveAccessibleName("Edited locally");
    await expect(note).toContainText("Updates keep your edits.");
  });
});

test.describe("the Publish dialog", () => {
  test("opens with the changed files the pane read; Publish with the summary it starts with runs skill publish <name>", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "changelog");
    await expect(
      pane(page, "changelog").getByRole("region", { name: "Files" })
    ).toContainText("edited");
    const shown = await openDialog(page, "changelog");
    await expect(shown).toContainText(
      "The changelog skill will be published to your hub."
    );
    const field = shown.getByRole("textbox", { name: "What changed" });
    await expect(field).toHaveValue("Edit SKILL.md and add examples.md");
    await expect(field).toBeFocused();
    const changed = shown.getByRole("list", { name: "Changed files" });
    await expect(changed.getByRole("listitem")).toHaveText([
      "SKILL.mdedited",
      "examples.mdadded",
    ]);
    cli.register(
      ["skill", "publish", "changelog"],
      publishReply("changelog", "pushed")
    );
    await shown.getByRole("button", { name: "Publish" }).click();
    await expect(
      toasts(page).getByText("Published changelog skill")
    ).toBeVisible();
    await expect(shown).toBeHidden();
    // The dialog read nothing: the pane's read of the skill held its files.
    expect(later(cli)).toEqual([["skill", "publish", "changelog", "--json"]]);
  });

  test("Enter publishes, with the message typed", async ({ cli, page }) => {
    await openSkill(cli, page, "changelog");
    const shown = await openDialog(page, "changelog");
    cli.register(
      ["skill", "publish", "changelog", "--message", "Tighten the wording"],
      publishReply("changelog", "pushed")
    );
    const field = shown.getByRole("textbox", { name: "What changed" });
    await field.fill("  Tighten the wording ");
    await field.press("Enter");
    await expect(
      toasts(page).getByText("Published changelog skill")
    ).toBeVisible();
    expect(later(cli).at(-1)).toEqual([
      "skill",
      "publish",
      "changelog",
      "--message",
      "Tighten the wording",
      "--json",
    ]);
  });

  test("What changed is optional: Publish with it empty runs with no message", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "changelog");
    const shown = await openDialog(page, "changelog");
    const field = shown.getByRole("textbox", { name: "What changed" });
    await field.fill("   ");
    cli.register(
      ["skill", "publish", "changelog"],
      publishReply("changelog", "pushed")
    );
    await shown.getByRole("button", { name: "Publish" }).click();
    await expect(
      toasts(page).getByText("Published changelog skill")
    ).toBeVisible();
    expect(later(cli).at(-1)).toEqual([
      "skill",
      "publish",
      "changelog",
      "--json",
    ]);
  });

  test("shows the run's progress until it ends", async ({ cli, page }) => {
    await openSkill(cli, page, "changelog");
    const shown = await openDialog(page, "changelog");
    const run = cli.register(["skill", "publish", "changelog"], {
      events: [
        { current: 1, phase: "fetch", subject: HUB, type: "progress" },
        {
          current: 1,
          phase: "publish",
          subject: "changelog",
          type: "progress",
        },
        {
          name: "changelog",
          outcome: "pushed",
          source: HUB,
          type: "publish",
        },
      ],
      holdAfter: 1,
    });
    await shown.getByRole("button", { name: "Publish" }).click();
    await run.started;
    await expect(shown.getByRole("status")).toHaveText("Fetching me/hub…");
    await expect(shown.getByRole("button", { name: "Publish" })).toBeDisabled();
    run.release();
    await expect(
      toasts(page).getByText("Published changelog skill")
    ).toBeVisible();
  });

  test("your skill never published: Publish from its banner, listing every file as new", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "draft");
    const shown = await openDialog(page, "draft");
    await expect(shown).toContainText(
      "The draft skill will be published to your hub."
    );
    // Never published: every file goes, even of a fork that matches the commit that created it.
    await expect(shown.getByRole("listitem")).toHaveText([
      "SKILL.mdnew",
      "assets/logo.pngnew",
      "reference.pdfnew",
      "reference/forms.mdnew",
    ]);
    cli.register(
      ["skill", "publish", "draft"],
      publishReply("draft", "pushed")
    );
    await shown.getByRole("button", { name: "Publish" }).click();
    await expect(toasts(page).getByText("Published draft skill")).toBeVisible();
    expect(later(cli)).toEqual([["skill", "publish", "draft", "--json"]]);
  });

  test("an edited skill of a writable source: Publish to its source", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "review");
    const shown = await openDialog(page, "review");
    await expect(shown).toContainText(
      "The review skill will be published to acme/skills."
    );
    await expect(shown.getByRole("listitem")).toHaveCount(3);
    cli.register(
      ["skill", "publish", "review"],
      publishReply("review", "pushed")
    );
    await shown.getByRole("button", { name: "Publish" }).click();
    await expect(
      toasts(page).getByText("Published review skill")
    ).toBeVisible();
    expect(later(cli).at(-1)).toEqual(["skill", "publish", "review", "--json"]);
  });
});

test.describe("outcomes", () => {
  test("up to date: not published, as it already matches the published version", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "changelog");
    const shown = await openDialog(page, "changelog");
    cli.register(
      ["skill", "publish", "changelog"],
      publishReply("changelog", "up to date")
    );
    await shown.getByRole("button", { name: "Publish" }).click();
    await expect(
      toasts(page).getByText(
        "changelog skill was not published. It already matches the published version."
      )
    ).toBeVisible();
  });

  for (const [outcome, exitCode] of [
    ["behind", 0],
    ["moved", 6],
  ] as const) {
    test(`${outcome}: a newer version on the hub, and Update runs skill update <name>`, async ({
      cli,
      page,
    }) => {
      await openSkill(cli, page, "changelog");
      const shown = await openDialog(page, "changelog");
      cli.register(
        ["skill", "publish", "changelog"],
        publishReply("changelog", outcome, {
          exitCode,
          ...(exitCode === 0
            ? {
                logs: [
                  {
                    level: "warn",
                    message:
                      "changelog has nothing to publish; the hub holds changes published from another machine",
                  },
                ],
              }
            : {
                error: {
                  code: "refused",
                  hint: "run 'agentx skill update changelog' to get them, then publish again",
                  message:
                    "changelog has changes published from another machine, so it was not published",
                  type: "error",
                },
              }),
        })
      );
      await shown.getByRole("button", { name: "Publish" }).click();
      const toast = toasts(page).getByText(
        "There is a newer version of changelog skill on the hub. Update it first, then publish your changes."
      );
      await expect(toast).toBeVisible();
      cli.register(["skill", "update", "changelog"], {
        events: [{ ok: true, type: "result" }],
      });
      await toasts(page).getByRole("button", { name: "Update" }).click();
      await expect(
        toasts(page).getByText("Updated changelog skill")
      ).toBeVisible();
      expect(later(cli).slice(-2)).toEqual([
        ["skill", "publish", "changelog", "--json"],
        ["skill", "update", "changelog", "--json"],
      ]);
    });
  }

  test("moved, for a skill of a source: a newer version in the source, with Update", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "review");
    const shown = await openDialog(page, "review");
    cli.register(
      ["skill", "publish", "review"],
      publishReply("review", "moved", {
        error: {
          code: "refused",
          hint: "run 'agentx skill update review' to take the change in, then publish again",
          message: `${ACME} changed review since it was installed, so it was not published`,
          type: "error",
        },
        exitCode: 6,
      })
    );
    await shown.getByRole("button", { name: "Publish" }).click();
    await expect(
      toasts(page).getByText(
        "There is a newer version of review skill in acme/skills. Update it first, then publish your changes."
      )
    ).toBeVisible();
    await expect(
      toasts(page).getByRole("button", { name: "Update" })
    ).toBeVisible();
    expect(later(cli).at(-1)).toEqual(["skill", "publish", "review", "--json"]);
  });

  for (const [outcome, exitCode, code, message, hint, sentence] of [
    [
      "declined",
      6,
      "refused",
      "the hub declined skills/changelog: protected branch",
      "once the hub takes it, run 'agentx skill publish changelog' again",
      "The hub declined skills/changelog: protected branch. Publish again once the hub takes it.",
    ],
    [
      "denied",
      3,
      "source",
      `the hub ${HUB} did not let this machine push skills/changelog: permission denied`,
      "check that the git credential helper or SSH key used for it may push there",
      `The hub ${HUB} did not let this machine push skills/changelog: permission denied. Check that the git credential helper or SSH key used for it may push there.`,
    ],
  ] as const) {
    test(`${outcome} (exit ${exitCode}): the CLI's message, and its hint in the app's words`, async ({
      cli,
      page,
    }) => {
      await openSkill(cli, page, "changelog");
      const shown = await openDialog(page, "changelog");
      cli.register(
        ["skill", "publish", "changelog"],
        publishReply("changelog", outcome, {
          error: { code, hint, message, type: "error" },
          exitCode,
        })
      );
      await shown.getByRole("button", { name: "Publish" }).click();
      // The message and the hint as one sentence-cased paragraph.
      await expect(
        toasts(page).getByText(sentence, { exact: true })
      ).toBeVisible();
    });
  }

  test("a failure whose message and hint start with skills' names keeps their case", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "changelog");
    const shown = await openDialog(page, "changelog");
    cli.register(
      ["skill", "publish", "changelog"],
      publishReply("changelog", undefined, {
        error: {
          code: "internal",
          // "draft" is another skill in the library: the error toast keeps any skill's name.
          hint: "draft went out in the same push; publish changelog again",
          message: "changelog could not be published: the push was cut off",
          type: "error",
        },
        exitCode: 10,
      })
    );
    await shown.getByRole("button", { name: "Publish" }).click();
    await expect(
      toasts(page).getByText(
        "changelog could not be published: the push was cut off. draft went out in the same push; publish changelog again.",
        { exact: true }
      )
    ).toBeVisible();
  });

  test("a hint that names a command shows the app's words: exit 5, with Add source", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "review");
    const shown = await openDialog(page, "review");
    cli.register(
      ["skill", "publish", "review"],
      publishReply("review", undefined, {
        error: {
          code: "not_found",
          hint: `run 'agentx source add ${ACME}' to add it again, then publish again`,
          message: `review was installed from ${ACME}, which was removed from this machine, so it cannot be published`,
          type: "error",
        },
        exitCode: 5,
      })
    );
    await shown.getByRole("button", { name: "Publish" }).click();
    const toast = toasts(page).getByRole("listitem");
    await expect(
      toast.getByText(
        `review was installed from ${ACME}, which was removed from this machine, so it cannot be published. Add the source again, then publish again.`,
        { exact: true }
      )
    ).toBeVisible();
    cli.register(["source", "add", ACME], {
      events: [{ type: "source", url: ACME }],
    });
    await toast.getByRole("button", { name: "Add source" }).click();
    await expect
      .poll(() => later(cli).at(-1))
      .toEqual(["source", "add", ACME, "--json"]);
  });

  test("a hint that ends by naming skill publish ends with publish again", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "review");
    const shown = await openDialog(page, "review");
    cli.register(
      ["skill", "publish", "review"],
      publishReply("review", undefined, {
        error: {
          code: "refused",
          hint: "move it out of the skill, then run 'agentx skill publish review' again",
          message:
            ".DS_Store cannot be published: agentx installs skills without it, so review was not published",
          type: "error",
        },
        exitCode: 6,
      })
    );
    await shown.getByRole("button", { name: "Publish" }).click();
    await expect(
      toasts(page).getByText(
        ".DS_Store cannot be published: agentx installs skills without it, so review was not published. Move it out of the skill, then publish again.",
        { exact: true }
      )
    ).toBeVisible();
  });

  test("exit 6 with no hub: Set a hub, with Open Settings", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "changelog");
    const shown = await openDialog(page, "changelog");
    cli.register(
      ["skill", "publish", "changelog"],
      publishReply("changelog", undefined, {
        error: {
          code: "refused",
          hint: "run 'agentx source add <url> --hub' to set your hub",
          message: "no hub is set",
          type: "error",
        },
        exitCode: 6,
      })
    );
    await shown.getByRole("button", { name: "Publish" }).click();
    await expect(toasts(page).getByText("Set a hub to publish")).toBeVisible();
    await toasts(page).getByRole("button", { name: "Open Settings" }).click();
    await expect(page).toHaveURL(/#\/settings$/u);
  });
});
