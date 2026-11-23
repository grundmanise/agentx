// What the Account remote section of Settings shows, projected from the Sources slice
// (`source list`). Pure functions, so the section and the tests read the account remote the same
// way.
import type { SourceEvent } from "#/cli/events.gen.ts";

/** The account remote: the one entry `source add --account` wrote, if any. */
export const accountRemote = (
  sources: SourceEvent[]
): SourceEvent | undefined => sources.find((s) => s.account === true);
