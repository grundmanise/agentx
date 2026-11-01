// The machine the Skills tests run against: an inventory with a skill of every kind the screen
// shows, the configurations that see them, and the history of one of your own skills. Inline
// events with only the fields the tests check, typed by the generated event types.
import type {
  AdoptionEvent,
  Configuration,
  HistoryEvent,
  LibraryEntry,
  LibraryPlacement,
  Occurrence,
  Plugin,
  SkillNode,
} from "#/cli/events.gen.ts";

import { configuration } from "../harness/cli.ts";
import type { InlineEvent } from "../harness/cli.ts";

export const ACME = "https://github.com/acme/skills";
export const ZETA = "https://gitlab.com/zeta/tools.git";
export const ACCOUNT = "git@github.com:me/account.git";
const LIBRARY = "/home/me/.agents/skills";
const WORKTREES = "/home/me/.agentx/worktrees";

/** Two configurations that place skills in their own directories, two that read the library. */
export const configurations: Configuration[] = [
  configuration("claude-code", { name: "Claude Code" }),
  configuration("codex", { name: "Codex", reads_library: true }),
  configuration("cursor", { name: "Cursor" }),
  configuration("gemini-cli", { name: "Gemini CLI", reads_library: true }),
];

const universal = ["codex", "gemini-cli"];

const placement = (
  configurationId: string,
  name: string,
  mode: LibraryPlacement["mode"] = "symlink",
  kind: LibraryPlacement["kind"] = mode
): LibraryPlacement => ({
  configuration: configurationId,
  kind,
  mode,
  path:
    mode === "library"
      ? `${LIBRARY}/${name}`
      : `/home/me/.${configurationId}/skills/${name}`,
});

const libraryPlacements = (name: string) =>
  universal.map((id) => placement(id, name, "library"));

export const library: LibraryEntry[] = [
  {
    content_hash: "h-draft",
    drift: ["not published"],
    fork_id: "d4af7001c0ffee",
    kind: "managed",
    name: "draft",
    placements: libraryPlacements("draft"),
    state: "current",
    universal,
  },
  {
    content_hash: "h-gone",
    drift: ["upstream removed"],
    kind: "managed",
    name: "gone",
    placements: [
      placement("claude-code", "gone"),
      ...libraryPlacements("gone"),
    ],
    source: ZETA,
    state: "current",
    subpath: "gone",
    universal,
    upstream_commit: "77aa66bb55cc",
  },
  {
    base_hash: "h-pdf",
    content_hash: "h-pdf",
    kind: "managed",
    name: "pdf",
    placements: [placement("claude-code", "pdf"), ...libraryPlacements("pdf")],
    source: ACME,
    state: "current",
    subpath: "pdf",
    universal,
    upstream_commit: "3f2a9c1e8d7b6a",
  },
  {
    base_hash: "h-pdf",
    content_hash: "h-letters",
    fork_id: "f0f0f0f0f0f0",
    kind: "managed",
    name: "pdf-letters",
    placements: [
      placement("claude-code", "pdf-letters"),
      ...libraryPlacements("pdf-letters"),
    ],
    source: ACCOUNT,
    state: "current",
    universal,
    upstream: ACME,
    upstream_commit: "3f2a9c1e8d7b6a",
    upstream_subpath: "pdf",
  },
  {
    content_hash: "h-rn",
    fork_id: "a1b2c3d4e5f6",
    kind: "managed",
    name: "release-notes",
    placements: [
      placement("claude-code", "release-notes"),
      placement("cursor", "release-notes", "symlink", "directory"),
      ...libraryPlacements("release-notes"),
    ],
    source: ACCOUNT,
    state: "modified",
    universal,
  },
  {
    base_hash: "h-review-base",
    candidate: {
      content_hash: "h-review-next",
      upstream_commit: "9b8c7d6e5f4a3",
    },
    content_hash: "h-review",
    kind: "managed",
    name: "review",
    placements: [placement("cursor", "review"), ...libraryPlacements("review")],
    source: ACME,
    state: "modified",
    subpath: "review",
    universal,
    upstream_commit: "1a2b3c4d5e6f7",
  },
  {
    content_hash: "h-scratch",
    kind: "unmanaged",
    name: "scratch",
    placements: libraryPlacements("scratch"),
    universal,
  },
];

const occurrence = (
  configurationId: string,
  name: string,
  kind: Occurrence["kind"],
  resolved: string,
  plugin?: string
): Occurrence => {
  const inPlugin = plugin === undefined ? "" : `plugins/${plugin}/`;
  const path = universal.includes(configurationId)
    ? `${LIBRARY}/${name}`
    : `/home/me/.${configurationId}/${inPlugin}skills/${name}`;
  return {
    configuration: configurationId,
    id: `${configurationId}:${path}`,
    kind,
    path,
    resolved_path: resolved,
    scope: "user",
    ...(plugin === undefined ? {} : { plugin }),
  };
};

