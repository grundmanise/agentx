// The machine the placement and Agents tests run against: the Skills fixture's library and skills
// on four configurations that place skills in their own directories and two that read the
// library, with MCP servers and the inventory's edges, which the Agents page counts from, and an
// account remote among the sources.
import type {
  Configuration,
  Edge,
  InventoryEvent,
  McpServer,
  ServerOccurrence,
} from "#/cli/events.gen.ts";

import { configuration } from "../harness/cli.ts";
import type { InlineEvent } from "../harness/cli.ts";
import {
  ACCOUNT,
  configurations as four,
  inventory as skillsInventory,
} from "./skills.ts";

/** Sorted by id, as an inventory lists them: four direct, two reading the library. */
export const configurations: Configuration[] = [
  ...four,
  configuration("github-copilot", { name: "GitHub Copilot" }),
  configuration("windsurf", { enabled: false, name: "Windsurf" }),
];

const serverOccurrence = (
  configurationId: string,
  name: string
): ServerOccurrence => ({
  args: [],
  command: "npx",
  config_file: `/home/me/.${configurationId}/mcp.json`,
  configuration: configurationId,
  env_keys: [],
  handshake: false,
  header_keys: [],
  id: `${configurationId}:${name}`,
  transport: "stdio",
  url: "",
});

const server = (name: string, inConfigurations: string[]): McpServer => ({
  logical_id: `server:${name}`,
  name,
  occurrences: inConfigurations.map((id) => serverOccurrence(id, name)),
  physical_id: `server:${name}`,
  signature: "none",
});

/** One server two direct clients share, one declared by a client that reads the library. */
export const servers: McpServer[] = [
  server("filesystem", ["claude-code", "cursor"]),
  server("github", ["codex"]),
];

type Nodes = Pick<
  InventoryEvent,
  "configurations" | "skills" | "mcp_servers" | "plugins"
>;

/**
 * The edges an inventory carries for these nodes: the machine to each configuration, each
 * configuration to the skills it sees, the servers it declares and the plugins it has, and each
 * plugin to what it provides; sorted by `from` then `to`, each pair once.
 */
export const edgesOf = (nodes: Nodes, machineId = "machine-1"): Edge[] => {
  const pairs = new Set<string>();
  const add = (from: string, to: string) => {
    pairs.add(`${from}\u0000${to}`);
  };
  for (const c of nodes.configurations) {
    add(machineId, c.physical_id);
  }
  for (const skill of nodes.skills) {
    for (const o of skill.occurrences) {
      add(o.configuration, skill.physical_id);
      if (o.plugin !== undefined) {
        add(`plugin:${o.plugin}`, skill.physical_id);
      }
    }
  }
  for (const s of nodes.mcp_servers) {
    for (const o of s.occurrences) {
      add(o.configuration, s.physical_id);
      if (o.plugin !== undefined) {
        add(`plugin:${o.plugin}`, s.physical_id);
      }
    }
  }
  for (const p of nodes.plugins) {
    add(p.configuration, p.physical_id);
  }
  const edges = [...pairs].map((pair) => {
    const [from = "", to = ""] = pair.split("\u0000");
    return { from, to };
  });
  edges.sort(
    (a, b) => a.from.localeCompare(b.from) || a.to.localeCompare(b.to)
  );
  return edges;
};

const nodes: Nodes = {
  configurations,
  mcp_servers: servers,
  plugins: skillsInventory.plugins,
  skills: skillsInventory.skills,
};

/** The serve child's first inventory on this machine. */
export const inventory = {
  ...skillsInventory,
  configurations,
  edges: edgesOf(nodes),
  mcp_servers: servers,
} satisfies InlineEvent;

/**
 * What the preview of `config remove` answers for these configurations: GitHub Copilot and
 * Windsurf are leftovers that can be removed, and the others hold something of their own.
 */
export const removals: InlineEvent[] = configurations.map((c) => {
  const holds: Record<string, string[]> = {
    "claude-code": [
      "settings.json",
      "1 plugin in plugins",
      "the skill notes, which nothing that stays on the machine holds",
    ],
    codex: ["MCP server declarations in config.toml"],
    cursor: ["MCP server declarations in mcp.json"],
    "gemini-cli": ["settings.json"],
  };
  const held = holds[c.id] ?? [];
  return {
    holds: held,
    id: c.id,
    name: c.name,
    path: c.path,
    state: held.length > 0 ? "kept" : "removable",
    type: "config_removal",
    with: [],
  };
});

/** The library's path on this machine, as its occurrences show it. */
export const LIBRARY = "/home/me/.agents/skills";

/** What `source list` answers: one shared source and the account remote. */
export const sources: InlineEvent[] = [
  {
    id: "1111111111111111",
    type: "source",
    url: "https://github.com/acme/skills",
  },
  { account: true, id: "2222222222222222", type: "source", url: ACCOUNT },
];
