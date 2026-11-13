import { useState } from "react";

import type { Configuration, ProgressEvent } from "#/cli/events.gen.ts";
import { AgentLogo, LogoStack } from "#/components/agent-logo.tsx";
import { Button } from "#/components/ui/button.tsx";
import { Toggle } from "#/components/ui/toggle.tsx";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "#/components/ui/tooltip.tsx";
import { AgentPickerDialog } from "#/screens/add/agent-picker-dialog.tsx";
import { universalTitle } from "#/screens/add/projection.ts";

/** How many target logos the bar shows before the rest go behind the "+n" button. */
const SHOWN_TARGETS = 5;

/** The ids of `direct`, the chosen ones first, each group in inventory order. */
const targetOrder = (direct: Configuration[], targets: string[]) => [
  ...direct.filter((c) => targets.includes(c.id)).map((c) => c.id),
  ...direct.filter((c) => !targets.includes(c.id)).map((c) => c.id),
];

/**
 * The configurations the bar shows as logos, and the rest, in `order`. A configuration the order
 * does not know yet (a new inventory) goes last. When one more would fit in place of the "+n"
 * button, the bar shows them all.
 */
const splitTargets = (direct: Configuration[], order: string[]) => {
  const rank = (c: Configuration) => {
    const i = order.indexOf(c.id);
    return i === -1 ? order.length : i;
  };
  const ordered = [...direct];
  ordered.sort((a, b) => rank(a) - rank(b));
  return ordered.length <= SHOWN_TARGETS + 1
    ? { hidden: [], shown: ordered }
    : {
        hidden: ordered.slice(SHOWN_TARGETS),
        shown: ordered.slice(0, SHOWN_TARGETS),
      };
};

/** "16 more agents", and how many of them are chosen. */
const moreLabel = (hidden: number, chosen: number) =>
  chosen === 0
    ? `${hidden} more agents`
    : `${hidden} more agents, ${chosen} chosen`;

/** "Adding 2 of 7" from the last progress event, or "Adding…" before the first. */
const progressText = (progress: ProgressEvent | undefined) => {
  if (progress === undefined) {
    return "Adding…";
  }
  return progress.total === undefined
    ? `Adding ${progress.current}`
    : `Adding ${progress.current} of ${progress.total}`;
};

const Divider = () => (
  <span aria-hidden className="bg-shell-divider h-6 w-px flex-none" />
);

/**
 * The floating action bar (DESIGN.md §2.2 "Overlays", §4 "Overlays", §8): how many cards are
 * selected, the install targets as round logos (at most five, the rest behind a "+n" button that
 * opens Choose agents, after the Universal stack, which is always on), and Add; while the install runs, its
 * progress and Cancel.
 */
export const ActionBar = ({
  count,
  direct,
  universal,
  targets,
  onToggleTarget,
  onClearTargets,
  adding,
  progress,
  onAdd,
  onCancel,
}: {
  /** How many cards are selected. */
  count: number;
  /** The configurations that take a placement, in inventory order. */
  direct: Configuration[];
  /** The configurations whose client reads the library. */
  universal: Configuration[];
  /** The ids of the chosen targets. */
  targets: string[];
  onToggleTarget: (id: string) => void;
  onClearTargets: () => void;
  /** The install runs. */
  adding: boolean;
  /** The last progress event of the run. */
  progress: ProgressEvent | undefined;
  onAdd: () => void;
  onCancel: () => void;
}) => {
  // The order is fixed when the bar opens and when the dialog closes, so a logo stays in place
  // while it is toggled.
  const [order, setOrder] = useState(() => targetOrder(direct, targets));
  const [picking, setPicking] = useState(false);
  const { shown, hidden } = splitTargets(direct, order);
  const hiddenChosen = hidden.filter((c) => targets.includes(c.id)).length;
  return (
    <div
      role="toolbar"
      aria-label="Selected skills"
      className="bottom-floating-bar animate-floating-bar rounded-floating-bar border-overlay-border bg-overlay-bg shadow-popover h-floating-bar absolute left-1/2 z-30 flex -translate-x-1/2 items-center gap-3 border pr-2 pl-5"
    >
      <output className="type-body text-text-primary font-medium whitespace-nowrap">
        {adding ? progressText(progress) : `${count} selected`}
      </output>
      <Divider />
      <span className="type-secondary text-text-muted whitespace-nowrap">
        Add to
      </span>
      <fieldset className="m-0 flex items-center gap-2 border-0 p-0">
        <legend className="sr-only">Add to</legend>
        {shown.map((c) => (
          <Tooltip key={c.id}>
            <TooltipTrigger asChild>
              <Toggle
                kind="logo"
                pressed={targets.includes(c.id)}
                disabled={adding}
                onPressedChange={() => {
                  onToggleTarget(c.id);
                }}
                aria-label={c.name}
              >
                <AgentLogo client={c} size={28} hover="none" decorative />
              </Toggle>
            </TooltipTrigger>
            <TooltipContent>{c.name}</TooltipContent>
          </Tooltip>
        ))}
        {universal.length > 0 && (
          <Toggle
            kind="logo"
            pressed
            onPressedChange={() => {
              // Universal clients read the library: every skill reaches them, whatever is chosen.
            }}
            aria-label="Universal"
            aria-description={universalTitle(universal.map((c) => c.name))}
          >
            <LogoStack
              clients={universal}
              size={28}
              hover="tooltip"
              decorative
            />
          </Toggle>
        )}
        {hidden.length > 0 && (
          <Button
            variant="secondary"
            size="sm"
            disabled={adding}
            aria-haspopup="dialog"
            aria-label={moreLabel(hidden.length, hiddenChosen)}
            onClick={() => {
              setPicking(true);
            }}
          >
            {`+${hidden.length}`}
            {hiddenChosen > 0 && (
              <span className="rounded-pill bg-agent-picker-badge-bg type-key-hint text-on-accent size-agent-picker-badge inline-flex items-center justify-center self-center">
                {hiddenChosen}
              </span>
            )}
          </Button>
        )}
      </fieldset>
      <AgentPickerDialog
        open={picking}
        onOpenChange={(open) => {
          setPicking(open);
          if (!open) {
            setOrder(targetOrder(direct, targets));
          }
        }}
        configurations={direct}
        targets={targets}
        onToggleTarget={onToggleTarget}
        onClearTargets={onClearTargets}
      />
      <Divider />
      {adding ? (
        <Button variant="ghost" size="sm" onClick={onCancel}>
          Cancel
        </Button>
      ) : (
        <Button variant="primary" onClick={onAdd}>
          {`Add ${count}`}
        </Button>
      )}
    </div>
  );
};
