// What the AGENTS chips, the banners and the Remove dialog say about a skill's placements, the
// `skill import` command line that takes over an unmanaged skill, and what the toasts after their
// commands say: pure functions over a row, the configurations and a command's events, so that
// every control reads a placement the same way. Nothing here predicts: a chip's state comes from
// the inventory, and a count from the CLI's result.
import type {
  CliEvent,
  Configuration,
  LibraryPlacement,
} from "#/cli/events.gen.ts";
import type { AgentChipState } from "#/components/agent-chip.tsx";
import { andList, plural } from "#/lib/plural.ts";
import { directoryName, occurrenceOf } from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";

/**
 * The configurations whose own directory is the most specific one that holds `path`: usually one,
 * two when they share a directory, one agent's a symlink to the other's, and none when it is in
 * no agent's own directory. The most specific wins, since one agent's directory can sit inside another's, as
 * Antigravity's `~/.gemini/antigravity` sits inside Gemini CLI's `~/.gemini`. A client that reads
 * the library places nothing in its own directory, so it owns no path, as the CLI judges it.
 */
const ownersOf = (path: string, configurations: Configuration[]) => {
  const holding = configurations.filter(
    (c) => !c.reads_library && path.startsWith(`${c.path}/`)
  );
  const depth = Math.max(0, ...holding.map((c) => c.path.length));
  return holding.filter((c) => c.path.length === depth);
};

/** The owners of each place `c` sees the skill at, at user scope and outside any plugin. */
const ownersSeenBy = (
  row: SkillRow,
  c: Configuration,
  configurations: Configuration[]
) =>
  row.occurrences
    .filter(
      (o) =>
        o.configuration === c.id && o.scope === "user" && o.plugin === undefined
    )
    .map((o) => ownersOf(o.path, configurations));

/**
 * The configurations `c` sees the skill through and nothing else: every other client whose
 * directory holds what `c` sees, in inventory order, or none when `c` sees it at a place of its
 * own, at a place no agent owns, or not at all. Cursor reads Claude Code's skills directory, so a
 * skill placed in Claude Code alone is inherited from it. A plugin's skill and a project's are not
 * placements, so they never count.
 */
export const inheritedFrom = (
  row: SkillRow,
  c: Configuration,
  configurations: Configuration[]
): Configuration[] => {
  const owners = ownersSeenBy(row, c, configurations);
  if (
    owners.length === 0 ||
    owners.some((each) => each.length === 0 || each.some((o) => o.id === c.id))
  ) {
    return [];
  }
  return configurations.filter((other) =>
    owners.some((each) => each.some((o) => o.id === other.id))
  );
};

/**
 * Whether a configuration sees the skill: through its placement, displaced when the placement's
 * kind on disk is not the one agentx keeps for it (drift `displaced`), inherited when it sees the
 * skill only through another client's placement, or through an occurrence of its own.
 */
export const chipStateOf = (
  row: SkillRow,
  c: Configuration,
  configurations: Configuration[]
): AgentChipState => {
  const placement = row.entry?.placements.find((p) => p.configuration === c.id);
  if (placement !== undefined) {
    return placement.kind === placement.mode ? "on" : "displaced";
  }
  if (!row.occurrences.some((o) => o.configuration === c.id)) {
    return "off";
  }
  return inheritedFrom(row, c, configurations).length > 0 ? "inherited" : "on";
};

/**
 * The configurations that lose the skill with `c`'s placement: those that see it only through
 * `c`'s directory, as Cursor sees a skill placed in Claude Code alone, or an agent whose skills
 * directory is a symlink to `c`'s sees one placed there.
 */
export const dependantsOf = (
  row: SkillRow,
  c: Configuration,
  configurations: Configuration[]
): Configuration[] =>
  configurations.filter((other) => {
    if (other.id === c.id || other.reads_library) {
      return false;
    }
    const owners = ownersSeenBy(row, other, configurations);
    return (
      owners.length > 0 &&
      owners.every((each) => each.some((o) => o.id === c.id))
    );
  });

/** The names a removal from `c` takes the skill from: `c` and every agent that sees it through `c`. */
export const removedFrom = (
  row: SkillRow,
  c: Configuration,
  configurations: Configuration[]
) => andList([c, ...dependantsOf(row, c, configurations)].map((d) => d.name));

/**
 * Why the switch of a chip in `state` is disabled, or undefined when a click changes what the agent
 * sees: a displaced placement is put back from the banner first, and an inherited one goes with
 * the placement it comes through.
 */
export const inertReason = (
  row: SkillRow,
  c: Configuration,
  state: AgentChipState,
  configurations: Configuration[]
): string | undefined => {
  switch (state) {
    case "displaced": {
      return `${c.name} owns a copy of ${row.name}, so your library changes don't reach it. Reconnect it from the banner to manage it here`;
    }
    case "inherited": {
      const from = inheritedFrom(row, c, configurations);
      const names = andList(from.map((f) => f.name));
      const folders = from.length > 1 ? "skills folders" : "skills folder";
      const all = from.length > 1 ? "all of them" : "both";
      const owners = andList(from.map((f) => `${f.name}'s`));
      return `${c.name} reads ${owners} ${folders}, so it sees ${row.name} there. Remove it from ${names} to remove it from ${all}`;
    }
    case "off":
    case "on": {
      return undefined;
    }
    default: {
      return undefined;
    }
  }
};

