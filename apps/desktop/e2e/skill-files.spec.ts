import type { Locator, Page } from "@playwright/test";

import type { InventoryEvent } from "#/cli/events.gen.ts";
import { locationsOf, projectRows } from "#/screens/skills/projection.ts";

import {
  adoption,
  deletedWithFrontMatter,
  fileReads,
  filesOf,
  history,
  inventory,
  meetingNotes,
  releaseNotesDiffs,
  reviewDiffs,
} from "./fixtures/skills.ts";
import { expect, libraryPath, loadLines, test } from "./harness/cli.ts";
import type { MockCli } from "./harness/cli.ts";

const launchLines = [
  ["version", "--json"],
  ["doctor", "--json"],
];

const isLaunchOrLoad = (line: string[]) =>
  [...launchLines, ...loadLines].some(
    (known) => known.join(" ") === line.join(" ")
  );

/** The command lines the Skills screen ran, but the history reads, which e2e/skills.spec.ts covers. */
const later = (cli: MockCli) =>
  cli.commands.filter((l) => !isLaunchOrLoad(l) && l[1] !== "history");

/** `later`, in a fixed order: the two reads of a modified skill run at once. */
const sortedLater = (cli: MockCli) => {
  const lines = later(cli).map((l) => l.join(" "));
  lines.sort((a, b) => a.localeCompare(b));
  return lines;
};

const list = (page: Page) => page.getByRole("navigation", { name: "Skills" });
const row = (page: Page, name: string) =>
  list(page).getByRole("button", { exact: true, name });
const pane = (page: Page, name: string) =>
  page
    .getByRole("region", { name: "Skill details" })
    .getByRole("article", { name });
const files = (page: Page, name: string) =>
  pane(page, name).getByRole("region", { name: "Files" });
/** The row of one file in the FILES list. */
const fileRow = (where: Locator, path: string) =>
  where.getByRole("button", { exact: true, name: path });
/** The open pane of one file. */
const content = (where: Locator, path: string) =>
  where.getByRole("region", { exact: true, name: path });
/** The "Show in Finder" button of one file's row. */
const reveal = (where: Locator, path: string) =>
  where.getByRole("button", { exact: true, name: `Show ${path} in Finder` });
/** How the button shows: its wrapper fades it in while the row is hovered or holds the focus. */
const opacity = async (locator: Locator) =>
  await locator.evaluate((el) => {
    let node: Element | null = el;
    while (node !== null && getComputedStyle(node).opacity === "1") {
      node = node.parentElement;
    }
    return node === null ? "1" : getComputedStyle(node).opacity;
  });
/** The paths of the file rows, in order: the buttons that open a row. */
const rowNames = async (where: Locator) =>
  await where
    .getByRole("button")
    .evaluateAll((buttons) =>
      buttons
        .filter(
          (b) =>
            b.hasAttribute("aria-expanded") && !b.hasAttribute("aria-haspopup")
        )
        .map((b) => b.getAttribute("aria-label"))
    );

/** Opens the Skills screen on the fixture machine at `path`, with every read registered. */
const openSkills = async (
  cli: MockCli,
  page: Page,
  path: string,
  machine: typeof inventory = inventory
) => {
  cli.onServeStart({ events: [machine] });
  cli.register(["adopt"], { events: adoption });
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
  await page.goto(path);
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();
  // The store harness shows the activity list, which no screen does yet.
  await page.addScriptTag({ type: "module", url: "/e2e/harness/store.tsx" });
  await page.waitForFunction(() => "store" in window);
};

test.afterEach(async ({ cli, page }) => {
  // Reads only: no refresh went to serve, and the activity list has no entry.
  expect(cli.serveRequests.filter((l) => l.includes('"refresh"'))).toEqual([]);
  await expect(
    page.getByRole("list", { name: "Activity" }).getByRole("listitem")
  ).toHaveCount(0);
});

