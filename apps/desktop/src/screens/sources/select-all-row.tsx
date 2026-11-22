import { useId } from "react";

import { Checkbox } from "#/components/ui/checkbox.tsx";
import { cn } from "#/lib/utils.ts";
import { selectAllLabel } from "#/screens/sources/projection.ts";

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
  /**
   * Where the row sits, and which side of its label the checkbox takes. `start`: the checkbox
   * before its label, at the left, heading a column of checkboxes (the Add skill and Update skills
   * dialogs). `end`: the label, then the checkbox, at the right, the checkbox centred over the
   * round checks at the end of the Sources cards.
   */
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
    <div
      className={cn(
        "flex items-center",
        align === "end" && "justify-end pr-4.5"
      )}
    >
      <label
        htmlFor={id}
        className={cn(
          "flex items-center gap-2.5",
          align === "end" && "flex-row-reverse"
        )}
      >
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
