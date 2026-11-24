import { useState } from "react";

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
  leaving = false,
  onLeft,
}: ActionBarProps & {
  /** The bar slides down and out, showing what it last showed. */
  leaving?: boolean;
  /** The bar has slid out. */
  onLeft?: () => void;
}) => (
  <div
    role="toolbar"
    aria-label="Selected skills"
    data-leaving={leaving ? "" : undefined}
    onAnimationEnd={(event) => {
      // Its own slide, not one of its children's animations.
      if (leaving && event.target === event.currentTarget) {
        onLeft?.();
      }
    }}
    className="bottom-floating-bar animate-floating-bar data-leaving:animate-floating-bar-out rounded-floating-bar border-overlay-border bg-overlay-bg shadow-popover h-floating-bar absolute left-1/2 z-30 flex -translate-x-1/2 items-center gap-3 border pr-2 pl-4 data-leaving:pointer-events-none"
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
      track="sources.add-skills"
      onRun={onAdd}
      onStop={onCancel}
    />
  </div>
);

const sameProps = (a: ActionBarProps, b: ActionBarProps) =>
  Object.keys(b).length === Object.keys(a).length &&
  Object.entries(b).every(([key, value]) =>
    Object.is(Reflect.get(a, key), value)
  );

/**
 * The floating action bar while `shown`, sliding up from below the window's edge, and through
 * its slide back down (DESIGN.md §4 "Overlays"). While it leaves it shows what it last
 * showed, so its count does not drop to 0 on the way out.
 */
export const FloatingActionBar = ({
  shown,
  ...props
}: ActionBarProps & { shown: boolean }) => {
  const [last, setLast] = useState(props);
  const [mounted, setMounted] = useState(shown);
  // Kept while shown, to show on the way out. The rest object is new each render, so compare
  // its values, or each render would set it again.
  if (shown && !sameProps(last, props)) {
    setLast(props);
  }
  if (shown && !mounted) {
    setMounted(true);
  }
  if (!mounted) {
    return null;
  }
  // It goes once its slide out ends.
  return shown ? (
    <ActionBar {...props} />
  ) : (
    <ActionBar
      {...last}
      leaving
      onLeft={() => {
        setMounted(false);
      }}
    />
  );
};
