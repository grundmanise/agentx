// What the Plugins screen shows, projected from the store and nothing else: one row per plugin
// node, one plugin in one agent configuration in one version, with the skills and servers the
// inventory's edges say it provides and the hooks its node lists. The rows are grouped by agent
// configuration, filtered by name and agent, and sorted as the sort toggle says. Pure functions over the applied
// inventory, so that every later change reads a plugin the same way. Nothing here predicts: a row
// changes only when the inventory does.
import type {
  Configuration,
  InventoryEvent,
  McpServer,
  Plugin,
  SkillNode,
} from "#/cli/events.gen.ts";

/** One row of a plugin's contents: a skill, a server or a hook. */
export interface PluginItem {
  /** The row's key, and for a skill or a server what opens it: the skill's row key, the server's logical id. */
  key: string;
  name: string;
  /** The second column: a skill's description, a server's transport, a hook's event. */
  meta: string;
}

export interface PluginRow {
  /** The row's key: the plugin node's physical id. */
  key: string;
  /** The id of the agent configuration it is installed in. */
  configuration: string;
  name: string;
  /** The installed version, or empty when the agent client records none. */
  version: string;
  /** Where it was installed from, or empty for a local plugin. */
  marketplace: string;
  /** The display name of the agent configuration it is installed in. */
  agent: string;
  /** Where it is installed on this machine. */
  path: string;
  /** The agent client records the plugin as turned off. */
  disabled: boolean;
  /** The plugin's own icon as a data URL, when the inventory carries one. */
  icon?: string;
  skills: PluginItem[];
  servers: PluginItem[];
  hooks: PluginItem[];
}

const compareNames = (a: { name: string }, b: { name: string }) =>
  a.name.localeCompare(b.name, undefined, { sensitivity: "base" });

/** A sorted copy of `items`; `items` is left as it is. */
const sorted = <T>(items: T[], compare: (a: T, b: T) => number): T[] => {
  const copy = [...items];
  copy.sort(compare);
  return copy;
};

/** The ids the plugin's edges point to. */
const providedBy = (plugin: Plugin, inventory: InventoryEvent) =>
  new Set(
    inventory.edges
      .filter((e) => e.from === plugin.physical_id)
      .map((e) => e.to)
  );

/** The skills the plugin provides, by its edges, each by the key its Skills row has. */
const skillsOf = (provided: Set<string>, skills: SkillNode[]): PluginItem[] =>
  sorted(
    skills
      .filter((n) => provided.has(n.physical_id))
      .map((n) => ({
        key: n.physical_id,
        meta: n.description,
        name: n.name,
      })),
    compareNames
  );

/** The transport of the server's declaration inside the plugin, else of its first declaration. */
const transportOf = (node: McpServer, plugin: Plugin) =>
  (
    node.occurrences.find(
      (o) =>
        o.plugin === plugin.name && o.configuration === plugin.configuration
    ) ?? node.occurrences[0]
  )?.transport ?? "";

/** The servers the plugin provides, by its edges, each by the key its MCP servers row has. */
const serversOf = (
  plugin: Plugin,
  provided: Set<string>,
  servers: McpServer[]
): PluginItem[] => {
  const items = new Map<string, PluginItem>();
  for (const node of servers.filter((n) => provided.has(n.physical_id))) {
    if (!items.has(node.logical_id)) {
      items.set(node.logical_id, {
        key: node.logical_id,
        meta: transportOf(node, plugin),
        name: node.name,
      });
    }
  }
  return sorted([...items.values()], compareNames);
};

/** The plugin's hooks, as the CLI lists them: sorted by event, then name. */
const hooksOf = (plugin: Plugin): PluginItem[] =>
  plugin.hooks.map((h, at) => ({
    // Two handlers can share an event and a name; their place tells them apart.
    key: `${at} ${h.event} ${h.name}`,
    meta: h.event,
    name: h.name,
  }));

const rowOf = (plugin: Plugin, inventory: InventoryEvent): PluginRow => {
  const provided = providedBy(plugin, inventory);
  const row: PluginRow = {
    agent:
      inventory.configurations.find((c) => c.id === plugin.configuration)
        ?.name ?? plugin.configuration,
    configuration: plugin.configuration,
    disabled: plugin.enabled === false,
    hooks: hooksOf(plugin),
    key: plugin.physical_id,
    marketplace: plugin.marketplace,
    name: plugin.name,
    path: plugin.path,
    servers: serversOf(plugin, provided, inventory.mcp_servers),
    skills: skillsOf(provided, inventory.skills),
    version: plugin.version,
  };
  if (plugin.icon !== undefined && plugin.icon !== "") {
    row.icon = plugin.icon;
  }
  return row;
};

/** One row per plugin node, in no set order. */
export const projectPlugins = (inventory: InventoryEvent): PluginRow[] =>
  inventory.plugins.map((p) => rowOf(p, inventory));

/** What the list shows: a case-insensitive part of the name, and the agent configurations. */
export interface PluginFilters {
  name: string;
  /** Configuration ids; none shows every agent. */
  agents: string[];
}

export const noPluginFilters: PluginFilters = { agents: [], name: "" };

/** The order of the sort toggle: A to Z, or Z to A, by name. */
export type PluginOrder = "asc" | "desc";

/** One group of the list: an agent configuration and its plugins. */
export interface PluginGroup {
  configuration: Configuration;
  rows: PluginRow[];
}

const byName = (a: PluginRow, b: PluginRow) =>
  compareNames(a, b) ||
  a.version.localeCompare(b.version) ||
  a.key.localeCompare(b.key);

const comparer = (order: PluginOrder) =>
  order === "asc" ? byName : (a: PluginRow, b: PluginRow) => byName(b, a);

/**
 * The rows that pass the filters, one group per agent configuration that holds any, the groups A
 * to Z by the configuration's name, the rows of each in `order`.
 */
export const groupPlugins = (
  rows: PluginRow[],
  configurations: Configuration[],
  filters: PluginFilters,
  order: PluginOrder
): PluginGroup[] => {
  const name = filters.name.trim().toLowerCase();
  const shown = rows.filter(
    (r) =>
      (name === "" || r.name.toLowerCase().includes(name)) &&
      (filters.agents.length === 0 || filters.agents.includes(r.configuration))
  );
  return sorted(configurations, (a, b) => a.name.localeCompare(b.name))
    .map((configuration) => ({
      configuration,
      rows: sorted(
        shown.filter((r) => r.configuration === configuration.id),
        comparer(order)
      ),
    }))
    .filter((g) => g.rows.length > 0);
};

/** The same plugin in the other agent configurations: one name from one marketplace. */
export const twinsOf = (row: PluginRow, rows: PluginRow[]): PluginRow[] =>
  sorted(
    rows.filter(
      (r) =>
        r.configuration !== row.configuration &&
        r.name === row.name &&
        r.marketplace === row.marketplace
    ),
    (a, b) => a.agent.localeCompare(b.agent)
  );
