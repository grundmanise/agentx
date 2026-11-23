// What the Hub section of Settings shows, projected from the Sources slice
// (`source list`). Pure functions, so the section and the tests read the hub the same
// way.
import type { SourceEvent } from "#/cli/events.gen.ts";

/** The hub: the one entry `source add --hub` wrote, if any. */
export const accountRemote = (
  sources: SourceEvent[]
): SourceEvent | undefined => sources.find((s) => s.account === true);
