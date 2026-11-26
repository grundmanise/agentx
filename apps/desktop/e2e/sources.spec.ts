import type { Locator, Page } from "@playwright/test";

import type { Install, SourceSkillEvent } from "#/cli/events.gen.ts";
import {
  installableCards,
  installOf,
  listingCards,
  planAdds,
  skillPage,
  webUrl,
} from "#/screens/sources/projection.ts";

import { filesOf } from "./fixtures/skills.ts";
import {
  ACME,
  ACME_ID,
  acmeSkills,
  configurations,
  docResults,
  HUB,
  installable,
  inventory,
  library,
  skillMd,
  sources,
  ZETA,
  ZETA_ID,
} from "./fixtures/sources.ts";
import type { InlineEvent, MockCli } from "./harness/cli.ts";
import { configuration, expect, loadLines, test } from "./harness/cli.ts";

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
/** The row of a skill, which opens and closes it. */
const row = (page: Page, name: string) =>
  page.getByRole("heading").getByRole("button", { exact: true, name });
/** What the row of a skill shows: its name, description or status, over the button. */
const rowLine = (page: Page, name: string) =>
  page.locator("[data-slot=accordion-row]").filter({ has: row(page, name) });
/** The round check that selects a skill. */
const check = (page: Page, name: string) =>
  page.getByRole("button", { exact: true, name: `Select ${name}` });
/** What the open row of a skill shows. */
const opened = (page: Page, name: string) =>
  page.getByRole("region", { exact: true, name });
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

/** The skills of `where`, by the accessible names of their rows. */
const skillNames = async (where: Locator) =>
  await where
    .getByRole("heading")
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

/** Opens the Sources screen on the fixture machine and waits for acme's skills. */
const openSources = async (
  cli: MockCli,
  page: Page,
  first: InlineEvent = inventory
) => {
  cli.onServeStart({ events: [first] });
  cli.register(["source", "skills", ACME_ID], { events: acmeSkills });
  cli.register(["source", "skills", ZETA_ID], { events: [] });
  cli.register(["skill", "list", "--remote"], { events: installable });
  await page.goto("/#/sources");
  await expect(
    page.getByRole("heading", { level: 1, name: "Sources" })
  ).toBeVisible();
  await expect(row(page, "artifacts")).toBeVisible();
};

/** The command lines of the screen, as text, without the --json the supervisor adds. */
const commandLines = (cli: MockCli) =>
  later(cli).map((line) => line.slice(0, -1).join(" "));

test("the sources row: one pill per shared source, Hub, the counts and the stale dots, and a source the inventory adds", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  expect(await names(sourcesRow(page))).toEqual([
    "acme/skills",
    "zeta/tools",
    "Hub",
  ]);
  await expect(pill(page, "acme/skills")).toHaveAttribute(
    "aria-pressed",
    "true"
  );
  // A count once the listing is read; Hub's from the inventory.
  await expect(pill(page, "acme/skills")).toHaveText("acme/skills8");
  await expect(pill(page, "zeta/tools")).toHaveText("zeta/tools");
  await expect(pill(page, "Hub")).toHaveText("Hub2");
  await expect(
    pill(page, "acme/skills").getByTitle("Not checked in 3 days")
  ).toBeAttached();
  await expect(
    pill(page, "zeta/tools").getByTitle("Not fetched yet")
  ).toBeAttached();
  // Opening the screen reads the listing of each fetched source.
  expect(commandLines(cli)).toEqual([`source skills ${ACME_ID}`]);

  // A source added elsewhere, as in a terminal, comes with the next inventory alone.
  const moreId = "9f8e7d6c5b4a3210";
  cli.register(["source", "skills", moreId], {
    events: [
      {
        description: "Review a change",
        name: "review",
        source: moreId,
        subpath: "review",
        tree: "t-review",
        type: "source_skill",
      },
    ],
  });
  await cli.serveEmit([
    {
      ...inventory,
      scan_counter: 2,
      sources: [
        ...sources,
        {
          id: moreId,
          last_fetched: new Date().toISOString(),
          url: "https://github.com/acme/more",
        },
      ],
    },
  ]);
  await expect(pill(page, "acme/more")).toHaveText("acme/more1");
  expect(await names(sourcesRow(page))).toEqual([
    "acme/more",
    "acme/skills",
    "zeta/tools",
    "Hub",
  ]);
  expect(commandLines(cli)).toEqual([
    `source skills ${ACME_ID}`,
    `source skills ${moreId}`,
  ]);
});

test("the open source's card: its name, its full URL linking to the repository, a Read-only tag, and its facts", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  await expect(
    details(page).getByRole("heading", { name: "acme/skills" })
  ).toBeVisible();
  const url = details(page).getByRole("link", { name: `${ACME}#v2` });
  await expect(
    details(page).getByText("Read-only", { exact: true })
  ).toBeVisible();
  await expect(fact(page, "Skills")).toHaveText("8");
  await expect(fact(page, "Branch")).toHaveText("main");
  await expect(fact(page, "Commit")).toHaveText("3f2a9c1");
  await expect(fact(page, "Fetched")).toHaveText("3 days ago");
  // A drag selects the URL's text and opens nothing; a click opens the repository.
  const box = await url.boundingBox();
  if (box === null) {
    throw new Error("the URL has no box");
  }
  const y = box.y + box.height / 2;
  await page.mouse.move(box.x + 1, y);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width - 1, y, { steps: 5 });
  await page.mouse.up();
  expect(
    await page.evaluate(() => window.getSelection()?.toString() ?? "")
  ).toContain("acme/skills");
  expect(cli.openedUrls).toEqual([]);
  await url.click();
  expect(cli.openedUrls).toEqual([ACME]);

  await pill(page, "zeta/tools").click();
  await expect(fact(page, "Fetched")).toHaveText("Not yet");

  // The hub is managed in Settings: its card has no Remove.
  await pill(page, "Hub").click();
  await expect(
    details(page).getByRole("heading", { name: "me/hub" })
  ).toBeVisible();
  await expect(fact(page, "Fetched")).toHaveText("2 hours ago");
  await expect(details(page).getByRole("button")).toHaveText(["Refresh"]);
});

