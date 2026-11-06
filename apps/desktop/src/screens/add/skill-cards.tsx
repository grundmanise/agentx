import { cva } from "class-variance-authority";
import { useId } from "react";

import { Toggle } from "#/components/ui/toggle.tsx";
import { statusLabels } from "#/screens/add/projection.ts";
import type { CardGroup, SkillCard } from "#/screens/add/projection.ts";

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
  const description = [status, card.description]
    .filter((part) => part !== undefined && part !== "")
    .join(". ");
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
