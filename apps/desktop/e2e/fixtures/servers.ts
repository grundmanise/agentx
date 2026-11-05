// The machine the MCP servers tests run against: an inventory with a server of every kind the
// screen shows, the configurations that declare them, and the inventory of a `scan --handshake`
// whose tool counts differ from serve's. Inline events with only the fields the tests check, typed
// by the generated event types. Environment variable and header names are listed as the CLI lists
// them; the tests check that none of them shows.
import type {
  Configuration,
  McpServer,
  McpTool,
  ServerOccurrence,
} from "#/cli/events.gen.ts";

import { configuration } from "../harness/cli.ts";
import type { InlineEvent } from "../harness/cli.ts";

export const configurations: Configuration[] = [
  configuration("claude-code", { name: "Claude Code" }),
  configuration("codex", { name: "Codex", reads_library: true }),
  configuration("cursor", { name: "Cursor" }),
  configuration("gemini-cli", { name: "Gemini CLI", reads_library: true }),
];

const configFiles: Record<string, string> = {
  "claude-code": "/home/me/.claude.json",
  codex: "/home/me/.codex/config.toml",
  cursor: "/home/me/.cursor/mcp.json",
  "gemini-cli": "/home/me/.gemini/settings.json",
};

/** A declaration of a server in one configuration, with `overrides` in place of the defaults. */
const declaration = (
  configurationId: string,
  name: string,
  overrides: Partial<ServerOccurrence> = {}
): ServerOccurrence => ({
  args: [],
  command: "",
  config_file: configFiles[configurationId] ?? "",
  configuration: configurationId,
  env_keys: [],
  handshake: false,
  header_keys: [],
  id: `occurrence:${configurationId}:${name}`,
  transport: "stdio",
  url: "",
  ...overrides,
});

const tools = (prefix: string, n: number, extra: McpTool[] = []): McpTool[] => [
  ...Array.from({ length: n }, (_, i) => ({
    description: `${prefix} tool ${i + 1}`,
    id: `tool:${prefix}-${i + 1}`,
    kind: "tool" as const,
    name: `${prefix}_${i + 1}`,
  })),
  ...extra,
];

export const GITHUB_URL = "https://api.githubcopilot.com/mcp/";
export const LINEAR_URL = "https://mcp.linear.app/sse";
export const SLACK_URL = "https://mcp.slack.com/mcp";
export const FILESYSTEM_COMMAND =
  "npx -y @modelcontextprotocol/server-filesystem /home/me";
export const DB_COMMAND = "node /home/me/.claude/plugins/acme-tools/server.js";
export const NOTES_COMMAND = "uvx notes-mcp";

/** The header and environment variable names the fixture declares; the screen never shows them. */
export const SECRET_NAMES = ["Authorization", "GITHUB_TOKEN", "LINEAR_API_KEY"];

/** When the latest signature was taken, on the filesystem server's newer version. */
export const LATEST_SIGNATURE_AT = "2026-10-21T10:30:00Z";

/** A remote server declared by two agents, with a header. */
const github: McpServer = {
  logical_id: "server:github",
  name: "github",
  occurrences: [
    declaration("claude-code", "github", {
      header_keys: ["Authorization"],
      transport: "streamable-http",
      url: GITHUB_URL,
    }),
    declaration("cursor", "github", {
      env_keys: ["GITHUB_TOKEN"],
      header_keys: ["Authorization"],
      transport: "streamable-http",
      url: GITHUB_URL,
    }),
  ],
  physical_id: "server:github:v1",
  signature: "sig-github",
  signature_at: "2026-10-20T09:00:00Z",
  tools: tools("github", 12, [
    {
      description: "Summarise a pull request",
      id: "tool:github-summary",
      kind: "prompt",
      name: "summarise_pr",
    },
  ]),
};

/** A package server in two versions: Codex runs the latest release, Claude Code a pinned one. */
const filesystemVersions: McpServer[] = [
  {
    logical_id: "server:pkg-filesystem",
    name: "filesystem",
    occurrences: [
      declaration("codex", "filesystem", {
        args: ["-y", "@modelcontextprotocol/server-filesystem", "/home/me"],
        command: "npx",
      }),
    ],
    physical_id: "server:pkg-filesystem:v2",
    signature: "sig-fs-2",
    signature_at: LATEST_SIGNATURE_AT,
    tools: tools("fs", 4),
  },
  {
    logical_id: "server:pkg-filesystem",
    name: "filesystem",
    occurrences: [
      declaration("claude-code", "filesystem", {
        args: [
          "-y",
          "@modelcontextprotocol/server-filesystem@0.6.2",
          "/home/me",
        ],
        command: "npx",
      }),
    ],
    physical_id: "server:pkg-filesystem:v1",
    signature: "sig-fs-1",
    signature_at: "2026-10-18T08:00:00Z",
    tools: tools("fs", 3),
  },
];

