// Run something once for each new item a list shows, such as bringing the selected row into view.
// A kept screen's effects run again when it shows again (app/kept-screens.tsx), and the list is
// still as the user left it, so a key that has not changed does nothing.
import { useEffect, useEffectEvent, useRef } from "react";

/**
 * Calls `run` when `key` names a new item: not on a render with the same key, and not when the
 * screen shows again. A key that goes (undefined) and comes back runs it again. A run cut short
 * within its first frame, as when StrictMode runs the effect twice, is made again.
 */
export const useOnNewKey = (
  key: string | undefined,
  run: (key: string) => void
) => {
  const onKey = useEffectEvent(run);
  const last = useRef<string | null>(null);
  useEffect(() => {
    const fresh = key !== undefined && key !== last.current;
    last.current = key ?? null;
    if (fresh) {
      onKey(key);
    }
    let started = !fresh;
    const frame = requestAnimationFrame(() => {
      started = true;
    });
    return () => {
      cancelAnimationFrame(frame);
      if (!started) {
        last.current = null;
      }
    };
  }, [key]);
};
