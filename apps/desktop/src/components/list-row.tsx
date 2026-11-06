import { cva } from "class-variance-authority";
import { useEffect, useId, useRef } from "react";
import type { ReactNode } from "react";

const rowVariants = cva(
  "rounded-row py-row pr-row-right pl-row-left gap-row relative flex w-full items-start text-left",
  {
    variants: {
      selected: {
        false: "hover:bg-row-hover",
        true: "bg-row-selected-bg before:bg-row-selected-bar before:rounded-pill before:absolute before:top-2.5 before:bottom-2.5 before:left-0 before:w-0.5",
      },
    },
  }
);

/**
 * A row of a split list (DESIGN.md §4 "Lists and rows"): `row.padding`, `row.radius`, `row.hover`;
 * the selected row has `row.selected-bg` and a `row.selected-bar` at its left edge, and comes into
 * view when it is selected from elsewhere. Selecting a row is a click, or Enter on the row. The
 * row is named by `label` alone; what else it shows is read as its description.
 */
export const ListRow = ({
  label,
  description,
  selected,
  onSelect,
  children,
}: {
  /** The row's accessible name: the name of what it shows. */
  label: string;
  /** The row's accessible description: its state, markers and description line, unseen. */
  description?: string;
  selected: boolean;
  onSelect: () => void;
  children: ReactNode;
}) => {
  const ref = useRef<HTMLButtonElement>(null);
  const descriptionId = useId();
  useEffect(() => {
    if (selected) {
      ref.current?.scrollIntoView({ block: "nearest" });
    }
  }, [selected]);
  return (
    <button
      ref={ref}
      type="button"
      aria-label={label}
      aria-describedby={description === undefined ? undefined : descriptionId}
      aria-current={selected ? "true" : undefined}
      onClick={onSelect}
      className={rowVariants({ selected })}
    >
      {children}
      {description !== undefined && (
        <span id={descriptionId} className="sr-only">
          {description}
        </span>
      )}
    </button>
  );
};
