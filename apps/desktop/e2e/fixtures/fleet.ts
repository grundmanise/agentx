// The machines the Machines tests run against. This machine, `studio`, is its inventory. The hub
// holds the reports of `workbench` and `atelier`, this machine's own report, and the cloud
// machine's snapshot, in the order `machine list` emits them: by machine id, the cloud machine
// last. Inline events with only the fields the tests check, typed by the generated event types.
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
 * This machine as serve scans it: one library skill both agents see, and a skill inside a project,
 * which is no part of what a machine reports.
 */
export const inventory = {
  configurations: [
    configuration("claude-code", { name: "Claude Code" }),
    configuration("codex", { name: "Codex", reads_library: true }),
  ],
  library: [changelogHere],
  machine: { id: MACHINE_ID, label: "studio" },
  scan_counter: 1,
  skills: [
    node("changelog", "h-changelog", [
      { configuration: "claude-code", kind: "symlink", scope: "user" },
      { configuration: "codex", kind: "directory", scope: "user" },
    ]),
    node("notes", "h-notes", [
      { configuration: "claude-code", kind: "directory", scope: "project" },
    ]),
  ],
  type: "inventory",
} satisfies InlineEvent;

const changelog: ReportLibrarySkill = {
  content_hash: "h-changelog-edited",
  kind: "managed",
  name: "changelog",
  placements: [
    { configuration: "claude-code", kind: "symlink", mode: "symlink" },
  ],
  source: SKILLS,
  state: "modified",
  subpath: "skills/changelog",
  universal: ["codex"],
};

const releaseNotes: ReportLibrarySkill = {
  content_hash: "h-release-notes",
  fork_id: "a1b2c3d4e5f6",
  kind: "managed",
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
 * A machine with a skill from every place: the library's, one of them edited there and one a hub
 * skill, a plugin's and one of an agent's own skills directory. Cursor is disabled, and its copy
 * of release-notes was edited in place, which is still the library skill's row.
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
  library: [changelog, releaseNotes],
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
  library: [],
  machine: { agentx: "1.3.2", id: ATELIER_ID, label: "atelier", os: "linux" },
  plugins: [],
  skills: [
    {
      content_hash: "h-scratch",
      name: "scratch",
      occurrences: [{ configuration: "claude-code", kind: "directory" }],
    },
  ],
};

/** The cloud machine, from its snapshot: nothing scanned it, so no skill has a hash or a state. */
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
      name: "changelog",
      placements: [{ configuration: "claude-code" }],
      source: SKILLS,
      subpath: "skills/changelog",
      universal: ["codex"],
    },
    {
      fork_id: "a1b2c3d4e5f6",
      kind: "managed",
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

/** The hub once the report of `workbench` is gone. */
export const fleetWithoutWorkbench = {
  machines: [studio, atelier, cloud],
  type: "fleet",
} satisfies InlineEvent;

/** A hub that holds this machine's report and no other machine. */
export const fleetOfThisMachine = {
  machines: [studio],
  type: "fleet",
} satisfies InlineEvent;
