import type { ReactNode } from "react";

/**
 * One number of the count, in a slot as wide as one digit: the dash that stands in for it until
 * the CLI sends the count takes the room of the digit that replaces it.
 */
const Count = ({ n }: { n: number | undefined }) => (
  <span className="min-w-toast-count inline-block text-center tabular-nums">
    {n ?? "–"}
  </span>
);

/**
 * The text of the Check updates toast: "Checking for updates · – of – sources" until the CLI
 * sends the count, then "· 0 of 2 sources" and on. It keeps its width as the count comes, so that
 * the toast, centred on the window, does not move: each number has a slot one digit wide, and
 * "source" keeps the room of the "s" it drops.
 */
export const checkingText = (current?: number, total?: number): ReactNode => (
  <>
    Checking for updates ·{" "}
    <Count n={total === undefined ? undefined : current} /> of{" "}
    <Count n={total} />{" "}
    {total === 1 ? (
      <>
        source
        {/* The room of the "s" that "source" drops, out of the text that is read. */}
        <span aria-hidden className="invisible">
          s
        </span>
      </>
    ) : (
      "sources"
    )}
  </>
);
