// The History sheet of a hub skill: each entry expands to the files its commit changed
// and collapses again, and the subject of the commit that created a fork links to its origin's page.
import type { Page } from "@playwright/test";

import {
  isShown,
  metaOf,
  originPage,
  originParts,
  subjectSegments,
} from "#/screens/skills/history.tsx";

import {
  ACME,
  adoption,
  fileReads,
  history,
  inventory,
} from "./fixtures/skills.ts";
import { expect, test } from "./harness/cli.ts";
import type { MockCli } from "./harness/cli.ts";

const SUBJECT = "pdf-letters: edit SKILL.md and add examples.md (studio-mac)";

const sheet = (page: Page) => page.getByRole("dialog", { name: "History" });
const entryButton = (page: Page) =>
  sheet(page).getByRole("button", { exact: true, name: SUBJECT });
const filesOf = (page: Page) =>
  sheet(page).getByRole("list", { name: `Files of ${SUBJECT}` });
const details = (page: Page) =>
  page.getByRole("region", { name: "Skill details" });

/** Starts the app on the fixture machine, with pdf-letters' history and every skill's files. */
const start = (cli: MockCli) => {
  cli.onServeStart({ events: [inventory] });
  cli.register(["skill", "import"], { events: adoption });
  cli.register(["skill", "history", "pdf-letters"], { events: history });
  for (const read of fileReads) {
    cli.register(["skill", "files", ...read.args], { events: read.events });
  }
};

/** Opens pdf-letters, a hub skill, and its History sheet. */
const openSheet = async (cli: MockCli, page: Page) => {
  start(cli);
  await page.goto("/#/skills/pdf-letters");
  await details(page).getByRole("button", { name: "Show all 4" }).click();
  await expect(sheet(page)).toBeVisible();
};

test("an entry expands to its files and collapses again", async ({
  cli,
  page,
}) => {
  await openSheet(cli, page);
  const button = entryButton(page);
  await expect(button).toHaveAttribute("aria-expanded", "false");

  await button.click();
  await expect(button).toHaveAttribute("aria-expanded", "true");
  await expect(filesOf(page).getByRole("listitem")).toHaveText([
    "modifiedSKILL.md",
    "addedexamples.md",
  ]);

  await button.click();
  await expect(button).toHaveAttribute("aria-expanded", "false");
  await expect(filesOf(page)).toHaveCount(0);
});

test("the subject of a fork's first commit links to the skill and the page it came from", async ({
  cli,
  page,
}) => {
  await openSheet(cli, page);
  const subject =
    "Fork pdf-letters from https://github.com/acme/skills/pdf at 3f2a9c1";
  const files = sheet(page).getByRole("button", {
    name: `Show the files of ${subject}`,
  });
  await files.click();
  await expect(files).toHaveAttribute("aria-expanded", "true");

  // The fork's name leads to the skill's folder at the forked commit, the source to its page.
  const links = sheet(page).getByRole("link");
  await expect(links).toHaveText(["pdf-letters", `${ACME}/pdf`]);
  await links.first().click();
  await links.last().click();
  await expect
    .poll(() => cli.openedUrls)
    .toEqual([`${ACME}/tree/3f2a9c1e8d7/pdf`, ACME]);
});

test("a fork's origin links its name, and only to an https page", () => {
  const [first] = history;
  const origin = {
    kind: "plugin" as const,
    plugin: "chrome-devtools-mcp",
    skill: "a11y-debugging",
  };
  const url = "https://github.com/ChromeDevTools/chrome-devtools-mcp";
  const fork = {
    ...first,
    origin: { ...origin, url },
    subject: "Fork a11y-debugging from plugin chrome-devtools-mcp",
  };
  expect(originPage(fork)).toBe(url);
  expect(originParts(fork)).toEqual({
    after: "",
    before: "Fork a11y-debugging from plugin ",
    name: "chrome-devtools-mcp",
  });
  // The skill a fork of a fork came from, named after "from" even when the fork shares the word.
  expect(
    originParts({
      ...first,
      origin: { kind: "skill", skill: "alpha" },
      subject: "Fork alpha-2 from alpha",
    })
  ).toEqual({ after: "", before: "Fork alpha-2 from ", name: "alpha" });
  // A source's path as the CLI writes it into the subject, its runs of spaces as one.
  expect(
    originParts({
      ...first,
      origin: {
        kind: "source",
        path: "tools/two  words",
        skill: "words",
        source: "https://github.com/acme/skills",
      },
      subject:
        "Fork words from https://github.com/acme/skills/tools/two words at 3f2a9c1",
    })?.name
  ).toBe("https://github.com/acme/skills/tools/two words");
  expect(originParts({ ...fork, subject: "Rename it" })).toBeUndefined();
  // Only a fork of a source's skill links the fork's name.
  expect(subjectSegments(fork).map((r) => r.page)).toEqual([undefined, url]);

  expect(originPage({ ...fork, origin })).toBeUndefined();
  expect(
    originPage({ ...fork, origin: { ...origin, url: "http://example.com" } })
  ).toBeUndefined();
  expect(originPage(first)).toBeUndefined();
});

test("the meta line names the machine, and stored upstream versions are left out", () => {
  const at = {
    ...history[0],
    files: [{ path: "SKILL.md", status: "modified" as const }],
    time: "2026-10-02T14:03:00Z",
  };
  expect(metaOf({ ...at, machine: "m-1" }, "m-1")).toMatch(
    /· this (?:machine|Mac) · 1 file$/u
  );
  expect(
    metaOf({ ...at, machine: "7c41e09b5a2d4f6e8b1c3d5e7f9a0b2c" }, "m-1")
  ).toMatch(/· 7c41e09b · 1 file$/u);
  expect(metaOf(at, "m-1")).toMatch(/^Oct 2(?:, 2026)? · 1 file$/u);
  expect(isShown({ ...at, import: true })).toBe(false);
  expect(isShown(at)).toBe(true);
});
