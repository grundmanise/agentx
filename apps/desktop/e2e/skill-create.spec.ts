// The Create skill dialog, from the Skills and the Add skills headers: the name checked as the CLI
// checks it, `skill new` with `--description` only when one is given, the new skill selected under
// YOUR SKILLS once the inventory that follows lists it, and a refused name's message under Name.
import type { Page } from "@playwright/test";

import {
  createdEvents,
  inventoryAfter,
  NEW_DESCRIPTION,
  NEW_SKILL,
} from "./fixtures/skill-create.ts";
import {
  adoption,
  fileReads,
  filesOf,
  history,
  inventory,
  releaseNotesDiffs,
  reviewDiffs,
} from "./fixtures/skills.ts";
import { expect, loadLines, test } from "./harness/cli.ts";
import type { MockCli } from "./harness/cli.ts";

const launchLines = [
  ["version", "--json"],
  ["doctor", "--json"],
];

/** The reads the detail pane runs on its own when a skill is selected. */
const detailReads = new Set(["skill history", "skill files", "skill diff"]);

/** The command lines the dialog ran: everything but the launch, the store's and the pane's reads. */
const later = (cli: MockCli) =>
  cli.commands.filter(
    (l) =>
      ![...launchLines, ...loadLines].some(
        (known) => known.join(" ") === l.join(" ")
      ) && !detailReads.has(l.slice(0, 2).join(" "))
  );

const list = (page: Page) => page.getByRole("navigation", { name: "Skills" });
const row = (page: Page, name: string) =>
  list(page).getByRole("button", { exact: true, name });
const yours = (page: Page) =>
  list(page).getByRole("region", { name: /^YOUR SKILLS/u });
const pane = (page: Page, name: string) =>
  page
    .getByRole("region", { name: "Skill details" })
    .getByRole("article", { name });
const toasts = (page: Page) =>
  page.getByRole("region", { name: /^Notifications/u });
const dialog = (page: Page) =>
  page.getByRole("dialog", { name: "Create skill" });
const nameField = (page: Page) =>
  dialog(page).getByRole("textbox", { name: "Name" });
const descriptionField = (page: Page) =>
  dialog(page).getByRole("textbox", { name: "Description" });
const createButton = (page: Page) =>
  dialog(page).getByRole("button", { exact: true, name: "Create" });

/** Registers the reads of the fixture machine and of the new skill once it is selected. */
const registerReads = (cli: MockCli) => {
  cli.onServeStart({ events: [inventory] });
  cli.register(["adopt"], { events: adoption });
  cli.register(["skill", "history", "pdf-letters"], { events: history });
  cli.register(["skill", "history", "release-notes"], { events: [] });
  cli.register(["skill", "history", "draft"], { events: [] });
  cli.register(["skill", "history", NEW_SKILL], { events: [] });
  for (const read of fileReads) {
    cli.register(["skill", "files", ...read.args], { events: read.events });
  }
  cli.register(["skill", "files", NEW_SKILL], { events: filesOf(NEW_SKILL) });
  cli.register(["skill", "diff", "review"], { events: reviewDiffs });
  cli.register(["skill", "diff", "release-notes"], {
    events: releaseNotesDiffs,
  });
  cli.register(["skill", "diff", "pdf-letters"], { events: [] });
};

/** Opens the Skills screen on the fixture machine and waits for its rows. */
const openSkills = async (cli: MockCli, page: Page) => {
  registerReads(cli);
  await page.goto("/#/skills");
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();
  await expect(row(page, "pdf")).toBeVisible();
};

/** Opens the Create skill dialog from the Skills header. */
const openDialog = async (cli: MockCli, page: Page) => {
  await openSkills(cli, page);
  await page.getByRole("button", { exact: true, name: "Create" }).click();
  await expect(dialog(page)).toBeVisible();
};

/** Runs `check` on each name in turn: a field takes one name at a time. */
const eachName = async (
  names: string[],
  check: (name: string) => Promise<void>
): Promise<void> => {
  const [first, ...rest] = names;
  if (first === undefined) {
    return;
  }
  await check(first);
  await eachName(rest, check);
};

