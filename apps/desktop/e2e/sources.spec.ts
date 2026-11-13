import type { Locator, Page } from "@playwright/test";

import { webUrl } from "#/screens/sources/projection.ts";

import {
  ACME,
  ACME_ID,
  acmeSkills,
  docResults,
  installable,
  inventory,
  library,
  sources,
  ZETA,
  ZETA_ID,
} from "./fixtures/sources.ts";
import type { InlineEvent, MockCli } from "./harness/cli.ts";
import { expect, loadLines, test } from "./harness/cli.ts";

const launchLines = [
  ["version", "--json"],
  ["doctor", "--json"],
];

/** Whether a command line is one the launch or the store's reads run, whichever order they come in. */
const isLaunchOrLoad = (line: string[]) =>
  [...launchLines, ...loadLines].some(
    (known) => known.join(" ") === line.join(" ")
  );

/** The command lines the screen itself ran. */
const later = (cli: MockCli) => cli.commands.filter((l) => !isLaunchOrLoad(l));

const sourcesRow = (page: Page) => page.getByRole("group", { name: "Sources" });
const pill = (page: Page, name: string) =>
  sourcesRow(page).getByRole("button", { exact: true, name });
const grid = (page: Page, name: string) =>
  page.getByRole("region", { exact: true, name });
const card = (page: Page, name: string) =>
  page.getByRole("list").getByRole("button", { exact: true, name });
const bar = (page: Page) =>
  page.getByRole("toolbar", { name: "Selected skills" });
const target = (page: Page, name: string) =>
  bar(page).getByRole("button", { exact: true, name });
const toasts = (page: Page) =>
  page.getByRole("region", { name: /^Notifications/u });
const dialog = (page: Page) => page.getByRole("dialog", { name: "Add source" });
const details = (page: Page) =>
  page.getByRole("region", { exact: true, name: "Source details" });
const selectAll = (page: Page) => page.getByRole("checkbox");
/** The value of the fact `label` on the open source's card. */
const fact = (page: Page, label: string) =>
  details(page)
    .getByRole("term")
    .filter({ hasText: label })
    .locator("xpath=following-sibling::dd");
const urlField = (page: Page) =>
  dialog(page).getByRole("textbox", { name: "Git URL or owner/name" });

/** The accessible names of `where`'s buttons. */
const names = async (where: Locator) =>
  await where
    .getByRole("button")
    .evaluateAll((buttons) => buttons.map((b) => b.getAttribute("aria-label")));

/** The last search request written to serve's stdin: its type, request id and query. */
const lastSearch = (cli: MockCli) => {
  const line = cli.serveRequests.at(-1) ?? "";
  const request: unknown = JSON.parse(line);
  const field = (key: string) =>
    typeof request === "object" && request !== null && key in request
      ? String(Reflect.get(request, key))
      : "";
  return {
    query: field("query"),
    request_id: field("request_id"),
    type: field("type"),
  };
};

/** Opens the Sources screen on the fixture machine and waits for acme's cards. */
const openSources = async (cli: MockCli, page: Page) => {
  cli.onServeStart({ events: [inventory] });
  cli.register(["source", "list"], { events: sources });
  cli.register(["source", "skills", ACME_ID], { events: acmeSkills });
  cli.register(["source", "skills", ZETA_ID], { events: [] });
  cli.register(["skill", "list", "--remote"], { events: installable });
  await page.goto("/#/sources");
  await expect(
    page.getByRole("heading", { level: 1, name: "Sources" })
  ).toBeVisible();
  await expect(card(page, "artifacts")).toBeVisible();
};

/** The command lines of the screen, as text, without the --json the supervisor adds. */
const commandLines = (cli: MockCli) =>
  later(cli).map((line) => line.slice(0, -1).join(" "));

