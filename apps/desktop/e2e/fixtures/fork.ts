// The machine the Fork tests run against: the Agents fixture's machine, the inventories serve
// reports after each fork, a second plugin that provides a skill of the same name, and the
// events `skill fork` answers with.
import type { LibraryEntry, Plugin, SkillNode } from "#/cli/events.gen.ts";

import type { InlineEvent } from "../harness/cli.ts";
import { inventory, LIBRARY } from "./agents.ts";
import { ACCOUNT, ACME } from "./skills.ts";

export { inventory } from "./agents.ts";

const WORKTREES = "/home/me/.agentx/worktrees";
const universal = ["codex", "gemini-cli"];

const libraryPlacement = (configurationId: string, name: string) => ({
  configuration: configurationId,
  kind: "library" as const,
  mode: "library" as const,
  path: `${LIBRARY}/${name}`,
});

/** One of your own skills as `skill fork` leaves it: its branch's worktree, in Claude Code too. */
const ownEntry = (
  name: string,
  hash: string,
  from: Partial<LibraryEntry> = {}
): LibraryEntry => ({
  content_hash: hash,
  drift: ["not published"],
  fork_id: `f-${name}`,
  kind: "managed",
  name,
  placements: [
    {
      configuration: "claude-code",
      kind: "symlink",
      mode: "symlink",
      path: `/home/me/.claude-code/skills/${name}`,
    },
    ...universal.map((id) => libraryPlacement(id, name)),
  ],
  source: ACCOUNT,
  state: "current",
  universal,
  ...from,
});

const ownNode = (name: string, hash: string, description: string) => {
  const resolved = `${WORKTREES}/${name}/${name}`;
  return {
    content_hash: hash,
    description,
    logical_id: `skill:${hash}`,
    name,
    occurrences: [
      {
        configuration: "claude-code",
        id: `claude-code:/home/me/.claude-code/skills/${name}`,
        kind: "symlink" as const,
        path: `/home/me/.claude-code/skills/${name}`,
        resolved_path: resolved,
        scope: "user" as const,
      },
      ...universal.map((id) => ({
        configuration: id,
        id: `${id}:${LIBRARY}/${name}`,
        kind: "directory" as const,
        path: `${LIBRARY}/${name}`,
        resolved_path: resolved,
        scope: "user" as const,
      })),
    ],
    physical_id: `skill:${hash}`,
  } satisfies SkillNode;
};

/** The upstream a fork of pdf keeps. */
const fromPdf: Partial<LibraryEntry> = {
  upstream: ACME,
  upstream_commit: "3f2a9c1e8d7b6a",
  upstream_subpath: "pdf",
};

/** After `skill fork pdf`: pdf is one of your own skills, in its place. */
export const pdfForkedInPlace = {
  ...inventory,
  library: inventory.library.map((e) =>
    e.name === "pdf"
      ? ownEntry("pdf", "h-pdf", { ...fromPdf, placements: e.placements })
      : e
  ),
  scan_counter: 2,
} satisfies InlineEvent;

/** After `skill fork pdf --name my-pdf`: my-pdf beside pdf, which stays as it was. */
export const pdfForkedBeside = {
  ...inventory,
  library: [...inventory.library, ownEntry("my-pdf", "h-my-pdf", fromPdf)],
  scan_counter: 2,
  skills: [
    ...inventory.skills,
    ownNode("my-pdf", "h-my-pdf", "Read and convert PDF files"),
  ],
} satisfies InlineEvent;

/** After `skill fork commit-helper`: the fork in the library, the plugin's copy where it was. */
export const commitHelperForked = {
  ...inventory,
  library: [...inventory.library, ownEntry("commit-helper", "h-commit-fork")],
  scan_counter: 2,
  skills: [
    ...inventory.skills,
    ownNode(
      "commit-helper",
      "h-commit-fork",
      "Write commit messages the way the team does"
    ),
  ],
} satisfies InlineEvent;

const teamTools: Plugin = {
  configuration: "claude-code",
  hooks: [],
  logical_id: "plugin:team-tools",
  marketplace: "team-marketplace",
  name: "team-tools",
  path: "/home/me/.claude/plugins/team-tools",
  physical_id: "plugin:team-tools",
  version: "0.3.0",
};

/** A second plugin, team-tools, provides a commit-helper of its own. */
export const twoPluginsInventory = {
  ...inventory,
  plugins: [...inventory.plugins, teamTools],
  skills: [
    ...inventory.skills,
    {
      content_hash: "h-team-commit",
      description: "The team's commit messages",
      logical_id: "skill:h-team-commit",
      name: "commit-helper",
      occurrences: [
        {
          configuration: "claude-code",
          id: "claude-code:/home/me/.claude-code/plugins/team-tools/skills/commit-helper",
          kind: "directory",
          path: "/home/me/.claude-code/plugins/team-tools/skills/commit-helper",
          plugin: "team-tools",
          resolved_path:
            "/home/me/.claude/plugins/team-tools/skills/commit-helper",
          scope: "user",
        },
      ],
      physical_id: "skill:h-team-commit",
    },
  ],
} satisfies InlineEvent;

/** What `skill fork commit-helper` says each agent sees, sorted by configuration. */
export const forkNotes: InlineEvent[] = [
  {
    configuration: "claude-code",
    name: "commit-helper",
    note: "the fork owns the bare name and the plugin's copy stays reachable under the plugin's prefix, so the model sees both",
    plugin: "acme-tools",
    type: "fork_note",
  },
  {
    configuration: "codex",
    name: "commit-helper",
    note: "it reads the library, so it sees commit-helper whether or not it has acme-tools",
    plugin: "acme-tools",
    type: "fork_note",
  },
  {
    configuration: "gemini-cli",
    name: "commit-helper",
    note: "it reads the library, so it sees commit-helper whether or not it has acme-tools",
    plugin: "acme-tools",
    type: "fork_note",
  },
];

/** The library_skill event `skill fork` writes for the fork it made. */
export const forked = (name: string): InlineEvent => ({
  name,
  type: "library_skill",
});

/** The refusal of an in-place fork of a skill folder that holds its own `.git`. */
export const refusedInPlace: InlineEvent = {
  code: "refused",
  hint: "move its .git out of the skill, or fork it beside the skill with --name <new>",
  message:
    "scratch is itself a Git repository, at /home/me/.agents/skills/scratch/.git",
  type: "error",
};