/** Plays the scan that follows `skill new`: the inventory with the new skill, then the answer. */
const answerRefresh = async (cli: MockCli) => {
  await expect.poll(() => cli.serveRequests.length).toBeGreaterThan(0);
  await cli.serveEmit([
    inventoryAfter,
    {
      ok: true,
      request_id: cli.requestIds.at(-1) ?? "",
      scan_counter: 2,
      type: "refresh_complete",
    },
  ]);
};

test("Create in the Skills header runs skill new with the description and selects the new skill", async ({
  cli,
  page,
}) => {
  await openDialog(cli, page);
  await expect(
    dialog(page).getByText(
      "Creates a skill of your own in your library and adds it to your agents."
    )
  ).toBeVisible();
  await expect(nameField(page)).toBeFocused();
  await expect(createButton(page)).toBeDisabled();

  await nameField(page).fill(NEW_SKILL);
  await descriptionField(page).fill(NEW_DESCRIPTION);
  const create = cli.register(
    ["skill", "new", NEW_SKILL, "--description", NEW_DESCRIPTION],
    { events: createdEvents, holdAfter: 0 }
  );
  await createButton(page).click();
  await create.started;
  await expect(createButton(page)).toHaveAttribute("aria-busy", "true");
  create.release();
  await expect(
    toasts(page).getByText(`Created ${NEW_SKILL} skill`)
  ).toBeVisible();

  // The command ended; the inventory that follows has not come: nothing is selected for it yet.
  await expect(dialog(page)).toBeVisible();
  await expect(createButton(page)).toHaveAttribute("aria-busy", "true");
  await expect(row(page, NEW_SKILL)).toHaveCount(0);

  await answerRefresh(cli);
  await expect(dialog(page)).toBeHidden();
  await expect(
    yours(page).getByRole("button", { exact: true, name: NEW_SKILL })
  ).toHaveAttribute("aria-current", "true");
  await expect(pane(page, NEW_SKILL)).toBeVisible();
  expect(page.url()).toContain(`#/skills/${NEW_SKILL}`);
  expect(later(cli)).toEqual([
    ["skill", "new", NEW_SKILL, "--description", NEW_DESCRIPTION, "--json"],
  ]);
});

test("Create skill in the Add skills header runs skill new without --description when it is empty", async ({
  cli,
  page,
}) => {
  registerReads(cli);
  await page.goto("/#/add");
  await expect(page.getByRole("heading", { name: "Add skills" })).toBeVisible();
  await page.getByRole("button", { exact: true, name: "Create skill" }).click();
  await expect(dialog(page)).toBeVisible();

  await nameField(page).fill(NEW_SKILL);
  // Spaces alone are no description.
  await descriptionField(page).fill("   ");
  cli.register(["skill", "new", NEW_SKILL], { events: createdEvents });
  // Enter in a field creates, as the button does.
  await nameField(page).press("Enter");
  await expect(
    toasts(page).getByText(`Created ${NEW_SKILL} skill`)
  ).toBeVisible();

  await answerRefresh(cli);
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();
  await expect(
    yours(page).getByRole("button", { exact: true, name: NEW_SKILL })
  ).toHaveAttribute("aria-current", "true");
  await expect(pane(page, NEW_SKILL)).toBeVisible();
  expect(later(cli)).toEqual([["skill", "new", NEW_SKILL, "--json"]]);
});

