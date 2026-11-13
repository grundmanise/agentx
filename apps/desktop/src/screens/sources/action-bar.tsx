import type { Configuration, ProgressEvent } from "#/cli/events.gen.ts";
import { AgentLogo, LogoStack } from "#/components/agent-logo.tsx";
import { Button } from "#/components/ui/button.tsx";
import { Toggle } from "#/components/ui/toggle.tsx";
import { universalTitle } from "#/screens/sources/projection.ts";

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
 * selected, the install targets as round logos with the Universal stack always on, and Add; while
 * the install runs, its progress and Cancel.
 */
export const ActionBar = ({
  count,
  direct,
  universal,
  targets,
  onToggleTarget,
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
    <span className="type-secondary text-text-muted whitespace-nowrap">
      Add to
    </span>
    <fieldset className="m-0 flex items-center gap-2 border-0 p-0">
      <legend className="sr-only">Add to</legend>
      {direct.map((c) => (
        <Toggle
          key={c.id}
          kind="logo"
          pressed={targets.includes(c.id)}
          disabled={adding}
          onPressedChange={() => {
            onToggleTarget(c.id);
          }}
          aria-label={c.name}
          title={c.name}
        >
          <AgentLogo client={c} size={28} decorative />
        </Toggle>
      ))}
      {universal.length > 0 && (
        <Toggle
          kind="logo"
          pressed
          onPressedChange={() => {
            // Universal clients read the library: every skill reaches them, whatever is chosen.
          }}
          aria-label="Universal"
          title={universalTitle(universal.map((c) => c.name))}
        >
          <LogoStack clients={universal} size={28} decorative />
        </Toggle>
      )}
    </fieldset>
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
