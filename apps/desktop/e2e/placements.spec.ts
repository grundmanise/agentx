// The controls of the Skills detail pane that change where a skill is: the AGENTS chips, the
// banners' buttons and Remove. Each runs exactly one command line, which the mocked CLI records,
// and shows pending until the command ends and serve answers the refresh that follows.
import type { Locator, Page } from "@playwright/test";

import {
  inventory,
  manyAgentsInventory,
  manyConfigurations,
} from "./fixtures/agents.ts";
import {
  adoption,
  filesOf,
  linked,
  registerShowReads,
} from "./fixtures/skills.ts";
import { expect, loadLines, test } from "./harness/cli.ts";
import type { InlineEvent, MockCli } from "./harness/cli.ts";

const launchLines = [
  ["version", "--json"],
  ["doctor", "--json"],
];

/** Whether a command line is one the launch or the store's reads run. */
const isLaunchOrLoad = (line: string[]) =>
  [...launchLines, ...loadLines].some(
    (known) => known.join(" ") === line.join(" ")
  );

/** The command lines the controls ran: everything but the launch, the store's and the pane's reads. */
const later = (cli: MockCli) =>
  cli.commands.filter(
    (l) => !(isLaunchOrLoad(l) || l.slice(0, 2).join(" ") === "skill show")
  );

const toasts = (page: Page) =>
  page.getByRole("region", { name: /^Notifications/u });
/**
 * The toast on screen. A replaced toast stays in the region for its exit animation (Sonner marks
 * it not front, then removed), so this skips it.
 */
const liveToast = (page: Page) =>
  toasts(page).locator(
    '[data-sonner-toast][data-front="true"]:not([data-removed="true"])'
  );
const pane = (page: Page, name: string) =>
  page
    .getByRole("region", { name: "Skill details" })
    .getByRole("article", { name });
const chip = (article: Locator, name: string) =>
  article.getByRole("switch", { exact: true, name });

/** Opens a skill on the fixture machine: a library skill by its name, any other by its row's key. */
const openSkill = async (
  cli: MockCli,
  page: Page,
  name: string,
  key = name,
  machine: InlineEvent = inventory,
  records: InlineEvent[] = adoption
) => {
  cli.onServeStart({ events: [machine] });
  cli.register(["skill", "import"], { events: records });
  registerShowReads(cli);
  await page.goto(`/#/skills/${encodeURIComponent(key)}`);
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();
  await expect(pane(page, name)).toBeVisible();
};

/** Answers the last refresh request, as serve does after the scan that follows a command. */
const answerRefresh = async (cli: MockCli) => {
  const requestId = cli.requestIds.at(-1) ?? "";
  await cli.serveEmit([
    {
      ok: true,
      request_id: requestId,
      scan_counter: 1,
      type: "refresh_complete",
    },
  ]);
};

