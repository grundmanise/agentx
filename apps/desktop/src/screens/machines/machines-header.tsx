import { useId } from "react";
import type { ReactNode } from "react";

import { Segmented } from "#/components/filter-controls.tsx";
import { Spinner } from "#/components/ui/spinner.tsx";
import { plural } from "#/lib/plural.ts";
import { cn } from "#/lib/utils.ts";
import { useMachinesView } from "#/screens/machines/view.ts";
import type { MachinesBy } from "#/screens/machines/view.ts";

const views: { value: MachinesBy; label: string }[] = [
  { label: "By machine", value: "machine" },
  { label: "By skill", value: "skill" },
];

/**
 * The head of the Machines screen, the same in both of its views: the title and how many
 * machines, with a spinner while the hub is read, and under them the switch between the views
 * once `by` says which one shows. The switch is as wide as the list column in both views, so it
 * stays where it is when the view changes; `children` follow it on its row.
 */
export const MachinesHeader = ({
  count,
  reading,
  by,
  children,
}: {
  /** How many machines the screen shows. */
  count: number;
  /** The hub is being read: a spinner turns beside the count. */
  reading: boolean;
  /** The view that shows; undefined while there is one machine, which has no other view. */
  by?: MachinesBy;
  /** What follows the switch, in a view as wide as the screen. */
  children?: ReactNode;
}) => {
  const titleId = useId();
  return (
    <>
      <div className="pt-split-header-top px-split-header flex items-baseline gap-3 pb-0">
        <h1 id={titleId} className="type-page-title text-text-primary m-0">
          Machines
        </h1>
        <span
          aria-label={plural(count, "machine")}
          className="type-technical text-text-caption"
        >
          {count}
        </span>
        {reading && (
          <span className="text-text-hint flex self-center">
            <Spinner />
          </span>
        )}
      </div>
      {by !== undefined && (
        <div className="pt-split-toolbar-top pb-split-toolbar-bottom flex items-center">
          {/* In the list column, the column's own width; in a wide view, the same width, its
              divider's 1px with it. */}
          <div
            className={cn(
              "px-split-toolbar",
              children === undefined
                ? "min-w-0 flex-1"
                : "w-split-list flex-none border-r border-transparent"
            )}
          >
            <Segmented
              options={views}
              value={by}
              onChange={(next) => {
                useMachinesView.setState({ by: next });
              }}
              labelledBy={titleId}
              surface="page"
            />
          </div>
          {children}
        </div>
      )}
    </>
  );
};
