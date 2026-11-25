// What the Skills screen shows, projected from the store and nothing else: one row per skill on
// the machine, grouped by where it comes from, with what the list and the detail pane say about
// it. Pure functions over the applied inventory and the store's slices, so that every later change
// to the screen reads a skill the same way. Nothing here predicts: a row changes only when the
// inventory does.
import type {
  AdoptionEvent,
  Configuration,
  InventoryEvent,
  LibraryCandidate,
  LibraryEntry,
  LibraryHubAhead,
  Occurrence,
  Plugin,
  SkillNode,
} from "#/cli/events.gen.ts";
import { formatDate } from "#/lib/dates.ts";

/** Where a skill comes from, which decides its group, glyph, origin and Details. */
export type SkillKindName = "hub" | "source" | "plugin" | "disk";

/** A row's one status pill: a hub skill not yet published. */
export type PillKind = "not-published";

export interface Pill {
  kind: PillKind;
  label: string;
}

export interface SkillRow {
  /** The row's own key: a library entry's name, or a skill node's physical id. */
  key: string;
  name: string;
  description: string;
  kind: SkillKindName;
  /** The group the row is in; the key of a Group. */
  group: string;
  entry?: LibraryEntry;
  node?: SkillNode;
  /** Every occurrence of the skill on the machine, from its node. */
  occurrences: Occurrence[];
  /** Seen by at least one agent client. */
  enabled: boolean;
  /** Edited since its base version, or edits not yet published. */
  modified: boolean;
  /** A hub skill whose branch the hub holds. */
  published: boolean;
  pills: Pill[];
  /** The configurations that see the skill, in inventory order. */
  agents: string[];
  /** Whether a universal client sees it: it is in the library. */
  universal: boolean;
  /** An update is known for it. */
  hasUpdate: boolean;
  /**
   * The update the app offers for it, which its banner's Update applies. Absent for a skill with a
   * pending merge: finishing that merge comes first.
   */
  update?: UpdateOffer;
  /** The plugin that provides it, for a plugin's skill. */
  plugin?: Plugin;
  /** The one adoption entry that names it, for an unmanaged skill another tool installed. */
  adoption?: AdoptionEvent;
  /** The name of the skill this one was forked from, when that skill is in the library too. */
  forkedFrom?: string;
  /**
   * For a skill a client holds in its own skills directory: whether the library holds a skill
   * under the name of that directory, the one `occurrenceOf` chooses. That is another skill.
   */
  nameInLibrary?: boolean;
}

/** The skills of one plugin, under their own header in the PLUGINS group. */
export interface Subgroup {
  key: string;
  label: string;
  /** The marketplace the plugin comes from, which its header shows; none for a local plugin. */
  marketplace?: string;
  rows: SkillRow[];
}

export interface Group {
  key: string;
  label: string;
  /** The full URL of a source's group, which its header's tooltip shows. */
  source?: string;
  /** Every row of the group, in the order the list shows them: subgroup by subgroup when it has them. */
  rows: SkillRow[];
  /** The PLUGINS group's skills by plugin, A to Z by the plugin's name. */
  subgroups?: Subgroup[];
}

/** How rows sort within a group: by name alone, A to Z or Z to A, whatever the state. */
export type SortOrder = "asc" | "desc";

/** The order the sort toggle steps to: A to Z and Z to A, in turn. */
export const nextOrder = (order: SortOrder): SortOrder =>
  order === "asc" ? "desc" : "asc";

const GITHUB_OR_GITLAB = /github|gitlab/u;
const SCP_URL = /^(?:[\w.-]+@)?(?<host>[\w.-]+):(?<path>.+)$/u;

/** The host and path of a source URL, HTTPS or SSH; undefined for anything else. */
export const hostAndPath = (
  url: string
): { host: string; path: string } | undefined => {
  if (url.includes("://")) {
    try {
      const parsed = new URL(url);
      return { host: parsed.hostname, path: parsed.pathname };
    } catch {
      return undefined;
    }
  }
  const scp = SCP_URL.exec(url);
  if (scp?.groups === undefined) {
    return undefined;
  }
  return { host: scp.groups.host ?? "", path: scp.groups.path ?? "" };
};

