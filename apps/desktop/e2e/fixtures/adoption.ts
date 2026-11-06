// The Skills fixture's machine with two more library skills, one whose source was removed and one
// the CLI refuses to adopt, and the adoption preview that names them: what the banners that settle
// a skill and the "Can't adopt" detail are tested against.
import type {
  AdoptionEvent,
  LibraryEntry,
  SkillNode,
} from "#/cli/events.gen.ts";

import type { InlineEvent } from "../harness/cli.ts";
import {
  adoption as skillsAdoption,
  inventory as skillsInventory,
} from "./skills.ts";

export const OLD = "https://github.com/old/skills";
const LIBRARY = "/home/me/.agents/skills";
const universal = ["codex", "gemini-cli"];

const libraryPlacements = (name: string) =>
  universal.map((id) => ({
    configuration: id,
    kind: "library" as const,
    mode: "library" as const,
    path: `${LIBRARY}/${name}`,
  }));

/** A skill of a source that was removed from this machine, and an unmanaged skill the CLI refuses to adopt. */
const more: LibraryEntry[] = [
  {
    base_hash: "h-lint",
    content_hash: "h-lint",
    drift: ["source removed"],
    kind: "managed",
    name: "lint",
    placements: [
      {
        configuration: "claude-code",
        kind: "symlink",
        mode: "symlink",
        path: "/home/me/.claude-code/skills/lint",
      },
      ...libraryPlacements("lint"),
    ],
    source: OLD,
    state: "current",
    subpath: "lint",
    universal,
    upstream_commit: "5e5e5e5e5e5e",
  },
  {
    content_hash: "h-legacy",
    kind: "unmanaged",
    name: "legacy",
    placements: libraryPlacements("legacy"),
    universal,
  },
];

const node = (name: string, hash: string, description: string): SkillNode => ({
  content_hash: hash,
  description,
  logical_id: `skill:${hash}`,
  name,
  occurrences: universal.map((id) => ({
    configuration: id,
    id: `${id}:${LIBRARY}/${name}`,
    kind: "directory" as const,
    path: `${LIBRARY}/${name}`,
    resolved_path: `${LIBRARY}/${name}`,
    scope: "user" as const,
  })),
  physical_id: `skill:${hash}`,
});

/** The serve child's first inventory on this machine. */
export const inventory = {
  ...skillsInventory,
  library: [...skillsInventory.library, ...more],
  skills: [
    ...skillsInventory.skills,
    node("lint", "h-lint", "Lint a change before it lands"),
    node("legacy", "h-legacy", "An old copy the skills CLI left as a link"),
  ],
} satisfies InlineEvent;

/** What `adopt` would take over: the candidate of the Skills fixture, and one entry it refuses. */
export const adoption: AdoptionEvent[] = [
  ...skillsAdoption,
  {
    file: "/home/me/.agents/.skill-lock.json",
    name: "legacy",
    reason: "legacy is a symlink, not a directory",
    schema_version: 2,
    source: "https://github.com/vercel/skills",
    state: "refused",
    subpath: "legacy",
    tool: "vercel",
    type: "adoption",
  },
];
