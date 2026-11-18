import { useMemo, useState } from "react";

import type { Configuration } from "#/cli/events.gen.ts";
import { PluginDetail } from "#/screens/plugins/plugin-detail.tsx";
import { PluginList } from "#/screens/plugins/plugin-list.tsx";
import {
  groupPlugins,
  noPluginFilters,
  projectPlugins,
  twinsOf,
} from "#/screens/plugins/projection.ts";
import type {
  PluginFilters,
  PluginOrder,
  PluginRow,
} from "#/screens/plugins/projection.ts";
import { useAppStore } from "#/store/store.ts";

const noRows: PluginRow[] = [];
const noConfigurations: Configuration[] = [];

/**
 * The Plugins screen: every plugin the agent clients installed, one row per plugin in one agent
 * configuration in one version, grouped by agent in the list on the left, and the open plugin on
 * the right. Read-only: nothing here installs, removes, enables or disables a plugin, and no
 * control runs a command.
 */
export const PluginsScreen = () => {
  const inventory = useAppStore((s) => s.inventory);
  const rows = useMemo(
    () => (inventory === undefined ? noRows : projectPlugins(inventory)),
    [inventory]
  );
  const all = inventory?.configurations ?? noConfigurations;
  // Only the agent configurations that hold a plugin are groups and filter choices.
  const configurations = useMemo(
    () => all.filter((c) => rows.some((r) => r.configuration === c.id)),
    [all, rows]
  );
  const [filters, setFilters] = useState<PluginFilters>(noPluginFilters);
  const [order, setOrder] = useState<PluginOrder>("default");
  const [selectedKey, setSelectedKey] = useState<string | undefined>();
  const groups = useMemo(
    () => groupPlugins(rows, configurations, filters, order),
    [rows, configurations, filters, order]
  );
  const shown = groups.flatMap((g) => g.rows);
  // The plugin opened last while the filters show it, else the first one shown.
  const selected = shown.find((r) => r.key === selectedKey) ?? shown[0];
  const configuration = configurations.find(
    (c) => c.id === selected?.configuration
  );
  return (
    <div className="flex min-h-0 flex-1">
      <PluginList
        rows={rows}
        groups={groups}
        configurations={configurations}
        filters={filters}
        onFilters={setFilters}
        order={order}
        onOrder={setOrder}
        selectedKey={selected?.key}
        onSelect={(row) => {
          setSelectedKey(row.key);
        }}
      />
      <section
        aria-label="Plugin details"
        className="bg-shell-content-bg min-w-0 flex-1 overflow-auto"
      >
        {selected !== undefined && configuration !== undefined && (
          <PluginDetail
            key={selected.key}
            row={selected}
            configuration={configuration}
            twins={twinsOf(selected, rows)}
            configurations={configurations}
            onSelect={(row) => {
              // A plugin the filters hide shows once they are cleared.
              if (!shown.includes(row)) {
                setFilters(noPluginFilters);
              }
              setSelectedKey(row.key);
            }}
          />
        )}
      </section>
    </div>
  );
};
