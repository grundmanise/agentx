// A big machine for the list views and their speed: 40 agents, 300 skills in four sources, most
// of them installed into every enabled agent, hub skills, and many MCP servers.
import { agentIds } from "./agents.ts";
import {
  addAgents,
  agentx,
  claudeServers,
  codexConfig,
  cursorServers,
  hub,
  newHubSkill,
  sourceRepo,
  unmanagedSkills,
} from "./kit.ts";
import type { Machine, Skill } from "./kit.ts";

const topics = [
  "api",
  "bash",
  "calendar",
  "css",
  "csv",
  "docker",
  "git",
  "go",
  "graphql",
  "html",
  "i18n",
  "invoice",
  "java",
  "json",
  "kotlin",
  "kubernetes",
  "log",
  "markdown",
  "metrics",
  "pdf",
  "python",
  "regex",
  "release",
  "rust",
  "sql",
  "swift",
  "terraform",
  "test",
  "typescript",
  "yaml",
];

const tasks: [string, string][] = [
  ["convert", "Convert {} files to and from other formats"],
  ["debug", "Find the cause of a failing {} change"],
  ["document", "Write docs for {} code a newcomer can follow"],
  ["explain", "Explain a piece of {} line by line"],
  ["format", "Format {} to the team's style"],
  ["lint", "Lint {} and fix what the linter reports"],
  ["migrate", "Plan and apply a {} migration step by step"],
  ["optimize", "Make slow {} faster and measure the gain"],
  ["review", "Review a {} change for bugs and style"],
  ["test", "Write tests for {} code, edge cases first"],
];

/** 300 skills: every task for every topic, such as csv-lint. */
const catalog: Skill[] = topics.flatMap((topic) =>
  tasks.map(([task, text]) => ({
    description: text.replace("{}", topic),
    name: `${topic}-${task}`,
  }))
);

/** Names for count MCP servers, such as tool-01. */
const numbered = (count: number, prefix: string): string[] =>
  Array.from(
    { length: count },
    (_, i) => `${prefix}-${String(i + 1).padStart(2, "0")}`
  );

const npxServers = (names: string[]) =>
  Object.fromEntries(
    names.map((name) => [
      name,
      { args: ["-y", `@acme-labs/${name}-mcp`], command: "npx" },
    ])
  );

export const heavy = (m: Machine): void => {
  addAgents(m, agentIds);
  const big = sourceRepo(m, "acme-labs/skills-catalog", catalog.slice(0, 120));
  const dev = sourceRepo(
    m,
    "paperplane-dev/dev-skills",
    catalog.slice(120, 210)
  );
  const data = sourceRepo(
    m,
    "orbit-tools/data-skills",
    catalog.slice(210, 270)
  );
  const ops = sourceRepo(m, "lattice-ops/ops-skills", catalog.slice(270));
  unmanagedSkills(
    m,
    "claude-code",
    topics.map((topic) => ({
      description: `Personal notes on ${topic}`,
      name: `${topic}-notes`,
    }))
  );
  claudeServers(m, npxServers(numbered(25, "tool")));
  cursorServers(m, npxServers(numbered(15, "editor-tool")));
  codexConfig(
    m,
    Object.fromEntries(
      numbered(10, "py-tool").map((name) => [
        name,
        `command = "uvx"\nargs = ["${name}"]`,
      ])
    )
  );

  agentx(m, "config", "disable", ...agentIds.slice(30));
  agentx(m, "skill", "add", big.url, "--all");
  agentx(
    m,
    "skill",
    "add",
    dev.url,
    "--all",
    ...["claude-code", "cursor", "codex", "junie", "roo"].flatMap((id) => [
      "--to",
      id,
    ])
  );
  agentx(m, "skill", "add", data.url, "--all");
  agentx(
    m,
    "skill",
    "add",
    ops.url,
    ...catalog.slice(270, 290).flatMap((skill) => ["--name", skill.name])
  );
  m.made.push("installed 290 skills from them; the last 10 agents disabled");

  hub(m, "example-dev/agentx-hub");
  const own = topics.slice(0, 12).map((topic) => ({
    description: `The team's own ${topic} playbook`,
    name: `team-${topic}-playbook`,
  }));
  for (const skill of own) {
    newHubSkill(m, skill);
  }
  agentx(m, "skill", "publish", ...own.map((skill) => skill.name));
  m.made.push(`${own.length} hub skills, published`);
};
