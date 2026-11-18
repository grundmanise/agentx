import { useId } from "react";

import { Checkbox } from "#/components/ui/checkbox.tsx";
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
}: {
  /** How many of the selectable cards are selected. */
  selected: number;
  /** How many cards can be selected. */
  total: number;
  onSelectAll: () => void;
  onClear: () => void;
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
    <div className="flex items-center">
      <label htmlFor={id} className="flex items-center gap-2.5">
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