test("an unmodified skill: every file, SKILL.md first and open, the rest by path", async ({
  cli,
  page,
}) => {
  await openSkills(cli, page, "/#/skills/pdf");
  const section = files(page, "pdf");
  await expect(fileRow(section, "SKILL.md")).toHaveAttribute(
    "aria-expanded",
    "true"
  );
  expect(later(cli)).toEqual([["skill", "files", "pdf", "--json"]]);
  await expect(section).toContainText("Read-only");
  await expect(section).not.toContainText("Compared with");
  expect(await rowNames(section)).toEqual([
    "SKILL.md",
    "assets/logo.png",
    "reference.pdf",
    "reference/forms.md",
  ]);
  await expect(fileRow(section, "SKILL.md")).toContainText("6 lines");
  await expect(fileRow(section, "assets/logo.png")).toContainText("binary");
  await expect(fileRow(section, "reference.pdf")).toContainText(
    "Too large to show (303 KiB)"
  );
  await expect(fileRow(section, "reference/forms.md")).toContainText("4 lines");
  await expect(fileRow(section, "reference/forms.md")).toHaveAttribute(
    "aria-expanded",
    "false"
  );

  // SKILL.md is open and shown as it is: nothing added or removed.
  const skillMd = content(section, "SKILL.md");
  await expect(skillMd).toBeVisible();
  await expect(skillMd).toContainText("name: pdf");
  await expect(skillMd).toContainText("Use this skill to pdf.");
  await expect(skillMd.getByRole("insertion")).toHaveCount(0);
  await expect(skillMd.getByRole("deletion")).toHaveCount(0);

  // Another file opens beside it; the binary and the oversized file show their placeholders.
  await fileRow(section, "reference/forms.md").click();
  await expect(content(section, "reference/forms.md")).toContainText(
    "Fill in every field."
  );
  await expect(skillMd).toBeVisible();
  await fileRow(section, "assets/logo.png").click();
  await expect(content(section, "assets/logo.png")).toContainText(
    "Binary file (8 KiB)"
  );
  await fileRow(section, "reference.pdf").click();
  await expect(content(section, "reference.pdf")).toContainText(
    "Too large to show (303 KiB)"
  );
  await fileRow(section, "SKILL.md").click();
  await expect(skillMd).toBeHidden();

  // The files were read once: selecting the skill again does not read them again.
  await row(page, "gone").click();
  await expect(files(page, "gone")).toBeVisible();
  await row(page, "pdf").click();
  await expect(fileRow(files(page, "pdf"), "SKILL.md")).toBeVisible();
  expect(later(cli)).toEqual([
    ["skill", "files", "pdf", "--json"],
    ["skill", "files", "gone", "--json"],
  ]);
});

test("a modified skill of a source: its edits labelled and tinted, against the version installed", async ({
  cli,
  page,
}) => {
  await openSkills(cli, page, "/#/skills/review");
  const section = files(page, "review");
  await expect(section).toContainText(
    "Compared with the version you installed"
  );
  expect(sortedLater(cli)).toEqual([
    "skill diff review --json",
    "skill files review --json",
  ]);
  // The deleted file is listed with the others, in path order.
  expect(await rowNames(section)).toEqual([
    "SKILL.md",
    "assets/logo.png",
    "notes.md",
    "old.md",
    "reference.pdf",
    "reference/forms.md",
  ]);
  await expect(fileRow(section, "SKILL.md")).toContainText("edited by you");
  await expect(fileRow(section, "notes.md")).toContainText("added by you");
  await expect(fileRow(section, "old.md")).toContainText("removed by you");
  await expect(fileRow(section, "old.md")).toContainText("2 lines");
  await expect(fileRow(section, "reference/forms.md")).not.toContainText(
    "by you"
  );

  // The edited file: the whole file, with the line it replaced put back as removed.
  const skillMd = content(section, "SKILL.md");
  await expect(skillMd.getByRole("deletion")).toHaveText([
    "Use this skill to review a change.",
  ]);
  await expect(skillMd.getByRole("insertion")).toHaveText([
    "Use this skill to review a change before merging.",
    "Always run the tests first.",
  ]);
  await expect(skillMd).toContainText("name: review");
  // The added file is all insertions; the deleted file is its old content, all deletions.
  await fileRow(section, "notes.md").click();
  await expect(content(section, "notes.md").getByRole("insertion")).toHaveText([
    "My own notes.",
  ]);
  await fileRow(section, "old.md").click();
  await expect(content(section, "old.md").getByRole("deletion")).toHaveText([
    "An old note.",
    "Nobody reads it.",
  ]);
});

