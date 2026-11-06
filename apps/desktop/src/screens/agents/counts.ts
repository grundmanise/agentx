// What the Agents page says about each agent configuration, read from the inventory and nothing
// else: its counts from the inventory's edges, and the library's path from an occurrence.
import type { Configuration, InventoryEvent } from "#/cli/events.gen.ts";

export interface Counts {
  skills: number;
  servers: number;
  plugins: number;
}

/**
 * How many skills, MCP servers and plugins the configuration has, counted from the edges that
 * leave its node: each edge points at a skill, a server or a plugin node.
 */
export const countsOf = (
  inventory: InventoryEvent,
  c: Configuration
): Counts => {
  const skills = new Set(inventory.skills.map((n) => n.physical_id));
  const servers = new Set(inventory.mcp_servers.map((n) => n.physical_id));
  const plugins = new Set(inventory.plugins.map((n) => n.physical_id));
  const counts: Counts = { plugins: 0, servers: 0, skills: 0 };
  for (const edge of inventory.edges) {
    if (edge.from !== c.physical_id) {
      continue;
    }
    if (skills.has(edge.to)) {
      counts.skills += 1;
    } else if (servers.has(edge.to)) {
      counts.servers += 1;
    } else if (plugins.has(edge.to)) {
      counts.plugins += 1;
    }
  }
  return counts;
};

const dirname = (path: string) => {
  const at = path.lastIndexOf("/");
  return at <= 0 ? path : path.slice(0, at);
};

/**
 * The library's path, from a library occurrence of the inventory: a library-mode placement, or
 * an occurrence in a configuration that reads the library. Undefined when the inventory shows none.
 */
export const libraryPathOf = (
  inventory: InventoryEvent
): string | undefined => {
  const placement = inventory.library
    .flatMap((e) => e.placements)
    .find((p) => p.mode === "library");
  if (placement !== undefined) {
    return dirname(placement.path);
  }
  const universal = new Set(
    inventory.configurations.filter((c) => c.reads_library).map((c) => c.id)
  );
  const occurrence = inventory.skills
    .flatMap((n) => n.occurrences)
    .find((o) => universal.has(o.configuration) && o.plugin === undefined);
  return occurrence === undefined ? undefined : dirname(occurrence.path);
};

/** How many library skills have no placement in the configuration: what Place all would place. */
export const unplacedCount = (inventory: InventoryEvent, id: string) =>
  inventory.library.filter(
    (e) => !e.placements.some((p) => p.configuration === id)
  ).length;
