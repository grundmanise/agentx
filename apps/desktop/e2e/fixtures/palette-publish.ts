// The machine the title bar's sync status and the palette tests run against: two hub
// skills edited since they were published, one never published, and two skills of a source, one
// with an update the inventory offers. `source list` answers with the hub and the source, or
// without the hub. Variants: nothing to publish, and no hub; the inventory can also
// say what another of your machines published. Inline events with only the fields the tests
// check.
import type { LibraryEntry, LibraryPlacement } from "#/cli/events.gen.ts";

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

const inventoryOf = (entries: LibraryEntry[]) =>
  ({
    configurations,
    library: entries,
    scan_counter: 1,
    type: "inventory",
  }) satisfies InlineEvent;

/** A newer version of pdf from its source, as pdf's library entry carries it. */
const pdfUpdate: Partial<LibraryEntry> = {
  candidate: { content_hash: "h-pdf-next", upstream_commit: "9b8c7d6e5f4a3" },
};

/** What another of your machines published to a hub skill, as its library entry carries it. */
export const fromAnotherMachine: Partial<LibraryEntry> = {
  hub_ahead: { commit: "c0ffee0123456" },
};

/** What `source list` answers: the hub and the source. */
export const sources: InlineEvent[] = [
  { hub: true, id: "2222222222222222", type: "source", url: HUB },
  { access: "read-only", id: "1111111111111111", type: "source", url: ACME },
];

/** What `source list` answers on a machine with no hub. */
export const sourcesWithoutHub: InlineEvent[] = sources.filter(
  (s) => s.type === "source" && s.hub !== true
);

export type Variant = "edited" | "synced" | "no-hub";

export interface LaunchOptions {
  /** Whether the inventory offers the update of pdf; it does unless a test says otherwise. */
  update?: boolean;
  /** Whether the inventory says another of your machines published to notes; it does not by default. */
  incoming?: boolean;
}

/**
 * Starts the app on the fixture machine: serve writes the inventory of `variant`, with the updates
 * it offers, and every read the Skills screen runs for a selected skill is registered.
 */
export const launch = (
  cli: MockCli,
  variant: Variant = "edited",
  { update = true, incoming = false }: LaunchOptions = {}
) => {
  const entries = (variant === "synced" ? syncedLibrary : library).map(
    (entry) => ({
      ...entry,
      ...(update && entry.name === "pdf" ? pdfUpdate : {}),
      ...(incoming && entry.name === "notes" ? fromAnotherMachine : {}),
    })
  );
  cli.onServeStart({ events: [inventoryOf(entries)] });
  cli.register(["source", "list"], {
    events: variant === "no-hub" ? sourcesWithoutHub : sources,
  });
  for (const entry of entries) {
    cli.register(["skill", "history", entry.name], { events: [] });
    cli.register(["skill", "files", entry.name], {
      events: filesOf(entry.name),
    });
    cli.register(["skill", "diff", entry.name], { events: [] });
    cli.register(["skill", "diff", entry.name, "--update"], { events: [] });
  }
  // What the Add skills screen reads when it opens: your skills on the hub and the source's.
  cli.register(["skill", "list", "--remote"], { events: [] });
  cli.register(["source", "skills", "1111111111111111"], { events: [] });
};
