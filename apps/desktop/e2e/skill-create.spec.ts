// The Create skill dialog, from the Skills header: the name checked as the CLI
// checks it, `skill new` with `--description` only when one is given, the new skill selected under
// HUB once the inventory that follows lists it, Open editor on the Created toast, or its SKILL.md
// opened in the editor chosen in Settings when Open editor after creating a skill is on, and a
// refused name's message under Name. And the Add skill dialog, which Add opens:
// Find skills runs `source skills` on the pasted link, the skills picked go to the agents chosen
// with one `skill add`, which adds the source, the first is selected once the inventory lists it,
// Cancel after a lookup runs nothing, and a refusal shows under the field.
import type { Page } from "@playwright/test";

import {
  ADD_LINK,
  ADDED_SKILLS,
  addedEvents,
  ALREADY_ADDED,
  createdEvents,
  inventoryAfter,
  inventoryAfterAdd,
  inventoryBeforeAdd,
  INSTALLED_LINK,
  installedSkillEvents,
  NEW_DESCRIPTION,
  NEW_SKILL,
  sourceSkillEvents,
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
import { expect, libraryPath, loadLines, test } from "./harness/cli.ts";
import type { InlineEvent, MockCli } from "./harness/cli.ts";
import {
  moreAddActions,
  openCreateSkillDialog,
  skillsHeader,
} from "./harness/skills-header.ts";

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
const hubGroup = (page: Page) =>
  list(page).getByRole("region", { name: /^HUB/u });
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
const registerReads = (cli: MockCli, machine: InlineEvent = inventory) => {
  cli.onServeStart({ events: [machine] });
  cli.register(["skill", "import"], { events: adoption });
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
  await openCreateSkillDialog(page);
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

/** Plays the scan that follows a command: the inventory with the new skill, then the answer. */
const answerRefresh = async (cli: MockCli, after = inventoryAfter) => {
  await expect.poll(() => cli.serveRequests.length).toBeGreaterThan(0);
  await cli.serveEmit([
    after,
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
    dialog(page).getByText("Add a new skill to your agents.")
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

  // The command ended; the inventory that follows has not come: the dialog stays busy.
  await expect(dialog(page)).toBeVisible();
  await expect(createButton(page)).toHaveAttribute("aria-busy", "true");

  await answerRefresh(cli);
  await expect(dialog(page)).toBeHidden();
  await expect(
    hubGroup(page).getByRole("button", { exact: true, name: NEW_SKILL })
  ).toHaveAttribute("aria-current", "true");
  await expect(pane(page, NEW_SKILL)).toBeVisible();
  expect(page.url()).toContain(`#/skills/${NEW_SKILL}`);
  expect(later(cli)).toEqual([
    ["skill", "new", NEW_SKILL, "--description", NEW_DESCRIPTION, "--json"],
  ]);
});

/** Opens the Skills screen on `name`, a skill whose reads `registerReads` answers. */
const openOnSkill = async (
  cli: MockCli,
  page: Page,
  name: string,
  machine: InlineEvent = inventory
) => {
  registerReads(cli, machine);
  await page.goto(`/#/skills/${name}`);
  await expect(pane(page, name)).toBeVisible();
};

const addDialog = (page: Page) =>
  page.getByRole("dialog", { name: "Add skill" });
const linkField = (page: Page) =>
  addDialog(page).getByRole("textbox", { name: "Link to skills" });
const findButton = (page: Page) =>
  addDialog(page).getByRole("button", { exact: true, name: "Find skills" });
const installButton = (page: Page) =>
  addDialog(page).getByRole("button", { exact: true, name: "Install" });
const found = (page: Page) =>
  addDialog(page).getByRole("list", { name: "Skills" });
const agent = (page: Page, name: string) =>
  addDialog(page)
    .getByRole("group", { name: "Add to" })
    .getByRole("button", { exact: true, name });

/** Opens the Add skill dialog from Add in the Skills header. */
const openAddDialog = async (cli: MockCli, page: Page) => {
  await openOnSkill(cli, page, "release-notes", inventoryBeforeAdd);
  await skillsHeader(page)
    .getByRole("button", { exact: true, name: "Add" })
    .click();
  await expect(addDialog(page)).toBeVisible();
  await expect(linkField(page)).toBeFocused();
};

test("Add skill finds the skills under a link, and installs the two picked in the agents chosen with one skill add", async ({
  cli,
  page,
}) => {
  await openAddDialog(cli, page);
  await expect(linkField(page)).toHaveAccessibleDescription(
    "A GitHub or GitLab link to a repository, a folder or a skill"
  );
  await expect(findButton(page)).toBeDisabled();
  await expect(installButton(page)).toBeDisabled();
  for (const name of ADDED_SKILLS) {
    cli.register(["skill", "files", name], { events: filesOf(name) });
  }

  // Enter in the field finds the skills under the link.
  await linkField(page).fill(` ${ADD_LINK} `);
  const lookup = cli.register(["source", "skills", ADD_LINK], {
    events: sourceSkillEvents,
    holdAfter: 0,
  });
  await linkField(page).press("Enter");
  await lookup.started;
  await expect(findButton(page)).toHaveAttribute("aria-busy", "true");
  await expect(installButton(page)).toBeDisabled();
  lookup.release();

  // The selectable skills by name, then the one this machine holds, which shows as added.
  await expect(found(page).getByRole("listitem")).toHaveText([
    /^docx/u,
    /^frontend-design/u,
    /^xlsx/u,
    new RegExp(`^${ALREADY_ADDED}.*Added$`, "u"),
  ]);
  expect(
    await found(page)
      .getByRole("checkbox")
      .evaluateAll((boxes) => boxes.map((b) => b.getAttribute("aria-label")))
  ).toEqual(["docx", "frontend-design", "xlsx"]);
  await expect(installButton(page)).toBeDisabled();

  await found(page).getByText("docx", { exact: true }).click();
  await found(page).getByRole("checkbox", { name: "xlsx" }).click();
  await expect(addDialog(page).getByText("2 of 3 selected")).toBeVisible();

  // The enabled agents are chosen; Cursor is turned off.
  await expect(agent(page, "Claude Code")).toHaveAttribute(
    "aria-pressed",
    "true"
  );
  await agent(page, "Cursor").click();
  await expect(agent(page, "Cursor")).toHaveAttribute("aria-pressed", "false");

  const add = cli.register(
    [
      "skill",
      "add",
      ADD_LINK,
      "--name",
      "docx",
      "--name",
      "xlsx",
      "--to",
      "claude-code",
    ],
    { events: addedEvents, holdAfter: 0 }
  );
  await installButton(page).click();
  await add.started;
  await expect(installButton(page)).toHaveAttribute("aria-busy", "true");
  add.release();
  await expect(
    toasts(page).getByText("Installed 2 skills from anthropics/skills")
  ).toBeVisible();

  // The command ended; the inventory that follows has not come: the dialog stays busy.
  await expect(installButton(page)).toHaveAttribute("aria-busy", "true");
  await answerRefresh(cli, inventoryAfterAdd);
  await expect(addDialog(page)).toBeHidden();
  await expect(row(page, "docx")).toHaveAttribute("aria-current", "true");
  await expect(pane(page, "docx")).toBeVisible();
  expect(later(cli)).toEqual([
    ["source", "skills", ADD_LINK, "--json"],
    [
      "skill",
      "add",
      ADD_LINK,
      "--name",
      "docx",
      "--name",
      "xlsx",
      "--to",
      "claude-code",
      "--json",
    ],
  ]);
});

test("a link whose skills are all installed says so, and Cancel then runs nothing", async ({
  cli,
  page,
}) => {
  await openAddDialog(cli, page);
  cli.register(["source", "skills", INSTALLED_LINK], {
    events: installedSkillEvents,
  });
  await linkField(page).fill(INSTALLED_LINK);
  await findButton(page).click();

  await expect(
    addDialog(page).getByText(
      "Every skill under this link is already installed"
    )
  ).toBeVisible();
  await expect(found(page).getByRole("listitem")).toHaveText([
    new RegExp(`^${ALREADY_ADDED}.*Added$`, "u"),
  ]);
  await expect(installButton(page)).toBeDisabled();

  await addDialog(page)
    .getByRole("button", { exact: true, name: "Cancel" })
    .click();
  await expect(addDialog(page)).toBeHidden();
  expect(later(cli)).toEqual([["source", "skills", INSTALLED_LINK, "--json"]]);
});

test("a link git cannot reach shows the refusal under the field, and a change to the link clears it", async ({
  cli,
  page,
}) => {
  await openAddDialog(cli, page);
  const link = "https://github.com/acme/missing";
  cli.register(["source", "skills", link], {
    events: [
      {
        hint: "check the URL and your access to it",
        message: "git could not read https://github.com/acme/missing",
        type: "error",
      },
    ],
    exitCode: 3,
  });
  await linkField(page).fill(link);
  await findButton(page).click();

  const alert = addDialog(page).getByRole("alert");
  await expect(alert).toHaveText(
    `Couldn't reach ${link}. Check the URL and your access to it.`
  );
  await expect(linkField(page)).toHaveAttribute("aria-invalid", "true");
  await expect(installButton(page)).toBeDisabled();

  await linkField(page).fill("https://github.com/acme/skills");
  await expect(alert).toHaveCount(0);
  await expect(linkField(page)).not.toHaveAttribute("aria-invalid");
  expect(later(cli)).toEqual([["source", "skills", link, "--json"]]);
});

test("Add's three-dots button opens a menu with Create skill, from the pointer and the keyboard; Escape, the trigger and a click outside close it", async ({
  cli,
  page,
}) => {
  await openOnSkill(cli, page, "release-notes");
  const menu = page.getByRole("menu");
  const create = page.getByRole("menuitem", { name: "Create skill" });

  await moreAddActions(page).click();
  await expect(menu).toBeVisible();
  await expect(page.getByRole("menuitem")).toHaveText(["Create skill"]);
  await page.keyboard.press("Escape");
  await expect(menu).toBeHidden();
  await expect(moreAddActions(page)).toBeFocused();

  // The trigger keeps the focus: Enter, Space and ArrowDown open the menu.
  const opensWith = async (key: string) => {
    await page.keyboard.press(key);
    await expect(create).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(menu).toBeHidden();
    await expect(moreAddActions(page)).toBeFocused();
  };
  await opensWith("Enter");
  await opensWith("Space");
  await opensWith("ArrowDown");

  // The trigger closes the menu it opened.
  await moreAddActions(page).click();
  await expect(menu).toBeVisible();
  await moreAddActions(page).click();
  await expect(menu).toBeHidden();

  // The page under the menu stays live: a click outside closes the menu and acts on what it hits.
  await moreAddActions(page).click();
  await expect(menu).toBeVisible();
  await row(page, "pdf").click();
  await expect(menu).toBeHidden();
  await expect(pane(page, "pdf")).toBeVisible();

  await moreAddActions(page).click();
  await create.click();
  await expect(menu).toBeHidden();
  await expect(dialog(page)).toBeVisible();
  await expect(nameField(page)).toBeFocused();
});

test("Create skill runs skill new without --description when it is empty", async ({
  cli,
  page,
}) => {
  await openDialog(cli, page);

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
    hubGroup(page).getByRole("button", { exact: true, name: NEW_SKILL })
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
        message: "the store already holds release-notes as a hub skill",
        type: "error",
      },
    ],
    exitCode: 6,
  });
  await createButton(page).click();

  const alert = dialog(page).getByRole("alert");
  // The message and the hint as one sentence-cased paragraph.
  await expect(alert).toHaveText(
    "The store already holds release-notes as a hub skill. Choose another name."
  );
  await expect(nameField(page)).toHaveAttribute("aria-invalid", "true");
  await expect(dialog(page)).toBeVisible();

  // Changing the name clears it.
  await nameField(page).fill("release-notes-2");
  await expect(alert).toHaveCount(0);
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

test("a refusal that starts with the typed name keeps its case", async ({
  cli,
  page,
}) => {
  await openDialog(cli, page);
  await nameField(page).fill("notes");
  cli.register(["skill", "new", "notes"], {
    events: [
      {
        hint: "choose another name",
        message: "notes is already a skill in your library",
        type: "error",
      },
    ],
    exitCode: 6,
  });
  await createButton(page).click();
  await expect(dialog(page).getByRole("alert")).toHaveText(
    "notes is already a skill in your library. Choose another name."
  );
});

test("a failure whose hint names agentx doctor names Run checks in Settings instead", async ({
  cli,
  page,
}) => {
  await openDialog(cli, page);
  await nameField(page).fill("notes");
  cli.register(["skill", "new", "notes"], {
    events: [
      {
        code: "internal",
        hint: "run 'agentx doctor' and check the library it names",
        message: "the library holds no notes after creating it",
        type: "error",
      },
    ],
    exitCode: 10,
  });
  await createButton(page).click();
  await expect(dialog(page).getByRole("alert")).toHaveText(
    "The library holds no notes after creating it. Use Run checks in Settings to check the library."
  );
});

test("the name is checked as the CLI checks it", async ({ cli, page }) => {
  await openDialog(cli, page);
  const rule = dialog(page).getByText(
    "1 to 64 lowercase letters, digits and single hyphens"
  );
  await expect(rule).toBeVisible();

  const refused = [
    "My-Skill",
    "my_skill",
    "-my-skill",
    "my--skill",
    "a".repeat(65),
  ];
  await eachName(refused, async (name) => {
    await nameField(page).fill(name);
    await expect(createButton(page)).toBeDisabled();
    await expect(nameField(page)).toHaveAttribute("aria-invalid", "true");
  });

  // A hyphen at the end is the start of the next part: not refused yet, and not a name either.
  await nameField(page).fill("my-");
  await expect(createButton(page)).toBeDisabled();
  await expect(nameField(page)).not.toHaveAttribute("aria-invalid");

  await eachName(["a", "my-skill-2", "a".repeat(64)], async (name) => {
    await nameField(page).fill(name);
    await expect(createButton(page)).toBeEnabled();
  });

  // Cancel closes the dialog.
  await dialog(page)
    .getByRole("button", { exact: true, name: "Cancel" })
    .click();
  await expect(dialog(page)).toBeHidden();
});

test("a space typed or pasted in Name becomes a hyphen, and the caret stays where it was", async ({
  cli,
  page,
}) => {
  await openDialog(cli, page);
  const field = nameField(page);

  await field.pressSequentially("release notes");
  await expect(field).toHaveValue("release-notes");
  await expect(createButton(page)).toBeEnabled();

  await field.evaluate((input: HTMLInputElement) => {
    input.setSelectionRange("release-".length, "release-".length);
  });
  await field.pressSequentially("pdf big ");
  await expect(field).toHaveValue("release-pdf-big-notes");

  await field.fill("weekly report\tdraft");
  await expect(field).toHaveValue("weekly-report-draft");
});

test("the description is invalid and Create is disabled past 1024 characters", async ({
  cli,
  page,
}) => {
  await openDialog(cli, page);
  const hint = dialog(page).getByText(
    "What it does and when to use it, in one or two sentences. Up to 1024 characters."
  );
  await expect(hint).toBeVisible();
  await expect(descriptionField(page)).toHaveAccessibleDescription(
    "What it does and when to use it, in one or two sentences. Up to 1024 characters."
  );

  await nameField(page).fill(NEW_SKILL);
  // Counted in characters, not bytes: 1024 two-byte letters still fit.
  await descriptionField(page).fill("é".repeat(1024));
  await expect(createButton(page)).toBeEnabled();

  await descriptionField(page).fill("a".repeat(1025));
  await expect(descriptionField(page)).toHaveAttribute("aria-invalid", "true");
  await expect(createButton(page)).toBeDisabled();

  await descriptionField(page).press("Backspace");
  await expect(createButton(page)).toBeEnabled();

  await dialog(page)
    .getByRole("button", { exact: true, name: "Cancel" })
    .click();
  await expect(dialog(page)).toBeHidden();
});

test("Create offers Open editor on its toast and opens no editor by itself", async ({
  cli,
  page,
}) => {
  await openDialog(cli, page);
  await nameField(page).fill(NEW_SKILL);
  cli.register(["skill", "new", NEW_SKILL], { events: createdEvents });
  await createButton(page).click();
  const toast = toasts(page)
    .getByRole("listitem")
    .filter({ hasText: `Created ${NEW_SKILL} skill` });
  await expect(toast).toBeVisible();

  await answerRefresh(cli);
  await expect(
    hubGroup(page).getByRole("button", { exact: true, name: NEW_SKILL })
  ).toHaveAttribute("aria-current", "true");
  expect(cli.editorOpens).toEqual([]);

  await toast.getByRole("button", { exact: true, name: "Open editor" }).click();
  await expect
    .poll(() => cli.editorOpens)
    .toEqual([
      {
        editor: { kind: "system" },
        path: `${libraryPath}/${NEW_SKILL}/SKILL.md`,
      },
    ]);
  await expect(toast).toBeHidden();
  expect(later(cli)).toEqual([["skill", "new", NEW_SKILL, "--json"]]);
});

test("with Open editor after creating a skill on, Create opens the new skill's SKILL.md in the chosen editor once it is selected", async ({
  cli,
  page,
}) => {
  const zed = { id: "zed", kind: "app", name: "Zed" };
  cli.answerEditors([{ id: "zed", name: "Zed" }]);
  cli.savePreferences({ editor: zed, open_after_create: true });
  await openDialog(cli, page);
  await nameField(page).fill(NEW_SKILL);
  cli.register(["skill", "new", NEW_SKILL], { events: createdEvents });
  await createButton(page).click();
  await expect(
    toasts(page).getByText(`Created ${NEW_SKILL} skill`)
  ).toBeVisible();

  await answerRefresh(cli);
  await expect(
    hubGroup(page).getByRole("button", { exact: true, name: NEW_SKILL })
  ).toHaveAttribute("aria-current", "true");
  await expect
    .poll(() => cli.editorOpens)
    .toEqual([{ editor: zed, path: `${libraryPath}/${NEW_SKILL}/SKILL.md` }]);
  await expect(
    toasts(page).getByText(`Created ${NEW_SKILL} skill`)
  ).toBeVisible();
  await expect(
    toasts(page).getByRole("button", { exact: true, name: "Open editor" })
  ).toHaveCount(0);
  expect(later(cli)).toEqual([["skill", "new", NEW_SKILL, "--json"]]);
});

test("with the system default, Create opens SKILL.md through the opener", async ({
  cli,
  page,
}) => {
  cli.savePreferences({ editor: { kind: "system" }, open_after_create: true });
  await openDialog(cli, page);
  await nameField(page).fill(NEW_SKILL);
  cli.register(["skill", "new", NEW_SKILL], { events: createdEvents });
  await createButton(page).click();
  await answerRefresh(cli);
  await expect
    .poll(() => cli.editorOpens)
    .toEqual([
      {
        editor: { kind: "system" },
        path: `${libraryPath}/${NEW_SKILL}/SKILL.md`,
      },
    ]);
  expect(cli.calls).toContain("plugin:opener|open_path");
});
