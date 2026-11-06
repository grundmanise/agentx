import { cva } from "class-variance-authority";
import { useEffect, useId, useRef } from "react";
import type { ReactNode } from "react";

// The box is the list item: the row's padding, radius, hover and selection, and the focus ring
// while the row button inside it has the focus. It isolates a stacking context so that the row
// button's ::after, which covers the whole box beneath its content, takes a click on the padding.
const boxVariants = cva(
  "rounded-row py-row pr-row-right pl-row-left gap-row-inline has-[>button:focus-visible]:outline-shell-focus-ring relative isolate flex w-full items-start has-[>button:focus-visible]:outline-2 has-[>button:focus-visible]:outline-offset-2",
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
 * A row of a split list (DESIGN.md §4 "Lists and rows", §6 "Skill row (Skills list)"): the list
 * item is the box, with `row.padding`, `row.radius`, `row.hover`; the selected row has
 * `row.selected-bg` and a `row.selected-bar` at its left edge, and comes into view when it is
 * selected from elsewhere. Inside the box the row button takes the remaining width; selecting a
 * row is a click anywhere in the box but on `aside`, or Enter on the row button. The row is named
 * by `label` alone; what else it shows is read as its description. `aside` is a control of its
 * own after the row button, inside the box and after it in the tab order, such as the Adopt pill:
 * a click on it does not select the row.
 */
export const ListRow = ({
  label,
  description,
  selected,
  onSelect,
  aside,
  children,
}: {
  /** The row's accessible name: the name of what it shows. */
  label: string;
  /** The row's accessible description: its state, markers and description line, unseen. */
  description?: string;
  selected: boolean;
  onSelect: () => void;
  /** A control of its own after the row button, inside the box, such as the Adopt pill. */
  aside?: ReactNode;
  children: ReactNode;
}) => {
  const ref = useRef<HTMLLIElement>(null);
  const descriptionId = useId();
  useEffect(() => {
    if (selected) {
      ref.current?.scrollIntoView({ block: "nearest" });
    }
  }, [selected]);
  return (
    <li ref={ref} className={boxVariants({ selected })}>
      <button
        type="button"
        aria-label={label}
        aria-describedby={description === undefined ? undefined : descriptionId}
        aria-current={selected ? "true" : undefined}
        onClick={onSelect}
        className="gap-row after:rounded-row flex min-w-0 flex-1 items-start text-left after:absolute after:inset-0 after:-z-10 focus-visible:outline-none"
      >
        {children}
        {description !== undefined && (
          <span id={descriptionId} className="sr-only">
            {description}
          </span>
        )}
      </button>
      {aside !== undefined && (
        <span className="flex h-5 flex-none items-center">{aside}</span>
      )}
    </li>
  );
};