/** acme's skills A to Z, the order the list keeps whatever their state. */
const acmeNames = [
  "artifacts",
  "docx",
  "forms",
  "letters",
  "pdf",
  "scratch",
  "slides",
  "xlsx",
];

test("a source listing: the skills A to Z, each state with its label and, once open, its reason", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  expect(await skillNames(grid(page, "acme/skills"))).toEqual(acmeNames);
  await expect(check(page, "artifacts")).toHaveAttribute(
    "aria-pressed",
    "false"
  );
  await expect(row(page, "artifacts")).toHaveAccessibleDescription(
    "Build web artifacts"
  );
  const states: [string, string][] = [
    ["letters", "Forked"],
    ["pdf", "Added"],
    ["scratch", "Name taken"],
    ["xlsx", "Name taken"],
  ];
  await Promise.all(
    states.flatMap(([name, label]) => [
      expect(check(page, name)).toHaveCount(0),
      expect(row(page, name)).toHaveAccessibleDescription(
        new RegExp(`^${label}\\.`, "u")
      ),
      expect(
        rowLine(page, name).getByText(label, { exact: true })
      ).toBeVisible(),
    ])
  );
  // An open row says why its skill cannot be selected.
  for (const name of ["xlsx", "scratch"]) {
    cli.register(["source", "read", ACME_ID, `skills/${name}`], {
      events: [skillMd(name, `# ${name}\n`)],
    });
  }
  await row(page, "xlsx").click();
  await row(page, "scratch").click();
  await expect(opened(page, "xlsx")).toContainText(
    "Your library has xlsx skill from zeta/tools. Remove or rename it to add this skill."
  );
  await expect(opened(page, "scratch")).toContainText(
    "Your library has a scratch folder that agentx doesn't manage. Import or remove it to add this skill."
  );
  await expect(
    opened(page, "scratch").getByRole("link", { exact: true, name: "scratch" })
  ).toHaveAttribute("href", "#/skills/scratch");

  // Opening another pill reads that source's listing.
  await pill(page, "zeta/tools").click();
  await expect(page.getByText("No skills in this source")).toBeVisible();
  expect(commandLines(cli)).toEqual([
    `source skills ${ACME_ID}`,
    `source read ${ACME_ID} skills/xlsx`,
    `source read ${ACME_ID} skills/scratch`,
    `source skills ${ZETA_ID}`,
  ]);
});

test("the name of the skill that holds a Name taken card's name opens it in Skills", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  cli.register(["source", "read", ACME_ID, "skills/xlsx"], {
    events: [skillMd("xlsx", "# xlsx\n")],
  });
  await row(page, "xlsx").click();
  cli.register(["skill", "files", "xlsx"], { events: filesOf("xlsx") });
  await opened(page, "xlsx")
    .getByRole("link", { exact: true, name: "xlsx" })
    .click();
  await expect(page).toHaveURL(/#\/skills\/xlsx$/u);
});

/**
 * The drawn rows of the list, in its order: the name of each and where it shows, in px. One read
 * in the page, so that no row the list takes out meanwhile is among them.
 */
const drawnRows = async (page: Page) =>
  await page.evaluate(() => {
    const items = document.querySelectorAll<HTMLElement>("[data-index]");
    const rows = [...items].map((item) => {
      const box = item.getBoundingClientRect();
      return {
        bottom: box.bottom,
        index: Number(item.dataset.index),
        name:
          item
            .querySelector("button[aria-label]")
            ?.getAttribute("aria-label") ?? "",
        top: box.top,
      };
    });
    rows.sort((a, b) => a.index - b.index);
    return rows;
  });

test("a source of plugins whose skills share names: a row each, drawn once, the later of a name in use", async ({
  cli,
  page,
}) => {
  // As cursor/plugins has: each of 90 names in two plugins, in a window as large as a full screen.
  const height = 1440;
  await page.setViewportSize({ height, width: 2560 });
  const shared = Array.from(
    { length: 90 },
    (_, i) => `skill-${String(i).padStart(2, "0")}`
  );
  const listing = ["alpha", "beta"].flatMap((plugin) =>
    shared.map((name): InlineEvent => ({
      description: `${name} of ${plugin}`,
      name,
      source: ACME_ID,
      subpath: `${plugin}/skills/${name}`,
      tree: `t-${plugin}-${name}`,
      type: "source_skill",
    }))
  );
  cli.onServeStart({ events: [inventory] });
  cli.register(["source", "skills", ACME_ID], { events: listing });
  cli.register(["source", "skills", ZETA_ID], { events: [] });
  cli.register(["skill", "list", "--remote"], { events: installable });
  await page.goto("/#/sources");
  // A to Z, so the two skills of a name sit side by side, the first of the listing first.
  await expect(row(page, "skill-00").first()).toBeVisible();
  await expect(row(page, "skill-00")).toHaveCount(2);
  // The later skill of each name is not on this machine, so none counts as added.
  await expect(page.getByText("Select all 90")).toBeVisible();
  await expect(page.getByText(/already added/u)).toHaveCount(0);

  // Scrolled through, each row is drawn once and none over another.
  const scroller = page
    .locator(".overflow-auto")
    .filter({ has: page.getByRole("heading", { level: 1, name: "Sources" }) });
  const middle = height / 2;
  /** Whether the list has drawn the rows at the new offset: one of them covers the middle. */
  const drawnAtMiddle = async () => {
    const rows = await drawnRows(page);
    return rows.some((r) => r.top <= middle && r.bottom >= middle);
  };
  const seen = new Map<number, string>();
  // Half a window at a time, until the list's end shows.
  let last = false;
  while (!last) {
    // oxlint-disable-next-line no-await-in-loop
    last = await scroller.evaluate((element, by) => {
      const end = element.scrollHeight - element.clientHeight;
      const at = element.scrollTop >= end - 1;
      element.scrollTop += by;
      return at;
    }, middle);
    // oxlint-disable-next-line no-await-in-loop
    await expect.poll(drawnAtMiddle).toBe(true);
    // oxlint-disable-next-line no-await-in-loop
    const rows = await drawnRows(page);
    expect(new Set(rows.map((r) => r.index)).size).toBe(rows.length);
    for (const [i, r] of rows.entries()) {
      expect(r.top).toBeGreaterThanOrEqual(rows[i - 1]?.bottom ?? r.top);
      seen.set(r.index, r.name);
    }
  }
  const drawnNames = [...seen.values()];
  drawnNames.sort((a, b) => a.localeCompare(b));
  expect(drawnNames).toEqual(shared.flatMap((name) => [name, name]));

  // The later skill of a name cannot be selected, and says why once open.
  const second = rowLine(page, "skill-89").last();
  await expect(second.getByText("Name taken", { exact: true })).toBeVisible();
  await expect(
    second.getByRole("button", { name: "Select skill-89" })
  ).toHaveCount(0);
  cli.register(["source", "read", ACME_ID, "beta/skills/skill-89"], {
    events: [skillMd("skill-89", "# skill-89\n")],
  });
  await row(page, "skill-89").last().click();
  await expect(opened(page, "skill-89")).toContainText(
    "The skill at alpha/skills/skill-89 in this source is named skill-89 too, and only the first of a name can be added."
  );
  expect(commandLines(cli)).toEqual([
    `source skills ${ACME_ID}`,
    `source read ${ACME_ID} beta/skills/skill-89`,
  ]);
});

