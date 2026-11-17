// What each agent sees of the skills that share a name: the places on the machine that hold a skill
// of the name, and the agents that find it there. Every fact comes from the inventory: which
// folder holds which skill, and which configuration sees it. What an agent does with two skills
// of one name is the agent's own business, so nothing here says which one it uses. The notice and
// the table's words are templates filled from those facts.
import type { Configuration, Occurrence } from "#/cli/events.gen.ts";
import { byPopularity } from "#/lib/agent-popularity.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";

/** Where a skill of the name is: your library, a plugin, an agent's own folder or a project's. */
export type PlaceKind = "library" | "plugin" | "folder" | "project";

/** One place that holds a skill of the name, and the agents that see it there, the most used first. */
export interface Place {
  key: string;
  kind: PlaceKind;
  /** The plugin's name, or the name of the agent whose folder it is. */
  label: string;
  agents: Configuration[];
}

/** The places of a skill's name, and which of them hold the selected skill. */
export interface NameSights {
  name: string;
  places: Place[];
  /** The keys of the places that hold the selected skill itself. */
  own: string[];
}

const LIBRARY = "library";

/** The folder an occurrence is in, as the configuration whose folder holds it: `c.path` holds it. */
const ownerOf = (o: Occurrence, configurations: Configuration[]) =>
  configurations.find(
    (c) => c.path !== "" && o.path.startsWith(`${c.path}/`)
  ) ?? configurations.find((c) => c.id === o.configuration);

/** The place an occurrence of a skill outside the library is in. */
const placeOf = (
  o: Occurrence,
  configurations: Configuration[]
): Omit<Place, "agents"> => {
  if (o.plugin !== undefined) {
    return { key: `plugin:${o.plugin}`, kind: "plugin", label: o.plugin };
  }
  if (o.scope === "project") {
    return { key: "project", kind: "project", label: "" };
  }
  const owner = ownerOf(o, configurations);
  return {
    key: `folder:${owner?.id ?? o.configuration}`,
    kind: "folder",
    label: owner?.name ?? o.configuration,
  };
};

/**
 * The agents that see a library skill: each agent that reads the library and sees it, and each
 * other agent that has it, through a link or a copy.
 */
const libraryAgents = (
  row: SkillRow,
  configurations: Configuration[]
): Configuration[] => {
  const byId = new Map(configurations.map((c) => [c.id, c]));
  const seen = new Map<string, Configuration>();
  for (const id of row.agents) {
    const c = byId.get(id);
    if (c?.reads_library === true) {
      seen.set(id, c);
    }
  }
  for (const o of row.occurrences) {
    const c = byId.get(o.configuration);
    if (c !== undefined) {
      seen.set(c.id, c);
    }
  }
  for (const p of row.entry?.placements ?? []) {
    const c = byId.get(p.configuration);
    if (c !== undefined && row.agents.includes(c.id)) {
      seen.set(c.id, c);
    }
  }
  return [...seen.values()];
};

/** The library first, then the plugins, then the agents' own folders, then a project's. */
const rank: Record<PlaceKind, number> = {
  folder: 2,
  library: 0,
  plugin: 1,
  project: 3,
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
  const own = new Set<string>();
  const add = (
    place: Omit<Place, "agents">,
    agent: Configuration,
    mine: boolean
  ) => {
    const kept = places.get(place.key) ?? { ...place, agents: [] };
    if (!kept.agents.some((a) => a.id === agent.id)) {
      kept.agents.push(agent);
    }
    places.set(place.key, kept);
    if (mine) {
      own.add(place.key);
    }
  };
  const byId = new Map(configurations.map((c) => [c.id, c]));
  for (const r of [row, ...others]) {
    const mine = r === row;
    if (r.entry !== undefined) {
      for (const agent of libraryAgents(r, configurations)) {
        add({ key: LIBRARY, kind: "library", label: "" }, agent, mine);
      }
      continue;
    }
    for (const o of r.occurrences) {
      const c = byId.get(o.configuration);
      if (c !== undefined) {
        add(placeOf(o, configurations), c, mine);
      }
    }
  }
  const kept = [...places.values()];
  kept.sort(
    (a, b) => rank[a.kind] - rank[b.kind] || a.label.localeCompare(b.label)
  );
  for (const place of kept) {
    place.agents.sort(byPopularity);
  }
  return { name: row.name, own: [...own], places: kept };
};

