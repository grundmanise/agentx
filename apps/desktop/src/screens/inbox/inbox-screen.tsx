// The Inbox: one page listing only what needs a decision, with the action that settles it on the
// card, and RECENT beside it. The cards come from the store alone (src/screens/inbox/cards.ts); a
// card leaves, with the resolve animation, once its state is gone from the store, never on a
// click. With nothing to decide the page reads "All clear." and points at Skills and Add.
import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router";

import { Button } from "#/components/ui/button.tsx";
import { CardView } from "#/screens/inbox/card-view.tsx";
import { numberWord, useInboxCards } from "#/screens/inbox/cards.ts";
import type { Card } from "#/screens/inbox/cards.ts";
import { Recent } from "#/screens/inbox/recent.tsx";
import { useAppStore } from "#/store/store.ts";

/** How long a resolved card stays while its animation plays (DESIGN.md §8, Card resolve). */
const RESOLVE_MS = 280;

interface Shown {
  card: Card;
  leaving: boolean;
}

interface Leaving {
  card: Card;
  /** Where the card was in the list, so it plays out in its place. */
  at: number;
}

interface ShownState {
  /** The cards last given, to tell which ones went. */
  cards: Card[];
  leaving: Leaving[];
}

/**
 * The cards to draw: the open ones, and for the moment a card whose state just went, that card as
 * it last was, playing out at its old place in the list. The headline and the badge count only the
 * open cards.
 */
const useShown = (cards: Card[]): Shown[] => {
  const [state, setState] = useState<ShownState>({ cards, leaving: [] });
  if (state.cards !== cards) {
    // Derived while rendering, so the card never disappears for a frame before it animates out.
    const ids = new Set(cards.map((c) => c.id));
    const kept = state.leaving.filter((l) => !ids.has(l.card.id));
    const gone = state.cards
      .map((card, at) => ({ at, card }))
      .filter(
        (g) => !ids.has(g.card.id) && !kept.some((l) => l.card.id === g.card.id)
      );
    setState({ cards, leaving: [...kept, ...gone] });
  }
  const { leaving } = state;
  // One timer per card that leaves, from when it starts to leave; none is cut short by the next.
  const timers = useRef(new Map<string, ReturnType<typeof setTimeout>>());
  useEffect(() => {
    for (const { card } of leaving) {
      if (!timers.current.has(card.id)) {
        timers.current.set(
          card.id,
          setTimeout(() => {
            timers.current.delete(card.id);
            setState((s) => ({
              ...s,
              leaving: s.leaving.filter((l) => l.card.id !== card.id),
            }));
          }, RESOLVE_MS)
        );
      }
    }
  }, [leaving]);
  useEffect(() => {
    const pending = timers.current;
    return () => {
      for (const timer of pending.values()) {
        clearTimeout(timer);
      }
    };
  }, []);
  const shown: Shown[] = cards.map((card) => ({ card, leaving: false }));
  for (const entry of leaving) {
    shown.splice(Math.min(entry.at, shown.length), 0, {
      card: entry.card,
      leaving: true,
    });
  }
  return shown;
};

/** The date line: the local date as "MONDAY, SEPTEMBER 28" (the caption style sets the case). */
const dateLine = () =>
  new Date().toLocaleDateString(undefined, {
    day: "numeric",
    month: "long",
    weekday: "long",
  });

const Headline = ({ count }: { count: number }) => (
  <h2 className="type-inbox-headline text-text-primary m-0 text-pretty">
    {count === 1 ? "One thing " : `${numberWord(count)} things `}
    <em className="text-text-muted italic">
      {count === 1 ? "needs you." : "need you."}
    </em>
  </h2>
);

const AllClear = () => {
  const navigate = useNavigate();
  return (
    <div className="flex flex-col items-start gap-6">
      <div className="flex flex-col gap-3">
        <h2 className="type-inbox-headline text-text-primary m-0">
          All clear.
        </h2>
        <p className="type-body text-text-muted m-0">
          Nothing needs you. We&apos;ll surface changes here.
        </p>
      </div>
      <div className="gap-button flex items-center">
        <Button
          variant="secondary"
          onClick={() => {
            void navigate("/skills");
          }}
        >
          Browse skills
        </Button>
        <Button
          variant="ghost"
          onClick={() => {
            void navigate("/add");
          }}
        >
          Add skills
        </Button>
      </div>
    </div>
  );
};

export const InboxScreen = () => {
  const cards = useInboxCards();
  const shown = useShown(cards);
  const configurations = useAppStore((s) => s.inventory?.configurations ?? []);
  return (
    <div className="pt-page-top px-page pb-page h-full overflow-auto">
      <h1 className="sr-only">Inbox</h1>
      <div className="max-w-page gap-inbox-columns flex items-start">
        <div className="flex min-w-0 flex-1 flex-col gap-10">
          <header className="flex flex-col gap-4">
            <p className="type-caption text-text-caption m-0">{dateLine()}</p>
            {cards.length === 0 ? (
              <AllClear />
            ) : (
              <Headline count={cards.length} />
            )}
          </header>
          {shown.length > 0 && (
            <ul
              aria-label="Needs you"
              className="gap-card m-0 flex list-none flex-col p-0"
            >
              {shown.map(({ card, leaving }) => (
                <CardView
                  key={card.id}
                  card={card}
                  configurations={configurations}
                  leaving={leaving}
                />
              ))}
            </ul>
          )}
        </div>
        <div className="w-inbox-recent flex-none pt-1">
          <Recent />
        </div>
      </div>
    </div>
  );
};