test("a row that opens keeps the scroll, its top hidden or not", async ({
  cli,
  page,
}) => {
  await page.setViewportSize({ height: 600, width: 1100 });
  await openSources(cli, page);
  // A later launch draws the kept sources at once, so the list mounts with the scroller it
  // scrolls in.
  await expect
    .poll(
      async () =>
        await page.evaluate(() => localStorage.getItem("agentx.launch"))
    )
    .not.toBeNull();
  cli.restart();
  await page.reload();
  await expect(row(page, "pdf")).toBeVisible();
  const lines = Array.from({ length: 40 }, (_, i) => `line ${i}`).join("\n");
  for (const name of ["pdf", "docx"]) {
    cli.register(["source", "read", ACME_ID, `skills/${name}`], {
      events: [skillMd(name, `---\nname: ${name}\n---\n${lines}\n`)],
    });
  }
  const scroller = page
    .locator(".overflow-auto")
    .filter({ has: row(page, "pdf") });
  const scrollTop = async () => await scroller.evaluate((el) => el.scrollTop);
  const top = async (name: string) => {
    const box = await rowLine(page, name).boundingBox();
    return box?.y ?? Number.NaN;
  };

  // pdf opens at the bottom edge: nothing moves, and its SKILL.md shows below the fold.
  const pdfBefore = await top("pdf");
  await row(page, "pdf").dispatchEvent("click");
  await expect(row(page, "pdf")).toHaveAttribute("aria-expanded", "true");
  expect(await top("pdf")).toBe(pdfBefore);

  // docx, its top 20px above the scroller's, opens in place too.
  const scrollerBox = await scroller.boundingBox();
  const offset = (await top("docx")) - (scrollerBox?.y ?? Number.NaN) + 20;
  await scroller.evaluate((el, by) => {
    el.scrollTop += by;
  }, offset);
  const scrolled = await scrollTop();
  await row(page, "docx").dispatchEvent("click");
  await expect(row(page, "docx")).toHaveAttribute("aria-expanded", "true");
  await expect(
    opened(page, "docx").getByRole("region", { name: "SKILL.md of docx" })
  ).toBeVisible();
  expect(await scrollTop()).toBe(scrolled);
});

test("opening a row reads its SKILL.md once, then opens, and links to the skill", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  const read = cli.register(["source", "read", ACME_ID, "skills/docx"], {
    events: [skillMd("docx", "---\nname: docx\n---\n# Word documents\n")],
    holdAfter: 0,
  });
  await row(page, "docx").click();
  await read.started;
  // The row waits closed, with a spinner, until SKILL.md is read.
  await expect(row(page, "docx")).toHaveAttribute("aria-busy", "true");
  await expect(row(page, "docx")).toHaveAttribute("aria-expanded", "false");
  const spinner = page
    .getByRole("status")
    .filter({ hasText: "Loading skill…" });
  await expect(spinner).toBeVisible();
  await spinner.locator("[data-slot=tooltip-trigger]").hover();
  await expect(page.getByRole("tooltip")).toHaveText("Loading skill…");
  read.release();
  await expect(row(page, "docx")).toHaveAttribute("aria-expanded", "true");
  await expect(
    opened(page, "docx").getByRole("region", { name: "SKILL.md of docx" })
  ).toContainText("# Word documents");
  await page.getByRole("link", { name: "See full skill" }).click();
  expect(cli.openedUrls).toEqual([`${ACME}/tree/3f2a9c1e8d7b6a/skills/docx`]);
  // The row opens and closes on a click; the check alone selects.
  await expect(bar(page)).toHaveCount(0);
  await row(page, "docx").click();
  await expect(row(page, "docx")).toHaveAttribute("aria-expanded", "false");
  await row(page, "docx").click();
  await expect(
    opened(page, "docx").getByRole("region", { name: "SKILL.md of docx" })
  ).toContainText("# Word documents");
  expect(commandLines(cli)).toEqual([
    `source skills ${ACME_ID}`,
    `source read ${ACME_ID} skills/docx`,
  ]);
});

