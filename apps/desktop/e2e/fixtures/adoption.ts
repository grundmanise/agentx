// The Skills fixture's machine with two more library skills, one whose source was removed and one
// the CLI refuses to adopt: what the banners that settle a skill and a refused skill are tested
// against.
import type { LibraryEntry, SkillNode } from "#/cli/events.gen.ts";

import type { InlineEvent } from "../harness/cli.ts";
import { inventory as skillsInventory } from "./skills.ts";

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

/**
 * A skill of a source that was removed from this machine, and an unmanaged skill the CLI refuses
 * to adopt: the library holds a symlink of it, and its record names nowhere to install it from.
 */
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

/** A library skill as the clients that read the library see it: a folder, or a symlink to `linkedTo`. */
const node = (
  name: string,
  hash: string,
  description: string,
  linkedTo?: string
): SkillNode => ({
  content_hash: hash,
  description,
  logical_id: `skill:${hash}`,
  name,
  occurrences: universal.map((id) => ({
    configuration: id,
    id: `${id}:${LIBRARY}/${name}`,
    kind:
      linkedTo === undefined ? ("directory" as const) : ("symlink" as const),
    path: `${LIBRARY}/${name}`,
    resolved_path: linkedTo ?? `${LIBRARY}/${name}`,
    scope: "user" as const,
  })),
  physical_id: `skill:${hash}`,
});

/**
 * The serve child's first inventory on this machine. What `skill import` can adopt is the skill
 * of the Skills fixture, and it refuses the symlink.
 */
export const inventory = {
  ...skillsInventory,
  adoptable: [
    {
      refused: [{ name: "legacy", reason: "symlink" }],
      skills: ["scratch"],
      tool: "vercel",
    },
  ],
  library: [...skillsInventory.library, ...more],
  skills: [
    ...skillsInventory.skills,
    node("lint", "h-lint", "Lint a change before it lands"),
    node(
      "legacy",
      "h-legacy",
      "Format a changelog from release tags",
      "/home/me/work/legacy"
    ),
  ],
} satisfies InlineEvent;
