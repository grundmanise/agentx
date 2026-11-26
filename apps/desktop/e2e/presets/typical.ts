// The default machine for screenshots: the ten first-class agents, two sources, a hub, an
// update waiting upstream, skills no tool manages, one the skills CLI installed, MCP servers and
// a Claude Code plugin.
import { firstClass } from "./agents.ts";
import {
  addAgents,
  agentx,
  claudePlugins,
  claudeServers,
  codexConfig,
  commit,
  cursorServers,
  editSkill,
  hub,
  laterDate,
  newHubSkill,
  skillTree,
  skillsCliInstall,
  sourceRepo,
  unmanagedSkills,
  variable,
} from "./kit.ts";
import type { Machine, Skill } from "./kit.ts";

/** A SKILL.md body: a title, when to use the skill, and its steps. */
const body = (title: string, when: string, steps: string[]): string =>
  [
    `# ${title}`,
    "",
    `Use this skill ${when}.`,
    "",
    "## Steps",
    "",
    ...steps.map((step, i) => `${i + 1}. ${step}`),
    "",
  ].join("\n");

const pdfForms: Skill = {
  body: body("PDF forms", "when a task asks to read or fill a PDF form", [
    "List the form fields with their names and types.",
    "Map each value the user gave to a field.",
    "Fill the fields and flatten the form when asked.",
  ]),
  description: "Read, fill and flatten PDF forms",
  files: {
    "reference.md": "# Field types\n\nText, checkbox, radio, choice.\n",
  },
  name: "pdf-forms",
};

const changelogWriter: Skill = {
  body: body("Changelog writer", "before a release is tagged", [
    "Collect the merged pull requests since the last tag.",
    "Group them as Added, Changed and Fixed.",
    "Write one line per change, in the imperative.",
  ]),
  description: "Write a changelog entry from merged pull requests",
  name: "changelog-writer",
};

const agentSkills: Skill[] = [
  pdfForms,
  {
    description: "Tidy messy spreadsheets: headers, types and duplicates",
    name: "spreadsheet-cleanup",
  },
  {
    description: "Turn notes into a slide outline with one idea per slide",
    name: "slide-outline",
  },
  {
    body: body("Meeting notes", "after a meeting with a transcript", [
      "Summarize the decisions.",
      "List the action items with their owners.",
      "Note the open questions.",
    ]),
    description:
      "Summarize a meeting transcript into decisions and action items",
    name: "meeting-notes",
  },
  changelogWriter,
  {
    description: "Write reference docs for an HTTP API from its routes",
    name: "api-docs",
  },
  {
    description: "Draft a test plan for a feature, with edge cases",
    name: "test-plan",
  },
  {
    description: "Review SQL queries for correctness and slow joins",
    name: "sql-review",
  },
];

const devSkills: Skill[] = [
  {
    description: "Check a pull request against a review checklist",
    name: "code-review-checklist",
  },
  {
    description: "Write a conventional commit message for the staged changes",
    name: "commit-message",
  },
  {
    description: "List outdated and vulnerable dependencies with upgrade notes",
    name: "dependency-audit",
  },
  {
    description: "Write and debug docker compose files for local services",
    name: "docker-compose-helper",
  },
];

const readmePolish: Skill = {
  description: "Tighten a README: install steps, usage and badges",
  name: "readme-polish",
};

const releaseNotes: Skill = {
  body: body("Release notes", "when a release is ready to announce", [
    "Read the changelog since the last release.",
    "Pick the three changes users will notice most.",
    "Write a short announcement with a link to the full changelog.",
  ]),
  description: "Draft release notes from merged pull requests",
  name: "release-notes",
};

const standupSummary: Skill = {
  body: body("Standup summary", "before the daily standup", [
    "Read yesterday's commits and pull requests.",
    "Write what was done, what is next and what blocks.",
  ]),
  description: "Summarize yesterday's work for the daily standup",
  name: "standup-summary",
};

const npx = (pkg: string) => ({
  args: ["-y", pkg],
  command: "npx",
  type: "stdio",
});