/** `a`, `a and b`, `a, b and c`. */
const listOf = (items: string[]) =>
  items.length < 2
    ? items.join("")
    : `${items.slice(0, -1).join(", ")} and ${items.at(-1) ?? ""}`;

/** `a`, `a and b`, `a, b and c`, `a, b, c and d`, and past four `a, b, c and 2 more`. */
export const namesOf = (agents: Pick<Configuration, "name">[]): string => {
  const names = agents.map((a) => a.name);
  if (names.length > 4) {
    return `${names.slice(0, 3).join(", ")} and ${names.length - 3} more`;
  }
  return listOf(names);
};

/** The places each agent sees, by the agent's id. */
const placesByAgent = (sights: NameSights) => {
  const by = new Map<string, Place[]>();
  for (const place of sights.places) {
    for (const a of place.agents) {
      by.set(a.id, [...(by.get(a.id) ?? []), place]);
    }
  }
  return by;
};

/** Every agent that sees a skill of the name, the most used first. */
const agentsOf = (sights: NameSights) => {
  const all = new Map<string, Configuration>();
  for (const place of sights.places) {
    for (const a of place.agents) {
      all.set(a.id, a);
    }
  }
  const agents = [...all.values()];
  agents.sort(byPopularity);
  return agents;
};

/** The agents that see more than one skill of the name, the most used first. */
export const clashingOf = (sights: NameSights): Configuration[] => {
  const by = placesByAgent(sights);
  return agentsOf(sights).filter((a) => (by.get(a.id)?.length ?? 0) > 1);
};

/** A skill of the name by where it is: "your pdf", "the acme plugin's pdf", "the copy in Codex's folder". */
const skillIn = (place: Place, name: string) => {
  switch (place.kind) {
    case "library": {
      return `your ${name}`;
    }
    case "plugin": {
      return `the ${place.label} plugin's ${name}`;
    }
    case "folder": {
      return `the copy in ${place.label}'s folder`;
    }
    case "project": {
      return "the copy in a project's folder";
    }
    default: {
      return name;
    }
  }
};

/** Agents in runs that share a key, in the order of their first agent. */
const groupBy = (
  agents: Configuration[],
  keyOf: (a: Configuration) => string
) => {
  const groups = new Map<string, Configuration[]>();
  for (const a of agents) {
    const key = keyOf(a);
    groups.set(key, [...(groups.get(key) ?? []), a]);
  }
  return [...groups.values()];
};

const keyOfPlaces = (places: Place[]) => places.map((p) => p.key).join("|");

/**
 * The notice under AGENTS while some agent sees more than one skill of the name: who also sees
 * which other skill, then that each of them picks one. Undefined when no agent sees two.
 */