test("a name the CLI refuses (exit 6) shows its message under Name and keeps the dialog open", async ({
  cli,
  page,
}) => {
  await openDialog(cli, page);
  await nameField(page).fill("release-notes");
  cli.register(["skill", "new", "release-notes"], {
    events: [
      {
        hint: "choose another name",
        message:
          "the account repo already holds release-notes as one of your own skills",
        type: "error",
      },
    ],
    exitCode: 6,
  });
  await createButton(page).click();

  const alert = dialog(page).getByRole("alert");
  // The message and the hint as one sentence-cased paragraph.
  await expect(alert).toHaveText(
    "The account repo already holds release-notes as one of your own skills. Choose another name."
  );
  await expect(nameField(page)).toHaveAttribute("aria-invalid", "true");
  // The message is in place of the rule, under Name.
  await expect(
    dialog(page).getByText(
      "1 to 64 lowercase letters, digits and single hyphens"
    )
  ).toHaveCount(0);
  await expect(dialog(page)).toBeVisible();
  // The dialog shows the failure: no error toast.
  await expect(
    toasts(page).getByText("account repo already holds", { exact: false })
  ).toHaveCount(0);

  // Changing the name clears it.
  await nameField(page).fill("release-notes-2");
  await expect(alert).toHaveCount(0);
  await expect(nameField(page)).not.toHaveAttribute("aria-invalid");
  // A refusal changed nothing, so Create waits for no inventory: the new name runs at once.
  cli.register(["skill", "new", "release-notes-2"], {
    events: [{ name: "release-notes-2", type: "library_skill" }],
  });
  await createButton(page).click();
  await expect(
    toasts(page).getByText("Created release-notes-2 skill")
  ).toBeVisible();
  expect(later(cli)).toEqual([
    ["skill", "new", "release-notes", "--json"],
    ["skill", "new", "release-notes-2", "--json"],
  ]);
});

test("the name is checked as the CLI checks it before Create runs anything", async ({
  cli,
  page,
}) => {
  await openDialog(cli, page);
  const rule = dialog(page).getByText(
    "1 to 64 lowercase letters, digits and single hyphens"
  );
  await expect(rule).toBeVisible();

  const refused = [
    "My-Skill",
    "my skill",
    "my_skill",
    "-my-skill",
    "my--skill",
    "a".repeat(65),
  ];
  await eachName(refused, async (name) => {
    await nameField(page).fill(name);
    await expect(createButton(page)).toBeDisabled();
    await expect(nameField(page)).toHaveAttribute("aria-invalid", "true");
    await nameField(page).press("Enter");
  });

  // A hyphen at the end is the start of the next part: not refused yet, and not a name either.
  await nameField(page).fill("my-");
  await expect(createButton(page)).toBeDisabled();
  await expect(nameField(page)).not.toHaveAttribute("aria-invalid");

  await eachName(["a", "my-skill-2", "a".repeat(64)], async (name) => {
    await nameField(page).fill(name);
    await expect(createButton(page)).toBeEnabled();
    await expect(nameField(page)).not.toHaveAttribute("aria-invalid");
  });

  // Cancel closes the dialog without running anything.
  await dialog(page)
    .getByRole("button", { exact: true, name: "Cancel" })
    .click();
  await expect(dialog(page)).toBeHidden();
  expect(later(cli)).toEqual([]);
});

test("the description hint turns red and Create is disabled past 1024 characters", async ({
  cli,
  page,
}) => {
  await openDialog(cli, page);
  const hint = dialog(page).getByText(
    "What it does and when to use it, in one or two sentences. Up to 1024 characters."
  );
  await expect(hint).toBeVisible();
  await expect(hint).toHaveClass(/text-text-hint/u);
  await expect(descriptionField(page)).toHaveAccessibleDescription(
    "What it does and when to use it, in one or two sentences. Up to 1024 characters."
  );

  await nameField(page).fill(NEW_SKILL);
  // Counted in characters, not bytes: 1024 two-byte letters still fit.
  await descriptionField(page).fill("é".repeat(1024));
  await expect(createButton(page)).toBeEnabled();
  await expect(descriptionField(page)).not.toHaveAttribute("aria-invalid");

  await descriptionField(page).fill("a".repeat(1025));
  await expect(hint).toHaveClass(/text-tone-danger-fg/u);
  await expect(descriptionField(page)).toHaveAttribute("aria-invalid", "true");
  await expect(createButton(page)).toBeDisabled();
  await descriptionField(page).press("Enter");

  await descriptionField(page).press("Backspace");
  await expect(hint).toHaveClass(/text-text-hint/u);
  await expect(descriptionField(page)).not.toHaveAttribute("aria-invalid");
  await expect(createButton(page)).toBeEnabled();

  await dialog(page)
    .getByRole("button", { exact: true, name: "Cancel" })
    .click();
  await expect(dialog(page)).toBeHidden();
  expect(later(cli)).toEqual([]);
});
