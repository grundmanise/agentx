// Fork in the Skills detail pane: which skills offer it, the command line each choice in the Fork
// dialog produces, and the refusal that keeps the dialog open; then what each agent sees of the
// skills of one name, which AGENTS offers. The screen redraws from the inventory that follows: the
// fork is selected once serve reports it.
import type { Page } from "@playwright/test";

import { sources } from "./fixtures/agents.ts";
import {
  commitHelperForked,
  forked,
  inventory,
  pdfForkedBeside,
  pdfForkedInPlace,
  refusedInPlace,
  twoPluginsInventory,
} from "./fixtures/fork.ts";
import { adoption, fileReads, filesOf } from "./fixtures/skills.ts";
import { expect, loadLines, test } from "./harness/cli.ts";
import type { MockCli } from "./harness/cli.ts";

const launchLines = [
  ["version", "--json"],
  ["doctor", "--json"],
];

/** The reads the launch, the store and the detail pane run on their own. */
const isRead = (line: string[]) =>
  [...launchLines, ...loadLines].some(
    (known) => known.join(" ") === line.join(" ")
  ) ||
  ["skill history", "skill files", "skill diff"].includes(
    line.slice(0, 2).join(" ")
  );

/** The command lines the Fork controls ran. */
const forks = (cli: MockCli) => cli.commands.filter((l) => !isRead(l));

const toasts = (page: Page) =>
  page.getByRole("region", { name: /^Notifications/u });
const pane = (page: Page, name: string) =>
  page
    .getByRole("region", { name: "Skill details" })
    .getByRole("article", { name });
const actions = (page: Page, name: string) =>
  pane(page, name).getByLabel("Actions", { exact: true });
const forkDialog = (page: Page, name: string) =>
  page.getByRole("dialog", { name: `Fork ${name}?` });

/** Every skill a test opens, the forks included, reads its files and history. */
const libraryNames = [
  "draft",
  "gone",
  "pdf",
  "pdf-letters",
  "release-notes",
  "review",
  "my-pdf",
  "commit-helper",
];

/** Opens a skill: a library skill by its name, any other by its row's key. */
const openSkill = async (
  cli: MockCli,
  page: Page,
  name: string,
  { key = name, first = inventory } = {}
) => {
  cli.onServeStart({ events: [first] });
  cli.register(["adopt"], { events: adoption });
  cli.register(["source", "list"], { events: sources });
  for (const n of libraryNames) {
    cli.register(["skill", "history", n], { events: [] });
    cli.register(["skill", "files", n], { events: filesOf(n) });
    cli.register(["skill", "diff", n], { events: [] });
  }
  for (const read of fileReads) {
    cli.register(["skill", "files", ...read.args], { events: read.events });
  }
  await page.goto(`/#/skills/${encodeURIComponent(key)}`);
  await expect(pane(page, name)).toBeVisible();
};

test("a skill of a source: Fork runs skill fork <name> and the pane redraws from the inventory", async ({
  cli,
  page,
}) => {
  await openSkill(cli, page, "pdf");
  await actions(page, "pdf").getByRole("button", { name: "Fork" }).click();
  const dialog = forkDialog(page, "pdf");
  await expect(dialog).toContainText(
    "A copy will be created in your skill library. Keep the same name to use it in place of this skill, or choose a new name to use both. You can still get updates from the original."
  );
  await expect(
    dialog.getByRole("switch", { name: "Use a different name" })
  ).not.toBeChecked();
  const fork = cli.register(["skill", "fork", "pdf"], {
    events: [forked("pdf")],
    holdAfter: 0,
  });

  await dialog.getByRole("button", { name: "Fork" }).click();
  await fork.started;
  await expect(dialog.getByRole("button", { name: "Fork" })).toHaveAttribute(
    "aria-busy",
    "true"
  );
  fork.release();
  await expect(dialog).toHaveCount(0);
  await expect(
    toasts(page).getByText("Forked pdf skill", { exact: true })
  ).toBeVisible();
  // Nothing predicted: pdf is still the skill of a source until serve reports the fork.
  await expect(pane(page, "pdf")).toContainText("acme/skills");
  await cli.serveEmit([pdfForkedInPlace]);
  await expect(pane(page, "pdf")).toContainText("from acme/skills");
  expect(forks(cli)).toEqual([["skill", "fork", "pdf", "--json"]]);
});