export const typical = (m: Machine): void => {
  addAgents(m, firstClass);
  const agents = sourceRepo(m, "acme-labs/agent-skills", agentSkills);
  const dev = sourceRepo(m, "paperplane-dev/skills", devSkills);
  const orbit = sourceRepo(m, "orbit-tools/skills", [readmePolish]);

  skillsCliInstall(m, orbit, [readmePolish], ["claude-code", "cursor"]);
  unmanagedSkills(m, "claude-code", [
    {
      description: "Find the commit that broke a test with git bisect",
      name: "git-bisect-helper",
    },
    {
      description: "Notes on the team's coding conventions",
      name: "team-conventions",
    },
  ]);
  unmanagedSkills(m, "cursor", [
    {
      description: "Map design tokens to Tailwind classes",
      name: "tailwind-tokens",
    },
  ]);

  claudeServers(m, {
    "docs-search": npx("@acme-labs/docs-search-mcp@1.4.0"),
    "issue-tracker": {
      headers: { Authorization: `Bearer ${variable("ISSUE_TRACKER_TOKEN")}` },
      type: "http",
      url: "https://mcp.example.com/issues",
    },
    postgres: {
      args: [
        "run",
        "-i",
        "--rm",
        "-e",
        "DATABASE_URL",
        "ghcr.io/acme-labs/postgres-mcp:2.1",
      ],
      command: "docker",
      env: { DATABASE_URL: "postgres://localhost:5432/app" },
      type: "stdio",
    },
  });
  codexConfig(m, {
    "docs-search":
      'command = "npx"\nargs = ["-y", "@acme-labs/docs-search-mcp@1.4.0"]',
    "web-fetch": 'command = "uvx"\nargs = ["web-fetch-mcp==0.6"]',
  });
  cursorServers(m, {
    "design-tokens": { url: "https://mcp.example.com/design-tokens" },
    "docs-search": {
      args: ["-y", "@acme-labs/docs-search-mcp"],
      command: "npx",
    },
  });
  claudePlugins(m, [
    {
      description: "Cut releases: version bumps, checklists and a release API",
      enabled: true,
      marketplace: "acme-labs",
      name: "release-kit",
      servers: {
        "release-api": {
          args: [`${variable("CLAUDE_PLUGIN_ROOT")}/server/index.js`],
          command: "node",
        },
      },
      skills: [
        {
          description: "Run the pre-release checklist",
          name: "ship-checklist",
        },
        {
          description: "Bump the version in every manifest",
          name: "version-bump",
        },
      ],
      version: "1.3.0",
    },
  ]);

  agentx(m, "config", "disable", "cline");
  agentx(
    m,
    "skill",
    "add",
    agents.url,
    "--name",
    "pdf-forms",
    "--name",
    "spreadsheet-cleanup",
    "--name",
    "meeting-notes",
    "--name",
    "changelog-writer"
  );
  agentx(
    m,
    "skill",
    "add",
    agents.url,
    "--name",
    "slide-outline",
    "--name",
    "api-docs",
    "--to",
    "claude-code",
    "--to",
    "cursor"
  );
  agentx(m, "skill", "add", agents.url, "--name", "test-plan", "--to", "codex");
  agentx(
    m,
    "skill",
    "add",
    dev.url,
    "--all",
    "--except",
    "docker-compose-helper"
  );
  m.made.push("installed 10 skills from the two sources");

  hub(m, "example-dev/agentx-hub");
  newHubSkill(m, releaseNotes);
  newHubSkill(m, standupSummary);
  agentx(
    m,
    "skill",
    "fork",
    "changelog-writer",
    "--name",
    "changelog-house-style"
  );
  agentx(
    m,
    "skill",
    "publish",
    "release-notes",
    "standup-summary",
    "changelog-house-style"
  );
  editSkill(m, {
    ...standupSummary,
    body: `${standupSummary.body}\n## Format\n\nKeep it under five lines.\n`,
  });
  m.made.push(
    "hub skills: release-notes, standup-summary (unpublished edits), changelog-house-style (fork)"
  );

  commit(
    agents,
    skillTree([
      {
        ...pdfForms,
        body: `${pdfForms.body}\n## Signatures\n\nLeave signature fields empty and say so.\n`,
      },
      {
        ...changelogWriter,
        description:
          "Write a changelog entry from merged pull requests and issues",
      },
    ]),
    "Cover signatures in pdf-forms and issues in changelog-writer",
    laterDate
  );
  m.made.push("update upstream: pdf-forms, changelog-writer");
};
