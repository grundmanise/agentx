// The Inbox: the cards for what needs a decision, each with the command that settles it, the
// headline and the sidebar badge that count them, RECENT, and the matching Adopt pill and banners
// of the Skills screen. Every button runs exactly one command line, which the mocked CLI records,
// and a card leaves only once its state is gone from the store.
import type { Locator, Page } from "@playwright/test";

import {
  adoption,
  IN_THE_WAY,
  inventory,
  OLD,
  serveStart,
} from "./fixtures/inbox.ts";
import { filesOf } from "./fixtures/skills.ts";
import { expect, loadLines, test } from "./harness/cli.ts";
import type { InlineEvent, MockCli } from "./harness/cli.ts";

const launchLines = [
  ["version", "--json"],
  ["doctor", "--json"],
];

const isLaunchOrLoad = (line: string[]) =>
  [...launchLines, ...loadLines].some(
    (known) => known.join(" ") === line.join(" ")
  );

const detailReads = new Set(["skill history", "skill files", "skill diff"]);

/** The command lines the controls ran: everything but the launch, the store's and the detail pane's reads. */
const later = (cli: MockCli) =>
  cli.commands.filter(
    (l) => !(isLaunchOrLoad(l) || detailReads.has(l.slice(0, 2).join(" ")))
  );

const toasts = (page: Page) =>
  page.getByRole("region", { name: /^Notifications/u });
const cards = (page: Page) =>
  page.getByRole("list", { name: "Needs you" }).getByRole("article");
const card = (page: Page, name: string) =>
  page.getByRole("list", { name: "Needs you" }).getByRole("article", { name });
const headline = (page: Page) =>
  page.getByRole("heading", {
    level: 2,
    name: /(?<clause>need you\.|needs you\.|All clear\.)$/u,
  });
const inboxLink = (page: Page) =>
  page.getByRole("navigation", { name: "Main" }).getByRole("link", {
    name: /^Inbox/u,
  });
const recent = (page: Page) =>
  page.getByRole("region", { name: "Recent" }).getByRole("listitem");
const pane = (page: Page, name: string): Locator =>
  page
    .getByRole("region", { name: "Skill details" })
    .getByRole("article", { name });
const list = (page: Page) => page.getByRole("navigation", { name: "Skills" });

/** The names of the cards shown, in order. */
const cardNames = async (page: Page) =>
  await cards(page).evaluateAll((articles) =>
    articles.map((a) => a.getAttribute("aria-label"))
  );

/** Opens the Inbox on the fixture machine. */
const openInbox = async (cli: MockCli, page: Page, start = serveStart) => {
  cli.onServeStart({ events: start });
  cli.register(["adopt"], { events: adoption });
  await page.goto("/#/inbox");
  await expect(page.getByRole("heading", { name: "Inbox" })).toBeAttached();
};

/** Answers the last refresh request, as serve does after the scan that follows a command. */
const answerRefresh = async (cli: MockCli, events: InlineEvent[] = []) => {
  const requestId = cli.requestIds.at(-1) ?? "";
  await cli.serveEmit([
    ...events,
    {
      ok: true,
      request_id: requestId,
      scan_counter: 2,
      type: "refresh_complete",
    },
  ]);
};

const sixCards = [
  "scratch · skills CLI",
  "release-notes · Cursor",
  "draft · your skill",
  "pdf-letters · your skill",
  "lint · old/skills",
  "gone · zeta/tools",
];

