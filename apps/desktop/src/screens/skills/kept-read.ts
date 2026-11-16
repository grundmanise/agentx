import { useState } from "react";

/**
 * The read a section shows: this version's when it is in, and else the one shown before, while
 * this version's is read. When a skill changes while it is selected, as after an update, the
 * section keeps what it showed until the new read comes in, and then swaps it in place, so the
 * pane keeps its height. A failed read is not kept. Undefined until a first read is in.
 * `onFirst` is called when the first read comes in, as the section first shows it.
 */
export const useKeptRead = <T extends { failed?: boolean }>(
  fresh: T | undefined,
  onFirst?: (read: T) => void
): T | undefined => {
  // Set while rendering, from the read the last render showed.
  const [kept, setKept] = useState(fresh);
  if (fresh !== undefined && kept !== fresh) {
    setKept(fresh);
    if (kept === undefined) {
      onFirst?.(fresh);
    }
  }
  return fresh ?? (kept?.failed === true ? undefined : kept);
};
