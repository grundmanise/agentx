// What each agent sees, as tables over the pure functions: the hover card's table (`sightsTable`) and
// the notice under AGENTS (`clashText`), for the places that hold a skill of one name.
import { expect, test } from "@playwright/test";

import type { Configuration } from "#/cli/events.gen.ts";
import { clashText, sightsTable } from "#/screens/skills/sights.ts";
import type { NameSights, Place, PlaceKind } from "#/screens/skills/sights.ts";

import { configuration } from "./harness/cli.ts";

const claude = configuration("claude-code", { name: "Claude Code" });
const cursor = configuration("cursor", { name: "Cursor" });
const codex = configuration("codex", { name: "Codex" });
const copilot = configuration("github-copilot", { name: "GitHub Copilot" });
const gemini = configuration("gemini-cli", { name: "Gemini CLI" });
const opencode = configuration("opencode", { name: "OpenCode" });
const cline = configuration("cline", { name: "Cline" });

const place = (
  kind: PlaceKind,
  agents: Configuration[],
  label = ""
): Place => ({
  agents,
  key: kind === "library" ? "library" : `${kind}:${label}`,
  kind,
  label,
});

const readers = [cursor, codex, copilot, gemini, opencode, cline];
const yours = place("library", [claude, ...readers]);
const devtools = place(
  "plugin",
  [claude, codex, gemini],
  "chrome-devtools-mcp"
);
const codexFolder = place("folder", [codex], "Codex");

const sights = (places: Place[], own: Place[]): NameSights => ({
  name: "a11y-debugging",
  own: own.map((p) => p.key),
  places,
});

/** A table as text: the header, then each row's agents, its cells and `!` when highlighted. */
const asText = (s: NameSights) => {
  const t = sightsTable(s);
  return [
    ["Agent", ...t.columns.map((c) => c.heading)].join(" | "),
    ...t.rows.map((r) =>
      [
        `${r.clash ? "! " : ""}${r.agents.map((a) => a.name).join(", ")}`,
        ...r.sees.map((x) => (x ? "✓" : "–")),
      ].join(" | ")
    ),
  ];
};

const cases: {
  name: string;
  sights: NameSights;
  table: string[];
  /** The footer and the notice, whole when `exact`, else a distinctive part of each. */
  footer?: string;
  notice?: string;
  exact?: true;
}[] = [
  {
    exact: true,
    footer:
      "The highlighted agents see more than one of these skills, and each decides which one to use. To avoid clashes, rename your skill or uninstall the chrome-devtools-mcp plugin.",
    name: "your fork of a plugin's skill under its name",
    notice:
      "Found 2 skills named a11y-debugging. Agents that see more than one may use the wrong one.",
    sights: sights([yours, devtools], [yours]),
    table: [
      "Agent | Your skill | chrome-devtools-mcp",
      "! Claude Code | ✓ | ✓",
      "! Codex | ✓ | ✓",
      "! Gemini CLI | ✓ | ✓",
      "Cursor, GitHub Copilot, OpenCode, Cline | ✓ | –",
    ],
  },
  {
    footer:
      "The highlighted agent sees more than one of these skills and decides which one to use. To avoid clashes, rename your skill or uninstall the acme plugin.",
    name: "the plugin's skill, seen beside yours by one agent",
    notice: "Found 2 skills named a11y-debugging.",
    sights: sights(
      [place("library", [claude, codex]), place("plugin", [claude], "acme")],
      [place("plugin", [claude], "acme")]
    ),
    table: [
      "Agent | Your skill | acme",
      "! Claude Code | ✓ | ✓",
      "Codex | ✓ | –",
    ],
  },
  {
    footer:
      "To avoid clashes, rename your skill, uninstall the chrome-devtools-mcp plugin or remove the copy in Codex's folder.",
    name: "three skills: an agent's own copy too",
    notice: "Found 3 skills named a11y-debugging.",
    sights: sights([yours, devtools, codexFolder], [yours]),
    table: [
      "Agent | Your skill | chrome-devtools-mcp | Codex's folder",
      "! Claude Code | ✓ | ✓ | –",
      "! Codex | ✓ | ✓ | ✓",
      "! Gemini CLI | ✓ | ✓ | –",
      "Cursor, GitHub Copilot, OpenCode, Cline | ✓ | – | –",
    ],
  },
  {
    footer:
      "To avoid clashes, uninstall the alpha plugin or uninstall the beta plugin.",
    name: "two plugins, none of yours",
    notice: "Found 2 skills named a11y-debugging.",
    sights: sights(
      [place("plugin", [claude], "alpha"), place("plugin", [claude], "beta")],
      [place("plugin", [claude], "alpha")]
    ),
    table: ["Agent | alpha | beta", "! Claude Code | ✓ | ✓"],
  },
  {
    name: "no agent sees two",
    sights: sights(
      [place("library", readers), place("plugin", [claude], "acme")],
      [place("library", readers)]
    ),
    table: [
      "Agent | Your skill | acme",
      "Claude Code | – | ✓",
      "Cursor, Codex, GitHub Copilot, Gemini CLI, OpenCode, Cline | ✓ | –",
    ],
  },
];

for (const c of cases) {
  test(`what each agent sees: ${c.name}`, () => {
    expect(asText(c.sights)).toEqual(c.table);
    const { footer } = sightsTable(c.sights);
    const notice = clashText(c.sights);
    if (c.exact === true || c.footer === undefined) {
      expect(footer).toBe(c.footer);
      expect(notice).toBe(c.notice);
    } else {
      expect(footer).toContain(c.footer);
      expect(notice).toContain(c.notice);
    }
  });
}