test("the sources row: one pill per shared source, Personal, the counts and the stale dots", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  expect(await names(sourcesRow(page))).toEqual([
    "acme/skills",
    "zeta/tools",
    "Personal",
  ]);
  await expect(pill(page, "acme/skills")).toHaveAttribute(
    "aria-pressed",
    "true"
  );
  // A count once the listing is read; Personal's from `source list`.
  await expect(pill(page, "acme/skills")).toHaveText("acme/skills8");
  await expect(pill(page, "zeta/tools")).toHaveText("zeta/tools");
  await expect(pill(page, "Personal")).toHaveText("Personal2");
  await expect(
    pill(page, "acme/skills").getByTitle("Not checked in 3 days")
  ).toBeAttached();
  await expect(
    pill(page, "zeta/tools").getByTitle("Not fetched yet")
  ).toBeAttached();
  // Opening the screen reads the listing of each fetched source (and the sources again, a store read).
  expect(commandLines(cli)).toEqual([`source skills ${ACME_ID}`]);
});

test("the open source's card: its name links to the repository, its full URL, a Read-only tag, and its facts", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  const name = details(page).getByRole("link", { name: "acme/skills" });
  await expect(
    details(page).getByText(`${ACME}#v2`, { exact: true })
  ).toBeVisible();
  await expect(
    details(page).getByText("Read-only", { exact: true })
  ).toBeVisible();
  await expect(fact(page, "Skills")).toHaveText("8");
  await expect(fact(page, "Branch")).toHaveText("main");
  await expect(fact(page, "Commit")).toHaveText("3f2a9c1");
  await expect(fact(page, "Fetched")).toHaveText("3 days ago");
  await name.click();
  expect(cli.openedUrls).toEqual([ACME]);

  await pill(page, "zeta/tools").click();
  await expect(fact(page, "Fetched")).toHaveText("Not yet");

  // The account remote is managed in Settings: its card has no Remove.
  await pill(page, "Personal").click();
  await expect(
    details(page).getByRole("link", { name: "me/account" })
  ).toBeVisible();
  await expect(fact(page, "Fetched")).toHaveText("2 hours ago");
  await expect(details(page).getByRole("button")).toHaveText(["Refresh"]);

  // A search answers across every source: the card hides.
  await page.getByRole("searchbox", { name: "Search all sources" }).fill("pdf");
  await expect(details(page)).toHaveCount(0);
});

test("a source listing: selectable cards first, then each state with its label", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  expect(await names(grid(page, "acme/skills"))).toEqual([
    "artifacts",
    "docx",
    "forms",
    "slides",
    "letters",
    "pdf",
    "scratch",
    "xlsx",
  ]);
  await expect(card(page, "artifacts")).toBeEnabled();
  await expect(card(page, "artifacts")).toHaveAccessibleDescription(
    "Build web artifacts"
  );
  const states: [string, string][] = [
    ["letters", "Forked"],
    ["pdf", "Added"],
    ["scratch", "Already in library"],
    ["xlsx", "Name in use"],
  ];
  await Promise.all(
    states.flatMap(([name, label]) => [
      expect(card(page, name)).toBeDisabled(),
      expect(card(page, name)).toHaveAccessibleDescription(
        new RegExp(`^${label}\\.`, "u")
      ),
      expect(card(page, name).getByText(label, { exact: true })).toBeVisible(),
    ])
  );
  await expect(card(page, "xlsx")).toHaveAttribute(
    "title",
    "Remove or rename xlsx first"
  );
  await expect(card(page, "scratch")).toHaveAttribute(
    "title",
    "A folder named scratch is already in your library, added outside agentx. Adopt it in Skills to manage it, or remove it to add this one."
  );

  // Opening another pill reads that source's listing.
  await pill(page, "zeta/tools").click();
  await expect(page.getByText("No skills in this source")).toBeVisible();
  expect(commandLines(cli)).toEqual([
    `source skills ${ACME_ID}`,
    `source skills ${ZETA_ID}`,
  ]);
});

