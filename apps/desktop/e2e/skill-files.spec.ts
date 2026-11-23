import type { Locator, Page } from "@playwright/test";

import type { InventoryEvent } from "#/cli/events.gen.ts";
import { locationsOf, projectRows } from "#/screens/skills/projection.ts";

import { manyAgentsInventory, manyConfigurations } from "./fixtures/agents.ts";
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
/** The "Show in file manager" button of one file's row (the suite runs as Linux). */
const reveal = (where: Locator, path: string) =>
  where.getByRole("button", {
    exact: true,
    name: `Show ${path} in file manager`,
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

/** The keys of the reads the kept reads' index in IndexedDB names. */
const keptReads = async (page: Page) => {
  await page.addScriptTag({
    type: "module",
    url: "/e2e/harness/kept-reads.ts",
  });
  await page.waitForFunction(() => "keptReads" in window);
  return await page.evaluate(async () => await window.keptReads());
};

/** Opens the Skills screen on the fixture machine at `path`, with every read registered. */
const openSkills = async (
  cli: MockCli,
  page: Page,
  path: string,
  machine: typeof inventory = inventory
) => {
  cli.onServeStart({ events: [machine] });
  cli.register(["skill", "import"], { events: adoption });
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
};

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
  await expect
    .poll(() => later(cli))
    .toEqual([["skill", "files", "pdf", "--json"]]);
  // The meta comes with the files: the SKILL.md row is there while they are read.
  await expect(fileRow(section, "SKILL.md")).toContainText("6 lines");
  expect(await rowNames(section)).toEqual([
    "SKILL.md",
    "assets/logo.png",
    "reference.pdf",
    "reference/forms.md",
  ]);
  await expect(fileRow(section, "assets/logo.png")).toContainText("binary");
  await expect(fileRow(section, "reference.pdf")).toContainText(
    "Too large to show (303 KiB)"
  );
  await expect(fileRow(section, "reference/forms.md")).toContainText("4 lines");
  await expect(fileRow(section, "reference/forms.md")).toHaveAttribute(
    "aria-expanded",
    "false"
  );

  // SKILL.md is open.
  const skillMd = content(section, "SKILL.md");
  await expect(skillMd).toBeVisible();
  await expect(skillMd).toContainText("name: pdf");
  await expect(skillMd).toContainText("Use this skill to pdf.");

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
  await expect
    .poll(() => later(cli))
    .toEqual([
      ["skill", "files", "pdf", "--json"],
      ["skill", "files", "gone", "--json"],
    ]);
});

test("after a relaunch, a skill's files show from the kept read, without reading them again", async ({
  cli,
  page,
}) => {
  await openSkills(cli, page, "/#/skills/pdf");
  await expect(fileRow(files(page, "pdf"), "SKILL.md")).toContainText(
    "6 lines"
  );
  // The read is kept once the index in IndexedDB names it.
  await expect.poll(async () => await keptReads(page)).toContain("files:pdf");

  const before = cli.commands.length;
  cli.restart();
  await page.reload();
  await expect(content(files(page, "pdf"), "SKILL.md")).toContainText(
    "Use this skill to pdf."
  );
  await row(page, "gone").click();
  await expect(files(page, "gone")).toBeVisible();
  // Only the skill not read before is read: pdf's files come from the kept read.
  await expect
    .poll(() =>
      cli.commands
        .slice(before)
        .filter((l) => l[0] === "skill" && l[1] === "files")
    )
    .toEqual([["skill", "files", "gone", "--json"]]);
});

test("after a relaunch, a kept history shows at once, and the history is read again to replace it", async ({
  cli,
  page,
}) => {
  await openSkills(cli, page, "/#/skills/pdf-letters");
  const article = pane(page, "pdf-letters");
  await expect(article.getByText("Tighten the examples")).toBeVisible();
  await expect
    .poll(async () => await keptReads(page))
    .toContain("history:pdf-letters");

  // Another subject for the same version, such as an amended message.
  const [latest, ...rest] = history;
  const read = cli.register(["skill", "history", "pdf-letters"], {
    events: [{ ...latest, subject: "Tighten the examples, again" }, ...rest],
    holdAfter: 0,
  });
  cli.restart();
  await page.reload();
  // The kept history shows while the read runs.
  await read.started;
  await expect(
    pane(page, "pdf-letters").getByText("Tighten the examples")
  ).toBeVisible();
  read.release();
  await expect(
    pane(page, "pdf-letters").getByText("Tighten the examples, again")
  ).toBeVisible();
});

