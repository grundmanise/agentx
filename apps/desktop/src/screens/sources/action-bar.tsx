import type { Configuration } from "#/cli/events.gen.ts";
import { FloatingBar, FloatingBarDivider } from "#/components/floating-bar.tsx";
import { ProgressRim } from "#/components/progress-rim/progress-rim.tsx";
import type { RimRun } from "#/components/progress-rim/rim-progress.ts";
import { RunButton } from "#/components/run-button.tsx";
import { AgentTargets } from "#/screens/sources/agent-targets.tsx";

/**
 * The floating action bar of Sources (DESIGN.md §8): the install targets (`AgentTargets`,
 * DESIGN.md §6 "Agent picker") and Add, which counts the selected cards. While the install runs,
 * Add keeps its size and turns into its Stop (DESIGN.md §6 "Run button"), and the install's
 * progress runs on the bar's rim (DESIGN.md §8 "Progress rim").
 */
interface ActionBarProps {
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
}

const ActionBar = ({
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
}: ActionBarProps) => (
  <>
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
    <FloatingBarDivider />
    <RunButton
      label={`Add ${count}`}
      running={adding}
      stopLabel="Stop adding"
      track="sources.add-skills"
      onRun={onAdd}
      onStop={onCancel}
    />
  </>
);

/** The action bar while `shown`, and through its slide back down. */
export const FloatingActionBar = ({
  shown,
  ...props
}: ActionBarProps & { shown: boolean }) => (
  <FloatingBar shown={shown} label="Selected skills" values={props}>
    {(values) => <ActionBar {...values} />}
  </FloatingBar>
);
