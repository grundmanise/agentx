// The machine the publish status and palette tests run against: two of your own skills edited
// since they were published, one never published, and two skills of a source, one with an update
// serve found. `source list` answers with the account remote and the source, or without the
// account remote. Variants: nothing to publish, and no account remote. Inline events with only
// the fields the tests check.
import type { LibraryEntry, LibraryPlacement } from "#/cli/events.gen.ts";

import type { InlineEvent, MockCli } from "../harness/cli.ts";
import { ACCOUNT, ACME, configurations, filesOf } from "./skills.ts";

const LIBRARY = "/home/me/.agents/skills";
const universal = ["codex", "gemini-cli"];

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

const ofSource = (name: string): LibraryEntry => ({
  base_hash: `h-${name}`,
  content_hash: `h-${name}`,
  kind: "managed",
  name,
  placements: placements(name),
  source: ACME,
  state: "current",
  subpath: name,
  universal,
  upstream_commit: "1a2b3c4d5e6f7",
});

/** changelog and notes hold edits not published; draft was never published. */
export const library: LibraryEntry[] = [
  yours("changelog", { state: "modified" }),
  yours("draft", { drift: ["not published"] }),
  yours("notes", { state: "modified" }),
  ofSource("pdf"),
  ofSource("review"),
];

/** The same skills with nothing to publish: changelog and notes as they were published. */
export const publishedLibrary: LibraryEntry[] = library.map((entry) =>
  entry.state === "modified" ? { ...entry, state: "current" } : entry
);

const inventoryOf = (entries: LibraryEntry[]) =>
  ({
    configurations,
    library: entries,
    scan_counter: 1,
    type: "inventory",
  }) satisfies InlineEvent;

/** A newer version of pdf from its source. */
export const pdfUpdate: InlineEvent = {
  base_hash: "h-pdf",
  candidate: "c0ffeepdf",
  files: [{ path: "SKILL.md", status: "modified" }],
  kind: "managed",
  name: "pdf",
  source: ACME,
  subpath: "pdf",
  type: "update_available",
  upstream_commit: "1a2b3c4d5e6f7",
};

/** What another of your machines published to notes: an update with no base. */
export const notesFromAnotherMachine: InlineEvent = {
  base_hash: "",
  candidate: "c0ffeenotes",
  candidate_hash: "",
  candidate_upstream_commit: "",
  files: [{ path: "SKILL.md", status: "modified" }],
  kind: "managed",
  name: "notes",
  source: ACCOUNT,
  type: "update_available",
  upstream_commit: "",
};

/** What `source list` answers: the account remote and the source. */
export const sources: InlineEvent[] = [
  { account: true, id: "2222222222222222", type: "source", url: ACCOUNT },
  { access: "read-only", id: "1111111111111111", type: "source", url: ACME },
];

/** What `source list` answers on a machine with no account remote. */
export const sourcesWithoutAccount: InlineEvent[] = sources.filter(
  (s) => s.type === "source" && s.account !== true
);

export type Variant = "edited" | "published" | "no-account-remote";

export interface LaunchOptions {
  /** Whether serve found the update of pdf; it does unless a test says otherwise. */
  update?: boolean;
}

/**
 * Starts the app on the fixture machine: serve writes the inventory of `variant`, then the update
 * it found for pdf, and every read the Skills screen runs for a selected skill is registered.
 */
export const launch = (
  cli: MockCli,
  variant: Variant = "edited",
  { update = true }: LaunchOptions = {}
) => {
  const entries = variant === "published" ? publishedLibrary : library;
  cli.onServeStart({
    events: [inventoryOf(entries), ...(update ? [pdfUpdate] : [])],
  });
  cli.register(["source", "list"], {
    events: variant === "no-account-remote" ? sourcesWithoutAccount : sources,
  });
  for (const entry of entries) {
    cli.register(["skill", "history", entry.name], { events: [] });
    cli.register(["skill", "files", entry.name], {
      events: filesOf(entry.name),
    });
    cli.register(["skill", "diff", entry.name], { events: [] });
  }
  // What the Add skills screen reads when it opens: your skills on the account remote and the source's.
  cli.register(["skill", "list", "--remote"], { events: [] });
  cli.register(["source", "skills", "1111111111111111"], { events: [] });
};

/** The warning a bare publish gives for draft, which it leaves out. */
export const draftNotPublished = {
  level: "warn" as const,
  message:
    "draft is not published: run 'agentx skill publish draft' to publish it",
};