/**
 * A source's short form: `owner/name` for a GitHub or GitLab URL, HTTPS or SSH, else the URL as
 * the CLI gave it.
 */
export const shortSource = (url: string): string => {
  const parsed = hostAndPath(url);
  if (parsed === undefined || !GITHUB_OR_GITLAB.test(parsed.host)) {
    return url;
  }
  const [owner, name] = parsed.path
    .replace(/\.git$/u, "")
    .split("/")
    .filter((s) => s.length > 0);
  return owner !== undefined && name !== undefined ? `${owner}/${name}` : url;
};

/** The first seven characters of a commit or fork id. */
export const shortId = (id: string) => id.slice(0, 7);

/**
 * The one update of a library skill, as its inventory entry says it (docs/spec/cli-contract.md,
 * `library_skill`): what another of your machines published to the hub that a hub skill lacks,
 * else the newer version the last update check found, from its source or its upstream. A hub
 * skill takes one update at a time, the hub's first, so it has `hub` or `upstream`, never both.
 */
export interface UpdateOffer {
  name: string;
  entry: LibraryEntry;
  /** What another machine published to the hub. */
  hub?: LibraryHubAhead;
  /** The newer version from the skill's source, or a hub skill's upstream. */
  upstream?: LibraryCandidate;
}

/** The update the inventory entry offers, if any: what the hub holds first, else the candidate. */
export const offerOf = (entry: LibraryEntry): UpdateOffer | undefined => {
  if (entry.hub_ahead !== undefined) {
    return { entry, hub: entry.hub_ahead, name: entry.name };
  }
  return entry.candidate === undefined
    ? undefined
    : { entry, name: entry.name, upstream: entry.candidate };
};

/** Whether an update is what another of your machines published to a hub skill. */
export const fromAnotherMachine = (update: UpdateOffer) =>
  update.hub !== undefined;

/** The newer version an update brings, by its date when the CLI dates it. */
export const newerTime = (update: UpdateOffer) =>
  update.hub?.time ?? update.upstream?.upstream_time;

/**
 * The two versions of an update, base then newer, each by its date when the CLI dates it, else by
 * its short id. An update from another machine has no base version, so it names the newer alone.
 */
export const updateVersions = (update: UpdateOffer): string => {
  const time = newerTime(update);
  const newer =
    time === undefined
      ? shortId(update.hub?.commit ?? update.upstream?.upstream_commit ?? "")
      : formatDate(time);
  if (fromAnotherMachine(update)) {
    return newer;
  }
  const { upstream_commit: commit, upstream_time: base } = update.entry;
  return `${base === undefined ? shortId(commit ?? "") : formatDate(base)} → ${newer}`;
};

/**
 * What names the versions of an update, for the read of what it changes: the base version, which a
 * hub update can move under an update from upstream, and the newer one. A read of another one is
 * not this update's.
 */
export const updateKey = (update: UpdateOffer) =>
  [
    update.entry.base_hash ?? "",
    update.hub?.commit ?? "",
    update.upstream?.upstream_commit ?? "",
  ].join(":");

/** The tool an adoption record names, as the product calls it. */
export const toolName = (adoption: AdoptionEvent): string => {
  switch (adoption.tool) {
    case "vercel": {
      return "Skills CLI";
    }
    case "skillshare": {
      return "Skillshare";
    }
    default: {
      return adoption.tool;
    }
  }
};

export const GROUP_HUB = "hub";
export const GROUP_PLUGINS = "plugins";
export const GROUP_DISK = "disk";
const SOURCE_PREFIX = "source:";

const groupNames: Record<string, string> = {
  [GROUP_DISK]: "Unmanaged",
  [GROUP_PLUGINS]: "Plugins",
  [GROUP_HUB]: "Hub",
};

/** The name of a group: a source's short form, or the fixed ones, which SHOW offers as choices. */
export const groupName = (key: string) =>
  groupNames[key] ?? shortSource(key.slice(SOURCE_PREFIX.length));

/** The caption of a group: its name, uppercase like every caption. */
export const groupLabel = (key: string) => groupName(key).toUpperCase();