test("the date line, the headline, the cards in order with their copy, and the badge", async ({
  cli,
  page,
}) => {
  await openInbox(cli, page);
  const date = page.getByText(
    new Date().toLocaleDateString("en-US", {
      day: "numeric",
      month: "long",
      timeZone: "UTC",
      weekday: "long",
    })
  );
  await expect(date).toBeVisible();
  await expect(date).toHaveCSS("text-transform", "uppercase");
  await expect(headline(page)).toHaveText("Six things need you.");
  await expect(inboxLink(page)).toHaveAccessibleName("Inbox, 6 need you");
  await expect(inboxLink(page)).toContainText("6");
  expect(await cardNames(page)).toEqual(sixCards);

  const adoptable = card(page, "scratch · skills CLI");
  await expect(adoptable).toContainText(
    "Installed by the skills CLI from vercel/skills. Adopt it and agentx can update it."
  );
  await expect(adoptable.getByRole("button")).toHaveText(["Adopt"]);

  const displaced = card(page, "release-notes · Cursor");
  await expect(displaced).toContainText(
    "Changes you make in your library won't reach Cursor until you reconnect it."
  );
  await expect(displaced.getByRole("button")).toHaveText(["Keep", "Reconnect"]);

  const missing = card(page, "draft · your skill");
  await expect(missing).toContainText(
    "Its folder is missing. agentx can restore it from your saved version; edits you had not published are gone."
  );
  await expect(missing.getByRole("button")).toHaveText(["Restore"]);

  const inTheWay = card(page, "pdf-letters · your skill");
  await expect(inTheWay).toContainText(
    `A different folder is where pdf-letters should be: ${IN_THE_WAY}. agentx can take it over; its files become unpublished edits of pdf-letters.`
  );
  await expect(inTheWay.getByRole("button")).toHaveText([
    "Keep",
    "Take it over",
  ]);

  const sourceRemoved = card(page, "lint · old/skills");
  await expect(sourceRemoved).toContainText(
    "Its source was removed, so it won't get updates."
  );
  await expect(sourceRemoved.getByRole("button")).toHaveText([
    "Remove",
    "Add source again",
  ]);

  const upstreamRemoved = card(page, "gone · zeta/tools");
  await expect(upstreamRemoved).toContainText(
    "Removed from its source. It still works but won't get updates."
  );
  await expect(upstreamRemoved.getByRole("button")).toHaveText([
    "Keep as is",
    "Fork",
  ]);
  expect(later(cli)).toEqual([]);
});

