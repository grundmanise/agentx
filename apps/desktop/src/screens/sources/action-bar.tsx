import type { Configuration, ProgressEvent } from "#/cli/events.gen.ts";
import { Button } from "#/components/ui/button.tsx";
import { AgentTargets } from "#/screens/sources/agent-targets.tsx";

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
 * selected, the install targets (`AgentTargets`, DESIGN.md §6 "Agent picker"), and Add; while the
 * install runs, its progress and Cancel.
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
}) => (
  <div
    role="toolbar"
    aria-label="Selected skills"
    className="bottom-floating-bar animate-floating-bar rounded-floating-bar border-overlay-border bg-overlay-bg shadow-popover h-floating-bar absolute left-1/2 z-30 flex -translate-x-1/2 items-center gap-3 border pr-2 pl-5"
  >
    <output className="type-body text-text-primary font-medium whitespace-nowrap">
      {adding ? progressText(progress) : `${count} selected`}
    </output>
    <Divider />
    <AgentTargets
      direct={direct}
      universal={universal}
      targets={targets}
      onToggleTarget={onToggleTarget}
      onClearTargets={onClearTargets}
      disabled={adding}
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
