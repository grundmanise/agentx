import type { ReactNode } from "react";

/**
 * The text of the Check updates toast: "Checking for updates…", then the sources fetched out of
 * all of them, "1/10", once the CLI sends the count, in figures of one width so the text keeps still.
 */
export const checkingText = (current?: number, total?: number): ReactNode => (
  <>
    Checking for updates…{" "}
    <span className="tabular-nums">
      {current === undefined || total === undefined
        ? ""
        : `${current}/${total}`}
    </span>
  </>
);