test("a modified skill of your own compares with what was last published", async ({
  cli,
  page,
}) => {
  await openSkills(cli, page, "/#/skills/release-notes");
  const section = files(page, "release-notes");
  await expect(section).toContainText("Compared with what you last published");
  await expect(fileRow(section, "SKILL.md")).toContainText("edited by you");
  await expect(content(section, "SKILL.md").getByRole("insertion")).toHaveText([
    "Use this skill to write release notes from merged changes.",
  ]);
  expect(sortedLater(cli)).toEqual([
    "skill diff release-notes --json",
    "skill files release-notes --json",
  ]);
});

test("a plugin's skill is read as <plugin>:<name>, a client's own as --configuration; no diff", async ({
  cli,
  page,
}) => {
  await openSkills(cli, page, "/#/skills/skill%3Ah-commit");
  const section = files(page, "commit-helper");
  await expect(fileRow(section, "SKILL.md")).toBeVisible();
  await expect(section).not.toContainText("Compared with");
  expect(later(cli)).toEqual([
    ["skill", "files", "acme-tools:commit-helper", "--json"],
  ]);

  await row(page, "notes").click();
  await expect(fileRow(files(page, "notes"), "SKILL.md")).toBeVisible();
  expect(later(cli)).toEqual([
    ["skill", "files", "acme-tools:commit-helper", "--json"],
    ["skill", "files", "notes", "--configuration", "cursor", "--json"],
  ]);
});

test("files that cannot be read say so, and are read again on the next selection", async ({
  cli,
  page,
}) => {
  await openSkills(cli, page, "/#/skills/pdf");
  cli.register(["skill", "files", "gone"], {
    events: [{ code: "not_found", message: "gone is not here", type: "error" }],
    exitCode: 5,
  });
  await row(page, "gone").click();
  await expect(files(page, "gone")).toContainText(
    "The files could not be read"
  );
  await page.getByRole("button", { name: "Dismiss" }).click();
  await row(page, "pdf").click();
  await row(page, "gone").click();
  // The failed read stays shown while the files are read again, as a failed history does, so the
  // text alone does not say the read ran: wait for the command line itself.
  await expect
    .poll(() => later(cli))
    .toEqual([
      ["skill", "files", "pdf", "--json"],
      ["skill", "files", "gone", "--json"],
      ["skill", "files", "gone", "--json"],
    ]);
  await expect(files(page, "gone")).toContainText(
    "The files could not be read"
  );
});

test("each file on disk can be shown in Finder from its row", async ({
  cli,
  page,
}) => {
  await openSkills(cli, page, "/#/skills/pdf");
  const section = files(page, "pdf");
  // The button is there, in the tab order, and shows while the row is hovered or holds the focus.
  const button = reveal(section, "SKILL.md");
  await expect(button).toHaveAttribute("title", "Show in Finder");
  expect(await opacity(button)).toBe("0");
  await fileRow(section, "SKILL.md").hover();
  await expect.poll(async () => await opacity(button)).toBe("1");
  await page.mouse.move(0, 0);
  await expect.poll(async () => await opacity(button)).toBe("0");
  await button.focus();
  await expect.poll(async () => await opacity(button)).toBe("1");

  // A click shows the file at the library entry, and leaves the row as it was.
  await expect(fileRow(section, "SKILL.md")).toHaveAttribute(
    "aria-expanded",
    "true"
  );
  await button.click();
  expect(cli.reveals).toEqual([["/home/me/.agents/skills/pdf/SKILL.md"]]);
  await expect(fileRow(section, "SKILL.md")).toHaveAttribute(
    "aria-expanded",
    "true"
  );
  await reveal(section, "assets/logo.png").click();
  expect(cli.reveals).toEqual([
    ["/home/me/.agents/skills/pdf/SKILL.md"],
    ["/home/me/.agents/skills/pdf/assets/logo.png"],
  ]);
  await expect(fileRow(section, "assets/logo.png")).toHaveAttribute(
    "aria-expanded",
    "false"
  );

  // A plugin's skill is shown where its occurrence resolves.
  await row(page, "commit-helper").click();
  await reveal(files(page, "commit-helper"), "reference/forms.md").click();
  expect(cli.reveals.at(-1)).toEqual([
    "/home/me/.claude/plugins/acme-tools/skills/commit-helper/reference/forms.md",
  ]);

  // A file the edits removed is not on disk: its row has no button.
  await row(page, "review").click();
  const review = files(page, "review");
  await expect(reveal(review, "notes.md")).toBeAttached();
  await expect(fileRow(review, "old.md")).toBeVisible();
  await expect(reveal(review, "old.md")).toHaveCount(0);
  // One of your own skills is shown at its library entry too, which leads into its worktree.
  await row(page, "pdf-letters").click();
  await reveal(files(page, "pdf-letters"), "SKILL.md").click();
  expect(cli.reveals.at(-1)).toEqual([`${libraryPath}/pdf-letters/SKILL.md`]);
});

