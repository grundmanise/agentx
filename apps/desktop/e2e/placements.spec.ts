// The controls of the Skills detail pane that change where a skill is: the AGENTS chips, the
// banners' buttons and Remove. Each runs exactly one command line, which the mocked CLI records,
// and shows pending until the command ends and serve answers the refresh that follows.
import type { Locator, Page } from "@playwright/test";

import {
  inventory,
  LIBRARY,
  manyAgentsInventory,
  sources,
} from "./fixtures/agents.ts";
import {
  adoption,
  fileReads,
  filesOf,
  history,
  linked,
  releaseNotesDiffs,
  reviewDiffs,
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

/** The reads the detail pane runs on its own when a skill is selected. */
const detailReads = new Set(["skill history", "skill files", "skill diff"]);

/** The command lines the controls ran: everything but the launch, the store's and the pane's reads. */
const later = (cli: MockCli) =>
  cli.commands.filter(
    (l) => !(isLaunchOrLoad(l) || detailReads.has(l.slice(0, 2).join(" ")))
  );

const toasts = (page: Page) =>
  page.getByRole("region", { name: /^Notifications/u });
const pane = (page: Page, name: string) =>
  page
    .getByRole("region", { name: "Skill details" })
    .getByRole("article", { name });
const chip = (article: Locator, name: string) =>
  article.getByRole("switch", { exact: true, name });
const activity = (page: Page) =>
  page.getByRole("list", { name: "Activity" }).getByRole("listitem");

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
  cli.register(["adopt"], { events: records });
  cli.register(["source", "list"], { events: sources });
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
  cli.register(["skill", "diff", "pdf-letters"], { events: [] });
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
    await expect(toasts(page).getByText("Added pdf to Cursor")).toBeVisible();
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
      toasts(page).getByText("Removed pdf from Cursor")
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
      toasts(page).getByText("Removed pdf from Claude Code")
    ).toBeVisible();
    await toasts(page).getByRole("button", { name: "Undo" }).click();
    await expect(
      toasts(page).getByText("Added pdf to Claude Code")
    ).toBeVisible();
    expect(later(cli)).toEqual([
      ["skill", "remove", "pdf", "--from", "claude-code", "--json"],
      ["skill", "place", "pdf", "--to", "claude-code", "--json"],
    ]);
  });

  test("a skipped placement: an attention toast with the warning, logged as attention", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "pdf");
    await page.addScriptTag({ type: "module", url: "/e2e/harness/store.tsx" });
    const warning =
      "windsurf: /home/me/.windsurf/skills/pdf is a directory of yours and was left as it is";
    cli.register(["skill", "place", "pdf", "--to", "windsurf"], {
      events: [
        {
          ok: true,
          summary: "placed pdf in 0 configurations, 1 placement skipped",
          type: "result",
        },
      ],
      logs: [{ level: "warn", message: warning }],
    });

    await chip(pane(page, "pdf"), "Windsurf").click();
    const toast = toasts(page)
      .getByRole("listitem")
      .filter({ hasText: "Added pdf to Windsurf" });
    await expect(toast).toContainText(warning);
    await expect(toast.getByRole("button", { name: "Undo" })).toBeVisible();
    await expect(activity(page)).toHaveText(["Added pdf to Windsurf"]);
    await expect(activity(page)).toHaveAttribute("data-tone", "attention");
    expect(later(cli)).toEqual([
      ["skill", "place", "pdf", "--to", "windsurf", "--json"],
    ]);
  });

  test("locked chips toast the lock reason", async ({ cli, page }) => {
    cli.register(["skill", "files", "linked", "--configuration", "cursor"], {
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
    await expect(toasts(page).getByText(reason)).toBeVisible();
    await chip(plugin, "Universal").click();
    // The toast before, with the same text, may still be on its way out.
    await expect(toasts(page).getByText(reason).last()).toBeVisible();

    const skills = page.getByRole("navigation", { name: "Skills" });
    await skills.getByRole("button", { exact: true, name: "scratch" }).click();
    const candidate = pane(page, "scratch");
    const adopt = "This skill is not managed by agentx. Adopt it to manage";
    await expect(chip(candidate, "Claude Code")).toHaveAttribute(
      "title",
      adopt
    );
    await chip(candidate, "Claude Code").click();
    await expect(toasts(page).getByText(adopt)).toBeVisible();
    // The toast offers what unlocks the chips: Adopt, for a skill another tool installed.
    cli.register(["adopt", "--skill", "scratch"]);
    await toasts(page).getByRole("button", { name: "Adopt" }).click();
    await expect(toasts(page).getByText("Adopted scratch skill")).toBeVisible();

    // A skill an agent holds in its own folder is adopted by its directory's name, and selected
    // as the library skill the CLI reports.
    await skills.getByRole("button", { exact: true, name: "notes" }).click();
    await chip(pane(page, "notes"), "Claude Code").click();
    await expect(toasts(page).getByText(adopt)).toBeVisible();
    cli.register(["adopt", "--skill", "notes"], {
      events: [{ name: "notes", type: "library_skill" }],
    });
    await toasts(page).getByRole("button", { name: "Adopt" }).click();
    await expect(toasts(page).getByText("Adopted notes skill")).toBeVisible();
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
        "This skill is not managed by agentx. Adopt it to manage"
      )
    ).toBeVisible();
    cli.register(["adopt", "--skill", "scratch"], {
      events: [{ name: "scratch", type: "library_skill" }],
    });
    await toasts(page).getByRole("button", { name: "Adopt" }).click();
    await expect(toasts(page).getByText("Adopted scratch skill")).toBeVisible();
    expect(later(cli)).toEqual([["adopt", "--skill", "scratch", "--json"]]);
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

  test("an agent that sees the skill through another's folder: its chip runs nothing, and the other's removes it from both", async ({
    cli,
    page,
  }) => {
    // Cursor reads Claude Code's skills directory, so it sees pdf through Claude Code's placement.
    const path = "/home/me/.claude-code/skills/pdf";
    const throughClaude = {
      ...inventory,
      skills: inventory.skills.map((node) =>
        node.name === "pdf"
          ? {
              ...node,
              occurrences: [
                ...node.occurrences,
                {
                  configuration: "cursor",
                  id: `cursor:${path}`,
                  kind: "symlink" as const,
                  path,
                  resolved_path: `${LIBRARY}/pdf`,
                  scope: "user" as const,
                },
              ],
            }
          : node
      ),
    } satisfies InlineEvent;
    await openSkill(cli, page, "pdf", "pdf", throughClaude);
    const article = pane(page, "pdf");
    const cursor = chip(article, "Cursor");
    await expect(cursor).toBeChecked();
    await expect(cursor).toHaveAttribute("aria-disabled", "true");
    await cursor.hover();
    await expect(page.getByRole("tooltip")).toHaveText(
      "Cursor reads Claude Code's skills folder, so it sees pdf there. Remove it from Claude Code to remove it from both"
    );
    await cursor.click({ force: true });
    expect(later(cli)).toEqual([]);

    const claude = chip(article, "Claude Code");
    await expect(claude).toHaveAttribute(
      "title",
      "Remove from Claude Code and Cursor"
    );
    cli.register(["skill", "remove", "pdf", "--from", "claude-code"]);
    await claude.click();
    await expect(
      toasts(page).getByText("Removed pdf from Claude Code and Cursor")
    ).toBeVisible();
    expect(later(cli)).toEqual([
      ["skill", "remove", "pdf", "--from", "claude-code", "--json"],
    ]);
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
      "Windsurf",
    ]);
    await expect(more).toHaveText(`Show ${27 - collapsed.names.length} more`);

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
    await expect(agents.getByRole("switch")).toHaveCount(27);
    const expanded = await chipsInView(article);
    // The clients with no rank follow by name.
    expect(expanded.names.slice(-3)).toEqual([
      "IBM Bob",
      "Zencoder",
      "Zenflow",
    ]);

    // A chip turned on stays where it is, and stays in view once the rest is hidden again.
    await cli.serveEmit([
      { ...manyAgentsInventory(["zenflow"]), scan_counter: 2 },
    ]);
    await expect(chip(article, "Zenflow")).toBeChecked();
    const afterToggle = await chipsInView(article);
    expect(afterToggle.names).toEqual(expanded.names);
    await agents.getByRole("button", { name: "Show fewer" }).click();
    await expect(more).toBeVisible();
    const again = await chipsInView(article);
    expect(again.rows).toBe(2);
    expect(again.names.at(-1)).toBe("Zenflow");
  });
});

