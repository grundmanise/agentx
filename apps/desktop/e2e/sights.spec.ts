// What each agent sees, as tables over the pure functions: the popover's table (`sightsTable`) and
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
  footer?: string;
  notice?: string;
}[] = [
  {
    footer:
      "Highlighted agents see both. agentx can't tell which one they use. To keep them apart, rename yours.",
    name: "your fork of a plugin's skill under its name",
    notice:
      "Claude Code, Codex and Gemini CLI also see the chrome-devtools-mcp plugin's a11y-debugging. Each picks one of the two.",
    sights: sights([yours, devtools], [yours]),
    table: [
      "Agent | Yours | chrome-devtools-mcp",
      "! Claude Code | ✓ | ✓",
      "! Codex | ✓ | ✓",
      "! Gemini CLI | ✓ | ✓",
      "Cursor, GitHub Copilot, OpenCode, Cline | ✓ | –",
    ],
  },
  {
    footer:
      "The highlighted agent sees both. agentx can't tell which one it uses. To keep them apart, rename yours.",
    name: "the plugin's skill, seen beside yours by one agent",
    notice:
      "Claude Code also sees your a11y-debugging. It picks one of the two.",
    sights: sights(
      [place("library", [claude, codex]), place("plugin", [claude], "acme")],
      [place("plugin", [claude], "acme")]
    ),
    table: ["Agent | Yours | acme", "! Claude Code | ✓ | ✓", "Codex | ✓ | –"],
  },
  {
    footer:
      "Highlighted agents see more than one. agentx can't tell which one they use. To keep them apart, rename yours.",
    name: "three skills: an agent's own copy too",
    notice:
      "Claude Code and Gemini CLI also see the chrome-devtools-mcp plugin's a11y-debugging. Codex also sees the chrome-devtools-mcp plugin's a11y-debugging and the copy in Codex's folder. Each picks one of them.",
    sights: sights([yours, devtools, codexFolder], [yours]),
    table: [
      "Agent | Yours | chrome-devtools-mcp | Codex's folder",
      "! Claude Code | ✓ | ✓ | –",
      "! Codex | ✓ | ✓ | ✓",
      "! Gemini CLI | ✓ | ✓ | –",
      "Cursor, GitHub Copilot, OpenCode, Cline | ✓ | – | –",
    ],
  },
  {
    footer:
      "The highlighted agent sees both. agentx can't tell which one it uses.",
    name: "two plugins, none of yours",
    notice:
      "Claude Code also sees the beta plugin's a11y-debugging. It picks one of the two.",
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
      "Agent | Yours | acme",
      "Claude Code | – | ✓",
      "Cursor, Codex, GitHub Copilot, Gemini CLI, OpenCode, Cline | ✓ | –",
    ],
  },
];

for (const c of cases) {
  test(`what each agent sees: ${c.name}`, () => {
    expect(asText(c.sights)).toEqual(c.table);
    expect(sightsTable(c.sights).footer).toBe(c.footer);
    expect(clashText(c.sights)).toBe(c.notice);
  });
}
