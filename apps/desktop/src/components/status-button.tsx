import { cva } from "class-variance-authority";

import { Spinner } from "#/components/spinner.tsx";

const dot = cva("rounded-shell-status-dot size-2 flex-none", {
  variants: {
    tone: {
      none: "bg-text-hint",
      pending: "bg-shell-status-pending shadow-status-pending",
      published: "bg-shell-status-published shadow-status-published",
    },
  },
});

/** The dot's colour: no account remote, skills to publish, or everything published. */
export type StatusTone = "none" | "pending" | "published";

/**
 * The publish status at the right of the title bar (DESIGN.md §6 "Publish status"): a dot in the
 * state's colour, or a `Spinner` in its place while `turning`, and the label, on a pill that a
 * click runs. `busy` marks it `aria-busy` while its command runs.
 */
export const StatusButton = ({
  tone,
  label,
  turning = false,
  busy = false,
  onClick,
}: {
  tone: StatusTone;
  label: string;
  turning?: boolean;
  busy?: boolean;
  onClick: () => void;
}) => (
  <button
    type="button"
    aria-busy={busy ? true : undefined}
    onClick={onClick}
    className="rounded-shell-status type-status text-shell-topbar-text hover:bg-shell-topbar-hover hover:text-text-secondary h-status flex flex-none items-center gap-2 px-3"
  >
    <span className="text-shell-topbar-text flex size-4 items-center justify-center">
      {turning ? <Spinner /> : <span className={dot({ tone })} />}
    </span>
    {label}
  </button>
);
