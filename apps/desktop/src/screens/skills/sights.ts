// What each agent sees of the skills that share a name: the places on the machine that hold a skill
// of the name, and the agents that find it there. Every fact comes from the inventory: which
// folder holds which skill, and which configuration sees it. What an agent does with two skills
// of one name is the agent's own business, so nothing here says which one it uses.
import type { Configuration, Occurrence } from "#/cli/events.gen.ts";
import { byPopularity } from "#/lib/agent-popularity.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";

/** How an agent that does not read the library reaches a library skill. */
export type Through = "link" | "copy";

/** One agent of a place: its configuration, and how it reaches a library skill, if not by reading. */
export interface PlaceAgent {
  configuration: Configuration;
  through?: Through;
}

/** One place that holds a skill of the name, and the agents that see it there. */
export interface Place {
  key: string;
  heading: string;
  agents: PlaceAgent[];
}

/** The places of a skill's name, and the agents that see more than one skill of it. */
export interface NameSights {
  name: string;
  places: Place[];
  /** The agents that find a skill of the name in more than one place, the most used first. */
  crowded: Configuration[];
  /** How many skills of the name the crowded agents see, when they all see as many. */
  count?: number;
}

const LIBRARY = "library";

/** The folder an occurrence is in, as the configuration whose folder holds it: `c.path` holds it. */
const ownerOf = (o: Occurrence, configurations: Configuration[]) =>
  configurations.find(
    (c) => c.path !== "" && o.path.startsWith(`${c.path}/`)
  ) ?? configurations.find((c) => c.id === o.configuration);

/** The key and heading of the place an occurrence of a skill outside the library is in. */
const placeOf = (
  o: Occurrence,
  configurations: Configuration[]
): { key: string; heading: string } => {
  if (o.plugin !== undefined) {
    return { heading: `${o.plugin} plugin`, key: `plugin:${o.plugin}` };
  }
  if (o.scope === "project") {
    return { heading: "A project's folder", key: "project" };
  }
  const owner = ownerOf(o, configurations);
  const name = owner?.name ?? o.configuration;
  return {
    heading: `${name}'s own folder`,
    key: `folder:${owner?.id ?? o.configuration}`,
  };
};

/**
 * The agents that see a library skill: each agent that reads the library and sees it, then each
 * other agent that has it, through a link or a copy.
 */
const libraryAgents = (
  row: SkillRow,
  configurations: Configuration[]
): PlaceAgent[] => {
  const byId = new Map(configurations.map((c) => [c.id, c]));
  const seen = new Map<string, PlaceAgent>();
  for (const id of row.agents) {
    const c = byId.get(id);
    if (c?.reads_library === true) {
      seen.set(id, { configuration: c });
    }
  }
  for (const o of row.occurrences) {
    const c = byId.get(o.configuration);
    if (c === undefined || seen.has(c.id)) {
      continue;
    }
    if (c.reads_library) {
      seen.set(c.id, { configuration: c });
      continue;
    }
    seen.set(c.id, {
      configuration: c,
      through: o.kind === "copy" ? "copy" : "link",
    });
  }
  for (const p of row.entry?.placements ?? []) {
    const c = byId.get(p.configuration);
    if (c !== undefined && !seen.has(c.id) && row.agents.includes(c.id)) {
      seen.set(c.id, {
        configuration: c,
        through: p.mode === "copy" ? "copy" : "link",
      });
    }
  }
  const agents = [...seen.values()];
  agents.sort(
    (a, b) =>
      Number(a.through !== undefined) - Number(b.through !== undefined) ||
      byPopularity(a.configuration, b.configuration)
  );
  return agents;
};

/** The library first, then the plugins, then the agents' own folders, each A to Z. */
const rank = (key: string) => {
  if (key === LIBRARY) {
    return 0;
  }
  return key.startsWith("plugin:") ? 1 : 2;
};

/**
 * The places that hold a skill named as `row` is, `row` and the `others` of its name together,
 * with the agents that see each; a place no agent sees is left out.
 */
export const sightsOf = (
  row: SkillRow,
  others: SkillRow[],
  configurations: Configuration[]
): NameSights => {
  const places = new Map<string, Place>();
  const add = (key: string, heading: string, agent: PlaceAgent) => {
    const place = places.get(key) ?? { agents: [], heading, key };
    if (
      !place.agents.some((a) => a.configuration.id === agent.configuration.id)
    ) {
      place.agents.push(agent);
    }
    places.set(key, place);
  };
  const byId = new Map(configurations.map((c) => [c.id, c]));
  for (const r of [row, ...others]) {
    if (r.entry !== undefined) {
      for (const agent of libraryAgents(r, configurations)) {
        add(LIBRARY, "Your library", agent);
      }
      continue;
    }
    for (const o of r.occurrences) {
      const c = byId.get(o.configuration);
      if (c !== undefined) {
        const { key, heading } = placeOf(o, configurations);
        add(key, heading, { configuration: c });
      }
    }
  }
  const kept = [...places.values()].filter((p) => p.agents.length > 0);
  kept.sort(
    (a, b) => rank(a.key) - rank(b.key) || a.heading.localeCompare(b.heading)
  );
  for (const place of kept) {
    if (place.key !== LIBRARY) {
      place.agents.sort((a, b) =>
        byPopularity(a.configuration, b.configuration)
      );
    }
  }
  const counts = new Map<string, number>();
  for (const place of kept) {
    for (const a of place.agents) {
      counts.set(a.configuration.id, (counts.get(a.configuration.id) ?? 0) + 1);
    }
  }
  const crowded = configurations.filter((c) => (counts.get(c.id) ?? 0) > 1);
  crowded.sort(byPopularity);
  const seen = new Set(crowded.map((c) => counts.get(c.id)));
  const sights: NameSights = { crowded, name: row.name, places: kept };
  if (seen.size === 1) {
    const [count] = seen;
    if (count !== undefined) {
      sights.count = count;
    }
  }
  return sights;
};

/** `a`, `a and b`, `a, b and c`, `a, b, c and d`, and past four `a, b, c and 2 more`. */
export const namesOf = (agents: Pick<Configuration, "name">[]): string => {
  const names = agents.map((a) => a.name);
  if (names.length > 4) {
    return `${names.slice(0, 3).join(", ")} and ${names.length - 3} more`;
  }
  return names.length < 2
    ? names.join("")
    : `${names.slice(0, -1).join(", ")} and ${names.at(-1) ?? ""}`;
};

/** How many skills: two in words, more in digits. */
const skillsCount = (count: number | undefined) => {
  if (count === undefined) {
    return "more than one skill";
  }
  return count === 2 ? "two skills" : `${count} skills`;
};

/**
 * The notice under AGENTS: the agents that see more than one skill of the name, or undefined when
 * none does.
 */
export const crowdedText = (sights: NameSights): string | undefined => {
  const { count, crowded, name } = sights;
  if (crowded.length === 0) {
    return undefined;
  }
  const verb = crowded.length === 1 ? "sees" : "see";
  return `${namesOf(crowded)} ${verb} ${skillsCount(count)} named ${name}.`;
};
