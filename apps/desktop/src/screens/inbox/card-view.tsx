// One Inbox card (DESIGN.md §6 "Card (Inbox)"): a tone icon tile, the skill's name with a mono
// tag, one line of text, and on the right the action that settles it, with Keep before it on a
// card the user may leave as it is. The action buttons are those of the Skills banners
// (src/screens/skills/actions.tsx), so a card and a banner run the same command the same way.
// Remove on a "Source removed" card opens the Remove dialog of the Skills detail pane.
import { cva } from "class-variance-authority";
import { useState } from "react";

import type { Configuration } from "#/cli/events.gen.ts";
import { AlertIcon } from "#/components/icons/alert.tsx";
import { FolderIcon } from "#/components/icons/folder.tsx";
import { SkillsIcon } from "#/components/icons/skills.tsx";
import { Spinner } from "#/components/spinner.tsx";
import { Button } from "#/components/ui/button.tsx";
import type { Card, CardAction } from "#/screens/inbox/cards.ts";
import { hideCard } from "#/screens/inbox/hidden.ts";
import {
  AddSourceAgain,
  Adopt,
  Fork,
  Reconnect,
  Restore,
  TakeOver,
} from "#/screens/skills/actions.tsx";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { RemoveDialog, removeLabel } from "#/screens/skills/remove-dialog.tsx";
import { useSettling } from "#/store/settle.ts";

const tileVariants = cva(
  "rounded-card-tile size-card-tile flex flex-none items-center justify-center border",
  {
    variants: {
      tone: {
        attention:
          "border-tone-attention-border bg-tone-attention-bg text-tone-attention-fg",
        danger:
          "border-tone-danger-border bg-tone-danger-bg text-tone-danger-fg",
        neutral:
          "border-tone-neutral-border bg-tone-neutral-bg text-tone-neutral-fg",
        primary:
          "border-tone-primary-border bg-tone-primary-bg text-tone-primary-fg",
        yours: "border-tone-yours-border bg-tone-yours-bg text-tone-yours-fg",
      },
    },
  }
);

const cardVariants = cva(
  "rounded-card border-card-border bg-card-bg gap-card-header flex items-center border p-4.5",
  {
    variants: {
      leaving: {
        false: "",
        true: "animate-card-resolve pointer-events-none",
      },
    },
  }
);

/** The icon of a card: a folder for a repair, the skills mark for an adoptable skill, else the alert. */
const TileIcon = ({ card }: { card: Card }) => {
  if (card.slot === "folder-missing" || card.slot === "in-the-way") {
    return <FolderIcon size="lg" />;
  }
  return card.slot === "adoptable" ? (
    <SkillsIcon size="lg" />
  ) : (
    <AlertIcon size="lg" />
  );
};

/** Remove, a danger button that opens the Remove dialog of the Skills detail pane for the skill. */
const RemoveAction = ({
  row,
  configurations,
}: {
  row: SkillRow;
  configurations: Configuration[];
}) => {
  const [open, setOpen] = useState(false);
  const pending = useSettling(removeLabel(row));
  return (
    <>
      <Button
        variant="danger"
        size="sm"
        disabled={pending}
        aria-busy={pending ? true : undefined}
        onClick={() => {
          setOpen(true);
        }}
      >
        {pending && <Spinner />}
        Remove
      </Button>
      <RemoveDialog
        row={row}
        configurations={configurations}
        open={open}
        onOpenChange={setOpen}
      />
    </>
  );
};

const Actions = ({
  action,
  configurations,
}: {
  action: CardAction;
  configurations: Configuration[];
}) => {
  switch (action.kind) {
    case "adopt": {
      return <Adopt name={action.name} variant="secondary" />;
    }
    case "reconnect": {
      return (
        <Reconnect
          row={action.row}
          placement={action.placement}
          agent={action.agent}
          variant="secondary"
        />
      );
    }
    case "restore": {
      return <Restore name={action.name} variant="secondary" />;
    }
    case "take-over": {
      return (
        <TakeOver name={action.name} path={action.path} variant="secondary" />
      );
    }
    case "source-removed": {
      return (
        <>
          <RemoveAction row={action.row} configurations={configurations} />
          <AddSourceAgain source={action.source} variant="secondary" />
        </>
      );
    }
    case "fork": {
      return <Fork name={action.name} variant="secondary" />;
    }
    default: {
      return null;
    }
  }
};

export const CardView = ({
  card,
  configurations,
  leaving,
}: {
  card: Card;
  configurations: Configuration[];
  /** The card's state is gone: it plays the resolve animation and is then removed. */
  leaving: boolean;
}) => (
  <li className="flex">
    <article
      aria-label={`${card.name} · ${card.tag}`}
      data-slot-kind={card.slot}
      data-leaving={leaving ? "true" : undefined}
      className={cardVariants({ leaving })}
    >
      <span className={tileVariants({ tone: card.tone })}>
        <TileIcon card={card} />
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <div className="flex min-w-0 items-baseline gap-2">
          <h2 className="type-card-title text-card-title m-0 truncate">
            {card.name}
          </h2>
          <span className="type-technical-sm text-card-tag truncate">
            {card.tag}
          </span>
        </div>
        <p className="type-secondary text-card-text m-0 text-pretty">
          {card.text}
        </p>
      </div>
      <div className="gap-button flex flex-none items-center">
        {card.keep !== undefined && (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              hideCard(card.id);
            }}
          >
            {card.keep}
          </Button>
        )}
        <Actions action={card.action} configurations={configurations} />
      </div>
    </article>
  </li>
);
