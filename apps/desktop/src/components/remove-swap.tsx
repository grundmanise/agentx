import { cn } from "#/lib/utils.ts";

/**
 * The status of a skill a Sources card holds already, Added, that turns into a red Remove while
 * hovered or focused (DESIGN.md §6 "Sources screen"): Added hides at once and Remove fades in, in
 * the same place. `label` is its accessible name; `busy` keeps Remove shown and the button
 * disabled while the removal runs.
 */
export const RemoveSwap = ({
  status,
  label,
  busy = false,
  onClick,
}: {
  status: string;
  label: string;
  busy?: boolean;
  onClick: () => void;
}) => (
  <button
    type="button"
    aria-label={label}
    aria-busy={busy ? true : undefined}
    disabled={busy}
    onClick={onClick}
    className="group/swap rounded-pill focus-visible:outline-shell-focus-ring grid h-8 cursor-pointer items-center justify-items-end px-1.5 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-solid disabled:cursor-default"
  >
    {/* Both words share one grid cell, so the button is as wide as the longer one and the row
        never moves when they swap. */}
    <span
      aria-hidden
      className={cn(
        "type-secondary text-add-card-status-added col-start-1 row-start-1 font-medium group-hover/swap:opacity-0 group-focus-visible/swap:opacity-0",
        busy && "opacity-0"
      )}
    >
      {status}
    </span>
    {/* Remove has no fill: red text that fades in while the button is hovered or focused, and
        stays while its command runs. */}
    <span
      aria-hidden
      className={cn(
        "type-secondary text-add-card-status-remove col-start-1 row-start-1 font-medium transition-opacity duration-150 group-hover/swap:opacity-100 group-focus-visible/swap:opacity-100",
        busy ? "opacity-100" : "opacity-0"
      )}
    >
      Remove
    </span>
  </button>
);