const kindOf = (entry: LibraryEntry): SkillKindName => {
  if (entry.kind === "unmanaged") {
    return "disk";
  }
  return entry.subpath === undefined ? "hub" : "source";
};

const groupOf = (kind: SkillKindName, entry: LibraryEntry) => {
  if (kind === "hub") {
    return GROUP_HUB;
  }
  if (kind === "disk") {
    return GROUP_DISK;
  }
  return `${SOURCE_PREFIX}${entry.source ?? ""}`;
};

const uniqueInOrder = (ids: string[], order: string[]) =>
  order.filter((id) => ids.includes(id));

const pillsOf = (entry: LibraryEntry, kind: SkillKindName): Pill[] => {
  const drift = entry.drift ?? [];
  const pills: Pill[] = [];
  if (kind === "hub" && drift.includes("not published")) {
    pills.push({ kind: "not-published", label: "Not published" });
  }
  return pills;
};

/** The node a library entry is seen as: the same content, or a copy edited in place at a placement. */
const nodeOf = (entry: LibraryEntry, nodes: SkillNode[]) =>
  nodes.find((n) => n.content_hash === entry.content_hash) ??
  nodes.find((n) =>
    n.occurrences.some((o) =>
      entry.placements.some(
        (p) => p.configuration === o.configuration && p.path === o.path
      )
    )
  );

/** The skill of a shared source that a fork came from, when the library holds it. */
const originalOf = (entry: LibraryEntry, library: LibraryEntry[]) =>
  entry.upstream === undefined
    ? undefined
    : library.find(
        (e) =>
          e.kind === "managed" &&
          e.subpath !== undefined &&
          e.source === entry.upstream &&
          e.subpath === (entry.upstream_subpath ?? "")
      );

export interface ProjectionInput {
  inventory: InventoryEvent;
  adoption: AdoptionEvent[];
}

interface Context extends ProjectionInput {
  /** The ids of the configurations whose client reads the library. */
  universal: Set<string>;
  /** Configuration ids in inventory order. */
  order: string[];
  /** The names the library holds a skill under. */
  names: Set<string>;
}

const entryRow = (entry: LibraryEntry, c: Context): SkillRow => {
  const kind = kindOf(entry);
  const node = nodeOf(entry, c.inventory.skills);
  const direct = entry.placements
    .filter((p) => !c.universal.has(p.configuration))
    .map((p) => p.configuration);
  const drift = entry.drift ?? [];
  const row: SkillRow = {
    agents: uniqueInOrder([...direct, ...entry.universal], c.order),
    description: node?.description ?? "",
    enabled: direct.length > 0 || entry.universal.length > 0,
    entry,
    group: groupOf(kind, entry),
    hasUpdate: offerOf(entry) !== undefined,
    key: entry.name,
    kind,
    modified: entry.state === "modified",
    name: entry.name,
    occurrences: node?.occurrences ?? [],
    pills: pillsOf(entry, kind),
    published: kind === "hub" && !drift.includes("not published"),
    universal: true,
  };
  if (node !== undefined) {
    row.node = node;
  }
  const update = offerOf(entry);
  if (
    update !== undefined &&
    entry.kind === "managed" &&
    entry.pending_merge !== true
  ) {
    row.update = update;
  }
  const adopted = c.adoption.find((a) => a.name === entry.name);
  if (kind === "disk" && adopted !== undefined) {
    row.adoption = adopted;
  }
  const original =
    kind === "hub" ? originalOf(entry, c.inventory.library) : undefined;
  if (original !== undefined) {
    row.forkedFrom = original.name;
  }
  return row;
};

/** The last segment of an occurrence's path: the skill directory's own name. */
const LAST_SEGMENT = /(?<name>[^/]+)\/*$/u;
export const directoryName = (path: string) =>
  LAST_SEGMENT.exec(path)?.groups?.name ?? "";

/**
 * The one occurrence a plugin's skill or a skill a client holds on its own is read and shown
 * from, so that `skill files` and Show in Finder meet the same copy: among the user-scope
 * occurrences, a plugin's for a plugin's skill, in the CLI's order (configuration, then path),
 * a real directory first. Undefined when there is none.
 */
