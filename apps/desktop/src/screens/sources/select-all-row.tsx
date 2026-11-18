import { cva } from "class-variance-authority";
import { useId } from "react";

import { Checkbox } from "#/components/ui/checkbox.tsx";
import { selectAllLabel } from "#/screens/sources/projection.ts";

// `start`: the checkbox before its label, at the left, heading a column of checkboxes (the Add
// skill and Update skills dialogs). `end`: the label, then the checkbox, at the right, the
// checkbox centred over the round checks at the end of the Sources cards.
const rowVariants = cva("flex items-center", {
  variants: {
    align: {
      end: "justify-end pr-4.5",
      start: "",
    },
  },
});

const labelVariants = cva("flex items-center gap-2.5", {
  variants: {
    align: {
      end: "flex-row-reverse",
      start: "",
    },
  },
});

/**
 * The row above the cards: one checkbox that selects every selectable card, checked, unchecked or
 * mixed as the selection is, and that clears the selection once every card is selected.
 */
export const SelectAllRow = ({
  selected,
  total,
  onSelectAll,
  onClear,
  align = "start",
}: {
  /** How many of the selectable cards are selected. */
  selected: number;
  /** How many cards can be selected. */
  total: number;
  onSelectAll: () => void;
  onClear: () => void;
  /** Where the row sits, and which side of its label the checkbox takes. */
  align?: "start" | "end";
}) => {
  const id = useId();
  const all = total > 0 && selected >= total;
  let checked: boolean | "indeterminate" = false;
  if (all) {
    checked = true;
  } else if (selected > 0) {
    checked = "indeterminate";
  }
  return (
    <div className={rowVariants({ align })}>
      <label htmlFor={id} className={labelVariants({ align })}>
        <Checkbox
          id={id}
          checked={checked}
          disabled={total === 0}
          onCheckedChange={() => {
            if (all) {
              onClear();
            } else {
              onSelectAll();
            }
          }}
        />
        <span className="type-secondary text-text-secondary">
          {selectAllLabel(selected, total)}
        </span>
      </label>
    </div>
  );
};
