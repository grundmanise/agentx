import type { ReactNode } from "react";

/**
 * The text of the Check updates toast: "Checking for updates…", then the sources fetched out of
 * all of them, "1/10", once the CLI sends the count. The count has a slot as wide as "99/99" from
 * the start, empty until it comes, so the toast, centred on the window, keeps its width.
 */
export const checkingText = (current?: number, total?: number): ReactNode => (
  <>
    Checking for updates…{" "}
    <span className="min-w-toast-count inline-block tabular-nums">
      {current === undefined || total === undefined
        ? ""
        : `${current}/${total}`}
    </span>
  </>
);