test("the places of a skill come from the inventory, the library first", () => {
  // The fixture inventory with the fields serve adds to it.
  const whole: InventoryEvent = {
    edges: [],
    instance_id: "instance-1",
    machine: { id: "machine-1", label: "studio-mac" },
    mcp_servers: [],
    schema_version: 2,
    warnings: [],
    ...inventory,
  };
  const rows = projectRows({ adoption, inventory: whole, updates: {} });
  // `library` is where agentx keeps the library, or null while that is not known.
  const places = (name: string, library: string | null) =>
    locationsOf(
      rows.find((r) => r.name === name) ?? rows[0],
      inventory.configurations,
      library ?? undefined
    ).map((l) => [l.label, l.path, l.directory]);
  // A library skill: the library entry, then each placement in a client's own directory, which
  // opens where it resolves; one of your own resolves into its worktree.
  expect(places("pdf", libraryPath)).toEqual([
    ["Library", `${libraryPath}/pdf`, `${libraryPath}/pdf`],
    ["Claude Code", "/home/me/.claude-code/skills/pdf", `${libraryPath}/pdf`],
  ]);
  expect(places("pdf-letters", libraryPath)).toEqual([
    ["Library", `${libraryPath}/pdf-letters`, `${libraryPath}/pdf-letters`],
    [
      "Claude Code",
      "/home/me/.claude-code/skills/pdf-letters",
      "/home/me/.agentx/worktrees/pdf-letters/pdf-letters",
    ],
  ]);
  // A plugin's skill and a skill a client holds on its own: each occurrence, named by its agent.
  expect(places("commit-helper", libraryPath)).toEqual([
    [
      "Claude Code (acme-tools plugin)",
      "/home/me/.claude-code/plugins/acme-tools/skills/commit-helper",
      "/home/me/.claude/plugins/acme-tools/skills/commit-helper",
    ],
  ]);
  expect(places("notes", libraryPath)).toEqual([
    [
      "Cursor",
      "/home/me/.cursor/skills/notes",
      "/home/me/.cursor/skills/notes",
    ],
  ]);
  // While the library's path is unknown, a library skill's placements are all it has.
  expect(places("pdf", null)).toEqual([
    ["Claude Code", "/home/me/.claude-code/skills/pdf", `${libraryPath}/pdf`],
  ]);
  expect(places("draft", null)).toEqual([]);
});

test("a removed file's line count is the count of the lines its pane shows", async ({
  cli,
  page,
}) => {
  await openSkills(cli, page, "/#/skills/pdf");
  cli.register(["skill", "diff", "review"], {
    events: [...reviewDiffs, deletedWithFrontMatter],
  });
  await row(page, "review").click();
  const section = files(page, "review");
  const removed = fileRow(section, "archive/OLD.md");
  await expect(removed).toContainText("removed by you");
  await expect(removed).toContainText("6 lines");
  await removed.click();
  const deletions = content(section, "archive/OLD.md").getByRole("deletion");
  await expect(deletions).toHaveCount(6);
  await expect(deletions.first()).toHaveText("---");
  await expect(deletions.last()).toHaveText("Gone.");
});