test("names and targets: two cards and one enabled agent turned off", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  const add = cli.register(
    [
      "skill",
      "add",
      ACME,
      "--name",
      "artifacts",
      "--name",
      "docx",
      "--to",
      "claude-code",
    ],
    {
      events: [
        { name: "artifacts", type: "library_skill" },
        { name: "docx", type: "library_skill" },
      ],
    }
  );
  await card(page, "docx").click();
  await card(page, "artifacts").click();
  await expect(bar(page)).toContainText("2 selected");
  // The enabled agents are preselected; the disabled one is off.
  await expect(target(page, "Claude Code")).toHaveAttribute(
    "aria-pressed",
    "true"
  );
  await expect(target(page, "Cursor")).toHaveAttribute("aria-pressed", "true");
  await expect(target(page, "Windsurf")).toHaveAttribute(
    "aria-pressed",
    "false"
  );
  await target(page, "Cursor").click();
  await expect(target(page, "Cursor")).toHaveAttribute("aria-pressed", "false");

  await bar(page).getByRole("button", { name: "Add 2" }).click();
  await add.started;
  await expect(toasts(page).getByText("Added 2 skills")).toBeVisible();
  // The selection clears; the cards turn Added only with the next inventory.
  await expect(bar(page)).toHaveCount(0);
  await expect(card(page, "docx")).toBeEnabled();
  await cli.serveEmit([
    {
      ...inventory,
      library: [
        ...library,
        {
          content_hash: "h-docx",
          kind: "managed",
          name: "docx",
          placements: [],
          source: ACME,
          state: "current",
          subpath: "skills/docx",
          universal: [],
        },
      ],
      scan_counter: 2,
    },
  ]);
  await expect(card(page, "docx")).toBeDisabled();
  await expect(
    card(page, "docx").getByText("Added", { exact: true })
  ).toBeVisible();
  expect(commandLines(cli)).toContain(
    `skill add ${ACME} --name artifacts --name docx --to claude-code`
  );
});

test("Select all, two deselected, agents as enabled: --all with --except and no --to", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  const line = [
    "skill",
    "add",
    ACME,
    "--all",
    "--except",
    "docx",
    "--except",
    "forms",
    "--except",
    "letters",
    "--except",
    "pdf",
    "--except",
    "scratch",
    "--except",
    "xlsx",
  ];
  const add = cli.register(line, {
    events: [
      { name: "artifacts", type: "library_skill" },
      { name: "slides", type: "library_skill" },
    ],
  });
  await expect(page.getByText("4 already added")).toBeVisible();
  await expect(selectAll(page)).toHaveAccessibleName("Select all 4");
  await selectAll(page).click();
  await expect(selectAll(page)).toHaveAccessibleName("All 4 selected");
  await expect(selectAll(page)).toHaveAttribute("aria-checked", "true");
  await expect(bar(page)).toContainText("4 selected");
  await card(page, "docx").click();
  await card(page, "forms").click();
  await expect(bar(page)).toContainText("2 selected");
  await expect(selectAll(page)).toHaveAccessibleName("2 of 4 selected");
  await expect(selectAll(page)).toHaveAttribute("aria-checked", "mixed");
  // The Universal stack is on and stays on.
  await expect(target(page, "Universal")).toHaveAttribute(
    "aria-pressed",
    "true"
  );
  await target(page, "Universal").click();
  await expect(target(page, "Universal")).toHaveAttribute(
    "aria-pressed",
    "true"
  );
  await bar(page).getByRole("button", { name: "Add 2" }).click();
  await add.started;
  await expect(toasts(page).getByText("Added 2 skills")).toBeVisible();
  expect(commandLines(cli)).toContain(line.join(" "));
});

test("progress shows from the progress events, and Cancel stops the run", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  const add = cli.register(
    ["skill", "add", ACME, "--name", "artifacts", "--name", "docx"],
    {
      events: [
        { current: 1, phase: "install", total: 3, type: "progress" },
        { current: 2, phase: "install", total: 3, type: "progress" },
        { current: 3, phase: "rescan", total: 3, type: "progress" },
        { name: "artifacts", type: "library_skill" },
        { name: "docx", type: "library_skill" },
      ],
      holdAfter: 2,
    }
  );
  await card(page, "artifacts").click();
  await card(page, "docx").click();
  await bar(page).getByRole("button", { name: "Add 2" }).click();
  await add.started;
  await expect(bar(page)).toContainText("Adding 2 of 3");
  await bar(page).getByRole("button", { name: "Cancel" }).click();
  await expect(
    toasts(page).getByText("Stopped. 2 skills were added.")
  ).toBeVisible();
  expect(cli.cancels).toHaveLength(1);
  // The selection stays, for another try.
  await expect(bar(page)).toContainText("2 selected");
});