test.describe("banners", () => {
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
      toasts(page).getByText("Added draft to 3 agents")
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
    const dialog = page.getByRole("dialog", {
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
      toasts(page).getByText("Reconnected release-notes to Cursor")
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
  test("one of your own skills: the dialog lists the agents, and --remote is a choice", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "pdf-letters");
    const article = pane(page, "pdf-letters");
    const dialog = page.getByRole("dialog", { name: "Remove pdf-letters?" });

    // The Universal chip, on, is the removal: a client that reads the library loses a skill
    // only with the library.
    await chip(article, "Universal").click();
    await expect(dialog).toContainText(
      "Universal agents read your library, where agentx keeps this skill. Removing it there deletes it from every agent on this machine:"
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
      name: /^Also delete it from your account remote/u,
    });
    await expect(dialog).toContainText(
      "Your other machines keep it until you remove it there."
    );
    await dialog.getByRole("button", { name: "Cancel" }).click();
    await expect(dialog).toBeHidden();

    cli.register(["skill", "remove", "pdf-letters", "--remote"]);
    await article.getByRole("button", { name: "Remove" }).click();
    // The Remove button has no reason to give: the dialog says only what goes.
    await expect(
      dialog.getByText("It will be deleted from every agent on this machine:", {
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
    const dialog = page.getByRole("dialog", { name: "Remove pdf-letters?" });
    const remote = dialog.getByRole("checkbox", {
      name: /^Also delete it from your account remote/u,
    });
    cli.register(["skill", "remove", "pdf-letters"]);

    await article.getByRole("button", { name: "Remove" }).click();
    await remote.click();
    await expect(remote).toBeChecked();
    await dialog.getByRole("button", { name: "Cancel" }).click();
    await expect(dialog).toBeHidden();
    await article.getByRole("button", { name: "Remove" }).click();
    await expect(remote).not.toBeChecked();
    // Escape and a click outside dismiss it the same way.
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

  test("one of your own skills not yet published: Remove runs skill remove", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "draft");
    cli.register(["skill", "remove", "draft"]);
    await pane(page, "draft").getByRole("button", { name: "Remove" }).click();
    const dialog = page.getByRole("dialog", { name: "Remove draft?" });
    await dialog.getByRole("button", { name: "Remove" }).click();
    await expect(toasts(page).getByText("Removed draft skill")).toBeVisible();
    expect(later(cli)).toEqual([["skill", "remove", "draft", "--json"]]);
  });

  test("a skill of a source: the dialog lists the agents that see it", async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, "pdf");
    await pane(page, "pdf").getByRole("button", { name: "Remove" }).click();
    const dialog = page.getByRole("dialog", { name: "Remove pdf?" });
    await expect(
      dialog
        .getByRole("list", { name: "Agents that see it" })
        .getByRole("listitem")
    ).toHaveText(["Claude Code", "Codex", "Gemini CLI"]);
    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();
  });
});
