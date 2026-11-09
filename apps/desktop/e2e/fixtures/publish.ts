// The machine the Publish tests run against: one of your own skills edited since it was
// published, one never published, an edited skill of a source this machine can write to, an
// edited skill of a read-only source and a skill of a source as it was installed. `source list`
// answers with the account remote and both sources, or, on the second machine, without the
// account remote. Inline events with only the fields the tests check.
import type {
  DiffEvent,
  LibraryEntry,
  LibraryPlacement,
} from "#/cli/events.gen.ts";

import type { InlineEvent } from "../harness/cli.ts";
import { ACCOUNT, ACME, configurations, filesOf, ZETA } from "./skills.ts";

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

const yours = (
  name: string,
  fields: Partial<LibraryEntry> = {}
): LibraryEntry => ({
  content_hash: `h-${name}`,
  fork_id: `${name}-fork-id`,
  kind: "managed",
  name,
  placements: placements(name),
  source: ACCOUNT,
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
  yours("changelog", { state: "modified" }),
  yours("draft", { drift: ["not published"] }),
  ofSource("lint", ZETA, "modified"),
  ofSource("pdf", ACME, "current"),
  ofSource("review", ACME, "modified"),
];

/** The serve child's first inventory on this machine. */
export const inventory = {
  configurations,
  library,
  scan_counter: 1,
  type: "inventory",
} satisfies InlineEvent;

const accountRemote: InlineEvent = {
  account: true,
  id: "2222222222222222",
  type: "source",
  url: ACCOUNT,
};

const shared: InlineEvent[] = [
  { access: "writable", id: "1111111111111111", type: "source", url: ACME },
  { access: "read-only", id: "3333333333333333", type: "source", url: ZETA },
];

/** What `source list` answers: the account remote and two shared sources. */
export const sources: InlineEvent[] = [accountRemote, ...shared];

/** What `source list` answers on a machine with no account remote set. */
export const sourcesWithoutAccount: InlineEvent[] = shared;

const diff = (
  name: string,
  path: string,
  status: DiffEvent["status"]
): DiffEvent => ({
  name,
  patch: `diff --git a/${path} b/${path}\n`,
  path,
  schema_version: 2,
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
