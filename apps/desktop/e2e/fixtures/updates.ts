// The machine the Updates tests run against: the Skills fixture with one skill of each kind of
// update, as the inventory's library entries carry it (`candidate`, `hub_ahead`), and the
// `update_available` events `skill check-updates` emits for them. Inline events with only the
// fields the tests check, typed by the generated event types.
import { SCHEMA_VERSION } from "#/cli/events.gen.ts";
import type { LibraryEntry, UpdateAvailableEvent } from "#/cli/events.gen.ts";

import type { InlineEvent } from "../harness/cli.ts";
import { HUB, ACME, inventory, library } from "./skills.ts";

export { ACME, ZETA } from "./skills.ts";

/** What the library entry of each skill with an update carries, by name. */
const offers: Record<string, Partial<LibraryEntry>> = {
  // An update of draft, whose pending merge comes first: it is not offered.
  draft: {
    candidate: {
      content_hash: "h-draft-next",
      upstream_commit: "5e4d3c2b1a09",
    },
  },
  // A newer version of pdf from its source, dated.
  pdf: {
    candidate: {
      content_hash: "h-pdf-next",
      upstream_commit: "8e7d6c5b4a39",
      upstream_time: "2026-09-28T09:00:00Z",
    },
    upstream_time: "2026-09-14T09:00:00Z",
  },
  // A newer version of the original pdf-letters was forked from.
  "pdf-letters": {
    candidate: {
      content_hash: "h-letters-next",
      upstream_commit: "9b8c7d6e5f4a3",
    },
  },
  // What another of your machines published to release-notes.
  "release-notes": {
    hub_ahead: { commit: "77e1d0c0ffee", time: "2026-10-01T18:30:00Z" },
  },
  // A newer version of review, which holds edits, from its source; the CLI dates neither version.
  review: {
    candidate: {
      content_hash: "h-review-next",
      upstream_commit: "9b8c7d6e5f4a3",
    },
  },
};

/**
 * The Skills library with an update for each skill of `names`, and none for the others:
 * pdf-letters, a hub skill, holds edits not published, and draft has a pending merge.
 */
export const libraryWith = (names: readonly string[]): LibraryEntry[] =>
  library.map((entry) => {
    const { candidate: _, hub_ahead: __, ...current } = entry;
    return {
      ...current,
      ...(entry.name === "pdf-letters" ? { state: "modified" as const } : {}),
      ...(entry.name === "draft" ? { pending_merge: true } : {}),
      ...(names.includes(entry.name) ? offers[entry.name] : {}),
    };
  });

/** Every skill of the fixture with an update. */
export const updated = Object.keys(offers);

/** The Skills library with every update, as the inventory carries it once a check found them. */
export const updatesLibrary = libraryWith(updated);

/** The inventory of the Skills fixture with `entries` as its library. */
export const inventoryOf = (entries: LibraryEntry[]) =>
  ({ ...inventory, library: entries }) satisfies InlineEvent;

/** The serve child's first inventory on this machine, with every update. */
export const updatesInventory = inventoryOf(updatesLibrary);

const event = (
  fields: Partial<UpdateAvailableEvent> & { name: string }
): UpdateAvailableEvent => ({
  base_hash: `h-${fields.name}-base`,
  candidate: `c0ffee${fields.name}`,
  candidate_hash: `h-${fields.name}-next`,
  candidate_upstream_commit: "9b8c7d6e5f4a3",
  files: [],
  kind: "managed",
  schema_version: SCHEMA_VERSION,
  type: "update_available",
  upstream_commit: "1a2b3c4d5e6f7",
  ...fields,
});

/** What `skill check-updates` emits for pdf's update. */
export const pdfUpdate = event({
  candidate_upstream_commit: "8e7d6c5b4a39",
  candidate_upstream_time: "2026-09-28T09:00:00Z",
  files: [
    { path: "SKILL.md", status: "modified" },
    { path: "scripts/convert.py", status: "added" },
  ],
  name: "pdf",
  source: ACME,
  subpath: "pdf",
  upstream_commit: "3f2a9c1e8d7b6a",
  upstream_time: "2026-09-14T09:00:00Z",
});

/** What `skill check-updates` emits for review's update. */
export const reviewUpdate = event({
  files: [{ path: "SKILL.md", status: "modified" }],
  name: "review",
  source: ACME,
  subpath: "review",
});

/** What `skill check-updates` emits for pdf-letters' update from its upstream. */
const lettersUpdate = event({
  files: [
    { path: "SKILL.md", status: "modified" },
    { path: "examples.md", status: "deleted" },
  ],
  name: "pdf-letters",
  source: HUB,
  upstream: ACME,
  upstream_subpath: "pdf",
});

/** What `skill check-updates` emits for what another of your machines published to release-notes. */
const releaseNotesUpdate = event({
  base_hash: "",
  candidate: "77e1d0c0ffee",
  candidate_hash: "",
  candidate_upstream_commit: "",
  candidate_upstream_time: "2026-10-01T18:30:00Z",
  files: [{ path: "template.md", status: "added" }],
  name: "release-notes",
  source: HUB,
  upstream_commit: "",
});

/** What `skill check-updates` emits for draft's update. */
const draftUpdate = event({
  files: [{ path: "SKILL.md", status: "modified" }],
  name: "draft",
  source: HUB,
});

/** Every update `skill check-updates` finds, sorted by name, as the CLI emits them. */
export const allUpdates: UpdateAvailableEvent[] = [
  draftUpdate,
  pdfUpdate,
  lettersUpdate,
  releaseNotesUpdate,
  reviewUpdate,
];
