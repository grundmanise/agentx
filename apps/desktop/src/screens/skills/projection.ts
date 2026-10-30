// What the Skills screen shows, projected from the store and nothing else: one row per skill on
// the machine, grouped by where it comes from, with what the list and the detail pane say about
// it. Pure functions over the applied inventory and the store's slices, so that every later change
// to the screen reads a skill the same way. Nothing here predicts: a row changes only when the
// inventory does.
import type {
  AdoptionEvent,
  InventoryEvent,
  LibraryEntry,
  Occurrence,
  Plugin,
  SkillNode,
  UpdateAvailableEvent,
} from "#/cli/events.gen.ts";

/** Where a skill comes from, which decides its group, glyph, origin and Details. */
export type SkillKindName = "yours" | "source" | "plugin" | "disk";

export type PillKind =
  | "not-published"
  | "removed-from-source"
  | "source-removed"
  | "update-waiting";

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
  /** One of your own skills whose branch the account remote holds. */
  published: boolean;
  pills: Pill[];
  /** The configurations that see the skill, in inventory order. */
  agents: string[];
  /** Whether a universal client sees it: it is in the library. */
  universal: boolean;
  /** An update candidate is known for it. */
  hasUpdate: boolean;
  /** The plugin that provides it, for a plugin's skill. */
  plugin?: Plugin;
  /** The one adoption entry that names it, for an unmanaged skill the skills CLI installed. */
  adoption?: AdoptionEvent;
  /** The name of the skill this one was forked from, when that skill is in the library too. */
  forkedFrom?: string;
}

export interface Group {
  key: string;
  label: string;
  rows: SkillRow[];
}

export type SortOrder = "asc" | "desc";

const GITHUB_OR_GITLAB = /github|gitlab/u;
const SCP_URL = /^(?:[\w.-]+@)?(?<host>[\w.-]+):(?<path>.+)$/u;