test("the name, the description and a file's text can be selected and copied", async ({
  cli,
  page,
}) => {
  await openSkills(cli, page, "/#/skills/pdf");
  const article = pane(page, "pdf");
  const selection = async () =>
    await page.evaluate(() => window.getSelection()?.toString().trim());
  const selected = async (target: Locator) => {
    await target.click({ clickCount: 3 });
    return await selection();
  };
  // A line of the file: its text, without its line number. Selected as SKILL.md fades in, it
  // stays selected once the loading pane over it has gone.
  const section = files(page, "pdf");
  expect(
    await selected(content(section, "SKILL.md").getByText("name: pdf"))
  ).toBe("name: pdf");
  await expect(section.getByText("Reading the files…")).toHaveCount(0);
  expect(await selection()).toBe("name: pdf");
  expect(
    await selected(article.getByRole("heading", { exact: true, name: "pdf" }))
  ).toBe("pdf");
  expect(await selected(article.getByText("Read and convert PDF files"))).toBe(
    "Read and convert PDF files"
  );
});

test("a modified skill of a source: its edits labelled and tinted, against the version installed", async ({
  cli,
  page,
}) => {
  await openSkills(cli, page, "/#/skills/review");
  const section = files(page, "review");
  await expect
    .poll(() => sortedLater(cli))
    .toEqual(["skill diff review --json", "skill files review --json"]);
  await expect(fileRow(section, "old.md")).toBeVisible();
  // The deleted file is listed with the others, in path order.
  expect(await rowNames(section)).toEqual([
    "SKILL.md",
    "assets/logo.png",
    "notes.md",
    "old.md",
    "reference.pdf",
    "reference/forms.md",
  ]);
  await expect(fileRow(section, "SKILL.md")).toContainText("edited");
  await expect(fileRow(section, "notes.md")).toContainText("added");
  await expect(fileRow(section, "old.md")).toContainText("removed");
  await expect(fileRow(section, "old.md")).toContainText("2 lines");

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
  await expect(fileRow(section, "SKILL.md")).toContainText("edited");
  await expect(content(section, "SKILL.md").getByRole("insertion")).toHaveText([
    "Use this skill to write release notes from merged changes.",
  ]);
  await expect
    .poll(() => sortedLater(cli))
    .toEqual([
      "skill diff release-notes --json",
      "skill files release-notes --json",
    ]);
});

test("a plugin's skill is read as <plugin>:<name>, a client's own as --configuration", async ({
  cli,
  page,
}) => {
  await openSkills(cli, page, "/#/skills/skill%3Ah-commit");
  const section = files(page, "commit-helper");
  await expect(fileRow(section, "SKILL.md")).toBeVisible();
  await expect
    .poll(() => later(cli))
    .toEqual([["skill", "files", "acme-tools:commit-helper", "--json"]]);

  await row(page, "notes").click();
  await expect(fileRow(files(page, "notes"), "SKILL.md")).toBeVisible();
  await expect
    .poll(() => later(cli))
    .toEqual([
      ["skill", "files", "acme-tools:commit-helper", "--json"],
      ["skill", "files", "notes", "--configuration", "cursor", "--json"],
    ]);
});

