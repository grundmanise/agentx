import { useEffect, useRef } from "react";

import type { SourceEvent } from "#/cli/events.gen.ts";
import { loadListing } from "#/store/loaders.ts";

/**
 * Reads the listing of each fetched shared source, for its skill count on the pills, and again
 * after each fetch, which `source list` reports as a new last fetch: `source list` gives no count
 * for a shared source. A source never fetched has no listing to read.
 */
export const useFetchedListings = (sources: SourceEvent[]) => {
  // The fetch each listing was read for: source id → last_fetched.
  const read = useRef(new Map<string, string>());
  useEffect(() => {
    for (const source of sources) {
      const fetched = source.last_fetched;
      if (
        source.account !== true &&
        fetched !== undefined &&
        read.current.get(source.id) !== fetched
      ) {
        read.current.set(source.id, fetched);
        void loadListing(source.id);
      }
    }
  }, [sources]);
};