/** The node of a library skill as every client sees it: its placements and the library itself. */
const libraryNode = (
  name: string,
  hash: string,
  description: string,
  direct: string[],
  resolved = `${LIBRARY}/${name}`
): SkillNode => ({
  content_hash: hash,
  description,
  logical_id: `skill:${hash}`,
  name,
  occurrences: [
    ...direct.map((id) => occurrence(id, name, "symlink", resolved)),
    ...universal.map((id) => occurrence(id, name, "directory", resolved)),
  ],
  physical_id: `skill:${hash}`,
});

export const skills: SkillNode[] = [
  libraryNode(
    "draft",
    "h-draft",
    "A skill you started and never published",
    [],
    `${WORKTREES}/draft/draft`
  ),
  libraryNode("gone", "h-gone", "Its source no longer has it", ["claude-code"]),
  libraryNode("pdf", "h-pdf", "Read and convert PDF files", ["claude-code"]),
  libraryNode(
    "pdf-letters",
    "h-letters",
    "Read PDFs and write letters from them",
    ["claude-code"],
    `${WORKTREES}/pdf-letters/pdf-letters`
  ),
  libraryNode(
    "release-notes",
    "h-rn",
    "Write release notes from merged changes",
    ["claude-code", "cursor"],
    `${WORKTREES}/release-notes/release-notes`
  ),
  libraryNode("review", "h-review", "Review a change before merging", [
    "cursor",
  ]),
  libraryNode("scratch", "h-scratch", "Notes the skills CLI installed", []),
  {
    content_hash: "h-commit",
    description: "Write commit messages the way the team does",
    logical_id: "skill:h-commit",
    name: "commit-helper",
    occurrences: [
      occurrence(
        "claude-code",
        "commit-helper",
        "directory",
        "/home/me/.claude/plugins/acme-tools/skills/commit-helper",
        "acme-tools"
      ),
    ],
    physical_id: "skill:h-commit",
  },
  {
    content_hash: "h-notes",
    description: "Meeting notes, kept in Cursor only",
    logical_id: "skill:h-notes",
    name: "notes",
    occurrences: [
      occurrence(
        "cursor",
        "notes",
        "directory",
        "/home/me/.cursor/skills/notes"
      ),
    ],
    physical_id: "skill:h-notes",
  },
];

export const plugins: Plugin[] = [
  {
    configuration: "claude-code",
    logical_id: "plugin:acme-tools",
    marketplace: "acme-marketplace",
    name: "acme-tools",
    path: "/home/me/.claude/plugins/acme-tools",
    physical_id: "plugin:acme-tools",
    version: "1.4.0",
  },
];

/** The serve child's first inventory on this machine. */
export const inventory = {
  configurations,
  library,
  plugins,
  scan_counter: 1,
  skills,
  type: "inventory",
} satisfies InlineEvent;

/** What `adopt` would take over: the unmanaged library skill the skills CLI installed. */
export const adoption: AdoptionEvent[] = [
  {
    file: "/home/me/.agents/.skill-lock.json",
    name: "scratch",
    schema_version: 2,
    source: "https://github.com/vercel/skills",
    state: "candidate",
    subpath: "scratch",
    tool: "vercel",
    type: "adoption",
  },
];

const historyEvent = (
  commit: string,
  time: string,
  subject: string,
  files: HistoryEvent["files"],
  flags: Partial<Pick<HistoryEvent, "import" | "foreign" | "machine">> = {}
): HistoryEvent => ({
  author: "Me <me@example.com>",
  commit,
  files,
  foreign: false,
  import: false,
  message: subject,
  name: "pdf-letters",
  parents: [],
  schema_version: 2,
  subject,
  time,
  type: "history",
  ...flags,
});

/** The history of pdf-letters, newest first: a commit made with Git, three of agentx's, the import. */
export const history: HistoryEvent[] = [
  historyEvent(
    "9b41c07aaaa",
    "2026-10-02T14:03:00Z",
    "Tighten the examples",
    [{ path: "examples.md", status: "modified" }],
    { foreign: true }
  ),
  historyEvent(
    "4c2e9a1bbbb",
    "2026-10-02T11:20:00Z",
    "pdf-letters: edit SKILL.md and add examples.md (studio-mac)",
    [
      { path: "SKILL.md", status: "modified" },
      { path: "examples.md", status: "added" },
    ],
    { machine: "machine-1" }
  ),
  historyEvent(
    "e07d5b2cccc",
    "2026-10-01T09:12:00Z",
    "pdf-letters: edit SKILL.md (studio-mac)",
    [{ path: "SKILL.md", status: "modified" }],
    { machine: "machine-1" }
  ),
  historyEvent(
    "51f3a8edddd",
    "2026-09-30T12:00:00Z",
    "Fork pdf as pdf-letters",
    [{ path: "SKILL.md", status: "modified" }],
    { machine: "machine-1" }
  ),
  historyEvent(
    "3f2a9c1e8d7",
    "2026-09-14T12:00:00Z",
    "Import pdf from acme/skills",
    [
      { path: "SKILL.md", status: "added" },
      { path: "reference.md", status: "added" },
    ],
    { import: true }
  ),
];
