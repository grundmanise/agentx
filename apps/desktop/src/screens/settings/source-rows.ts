// What the Account repo section of Settings shows, projected from the Sources slice
// (`source list`). Pure functions, so the section and the tests read the account repo the same
// way.
import type { SourceEvent } from "#/cli/events.gen.ts";

/** The account repo: the one entry `source add --account` wrote, if any. */
export const accountSource = (
  sources: SourceEvent[]
): SourceEvent | undefined => sources.find((s) => s.account === true);