test("a partial failure lists what was added, the error, its hint and each skipped skill", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  const error = {
    code: "refused",
    hint: "remove it with 'agentx skill remove docx', then install again",
    message:
      "1 of 2 skills could not be installed: docx: the library already holds docx",
    type: "error",
  } as const;
  const warning =
    "docx: the library already holds docx at /home/me/.agents/skills";
  const add = cli.register(
    ["skill", "add", ACME, "--name", "artifacts", "--name", "docx"],
    {
      events: [{ name: "artifacts", type: "library_skill" }, error],
      exitCode: 6,
      logs: [{ level: "warn", message: warning }],
    }
  );
  await card(page, "artifacts").click();
  await card(page, "docx").click();
  await bar(page).getByRole("button", { name: "Add 2" }).click();
  await add.started;
  const toast = toasts(page)
    .getByRole("listitem")
    .filter({ hasText: "Added 1 of 2 skills" });
  await expect(toast).toBeVisible();
  await expect(toast).toContainText(error.message);
  await expect(toast).toContainText(error.hint);
  await expect(toast).toContainText(warning);
  // The skill that landed leaves the selection; the skipped one stays.
  await expect(bar(page)).toContainText("1 selected");
  await expect(card(page, "docx")).toHaveAttribute("aria-pressed", "true");
  await expect(card(page, "artifacts")).toHaveAttribute(
    "aria-pressed",
    "false"
  );
});

test("search asks serve once per settled query and shows only the latest answer", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  const field = page.getByRole("searchbox", { name: "Search all sources" });
  await field.fill("pd");
  await expect.poll(() => cli.serveRequests.length).toBe(1);
  const first = lastSearch(cli);
  expect(first).toEqual({
    query: "pd",
    request_id: expect.any(String),
    type: "search",
  });
  await field.fill("pdf");
  await expect.poll(() => cli.serveRequests.length).toBe(2);
  const second = lastSearch(cli);
  expect(second.query).toBe("pdf");
  expect(second.request_id).not.toBe(first.request_id);

  // The answer to the older request is dropped; the listing stays.
  await cli.serveEmit([
    {
      query: "pd",
      request_id: first.request_id,
      results: docResults,
      type: "search",
    },
  ]);
  await expect(card(page, "artifacts")).toBeVisible();

  // The matching answer replaces the grid, grouped by source.
  await cli.serveEmit([
    {
      query: "pdf",
      request_id: second.request_id,
      results: docResults,
      type: "search",
    },
  ]);
  await expect(grid(page, "zeta/tools")).toBeVisible();
  expect(await names(grid(page, "acme/skills"))).toEqual(["docx"]);
  expect(await names(grid(page, "zeta/tools"))).toEqual(["docgen"]);

  // Clearing the query shows the listing again.
  await field.fill("");
  await expect(card(page, "artifacts")).toBeVisible();
});

