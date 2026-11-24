import { cva } from "class-variance-authority";
import type { ReactNode } from "react";

import { ProgressRim } from "#/components/progress-rim/progress-rim.tsx";
import type { RimRun } from "#/components/progress-rim/rim-progress.ts";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "#/components/ui/tooltip.tsx";

const dot = cva("rounded-shell-status-dot size-2 flex-none", {
  variants: {
    tone: {
      none: "bg-text-hint",
      pending: "bg-shell-status-pending shadow-status-pending",
      synced: "bg-shell-status-published shadow-status-published",
    },
  },
});

const noLines: readonly string[] = [];
const noop = () => {
  // Nothing to do once the rim has faded.
};

/** The dot's colour: no hub, something to publish or take in, or synced. */
export type StatusTone = "none" | "pending" | "synced";

/**
 * A status at the right of the title bar (DESIGN.md §6 "Publish status (title bar)", "App
 * update (title bar)"): a dot in the state's colour, or `icon` in its place, and the label, on a
 * pill. `tooltip` holds the lines its tooltip shows, one each; none shows no tooltip. `busy`
 * marks it `aria-busy` while its command runs, and `rim` draws that run's progress on its edge
 * (§8 "Progress rim"), until the rim has faded. As the trigger of the popover it opens, it takes
 * what the popover gives it (its state attributes, the click and the ref) and passes them to the
 * button; its accessible name stays the label.
 */
export const StatusButton = ({
  tone,
  icon,
  label,
  tooltip = noLines,
  busy = false,
  rim,
  onRimSettled = noop,
  ...props
}: {
  tone: StatusTone;
  icon?: ReactNode;
  label: string;
  tooltip?: readonly string[];
  busy?: boolean;
  rim?: RimRun;
  onRimSettled?: () => void;
  onClick?: () => void;
}) => (
  <Tooltip>
    <TooltipTrigger asChild>
      <button
        type="button"
        aria-busy={busy ? true : undefined}
        className="rounded-shell-status type-status text-shell-topbar-text bg-button-ghost-bg hover:bg-button-ghost-hover hover:text-text-secondary h-status relative flex flex-none items-center gap-2 px-3"
        {...props}
      >
        <span className="text-shell-topbar-text flex size-4 items-center justify-center">
          {icon ?? <span className={dot({ tone })} />}
        </span>
        {label}
        <ProgressRim run={rim} onSettled={onRimSettled} size="sm" />
      </button>
    </TooltipTrigger>
    {tooltip.length > 0 && (
      <TooltipContent placement="above-end">
        {tooltip.map((line) => (
          <span key={line} className="block">
            {line}
          </span>
        ))}
      </TooltipContent>
    )}
  </Tooltip>
);

/**
 * The sync status for a screen reader: a polite live region beside the button, never inside it,
 * where a live region may go unread. It holds the summary the button shows, so a change the
 * background check brings is announced; the button keeps its own name.
 */
export const StatusAnnouncement = ({ text }: { text: string }) => (
  <output className="sr-only">{text}</output>
);
