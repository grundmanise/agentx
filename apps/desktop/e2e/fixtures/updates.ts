// The machine the Updates tests run against: the Skills fixture with one skill of each kind of
// update, and the `update_available` events an update check finds for them. Inline events with
// only the fields the tests check, typed by the generated event types.
import { SCHEMA_VERSION } from "#/cli/events.gen.ts";
import type { LibraryEntry, UpdateAvailableEvent } from "#/cli/events.gen.ts";

import type { InlineEvent } from "../harness/cli.ts";
import { ACCOUNT, ACME, inventory, library } from "./skills.ts";

export { ACME, ZETA } from "./skills.ts";

/**
 * The Skills library, where pdf-letters, one of your own, holds edits not published, and draft
 * has a pending merge: pdf is an unmodified skill of a source, review a modified one.
 */
export const updatesLibrary: LibraryEntry[] = library.map((entry) => {
  if (entry.name === "pdf-letters") {
    return { ...entry, state: "modified" };
  }
  if (entry.name === "draft") {
    return { ...entry, pending_merge: true };
  }
  return entry;
});

/** The serve child's first inventory on this machine. */
export const updatesInventory = {
  ...inventory,
  library: updatesLibrary,
} satisfies InlineEvent;

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

/** A newer version of pdf from its source, dated. */
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

/** A newer version of review, which holds edits, from its source; the CLI dates neither version. */
export const reviewUpdate = event({
  files: [{ path: "SKILL.md", status: "modified" }],
  name: "review",
  source: ACME,
  subpath: "review",
});

/** A newer version of the original pdf-letters was forked from. */
export const lettersUpdate = event({
  files: [
    { path: "SKILL.md", status: "modified" },
    { path: "examples.md", status: "deleted" },
  ],
  name: "pdf-letters",
  source: ACCOUNT,
  upstream: ACME,
  upstream_subpath: "pdf",
});

/** What another of your machines published to release-notes: no base version. */
export const releaseNotesUpdate = event({
  base_hash: "",
  candidate: "77e1d0c0ffee",
  candidate_hash: "",
  candidate_upstream_commit: "",
  candidate_upstream_time: "2026-10-01T18:30:00Z",
  files: [{ path: "template.md", status: "added" }],
  name: "release-notes",
  source: ACCOUNT,
  upstream_commit: "",
});

/** An update of draft, whose pending merge comes first: it is not offered. */
export const draftUpdate = event({
  files: [{ path: "SKILL.md", status: "modified" }],
  name: "draft",
  source: ACCOUNT,
});

/** Every update the check finds, sorted by name, as the CLI emits them. */
export const allUpdates: UpdateAvailableEvent[] = [
  draftUpdate,
  pdfUpdate,
  lettersUpdate,
  releaseNotesUpdate,
  reviewUpdate,
];
