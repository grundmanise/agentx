// What the Machines screen shows, projected from the store and nothing else: this machine, from
// the applied inventory, then every other machine the hub holds, from the `fleet` slice, and the
// cloud machine last. Each machine lists the skills each of its agent configurations sees, as
// `agentx machine show` does: the library's skills, then a plugin's, then those of the agent's own
// skills directory. Pure functions, so that every later change reads a machine the same way.
// Nothing here compares one machine with another: across.ts does, from what each machine holds.
import type {
  FleetMachine,
  InventoryEvent,
  Machine,
  ReportConfiguration,
  ReportContents,
  ReportLibrarySkill,
  ReportSkill,
} from "#/cli/events.gen.ts";
import { formatAgo, formatDate } from "#/lib/dates.ts";
import { shortSource } from "#/screens/skills/projection.ts";

/** The machine id of the cloud machine, which no computer has. */
export const CLOUD_ID = "cloud";

/** This machine, another computer that wrote a report, or the cloud machine. */
export type MachineKind = "this" | "other" | "cloud";

/** Where a library skill is published: the hub, a source, or nowhere. */
export interface SkillSource {
  /** A hub skill: published to the hub. */
  hub: boolean;
  /** The source's short form, or a forked hub skill's upstream's; empty when there is none. */
  short: string;
  /** The same in full, with the directory in it, for a tooltip; empty when there is none. */
  full: string;
}

/** What brings a skill to a machine. */
export interface SkillOrigin {
  /** The machine's library, a plugin, or an agent's own skills directory. */
  from: "library" | "plugin" | "directory";
  /** The plugin's name, for a plugin's skill. */
  plugin: string | undefined;
  /** For a library skill, where it is published. */
  source: SkillSource | undefined;
}

/**
 * What tells a skill from the same skill on another machine, and one version of it from another:
 * across.ts compares two of these.
 */
export interface Holding {
  name: string;
  /**
   * The logical id of its library entry, when the entry names a fork id or a source: the same on
   * every machine that holds the skill. A skill with none is known by its name alone.
   */
  logicalId: string | undefined;
  /** What the machine holds of it; the cloud machine's skills have none. */
  contentHash: string | undefined;
  /** The commit of its source it was installed at, when the entry names one. */
  upstreamCommit: string | undefined;
}

/** One skill a machine holds: in its library, from a plugin or in an agent's own skills directory. */
export interface HeldSkill extends Holding, SkillOrigin {}

/** One skill an agent configuration sees. */
export interface SkillLine extends SkillOrigin {
  /** Unique among its configuration's skills. */
  key: string;
  name: string;
  /** A library skill edited on that machine. */
  modified: boolean;
  /** What the machine holds of it. */
  contentHash: string | undefined;
}

/** One agent configuration of a machine, with the skills its agent client sees. */
export interface AgentGroup {
  /** The configuration id, which is its client's slug. */
  id: string;
  name: string;
  enabled: boolean;
  skills: SkillLine[];
}

export interface MachineView {
  /** The machine id, and the row's key. */
  id: string;
  kind: MachineKind;
  label: string;
  /** The operating system, as the machine reported it; this machine and the cloud one have none. */
  os: string | undefined;
  /** The agentx version that wrote the report. */
  agentx: string | undefined;
  /** When the machine's report last changed on the hub; this machine's state is live. */
  changedAt: string | undefined;
  /** How many skills its agents list: the names under them, each counted once. */
  skillCount: number;
  /** Its agent configurations, A to Z by name. */
  agents: AgentGroup[];
  /** Every skill it holds, whether an agent sees it or not. */
  held: HeldSkill[];
}

const byName = (a: { name: string }, b: { name: string }) =>
  a.name.localeCompare(b.name, undefined, { sensitivity: "base" });

/** A sorted copy of `items`; `items` is left as it is. */
export const sorted = <T>(
  items: readonly T[],
  compare: (a: T, b: T) => number
) => {
  const copy = [...items];
  copy.sort(compare);
  return copy;
};

/**
 * What this machine's inventory report would hold, from the applied inventory: what the user
 * scope holds, as `agentx machine report` writes it, so that this machine reads as the others do.
 * A skill inside a project is no part of it.
 */
const reportOf = (inventory: InventoryEvent): ReportContents => {
  const skills = new Map<string, ReportSkill>();
  for (const node of inventory.skills) {
    const occurrences = node.occurrences.filter((o) => o.scope === "user");
    const key = `${node.name}\n${node.content_hash}`;
    const seen = skills.get(key);
    if (seen !== undefined) {
      seen.occurrences.push(...occurrences);
    } else if (occurrences.length > 0) {
      skills.set(key, {
        content_hash: node.content_hash,
        name: node.name,
        occurrences,
      });
    }
  }
  return {
    configurations: inventory.configurations,
    library: inventory.library,
    plugins: inventory.plugins,
    skills: [...skills.values()],
  };
};