test("Add source runs source add, then opens the new source fully preselected", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  const newId = "9f8e7d6c5b4a3210";
  const newUrl = "https://github.com/acme/more";
  cli.register(["source", "add", "acme/more"], {
    events: [
      {
        id: newId,
        last_fetched: new Date().toISOString(),
        skills: 2,
        type: "source",
        url: newUrl,
      },
    ],
  });
  cli.register(["source", "skills", newId], {
    events: [
      {
        description: "Review a change",
        name: "review",
        source: newId,
        subpath: "review",
        tree: "t1",
        type: "source_skill",
      },
      {
        description: "Triage an issue",
        name: "triage",
        source: newId,
        subpath: "triage",
        tree: "t2",
        type: "source_skill",
      },
    ],
  });
  await page.getByRole("button", { name: "Add source" }).click();
  await expect(dialog(page)).toBeVisible();
  await expect(dialog(page)).toContainText("Add #branch or #tag to pin it");
  await urlField(page).fill("acme/more");
  await dialog(page).getByRole("button", { name: "Add" }).click();
  await expect(dialog(page)).toHaveCount(0);
  await expect(
    toasts(page).getByText(`Added ${newUrl}: 2 skills`)
  ).toBeVisible();
  await expect(pill(page, "acme/more")).toHaveAttribute("aria-pressed", "true");
  await expect(card(page, "review")).toHaveAttribute("aria-pressed", "true");
  await expect(card(page, "triage")).toHaveAttribute("aria-pressed", "true");
  await expect(bar(page)).toContainText("2 selected");
  expect(commandLines(cli)).toEqual([
    `source skills ${ACME_ID}`,
    "source add acme/more",
    `source skills ${newId}`,
  ]);

  // The preselection installs with --all.
  const add = cli.register(["skill", "add", newUrl, "--all"], {
    events: [
      { name: "review", type: "library_skill" },
      { name: "triage", type: "library_skill" },
    ],
  });
  await bar(page).getByRole("button", { name: "Add 2" }).click();
  await add.started;
  await expect(toasts(page).getByText("Added 2 skills")).toBeVisible();
});

test("Add source shows a source git cannot reach, and the CLI's refusals, under the field", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  cli.register(["source", "add", "bad/repo"], {
    events: [
      {
        code: "source",
        hint: "check that the repository exists and that you can read it",
        message:
          "cannot reach https://github.com/bad/repo: repository not found",
        type: "error",
      },
    ],
    exitCode: 3,
  });
  cli.register(["source", "add", "me/account"], {
    events: [
      {
        code: "refused",
        hint: "run 'agentx skill list --remote' to see its skills, then 'agentx skill add --name <name>' to install one",
        message:
          "https://github.com/me/account is the account remote: it holds one branch per skill rather than skills on one branch",
        type: "error",
      },
    ],
    exitCode: 6,
  });
  await page.getByRole("button", { name: "Add source" }).click();
  await urlField(page).fill("bad/repo");
  await dialog(page).getByRole("button", { name: "Add" }).click();
  const alert = dialog(page).getByRole("alert");
  // The message and the CLI's hint, as one sentence-cased paragraph.
  await expect(alert).toHaveText(
    "Couldn't reach bad/repo. Check that the repository exists and that you can read it."
  );

  await urlField(page).fill("me/account");
  await dialog(page).getByRole("button", { name: "Add" }).click();
  // The CLI's hint names commands: the dialog names the place in the app instead.
  await expect(alert).toHaveText(
    "https://github.com/me/account is the account remote: it holds one branch per skill rather than skills on one branch. See and install its skills under Personal."
  );
  await expect(dialog(page)).toBeVisible();
  expect(commandLines(cli).slice(-2)).toEqual([
    "source add bad/repo",
    "source add me/account",
  ]);
});

test("Personal lists the account remote's installable skills and adds by name", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  await pill(page, "Personal").click();
  await expect(card(page, "release-notes")).toBeVisible();
  expect(await names(grid(page, "Personal"))).toEqual([
    "my-pdf",
    "release-notes",
  ]);
  await expect(card(page, "my-pdf")).toContainText("from acme/skills");
  await expect(card(page, "release-notes")).toContainText("personal skill");

  const add = cli.register(["skill", "add", "--name", "release-notes"], {
    events: [{ name: "release-notes", type: "library_skill" }],
  });
  await card(page, "release-notes").click();
  await bar(page).getByRole("button", { name: "Add 1" }).click();
  await add.started;
  await expect(toasts(page).getByText("Added 1 skill")).toBeVisible();
  expect(commandLines(cli)).toEqual([
    `source skills ${ACME_ID}`,
    "skill list --remote",
    "skill add --name release-notes",
  ]);
});

