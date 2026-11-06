// What the Inbox lists: one card per state that needs a decision, projected from the store and
// nothing else. Pure functions over the applied inventory, the adoption preview and the reconcile
// outcomes of serve's start, so that the page, the headline count and the sidebar badge read the
// same cards. Nothing here predicts: a card leaves when its state is gone from the store.
import { useMemo } from "react";

import type {
  AdoptionEvent,
  Configuration,
  InventoryEvent,
  LibraryPlacement,
  ReconcileEvent,
} from "#/cli/events.gen.ts";
import { useHidden } from "#/screens/inbox/hidden.ts";
import {
  projectRows,
  shortSource,
  toolName,
} from "#/screens/skills/projection.ts";
import type { ProjectionInput, SkillRow } from "#/screens/skills/projection.ts";
import { useAppStore } from "#/store/store.ts";

/**
 * The slots of the Inbox, in the order the cards come. The first three are filled by the changes
 * that build the update, review and publish cards; adding one there needs no reordering here.
 */
export const CARD_SLOTS = [
  "updates",
  "update-waiting",
  "not-published",
  "adoptable",
  "displaced",
  "folder-missing",
  "in-the-way",
  "source-removed",
  "upstream-removed",
] as const;

export type CardSlot = (typeof CARD_SLOTS)[number];

export type CardTone = "primary" | "attention" | "danger" | "yours" | "neutral";

/** What settles the card: one command each (src/screens/skills/actions.tsx). */
export type CardAction =
  | { kind: "adopt"; name: string }
  | {
      kind: "reconnect";
      row: SkillRow;
      placement: LibraryPlacement;
      agent: string;
    }
  | { kind: "restore"; name: string }
  | { kind: "take-over"; name: string; path: string }
  | { kind: "source-removed"; row: SkillRow; source: string }
  | { kind: "fork"; name: string };

export interface Card {
  /** Stable across inventories, so a card that stays is the same element and one that goes animates out. */
  id: string;
  slot: CardSlot;
  tone: CardTone;
  /** The skill the card is about. */
  name: string;
  /** The mono tag after the title: the agent, the tool, the source, or "your skill". */
  tag: string;
  text: string;
  action: CardAction;
  /** The ghost button that hides the card until the app restarts; absent when the card has none. */
  keep?: "Keep" | "Keep as is";
}

export interface CardsInput extends Omit<ProjectionInput, "inventory"> {
  /** The applied inventory; none before serve's first. */
  inventory: InventoryEvent | undefined;
  reconciles: ReconcileEvent[];
  hidden: Record<string, true>;
}

const agentName = (configurations: Configuration[], id: string) =>
  configurations.find((c) => c.id === id)?.name ?? id;

/** Adoptable: a skill another tool installed, which `adopt` could take over. */
const adoptable = (adoption: AdoptionEvent[]): Card[] =>
  adoption
    .filter((a) => a.state === "candidate")
    .map((a) => ({
      action: { kind: "adopt", name: a.name },
      id: `adoptable:${a.name}`,
      name: a.name,
      slot: "adoptable",
      tag: toolName(a),
      text: `Installed by the ${toolName(a)} from ${shortSource(a.source ?? "")}. Adopt it and agentx can update it.`,
      tone: "neutral",
    }));

/** Displaced: one card per skill and agent whose placement is not the one agentx keeps for it. */
const displaced = (rows: SkillRow[], configurations: Configuration[]): Card[] =>
  rows.flatMap((row) =>
    (row.entry?.placements ?? [])
      .filter((p) => p.mode !== "library" && p.kind !== p.mode)
      .map((placement) => {
        const agent = agentName(configurations, placement.configuration);
        return {
          action: { agent, kind: "reconnect", placement, row },
          id: `displaced:${row.key}:${placement.configuration}`,
          keep: "Keep",
          name: row.name,
          slot: "displaced",
          tag: agent,
          text: `Changes you make in your library won't reach ${agent} until you reconnect it.`,
          tone: "attention",
        } satisfies Card;
      })
  );