/** A URL with the directory in it, when that is not its root. */
const withSubpath = (url: string, subpath: string | undefined) =>
  subpath === undefined || subpath === "" ? url : `${url}/${subpath}`;

const sourceOf = (entry: ReportLibrarySkill): SkillSource => {
  const hub = entry.fork_id !== undefined && entry.fork_id !== "";
  const url = (hub ? entry.upstream : entry.source) ?? "";
  return {
    full:
      url === ""
        ? ""
        : withSubpath(url, hub ? entry.upstream_subpath : entry.subpath),
    hub,
    short: url === "" ? "" : shortSource(url),
  };
};

/**
 * The skills the configuration `id` sees on a machine, from what the hub holds of it: every
 * library skill placed in it or that it reads as a universal client, then every skill a plugin of
 * it brings, then every other skill of its own skills directory. An occurrence the library
 * explains, a placement with the library skill's content or a copy of it by its name, is the
 * library skill's row and no row of its own. Each list is sorted by name.
 */
const skillsSeenBy = (held: ReportContents, id: string): SkillLine[] => {
  const library: SkillLine[] = [];
  // What the library explains: content it holds, and names it keeps a copy of here.
  const hashes = new Set<string>();
  const copies = new Set<string>();
  for (const entry of sorted(held.library, byName)) {
    const placed = entry.placements.filter((p) => p.configuration === id);
    if (placed.length > 0 || entry.universal.includes(id)) {
      library.push({
        contentHash: entry.content_hash,
        from: "library",
        key: `library ${entry.name}`,
        modified: entry.state === "modified",
        name: entry.name,
        plugin: undefined,
        source: sourceOf(entry),
      });
      if (entry.content_hash !== undefined) {
        hashes.add(entry.content_hash);
      }
      if (placed.some((p) => p.kind === "copy")) {
        copies.add(entry.name);
      }
    }
  }
  const plugins = new Map<string, SkillLine>();
  const own = new Map<string, SkillLine>();
  for (const skill of held.skills) {
    for (const o of skill.occurrences.filter((x) => x.configuration === id)) {
      if (o.plugin !== undefined && o.plugin !== "") {
        const key = `plugin ${skill.name} ${o.plugin}`;
        plugins.set(key, {
          contentHash: skill.content_hash,
          from: "plugin",
          key,
          modified: false,
          name: skill.name,
          plugin: o.plugin,
          source: undefined,
        });
      } else if (
        !hashes.has(skill.content_hash) &&
        !(o.kind === "copy" && copies.has(skill.name))
      ) {
        const key = `directory ${skill.name}`;
        own.set(key, {
          contentHash: skill.content_hash,
          from: "directory",
          key,
          modified: false,
          name: skill.name,
          plugin: undefined,
          source: undefined,
        });
      }
    }
  }
  return [
    ...library,
    ...sorted(
      [...plugins.values()],
      (a, b) => byName(a, b) || (a.plugin ?? "").localeCompare(b.plugin ?? "")
    ),
    ...sorted([...own.values()], byName),
  ];
};

const agentsOf = (held: ReportContents): AgentGroup[] =>
  sorted<ReportConfiguration>(held.configurations, byName).map((c) => ({
    enabled: c.enabled,
    id: c.id,
    name: c.name,
    skills: skillsSeenBy(held, c.id),
  }));

/**
 * How many skills a machine's row counts: the skill names its agents list, each once, so that the
 * row and the machine's details agree. A skill several agents see, or one an agent sees from two
 * places, is one skill.
 */
const skillCountOf = (agents: readonly AgentGroup[]) =>
  new Set(agents.flatMap((a) => a.skills.map((s) => s.name))).size;

/**
 * A library entry as what its machine holds of the skill. Its logical id counts when the entry
 * names a fork id, or a source and the directory in it, as the CLI forms the id from them: with
 * neither, the id is the skill's content, which says nothing of the skill on another machine.
 */
export const holdingOf = (
  entry: Pick<
    ReportLibrarySkill,
    | "name"
    | "logical_id"
    | "fork_id"
    | "source"
    | "subpath"
    | "content_hash"
    | "upstream_commit"
  >
): Holding => ({
  contentHash: entry.content_hash,
  logicalId:
    (entry.fork_id ?? "") !== "" ||
    ((entry.source ?? "") !== "" && entry.subpath !== undefined)
      ? entry.logical_id
      : undefined,
  name: entry.name,
  upstreamCommit: entry.upstream_commit,
});