test("the bar slides up from below the edge, and back down once nothing is selected", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  await check(page, "docx").click();
  await expect(bar(page)).toHaveCSS("animation-name", "floating-bar");
  await expect(bar(page)).not.toHaveAttribute("data-leaving");
  await check(page, "docx").click();
  // On its way out it keeps its count, then it goes.
  await expect(bar(page)).toHaveAttribute("data-leaving", "");
  await expect(bar(page)).toHaveCSS("animation-name", "floating-bar-out");
  await expect(
    bar(page).getByRole("button", { exact: true, name: "Add 1" })
  ).toBeVisible();
  await expect(bar(page)).toHaveCount(0);
  // Selected again while it leaves, it comes back.
  await check(page, "docx").click();
  await check(page, "docx").click();
  await check(page, "docx").click();
  await expect(bar(page)).not.toHaveAttribute("data-leaving");
  await page.waitForTimeout(400);
  await expect(bar(page)).toBeVisible();
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
  await check(page, "docx").click();
  await check(page, "artifacts").click();
  await expect(
    bar(page).getByRole("button", { exact: true, name: "Add 2" })
  ).toBeVisible();
  // The enabled agents are preselected; the disabled one is off.
  await expect(target(page, "Claude Code")).toHaveAttribute(
    "aria-pressed",
    "true"
  );
  await expect(target(page, "Cursor")).toHaveAttribute("aria-pressed", "true");
  await expect(target(page, "Devin")).toHaveAttribute("aria-pressed", "false");
  await target(page, "Cursor").click();
  await expect(target(page, "Cursor")).toHaveAttribute("aria-pressed", "false");

  // Watched in the page from before the run, so the short sweep cannot slip by unseen.
  const swept = page.waitForFunction(
    () =>
      Number(
        document.querySelector<HTMLCanvasElement>(
          "[role=toolbar] canvas[data-sweep]"
        )?.dataset.sweep
      ) > 0.5,
    undefined,
    { polling: "raf" }
  );
  await bar(page).getByRole("button", { name: "Add 2" }).click();
  await add.started;
  await expect(toasts(page).getByText("Added 2 skills")).toBeVisible();
  // The selection clears, but the bar stays while its rim fills, joins, sweeps and fades.
  await expect(bar(page).locator("canvas[data-progress]")).toBeAttached();
  await swept;
  await expect(bar(page)).toHaveCount(0);
  // The cards turn Added only with the next inventory.
  await expect(check(page, "docx")).toBeVisible();
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
  await expect(
    rowLine(page, "docx").getByText("Added", { exact: true })
  ).toBeVisible();
  // An added skill keeps its place.
  expect(await skillNames(grid(page, "acme/skills"))).toEqual(acmeNames);
  expect(commandLines(cli)).toContain(
    `skill add ${ACME} --name artifacts --name docx --to claude-code`
  );
});

test("Remove on an Added skill confirms, then runs skill remove for every agent", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  const remove = rowLine(page, "pdf").getByRole("button", {
    exact: true,
    name: "Remove pdf",
  });
  const word = (text: string) =>
    rowLine(page, "pdf").getByText(text, { exact: true });
  await expect(word("Added")).toHaveCSS("opacity", "1");
  await expect(word("Remove")).toHaveCSS("opacity", "0");
  await remove.hover();
  await expect(word("Added")).toHaveCSS("opacity", "0");
  await expect(word("Remove")).toHaveCSS("opacity", "1");
  const run = cli.register(["skill", "remove", "pdf"], {
    events: [{ ok: true, summary: "removed pdf", type: "result" }],
  });
  const confirm = page.getByRole("alertdialog", {
    name: "Remove pdf from this machine?",
  });
  // Cancel puts the focus back on the card's Remove: the dialog has no trigger.
  await remove.click();
  await confirm.getByRole("button", { exact: true, name: "Cancel" }).click();
  await expect(confirm).toBeHidden();
  await expect(remove).toBeFocused();
  await remove.click();
  await expect(confirm).toContainText("Removes it from:");
  await expect(
    confirm.getByRole("list", { name: "Agents that see it" })
  ).toContainText("Codex");
  await confirm.getByRole("button", { exact: true, name: "Remove" }).click();
  await run.started;
  await expect(toasts(page).getByText("Removed pdf skill")).toBeVisible();
  expect(commandLines(cli)).toContain("skill remove pdf");
});

test("an Added skill shows when it was added, a removed one when it was removed, an open row both", async ({
  cli,
  page,
}) => {
  const year = new Date().getFullYear();
  await openSources(cli, page, {
    ...inventory,
    installs: [
      {
        added: `${year}-10-02T09:00:00Z`,
        name: "pdf",
        removed: `${year}-09-20T09:00:00Z`,
        source: ACME,
        subpath: "skills/pdf",
      },
      {
        name: "docx",
        removed: `${year}-11-18T09:00:00Z`,
        source: ACME,
        subpath: "skills/docx",
      },
    ],
  });
  await expect(row(page, "pdf")).toHaveAccessibleDescription(
    /^Added on Oct 2\./u
  );
  await expect(
    rowLine(page, "pdf").getByText("Added on Oct 2", { exact: true })
  ).toBeVisible();
  await expect(
    rowLine(page, "docx").getByText("Removed Nov 18", { exact: true })
  ).toBeVisible();
  cli.register(["source", "read", ACME_ID, "skills/pdf"], {
    events: [skillMd("pdf", "# pdf\n")],
  });
  await row(page, "pdf").click();
  await expect(opened(page, "pdf")).toBeVisible();
  await expect(
    rowLine(page, "pdf").getByText("Added on Oct 2", { exact: true })
  ).toBeVisible();
});

