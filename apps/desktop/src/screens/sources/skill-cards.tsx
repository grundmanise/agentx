import { cva } from "class-variance-authority";
import { useId } from "react";

import { Checkbox } from "#/components/ui/checkbox.tsx";
import { Toggle } from "#/components/ui/toggle.tsx";
import { statusLabels } from "#/screens/sources/projection.ts";
import type { CardGroup, SkillCard } from "#/screens/sources/projection.ts";

/** The round check at the right of a selectable card: a ring, filled with the check when selected. */
const CheckMark = ({ checked }: { checked: boolean }) => (
  <svg
    viewBox="0 0 20 20"
    aria-hidden
    focusable="false"
    className="block size-5 flex-none"
  >
    {checked ? (
      <>
        <circle cx="10" cy="10" r="10" className="fill-add-card-check-bg" />
        <path
          d="M6 10.25l2.75 2.75 5.25-6"
          fill="none"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          className="stroke-on-accent"
        />
      </>
    ) : (
      <circle
        cx="10"
        cy="10"
        r="9.25"
        fill="none"
        strokeWidth="1.5"
        className="stroke-add-card-check-ring"
      />
    )}
  </svg>
);

const statusVariants = cva("type-secondary flex-none font-medium", {
  variants: {
    status: {
      added: "text-add-card-status-added",
      forked: "text-add-card-status-forked",
      "name-in-use": "text-add-card-status-in-use",
      "on-disk": "text-add-card-status-disk",
    },
  },
});

/** What a card or row is described by: its status, if any, then its description. */
const cardDescription = (card: SkillCard) =>
  [
    card.status === undefined ? undefined : statusLabels[card.status],
    card.description,
  ]
    .filter((part) => part !== undefined && part !== "")
    .join(". ");

/**
 * One skill of a source as a card (DESIGN.md §2.2 "Cards and banners", `add-card.*`): its name,
 * its description and the round check, or, for a skill this machine holds already, its status in
 * place of the check. The card is named by the skill alone; its status and description are read
 * as its description.
 */
const SkillCardItem = ({
  card,
  selected,
  onToggle,
}: {
  card: SkillCard;
  selected: boolean;
  onToggle: () => void;
}) => {
  const descriptionId = useId();
  const status =
    card.status === undefined ? undefined : statusLabels[card.status];
  const description = cardDescription(card);
  return (
    <Toggle
      kind="card"
      pressed={selected}
      disabled={card.status !== undefined}
      onPressedChange={onToggle}
      aria-label={card.name}
      aria-describedby={description === "" ? undefined : descriptionId}
      title={card.title}
    >
      <span className="flex min-w-0 flex-1 flex-col gap-1.5">
        <span className="type-add-card-name text-text-primary break-all">
          {card.name}
        </span>
        {card.description !== "" && (
          <span className="type-add-card-desc text-text-muted text-pretty">
            {card.description}
          </span>
        )}
      </span>
      {card.status === undefined ? (
        <CheckMark checked={selected} />
      ) : (
        <span className={statusVariants({ status: card.status })}>
          {status}
        </span>
      )}
      {description !== "" && (
        <span id={descriptionId} className="sr-only">
          {description}
        </span>
      )}
    </Toggle>
  );
};

/**
 * The 3-column grid of skill cards, `add-card.gap` apart, one section per source. With
 * `captions`, each section opens with the source's mono caption, as a search answer does.
 */
export const SkillCards = ({
  groups,
  captions,
  selected,
  onToggle,
}: {
  groups: CardGroup[];
  captions: boolean;
  /** The keys of the selected cards. */
  selected: ReadonlySet<string>;
  onToggle: (card: SkillCard) => void;
}) => (
  <div className="flex flex-col gap-7">
    {groups.map((group) => (
      <section
        key={group.source}
        aria-label={group.label}
        className="flex flex-col gap-3"
      >
        {captions && (
          <h2 className="type-caption text-group-label m-0">{group.label}</h2>
        )}
        <ul className="gap-add-card m-0 grid list-none grid-cols-3 p-0">
          {group.cards.map((card) => (
            <li key={card.key} className="flex">
              <SkillCardItem
                card={card}
                selected={selected.has(card.key)}
                onToggle={() => {
                  onToggle(card);
                }}
              />
            </li>
          ))}
        </ul>
      </section>
    ))}
  </div>
);

/**
 * One skill as a row of the Add skill dialog's list: its name over its description, then the
 * round checkbox, or, for a skill this machine holds already, its status in the card's colour in
 * place of the checkbox. A selectable row is the whole hit target.
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
  const text = (
    <span className="flex min-w-0 flex-1 flex-col gap-0.5">
      <span className="type-body text-text-primary truncate">{card.name}</span>
      {card.description !== "" && (
        <span
          id={descriptionId}
          className="type-secondary text-text-muted line-clamp-2"
        >
          {card.description}
        </span>
      )}
    </span>
  );
  if (card.status !== undefined) {
    return (
      <li title={card.title} className="flex items-center gap-3 px-3 py-2">
        {text}
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
        {text}
        <Checkbox
          id={id}
          aria-label={card.name}
          aria-describedby={card.description === "" ? undefined : descriptionId}
          checked={checked}
          onCheckedChange={onToggle}
        />
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