test.describe("chips", () => {
  test("a direct agent, off: adds the placement, pending until the refresh, with Undo", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "pdf");
    const article = pane(page, "pdf");
    const cursor = chip(article, "Cursor");
    await expect(cursor).not.toBeChecked();
    await expect(cursor).toHaveAttribute("title", "Add to Cursor");
    const place = cli.register(["skill", "place", "pdf", "--to", "cursor"], {
      events: [
        { ok: true, summary: "placed pdf in 1 configuration", type: "result" },
      ],
      holdAfter: 0,
    });

    await cursor.click();
    await place.started;
    await expect(cursor).toHaveAttribute("aria-busy", "true");
    // The chip shows what the inventory says, not what the command will do.
    await expect(cursor).not.toBeChecked();
    place.release();
    await expect(
      toasts(page).getByText("Added pdf skill to Cursor")
    ).toBeVisible();
    await expect.poll(() => cli.serveRequests.length).toBe(1);
    // The command ended; the inventory that follows has not come: still pending.
    await expect(cursor).toHaveAttribute("aria-busy", "true");
    await answerRefresh(cli);
    await expect(cursor).not.toHaveAttribute("aria-busy");
    expect(later(cli)).toEqual([
      ["skill", "place", "pdf", "--to", "cursor", "--json"],
    ]);

    cli.register(["skill", "remove", "pdf", "--from", "cursor"]);
    await toasts(page).getByRole("button", { name: "Undo" }).click();
    await expect(
      toasts(page).getByText("Removed pdf skill from Cursor")
    ).toBeVisible();
    expect(later(cli)).toEqual([
      ["skill", "place", "pdf", "--to", "cursor", "--json"],
      ["skill", "remove", "pdf", "--from", "cursor", "--json"],
    ]);
  });

  test("a direct agent, on: removes the placement, with Undo", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "pdf");
    const article = pane(page, "pdf");
    const claude = chip(article, "Claude Code");
    await expect(claude).toBeChecked();
    await expect(claude).toHaveAttribute("title", "Remove from Claude Code");
    cli.register(["skill", "remove", "pdf", "--from", "claude-code"]);
    cli.register(["skill", "place", "pdf", "--to", "claude-code"]);

    await claude.click();
    await expect(
      toasts(page).getByText("Removed pdf skill from Claude Code")
    ).toBeVisible();
    await toasts(page).getByRole("button", { name: "Undo" }).click();
    await expect(
      toasts(page).getByText("Added pdf skill to Claude Code")
    ).toBeVisible();
    expect(later(cli)).toEqual([
      ["skill", "remove", "pdf", "--from", "claude-code", "--json"],
      ["skill", "place", "pdf", "--to", "claude-code", "--json"],
    ]);
  });

  test("a removal that left a directory agentx did not place: says so, with the warning and no Undo", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "pdf");
    const warning =
      "/home/me/.claude/skills/pdf is a directory agentx did not place there and was left as it is; claude-code still sees it";
    cli.register(["skill", "remove", "pdf", "--from", "claude-code"], {
      events: [
        {
          ok: true,
          summary:
            "removed pdf from claude-code: 0 placements, 1 placement left in place",
          type: "result",
        },
      ],
      logs: [{ level: "warn", message: warning }],
    });

    await chip(pane(page, "pdf"), "Claude Code").click();
    const toast = toasts(page)
      .getByRole("listitem")
      .filter({ hasText: "pdf skill wasn't removed from Claude Code" });
    await expect(toast).toContainText(
      "/home/me/.claude/skills/pdf is a directory agentx didn't place. It was left as is."
    );
    await expect(toast).not.toContainText("still sees it");
    await expect(toast.getByRole("button", { name: "Undo" })).toHaveCount(0);
    await expect(toast.getByRole("button", { name: "Dismiss" })).toBeVisible();
    expect(later(cli)).toEqual([
      ["skill", "remove", "pdf", "--from", "claude-code", "--json"],
    ]);
  });

  test("a skipped placement: says it wasn't added, with the warning and no Undo", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "pdf");
    const warning =
      "/home/me/.config/devin/skills/pdf is not this skill and was left as it is; no placement was made for devin";
    cli.register(["skill", "place", "pdf", "--to", "devin"], {
      events: [
        {
          ok: true,
          summary: "placed pdf in 0 configurations, 1 placement skipped",
          type: "result",
        },
      ],
      logs: [{ level: "warn", message: warning }],
    });

    await chip(pane(page, "pdf"), "Devin").click();
    const toast = toasts(page)
      .getByRole("listitem")
      .filter({ hasText: "pdf skill wasn't added to Devin" });
    await expect(toast).toContainText(
      "/home/me/.config/devin/skills/pdf has other files. It was left as is."
    );
    await expect(toast.getByRole("button", { name: "Undo" })).toHaveCount(0);
    await expect(toast.getByRole("button", { name: "Dismiss" })).toBeVisible();
    expect(later(cli)).toEqual([
      ["skill", "place", "pdf", "--to", "devin", "--json"],
    ]);
  });

  test("locked chips toast the lock reason", async ({ cli, page }) => {
    cli.register(["skill", "show", "linked", "--configuration", "cursor"], {
      events: filesOf("linked"),
    });
    await openSkill(cli, page, "commit-helper", "skill:h-commit", {
      ...inventory,
      skills: [...inventory.skills, linked],
    });
    const plugin = pane(page, "commit-helper");
    const reason = "Comes with the acme-tools plugin. Fork to edit";
    await expect(chip(plugin, "Cursor")).toHaveAttribute("title", reason);
    await chip(plugin, "Cursor").click();
    await expect(liveToast(page).getByText(reason)).toBeVisible();
    const cursorToast = await liveToast(page).elementHandle();
    await chip(plugin, "Universal").click();
    // The Cursor toast gives way to the Universal one, with the same text.
    await expect
      .poll(async () => {
        const gone = await cursorToast.evaluate(
          (toast) => !toast.isConnected || toast.dataset.removed === "true"
        );
        return gone;
      })
      .toBe(true);
    await expect(liveToast(page).getByText(reason)).toBeVisible();

    const skills = page.getByRole("navigation", { name: "Skills" });
    await skills.getByRole("button", { exact: true, name: "scratch" }).click();
    const candidate = pane(page, "scratch");
    const adopt = "This skill is not managed by agentx. Import it to manage";
    await expect(chip(candidate, "Claude Code")).toHaveAttribute(
      "title",
      adopt
    );
    await chip(candidate, "Claude Code").click();
    await expect(toasts(page).getByText(adopt)).toBeVisible();
    // The toast offers what unlocks the chips: Adopt, for a skill another tool installed.
    cli.register(["skill", "import", "scratch"]);
    await toasts(page).getByRole("button", { name: "Import" }).click();
    await expect(
      toasts(page).getByText("Imported scratch skill")
    ).toBeVisible();

    // A skill an agent holds in its own folder is adopted by its directory's name, and selected
    // as the library skill the CLI reports.
    // The scratch toast, with the same text, leaves before the notes one opens.
    await expect(toasts(page).getByText(adopt)).toHaveCount(0);
    await skills.getByRole("button", { exact: true, name: "notes" }).click();
    await chip(pane(page, "notes"), "Claude Code").click();
    await expect(toasts(page).getByText(adopt)).toBeVisible();
    cli.register(["skill", "import", "notes"], {
      events: [{ name: "notes", type: "library_skill" }],
    });
    await toasts(page).getByRole("button", { name: "Import" }).click();
    await expect(toasts(page).getByText("Imported notes skill")).toBeVisible();
    await expect(page).toHaveURL(/#\/skills\/notes$/u);

    // A symlink in an agent's folder is not adopt's to take: the toast has no action.
    await skills.getByRole("button", { exact: true, name: "linked" }).click();
    await chip(pane(page, "linked"), "Claude Code").click();
    await expect(
      toasts(page).getByText("This skill is not managed by agentx", {
        exact: true,
      })
    ).toBeVisible();
  });

  test("an unmanaged library skill no tool recorded: the toast's Adopt takes it over", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "scratch", "scratch", inventory, []);
    await chip(pane(page, "scratch"), "Claude Code").click();
    await expect(
      toasts(page).getByText(
        "This skill is not managed by agentx. Import it to manage"
      )
    ).toBeVisible();
    cli.register(["skill", "import", "scratch"], {
      events: [{ name: "scratch", type: "library_skill" }],
    });
    await toasts(page).getByRole("button", { name: "Import" }).click();
    await expect(
      toasts(page).getByText("Imported scratch skill")
    ).toBeVisible();
    expect(later(cli)).toEqual([["skill", "import", "scratch", "--json"]]);
  });

  test("an adoption a folder whose files differ stops: says so, and Replace asks before --force", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "scratch", "scratch", inventory, []);
    await chip(pane(page, "scratch"), "Claude Code").click();
    cli.register(["skill", "import", "scratch"], {
      events: [
        {
          code: "refused",
          message:
            'scratch cannot be imported: "/home/me/.claude/skills/scratch" contains files that differ from the skill version',
          type: "error",
        },
      ],
      exitCode: 6,
    });
    await toasts(page).getByRole("button", { name: "Import" }).click();
    const toast = toasts(page)
      .getByRole("listitem")
      .filter({ hasText: "scratch skill wasn't imported" });
    await expect(toast).toContainText(
      "/home/me/.claude/skills/scratch contains files that differ from the skill version"
    );
    cli.register(["skill", "import", "scratch", "--force"], {
      events: [{ name: "scratch", type: "library_skill" }],
    });
    await toast.getByRole("button", { name: "Replace" }).click();
    const dialog = page.getByRole("alertdialog", {
      name: "Replace this folder?",
    });
    await expect(dialog).toContainText("/home/me/.claude/skills/scratch");
    await dialog.getByRole("button", { name: "Replace" }).click();
    await expect(
      toasts(page).getByText("Imported scratch skill")
    ).toBeVisible();
    expect(later(cli)).toEqual([
      ["skill", "import", "scratch", "--json"],
      ["skill", "import", "scratch", "--force", "--json"],
    ]);
  });

  test("an adoption agents with different placements stop: says so, with no Replace", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "scratch", "scratch", inventory, []);
    await chip(pane(page, "scratch"), "Claude Code").click();
    cli.register(["skill", "import", "scratch"], {
      events: [
        {
          code: "refused",
          message:
            'scratch cannot be imported: "/home/me/.claude/skills/scratch" is also read by Cursor agent which is set to copy files, while Claude Code uses symlinks',
          type: "error",
        },
      ],
      exitCode: 6,
    });
    await toasts(page).getByRole("button", { name: "Import" }).click();
    const toast = toasts(page)
      .getByRole("listitem")
      .filter({ hasText: "scratch skill wasn't imported" });
    await expect(toast).toContainText(
      "/home/me/.claude/skills/scratch is also read by Cursor agent which is set to copy files, while Claude Code uses symlinks"
    );
    await expect(toast.getByRole("button", { name: "Replace" })).toHaveCount(0);
  });

  test("a displaced chip runs nothing, and its tooltip says to reconnect it", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "release-notes");
    const cursor = chip(pane(page, "release-notes"), "Cursor, displaced");
    await expect(cursor).toBeChecked();
    await expect(cursor).toHaveAttribute("aria-disabled", "true");
    await cursor.hover();
    await expect(page.getByRole("tooltip")).toHaveText(
      "Cursor owns a copy of release-notes, so your library changes don't reach it. Reconnect it from the banner to manage it here"
    );
    await cursor.click({ force: true });
    expect(later(cli)).toEqual([]);
  });
});

