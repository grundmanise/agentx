import { useMemo } from "react";

import { PluginCardView } from "#/screens/plugins/plugin-card.tsx";
import { projectPlugins } from "#/screens/plugins/projection.ts";
import type { PluginCard } from "#/screens/plugins/projection.ts";
import { useAppStore } from "#/store/store.ts";

const none: PluginCard[] = [];

/**
 * The Plugins screen: every plugin the agent clients installed, one card per plugin in one agent
 * configuration in one version, in three masonry columns. Read-only: nothing here installs,
 * removes, enables or disables a plugin, and no control runs a command.
 */
export const PluginsScreen = () => {
  const inventory = useAppStore((s) => s.inventory);
  const cards = useMemo(
    () => (inventory === undefined ? none : projectPlugins(inventory)),
    [inventory]
  );
  return (
    <div className="pt-page-top px-page pb-page gap-page-title flex h-full flex-col overflow-auto">
      <header className="flex items-baseline gap-3">
        <h1 className="type-page-title text-text-primary m-0">Plugins</h1>
        <span className="type-caption text-text-caption">Read-only</span>
      </header>
      {cards.length === 0 ? (
        <p className="type-secondary text-text-hint m-0">
          No plugins found in your agents.
        </p>
      ) : (
        <ul
          aria-label="Plugins"
          className="gap-plugin-card m-0 list-none columns-3 p-0"
        >
          {/* Each card sits in a wrapper with the gap below it, so the columns start level. */}
          {cards.map((card) => (
            <li key={card.key} className="break-inside-avoid pb-3">
              <PluginCardView card={card} />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
};