/** `skill import <name>`. It names no tool, so the CLI decides by itself how it takes the skill. */
/** A name that starts with a dash goes after `--`, so the CLI does not read it as a flag. */
const adoptLine = (name: string): string[] =>
  name.startsWith("-")
    ? ["skill", "import", "--", name]
    : ["skill", "import", name];

/**
 * What Import does for an unmanaged skill: the command line of `adoptArgs`, or why `skill import`
 * cannot take it, with the library skill in the way when that is why. Undefined for a skill agentx
 * manages and for a plugin's skill.
 */
export type Importability =
  | { args: string[] }
  | { why: string; inTheWay?: string };

/** The CLI's reason, a clause about the skill, as a sentence of the app's. */
const asSentence = (reason: string | undefined, fallback: string) =>
  reason === undefined || reason === ""
    ? fallback
    : `${reason.charAt(0).toUpperCase()}${reason.slice(1)}`;

export const importOf = (row: SkillRow): Importability | undefined => {
  if (row.kind !== "disk") {
    return undefined;
  }
  const state = row.adoption?.state;
  if (state === "candidate") {
    return { args: adoptLine(row.name) };
  }
  if (state === "refused") {
    return { why: asSentence(row.adoption?.reason, "It can't be imported") };
  }
  if (row.entry !== undefined) {
    return state === undefined
      ? { args: adoptLine(row.name) }
      : { why: asSentence(row.adoption?.reason, "agentx already manages it") };
  }
  const occurrence = occurrenceOf(row);
  if (occurrence === undefined) {
    return { why: "It's in a project's folder, so it can't be imported" };
  }
  if (occurrence.kind !== "directory") {
    return { why: "It's a link, so it can't be imported" };
  }
  const folder = directoryName(occurrence.path);
  if (row.nameInLibrary === true) {
    return {
      inTheWay: folder,
      why: `Your library already has a skill named ${folder}, this folder's name. Import or remove that skill first`,
    };
  }
  return { args: adoptLine(folder) };
};

/**
 * The `skill import` command line that takes over an unmanaged skill, or undefined when
 * `skill import` has no way to take it:
 *
 * - a skill another tool recorded, which the CLI can adopt (`candidate`), is installed from its
 *   source, or becomes a hub skill with its files as they are when the record names none;
 * - a library skill no tool recorded becomes a hub skill in its place;
 * - a skill a client holds as a real directory in its own skills directory is named by that
 *   directory, which moves into the library, unless the library holds another skill under that
 *   name.
 *
 * A skill whose record the CLI refuses is one no form of `skill import` takes. A skill of a
 * project, a symlink in a client's skills directory and a plugin's skill are not its to take
 * either.
 */
export const adoptArgs = (row: SkillRow): string[] | undefined => {
  const result = importOf(row);
  return result !== undefined && "args" in result ? result.args : undefined;
};

/** Why no chip of a skill can be changed, and the control that unlocks them when there is one. */
export interface Lock {
  reason: string;
  /** What the toast of a locked chip offers: Adopt, for a skill `adoptArgs` has a command for. */
  action?: "adopt";
}

const UNMANAGED = "This skill is not managed by agentx";

/**
 * Why no chip of the skill can be changed, or undefined when they can: a plugin's skill belongs
 * to its plugin, and an unmanaged skill is not agentx's to place until it is adopted.
 */
export const lockOf = (row: SkillRow): Lock | undefined => {
  switch (row.kind) {
    case "plugin": {
      const plugin = row.plugin?.name ?? row.occurrences[0]?.plugin ?? "";
      return { reason: `Comes with the ${plugin} plugin. Fork to edit` };
    }
    case "disk": {
      return adoptArgs(row) === undefined
        ? { reason: UNMANAGED }
        : { action: "adopt", reason: `${UNMANAGED}. Import it to manage` };
    }
    case "source":
    case "hub": {
      return undefined;
    }
    default: {
      return undefined;
    }
  }
};

/** The reason of `lockOf`, for a chip's title. */
export const lockReason = (row: SkillRow): string | undefined =>
  lockOf(row)?.reason;

/** The chip's title: what a click does, or why it does nothing. */
export const chipTitle = (
  row: SkillRow,
  c: Configuration,
  state: AgentChipState,
  configurations: Configuration[]
): string => {
  const locked = lockReason(row);
  if (locked !== undefined) {
    return locked;
  }
  if (state === "off") {
    return `Add to ${c.name}`;
  }
  return (
    inertReason(row, c, state, configurations) ??
    `Remove from ${removedFrom(row, c, configurations)}`
  );
};

