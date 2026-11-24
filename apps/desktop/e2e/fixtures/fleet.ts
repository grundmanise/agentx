// The machines the Machines tests run against, and the test of a skill's Also on line. This
// machine, `studio`, is its inventory. The hub holds the reports of `workbench` and `atelier`, this
// machine's own report, and the cloud machine's snapshot, in the order `machine list` emits them:
// by machine id, the cloud machine last. Across them: `commit-message` is the same on every
// machine; `changelog` is the same on `atelier`, edited on `workbench` and at an older commit of
// its source on the cloud machine; `release-notes`, `lint` and `scratch` are on other machines
// only. Inline events with only the fields the tests check, typed by the generated event types.
import type {
  FleetMachine,
  LibraryEntry,
  ReportLibrarySkill,
  SkillNode,
} from "#/cli/events.gen.ts";

import { configuration } from "../harness/cli.ts";
import type { InlineEvent } from "../harness/cli.ts";
import { MACHINE_ID, machine } from "./settings.ts";

export const SKILLS = "https://github.com/example/skills";
const HUB = "https://github.com/example/hub";
export const WORKBENCH_ID = "0b6e2d9c4a7f41e3a5d8c1b2e3f40a96";
const ATELIER_ID = "c4d1e8a2f6b94c07921d3e5a7b8f6c10";

/** The commit of the source `changelog` is installed at, and the one before it. */
const CHANGELOG_COMMIT = "9d8c7b6a5f4e3d2c";
const CHANGELOG_COMMIT_BEFORE = "3e2d1c0b9a8f7e6d";
/** The fork ids of the hub skills. */
const COMMIT_MESSAGE_FORK = "c0de5a7e1b2c";
const RELEASE_NOTES_FORK = "a1b2c3d4e5f6";

const HOUR = 3_600_000;
const ago = (ms: number) => new Date(Date.now() - ms).toISOString();

/** `machine` on this machine: its label and its id. */
export const thisMachine = machine("studio");

const changelogHere: LibraryEntry = {
  content_hash: "h-changelog",
  kind: "managed",
  logical_id: "skill:changelog",
  name: "changelog",
  placements: [
    {
      configuration: "claude-code",
      kind: "symlink",
      mode: "symlink",
      path: "/home/me/.claude-code/skills/changelog",
    },
  ],
  source: SKILLS,
  state: "current",
  subpath: "skills/changelog",
  universal: ["codex"],
  upstream_commit: CHANGELOG_COMMIT,
};

const commitMessageHere: LibraryEntry = {
  content_hash: "h-commit-message",
  fork_id: COMMIT_MESSAGE_FORK,
  kind: "managed",
  logical_id: "skill:commit-message",
  name: "commit-message",
  placements: [
    {
      configuration: "claude-code",
      kind: "symlink",
      mode: "symlink",
      path: "/home/me/.claude-code/skills/commit-message",
    },
  ],
  source: HUB,
  state: "current",
  universal: ["codex"],
};

const node = (
  name: string,
  hash: string,
  occurrences: Pick<
    SkillNode["occurrences"][number],
    "configuration" | "kind" | "scope"
  >[]
): SkillNode => ({
  content_hash: hash,
  description: "",
  logical_id: `skill:${hash}`,
  name,
  occurrences: occurrences.map((o) => ({
    ...o,
    id: `${o.configuration}:${name}:${o.scope}`,
    path: `/home/me/.${o.configuration}/skills/${name}`,
    resolved_path: `/home/me/.${o.configuration}/skills/${name}`,
  })),
  physical_id: `skill:${hash}`,
});

/**
 * This machine as serve scans it: two library skills both agents see, and a skill inside a
 * project, which is no part of what a machine reports.
 */