test("many agents: five logos and +n, the agent picker toggles a target, two-letter monograms", async ({
  cli,
  page,
}) => {
  const off = { enabled: false };
  await openSources(cli, page, {
    ...inventory,
    configurations: [
      ...configurations,
      configuration("kilo", { ...off, name: "Kilo Code" }),
      configuration("aider", { ...off, name: "Aider" }),
      configuration("amp", { ...off, name: "Amp" }),
      configuration("cline", { ...off, name: "Cline" }),
      configuration("zed", { ...off, name: "Zed" }),
    ],
  });
  const add = cli.register(
    [
      "skill",
      "add",
      ACME,
      "--name",
      "docx",
      "--to",
      "claude-code",
      "--to",
      "cursor",
      "--to",
      "zed",
    ],
    { events: [{ name: "docx", type: "library_skill" }] }
  );
  await check(page, "docx").click();
  // The chosen agents first, then the rest, five in all; the other three behind +3, last.
  expect(await names(bar(page).getByRole("group", { name: "Add to" }))).toEqual(
    ["Claude Code", "Cursor", "Devin", "Kilo Code", "Aider", "3 more agents"]
  );
  // A client with no bundled logo draws two letters: its initials, or a one-word name's first two.
  await expect(target(page, "Kilo Code").locator("img")).toHaveAttribute(
    "src",
    /%3EKC%3C/u
  );
  await expect(target(page, "Aider").locator("img")).toHaveAttribute(
    "src",
    /%3EAi%3C/u
  );
  await target(page, "Kilo Code").hover();
  await expect(page.getByRole("tooltip")).toHaveText("Kilo Code");

  await target(page, "3 more agents").click();
  const picker = page.getByRole("dialog", { name: "Where to install" });
  await expect(picker).toHaveAccessibleDescription(
    "Choose agents to add the skill to. It is always available to universal agents."
  );
  // The term explains itself on hover.
  await picker.getByRole("button", { name: "universal agents" }).hover();
  await expect(page.getByRole("tooltip")).toHaveText(
    "Universal agents read your skills library, ~/.agents/skills, so every skill you add reaches them."
  );
  // Every agent by name, the universal ones among them, locked on with the Universal pill.
  const agents = picker.getByRole("list", { name: "Agents" });
  await expect(agents.getByRole("listitem")).toHaveText([
    /^Aider/u,
    /^Amp/u,
    /^Claude Code/u,
    /^Cline/u,
    "CodexUniversal/home/me/.codexAlways on",
    /^Cursor/u,
    /^Devin/u,
    "Gemini CLIUniversal/home/me/.gemini-cliAlways on",
    /^Kilo Code/u,
    /^Zed/u,
  ]);
  // The Universal pill says the same, from the keyboard too.
  await page.mouse.move(0, 0);
  await agents.getByRole("button", { name: "Universal" }).first().focus();
  await expect(page.getByRole("tooltip")).toContainText(
    "Universal agents read your skills library"
  );
  expect(
    await agents
      .getByRole("checkbox")
      .evaluateAll((boxes) => boxes.map((b) => b.getAttribute("aria-label")))
  ).toEqual([
    "Aider",
    "Amp",
    "Claude Code",
    "Cline",
    "Cursor",
    "Devin",
    "Kilo Code",
    "Zed",
  ]);
  const search = picker.getByRole("searchbox", { name: "Search agents" });
  await search.fill("gem");
  await expect(agents.getByRole("listitem")).toHaveText([
    "Gemini CLIUniversal/home/me/.gemini-cliAlways on",
  ]);
  await search.fill("ze");
  await agents.getByText("Zed", { exact: true }).click();
  await expect(agents.getByRole("checkbox", { name: "Zed" })).toBeChecked();
  await picker.getByRole("button", { name: "Done" }).click();
  await expect(picker).toHaveCount(0);

  // Zed is chosen, so it moves into the bar.
  await expect(target(page, "Zed")).toHaveAttribute("aria-pressed", "true");
  await bar(page).getByRole("button", { name: "Add 1" }).click();
  await add.started;
  await expect(toasts(page).getByText("Added 1 skill")).toBeVisible();
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
  await expect(selectAll(page)).toHaveAccessibleName("Select all 4");
  await selectAll(page).click();
  await expect(selectAll(page)).toHaveAccessibleName("All 4 selected");
  await expect(selectAll(page)).toHaveAttribute("aria-checked", "true");
  await expect(
    bar(page).getByRole("button", { exact: true, name: "Add 4" })
  ).toBeVisible();
  await check(page, "docx").click();
  await check(page, "forms").click();
  await expect(
    bar(page).getByRole("button", { exact: true, name: "Add 2" })
  ).toBeVisible();
  await expect(selectAll(page)).toHaveAccessibleName("2 of 4 selected");
  await expect(selectAll(page)).toHaveAttribute("aria-checked", "mixed");
  // The Universal stack is always on: a locked mark, not a toggle.
  await expect(bar(page)).toContainText(
    "Skills are always added to universal agents: Codex and Gemini CLI"
  );
  await bar(page).getByRole("button", { name: "Add 2" }).click();
  await add.started;
  await expect(toasts(page).getByText("Added 2 skills")).toBeVisible();
  expect(commandLines(cli)).toContain(line.join(" "));
});

test("while the install runs, Add keeps its size and turns into Stop, and the rim grows without jumping", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  const add = cli.register(
    ["skill", "add", ACME, "--name", "artifacts", "--name", "docx"],
    {
      events: [
        // Three steps per skill and one rescan: the bar counts skills, not steps.
        {
          current: 1,
          phase: "blobs",
          subject: "artifacts",
          total: 7,
          type: "progress",
        },
        {
          current: 2,
          phase: "blobs",
          subject: "docx",
          total: 7,
          type: "progress",
        },
        {
          current: 3,
          phase: "import",
          subject: "artifacts",
          total: 7,
          type: "progress",
        },
        {
          current: 4,
          phase: "import",
          subject: "docx",
          total: 7,
          type: "progress",
        },
        {
          current: 5,
          phase: "install",
          subject: "artifacts",
          total: 7,
          type: "progress",
        },
        {
          current: 6,
          phase: "install",
          subject: "docx",
          total: 7,
          type: "progress",
        },
        { current: 7, phase: "rescan", total: 7, type: "progress" },
        { name: "artifacts", type: "library_skill" },
        { name: "docx", type: "library_skill" },
      ],
      holdAfter: 6,
    }
  );
  await check(page, "artifacts").click();
  await check(page, "docx").click();
  const addButton = bar(page).getByRole("button", { name: "Add 2" });
  const before = await addButton.boundingBox();
  // A double-click starts the install; its second click does not stop it.
  await addButton.dblclick();
  await add.started;
  const stop = bar(page).getByRole("button", { name: "Stop adding" });
  await expect(stop).toHaveAttribute("aria-busy", "true");
  const during = await stop.boundingBox();
  expect(during?.width).toBe(before?.width);
  // The click shows the spinner, not Stop, until the pointer leaves.
  await expect(stop.getByText("Stop", { exact: true })).toBeHidden();
  // No count of steps or skills: the progress is the rim's.
  await expect(bar(page)).not.toContainText("Adding");
  const rim = bar(page).locator("canvas[data-progress]");
  await expect(rim).toBeAttached();
  // Six of seven steps ended at once, yet the rim eases there: never a jump, never full early.
  // Read in the page every 50ms, so the samples are evenly spaced; kept on the canvas.
  await page.waitForFunction(
    (canvas) => {
      const read = `${canvas.dataset.samples ?? ""} ${canvas.dataset.progress}`;
      canvas.dataset.samples = read.trim();
      return canvas.dataset.samples.split(" ").length >= 12;
    },
    await rim.elementHandle(),
    { polling: 50 }
  );
  const samples = ((await rim.getAttribute("data-samples")) ?? "")
    .split(" ")
    .map(Number);
  expect(samples).toHaveLength(12);
  for (const [i, value] of samples.entries()) {
    expect(value).toBeLessThan(0.95);
    if (i > 0) {
      expect(value).toBeGreaterThanOrEqual(samples[i - 1] ?? 0);
      expect(value - (samples[i - 1] ?? 0)).toBeLessThan(0.05);
    }
  }
  await stop.click();
  await expect(
    toasts(page).getByText("Stopped. 2 skills were added.")
  ).toBeVisible();
  expect(cli.cancels).toHaveLength(1);
  // The selection stays, for another try.
  await expect(
    bar(page).getByRole("button", { exact: true, name: "Add 2" })
  ).toBeVisible();
});