export const occurrenceOf = (row: SkillRow): Occurrence | undefined => {
  const candidates = row.occurrences.filter(
    (o) =>
      o.scope === "user" && (row.kind === "plugin") === (o.plugin !== undefined)
  );
  candidates.sort(
    (a, b) =>
      a.configuration.localeCompare(b.configuration) ||
      a.path.localeCompare(b.path)
  );
  return candidates.find((o) => o.kind === "directory") ?? candidates[0];
};

/** A skill node no library entry accounts for: a plugin's skill, or one found in a client's directory. */
const nodeRow = (node: SkillNode, c: Context): SkillRow => {
  const inPlugin = node.occurrences.every((o) => o.plugin !== undefined);
  const [first] = node.occurrences;
  const plugin = inPlugin
    ? c.inventory.plugins.find(
        (p) =>
          p.name === first?.plugin && p.configuration === first.configuration
      )
    : undefined;
  const row: SkillRow = {
    agents: uniqueInOrder(
      node.occurrences.map((o) => o.configuration),
      c.order
    ),
    description: node.description,
    enabled: true,
    group: inPlugin ? GROUP_PLUGINS : GROUP_DISK,
    hasUpdate: false,
    key: node.physical_id,
    kind: inPlugin ? "plugin" : "disk",
    modified: false,
    name: node.name,
    node,
    occurrences: node.occurrences,
    pills: [],
    published: false,
    universal: node.occurrences.some((o) => c.universal.has(o.configuration)),
  };
  if (plugin !== undefined) {
    row.plugin = plugin;
  }
  const adopted = inPlugin
    ? undefined
    : c.adoption.find((a) => a.name === node.name);
  if (adopted !== undefined) {
    row.adoption = adopted;
  }
  const held = inPlugin ? undefined : occurrenceOf(row);
  if (held !== undefined && c.names.has(directoryName(held.path))) {
    row.nameInLibrary = true;
  }
  return row;
};

/** Every skill on the machine as a row, in inventory order and ungrouped. */
export const projectRows = (input: ProjectionInput): SkillRow[] => {
  const { inventory } = input;
  const c: Context = {
    ...input,
    names: new Set(inventory.library.map((entry) => entry.name)),
    order: inventory.configurations.map((cfg) => cfg.id),
    universal: new Set(
      inventory.configurations
        .filter((cfg) => cfg.reads_library)
        .map((cfg) => cfg.id)
    ),
  };
  const rows = inventory.library.map((entry) => entryRow(entry, c));
  const taken = new Set(
    rows.map((r) => r.node?.physical_id).filter((id) => id !== undefined)
  );
  const nodes = inventory.skills.filter(
    (n) => !taken.has(n.physical_id) && n.occurrences.length > 0
  );
  return [...rows, ...nodes.map((n) => nodeRow(n, c))];
};

const compareNames = (a: SkillRow, b: SkillRow) =>
  a.name.localeCompare(b.name, undefined, { sensitivity: "base" }) ||
  a.key.localeCompare(b.key);

/** A sorted copy of `items`; `items` is left as it is. */
const sorted = <T>(items: T[], compare: (a: T, b: T) => number): T[] => {
  const copy = [...items];
  copy.sort(compare);
  return copy;
};

const comparer = (order: SortOrder) =>
  order === "asc"
    ? compareNames
    : (a: SkillRow, b: SkillRow) => compareNames(b, a);

/** The name of the plugin that provides a plugin's skill. */
const pluginNameOf = (row: SkillRow) =>
  row.plugin?.name ?? row.occurrences[0]?.plugin ?? "";

/**
 * The key of a plugin's subgroup: the plugin's name and its marketplace, so a plugin installed
 * in several agent clients is one subgroup, and two marketplaces' plugins of one name are two.
 */
export const subgroupKey = (row: SkillRow) =>
  `${GROUP_PLUGINS}:${pluginNameOf(row)}@${row.plugin?.marketplace ?? ""}`;

/** Which sections of the list are collapsed: groups are open but the ones in `collapsed`, plugins' subgroups collapsed but the ones in `expanded`. */
export interface Sections {
  collapsed: ReadonlySet<string>;
  expanded: ReadonlySet<string>;
}

