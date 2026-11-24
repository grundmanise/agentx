import { useMemo, useSyncExternalStore } from "react";

/** How old the time read last may be when a caller starts to listen, before it is read again. */
const STALE_MS = 1000;

/**
 * A clock that reads Date.now() every `everyMs` while a caller listens. A kept screen stops
 * listening while it is hidden and listens again when it shows, so the clock is also read then:
 * no tick ran in between, and the time it holds may be hours old.
 */
const clockOf = (everyMs: number) => {
  let now = Date.now();
  return {
    read: () => now,
    subscribe: (changed: () => void) => {
      if (Date.now() - now >= STALE_MS) {
        now = Date.now();
        changed();
      }
      const timer = window.setInterval(() => {
        now = Date.now();
        changed();
      }, everyMs);
      return () => {
        window.clearInterval(timer);
      };
    },
  };
};

/**
 * Date.now(), read again every `everyMs` while the caller is mounted and each time its screen
 * shows again, so that "fetched 2 hours ago" or a countdown keeps time while a screen is open.
 */
export const useNow = (everyMs = 60_000) => {
  const clock = useMemo(() => clockOf(everyMs), [everyMs]);
  return useSyncExternalStore(clock.subscribe, clock.read);
};