test("Retry after the lock installs again, and the bar closes once it ends", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  await check(page, "docx").click();
  cli.register(["skill", "add", ACME, "--name", "docx"], {
    events: [
      {
        code: "locked",
        message: "another agentx command holds the lock",
        type: "error",
      },
    ],
    exitCode: 7,
  });
  await bar(page).getByRole("button", { exact: true, name: "Add 1" }).click();
  await expect(
    toasts(page).getByText("Another agentx command is running")
  ).toBeVisible();
  cli.register(["skill", "add", ACME, "--name", "docx"], {
    events: [
      {
        current: 1,
        phase: "install",
        subject: "docx",
        total: 2,
        type: "progress",
      },
      { name: "docx", type: "library_skill" },
    ],
  });
  await page.getByRole("button", { name: "Retry" }).click();
  await expect(toasts(page).getByText("Added 1 skill")).toBeVisible();
  // The retry's rim ends and fades, so the bar, with nothing selected, goes.
  await expect(bar(page)).toHaveCount(0, { timeout: 10_000 });
});

test("a partial failure lists what was added and why the rest was not, in the app's words", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  const error = {
    code: "refused",
    hint: "remove it with 'agentx skill remove docx', then install again; to keep it, first fork it with 'agentx skill fork docx --name <new>'",
    message:
      "1 of 2 skills could not be installed: docx: the library already holds docx at /home/me/.agents/skills",
    type: "error",
  } as const;
  const held =
    "docx: the library already holds docx at /home/me/.agents/skills";
  const other =
    "/home/me/.cursor/skills is a link of your own and was left as it is; no placement was made for cursor";
  const add = cli.register(
    ["skill", "add", ACME, "--name", "artifacts", "--name", "docx"],
    {
      events: [{ name: "artifacts", type: "library_skill" }, error],
      exitCode: 6,
      logs: [
        { level: "warn", message: held },
        { level: "warn", message: other },
      ],
    }
  );
  await check(page, "artifacts").click();
  await check(page, "docx").click();
  await bar(page).getByRole("button", { name: "Add 2" }).click();
  await add.started;
  const toast = toasts(page)
    .getByRole("listitem")
    .filter({ hasText: "Added 1 of 2 skills" });
  await expect(toast).toBeVisible();
  await expect(toast).toContainText("docx skill is already in your library.");
  await expect(toast).toContainText("Remove it, then add again.");
  // The skill that landed leaves the selection; the skipped one stays.
  await expect(
    bar(page).getByRole("button", { exact: true, name: "Add 1" })
  ).toBeVisible();
  await expect(check(page, "docx")).toHaveAttribute("aria-pressed", "true");
  await expect(check(page, "artifacts")).toHaveAttribute(
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
  await expect(row(page, "artifacts")).toBeVisible();

  // The matching answer takes the place of the source's card and listing: the open source's
  // matches, each source counting its own, Hub once your skills on the hub are read.
  await cli.serveEmit([
    {
      query: "pdf",
      request_id: second.request_id,
      results: docResults,
      type: "search",
    },
  ]);
  await expect(details(page)).toBeHidden();
  expect(await skillNames(grid(page, "acme/skills"))).toEqual(["docx"]);
  await expect(pill(page, "acme/skills")).toHaveText("acme/skills1");
  await expect(pill(page, "zeta/tools")).toHaveText("zeta/tools1");
  await expect(pill(page, "Hub")).toHaveText("Hub0");

  // Another source shows its matches, and the search stays.
  await pill(page, "zeta/tools").click();
  expect(await skillNames(grid(page, "zeta/tools"))).toEqual(["docgen"]);
  await expect(field).toHaveValue("pdf");
  await expect(grid(page, "acme/skills")).toHaveCount(0);
  await pill(page, "acme/skills").click();

  // While the next query waits, the last answer stays: the listing does not come back between.
  await field.fill("pdfz");
  await expect.poll(() => cli.serveRequests.length).toBe(3);
  await expect(row(page, "artifacts")).toHaveCount(0);
  expect(await skillNames(grid(page, "acme/skills"))).toEqual(["docx"]);
  await cli.serveEmit([
    {
      query: "pdfz",
      request_id: lastSearch(cli).request_id,
      results: [],
      type: "search",
    },
  ]);
  await expect(page.getByText("No skills match")).toBeVisible();

  // Clearing the query shows the card and the listing again, and the next query starts from them.
  await field.fill("");
  await expect(row(page, "artifacts")).toBeVisible();
  await expect(details(page)).toBeVisible();
  await expect(pill(page, "acme/skills")).toHaveText("acme/skills8");
  await field.fill("x");
  await expect.poll(() => cli.serveRequests.length).toBe(4);
  await expect(row(page, "artifacts")).toBeVisible();
  await expect(page.getByText("No skills match")).toHaveCount(0);
});

/** A fork on the hub as a search answers it: under the hub's URL, by its branch's name. */
const hubFork = (name: string) => ({
  description: `The ${name} skill`,
  name,
  source: HUB,
  subpath: "",
  tree: `t-${name}`,
});