export const inventory = {
  configurations: [
    configuration("claude-code", { name: "Claude Code" }),
    configuration("codex", { name: "Codex", reads_library: true }),
  ],
  library: [changelogHere, commitMessageHere],
  machine: { id: MACHINE_ID, label: "studio" },
  scan_counter: 1,
  skills: [
    node("changelog", "h-changelog", [
      { configuration: "claude-code", kind: "symlink", scope: "user" },
      { configuration: "codex", kind: "directory", scope: "user" },
    ]),
    node("commit-message", "h-commit-message", [
      { configuration: "claude-code", kind: "symlink", scope: "user" },
      { configuration: "codex", kind: "directory", scope: "user" },
    ]),
    node("notes", "h-notes", [
      { configuration: "claude-code", kind: "directory", scope: "project" },
    ]),
  ],
  type: "inventory",
} satisfies InlineEvent;

/** `changelog` as another machine reports it: as it is here, or edited there. */
const changelog = (edited: boolean): ReportLibrarySkill => ({
  content_hash: edited ? "h-changelog-edited" : "h-changelog",
  kind: "managed",
  logical_id: "skill:changelog",
  name: "changelog",
  placements: [
    { configuration: "claude-code", kind: "symlink", mode: "symlink" },
  ],
  source: SKILLS,
  state: edited ? "modified" : "current",
  subpath: "skills/changelog",
  universal: ["codex"],
  upstream_commit: CHANGELOG_COMMIT,
});

/** `commit-message` as another machine reports it: the same content everywhere. */
const commitMessage: ReportLibrarySkill = {
  content_hash: "h-commit-message",
  fork_id: COMMIT_MESSAGE_FORK,
  kind: "managed",
  logical_id: "skill:commit-message",
  name: "commit-message",
  placements: [
    { configuration: "claude-code", kind: "symlink", mode: "symlink" },
  ],
  source: HUB,
  state: "current",
  universal: ["codex"],
};

const releaseNotes: ReportLibrarySkill = {
  content_hash: "h-release-notes",
  fork_id: RELEASE_NOTES_FORK,
  kind: "managed",
  logical_id: "skill:release-notes",
  name: "release-notes",
  placements: [
    { configuration: "claude-code", kind: "symlink", mode: "symlink" },
    { configuration: "cursor", kind: "copy", mode: "copy" },
  ],
  source: HUB,
  state: "current",
  universal: ["codex"],
};

/**
 * A machine with a skill from every place: the library's, one of them edited there and two of them
 * hub skills, a plugin's and one of an agent's own skills directory. Cursor is disabled, and its
 * copy of release-notes was edited in place, which is still the library skill's row.
 */
const workbench: FleetMachine = {
  changed_at: "2025-03-09T10:00:00Z",
  commit: "4c1d2e7a9b3f",
  configurations: [
    {
      enabled: true,
      id: "claude-code",
      name: "Claude Code",
      reads_library: false,
    },
    { enabled: true, id: "codex", name: "Codex", reads_library: true },
    { enabled: false, id: "cursor", name: "Cursor", reads_library: false },
  ],
  library: [changelog(true), commitMessage, releaseNotes],
  machine: {
    agentx: "1.4.0",
    id: WORKBENCH_ID,
    label: "workbench",
    os: "darwin",
  },
  plugins: [
    {
      configuration: "claude-code",
      marketplace: "example-marketplace",
      name: "review",
      version: "1.2.0",
    },
  ],
  skills: [
    {
      content_hash: "h-changelog-edited",
      name: "changelog",
      occurrences: [
        { configuration: "claude-code", kind: "symlink" },
        { configuration: "codex", kind: "directory" },
      ],
    },
    {
      content_hash: "h-commit-message",
      name: "commit-message",
      occurrences: [
        { configuration: "claude-code", kind: "symlink" },
        { configuration: "codex", kind: "directory" },
      ],
    },
    {
      content_hash: "h-lint",
      name: "lint",
      occurrences: [
        { configuration: "claude-code", kind: "directory", plugin: "review" },
      ],
    },
    {
      content_hash: "h-release-notes",
      name: "release-notes",
      occurrences: [
        { configuration: "claude-code", kind: "symlink" },
        { configuration: "codex", kind: "directory" },
      ],
    },
    {
      content_hash: "h-release-notes-copy",
      name: "release-notes",
      occurrences: [{ configuration: "cursor", kind: "copy" }],
    },
    {
      content_hash: "h-scratch",
      name: "scratch",
      occurrences: [{ configuration: "claude-code", kind: "directory" }],
    },
  ],
};