export const clashText = (sights: NameSights): string | undefined => {
  const clashing = clashingOf(sights);
  if (clashing.length === 0) {
    return undefined;
  }
  const by = placesByAgent(sights);
  const placesOf = (a: Configuration) => by.get(a.id) ?? [];
  const sees = (a: Configuration) =>
    placesOf(a).some((p) => sights.own.includes(p.key));
  const othersOf = (a: Configuration) =>
    placesOf(a).filter((p) => !sights.own.includes(p.key));
  const sentences: string[] = [];
  const also = clashing.filter(sees);
  for (const group of groupBy(also, (a) => keyOfPlaces(othersOf(a)))) {
    const [first] = group;
    if (first !== undefined) {
      const skills = othersOf(first).map((p) => skillIn(p, sights.name));
      const verb = group.length === 1 ? "sees" : "see";
      sentences.push(`${namesOf(group)} also ${verb} ${listOf(skills)}.`);
    }
  }
  const rest = clashing.filter((a) => !sees(a));
  for (const group of groupBy(rest, (a) => keyOfPlaces(placesOf(a)))) {
    const [first] = group;
    if (first !== undefined) {
      const skills = placesOf(first).map((p) => skillIn(p, sights.name));
      const verb = group.length === 1 ? "sees" : "see";
      sentences.push(`${namesOf(group)} ${verb} ${listOf(skills)}.`);
    }
  }
  const two = clashing.every((a) => placesOf(a).length === 2);
  const who = clashing.length === 1 ? "It picks" : "Each picks";
  sentences.push(`${who} one of ${two ? "the two" : "them"}.`);
  return sentences.join(" ");
};

/** A column of the table: a place, under a short heading. */
export interface SightsColumn {
  key: string;
  heading: string;
}

/** A row of the table: one agent that sees more than one skill, or the agents that see the same one. */
export interface SightsRow {
  key: string;
  agents: Configuration[];
  /** Whether the row's agents see the skill of each column, in the columns' order. */
  sees: boolean[];
  /** The agent sees more than one skill of the name, so the row is highlighted. */
  clash: boolean;
}

/** The table of the hover card: a column per place, a row per agent or per agents that see alike. */
export interface SightsTable {
  title: string;
  columns: SightsColumn[];
  rows: SightsRow[];
  /** What the highlighted rows mean, while there are some. */
  footer?: string;
  /** Some agent sees your library's skill and another one, which renaming yours keeps apart. */
  renamable: boolean;
}

const headingOf = (place: Place) => {
  switch (place.kind) {
    case "library": {
      return "Yours";
    }
    case "plugin": {
      return place.label;
    }
    case "folder": {
      return `${place.label}'s folder`;
    }
    case "project": {
      return "A project's folder";
    }
    default: {
      return place.label;
    }
  }
};

/**
 * The hover card's table: the agents that see more than one skill of the name first, a row each,
 * highlighted; then the agents that see one, a row for those that see the same one.
 */
export const sightsTable = (sights: NameSights): SightsTable => {
  const by = placesByAgent(sights);
  const placesOf = (a: Configuration) => by.get(a.id) ?? [];
  const seesOf = (agents: Configuration[]) => {
    const seen = new Set(agents.flatMap((a) => placesOf(a).map((p) => p.key)));
    return sights.places.map((p) => seen.has(p.key));
  };
  const clashing = clashingOf(sights);
  const rows: SightsRow[] = clashing.map((a) => ({
    agents: [a],
    clash: true,
    key: a.id,
    sees: seesOf([a]),
  }));
  const alone = agentsOf(sights).filter((a) => placesOf(a).length === 1);
  for (const group of groupBy(alone, (a) => keyOfPlaces(placesOf(a)))) {
    rows.push({
      agents: group,
      clash: false,
      key: group.map((a) => a.id).join(","),
      sees: seesOf(group),
    });
  }
  const renamable = clashing.some((a) =>
    placesOf(a).some((p) => p.kind === "library")
  );
  const table: SightsTable = {
    columns: sights.places.map((p) => ({ heading: headingOf(p), key: p.key })),
    renamable,
    rows,
    title: `Skills named ${sights.name}`,
  };
  if (clashing.length > 0) {
    const two = clashing.every((a) => placesOf(a).length === 2);
    const one = clashing.length === 1;
    const sees = one
      ? "The highlighted agent sees"
      : "The highlighted agents see";
    const lines = [
      `${sees} ${two ? "both skills" : "all of them"}.`,
      `It's up to ${one ? "the agent" : "each agent"} to decide which one to use.`,
    ];
    if (renamable) {
      lines.push("To prevent possible clashes, rename your skill.");
    }
    table.footer = lines.join(" ");
  }
  return table;
};