/** Folder missing and Something in the way: what serve found at its start and left as it is. */
const repairs = (reconciles: ReconcileEvent[]): Card[] =>
  reconciles.flatMap((r): Card[] => {
    if (r.outcome === "worktree missing") {
      return [
        {
          action: { kind: "restore", name: r.name },
          id: `folder-missing:${r.name}`,
          name: r.name,
          slot: "folder-missing",
          tag: "your skill",
          text: "Its folder is missing. agentx can restore it from your saved version; edits you had not published are gone.",
          tone: "attention",
        },
      ];
    }
    if (r.outcome === "adopt candidate" && r.path !== undefined) {
      return [
        {
          action: { kind: "take-over", name: r.name, path: r.path },
          id: `in-the-way:${r.name}`,
          keep: "Keep",
          name: r.name,
          slot: "in-the-way",
          tag: "your skill",
          text: `A different folder is where ${r.name} should be: ${r.path}. agentx can take it over; its files become unpublished edits of ${r.name}.`,
          tone: "attention",
        },
      ];
    }
    return [];
  });

/** Source removed: the skill's source is gone from this machine, so it gets no updates. */
const sourceRemoved = (rows: SkillRow[]): Card[] =>
  rows
    .filter((row) => row.entry?.drift?.includes("source removed") ?? false)
    .map((row) => {
      const source = row.entry?.source ?? "";
      return {
        action: { kind: "source-removed", row, source },
        id: `source-removed:${row.key}`,
        name: row.name,
        slot: "source-removed",
        tag: shortSource(source),
        text: "Its source was removed, so it won't get updates.",
        tone: "danger",
      };
    });

/** Removed from its source: the source no longer holds the skill, which still works as it is. */
const upstreamRemoved = (rows: SkillRow[]): Card[] =>
  rows
    .filter((row) => row.entry?.drift?.includes("upstream removed") ?? false)
    .map((row) => ({
      action: { kind: "fork", name: row.name },
      id: `upstream-removed:${row.key}`,
      keep: "Keep as is",
      name: row.name,
      slot: "upstream-removed",
      tag: shortSource(row.entry?.source ?? ""),
      text: "Removed from its source. It still works but won't get updates.",
      tone: "danger",
    }));

const byName = (a: Card, b: Card) => a.name.localeCompare(b.name);

/** The cards, in slot order and by name within a slot, the hidden ones left out. */
export const projectCards = (input: CardsInput): Card[] => {
  const rows =
    input.inventory === undefined
      ? []
      : projectRows({
          adoption: input.adoption,
          inventory: input.inventory,
          updates: input.updates,
        });
  const configurations = input.inventory?.configurations ?? [];
  const bySlot: Record<CardSlot, Card[]> = {
    adoptable: adoptable(input.adoption),
    displaced: displaced(rows, configurations),
    "folder-missing": repairs(input.reconciles).filter(
      (c) => c.slot === "folder-missing"
    ),
    "in-the-way": repairs(input.reconciles).filter(
      (c) => c.slot === "in-the-way"
    ),
    "not-published": [],
    "source-removed": sourceRemoved(rows),
    "update-waiting": [],
    updates: [],
    "upstream-removed": upstreamRemoved(rows),
  };
  return CARD_SLOTS.flatMap((slot) => {
    const cards = [...bySlot[slot]];
    cards.sort(byName);
    return cards.filter((c) => !Object.hasOwn(input.hidden, c.id));
  });
};

/** The cards the Inbox shows now, from the store. */
export const useInboxCards = (): Card[] => {
  const inventory = useAppStore((s) => s.inventory);
  const adoption = useAppStore((s) => s.adoption);
  const reconciles = useAppStore((s) => s.reconciles);
  const updates = useAppStore((s) => s.updates);
  const hidden = useHidden();
  return useMemo(
    () => projectCards({ adoption, hidden, inventory, reconciles, updates }),
    [adoption, hidden, inventory, reconciles, updates]
  );
};

const NUMBER_WORDS = [
  "No",
  "One",
  "Two",
  "Three",
  "Four",
  "Five",
  "Six",
  "Seven",
  "Eight",
  "Nine",
];

/** The headline's count, spelled out up to nine; more reads "Nine" for now. */
export const numberWord = (n: number) =>
  NUMBER_WORDS[Math.min(n, NUMBER_WORDS.length - 1)] ?? "Nine";