/**
 * Every skill a machine holds: each skill of its library, placed or not, then the skills its
 * agents see from a plugin or their own skills directory, each content of a name once.
 */
const heldOf = (
  contents: ReportContents,
  agents: readonly AgentGroup[]
): HeldSkill[] => {
  const held: HeldSkill[] = contents.library.map((entry) => ({
    ...holdingOf(entry),
    from: "library",
    plugin: undefined,
    source: sourceOf(entry),
  }));
  const seen = new Set<string>();
  for (const line of agents.flatMap((a) => a.skills)) {
    const key = `${line.key}\n${line.contentHash ?? ""}`;
    if (line.from !== "library" && !seen.has(key)) {
      seen.add(key);
      held.push({
        contentHash: line.contentHash,
        from: line.from,
        logicalId: undefined,
        name: line.name,
        plugin: line.plugin,
        source: undefined,
        upstreamCommit: undefined,
      });
    }
  }
  return held;
};

const viewOf = (machine: FleetMachine): MachineView => {
  const agents = agentsOf(machine);
  return {
    agents,
    agentx: machine.machine.agentx,
    changedAt: machine.changed_at,
    held: heldOf(machine, agents),
    id: machine.machine.id,
    kind: machine.machine.id === CLOUD_ID ? "cloud" : "other",
    label: machine.machine.label,
    os: machine.machine.os,
    skillCount: skillCountOf(agents),
  };
};

/**
 * The machines the screen lists: this machine first, from the applied inventory, under the label
 * and id of `machine` once the app has read them; then the other machines the hub holds, by label;
 * then the cloud machine. The report this machine wrote to the hub is no row of its own: its row
 * shows it as it is now.
 */
export const projectMachines = (
  inventory: InventoryEvent | undefined,
  machine: Machine | undefined,
  fleet: readonly FleetMachine[] | undefined
): MachineView[] => {
  const self = machine ?? inventory?.machine;
  const views: MachineView[] = [];
  if (inventory !== undefined && self !== undefined) {
    const contents = reportOf(inventory);
    const agents = agentsOf(contents);
    views.push({
      agents,
      agentx: undefined,
      changedAt: undefined,
      held: heldOf(contents, agents),
      id: self.id,
      kind: "this",
      label: self.label,
      os: undefined,
      skillCount: skillCountOf(agents),
    });
  }
  const held = (fleet ?? [])
    .filter((m) => m.machine.id !== self?.id)
    .map(viewOf);
  return [
    ...views,
    ...sorted(
      held.filter((m) => m.kind === "other"),
      (a, b) =>
        a.label.localeCompare(b.label, undefined, { sensitivity: "base" }) ||
        a.id.localeCompare(b.id)
    ),
    ...held.filter((m) => m.kind === "cloud"),
  ];
};

/** The name a machine goes by: its label, or what the glossary calls the cloud machine. */
export const nameOf = (machine: Pick<MachineView, "kind" | "label">) =>
  machine.kind === "cloud" ? "Cloud machine" : machine.label;

/** What brings a skill to a machine, in the app's words. */
export const fromText = (skill: SkillOrigin) => {
  if (skill.from === "library") {
    return "Library";
  }
  return skill.from === "plugin"
    ? `${skill.plugin ?? ""} plugin`
    : "Skills directory";
};

/** Where a library skill is published: the hub, with a fork's upstream, or its source. */
export const sourceText = ({ source }: SkillOrigin) => {
  if (source === undefined) {
    return "";
  }
  if (!source.hub) {
    return source.short;
  }
  return source.short === "" ? "Hub" : `Hub, from ${source.short}`;
};

/** The two in one: where a library skill is published when it is, else what brings the skill. */
export const originText = (skill: SkillOrigin) =>
  sourceText(skill) || fromText(skill);

/** The names the operating systems go by, for the ones a report names by another word. */
const osNames: Record<string, string> = {
  darwin: "macOS",
  linux: "Linux",
  windows: "Windows",
};

/** A reported operating system by its usual name, or as the report has it. */
export const osName = (os: string) => osNames[os] ?? os;

const DAY = 24 * 60 * 60 * 1000;

/**
 * When a machine's report last changed, for its row: how long ago within a month, else its date.
 * Undefined when the CLI's date does not parse.
 */
export const changedWhen = (time: string, now: number): string | undefined => {
  const at = Date.parse(time);
  if (Number.isNaN(at)) {
    return undefined;
  }
  return now - at < 30 * DAY ? formatAgo(time, now) : formatDate(time);
};

/** The same in full, its date and time, for the detail pane. */
export const changedInFull = (time: string) => {
  const date = new Date(time);
  return Number.isNaN(date.getTime())
    ? time
    : date.toLocaleString(undefined, {
        dateStyle: "medium",
        timeStyle: "short",
      });
};
