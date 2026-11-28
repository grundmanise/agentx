// The machine the Publish tests run against: a hub skill edited since it was
// published, one never published, an edited skill of a source this machine can write to, an
// edited skill of a read-only source and a skill of a source as it was installed. The inventory
// lists the hub and both sources, or, on the second machine, both sources without the hub.
// Inline events with only the fields the tests check.
import type {
  DiffEvent,
  LibraryEntry,
  LibraryPlacement,
  Source,
} from "#/cli/events.gen.ts";

import type { InlineEvent } from "../harness/cli.ts";
import { HUB, ACME, configurations, filesOf, ZETA } from "./skills.ts";

const LIBRARY = "/home/me/.agents/skills";
const universal = ["codex", "gemini-cli"];

/** Placed in Claude Code, and seen by the two clients that read the library. */
const placements = (name: string): LibraryPlacement[] => [
  {
    configuration: "claude-code",
    kind: "symlink",
    mode: "symlink",
    path: `/home/me/.claude-code/skills/${name}`,
  },
  ...universal.map((id): LibraryPlacement => ({
    configuration: id,
    kind: "library",
    mode: "library",
    path: `${LIBRARY}/${name}`,
  })),
];

const hubSkill = (
  name: string,
  fields: Partial<LibraryEntry> = {}
): LibraryEntry => ({
  content_hash: `h-${name}`,
  fork_id: `${name}-fork-id`,
  kind: "managed",
  name,
  placements: placements(name),
  source: HUB,
  state: "current",
  universal,
  ...fields,
});

const ofSource = (
  name: string,
  source: string,
  state: LibraryEntry["state"]
): LibraryEntry => ({
  base_hash: `h-${name}-base`,
  content_hash: state === "modified" ? `h-${name}` : `h-${name}-base`,
  kind: "managed",
  name,
  placements: placements(name),
  source,
  state,
  subpath: name,
  universal,
  upstream_commit: "1a2b3c4d5e6f7",
});

export const library: LibraryEntry[] = [
  hubSkill("changelog", { state: "modified" }),
  hubSkill("draft", { drift: ["not published"] }),
  ofSource("lint", ZETA, "modified"),
  ofSource("pdf", ACME, "current"),
  ofSource("review", ACME, "modified"),
];

const hub: Source = { hub: true, id: "2222222222222222", url: HUB };

const shared: Source[] = [
  { access: "writable", id: "1111111111111111", url: ACME },
  { access: "read-only", id: "3333333333333333", url: ZETA },
];

/** The sources the inventory lists: the hub and two shared sources. */
export const sources: Source[] = [hub, ...shared];

/** The sources the inventory lists on a machine with no hub set. */
export const sourcesWithoutHub: Source[] = shared;

/** The serve child's first inventory on this machine, which lists `listed` as its sources. */
export const inventoryWith = (listed = sources) =>
  ({
    configurations,
    library,
    scan_counter: 1,
    sources: listed,
    type: "inventory",
  }) satisfies InlineEvent;

/** The serve child's first inventory on this machine. */
export const inventory = inventoryWith();

const diff = (
  name: string,
  path: string,
  status: DiffEvent["status"]
): DiffEvent => ({
  name,
  patch: `diff --git a/${path} b/${path}\n`,
  path,
  schema_version: 6,
  status,
  type: "diff",
});

/** What `skill diff <name>` reports for each skill: the files changed since its base. */
export const diffs: Record<string, DiffEvent[]> = {
  changelog: [
    diff("changelog", "SKILL.md", "modified"),
    diff("changelog", "examples.md", "added"),
  ],
  draft: [diff("draft", "SKILL.md", "added")],
  lint: [diff("lint", "SKILL.md", "modified")],
  review: [
    diff("review", "SKILL.md", "modified"),
    diff("review", "notes.md", "added"),
    diff("review", "old.md", "deleted"),
  ],
};

/** The `skill files` read of every skill. */
export const fileReads = library.map((entry) => ({
  args: [entry.name],
  events: filesOf(entry.name),
}));