/** The host and path of a source URL, HTTPS or SSH; undefined for anything else. */
const hostAndPath = (
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

export const GROUP_YOURS = "yours";
export const GROUP_PLUGINS = "plugins";
export const GROUP_DISK = "disk";
const SOURCE_PREFIX = "source:";

const labels: Record<string, string> = {
  [GROUP_DISK]: "ON DISK",
  [GROUP_PLUGINS]: "PLUGINS",
  [GROUP_YOURS]: "YOUR SKILLS",
};

/** The caption of a group: a source's short form, uppercase like every caption, or the fixed ones. */
export const groupLabel = (key: string) =>
  labels[key] ?? shortSource(key.slice(SOURCE_PREFIX.length)).toUpperCase();

const kindOf = (entry: LibraryEntry): SkillKindName => {
  if (entry.kind === "unmanaged") {
    return "disk";
  }
  return entry.subpath === undefined ? "yours" : "source";
};

const groupOf = (kind: SkillKindName, entry: LibraryEntry) => {
  if (kind === "yours") {
    return GROUP_YOURS;
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
  if (kind === "yours" && drift.includes("not published")) {
    pills.push({ kind: "not-published", label: "Not published" });
  }
  if (drift.includes("upstream removed")) {
    pills.push({ kind: "removed-from-source", label: "Removed from source" });
  }
  if (drift.includes("source removed")) {
    pills.push({ kind: "source-removed", label: "Source removed" });
  }
  if (entry.pending_merge === true) {
    pills.push({ kind: "update-waiting", label: "Update waiting" });
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
  updates: Record<string, UpdateAvailableEvent>;
  adoption: AdoptionEvent[];
}

interface Context extends ProjectionInput {
  /** The ids of the configurations whose client reads the library. */
  universal: Set<string>;
  /** Configuration ids in inventory order. */
  order: string[];
}

const entryRow = (entry: LibraryEntry, c: Context): SkillRow => {
  const kind = kindOf(entry);
  const node = nodeOf(entry, c.inventory.skills);
  const direct = entry.placements
    .filter((p) => p.mode !== "library" && !c.universal.has(p.configuration))
    .map((p) => p.configuration);
  const drift = entry.drift ?? [];
  const row: SkillRow = {
    agents: uniqueInOrder([...direct, ...entry.universal], c.order),
    description: node?.description ?? "",
    enabled: direct.length > 0 || entry.universal.length > 0,
    entry,
    group: groupOf(kind, entry),
    hasUpdate:
      Object.hasOwn(c.updates, entry.name) || entry.candidate !== undefined,
    key: entry.name,
    kind,
    modified: entry.state === "modified",
    name: entry.name,
    occurrences: node?.occurrences ?? [],
    pills: pillsOf(entry, kind),
    published: kind === "yours" && !drift.includes("not published"),
    universal: true,
  };
  if (node !== undefined) {
    row.node = node;
  }
  const adopted = c.adoption.find((a) => a.name === entry.name);
  if (kind === "disk" && adopted !== undefined) {
    row.adoption = adopted;
  }
  const original =
    kind === "yours" ? originalOf(entry, c.inventory.library) : undefined;
  if (original !== undefined) {
    row.forkedFrom = original.name;
  }
  return row;
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
  return row;
};

/** Every skill on the machine as a row, in inventory order and ungrouped. */
export const projectRows = (input: ProjectionInput): SkillRow[] => {
  const { inventory } = input;
  const c: Context = {
    ...input,
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

/**
 * The rows in groups: YOUR SKILLS, each source A to Z by its short form, PLUGINS, then ON DISK,
 * each sorted by name in `order`. A group with no row is left out.
 */
export const groupRows = (rows: SkillRow[], order: SortOrder): Group[] => {
  const keys = [...new Set(rows.map((r) => r.group))];
  const sources = sorted(
    keys.filter((k) => k.startsWith(SOURCE_PREFIX)),
    (a, b) => groupLabel(a).localeCompare(groupLabel(b))
  );
  const ordered = [GROUP_YOURS, ...sources, GROUP_PLUGINS, GROUP_DISK].filter(
    (k) => keys.includes(k)
  );
  return ordered.map((key) => {
    const byName = sorted(
      rows.filter((r) => r.group === key),
      order === "asc" ? compareNames : (a, b) => compareNames(b, a)
    );
    return { key, label: groupLabel(key), rows: byName };
  });
};

/** The rows forked from `row` under a new name: the original carries a marker for each. */
export const forksOf = (row: SkillRow, rows: SkillRow[]) =>
  sorted(
    rows.filter((r) => r.forkedFrom === row.name),
    compareNames
  );

export type Show = "all" | "updates" | "yours" | "unmanaged";
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

const matchesShow = (row: SkillRow, show: Show) => {
  switch (show) {
    case "all": {
      return true;
    }
    case "updates": {
      return row.hasUpdate;
    }
    case "yours": {
      return row.kind === "yours";
    }
    case "unmanaged": {
      return row.kind === "disk";
    }
    default: {
      return true;
    }
  }
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
    unmanaged: count("unmanaged"),
    updates: count("updates"),
    yours: count("yours"),
  };
};

/** Where a skill comes from, in the words of the detail header. */
export const originOf = (row: SkillRow): string => {
  switch (row.kind) {
    case "source": {
      return shortSource(row.entry?.source ?? "");
    }
    case "yours": {
      const upstream = row.entry?.upstream;
      return upstream === undefined
        ? "your skill"
        : `from ${shortSource(upstream)}`;
    }
    case "plugin": {
      return `${row.plugin?.name ?? row.occurrences[0]?.plugin ?? ""} plugin`;
    }
    case "disk": {
      return row.adoption === undefined ? "found on disk" : "skills CLI";
    }
    default: {
      return "";
    }
  }
};

const kindWords: Record<SkillKindName, string> = {
  disk: "Unmanaged",
  plugin: "Plugin skill",
  source: "Skill",
  yours: "Your skill",
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
  return row.kind === "yours" ? "fork" : "dot";
};

/** Whether the glyph is lit: enabled, and for one of your own skills also published. */
export const glyphLit = (row: SkillRow) =>
  row.kind === "yours" ? row.enabled && row.published : row.enabled;