/** The names of the chips in view, in order, and how many rows they and the button take. */
const chipsInView = async (article: Locator) => {
  const agents = article.getByRole("region", { name: "Agents" });
  const chips = await agents.getByRole("switch").all();
  const boxes = await Promise.all(
    [
      ...chips,
      agents.getByRole("button", { name: /^Show (?:\d+ more|fewer)$/u }),
    ].map(async (el) => await el.boundingBox())
  );
  const names = await Promise.all(
    chips.map(async (el) => await el.getAttribute("aria-label"))
  );
  // A row's chips share a middle line; the button, which is lower than a chip, sits on it too.
  const middles = boxes.map((box) =>
    Math.round((box?.y ?? 0) + (box?.height ?? 0) / 2)
  );
  return { names, rows: new Set(middles).size };
};

test.describe("many agents", () => {
  test("the chips that are on, then the most used clients, in two rows until the rest is shown", async ({
    cli,
    page,
  }) => {
    await page.setViewportSize({ height: 800, width: 1440 });
    await openSkill(cli, page, "pdf", "pdf", manyAgentsInventory());
    const article = pane(page, "pdf");
    const agents = article.getByRole("region", { name: "Agents" });
    const more = agents.getByRole("button", { name: /^Show \d+ more$/u });
    await expect(more).toHaveAttribute("aria-expanded", "false");

    const collapsed = await chipsInView(article);
    expect(collapsed.rows).toBe(2);
    // On first: Claude Code and Universal. Then by popularity, whatever the inventory's order.
    expect(collapsed.names.slice(0, 5)).toEqual([
      "Claude Code",
      "Universal",
      "Cursor",
      "GitHub Copilot",
      "Devin",
    ]);
    await expect(more).toHaveText(`Show ${26 - collapsed.names.length} more`);

    // A narrower pane fits fewer chips, still in two rows.
    await page.setViewportSize({ height: 800, width: 1150 });
    await expect(agents.getByRole("switch")).not.toHaveCount(
      collapsed.names.length
    );
    const narrow = await chipsInView(article);
    expect(narrow.rows).toBe(2);
    expect(narrow.names).toEqual(collapsed.names.slice(0, narrow.names.length));
    await page.setViewportSize({ height: 800, width: 1440 });
    await expect(agents.getByRole("switch")).toHaveCount(
      collapsed.names.length
    );

    await more.click();
    await expect(agents.getByRole("switch")).toHaveCount(26);
    const expanded = await chipsInView(article);
    // The clients with no rank follow by name.
    expect(expanded.names.slice(-3)).toEqual([
      "Cortex Code",
      "IBM Bob",
      "Zencoder",
    ]);

    // A chip turned on stays where it is, and stays in view once the rest is hidden again.
    await cli.serveEmit([
      { ...manyAgentsInventory(["zencoder"]), scan_counter: 2 },
    ]);
    await expect(chip(article, "Zencoder")).toBeChecked();
    const afterToggle = await chipsInView(article);
    expect(afterToggle.names).toEqual(expanded.names);
    await agents.getByRole("button", { name: "Show fewer" }).click();
    await expect(more).toBeVisible();
    const again = await chipsInView(article);
    expect(again.rows).toBe(2);
    expect(again.names.at(-1)).toBe("Zencoder");
  });

  test("the Remove dialog stays in the window: only its list of agents scrolls", async ({
    cli,
    page,
  }) => {
    await page.setViewportSize({ height: 600, width: 1440 });
    await openSkill(
      cli,
      page,
      "pdf",
      "pdf",
      manyAgentsInventory(manyConfigurations.map((c) => c.id))
    );
    await pane(page, "pdf").getByRole("button", { name: "Remove" }).click();
    const dialog = page.getByRole("alertdialog", {
      name: "Remove pdf from this machine?",
    });
    const list = dialog.getByRole("list", { name: "Agents that see it" });
    await expect(list.getByRole("listitem")).toHaveCount(27);
    const box = await dialog.boundingBox();
    expect(box?.y).toBeGreaterThanOrEqual(0);
    expect((box?.y ?? 0) + (box?.height ?? 0)).toBeLessThanOrEqual(600);
    await expect(
      dialog.getByRole("heading", { name: "Remove pdf from this machine?" })
    ).toBeInViewport();
    await expect(
      dialog.getByRole("button", { name: "Remove" })
    ).toBeInViewport();
    expect(await list.evaluate((el) => el.scrollHeight > el.clientHeight)).toBe(
      true
    );
  });
});

