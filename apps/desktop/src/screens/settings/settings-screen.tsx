// The Settings page (DESIGN.md §6 "Settings"): every machine setting on one page, in cards. Each
// row reads a store slice and writes only through the CLI command named beside it; the app writes
// no file of the CLI's. The settings the app keeps itself are Editor and Updates, app preferences
// (ADR 0013): the page reads them in one read each time it shows (`src/lib/app-settings.ts`).
//
// The page is the title and the cards below it. A card is a component that renders
// `SettingsCard` (`section.tsx`) with its title and its rows, or nothing when it has nothing to
// show. To add a card, write it in its own file in this folder and add it to `cards` at its slot
// below, in this order: This machine, Hub, Editor, Updates, Analytics and crash reporting,
// Troubleshooting, Snapshot. A new row goes into the card it belongs to.
import { useEffect } from "react";
import type { ComponentType } from "react";

import { readAppSettings } from "#/lib/app-settings.ts";
import { AppUpdatesCard } from "#/screens/settings/app-updates.tsx";
import { EditorCard } from "#/screens/settings/editor.tsx";
import { HubCard } from "#/screens/settings/hub.tsx";
import { SnapshotCard } from "#/screens/settings/snapshot.tsx";
import { ThisMachineCard } from "#/screens/settings/this-machine.tsx";
import { TroubleshootingCard } from "#/screens/settings/troubleshooting.tsx";
import { UsageDataCard } from "#/screens/settings/usage-data.tsx";

interface Card {
  /** Names the card, as React's key. */
  id: string;
  Card: ComponentType;
}

/** The cards, top to bottom. */
const cards: Card[] = [
  { Card: ThisMachineCard, id: "this-machine" },
  { Card: HubCard, id: "hub" },
  { Card: EditorCard, id: "editor" },
  { Card: AppUpdatesCard, id: "updates" },
  { Card: UsageDataCard, id: "usage-data" },
  { Card: TroubleshootingCard, id: "troubleshooting" },
  { Card: SnapshotCard, id: "snapshot" },
];

export const SettingsScreen = () => {
  // Read each time Settings shows: an effect runs again when a kept screen shows again.
  useEffect(() => {
    void readAppSettings();
  }, []);
  return (
    <div className="pt-page-top px-page pb-page gap-page-title flex h-full flex-col overflow-auto">
      <h1 className="type-page-title text-text-primary m-0">Settings</h1>
      <div className="max-w-settings gap-settings-cards flex flex-col">
        {cards.map(({ id, Card }) => (
          <Card key={id} />
        ))}
      </div>
    </div>
  );
};