test("files that cannot be read say so, and are read again on the next selection", async ({
  cli,
  page,
}) => {
  await openSkills(cli, page, "/#/skills/pdf");
  const read = cli.register(["skill", "files", "gone"], {
    events: [{ code: "not_found", message: "gone is not here", type: "error" }],
    exitCode: 5,
    holdAfter: 0,
  });
  await row(page, "gone").click();
  await read.started;
  // While the files are read, the SKILL.md row every skill has stands open, a spinner in its pane.
  expect(await rowNames(files(page, "gone"))).toEqual(["SKILL.md"]);
  await expect(fileRow(files(page, "gone"), "SKILL.md")).toHaveAttribute(
    "aria-expanded",
    "true"
  );
  await expect(
    content(files(page, "gone"), "SKILL.md").getByRole("status")
  ).toHaveText("Reading the files…");
  read.release();
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

test("a skill that changes while selected keeps its files until the new ones are read", async ({
  cli,
  page,
}) => {
  await openSkills(cli, page, "/#/skills/pdf");
  const section = files(page, "pdf");
  const skillMd = content(section, "SKILL.md");
  await expect(skillMd).toContainText("Use this skill to pdf.");
  // An update gives pdf a new version, whose files take a moment to read.
  const read = cli.register(["skill", "files", "pdf"], {
    events: filesOf("pdf", "Use this skill to read PDF files."),
    holdAfter: 0,
  });
  await cli.serveEmit([
    {
      ...inventory,
      library: inventory.library.map((e) =>
        e.name === "pdf" ? { ...e, content_hash: "h-pdf-next" } : e
      ),
      scan_counter: 2,
      skills: inventory.skills.map((n) =>
        n.name === "pdf" ? { ...n, content_hash: "h-pdf-next" } : n
      ),
    },
  ]);
  await read.started;
  // The files read before stay, with no loading pane, until the new ones replace them.
  await expect(skillMd).toContainText("Use this skill to pdf.");
  await expect(section.getByRole("status")).toHaveCount(0);
  read.release();
  await expect(skillMd).toContainText("Use this skill to read PDF files.");
});

test("each file on disk can be shown in the file manager from its row", async ({
  cli,
  page,
}) => {
  await openSkills(cli, page, "/#/skills/pdf");
  const section = files(page, "pdf");
  const button = reveal(section, "SKILL.md");
  await expect(button).toHaveAttribute("title", "Show in file manager");

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
  await page.getByRole("button", { name: "Collapse all" }).click();
  await page.getByRole("button", { name: "Expand all" }).click();
  await row(page, "commit-helper").click();
  await reveal(files(page, "commit-helper"), "reference/forms.md").click();
  expect(cli.reveals.at(-1)).toEqual([
    "/home/me/.claude/plugins/acme-tools/skills/commit-helper/reference/forms.md",
  ]);

  // One of your own skills is shown at its library entry too, which leads into its worktree.
  await row(page, "pdf-letters").click();
  await reveal(files(page, "pdf-letters"), "SKILL.md").click();
  expect(cli.reveals.at(-1)).toEqual([`${libraryPath}/pdf-letters/SKILL.md`]);
});

test("the places of a skill come from the inventory, the library first", () => {
  // The fixture inventory with the fields serve adds to it.
  const whole: InventoryEvent = {
    edges: [],
    installs: [],
    instance_id: "instance-1",
    machine: { id: "machine-1", label: "studio-mac" },
    mcp_servers: [],
    schema_version: 4,
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
    ).map((l) => [l.label, l.path, l.directory, l.symlink]);
  // A library skill: the library entry, then each placement in a client's own directory, which
  // opens where it resolves; one of your own resolves into its worktree.
  expect(places("pdf", libraryPath)).toEqual([
    ["Library", `${libraryPath}/pdf`, `${libraryPath}/pdf`, false],
    [
      "Claude Code",
      "/home/me/.claude-code/skills/pdf",
      `${libraryPath}/pdf`,
      true,
    ],
  ]);
  expect(places("pdf-letters", libraryPath)).toEqual([
    [
      "Library",
      `${libraryPath}/pdf-letters`,
      `${libraryPath}/pdf-letters`,
      false,
    ],
    [
      "Claude Code",
      "/home/me/.claude-code/skills/pdf-letters",
      "/home/me/.agentx/worktrees/pdf-letters/pdf-letters",
      true,
    ],
  ]);
  // A symlink into the library is called the library; one into a worktree is not.
  const intoLibrary = (name: string) =>
    locationsOf(
      rows.find((r) => r.name === name) ?? rows[0],
      inventory.configurations,
      libraryPath
    ).map((l) => l.intoLibrary);
  expect(intoLibrary("pdf")).toEqual([false, true]);
  expect(intoLibrary("pdf-letters")).toEqual([false, false]);
  // Symlinks that lead to one place are one place, each agent once.
  const pdf = rows.find((r) => r.name === "pdf") ?? rows[0];
  const cursorLinks = [
    "/home/me/.claude-code/skills/pdf",
    "/home/me/.cursor/skills/pdf",
  ];
  const linkedTwice = {
    ...pdf,
    entry:
      pdf.entry === undefined
        ? undefined
        : {
            ...pdf.entry,
            placements: [
              ...pdf.entry.placements,
              ...cursorLinks.map((path) => ({
                configuration: "cursor",
                kind: "symlink" as const,
                mode: "symlink" as const,
                path,
              })),
            ],
          },
    occurrences: [
      ...pdf.occurrences,
      ...cursorLinks.map((path) => ({
        configuration: "cursor",
        id: `occurrence:cursor:${path}`,
        kind: "symlink" as const,
        path,
        resolved_path: `${libraryPath}/pdf`,
        scope: "user" as const,
      })),
    ],
  };
  expect(
    locationsOf(linkedTwice, inventory.configurations, libraryPath).map((l) => [
      l.label,
      l.path,
    ])
  ).toEqual([
    ["Library", `${libraryPath}/pdf`],
    ["Claude Code, Cursor", "/home/me/.claude-code/skills/pdf"],
  ]);
  // A plugin's skill and a skill a client holds on its own: each occurrence, named by its agent.
  expect(places("commit-helper", libraryPath)).toEqual([
    [
      "Claude Code (acme-tools plugin)",
      "/home/me/.claude-code/plugins/acme-tools/skills/commit-helper",
      "/home/me/.claude/plugins/acme-tools/skills/commit-helper",
      false,
    ],
  ]);
  expect(places("notes", libraryPath)).toEqual([
    [
      "Cursor",
      "/home/me/.cursor/skills/notes",
      "/home/me/.cursor/skills/notes",
      false,
    ],
  ]);
  // While the library's path is unknown, a library skill's placements are all it has.
  expect(places("pdf", null)).toEqual([
    [
      "Claude Code",
      "/home/me/.claude-code/skills/pdf",
      `${libraryPath}/pdf`,
      true,
    ],
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
  await expect(removed).toContainText("removed");
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
  await expect
    .poll(() => later(cli))
    .toEqual([
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
  // The action and its menu's trigger are one button group.
  await expect(
    section
      .getByRole("group")
      .filter({
        has: page.getByRole("button", {
          exact: true,
          name: "Show SKILL.md in file manager",
        }),
      })
      .getByRole("button")
  ).toHaveCount(2);
  // The other copy is in the menu, with its path, and opens there.
  await section
    .getByRole("button", { name: "Show SKILL.md in file manager elsewhere" })
    .click();
  // The menu is named by its trigger.
  const menu = page.getByRole("menu", {
    name: "Show SKILL.md in file manager elsewhere",
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

test("the menu beside Show in file manager lists a skill's other places", async ({
  cli,
  page,
}) => {
  await openSkills(cli, page, "/#/skills/pdf");
  const section = files(page, "pdf");
  await section
    .getByRole("button", { name: "Show SKILL.md in file manager elsewhere" })
    .click();
  const menu = page.getByRole("menu", {
    name: "Show SKILL.md in file manager elsewhere",
  });
  // The placement is a symlink into the library: marked as the library, it shows where it leads,
  // and opens the file there.
  await expect(menu.getByRole("menuitem")).toHaveText([
    `Claude CodeLibrary${libraryPath}/pdf`,
  ]);
  await menu.getByRole("menuitem", { name: /^Claude Code/u }).click();
  await expect(menu).toBeHidden();
  expect(cli.reveals).toEqual([[`${libraryPath}/pdf/SKILL.md`]]);
  await expect(fileRow(section, "SKILL.md")).toHaveAttribute(
    "aria-expanded",
    "true"
  );
  // A skill in one place has the button too.
  await row(page, "notes").click();
  await expect(reveal(files(page, "notes"), "SKILL.md")).toBeAttached();
});

test("the menu of a skill in many places stays in the window and scrolls", async ({
  cli,
  page,
}) => {
  await page.setViewportSize({ height: 700, width: 1100 });
  await openSkills(
    cli,
    page,
    "/#/skills/pdf",
    manyAgentsInventory(manyConfigurations.map((c) => c.id))
  );
  await files(page, "pdf")
    .getByRole("button", { name: "Show SKILL.md in file manager elsewhere" })
    .click();
  const menu = page.getByRole("menu", {
    name: "Show SKILL.md in file manager elsewhere",
  });
  const box = await menu.boundingBox();
  expect(box?.y).toBeGreaterThanOrEqual(10);
  expect((box?.y ?? 0) + (box?.height ?? 0)).toBeLessThanOrEqual(690);
  expect(await menu.evaluate((el) => el.scrollHeight > el.clientHeight)).toBe(
    true
  );
  await menu.getByRole("menuitem").last().focus();
  await expect(menu.getByRole("menuitem").last()).toBeInViewport();
});

test("symlinks of many agents that lead to one place are one row of the menu", async ({
  cli,
  page,
}) => {
  // Every agent that keeps its own skills links pdf to the library, but Junie, which has a copy.
  const linked = manyConfigurations
    .filter((c) => !c.reads_library && c.id !== "junie")
    .map((c) => c.id);
  const machine = manyAgentsInventory(linked);
  const junie = "/home/me/.junie/skills/pdf";
  await openSkills(cli, page, "/#/skills/pdf", {
    ...machine,
    library: machine.library.map((entry) =>
      entry.name === "pdf"
        ? {
            ...entry,
            placements: [
              ...entry.placements,
              {
                configuration: "junie",
                kind: "copy" as const,
                mode: "copy" as const,
                path: junie,
              },
            ],
          }
        : entry
    ),
    skills: machine.skills.map((node) =>
      node.name === "pdf"
        ? {
            ...node,
            occurrences: [
              ...node.occurrences,
              ...linked.map((id) => ({
                configuration: id,
                id: `${id}:/home/me/.${id}/skills/pdf`,
                kind: "symlink" as const,
                path: `/home/me/.${id}/skills/pdf`,
                resolved_path: `${libraryPath}/pdf`,
                scope: "user" as const,
              })),
            ],
          }
        : node
    ),
  });
  await files(page, "pdf")
    .getByRole("button", { name: "Show SKILL.md in file manager elsewhere" })
    .click();
  const menu = page.getByRole("menu", {
    name: "Show SKILL.md in file manager elsewhere",
  });
  const items = menu.getByRole("menuitem");
  // The 24 symlinks are one row: four logos, a count of the rest, and where they lead.
  await expect(items).toHaveCount(2);
  const links = items.first();
  await expect(links).toHaveText(`+20Library${libraryPath}/pdf`);
  await expect(links.getByRole("img")).toHaveCount(4);
  await expect(links).toHaveAccessibleName(
    new RegExp(
      `^AdaL, Augment, .+, Zencoder, library, ${libraryPath}/pdf$`,
      "u"
    )
  );
  // Pointing at the count lists all 24, to the menu's left.
  await links.getByText("+20").hover();
  const listed = page.getByRole("list", { name: "Agents" });
  await expect(listed.getByRole("listitem")).toHaveCount(24);
  const [card, menuBox] = await Promise.all([
    listed.boundingBox(),
    menu.boundingBox(),
  ]);
  expect((card?.x ?? 0) + (card?.width ?? 0)).toBeLessThanOrEqual(
    menuBox?.x ?? 0
  );
  // The copy keeps its own row, with its agent's name and path.
  await expect(items.last()).toHaveText(`Junie${junie}`);
  await links.click();
  expect(cli.reveals).toEqual([[`${libraryPath}/pdf/SKILL.md`]]);
});
