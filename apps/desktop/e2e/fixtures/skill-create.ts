// The Create skill tests: the Skills fixture's machine before `skill new`, and the inventory the
// scan after it reports, with the new skill of your own in the library, placed in every enabled
// agent: a symlink in Claude Code and Cursor, the library itself for the two that read it.
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
