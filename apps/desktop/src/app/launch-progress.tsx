import { Spinner } from "#/components/spinner.tsx";
import { useNow } from "#/lib/now.ts";
import { CHECK_DEADLINE_MS } from "#/startup/startup.ts";
import type { Step } from "#/startup/startup.ts";

/** From this far into a check, the panel says how much longer the check keeps trying. */
const COUNTDOWN_AFTER_MS = 5000;

/**
 * What a launch with no kept inventory shows in the empty panel, from its first moment: that it
 * loads and, from 5 seconds into a check, how much longer that check keeps trying. A screen
 * reader hears each line once, not each second of the countdown.
 */
export const LaunchProgress = ({ step }: { step: Step }) => {
  const elapsed = Math.max(0, useNow(500) - step.startedAt);
  const late = elapsed >= COUNTDOWN_AFTER_MS;
  const left = Math.max(0, Math.ceil((CHECK_DEADLINE_MS - elapsed) / 1000));
  return (
    <output className="animate-fade-in flex flex-1 flex-col items-center justify-center gap-1">
      <span className="sr-only">
        Starting the app…
        {late && ". Taking longer than usual"}
      </span>
      <span
        aria-hidden
        className="type-secondary text-text-muted flex items-center gap-2"
      >
        <Spinner />
        Starting the app…
      </span>
      {late && (
        <span aria-hidden className="type-row-desc text-text-hint">
          Taking longer than usual · trying for {left}s more
        </span>
      )}
    </output>
  );
};
