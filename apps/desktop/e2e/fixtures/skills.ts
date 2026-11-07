// The machine the Skills tests run against: an inventory with a skill of every kind the screen
// shows, the configurations that see them, and the history of one of your own skills. Inline
// events with only the fields the tests check, typed by the generated event types.
import type {
  AdoptionEvent,
  Configuration,
  DiffEvent,
  HistoryEvent,
  LibraryEntry,
  LibraryPlacement,
  Occurrence,
  Plugin,
  SkillFileEvent,
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
    "Turn a design doc into a task list",
    [],
    `${WORKTREES}/draft/draft`
  ),
  libraryNode("gone", "h-gone", "Lint SQL migrations before they run", [
    "claude-code",
  ]),
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
  libraryNode("scratch", "h-scratch", "Summarise a long thread into notes", []),
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
    description: "Take meeting notes and list the action items",
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
    hooks: [],
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

// The files of each skill, as `skill files` reads them, and the edits of the modified skills, as
// `skill diff` reports them. Every skill has a SKILL.md, a nested file, a binary file and a file
// over the size limit; the modified skill of a source also has a file it added.

const skillFile = (
  name: string,
  path: string,
  fields: Partial<Omit<SkillFileEvent, "type" | "name" | "path">>
): SkillFileEvent => ({
  binary: false,
  name,
  path,
  schema_version: 2,
  size: 0,
  type: "skill_file",
  ...fields,
});

/** A text file's event, with the size and line count the CLI would report for `content`. */
const textFile = (name: string, path: string, content: string) =>
  skillFile(name, path, {
    content,
    lines: content
      .split("\n")
      .filter((l, i, all) => i < all.length - 1 || l !== "").length,
    size: new TextEncoder().encode(content).length,
  });

/** The SKILL.md of a fixture skill: front matter, a heading and a line of prose. */
const skillMd = (name: string, prose: string) =>
  `---\nname: ${name}\n---\n\n# ${name}\n${prose}\n`;

/**
 * The files of `name`, in a different order from the CLI's so that the app's own order shows:
 * SKILL.md first, then by path, where `reference.pdf` sorts before `reference/forms.md`.
 */
export const filesOf = (
  name: string,
  prose = `Use this skill to ${name}.`,
  extra: SkillFileEvent[] = []
): SkillFileEvent[] => [
  textFile(
    name,
    "reference/forms.md",
    "# Forms\n\n- Fill in every field.\n- Keep the original.\n"
  ),
  skillFile(name, "reference.pdf", { size: 310_400, truncated: true }),
  textFile(name, "SKILL.md", skillMd(name, prose)),
  skillFile(name, "assets/logo.png", { binary: true, size: 8192 }),
  ...extra,
];

const diffEvent = (
  name: string,
  path: string,
  status: DiffEvent["status"],
  patch: string
): DiffEvent => ({
  name,
  patch,
  path,
  schema_version: 2,
  status,
  type: "diff",
});

/** The prose of review's SKILL.md after its edit; the patch below adds the second line. */
export const reviewProse =
  "Use this skill to review a change before merging.\nAlways run the tests first.";

/** The edits of `review`, a modified skill of a source: one file edited, one added, one deleted. */
export const reviewDiffs: DiffEvent[] = [
  diffEvent(
    "review",
    "SKILL.md",
    "modified",
    [
      "diff --git a/SKILL.md b/SKILL.md",
      "index 5d1e2c4..8f0a3b1 100644",
      "--- a/SKILL.md",
      "+++ b/SKILL.md",
      "@@ -4,3 +4,4 @@ name: review",
      " ",
      " # review",
      "-Use this skill to review a change.",
      "+Use this skill to review a change before merging.",
      "+Always run the tests first.",
      "",
    ].join("\n")
  ),
  diffEvent(
    "review",
    "notes.md",
    "added",
    [
      "diff --git a/notes.md b/notes.md",
      "new file mode 100644",
      "index 0000000..2f1c9e0",
      "--- /dev/null",
      "+++ b/notes.md",
      "@@ -0,0 +1 @@",
      "+My own notes.",
      "",
    ].join("\n")
  ),
  diffEvent(
    "review",
    "old.md",
    "deleted",
    [
      "diff --git a/old.md b/old.md",
      "deleted file mode 100644",
      "index 9ab3f2e..0000000",
      "--- a/old.md",
      "+++ /dev/null",
      "@@ -1,2 +0,0 @@",
      "-An old note.",
      "-Nobody reads it.",
      "",
    ].join("\n")
  ),
];

/** The edits of `release-notes`, one of your own skills: SKILL.md edited since the last publish. */
export const releaseNotesDiffs: DiffEvent[] = [
  diffEvent(
    "release-notes",
    "SKILL.md",
    "modified",
    [
      "diff --git a/SKILL.md b/SKILL.md",
      "index 1111111..2222222 100644",
      "--- a/SKILL.md",
      "+++ b/SKILL.md",
      "@@ -5,2 +5,2 @@",
      " # release-notes",
      "-Use this skill to write release notes.",
      "+Use this skill to write release notes from merged changes.",
      "",
    ].join("\n")
  ),
];

/** One `skill files` read: the arguments after `skill files`, and the events it answers with. */
export interface FilesRead {
  args: string[];
  events: SkillFileEvent[];
}

/** The `skill files` read of every skill of the inventory, as the app names each one. */
export const fileReads: FilesRead[] = [
  { args: ["draft"], events: filesOf("draft") },
  { args: ["gone"], events: filesOf("gone") },
  { args: ["pdf"], events: filesOf("pdf") },
  { args: ["pdf-letters"], events: filesOf("pdf-letters") },
  {
    args: ["release-notes"],
    events: filesOf(
      "release-notes",
      "Use this skill to write release notes from merged changes."
    ),
  },
  {
    args: ["review"],
    events: filesOf("review", reviewProse, [
      textFile("review", "notes.md", "My own notes.\n"),
    ]),
  },
  { args: ["scratch"], events: filesOf("scratch") },
  {
    args: ["acme-tools:commit-helper"],
    events: filesOf("commit-helper"),
  },
  {
    args: ["notes", "--configuration", "cursor"],
    events: filesOf("notes"),
  },
];

/**
 * A deleted Markdown file with front matter: its `---` lines read `----` in the patch, which a
 * count of the lines that start with `-` but not `---` would miss.
 */
export const deletedWithFrontMatter: DiffEvent = diffEvent(
  "review",
  "archive/OLD.md",
  "deleted",
  [
    "diff --git a/archive/OLD.md b/archive/OLD.md",
    "deleted file mode 100644",
    "index 7c1d2e3..0000000",
    "--- a/archive/OLD.md",
    "+++ /dev/null",
    "@@ -1,6 +0,0 @@",
    "----",
    "-name: old",
    "----",
    "-",
    "-# Old",
    "-Gone.",
    "",
  ].join("\n")
);

/**
 * A skill two clients each hold as a directory of their own, with the same content, whose
 * frontmatter name is not its directory's name: the CLI reads and the app shows the copy that
 * comes first by configuration, named by its directory.
 */
export const meetingNotes: SkillNode = {
  content_hash: "h-meeting",
  description: "Write up a meeting and its decisions",
  logical_id: "skill:h-meeting",
  name: "Meeting Notes",
  occurrences: [
    occurrence(
      "cursor",
      "meeting-notes",
      "directory",
      "/home/me/.cursor/skills/meeting-notes"
    ),
    occurrence(
      "claude-code",
      "meeting-notes",
      "directory",
      "/home/me/.claude-code/skills/meeting-notes"
    ),
  ],
  physical_id: "skill:h-meeting",
};