test.describe("banners", () => {
  test("a folder named as a library skill: the banner says why it can't be imported, and Open selects that skill", async ({
    cli,
    page,
  }) => {
    const folder = "/home/me/.cursor/skills/pdf";
    const ownPdf = {
      content_hash: "h-own-pdf",
      description: "",
      logical_id: "skill:h-own-pdf",
      name: "pdf",
      occurrences: [
        {
          configuration: "cursor",
          id: `cursor:${folder}`,
          kind: "directory" as const,
          path: folder,
          resolved_path: folder,
          scope: "user" as const,
        },
      ],
      physical_id: "skill:h-own-pdf",
    };
    cli.register(["skill", "show", "pdf", "--configuration", "cursor"], {
      events: filesOf("pdf"),
    });
    await openSkill(cli, page, "pdf", "skill:h-own-pdf", {
      ...inventory,
      skills: [...inventory.skills, ownPdf],
    });
    const banner = pane(page, "pdf").getByRole("note", {
      name: "Not managed by agentx",
    });
    await expect(banner).toContainText(
      "Your library already has a skill named pdf, this folder's name."
    );
    await banner.getByRole("button", { name: "Open pdf" }).click();
    await expect(page).toHaveURL(/#\/skills\/pdf$/u);
  });

  test("Not in any agent: Add to agents places the skill in every enabled agent", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "draft");
    // With no client that reads the library, a skill placed nowhere is in no agent.
    await cli.serveEmit([
      {
        ...inventory,
        configurations: inventory.configurations.filter(
          (c) => !c.reads_library
        ),
        library: inventory.library.map((e) => ({
          ...e,
          placements: e.placements.filter((p) => p.mode !== "library"),
          universal: [],
        })),
        scan_counter: 2,
      },
    ]);
    const banner = pane(page, "draft").getByRole("note", {
      name: "Not in any agent",
    });
    await expect(banner).toBeVisible();
    cli.register(["skill", "place", "draft"], {
      events: [
        {
          ok: true,
          summary: "placed draft in 3 configurations",
          type: "result",
        },
      ],
    });
    await banner.getByRole("button", { name: "Add to agents" }).click();
    await expect(
      toasts(page).getByText("Added draft skill to 3 agents")
    ).toBeVisible();
    expect(later(cli)).toEqual([["skill", "place", "draft", "--json"]]);
  });

  test("Reconnect: a refused run opens the confirm dialog, whose Replace adds --force", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "release-notes");
    const article = pane(page, "release-notes");
    const banner = article.getByRole("note", {
      name: "Cursor is not managed by agentx",
    });
    await expect(banner).toContainText(
      "It owns a copy of the skill, so your library changes don't reach it."
    );
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

    await banner.getByRole("button", { name: "Reconnect" }).click();
    const dialog = page.getByRole("alertdialog", {
      name: "Replace Cursor's copy?",
    });
    await expect(dialog).toContainText(
      "Cursor's copy of release-notes is different from the one in your library. Replacing it deletes Cursor's changes."
    );
    await expect(dialog).toContainText(path);
    await dialog.getByRole("button", { name: "Cancel" }).click();
    await expect(dialog).toBeHidden();
    expect(later(cli)).toEqual([
      ["skill", "place", "release-notes", "--to", "cursor", "--json"],
    ]);
    // A refused command may still have changed something: the refresh that follows it is
    // answered before the button is ready again.
    const reconnect = banner.getByRole("button", { name: "Reconnect" });
    await expect(reconnect).toBeDisabled();
    await answerRefresh(cli);
    await expect(reconnect).toBeEnabled();

    cli.register(
      ["skill", "place", "release-notes", "--to", "cursor", "--force"],
      {
        events: [
          {
            ok: true,
            summary:
              "placed release-notes in 1 configuration; discarded what /home/me/.cursor/skills/release-notes held",
            type: "result",
          },
        ],
      }
    );
    await banner.getByRole("button", { name: "Reconnect" }).click();
    await dialog.getByRole("button", { name: "Replace" }).click();
    await expect(dialog).toBeHidden();
    await expect(
      toasts(page).getByText("Reconnected release-notes skill to Cursor")
    ).toBeVisible();
    expect(later(cli)).toEqual([
      ["skill", "place", "release-notes", "--to", "cursor", "--json"],
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
});

test.describe("Remove", () => {
  test("a hub skill: the dialog lists the agents, and --remote is a choice", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "pdf-letters");
    const article = pane(page, "pdf-letters");
    const dialog = page.getByRole("alertdialog", {
      name: "Remove pdf-letters from this machine?",
    });

    // The Universal chip, on, is the removal: a client that reads the library loses a skill
    // only with the library.
    await chip(article, "Universal").click();
    await expect(dialog).toContainText(
      "Removing it from universal agents will also remove it from your library. It will be removed from:"
    );
    await expect(
      dialog
        .getByRole("list", { name: "Agents that see it" })
        .getByRole("listitem")
    ).toHaveText(["Claude Code", "Codex", "Gemini CLI"]);
    await expect(dialog).toContainText(
      "Edits you have not published are deleted."
    );
    const remote = dialog.getByRole("checkbox", {
      name: /^Also delete it from your hub/u,
    });
    await dialog.getByRole("button", { name: "Cancel" }).click();
    await expect(dialog).toBeHidden();

    cli.register(["skill", "remove", "pdf-letters", "--remote"]);
    await article.getByRole("button", { name: "Remove" }).click();
    // The Remove button has no reason to give: the dialog says only what goes.
    await expect(
      dialog.getByText("Removes it from:", {
        exact: true,
      })
    ).toBeVisible();
    await remote.click();
    await expect(remote).toBeChecked();
    await dialog.getByRole("button", { name: "Remove" }).click();
    await expect(dialog).toBeHidden();
    const toast = toasts(page)
      .getByRole("listitem")
      .filter({ hasText: "Removed pdf-letters skill" });
    await expect(toast).toBeVisible();
    expect(later(cli)).toEqual([
      ["skill", "remove", "pdf-letters", "--remote", "--json"],
    ]);
  });

  test("the --remote choice lasts one confirmation: a dismissed dialog opens unticked", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "pdf-letters");
    const article = pane(page, "pdf-letters");
    const dialog = page.getByRole("alertdialog", {
      name: "Remove pdf-letters from this machine?",
    });
    const remote = dialog.getByRole("checkbox", {
      name: /^Also delete it from your hub/u,
    });
    cli.register(["skill", "remove", "pdf-letters"]);

    await article.getByRole("button", { name: "Remove" }).click();
    await remote.click();
    await expect(remote).toBeChecked();
    await dialog.getByRole("button", { name: "Cancel" }).click();
    await expect(dialog).toBeHidden();
    await article.getByRole("button", { name: "Remove" }).click();
    await expect(remote).not.toBeChecked();
    // Escape dismisses it the same way.
    await remote.click();
    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();
    await article.getByRole("button", { name: "Remove" }).click();
    await expect(remote).not.toBeChecked();
    await dialog.getByRole("button", { name: "Remove" }).click();
    await expect(
      toasts(page).getByText("Removed pdf-letters skill")
    ).toBeVisible();
    expect(later(cli)).toEqual([["skill", "remove", "pdf-letters", "--json"]]);
  });

  test("a hub skill not yet published: Remove runs skill remove", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "draft");
    cli.register(["skill", "remove", "draft"]);
    await pane(page, "draft").getByRole("button", { name: "Remove" }).click();
    const dialog = page.getByRole("alertdialog", {
      name: "Remove draft from this machine?",
    });
    await dialog.getByRole("button", { name: "Remove" }).click();
    await expect(toasts(page).getByText("Removed draft skill")).toBeVisible();
    expect(later(cli)).toEqual([["skill", "remove", "draft", "--json"]]);
  });

  test("an unmanaged skill: the dialog names the agents that keep a link Remove leaves", async ({
    cli,
    page,
  }) => {
    const cursorLink = "/home/me/.cursor/skills/scratch";
    const machine = {
      ...inventory,
      skills: inventory.skills.map((n) =>
        n.name === "scratch"
          ? {
              ...n,
              occurrences: [
                ...n.occurrences,
                {
                  configuration: "cursor",
                  id: `cursor:${cursorLink}`,
                  kind: "symlink" as const,
                  path: cursorLink,
                  resolved_path: "/home/me/notes/scratch",
                  scope: "user" as const,
                },
              ],
            }
          : n
      ),
    } satisfies InlineEvent;
    await openSkill(cli, page, "scratch", "scratch", machine);
    await pane(page, "scratch").getByRole("button", { name: "Remove" }).click();
    const dialog = page.getByRole("alertdialog", {
      name: "Remove scratch from this machine?",
    });
    await expect(
      dialog
        .getByRole("list", { name: "Agents that see it" })
        .getByRole("listitem")
    ).toHaveText(["Codex", "Gemini CLI"]);
    await expect(dialog).toContainText(
      "Cursor still sees it: Remove leaves links and folders under another name."
    );
    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();
  });

  test("a skill only in an agent's own folder: Remove runs skill remove with its folder's name", async ({
    cli,
    page,
  }) => {
    cli.register(["skill", "show", "notes", "--configuration", "cursor"], {
      events: filesOf("notes"),
    });
    await openSkill(cli, page, "notes", "skill:h-notes");
    cli.register(["skill", "remove", "notes"]);
    await pane(page, "notes").getByRole("button", { name: "Remove" }).click();
    const dialog = page.getByRole("alertdialog", {
      name: "Remove notes from this machine?",
    });
    await expect(
      dialog
        .getByRole("list", { name: "Agents that see it" })
        .getByRole("listitem")
    ).toHaveText(["Cursor"]);
    await dialog.getByRole("button", { name: "Remove" }).click();
    await expect(toasts(page).getByText("Removed notes skill")).toBeVisible();
    expect(later(cli)).toEqual([["skill", "remove", "notes", "--json"]]);
  });

  test("a skill of a source: the dialog lists the agents that see it", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "pdf");
    await pane(page, "pdf").getByRole("button", { name: "Remove" }).click();
    const dialog = page.getByRole("alertdialog", {
      name: "Remove pdf from this machine?",
    });
    await expect(
      dialog
        .getByRole("list", { name: "Agents that see it" })
        .getByRole("listitem")
    ).toHaveText(["Claude Code", "Codex", "Gemini CLI"]);
    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();
  });
});
