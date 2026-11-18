import type { Configuration } from "#/cli/events.gen.ts";
import { Button } from "#/components/ui/button.tsx";
import { AgentTargets } from "#/screens/sources/agent-targets.tsx";

/** How far an install got: the skills it started, out of the skills it installs. */
export interface AddProgress {
  started: number;
  total: number;
}

/**
 * "Adding 2 of 7": skills, not steps. The CLI reports three steps per skill and one rescan, so
 * its own `current` and `total` count steps (docs/spec/cli-contract.md, "After the install").
 */
const progressText = (progress: AddProgress | undefined) =>
  progress === undefined || progress.started === 0
    ? "Adding…"
    : `Adding ${progress.started} of ${progress.total}`;

const Divider = () => (
  <span aria-hidden className="bg-shell-divider h-6 w-px flex-none" />
);

/**
 * The floating action bar (DESIGN.md §2.2 "Overlays", §4 "Overlays", §8): the install targets
 * (`AgentTargets`, DESIGN.md §6 "Agent picker") and Add, which counts the selected cards; while
 * the install runs, its progress first and Cancel.
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
  /** The skills the run started, out of those it installs. */
  progress: AddProgress | undefined;
  onAdd: () => void;
  onCancel: () => void;
}) => (
  <div
    role="toolbar"
    aria-label="Selected skills"
    className="bottom-floating-bar animate-floating-bar rounded-floating-bar border-overlay-border bg-overlay-bg shadow-popover h-floating-bar absolute left-1/2 z-30 flex -translate-x-1/2 items-center gap-3 border pr-2 pl-4"
  >
    {adding && (
      <>
        <output className="type-body text-text-primary font-medium whitespace-nowrap">
          {progressText(progress)}
        </output>
        <Divider />
      </>
    )}
    <AgentTargets
      skills={count}
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