test("a search covers your skills on the hub: Hub counts and lists the ones to install that match", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  await pill(page, "Hub").click();
  await expect(
    details(page).getByRole("heading", { name: "me/hub" })
  ).toBeVisible();
  const field = page.getByRole("searchbox", { name: "Search all sources" });
  await field.fill("pdf");
  await expect.poll(() => cli.serveRequests.length).toBe(1);
  await cli.serveEmit([
    {
      query: "pdf",
      request_id: lastSearch(cli).request_id,
      // A fork this machine holds already is not one the hub lists to install.
      results: [...docResults, hubFork("held-pdf"), hubFork("my-pdf")],
      type: "search",
    },
  ]);
  await expect(details(page)).toBeHidden();
  await expect(pill(page, "Hub")).toHaveAttribute("aria-pressed", "true");
  await expect(pill(page, "Hub")).toHaveText("Hub1");
  expect(await skillNames(grid(page, "Hub"))).toEqual(["my-pdf"]);

  await field.fill("");
  await expect(
    details(page).getByRole("heading", { name: "me/hub" })
  ).toBeVisible();
  expect(await skillNames(grid(page, "Hub"))).toEqual([
    "my-pdf",
    "release-notes",
  ]);
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
  // A search stays while the dialog is open, and ends once the source is added.
  const field = page.getByRole("searchbox", { name: "Search all sources" });
  await field.fill("doc");
  await page.getByRole("button", { name: "Add source" }).click();
  await expect(dialog(page)).toBeVisible();
  await expect(dialog(page)).toContainText("Add #branch or #tag to pin it");
  await expect(urlField(page)).toBeFocused();
  // The dialog hides the page from the accessibility tree.
  await expect(
    page.getByRole("searchbox", {
      includeHidden: true,
      name: "Search all sources",
    })
  ).toHaveValue("doc");
  await urlField(page).fill("acme/more");
  await dialog(page).getByRole("button", { name: "Add" }).click();
  await expect(dialog(page)).toHaveCount(0);
  await expect(field).toHaveValue("");
  await expect(
    toasts(page).getByText(`Added ${newUrl}: 2 skills`)
  ).toBeVisible();
  await expect(pill(page, "acme/more")).toHaveAttribute("aria-pressed", "true");
  await expect(check(page, "review")).toHaveAttribute("aria-pressed", "true");
  await expect(check(page, "triage")).toHaveAttribute("aria-pressed", "true");
  await expect(
    bar(page).getByRole("button", { exact: true, name: "Add 2" })
  ).toBeVisible();
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
  cli.register(["source", "add", "me/hub"], {
    events: [
      {
        code: "refused",
        hint: "run 'agentx skill list --remote' to see its skills, then 'agentx skill add --name <name>' to install one",
        message:
          "https://github.com/me/hub is the hub: it holds one branch per skill rather than skills on one branch",
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

  await urlField(page).fill("me/hub");
  await dialog(page).getByRole("button", { name: "Add" }).click();
  // The CLI's hint names commands: the dialog names the place in the app instead.
  await expect(alert).toHaveText(
    "https://github.com/me/hub is the hub: it holds one branch per skill rather than skills on one branch. See and install its skills under Hub."
  );
  await expect(dialog(page)).toBeVisible();
  expect(commandLines(cli).slice(-2)).toEqual([
    "source add bad/repo",
    "source add me/hub",
  ]);
});

test("Hub lists the hub's installable skills and adds by name", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  await pill(page, "Hub").click();
  await expect(row(page, "release-notes")).toBeVisible();
  expect(await skillNames(grid(page, "Hub"))).toEqual([
    "my-pdf",
    "release-notes",
  ]);
  await expect(rowLine(page, "my-pdf")).toContainText("from acme/skills");
  // Your skills on the hub open to their description alone.
  await row(page, "release-notes").click();
  await expect(opened(page, "release-notes")).toHaveText("hub skill");

  const add = cli.register(["skill", "add", "--name", "release-notes"], {
    events: [{ name: "release-notes", type: "library_skill" }],
  });
  await check(page, "release-notes").click();
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
  const fetch = cli.register(["source", "fetch", ACME_ID], { events: fetched });
  await details(page).getByRole("button", { name: "Refresh" }).click();
  await fetch.started;
  // The new fetch comes with the inventory that follows it.
  await expect.poll(() => cli.serveRequests.length).toBe(1);
  const now = new Date().toISOString();
  await cli.serveEmit([
    {
      ...inventory,
      scan_counter: 2,
      sources: sources.map((s) =>
        s.id === ACME_ID ? { ...s, last_fetched: now } : s
      ),
    },
  ]);
  await expect(fact(page, "Fetched")).toHaveText("just now");
  await expect
    .poll(() => commandLines(cli))
    .toEqual([
      `source skills ${ACME_ID}`,
      `source fetch ${ACME_ID}`,
      `source skills ${ACME_ID}`,
    ]);
});

test("Refresh all fetches every source in one run, then reads your skills, pending until it ends", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  const fetch = cli.register(["source", "fetch", "--all"], {
    events: [{ id: ACME_ID, type: "source", url: ACME }],
    holdAfter: 0,
  });
  const read = cli.register(["skill", "list", "--remote"], {
    events: installable,
    holdAfter: 0,
  });
  const refreshAll = page.getByRole("button", { name: "Refresh all" });
  // Refresh all ends a search.
  const field = page.getByRole("searchbox", { name: "Search all sources" });
  await field.fill("doc");
  await refreshAll.click();
  await expect(field).toHaveValue("");
  await fetch.started;
  await expect(refreshAll).toHaveAttribute("aria-busy", "true");
  await expect(refreshAll).toBeDisabled();
  fetch.release();
  // Pending until your skills are read again too.
  await read.started;
  await expect(refreshAll).toBeDisabled();
  read.release();
  await expect(refreshAll).toBeEnabled();
  // The new fetch comes with the inventory that follows it.
  const now = new Date().toISOString();
  await cli.serveEmit([
    {
      ...inventory,
      scan_counter: 2,
      sources: sources.map((s) =>
        s.id === ACME_ID ? { ...s, last_fetched: now } : s
      ),
    },
  ]);
  await expect(fact(page, "Fetched")).toHaveText("just now");
  // After the fetch, the listing and your skills are read in either order.
  await expect.poll(() => commandLines(cli).length).toBe(4);
  const lines = commandLines(cli);
  expect(lines.slice(0, 2)).toEqual([
    `source skills ${ACME_ID}`,
    "source fetch --all",
  ]);
  expect(lines.slice(2)).toEqual(
    expect.arrayContaining([`source skills ${ACME_ID}`, "skill list --remote"])
  );
});

test("Remove with its installed skills removes each skill, then the source", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  // The first source, open without a click, with a skill selected.
  await check(page, "artifacts").click();
  const remove = details(page).getByRole("button", { name: "Remove" });
  await remove.click();
  const confirm = page.getByRole("alertdialog", {
    name: "Remove acme/skills?",
  });
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
  await confirm.getByRole("button", { name: "Remove" }).click();
  await expect(confirm).toHaveCount(0);
  // The removed source's card goes, so focus moves to the search field.
  await expect(
    page.getByRole("searchbox", { name: "Search all sources" })
  ).toBeFocused();
  await expect(
    toasts(page).getByText("Source and 1 skill removed")
  ).toBeVisible();
  // The source's pill goes with the next inventory, the first pill opens, and the selection goes.
  await cli.serveEmit([
    {
      ...inventory,
      scan_counter: 2,
      sources: sources.filter((s) => s.id !== ACME_ID),
    },
  ]);
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

test("Retry after the lock resumes the removal: the skill, then the source", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  await details(page).getByRole("button", { name: "Remove" }).click();
  const confirm = page.getByRole("alertdialog", {
    name: "Remove acme/skills?",
  });
  await confirm
    .getByRole("checkbox", { name: "Also remove its 1 installed skill pdf" })
    .click();
  cli.register(["skill", "remove", "pdf"], {
    events: [
      {
        code: "locked",
        message: "another agentx command holds the lock",
        type: "error",
      },
    ],
    exitCode: 7,
  });
  await confirm.getByRole("button", { name: "Remove" }).click();
  await expect(
    toasts(page).getByText("Another agentx command is running")
  ).toBeVisible();
  cli.register(["skill", "remove", "pdf"], {
    events: [{ name: "pdf", type: "library_skill" }],
  });
  cli.register(["source", "remove", ACME], {
    events: [{ id: ACME_ID, type: "source", url: ACME }],
  });
  await page.getByRole("button", { name: "Retry" }).click();
  await expect(
    toasts(page).getByText("Source and 1 skill removed")
  ).toBeVisible();
  expect(commandLines(cli)).toEqual([
    `source skills ${ACME_ID}`,
    "skill remove pdf",
    "skill remove pdf",
    `source remove ${ACME}`,
  ]);
});

test("Remove alone removes the source and keeps its skills", async ({
  cli,
  page,
}) => {
  await openSources(cli, page);
  await pill(page, "zeta/tools").click();
  await details(page).getByRole("button", { name: "Remove" }).click();
  const confirm = page.getByRole("alertdialog", { name: "Remove zeta/tools?" });
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

test("Select all in a source whose skills share a name: --all keeps the selected one of the name", () => {
  const listing: SourceSkillEvent[] = [
    ["alpha/pdf", "pdf"],
    ["beta/pdf", "pdf"],
    ["format", "format"],
  ].map(([subpath = "", name = ""]) => ({
    description: name,
    name,
    schema_version: 5,
    source: ACME_ID,
    subpath,
    tree: `t-${subpath}`,
    type: "source_skill",
  }));
  const cards = listingCards(listing, ACME, []);
  const selectable = cards.filter((c) => c.status === undefined);
  expect(selectable.map((c) => c.skillMd?.subpath)).toEqual([
    "alpha/pdf",
    "format",
  ]);
  const plan = (selected: typeof cards) =>
    planAdds({
      allOf: { cards, source: ACME },
      enabled: [],
      selected,
      targets: [],
    }).map((p) => p.args);
  expect(plan(selectable)).toEqual([["skill", "add", ACME, "--all"]]);
  expect(plan(selectable.filter((c) => c.name !== "pdf"))).toEqual([
    ["skill", "add", ACME, "--all", "--except", "pdf", "--except", "pdf"],
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

test("the page of a skill in its source", () => {
  const cases: [string, string, string, string | undefined][] = [
    [ACME, "v2", "skills/docx", `${ACME}/tree/v2/skills/docx`],
    [ACME, "release/1.x", "", `${ACME}/tree/release/1.x`],
    [
      "https://gitlab.com/group/sub/repo",
      "HEAD",
      "tools/a b",
      "https://gitlab.com/group/sub/repo/-/tree/HEAD/tools/a%20b",
    ],
    ["https://git.example.com/team/skills", "HEAD", "x", undefined],
    ["/home/me/skills", "HEAD", "x", undefined],
  ];
  expect(
    cases.map(([url, ref, subpath]) => [
      url,
      ref,
      subpath,
      skillPage(url, ref, subpath),
    ])
  ).toEqual(cases);
});

test("the dates of a card's skill: a source's by its source and subpath, a hub skill's by its fork id", () => {
  const added = "2026-10-02T09:00:00Z";
  const installs: Install[] = [
    { added, fork_id: "a1a1a1a1a1a1", name: "my-pdf" },
    { added, name: "pdf", source: ZETA, subpath: "skills/pdf" },
    { added, name: "pdf", source: ACME, subpath: "skills/pdf" },
    { added, name: "docx", source: ACME, subpath: "docx" },
  ];
  const cards = [
    ...listingCards(acmeSkills, ACME, library),
    ...installableCards(installable, library),
  ];
  // Which entry each card reads, by its index; -1 for none.
  const read = (name: string) => {
    const found = cards.find((c) => c.name === name);
    const install =
      found === undefined ? undefined : installOf(found, installs);
    return install === undefined ? -1 : installs.indexOf(install);
  };
  const cases: [string, number][] = [
    ["pdf", 2],
    ["docx", -1],
    ["my-pdf", 0],
    ["release-notes", -1],
  ];
  expect(cases.map(([name]) => [name, read(name)])).toEqual(cases);
});