/** Whether a row is in a group or a plugin's subgroup that is collapsed, so the list hides it. */
export const inCollapsed = (row: SkillRow, sections: Sections) =>
  sections.collapsed.has(row.group) ||
  (row.group === GROUP_PLUGINS && !sections.expanded.has(subgroupKey(row)));

/** A plugin group's skills by plugin, A to Z by the plugin's name; each keeps the order of `rows`. */
const pluginSubgroups = (rows: SkillRow[]): Subgroup[] => {
  const byKey = new Map<string, Subgroup>();
  for (const row of rows) {
    const key = subgroupKey(row);
    let subgroup = byKey.get(key);
    if (subgroup === undefined) {
      const marketplace = row.plugin?.marketplace;
      subgroup = {
        key,
        label: pluginNameOf(row).toUpperCase(),
        rows: [],
        ...(marketplace === undefined || marketplace === ""
          ? {}
          : { marketplace }),
      };
      byKey.set(key, subgroup);
    }
    subgroup.rows.push(row);
  }
  return sorted(
    [...byKey.values()],
    (a, b) => a.label.localeCompare(b.label) || a.key.localeCompare(b.key)
  );
};

/**
 * The rows in groups: HUB, each source A to Z by its short form, PLUGINS, then UNMANAGED,
 * each sorted in `order`. PLUGINS holds one subgroup per plugin. A group with no row is left out.
 */
export const groupRows = (rows: SkillRow[], order: SortOrder): Group[] => {
  const keys = [...new Set(rows.map((r) => r.group))];
  const sources = sorted(
    keys.filter((k) => k.startsWith(SOURCE_PREFIX)),
    (a, b) => groupLabel(a).localeCompare(groupLabel(b))
  );
  const ordered = [GROUP_HUB, ...sources, GROUP_PLUGINS, GROUP_DISK].filter(
    (k) => keys.includes(k)
  );
  return ordered.map((key) => {
    const inGroup = sorted(
      rows.filter((r) => r.group === key),
      comparer(order)
    );
    const group: Group = { key, label: groupLabel(key), rows: inGroup };
    if (key.startsWith(SOURCE_PREFIX)) {
      group.source = key.slice(SOURCE_PREFIX.length);
    }
    if (key === GROUP_PLUGINS) {
      group.subgroups = pluginSubgroups(inGroup);
      group.rows = group.subgroups.flatMap((s) => s.rows);
    }
    return group;
  });
};

/** The rows forked from `row` under a new name: the original carries a marker for each. */
export const forksOf = (row: Pick<SkillRow, "name">, rows: SkillRow[]) =>
  sorted(
    rows.filter((r) => r.forkedFrom === row.name),
    compareNames
  );

export type Show = "all" | "hub" | "plugins" | "unmanaged" | "updates";
export type State = "all" | "enabled" | "disabled";
/** The id of the Universal choice among the agent filters. */
export const UNIVERSAL = "universal";

export interface Filters {
  /** A case-insensitive substring of the name. */
  name: string;
  show: Show;
  state: State;
  /** Configuration ids, and UNIVERSAL for the clients that read the library. */
  agents: string[];
}

export const noFilters: Filters = {
  agents: [],
  name: "",
  show: "all",
  state: "all",
};

/** How many filters but the name are set, which the Filter button shows. */
export const filterCount = (f: Filters) =>
  (f.show === "all" ? 0 : 1) +
  (f.state === "all" ? 0 : 1) +
  (f.agents.length > 0 ? 1 : 0);

/** The group each SHOW choice keeps, for the choices that keep one group. */
const showGroups: Partial<Record<Show, string>> = {
  hub: GROUP_HUB,
  plugins: GROUP_PLUGINS,
  unmanaged: GROUP_DISK,
};

const matchesShow = (row: SkillRow, show: Show) => {
  if (show === "all") {
    return true;
  }
  if (show === "updates") {
    return row.hasUpdate;
  }
  return row.group === showGroups[show];
};

const matchesState = (row: SkillRow, state: State) => {
  switch (state) {
    case "all": {
      return true;
    }
    case "enabled": {
      return row.enabled;
    }
    case "disabled": {
      return !row.enabled;
    }
    default: {
      return true;
    }
  }
};

