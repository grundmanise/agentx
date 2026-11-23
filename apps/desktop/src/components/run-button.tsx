import { useState } from "react";

import { Spinner } from "#/components/ui/spinner.tsx";

/**
 * A primary button that runs a command and stops it (DESIGN.md §6 "Run button"). While the command
 * runs it keeps its size: the label stays in place, hidden, under a `Spinner`, and pointing at it
 * or focusing it shows Stop, which a click does. The pointer that clicked it shows Stop only once it
 * has left and come back, so the spinner is what a click shows. Its name says what it does at each
 * moment.
 */
export const RunButton = ({
  label,
  running,
  stopLabel = "Stop",
  onRun,
  onStop,
}: {
  label: string;
  running: boolean;
  /** The name the button has while it runs, for a screen reader; it shows Stop. */
  stopLabel?: string;
  onRun: () => void;
  onStop: () => void;
}) => {
  // The pointer that started the run has not left the button yet.
  const [clicked, setClicked] = useState(false);
  return (
    <button
      type="button"
      data-running={running ? "" : undefined}
      data-stoppable={running && !clicked ? "" : undefined}
      aria-busy={running}
      aria-label={running ? stopLabel : undefined}
      onClick={(event) => {
        if (running) {
          // The second click of a double-click on Add lands on the spinner, not on Stop.
          if (!(clicked && event.detail > 1)) {
            onStop();
          }
          return;
        }
        // A key's click has no pointer on the button.
        setClicked(event.detail > 0);
        onRun();
      }}
      onPointerLeave={() => {
        setClicked(false);
      }}
      className="group rounded-button bg-button-primary-bg text-button-primary-text hover:bg-button-primary-hover type-button grid h-9.5 shrink-0 items-center justify-items-center px-4 font-semibold whitespace-nowrap"
    >
      <span className="col-start-1 row-start-1 group-data-running:invisible">
        {label}
      </span>
      {running && (
        <span className="col-start-1 row-start-1 group-focus-visible:invisible group-data-stoppable:group-hover:invisible">
          <Spinner size="sm" />
        </span>
      )}
      {/* Always laid out, so the button is as wide as its wider word from the start. */}
      <span
        aria-hidden
        className="invisible col-start-1 row-start-1 group-data-running:group-focus-visible:visible group-data-stoppable:group-hover:visible"
      >
        Stop
      </span>
    </button>
  );
};
