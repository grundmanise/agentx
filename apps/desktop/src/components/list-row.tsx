import { cva } from "class-variance-authority";
import { useEffect, useRef } from "react";
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
 * view when it is selected from elsewhere. Selecting a row is a click, or Enter on the row.
 */
export const ListRow = ({
  label,
  selected,
  onSelect,
  children,
}: {
  /** The row's accessible name: the name of what it shows. */
  label: string;
  selected: boolean;
  onSelect: () => void;
  children: ReactNode;
}) => {
  const ref = useRef<HTMLButtonElement>(null);
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
      aria-current={selected ? "true" : undefined}
      onClick={onSelect}
      className={rowVariants({ selected })}
    >
      {children}
    </button>
  );
};