/** The Universal chip's title: a universal client reads the library, so removal is removal. */
export const universalTitle = (row: SkillRow): string =>
  lockReason(row) ??
  (row.universal
    ? "Remove from every agent"
    : "Not in your library, so no agent that reads it sees it");

/** The first placement whose kind on disk is not the one agentx keeps for it. */
export const displacedPlacement = (
  row: SkillRow
): LibraryPlacement | undefined =>
  row.entry?.placements.find((p) => p.kind !== p.mode);

/** The configurations that see the skill now, in inventory order. */
export const seenBy = (
  row: SkillRow,
  configurations: Configuration[]
): Configuration[] =>
  row.agents
    .map((id) => configurations.find((c) => c.id === id))
    .filter((c) => c !== undefined);

/**
 * What `skill remove <name>` does to the agents that see a library skill: the paths it deletes
 * are the library entry and each placement agentx keeps as it made it, and every other folder
 * of the skill stays as it is. `losing` are the agents that see the skill now and see it nowhere
 * else, in inventory order; `keeping` those that still see it in a folder that stays.
 */
export const removalOf = (
  row: SkillRow,
  configurations: Configuration[]
): { losing: Configuration[]; keeping: Configuration[] } => {
  const deleted = new Set(
    (row.entry?.placements ?? [])
      .filter((p) => p.mode === "library" || p.kind === p.mode)
      .map((p) => p.path)
  );
  const keeps = new Set(
    row.occurrences
      .filter(
        (o) =>
          o.scope === "user" && o.plugin === undefined && !deleted.has(o.path)
      )
      .map((o) => o.configuration)
  );
  return {
    keeping: configurations.filter((c) => keeps.has(c.id)),
    losing: seenBy(row, configurations).filter((c) => !keeps.has(c.id)),
  };
};

/** The label of the chip of `c` for `row`: the control's name in the runner. */
export const chipLabel = (row: SkillRow, c: Configuration) =>
  `agent ${row.key} ${c.id}`;

export const agentsWord = (n: number) => plural(n, "agent");

const summaryOf = (events: CliEvent[]) =>
  events.find((e) => e.type === "result")?.summary ?? "";

/** The number the `n` group of `pattern` matches in `text`, or undefined when it does not match. */
const firstNumber = (text: string, pattern: RegExp): number | undefined => {
  const n = pattern.exec(text)?.groups?.n;
  return n === undefined ? undefined : Number(n);
};

/**
 * How many configurations `skill place <name>` placed the skill in, from its result: `placed
 * <name> in <n> configurations`; failing that, the placements of the library_skill event that
 * are not the library itself.
 */
export const placedCount = (events: CliEvent[]): number =>
  firstNumber(summaryOf(events), /ed \S+ in (?<n>\d+) configurations?/u) ??
  (events.find((e) => e.type === "library_skill")?.placements ?? []).filter(
    (p) => p.mode !== "library"
  ).length;

/**
 * Whether `skill place <name> --to <id>` placed the skill nowhere, as its result says: `placed
 * <name> in 0 configurations`, with every placement skipped and a warning for each.
 */
export const placedNowhere = (events: CliEvent[]): boolean =>
  firstNumber(
    summaryOf(events),
    /^placed \S+ in (?<n>\d+) configurations?/u
  ) === 0;

/**
 * The toast after `config enable <id> --place-all`, from its result: `<id> is now enabled, <k>
 * skills placed[, <s> placements skipped]`; failing that, k counts the library_skill events with
 * a placement.
 */
export const placedAllText = (events: CliEvent[], agent: string): string => {
  const summary = summaryOf(events);
  const placed =
    firstNumber(summary, /(?<n>\d+) skills? placed/u) ??
    events.filter((e) => e.type === "library_skill" && e.placements.length > 0)
      .length;
  const skipped = firstNumber(summary, /(?<n>\d+) placements? skipped/u) ?? 0;
  const text = `Placed ${plural(placed, "skill")} in ${agent}`;
  return skipped > 0 ? `${text}, ${skipped} skipped` : text;
};

/**
 * The toast after `config placement <id> <mode> --convert`, from its result: `<id> now places
 * <mode>s, <k> skills converted[, <s> skipped]`. A skipped skill is a copy edited in place, which
 * the CLI keeps.
 */
export const convertedText = (
  events: CliEvent[],
  agent: string,
  mode: "symlink" | "copy"
): string => {
  const summary = summaryOf(events);
  const converted = firstNumber(summary, /(?<n>\d+) skills? converted/u) ?? 0;
  const skipped = firstNumber(summary, /(?<n>\d+) skipped/u) ?? 0;
  const text = `Converted ${plural(converted, "skill")} in ${agent} to ${mode === "copy" ? "copies" : "symlinks"}`;
  return skipped > 0
    ? `${text}. Kept ${skipped} edited ${skipped === 1 ? "copy" : "copies"}`
    : text;
};

/** The error that refuses a Reconnect because the directory holds another version (exit code 6). */
export const holdsOtherContent = (events: CliEvent[]) =>
  events.some(
    (e) =>
      e.type === "error" &&
      e.message.includes("content differs from the library")
  );
