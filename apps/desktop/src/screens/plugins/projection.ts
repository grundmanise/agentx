// What the Plugins screen shows, projected from the store and nothing else: one card per plugin
// node, one plugin in one agent configuration in one version, with the skills and servers the
// inventory's edges say it provides and the hooks its node lists. Pure functions over the applied
// inventory, so that every later change reads a plugin the same way. Nothing here predicts: a card
// changes only when the inventory does.
import type {
  InventoryEvent,
  McpServer,
  Plugin,
  SkillNode,
} from "#/cli/events.gen.ts";

/** One row of a card's contents: a skill, a server or a hook. */
export interface PluginItem {
  /** The row's key, and for a skill or a server what opens it: the skill's row key, the server's logical id. */
  key: string;
  name: string;
  /** Mono meta after the name: a server's transport, a hook's event; empty for a skill. */
  meta: string;
}

export interface PluginCard {
  /** The card's key: the plugin node's physical id. */
  key: string;
  name: string;
  /** The installed version, or empty when the agent client records none. */
  version: string;
  /** Where it was installed from, or empty for a local plugin. */
  marketplace: string;
  /** The display name of the agent configuration it is installed in. */
  agent: string;
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
      .map((n) => ({ key: n.physical_id, meta: "", name: n.name })),
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
  plugin.hooks.map((h) => ({
    key: `${h.event} ${h.name}`,
    meta: h.event,
    name: h.name,
  }));

const cardOf = (plugin: Plugin, inventory: InventoryEvent): PluginCard => {
  const provided = providedBy(plugin, inventory);
  const card: PluginCard = {
    agent:
      inventory.configurations.find((c) => c.id === plugin.configuration)
        ?.name ?? plugin.configuration,
    disabled: plugin.enabled === false,
    hooks: hooksOf(plugin),
    key: plugin.physical_id,
    marketplace: plugin.marketplace,
    name: plugin.name,
    servers: serversOf(plugin, provided, inventory.mcp_servers),
    skills: skillsOf(provided, inventory.skills),
    version: plugin.version,
  };
  if (plugin.icon !== undefined && plugin.icon !== "") {
    card.icon = plugin.icon;
  }
  return card;
};

/** One card per plugin node, sorted by name, then by agent, then by version. */
export const projectPlugins = (inventory: InventoryEvent): PluginCard[] =>
  sorted(
    inventory.plugins.map((p) => cardOf(p, inventory)),
    (a, b) =>
      compareNames(a, b) ||
      a.agent.localeCompare(b.agent) ||
      a.version.localeCompare(b.version) ||
      a.key.localeCompare(b.key)
  );