test("Refresh fetches the source, then reads the listing of the new fetch", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  const fetched: InlineEvent[] = [
    { current: 1, phase: "fetch", subject: ACME, total: 1, type: "progress" },
    { id: ACME_ID, type: "source", url: ACME },
  ];
  cli.register(["source", "fetch", ACME_ID], { events: fetched });
  const now = new Date().toISOString();
  cli.register(["source", "list"], {
    events: sources.map((s) =>
      s.id === ACME_ID ? { ...s, last_fetched: now } : s
    ),
  });
  await details(page).getByRole("button", { name: "Refresh" }).click();
  await expect(fact(page, "Fetched")).toHaveText("just now");
  await expect
    .poll(() => commandLines(cli))
    .toEqual([
      `source skills ${ACME_ID}`,
      `source fetch ${ACME_ID}`,
      `source skills ${ACME_ID}`,
    ]);
});

test("Remove with its installed skills removes each skill, then the source", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  // The first source, open without a click, with a card selected.
  await card(page, "artifacts").click();
  const remove = details(page).getByRole("button", { name: "Remove" });
  await remove.click();
  const confirm = page.getByRole("dialog", { name: "Remove acme/skills?" });
  // Cancel returns focus to Remove.
  await confirm.getByRole("button", { name: "Cancel" }).click();
  await expect(remove).toBeFocused();
  await remove.click();
  const also = confirm.getByRole("checkbox", {
    name: "Also remove its 1 installed skill pdf",
  });
  await expect(also).not.toBeChecked();
  await also.click();
  cli.register(["skill", "remove", "pdf"], {
    events: [{ name: "pdf", type: "library_skill" }],
  });
  cli.register(["source", "remove", ACME], {
    events: [{ id: ACME_ID, type: "source", url: ACME }],
  });
  cli.register(["source", "list"], {
    events: sources.filter((s) => s.id !== ACME_ID),
  });
  await confirm.getByRole("button", { name: "Remove" }).click();
  await expect(confirm).toHaveCount(0);
  // The removed source's card goes, so focus moves to the search field.
  await expect(
    page.getByRole("searchbox", { name: "Search all sources" })
  ).toBeFocused();
  await expect(
    toasts(page).getByText("Source and 1 skill removed")
  ).toBeVisible();
  // The source's pill goes with the next `source list`, the first pill opens, and the selection goes.
  await expect(pill(page, "acme/skills")).toHaveCount(0);
  await expect(pill(page, "zeta/tools")).toHaveAttribute(
    "aria-pressed",
    "true"
  );
  await expect(bar(page)).toHaveCount(0);
  expect(commandLines(cli)).toEqual([
    `source skills ${ACME_ID}`,
    "skill remove pdf",
    `source remove ${ACME}`,
    `source skills ${ZETA_ID}`,
  ]);
});

test("Remove alone removes the source and keeps its skills", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  await pill(page, "zeta/tools").click();
  await details(page).getByRole("button", { name: "Remove" }).click();
  const confirm = page.getByRole("dialog", { name: "Remove zeta/tools?" });
  cli.register(["source", "remove", ZETA], {
    events: [{ id: ZETA_ID, type: "source", url: ZETA }],
  });
  await confirm.getByRole("button", { name: "Remove" }).click();
  await expect(toasts(page).getByText("Source removed")).toBeVisible();
  expect(commandLines(cli)).toEqual([
    `source skills ${ACME_ID}`,
    `source skills ${ZETA_ID}`,
    `source remove ${ZETA}`,
  ]);
});

test("the repository link of a source URL", () => {
  const cases: [string, string | undefined][] = [
    ["https://github.com/acme/skills.git", "https://github.com/acme/skills"],
    [
      "https://git.example.com:8443/team/skills.git",
      "https://git.example.com:8443/team/skills",
    ],
    [
      "ssh://git@git.example.com:2222/team/skills.git",
      "https://git.example.com/team/skills",
    ],
    ["git@github.com:acme/skills.git", "https://github.com/acme/skills"],
    ["/home/me/skills", undefined],
  ];
  expect(cases.map(([url]) => [url, webUrl(url)])).toEqual(cases);
});