const matchesAgents = (row: SkillRow, agents: string[]) =>
  agents.length === 0 ||
  agents.some((id) =>
    id === UNIVERSAL ? row.universal : row.agents.includes(id)
  );

/** The rows that pass every filter. */
export const filterRows = (rows: SkillRow[], f: Filters) => {
  const needle = f.name.trim().toLowerCase();
  return rows.filter(
    (row) =>
      (needle === "" || row.name.toLowerCase().includes(needle)) &&
      matchesShow(row, f.show) &&
      matchesState(row, f.state) &&
      matchesAgents(row, f.agents)
  );
};

/** How many rows each SHOW choice would leave, with the other filters as they are. */
export const showCounts = (
  rows: SkillRow[],
  f: Filters
): Record<Show, number> => {
  const count = (show: Show) => filterRows(rows, { ...f, show }).length;
  return {
    all: count("all"),
    hub: count("hub"),
    plugins: count("plugins"),
    unmanaged: count("unmanaged"),
    updates: count("updates"),
  };
};

/** Where a skill comes from, in the words of the detail header. */
export const originOf = (row: SkillRow): string => {
  switch (row.kind) {
    case "source": {
      return shortSource(row.entry?.source ?? "");
    }
    case "hub": {
      const upstream = row.entry?.upstream;
      return upstream === undefined
        ? "hub skill"
        : `from ${shortSource(upstream)}`;
    }
    case "plugin": {
      return `${row.plugin?.name ?? row.occurrences[0]?.plugin ?? ""} plugin`;
    }
    case "disk": {
      return row.adoption === undefined
        ? "found on disk"
        : toolName(row.adoption);
    }
    default: {
      return "";
    }
  }
};

const kindWords: Record<SkillKindName, string> = {
  disk: "Unmanaged",
  hub: "Hub skill",
  plugin: "Plugin skill",
  source: "Skill",
};

/** What the glyph's title says: the skill's kind and whether an agent client sees it. */
export const glyphTitle = (row: SkillRow): string => {
  const what = row.modified ? "Edited" : kindWords[row.kind];
  const where = row.enabled ? "Enabled" : "Not in any agent";
  return `${what} · ${where}`;
};

/** The glyph a row leads with. */
export type Glyph = "dot" | "pencil" | "fork";

export const glyphOf = (row: SkillRow): Glyph => {
  if (row.modified) {
    return "pencil";
  }
  return row.kind === "hub" ? "fork" : "dot";
};

/** The reads that show a skill's files; see FilesRequest in src/store/loaders.ts. */
export interface FilesRequest {
  args: string[];
  diff?: string;
}

/**
 * How `skill files` names a skill, and whether `skill diff` is read for it: a library skill by its
 * name, a plugin's as `<plugin>:<directory>`, and a skill a client holds in its own skills
 * directory, which is neither, by its directory with `--configuration`. The CLI finds a plugin's
 * or a configuration's skill by its directory's name, not the name its frontmatter gives, so the
 * occurrence `occurrenceOf` chooses names it. Only a modified skill of a source or a hub
 * skill with edits not published has a diff. Undefined when the CLI has no way to name it.
 */
export const filesRequestOf = (row: SkillRow): FilesRequest | undefined => {
  if (row.entry !== undefined && row.kind !== "plugin") {
    return row.modified && row.kind !== "disk"
      ? { args: [row.name], diff: row.name }
      : { args: [row.name] };
  }
  const occurrence = occurrenceOf(row);
  if (occurrence === undefined) {
    return undefined;
  }
  const directory = directoryName(occurrence.path);
  if (row.kind === "plugin") {
    return { args: [`${occurrence.plugin ?? ""}:${directory}`] };
  }
  return { args: [directory, "--configuration", occurrence.configuration] };
};

/** One place a skill is on disk, which Finder can open. */
export interface SkillLocation {
  key: string;
  /**
   * "Library", or the agent client's name, with the plugin for a plugin's copy; for symlinks of
   * several agent clients that lead to one place, their names joined with commas.
   */
  label: string;
  /** The agent clients at this place, in the row's order: none for the library. */
  clients: Pick<Configuration, "client" | "name">[];
  /** The path as the inventory carries it: for a symlink, the link itself. */
  path: string;
  /** The directory Finder opens: where the place resolves. */
  directory: string;
  /** Whether the place is a symlink, which the menu shows by where it leads, `directory`. */
  symlink: boolean;
  /** Whether the place is a symlink into the library, which the menu calls the library. */
  intoLibrary: boolean;
}