test("a failed read of the edits is read again on the next selection", async ({
  cli,
  page,
}) => {
  await openSkills(cli, page, "/#/skills/pdf");
  cli.register(["skill", "diff", "review"], {
    events: [
      { code: "refused", message: "review cannot be compared", type: "error" },
    ],
    exitCode: 6,
  });
  await row(page, "review").click();
  const section = files(page, "review");
  // The files show without their edits, and the caption says so.
  await expect(section).toContainText("The edits could not be read");
  await expect(fileRow(section, "SKILL.md")).toContainText("7 lines");
  await expect(fileRow(section, "SKILL.md")).not.toContainText("by you");
  await page.getByRole("button", { name: "Dismiss" }).click();
  await row(page, "pdf").click();
  await row(page, "review").click();
  await expect
    .poll(() => sortedLater(cli))
    .toEqual([
      "skill diff review --json",
      "skill diff review --json",
      "skill files pdf --json",
      "skill files review --json",
      "skill files review --json",
    ]);
});

test("a skill two clients hold is read and shown from the same copy, by its directory", async ({
  cli,
  page,
}) => {
  cli.register(
    ["skill", "files", "meeting-notes", "--configuration", "claude-code"],
    { events: filesOf("meeting-notes") }
  );
  await openSkills(cli, page, "/#/skills/skill%3Ah-meeting", {
    ...inventory,
    skills: [...inventory.skills, meetingNotes],
  });
  const section = files(page, "Meeting Notes");
  await expect(fileRow(section, "SKILL.md")).toBeVisible();
  // The copy that comes first by configuration, named by its directory, not its frontmatter name.
  expect(later(cli)).toEqual([
    [
      "skill",
      "files",
      "meeting-notes",
      "--configuration",
      "claude-code",
      "--json",
    ],
  ]);
  await reveal(section, "SKILL.md").click();
  expect(cli.reveals).toEqual([
    ["/home/me/.claude-code/skills/meeting-notes/SKILL.md"],
  ]);
  // The other copy is in the menu, with its path, and opens there.
  await section
    .getByRole("button", { name: "Show SKILL.md in Finder elsewhere" })
    .click();
  // The menu is named by its trigger.
  const menu = page.getByRole("menu", {
    name: "Show SKILL.md in Finder elsewhere",
  });
  await expect(menu.getByRole("menuitem")).toHaveText([
    "Cursor/home/me/.cursor/skills/meeting-notes",
  ]);
  await menu.getByRole("menuitem", { name: /^Cursor/u }).click();
  await expect(menu).toBeHidden();
  expect(cli.reveals.at(-1)).toEqual([
    "/home/me/.cursor/skills/meeting-notes/SKILL.md",
  ]);
});

test("the menu beside Show in Finder lists a skill's other places; one place has no menu", async ({
  cli,
  page,
}) => {
  await openSkills(cli, page, "/#/skills/pdf");
  const section = files(page, "pdf");
  await section
    .getByRole("button", { name: "Show SKILL.md in Finder elsewhere" })
    .click();
  const menu = page.getByRole("menu", {
    name: "Show SKILL.md in Finder elsewhere",
  });
  await expect(menu.getByRole("menuitem")).toHaveText([
    "Claude Code/home/me/.claude-code/skills/pdf",
  ]);
  // The placement is a symlink: the file opens where it resolves, and the row stays as it was.
  await menu.getByRole("menuitem", { name: /^Claude Code/u }).click();
  await expect(menu).toBeHidden();
  expect(cli.reveals).toEqual([[`${libraryPath}/pdf/SKILL.md`]]);
  await expect(fileRow(section, "SKILL.md")).toHaveAttribute(
    "aria-expanded",
    "true"
  );
  // A skill in one place has the button alone.
  await row(page, "notes").click();
  const notes = files(page, "notes");
  await expect(reveal(notes, "SKILL.md")).toBeAttached();
  await expect(
    notes.getByRole("button", { name: "Show SKILL.md in Finder elsewhere" })
  ).toHaveCount(0);
});
