// The machine the title bar's sync status and the palette tests run against: two hub
// skills edited since they were published, one never published, and two skills of a source, one
// with an update serve found. The inventory lists the hub and the source, or the source
// without the hub. Variants: nothing to publish, and no hub; serve can also
// find what another of your machines published. Inline events with only the fields the tests
// check.
import type {
  LibraryEntry,
  LibraryPlacement,
  Source,
} from "#/cli/events.gen.ts";

import type { InlineEvent, MockCli } from "../harness/cli.ts";
import { HUB, ACME, configurations, filesOf } from "./skills.ts";

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
  hubSkill("changelog", { state: "modified" }),
  hubSkill("draft", { drift: ["not published"] }),
  hubSkill("notes", { state: "modified" }),
  ofSource("pdf"),
  ofSource("review"),
];

/** The same skills with nothing to publish: changelog and notes as published, draft published. */
export const syncedLibrary: LibraryEntry[] = library.map((entry) => ({
  ...entry,
  drift: [],
  state: "current",
}));

const inventoryOf = (entries: LibraryEntry[], listed: Source[]) =>
  ({
    configurations,
    library: entries,
    scan_counter: 1,
    sources: listed,
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
  source: HUB,
  type: "update_available",
  upstream_commit: "",
};

/** The hub, as the inventory lists it. */
export const hub: Source = { hub: true, id: "2222222222222222", url: HUB };

const acme: Source = { access: "read-only", id: "1111111111111111", url: ACME };

/** The sources the inventory lists: the hub and the source. */
export const sources: Source[] = [hub, acme];

export type Variant = "edited" | "synced" | "no-hub";

export interface LaunchOptions {
  /** Whether serve found the update of pdf; it does unless a test says otherwise. */
  update?: boolean;
  /** Whether serve found what another of your machines published to notes; it does not by default. */
  incoming?: boolean;
  /** The hub the inventory lists in place of the variant's. */
  hub?: Source;
}

/**
 * Starts the app on the fixture machine: serve writes the inventory of `variant`, then the updates
 * it found, and every read the Skills screen runs for a selected skill is registered.
 */
export const launch = (
  cli: MockCli,
  variant: Variant = "edited",
  { update = true, incoming = false, ...options }: LaunchOptions = {}
) => {
  const entries = variant === "synced" ? syncedLibrary : library;
  const listed = variant === "no-hub" ? [acme] : [options.hub ?? hub, acme];
  cli.onServeStart({
    events: [
      inventoryOf(entries, listed),
      ...(update ? [pdfUpdate] : []),
      ...(incoming ? [notesFromAnotherMachine] : []),
    ],
  });
  for (const entry of entries) {
    cli.register(["skill", "history", entry.name], { events: [] });
    cli.register(["skill", "files", entry.name], {
      events: filesOf(entry.name),
    });
    cli.register(["skill", "diff", entry.name], { events: [] });
  }
  // What the Add skills screen reads when it opens: your skills on the hub and the source's.
  cli.register(["skill", "list", "--remote"], { events: [] });
  cli.register(["source", "skills", "1111111111111111"], { events: [] });
};
