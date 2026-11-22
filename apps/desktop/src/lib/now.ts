import { useInterval } from "@mantine/hooks";
import { useState } from "react";

/**
 * Date.now(), read again every `everyMs` while the caller is mounted, so that "fetched 2 hours
 * ago" or a countdown keeps time while a screen is open.
 */
export const useNow = (everyMs = 60_000) => {
  const [now, setNow] = useState(() => Date.now());
  useInterval(
    () => {
      setNow(Date.now());
    },
    everyMs,
    { autoInvoke: true }
  );
  return now;
};