test.describe("actions", () => {
  test("Adopt records adopt --skill and toasts; the card stays until the preview no longer lists it", async ({
    cli,
    page,
  }) => {
    await openInbox(cli, page);
    cli.register(["adopt", "--skill", "scratch"], {
      events: [
        {
          name: "scratch",
          source: "https://github.com/vercel/skills",
          state: "adopted",
          type: "adoption",
        },
      ],
    });
    const adoptable = card(page, "scratch · skills CLI");
    const adopt = adoptable.getByRole("button", { name: "Adopt" });
    await adopt.click();
    await expect(toasts(page).getByText("Adopted scratch")).toBeVisible();
    await expect(adopt).toHaveAttribute("aria-busy", "true");
    expect(later(cli)).toEqual([["adopt", "--skill", "scratch", "--json"]]);
    // The command exited 0, but the preview still lists the skill: the card stays.
    await expect.poll(() => cli.serveRequests.length).toBe(1);
    await expect(headline(page)).toHaveText("Six things need you.");
    await expect(adoptable).toBeVisible();

    // The library changed, so the app reads the preview again, which no longer lists it.
    cli.register(["adopt"], {
      events: adoption.filter((a) => a.name !== "scratch"),
    });
    await answerRefresh(cli, [
      {
        ...inventory,
        library: inventory.library.map((e) =>
          e.name === "scratch"
            ? {
                ...e,
                kind: "managed",
                source: "https://github.com/vercel/skills",
                state: "current",
                subpath: "scratch",
              }
            : e
        ),
        scan_counter: 2,
      },
    ]);
    await expect(adoptable).toHaveCount(0);
    await expect(headline(page)).toHaveText("Five things need you.");
    await expect(inboxLink(page)).toHaveAccessibleName("Inbox, 5 need you");
    await expect(cards(page)).toHaveCount(5);
    expect(await cardNames(page)).toEqual(sixCards.slice(1));
  });

  test("Reconnect records skill place --to and toasts", async ({
    cli,
    page,
  }) => {
    await openInbox(cli, page);
    cli.register(["skill", "place", "release-notes", "--to", "cursor"], {
      events: [
        {
          ok: true,
          summary: "placed release-notes in 1 configuration",
          type: "result",
        },
      ],
    });
    await card(page, "release-notes · Cursor")
      .getByRole("button", { name: "Reconnect" })
      .click();
    await expect(
      toasts(page).getByText("Reconnected release-notes to Cursor")
    ).toBeVisible();
    expect(later(cli)).toEqual([
      ["skill", "place", "release-notes", "--to", "cursor", "--json"],
    ]);
  });

  test("Restore records skill place and the card leaves once it exits 0", async ({
    cli,
    page,
  }) => {
    await openInbox(cli, page);
    const missing = card(page, "draft · your skill");
    const restore = cli.register(["skill", "place", "draft"], {
      holdAfter: 0,
    });
    await missing.getByRole("button", { name: "Restore" }).click();
    await restore.started;
    await expect(
      missing.getByRole("button", { name: "Restore" })
    ).toHaveAttribute("aria-busy", "true");
    await expect(missing).toBeVisible();
    restore.release();
    await expect(toasts(page).getByText("Restored draft")).toBeVisible();
    await expect(missing).toHaveCount(0);
    await expect(headline(page)).toHaveText("Five things need you.");
    expect(later(cli)).toEqual([["skill", "place", "draft", "--json"]]);
  });

  test("Take it over confirms, then records skill place --force", async ({
    cli,
    page,
  }) => {
    await openInbox(cli, page);
    const inTheWay = card(page, "pdf-letters · your skill");
    cli.register(["skill", "place", "pdf-letters", "--force"]);
    await inTheWay.getByRole("button", { name: "Take it over" }).click();
    const dialog = page.getByRole("dialog", { name: "Take over this folder?" });
    await expect(dialog).toContainText(
      "Its files become unpublished edits of pdf-letters."
    );
    await expect(dialog).toContainText(IN_THE_WAY);
    await dialog.getByRole("button", { name: "Cancel" }).click();
    await expect(dialog).toBeHidden();
    expect(later(cli)).toEqual([]);

    await inTheWay.getByRole("button", { name: "Take it over" }).click();
    await dialog.getByRole("button", { name: "Take it over" }).click();
    await expect(dialog).toBeHidden();
    await expect(
      toasts(page).getByText(`Took over ${IN_THE_WAY}`)
    ).toBeVisible();
    await expect(inTheWay).toHaveCount(0);
    expect(later(cli)).toEqual([
      ["skill", "place", "pdf-letters", "--force", "--json"],
    ]);
  });

  test("Reconnect refused opens the Replace dialog, whose Replace adds --force", async ({
    cli,
    page,
  }) => {
    await openInbox(cli, page);
    const path = "/home/me/.cursor/skills/release-notes";
    cli.register(["skill", "place", "release-notes", "--to", "cursor"], {
      events: [
        {
          code: "refused",
          hint: "to replace it with the library's version and delete what it holds, run 'agentx skill place release-notes --to cursor --force'; to keep it, move it elsewhere first",
          message: `${path} is a directory whose content differs from the library's release-notes, so nothing was placed`,
          type: "error",
        },
      ],
      exitCode: 6,
    });
    cli.register(
      ["skill", "place", "release-notes", "--to", "cursor", "--force"],
      {
        events: [
          {
            ok: true,
            summary: `placed release-notes in 1 configuration; discarded what ${path} held`,
            type: "result",
          },
        ],
      }
    );
    const displaced = card(page, "release-notes · Cursor");
    await displaced.getByRole("button", { name: "Reconnect" }).click();
    const dialog = page.getByRole("dialog", { name: "Replace Cursor's copy?" });
    await expect(dialog).toContainText(
      "Cursor's copy of release-notes is different from the one in your library. Replacing it deletes Cursor's changes."
    );
    await expect(dialog).toContainText(path);
    await expect(toasts(page).getByRole("listitem")).toHaveCount(0);
    await dialog.getByRole("button", { name: "Replace" }).click();
    await expect(dialog).toBeHidden();
    await expect(
      toasts(page).getByText("Reconnected release-notes to Cursor")
    ).toBeVisible();
    expect(later(cli)).toEqual([
      ["skill", "place", "release-notes", "--to", "cursor", "--json"],
      [
        "skill",
        "place",
        "release-notes",
        "--to",
        "cursor",
        "--force",
        "--json",
      ],
    ]);
  });

  test("Remove opens the Remove dialog, and Add source again records source add", async ({
    cli,
    page,
  }) => {
    await openInbox(cli, page);
    const sourceRemoved = card(page, "lint · old/skills");
    cli.register(["skill", "remove", "lint"]);
    await sourceRemoved.getByRole("button", { name: "Remove" }).click();
    const dialog = page.getByRole("dialog", { name: "Remove lint?" });
    await expect(dialog).toContainText(
      "It will be deleted from every agent on this machine:"
    );
    await expect(
      dialog
        .getByRole("list", { name: "Agents that see it" })
        .getByRole("listitem")
    ).toHaveText(["Claude Code", "Codex", "Gemini CLI"]);
    await dialog.getByRole("button", { name: "Remove" }).click();
    await expect(toasts(page).getByText("Removed lint")).toBeVisible();

    cli.register(["source", "add", OLD]);
    await sourceRemoved
      .getByRole("button", { name: "Add source again" })
      .click();
    await expect(
      toasts(page).getByText("Added old/skills again")
    ).toBeVisible();
    expect(later(cli)).toEqual([
      ["skill", "remove", "lint", "--json"],
      ["source", "add", OLD, "--json"],
    ]);
  });

  test("Fork records skill fork and toasts; a failure keeps the card and shows the error", async ({
    cli,
    page,
  }) => {
    await openInbox(cli, page);
    const upstreamRemoved = card(page, "gone · zeta/tools");
    const fork = upstreamRemoved.getByRole("button", { name: "Fork" });
    cli.register(["skill", "fork", "gone"], {
      events: [
        {
          code: "git",
          message: "git failed: cannot lock ref 'refs/heads/skills/gone'",
          type: "error",
        },
      ],
      exitCode: 1,
    });
    await fork.click();
    await expect(
      toasts(page).getByText(
        "git failed: cannot lock ref 'refs/heads/skills/gone'"
      )
    ).toBeVisible();
    await answerRefresh(cli);
    await expect(upstreamRemoved).toBeVisible();
    await expect(headline(page)).toHaveText("Six things need you.");

    cli.register(["skill", "fork", "gone"], {
      events: [{ name: "gone", type: "library_skill" }],
    });
    await fork.click();
    await expect(
      toasts(page).getByText("Forked gone. It's one of your skills now")
    ).toBeVisible();
    expect(later(cli)).toEqual([
      ["skill", "fork", "gone", "--json"],
      ["skill", "fork", "gone", "--json"],
    ]);
  });

  test("Keep and Keep as is hide the card until the app restarts and run nothing", async ({
    cli,
    page,
  }) => {
    await openInbox(cli, page);
    await card(page, "release-notes · Cursor")
      .getByRole("button", { name: "Keep" })
      .click();
    await card(page, "pdf-letters · your skill")
      .getByRole("button", { name: "Keep" })
      .click();
    await card(page, "gone · zeta/tools")
      .getByRole("button", { name: "Keep as is" })
      .click();
    await expect(headline(page)).toHaveText("Three things need you.");
    await expect(cards(page)).toHaveCount(3);
    expect(await cardNames(page)).toEqual([
      "scratch · skills CLI",
      "draft · your skill",
      "lint · old/skills",
    ]);
    expect(later(cli)).toEqual([]);

    // A reload is a restart: the cards are back.
    cli.restart();
    await page.reload();
    await expect(headline(page)).toHaveText("Six things need you.");
    expect(await cardNames(page)).toEqual(sixCards);
  });
});

