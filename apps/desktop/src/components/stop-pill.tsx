import { CloseIcon } from "#/components/icons/close.tsx";
import { Spinner } from "#/components/ui/spinner.tsx";
import { trackAttribute } from "#/lib/track.ts";
import type { Tracked } from "#/lib/track.ts";

/**
 * A source pill whose command runs (DESIGN.md §6 "Stop pill"): a `Spinner` before its label, which
 * pointing at it or focusing it swaps for a cross; a click stops the command. Its name says so.
 */
export const StopPill = ({
  label,
  stopLabel,
  onStop,
  track,
}: Tracked & {
  label: string;
  /** Its name for a screen reader, such as Stop adding {label}. */
  stopLabel: string;
  onStop: () => void;
}) => (
  <button
    type="button"
    {...trackAttribute(track)}
    aria-busy
    aria-label={stopLabel}
    onClick={onStop}
    className="group rounded-pill type-button-sm bg-source-pill-bg text-text-secondary hover:text-text-primary inline-flex h-8 items-center gap-2 px-3.5 font-medium whitespace-nowrap"
  >
    <span className="grid size-4 place-items-center">
      <span className="col-start-1 row-start-1 group-hover:invisible group-focus-visible:invisible">
        <Spinner size="sm" />
      </span>
      <span className="invisible col-start-1 row-start-1 group-hover:visible group-focus-visible:visible">
        <CloseIcon size="xs" />
      </span>
    </span>
    {label}
  </button>
);
