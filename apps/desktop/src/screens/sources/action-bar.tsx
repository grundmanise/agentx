import type { Configuration } from "#/cli/events.gen.ts";
import { ProgressRim } from "#/components/progress-rim/progress-rim.tsx";
import type { RimRun } from "#/components/progress-rim/rim-progress.ts";
import { RunButton } from "#/components/run-button.tsx";
import { AgentTargets } from "#/screens/sources/agent-targets.tsx";

const Divider = () => (
  <span aria-hidden className="bg-shell-divider h-6 w-px flex-none" />
);

/**
 * The floating action bar (DESIGN.md §2.2 "Overlays", §4 "Overlays", §8): the install targets
 * (`AgentTargets`, DESIGN.md §6 "Agent picker") and Add, which counts the selected cards. While
 * the install runs, Add keeps its size and turns into its Stop (DESIGN.md §6 "Run button"), and
 * the install's progress runs on the bar's rim (DESIGN.md §8 "Progress rim").
 */
export const ActionBar = ({
  count,
  direct,
  universal,
  targets,
  onToggleTarget,
  onClearTargets,
  adding,
  rim,
  onRimSettled,
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
  /** The install runs, or its rim has not faded yet. */
  adding: boolean;
  /** The install the rim shows; undefined once it has faded. */
  rim: RimRun | undefined;
  onRimSettled: () => void;
  onAdd: () => void;
  onCancel: () => void;
}) => (
  <div
    role="toolbar"
    aria-label="Selected skills"
    className="bottom-floating-bar animate-floating-bar rounded-floating-bar border-overlay-border bg-overlay-bg shadow-popover h-floating-bar absolute left-1/2 z-30 flex -translate-x-1/2 items-center gap-3 border pr-2 pl-4"
  >
    <ProgressRim run={rim} onSettled={onRimSettled} />
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
    <RunButton
      label={`Add ${count}`}
      running={adding}
      stopLabel="Stop adding"
      onRun={onAdd}
      onStop={onCancel}
    />
  </div>
);
