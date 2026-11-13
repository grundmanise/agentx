import { useId } from "react";

import { Checkbox } from "#/components/ui/checkbox.tsx";
import { selectAllLabel } from "#/screens/sources/projection.ts";

/**
 * The row above the cards: one checkbox that selects every selectable card, checked, unchecked or
 * mixed as the selection is, and that clears the selection once every card is selected; at the
 * right, how many cards this machine already holds.
 */
export const SelectAllRow = ({
  selected,
  total,
  added,
  onSelectAll,
  onClear,
}: {
  /** How many of the selectable cards are selected. */
  selected: number;
  /** How many cards can be selected. */
  total: number;
  /** How many cards cannot be selected. */
  added: number;
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
    <div className="flex items-center gap-3">
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
      <span className="flex-1" />
      {added > 0 && (
        <span className="type-secondary text-text-hint">{`${added} already added`}</span>
      )}
    </div>
  );
};