test("All clear: nothing to decide, with the way to Skills and Add", async ({
  page,
}) => {
  await page.goto("/#/inbox");
  await expect(headline(page)).toHaveText("All clear.");
  await expect(
    page.getByText("Nothing needs you. We'll surface changes here.")
  ).toBeVisible();
  await expect(inboxLink(page)).toHaveAccessibleName("Inbox");
  await page.getByRole("button", { name: "Browse skills" }).click();
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();
  await page.goto("/#/inbox");
  await page.getByRole("button", { name: "Add skills" }).click();
  await expect(page.getByRole("heading", { name: "Add skills" })).toBeVisible();
});

test("Recent lists what just happened, newest first, at most nine", async ({
  cli,
  page,
}) => {
  await openInbox(cli, page);
  // The two repairs serve left as they are at its start are cards, not entries.
  await expect(page.getByRole("region", { name: "Recent" })).toContainText(
    "Nothing yet"
  );
  await cli.serveEmit([
    {
      kind: "managed",
      name: "pdf",
      outcome: "repaired",
      type: "reconcile",
    },
    {
      candidate: "9b8c7d6e5f4a3",
      files: [],
      instance_id: "instance-1",
      kind: "managed",
      name: "pdf",
      type: "update_available",
    },
  ]);
  await expect(recent(page)).toHaveText([
    /pdf has an update$/u,
    /Put pdf back$/u,
  ]);
  await expect(recent(page).first()).toHaveAttribute("data-tone", "success");
  await expect(recent(page).first().getByRole("time")).toHaveText(
    /^\d\d:\d\d$/u
  );

  const drifts: InlineEvent[] = Array.from({ length: 10 }, (_, i) => ({
    drift: ["missing"],
    instance_id: "instance-1",
    kind: "managed",
    name: `skill-${i}`,
    previous_drift: [],
    scan_counter: 2,
    state: "current",
    type: "drift",
  }));
  await cli.serveEmit(drifts);
  await expect(recent(page)).toHaveCount(9);
  await expect(recent(page).first()).toHaveText(/skill-9 is now missing$/u);

  // An action's outcome is prepended as it happens.
  cli.register(["skill", "fork", "gone"]);
  await card(page, "gone · zeta/tools")
    .getByRole("button", { name: "Fork" })
    .click();
  await expect(recent(page).first()).toHaveText(
    /Forked gone\. It's one of your skills now$/u
  );
  await expect(recent(page)).toHaveCount(9);
});