/**
 * Every place a skill is on disk, the one Show in Finder opens first: for a skill the library
 * holds, the library entry, `<library>/<name>`, then each placement in a client's own skills
 * directory, one whose skills directory links to the library included; for a skill a client holds on its own, or a plugin's, each occurrence. Placements
 * and occurrences come in the order the row lists its agents, then by path. Symlinks that lead
 * to one place are one place, at the first one's position, with each agent client once: the
 * menu shows that place once, with the agents that use it, and calls a symlink into the library
 * the library. `library` is where agentx keeps the
 * library, from `machine`; without it a library skill's placements are all.
 */
export const locationsOf = (
  row: SkillRow,
  configurations: Configuration[],
  library: string | undefined
): SkillLocation[] => {
  const client = (id: string) => {
    const c = configurations.find((x) => x.id === id);
    return { client: c?.client ?? id, name: c?.name ?? id };
  };
  const name = (id: string) => client(id).name;
  const rank = (id: string) => {
    const at = row.agents.indexOf(id);
    return at === -1 ? row.agents.length : at;
  };
  const locations: SkillLocation[] = [];
  const libraryDirectory =
    library === undefined ? undefined : `${library}/${row.name}`;
  const add = (place: Omit<SkillLocation, "intoLibrary">) => {
    const location = {
      ...place,
      intoLibrary: place.symlink && place.directory === libraryDirectory,
    };
    const at = locations.findIndex(
      (l) => location.symlink && l.symlink && l.directory === location.directory
    );
    const same = locations[at];
    if (same === undefined) {
      locations.push(location);
      return;
    }
    const clients = [
      ...same.clients,
      ...location.clients.filter(
        (c) => !same.clients.some((s) => s.name === c.name)
      ),
    ];
    locations[at] = {
      ...same,
      clients,
      label: clients.map((c) => c.name).join(", "),
    };
  };
  if (row.entry !== undefined && library !== undefined) {
    add({
      clients: [],
      directory: `${library}/${row.name}`,
      key: "library",
      label: "Library",
      path: `${library}/${row.name}`,
      symlink: false,
    });
  }
  if (row.entry === undefined) {
    const occurrences = [...row.occurrences];
    occurrences.sort(
      (a, b) =>
        rank(a.configuration) - rank(b.configuration) ||
        a.path.localeCompare(b.path)
    );
    for (const o of occurrences) {
      add({
        clients: [client(o.configuration)],
        directory: o.resolved_path,
        key: o.id,
        label:
          o.plugin === undefined
            ? name(o.configuration)
            : `${name(o.configuration)} (${o.plugin} plugin)`,
        path: o.path,
        symlink: o.kind === "symlink",
      });
    }
    return locations;
  }
  const reads = new Set(
    configurations.filter((c) => c.reads_library).map((c) => c.id)
  );
  const placements = row.entry.placements.filter(
    (p) => p.mode !== "library" || !reads.has(p.configuration)
  );
  placements.sort(
    (a, b) =>
      rank(a.configuration) - rank(b.configuration) ||
      a.path.localeCompare(b.path)
  );
  for (const p of placements) {
    const through = row.occurrences.find(
      (o) => o.configuration === p.configuration && o.path === p.path
    );
    const directory = through?.resolved_path ?? p.path;
    add({
      clients: [client(p.configuration)],
      directory,
      key: `${p.configuration}:${p.path}`,
      label: name(p.configuration),
      path: p.path,
      symlink:
        p.kind === "symlink" || (p.mode === "library" && directory !== p.path),
    });
  }
  return locations;
};

/**
 * The version a skill's files are read for: a new read when its content, its state or the base its
 * edits are compared with changed.
 */
export const filesVersionOf = (row: SkillRow) =>
  [
    row.entry?.content_hash ?? row.node?.content_hash ?? "",
    row.entry?.state ?? "",
    row.entry?.base_hash ?? "",
  ].join(":");
