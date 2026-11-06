// The machine the Plugins tests run against: an inventory with a plugin of every kind the screen
// shows, the skills and servers they provide, and the edges that tie them together. Inline events
// with only the fields the tests check, typed by the generated event types.
import type {
  Configuration,
  Edge,
  McpServer,
  Plugin,
  ServerOccurrence,
  SkillNode,
} from "#/cli/events.gen.ts";

import { configuration } from "../harness/cli.ts";
import type { InlineEvent } from "../harness/cli.ts";

export const configurations: Configuration[] = [
  configuration("claude-code", { name: "Claude Code" }),
  configuration("codex", { name: "Codex", reads_library: true }),
  configuration("cursor", { name: "Cursor" }),
];

const ACME_DIR = "/home/me/.claude/plugins/acme-tools";
const REVIEW_DIR = "/home/me/.codex/plugins/openai-plugins/codex-review";

/** The icon of acme-tools: an SVG the CLI carried inline, as a data URL. */
const ACME_ICON_SVG =
  '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 40"><rect width="40" height="40" rx="10" fill="#b69cff"/><path d="M12 26 20 12l8 14" fill="none" stroke="#0b0b0c" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"/></svg>';
export const ACME_ICON = `data:image/svg+xml;base64,${Buffer.from(ACME_ICON_SVG).toString("base64")}`;

const pluginSkill = (
  configurationId: string,
  plugin: string,
  dir: string,
  name: string,
  hash: string,
  description: string
): SkillNode => ({
  content_hash: hash,
  description,
  logical_id: `skill:${hash}`,
  name,
  occurrences: [
    {
      configuration: configurationId,
      id: `${configurationId}:${dir}/skills/${name}`,
      kind: "directory",
      path: `${dir}/skills/${name}`,
      plugin,
      resolved_path: `${dir}/skills/${name}`,
      scope: "user",
    },
  ],
  physical_id: `skill:${hash}`,
});

/** The two skills of acme-tools and the one of codex-review. */
export const skills: SkillNode[] = [
  pluginSkill(
    "claude-code",
    "acme-tools",
    ACME_DIR,
    "commit-helper",
    "h-commit",
    "Write commit messages the way the team does"
  ),
  pluginSkill(
    "claude-code",
    "acme-tools",
    ACME_DIR,
    "release-check",
    "h-release",
    "Check a release before it ships"
  ),
  pluginSkill(
    "codex",
    "codex-review",
    REVIEW_DIR,
    "review-pr",
    "h-review-pr",
    "Review a pull request"
  ),
];

/** A declaration of a server in one configuration, with `overrides` in place of the defaults. */
const declaration = (
  configurationId: string,
  name: string,
  overrides: Partial<ServerOccurrence> = {}
): ServerOccurrence => ({
  args: [],
  command: "",
  config_file: `/home/me/.${configurationId}/mcp.json`,
  configuration: configurationId,
  env_keys: [],
  handshake: false,
  header_keys: [],
  id: `occurrence:${configurationId}:${name}`,
  transport: "stdio",
  url: "",
  ...overrides,
});

/** The server acme-tools provides. Its name sorts last, so the MCP servers row is below the fold. */
export const TEAM_DB = "team-db";

const teamDb: McpServer = {
  logical_id: `server:${TEAM_DB}`,
  name: TEAM_DB,
  occurrences: [
    declaration("claude-code", TEAM_DB, {
      args: [`${ACME_DIR}/server.js`],
      command: "node",
      config_file: `${ACME_DIR}/.mcp.json`,
      plugin: "acme-tools",
    }),
  ],
  physical_id: `server:${TEAM_DB}:v1`,
  signature: "none",
};

/** Servers of the configurations' own, enough to push the plugin's server out of the first view. */
const fillers: McpServer[] = Array.from({ length: 9 }, (_, i) => {
  const name = `alpha-${i + 1}`;
  return {
    logical_id: `server:${name}`,
    name,
    occurrences: [
      declaration("codex", name, {
        transport: "streamable-http",
        url: `https://mcp.example.com/${name}`,
      }),
    ],
    physical_id: `server:${name}:v1`,
    signature: "none",
  };
});

export const servers: McpServer[] = [...fillers, teamDb];

/** The three plugins: Claude Code's with everything, Codex's turned off, Cursor's local and bare. */
export const plugins: Plugin[] = [
  {
    configuration: "claude-code",
    hooks: [
      { event: "PostToolUse", name: "format-on-save" },
      { event: "SessionStart", name: "load-context.sh" },
    ],
    icon: ACME_ICON,
    logical_id: "plugin:acme-tools",
    marketplace: "acme-marketplace",
    name: "acme-tools",
    path: ACME_DIR,
    physical_id: "plugin:acme-tools:claude-code:1.4.0",
    version: "1.4.0",
  },
  {
    configuration: "codex",
    enabled: false,
    hooks: [],
    logical_id: "plugin:codex-review",
    marketplace: "openai-plugins",
    name: "codex-review",
    path: REVIEW_DIR,
    physical_id: "plugin:codex-review:codex:0.3.1",
    version: "0.3.1",
  },
  {
    configuration: "cursor",
    hooks: [],
    logical_id: "plugin:notes-local",
    marketplace: "",
    name: "notes-local",
    path: "/home/me/.cursor/plugins/local/notes-local",
    physical_id: "plugin:notes-local:cursor",
    version: "",
  },
];

const pluginId = (name: string) =>
  plugins.find((p) => p.name === name)?.physical_id ?? "";

/** Each configuration to what it holds, and each plugin to what it provides. */
export const edges: Edge[] = [
  { from: "claude-code", to: pluginId("acme-tools") },
  { from: "claude-code", to: "skill:h-commit" },
  { from: "claude-code", to: "skill:h-release" },
  { from: "claude-code", to: teamDb.physical_id },
  { from: "codex", to: pluginId("codex-review") },
  { from: "codex", to: "skill:h-review-pr" },
  ...fillers.map((s) => ({ from: "codex", to: s.physical_id })),
  { from: "cursor", to: pluginId("notes-local") },
  { from: pluginId("acme-tools"), to: "skill:h-commit" },
  { from: pluginId("acme-tools"), to: "skill:h-release" },
  { from: pluginId("acme-tools"), to: teamDb.physical_id },
  { from: pluginId("codex-review"), to: "skill:h-review-pr" },
];

/** The serve child's first inventory on this machine. */
export const inventory = {
  configurations,
  edges,
  mcp_servers: servers,
  plugins,
  scan_counter: 1,
  skills,
  type: "inventory",
} satisfies InlineEvent;

/** The same machine with no plugin installed. */
export const emptyInventory = {
  configurations,
  plugins: [],
  scan_counter: 1,
  type: "inventory",
} satisfies InlineEvent;