/** The report this machine wrote: its row shows the machine as it is now, not this. */
const studio: FleetMachine = {
  changed_at: ago(26 * HOUR),
  commit: "8e5f6a7b0c1d",
  configurations: [],
  library: [],
  machine: { agentx: "1.4.0", id: MACHINE_ID, label: "studio", os: "darwin" },
  plugins: [],
  skills: [],
};

const atelier: FleetMachine = {
  changed_at: ago(2 * HOUR),
  commit: "1a2b3c4d5e6f",
  configurations: [
    {
      enabled: true,
      id: "claude-code",
      name: "Claude Code",
      reads_library: false,
    },
  ],
  // Its one agent reads no library: each skill is placed in it.
  library: [changelog(false), commitMessage].map((entry) => ({
    ...entry,
    universal: [],
  })),
  machine: { agentx: "1.3.2", id: ATELIER_ID, label: "atelier", os: "linux" },
  plugins: [],
  skills: [
    {
      content_hash: "h-changelog",
      name: "changelog",
      occurrences: [{ configuration: "claude-code", kind: "symlink" }],
    },
    {
      content_hash: "h-commit-message",
      name: "commit-message",
      occurrences: [{ configuration: "claude-code", kind: "symlink" }],
    },
    {
      content_hash: "h-scratch",
      name: "scratch",
      occurrences: [{ configuration: "claude-code", kind: "directory" }],
    },
  ],
};

/**
 * The cloud machine, from its snapshot: nothing scanned it, so no skill has a hash or a state. Its
 * `changelog` is pinned at the commit before the one this machine has; its hub skills name no
 * commit, so nothing tells their version.
 */
const cloud: FleetMachine = {
  changed_at: ago(72 * HOUR),
  commit: "7f6e5d4c3b2a",
  configurations: [
    {
      enabled: true,
      id: "claude-code",
      name: "Claude Code",
      reads_library: false,
    },
    { enabled: true, id: "codex", name: "Codex", reads_library: true },
  ],
  library: [
    {
      kind: "managed",
      logical_id: "skill:changelog",
      name: "changelog",
      placements: [{ configuration: "claude-code" }],
      source: SKILLS,
      subpath: "skills/changelog",
      universal: ["codex"],
      upstream_commit: CHANGELOG_COMMIT_BEFORE,
    },
    {
      fork_id: COMMIT_MESSAGE_FORK,
      kind: "managed",
      logical_id: "skill:commit-message",
      name: "commit-message",
      placements: [],
      universal: ["codex"],
    },
    {
      fork_id: RELEASE_NOTES_FORK,
      kind: "managed",
      logical_id: "skill:release-notes",
      name: "release-notes",
      placements: [],
      universal: ["codex"],
    },
  ],
  machine: { id: "cloud", label: "cloud" },
  plugins: [],
  skills: [],
};

/** What `machine list` emits: every machine the hub holds. */
export const fleet = {
  machines: [workbench, studio, atelier, cloud],
  type: "fleet",
} satisfies InlineEvent;

/** The hub once the report of `workbench` is removed. */
export const fleetWithoutWorkbench = {
  machines: [studio, atelier, cloud],
  type: "fleet",
} satisfies InlineEvent;

/** A hub whose one other machine holds this machine's skills alone, in the same versions. */
export const fleetOfSameSkills = {
  machines: [
    studio,
    { ...atelier, skills: atelier.skills.filter((s) => s.name !== "scratch") },
  ],
  type: "fleet",
} satisfies InlineEvent;

/** A hub that holds this machine's report and no other machine. */
export const fleetOfThisMachine = {
  machines: [studio],
  type: "fleet",
} satisfies InlineEvent;