/** Opens the Skills screen on the Inbox fixture machine with `key` selected. */
const openSkills = async (cli: MockCli, page: Page, key: string) => {
  cli.onServeStart({ events: serveStart });
  cli.register(["adopt"], { events: adoption });
  for (const name of ["scratch", "legacy", "lint", "gone"]) {
    cli.register(["skill", "files", name], { events: filesOf(name) });
  }
  await page.goto(`/#/skills/${key}`);
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();
};

test.describe("Skills additions", () => {
  test("the Adopt pill records adopt --skill and does not select the row", async ({
    cli,
    page,
  }) => {
    await openSkills(cli, page, "gone");
    cli.register(["adopt", "--skill", "scratch"]);
    const pill = list(page).getByRole("button", { name: "Adopt scratch" });
    await expect(pill).toHaveText("Adopt");
    await pill.click();
    await expect(toasts(page).getByText("Adopted scratch")).toBeVisible();
    await expect(
      list(page).getByRole("button", { exact: true, name: "scratch" })
    ).not.toHaveAttribute("aria-current", "true");
    await expect(pane(page, "gone")).toBeVisible();
    // The refused entry gets no pill.
    await expect(
      list(page).getByRole("button", { name: "Adopt legacy" })
    ).toHaveCount(0);
    expect(later(cli)).toEqual([["adopt", "--skill", "scratch", "--json"]]);
  });

  test("the three banners record the same commands as the cards", async ({
    cli,
    page,
  }) => {
    await openSkills(cli, page, "scratch");
    cli.register(["adopt", "--skill", "scratch"]);
    const adoptBanner = pane(page, "scratch").getByRole("note", {
      name: "Installed by the skills CLI",
    });
    await expect(adoptBanner).toContainText(
      "Adopt it and agentx can update it from vercel/skills. It installs the skill from there again, so any edits to this copy are replaced."
    );
    await adoptBanner.getByRole("button", { name: "Adopt" }).click();
    await expect(toasts(page).getByText("Adopted scratch")).toBeVisible();

    await list(page).getByRole("button", { exact: true, name: "lint" }).click();
    cli.register(["source", "add", OLD]);
    const sourceBanner = pane(page, "lint").getByRole("note", {
      name: "Source removed",
    });
    await expect(sourceBanner).toContainText(
      "Add old/skills again to check for updates."
    );
    await sourceBanner
      .getByRole("button", { name: "Add source again" })
      .click();
    await expect(
      toasts(page).getByText("Added old/skills again")
    ).toBeVisible();

    await list(page).getByRole("button", { exact: true, name: "gone" }).click();
    cli.register(["skill", "fork", "gone"]);
    const forkBanner = pane(page, "gone").getByRole("note", {
      name: "Removed from its source",
    });
    await expect(forkBanner).toContainText(
      "It still works but won't get updates."
    );
    await forkBanner.getByRole("button", { name: "Fork" }).click();
    await expect(
      toasts(page).getByText("Forked gone. It's one of your skills now")
    ).toBeVisible();
    expect(later(cli)).toEqual([
      ["adopt", "--skill", "scratch", "--json"],
      ["source", "add", OLD, "--json"],
      ["skill", "fork", "gone", "--json"],
    ]);
  });

  test("a refused entry has no card and no pill, and Details says why", async ({
    cli,
    page,
  }) => {
    await openSkills(cli, page, "legacy");
    const article = pane(page, "legacy");
    await expect(article.getByRole("note")).toHaveCount(0);
    const details = article.getByRole("region", { name: "Details" });
    await expect(details.getByRole("term")).toHaveText([
      "Found at",
      "Installed by",
      "Adopt",
    ]);
    await expect(details.getByRole("definition").last()).toHaveText(
      "Can't adopt: legacy is a symlink, not a directory"
    );
    await page.getByRole("link", { name: /^Inbox/u }).click();
    await expect(headline(page)).toHaveText("Six things need you.");
    expect(await cardNames(page)).toEqual(sixCards);
    expect(later(cli)).toEqual([]);
  });
});
