// The Create skill tests: the Skills fixture's machine before `skill new`, and the inventory the
// scan after it reports, with the new skill of your own in the library, placed in every enabled
// agent: a symlink in Claude Code and Cursor, the library itself for the two that read it. And the
// Add skill tests: a folder of a source that holds four skills, one of them installed already,
// what Find skills reads of it, and the machine after two more are installed.
import type {
  LibraryEntry,
  LibraryPlacement,
  Occurrence,
  SkillNode,
} from "#/cli/events.gen.ts";

import type { InlineEvent } from "../harness/cli.ts";
import { ACCOUNT, inventory, library, skills } from "./skills.ts";

const LIBRARY = "/home/me/.agents/skills";
const WORKTREES = "/home/me/.agentx/worktrees";

/** The skill the tests create, and the description they give it. */
export const NEW_SKILL = "standup-notes";
export const NEW_DESCRIPTION =
  "Write the daily standup from yesterday's commits";

const direct = ["claude-code", "cursor"];
const universal = ["codex", "gemini-cli"];

const placements: LibraryPlacement[] = [
  ...direct.map((id): LibraryPlacement => ({
    configuration: id,
    kind: "symlink",
    mode: "symlink",
    path: `/home/me/.${id}/skills/${NEW_SKILL}`,
  })),
  ...universal.map((id): LibraryPlacement => ({
    configuration: id,
    kind: "library",
    mode: "library",
    path: `${LIBRARY}/${NEW_SKILL}`,
  })),
];

/** The library entry `skill new` made: one of your own skills, never published. */
export const created: LibraryEntry = {
  content_hash: "h-standup",
  drift: ["not published"],
  fork_id: "5a1d0c3e9b7f2468",
  kind: "managed",
  name: NEW_SKILL,
  placements,
  source: ACCOUNT,
  state: "current",
  universal,
};

const resolved = `${WORKTREES}/${NEW_SKILL}/${NEW_SKILL}`;

const occurrence = (id: string): Occurrence => {
  const path = universal.includes(id)
    ? `${LIBRARY}/${NEW_SKILL}`
    : `/home/me/.${id}/skills/${NEW_SKILL}`;
  return {
    configuration: id,
    id: `${id}:${path}`,
    kind: universal.includes(id) ? "directory" : "symlink",
    path,
    resolved_path: resolved,
    scope: "user",
  };
};

const node: SkillNode = {
  content_hash: "h-standup",
  description: NEW_DESCRIPTION,
  logical_id: "skill:h-standup",
  name: NEW_SKILL,
  occurrences: [...direct, ...universal].map(occurrence),
  physical_id: "skill:h-standup",
};

/** The scan after `skill new`: the Skills fixture's machine with the new skill. */
export const inventoryAfter = {
  ...inventory,
  // Sorted by name, as an inventory lists it: standup-notes comes after every fixture skill.
  library: [...library, created],
  scan_counter: 2,
  skills: [...skills, node],
} satisfies InlineEvent;

/** What `skill new` writes on success: the skill it created, then the result. */
export const createdEvents: InlineEvent[] = [
  { type: "library_skill", ...created },
  {
    ok: true,
    summary: `created ${NEW_SKILL} in 4 configurations`,
    type: "result",
  },
];

/** The source the Add skill tests link into. */
export const ANTHROPIC = "https://github.com/anthropics/skills";
/** The link the Add skill tests paste: a folder of the source that holds several skills. */
export const ADD_LINK = `${ANTHROPIC}/tree/main/skills`;
/** The skills under the link, with their descriptions. */
const descriptions: Record<string, string> = {
  docx: "Read and write Word documents",
  "frontend-design": "Build distinctive web interfaces",
  pptx: "Read and write slide decks",
  xlsx: "Read and write spreadsheets",
};
/** The skill under the link this machine installed before the tests start. */
export const ALREADY_ADDED = "pptx";
/** The two skills the happy path installs. */
export const ADDED_SKILLS = ["docx", "xlsx"];

/** A managed skill of the source, placed like the new skill. */
const addedEntry = (name: string): LibraryEntry => ({
  base_hash: `h-${name}`,
  content_hash: `h-${name}`,
  kind: "managed",
  name,
  placements: placements.map((p) => ({
    ...p,
    path: p.path.replace(NEW_SKILL, name),
  })),
  source: ANTHROPIC,
  state: "current",
  subpath: `skills/${name}`,
  universal,
  upstream_commit: "c0ffee1234567",
});

/** The skill node of a managed skill of the source. */
const addedNode = (name: string): SkillNode => ({
  ...node,
  content_hash: `h-${name}`,
  description: descriptions[name] ?? "",
  logical_id: `skill:h-${name}`,
  name,
  occurrences: node.occurrences.map((o) => ({
    ...o,
    id: o.id.replace(NEW_SKILL, name),
    path: o.path.replace(NEW_SKILL, name),
    resolved_path: `${LIBRARY}/${name}`,
  })),
  physical_id: `skill:h-${name}`,
});

/** The Skills fixture's machine with these skills of the source installed. */
const withAdded = (names: string[], scanCounter: number) => {
  const entries = [...library, ...names.map(addedEntry)];
  // Sorted by name, as an inventory lists it.
  entries.sort((a, b) => a.name.localeCompare(b.name));
  return {
    ...inventory,
    library: entries,
    scan_counter: scanCounter,
    skills: [...skills, ...names.map(addedNode)],
  } satisfies InlineEvent;
};

/** The machine before Add skill: the Skills fixture's, with one skill of the source installed. */
export const inventoryBeforeAdd = withAdded([ALREADY_ADDED], 1);

/** The scan after `skill add`: the two skills it installed besides. */
export const inventoryAfterAdd = withAdded([ALREADY_ADDED, ...ADDED_SKILLS], 2);

/** The `source` event `source add` and `source skills` write for the link. */
const sourceEvent: InlineEvent = {
  id: "anthropics-skills",
  skills: Object.keys(descriptions).length,
  subpath: "skills",
  type: "source",
  url: ANTHROPIC,
};

/** What `source add` writes for the link: the source, added or fetched again. */
export const sourceAddedEvents: InlineEvent[] = [sourceEvent];

/** What `source skills` writes for the link: the source, then one event per skill under it. */
export const sourceSkillEvents: InlineEvent[] = [
  sourceEvent,
  ...Object.entries(descriptions).map(([name, description]): InlineEvent => ({
    description,
    name,
    source: "anthropics-skills",
    subpath: `skills/${name}`,
    tree: `tree-${name}`,
    type: "source_skill",
  })),
];

/** What `skill add` writes when it installs the two skills. */
export const addedEvents: InlineEvent[] = [
  ...ADDED_SKILLS.map((name): InlineEvent => ({
    type: "library_skill",
    ...addedEntry(name),
  })),
  {
    ok: true,
    summary: "installed 2 skills in 4 configurations",
    type: "result",
  },
];
