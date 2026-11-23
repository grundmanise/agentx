// The skills of a listing as rows to check, for the Add skill dialog (`src/screens/skills/add-skill-dialog.tsx`).
import { useId } from "react";
import type { ReactNode } from "react";

import { InlineMarkdown } from "#/components/inline-markdown.tsx";
import { Checkbox } from "#/components/ui/checkbox.tsx";
import { statusLabels } from "#/screens/sources/projection.ts";
import type { SkillCard } from "#/screens/sources/projection.ts";
import { statusVariants } from "#/screens/sources/skill-list.tsx";

/**
 * One skill as a row of the Add skill dialog's list: the round checkbox before the name, the
 * description under the name. A skill this machine holds already has an empty slot in place of
 * the checkbox, so the names keep one column, and its status in the card's colour at the right.
 * A selectable row is the whole hit target.
 */
const SkillRow = ({
  card,
  checked,
  onToggle,
}: {
  card: SkillCard;
  checked: boolean;
  onToggle: () => void;
}) => {
  const id = useId();
  const descriptionId = useId();
  // The first line opens with a 16 slot, `gap-2` before the name, as in the Update skills
  // dialog; the description starts under the name.
  const text = (lead: ReactNode) => (
    <span className="flex min-w-0 flex-1 flex-col gap-0.5">
      <span className="flex min-w-0 items-center gap-2">
        {lead}
        <span className="type-body text-text-primary truncate">
          {card.name}
        </span>
      </span>
      {card.description !== "" && (
        <span
          id={descriptionId}
          className="type-secondary text-text-muted line-clamp-2 pl-6"
        >
          <InlineMarkdown text={card.description} />
        </span>
      )}
    </span>
  );
  if (card.status !== undefined) {
    return (
      <li title={card.title} className="flex items-center gap-3 px-3 py-2">
        {text(<span aria-hidden className="size-checkbox flex-none" />)}
        <span className={statusVariants({ status: card.status })}>
          {statusLabels[card.status]}
        </span>
      </li>
    );
  }
  return (
    <li>
      <label
        htmlFor={id}
        className="rounded-add-skill-row hover:bg-add-skill-row-hover has-focus-visible:outline-shell-focus-ring flex cursor-pointer items-center gap-3 px-3 py-2 has-focus-visible:outline-2 has-focus-visible:-outline-offset-2"
      >
        {text(
          <Checkbox
            id={id}
            aria-label={card.name}
            aria-describedby={
              card.description === "" ? undefined : descriptionId
            }
            checked={checked}
            onCheckedChange={onToggle}
          />
        )}
      </label>
    </li>
  );
};

/** The skills of a listing as a list of rows to check, which scrolls past `add-skill.list-max-height`. */
export const SkillRows = ({
  cards,
  selected,
  onToggle,
}: {
  cards: SkillCard[];
  /** The keys of the selected cards. */
  selected: ReadonlySet<string>;
  onToggle: (card: SkillCard) => void;
}) => (
  <ul
    aria-label="Skills"
    className="max-h-add-skill-list m-0 flex list-none flex-col overflow-auto p-0"
  >
    {cards.map((card) => (
      <SkillRow
        key={card.key}
        card={card}
        checked={selected.has(card.key)}
        onToggle={() => {
          onToggle(card);
        }}
      />
    ))}
  </ul>
);