/** A plugin's server. */
const acmeDb: McpServer = {
  logical_id: "server:acme-db",
  name: "acme-db",
  occurrences: [
    declaration("claude-code", "acme-db", {
      args: ["/home/me/.claude/plugins/acme-tools/server.js"],
      command: "node",
      config_file: "/home/me/.claude/plugins/acme-tools/.mcp.json",
      plugin: "acme-tools",
    }),
  ],
  physical_id: "server:acme-db:v1",
  signature: "sig-db",
  signature_at: "2026-10-19T12:00:00Z",
  tools: tools("db", 2),
};

/** A remote server on SSE, declared by two agents, one of which turned it off. */
const linear: McpServer = {
  logical_id: "server:linear",
  name: "linear",
  occurrences: [
    declaration("codex", "linear", {
      enabled: false,
      env_keys: ["LINEAR_API_KEY"],
      transport: "sse",
      url: LINEAR_URL,
    }),
    declaration("gemini-cli", "linear", {
      transport: "sse",
      url: LINEAR_URL,
    }),
  ],
  physical_id: "server:linear:v1",
  signature: "sig-linear",
  signature_at: "2026-10-17T15:00:00Z",
  tools: tools("linear", 5),
};

/** A local server never checked. */
const localNotes: McpServer = {
  logical_id: "server:local-notes",
  name: "local-notes",
  occurrences: [
    declaration("cursor", "local-notes", {
      args: ["notes-mcp"],
      command: "uvx",
    }),
  ],
  physical_id: "server:local-notes:none",
  signature: "none",
};

/** A remote server that every client that declares it turned off. */
const slack: McpServer = {
  logical_id: "server:slack",
  name: "slack",
  occurrences: [
    declaration("codex", "slack", {
      enabled: false,
      transport: "streamable-http",
      url: SLACK_URL,
    }),
    declaration("gemini-cli", "slack", {
      enabled: false,
      transport: "streamable-http",
      url: SLACK_URL,
    }),
  ],
  physical_id: "server:slack:v1",
  signature: "sig-slack",
  signature_at: "2026-10-16T09:00:00Z",
  tools: tools("slack", 3),
};

export const servers: McpServer[] = [
  acmeDb,
  ...filesystemVersions,
  github,
  linear,
  localNotes,
  slack,
];

/** The serve child's first inventory on this machine. */
export const inventory = {
  configurations,
  mcp_servers: servers,
  scan_counter: 1,
  type: "inventory",
} satisfies InlineEvent;

/** The same machine with no server declared. */
export const emptyInventory = {
  configurations,
  mcp_servers: [],
  scan_counter: 1,
  type: "inventory",
} satisfies InlineEvent;

export const GITHUB_HINT =
  "Sign in to github in Claude Code: agentx sends only the declared headers";
export const LINEAR_HINT = "Check that https://mcp.linear.app/sse answers";

/**
 * The inventory `scan --handshake` emits: github answered 401 to both agents that declare it,
 * linear is unreachable from Gemini CLI, and every other server answered as before, github with
 * more tools than serve knows.
 */
export const handshakeInventory = {
  configurations,
  mcp_servers: servers.map((node): McpServer => {
    if (node.logical_id === "server:github") {
      return {
        ...node,
        occurrences: node.occurrences.map((o) => ({
          ...o,
          handshake_error: {
            hint: GITHUB_HINT,
            message: "the server answered HTTP 401",
            reason: "unauthorized",
          },
        })),
        physical_id: "server:github:v2",
        signature: "sig-github-2",
        signature_at: "2026-10-22T09:00:00Z",
        tools: tools("github", 20),
      };
    }
    if (node.logical_id === "server:linear") {
      return {
        ...node,
        occurrences: node.occurrences.map((o) =>
          o.configuration === "gemini-cli"
            ? {
                ...o,
                handshake_error: {
                  hint: LINEAR_HINT,
                  message: "connection refused",
                  reason: "unreachable",
                },
              }
            : o
        ),
      };
    }
    return node;
  }),
  scan_counter: 1,
  type: "inventory",
} satisfies InlineEvent;