test("Use a different name: Fork runs skill fork <name> --name <new> and selects the fork", async ({
  cli,
  page,
}) => {
  await openSkill(cli, page, "pdf");
  await actions(page, "pdf").getByRole("button", { name: "Fork" }).click();
  const dialog = forkDialog(page, "pdf");
  await dialog.getByText("Use a different name", { exact: true }).click();
  await expect(
    dialog.getByRole("switch", { name: "Use a different name" })
  ).toBeChecked();
  const field = dialog.getByRole("textbox", { name: "Name" });
  await expect(field).toBeFocused();
  const submit = dialog.getByRole("button", { name: "Fork" });
  await expect(submit).toBeDisabled();
  await field.fill("My_PDF");
  await expect(
    dialog.getByText("Use 1 to 64 lowercase letters, digits and single hyphens")
  ).toBeVisible();
  await expect(submit).toBeDisabled();
  await field.fill("my--pdf");
  await expect(submit).toBeDisabled();
  await field.fill("my-pdf");
  await expect(submit).toBeEnabled();
  cli.register(["skill", "fork", "pdf", "--name", "my-pdf"], {
    events: [forked("my-pdf"), forked("pdf")],
  });

  await field.press("Enter");
  await expect(dialog).toHaveCount(0);
  await expect(
    toasts(page).getByText("Forked pdf skill", { exact: true })
  ).toBeVisible();
  await expect(page).toHaveURL(/#\/skills\/my-pdf$/u);
  await cli.serveEmit([pdfForkedBeside]);
  await expect(pane(page, "my-pdf")).toBeVisible();
  await expect(
    page
      .getByRole("navigation", { name: "Skills" })
      .getByRole("region", { name: "PERSONAL" })
      .getByRole("button", { exact: true, name: "my-pdf" })
  ).toHaveAttribute("aria-current", "true");
  expect(forks(cli)).toEqual([
    ["skill", "fork", "pdf", "--name", "my-pdf", "--json"],
  ]);
});

test("refused in place: exit 6 keeps the dialog open with the CLI's words and turns Use a different name on", async ({
  cli,
  page,
}) => {
  await openSkill(cli, page, "pdf");
  await actions(page, "pdf").getByRole("button", { name: "Fork" }).click();
  const dialog = forkDialog(page, "pdf");
  cli.register(["skill", "fork", "pdf"], {
    events: [refusedInPlace],
    exitCode: 6,
  });

  await dialog.getByRole("button", { name: "Fork" }).click();
  const alert = dialog.getByRole("alert");
  // The CLI's message and the dialog's own hint, as one sentence-cased paragraph; the skill's
  // name keeps its case. Exit 6 turns the switch on, so the hint names what is left to do with
  // the form as it now stands.
  await expect(alert).toHaveText(
    "pdf is itself a Git repository, at /home/me/.agents/skills/pdf/.git. Move its .git out of the skill, or type a new name."
  );
  await expect(
    dialog.getByRole("switch", { name: "Use a different name" })
  ).toBeChecked();
  const field = dialog.getByRole("textbox", { name: "Name" });
  await expect(field).toBeFocused();

  cli.register(["skill", "fork", "pdf", "--name", "my-pdf"], {
    events: [forked("my-pdf"), forked("pdf")],
  });
  await field.fill("my-pdf");
  await dialog.getByRole("button", { name: "Fork" }).click();
  await expect(dialog).toHaveCount(0);
  await expect(
    toasts(page).getByText("Forked pdf skill", { exact: true })
  ).toBeVisible();
  await expect(page).toHaveURL(/#\/skills\/my-pdf$/u);
  expect(forks(cli)).toEqual([
    ["skill", "fork", "pdf", "--json"],
    ["skill", "fork", "pdf", "--name", "my-pdf", "--json"],
  ]);
});

test("a refusal clears when the switch or the name changes", async ({
  cli,
  page,
}) => {
  await openSkill(cli, page, "pdf");
  await actions(page, "pdf").getByRole("button", { name: "Fork" }).click();
  const dialog = forkDialog(page, "pdf");
  const toggle = dialog.getByRole("switch", { name: "Use a different name" });
  const alert = dialog.getByRole("alert");
  cli.register(["skill", "fork", "pdf"], {
    events: [refusedInPlace],
    exitCode: 6,
  });

  await dialog.getByRole("button", { name: "Fork" }).click();
  await expect(alert).toContainText("or type a new name.");
  // Turning the switch off, then on again, clears it.
  await toggle.click();
  await expect(toggle).not.toBeChecked();
  await expect(alert).toHaveCount(0);
  await toggle.click();
  await expect(toggle).toBeChecked();
  await expect(alert).toHaveCount(0);

  // Typing a name clears it too.
  await toggle.click();
  await dialog.getByRole("button", { name: "Fork" }).click();
  await expect(alert).toContainText("or type a new name.");
  await dialog.getByRole("textbox", { name: "Name" }).fill("my-pdf");
  await expect(alert).toHaveCount(0);
});

test("refused in place with Use a different name on: the hint leaves the switch out", async ({
  cli,
  page,
}) => {
  await openSkill(cli, page, "pdf");
  await actions(page, "pdf").getByRole("button", { name: "Fork" }).click();
  const dialog = forkDialog(page, "pdf");
  await dialog.getByRole("switch", { name: "Use a different name" }).click();
  // The skill's own name forks it in its place, which the CLI refuses as without the switch.
  await dialog.getByRole("textbox", { name: "Name" }).fill("pdf");
  cli.register(["skill", "fork", "pdf", "--name", "pdf"], {
    events: [refusedInPlace],
    exitCode: 6,
  });
  await dialog.getByRole("button", { name: "Fork" }).click();
  await expect(dialog.getByRole("alert")).toHaveText(
    "pdf is itself a Git repository, at /home/me/.agents/skills/pdf/.git. Move its .git out of the skill."
  );
  expect(forks(cli)).toEqual([
    ["skill", "fork", "pdf", "--name", "pdf", "--json"],
  ]);
});

test("a plugin's skill: Use a different name starts on; turned off, Fork runs skill fork <skill> and AGENTS says who sees two skills", async ({
  cli,
  page,
}) => {
  await openSkill(cli, page, "commit-helper", { key: "skill:h-commit" });
  await actions(page, "commit-helper")
    .getByRole("button", { name: "Fork" })
    .click();
  const dialog = forkDialog(page, "commit-helper");
  await expect(dialog).toContainText(
    "A copy will be created in your skill library. The plugin keeps its own skill."
  );
  const toggle = dialog.getByRole("switch", { name: "Use a different name" });
  await expect(toggle).toBeChecked();
  const field = dialog.getByRole("textbox", { name: "Name" });
  await expect(field).toHaveValue("my-commit-helper");
  await expect(field).toBeFocused();
  await toggle.click();
  await expect(dialog).toContainText(
    "Agents with the plugin will see two skills named commit-helper."
  );
  cli.register(["skill", "fork", "commit-helper"], {
    events: [forked("commit-helper")],
  });

  await dialog.getByRole("button", { name: "Fork" }).click();
  await expect(dialog).toHaveCount(0);
  await expect(
    toasts(page).getByText("Forked commit-helper skill", { exact: true })
  ).toBeVisible();
  await cli.serveEmit([commitHelperForked]);
  const fork = pane(page, "commit-helper");
  await expect(fork).toContainText("personal skill");
  await expect(page).toHaveURL(/#\/skills\/commit-helper$/u);
  expect(forks(cli)).toEqual([["skill", "fork", "commit-helper", "--json"]]);

  // Claude Code has the plugin's skill and a link to the fork; the agents that read the library
  // see the fork alone.
  const agents = fork.getByRole("region", { name: "Agents" });
  const notice = agents.getByRole("note", { name: "Skills of the same name" });
  await expect(notice).toContainText(
    "Claude Code also sees the acme-tools plugin's commit-helper. It picks one of the two."
  );
  await notice.getByText("Details", { exact: true }).hover();
  const table = page.getByRole("table", {
    name: "Skills named commit-helper",
  });
  await expect(table.getByRole("columnheader")).toHaveText([
    "Agent",
    "Yours",
    "acme-tools",
  ]);
  await expect(table.getByRole("rowheader")).toHaveText([
    "Claude Code",
    "Codex, Gemini CLI",
  ]);
  await expect(
    table.getByRole("row", { name: /^Claude Code/u }).getByRole("cell")
  ).toHaveText(["Sees it", "Sees it"]);
  await expect(
    page.getByText(
      "The highlighted agent sees both skills. It's up to the agent to decide which one to use. To prevent possible clashes, rename your skill."
    )
  ).toBeVisible();
});

test("a plugin's skill several plugins provide is named <plugin>:<skill>", async ({
  cli,
  page,
}) => {
  await openSkill(cli, page, "commit-helper", {
    first: twoPluginsInventory,
    key: "skill:h-commit",
  });
  cli.register(["skill", "files", "team-tools:commit-helper"], {
    events: filesOf("commit-helper"),
  });
  await actions(page, "commit-helper")
    .getByRole("button", { name: "Fork" })
    .click();
  const dialog = forkDialog(page, "commit-helper");
  cli.register(
    ["skill", "fork", "acme-tools:commit-helper", "--name", "my-commit-helper"],
    { events: [forked("my-commit-helper")] }
  );
  await dialog.getByRole("button", { name: "Fork" }).click();
  await expect(dialog).toHaveCount(0);
  expect(forks(cli)).toEqual([
    [
      "skill",
      "fork",
      "acme-tools:commit-helper",
      "--name",
      "my-commit-helper",
      "--json",
    ],
  ]);
});
