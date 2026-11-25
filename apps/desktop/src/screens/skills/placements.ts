// What the AGENTS chips, the banners and the Remove dialog say about a skill's placements, the
// `skill import` command line that takes over an unmanaged skill, and what the toasts after their
// commands say: pure functions over a row, the configurations and a command's events, so that
// every control reads a placement the same way. Nothing here predicts: a chip's state comes from
// the inventory, and a count from the CLI's result.
import type {
  CliEvent,
  Configuration,
  LibraryPlacement,
  Occurrence,
} from "#/cli/events.gen.ts";
import type { AgentChipState } from "#/components/agent-chip.tsx";
import { plural } from "#/lib/plural.ts";
import { directoryName, occurrenceOf } from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";

/**
 * Whether a configuration sees the skill: through its placement, displaced when the placement's
 * kind on disk is not the one agentx keeps for it (drift `displaced`), or through an occurrence.
 */
export const chipStateOf = (
  row: SkillRow,
  c: Configuration
): AgentChipState => {
  const placement = row.entry?.placements.find((p) => p.configuration === c.id);
  if (placement !== undefined) {
    return placement.kind === placement.mode ? "on" : "displaced";
  }
  return row.occurrences.some((o) => o.configuration === c.id) ? "on" : "off";
};

/**
 * Why the switch of a chip in `state` is disabled, or undefined when a click changes what the agent
 * sees: a displaced placement is put back from the banner first.
 */
export const inertReason = (
  row: SkillRow,
  c: Configuration,
  state: AgentChipState
): string | undefined =>
  state === "displaced"
    ? `${c.name} owns a copy of ${row.name}, so your library changes don't reach it. Reconnect it from the banner to manage it here`
    : undefined;

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
  state: AgentChipState
): string => {
  const locked = lockReason(row);
  if (locked !== undefined) {
    return locked;
  }
  if (state === "off") {
    return `Add to ${c.name}`;
  }
  return inertReason(row, c, state) ?? `Remove from ${c.name}`;
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

/**
 * The name `skill remove` takes a skill by: a library skill's, or, for a skill only agents' own
 * skills folders hold, the name of its folder, unless the library holds another skill under it.
 * Undefined for a skill no removal takes: a plugin's, a project's, or a link in an agent's folder.
 */
export const removeName = (row: SkillRow): string | undefined => {
  if (row.entry !== undefined) {
    return row.name;
  }
  if (row.kind !== "disk" || row.nameInLibrary === true) {
    return undefined;
  }
  const occurrence = occurrenceOf(row);
  return occurrence?.kind === "directory"
    ? directoryName(occurrence.path)
    : undefined;
};

/** `skill remove <name>`, with a name that starts with a dash after `--`. */
export const removeArgs = (name: string): string[] =>
  name.startsWith("-")
    ? ["skill", "remove", "--", name]
    : ["skill", "remove", name];

/**
 * What `skill remove <name>` does to the agents that see a skill: it deletes the library entry,
 * each placement agentx keeps as it made it, and every folder of the skill under its name in an
 * agent's own skills folder, the folders holding the skill's files. A link of the user's and a
 * folder under another name stay; a link into what goes no longer reaches the skill. `losing` are the agents that see the skill now and nowhere
 * else, in inventory order; `keeping` those that still see it in a place that stays.
 */
export const removalOf = (
  row: SkillRow,
  configurations: Configuration[]
): { losing: Configuration[]; keeping: Configuration[] } => {
  const name = removeName(row);
  const deleted = new Set(
    (row.entry?.placements ?? [])
      .filter((p) => p.mode === "library" || p.kind === p.mode)
      .map((p) => p.path)
  );
  const goes = (o: Occurrence) =>
    deleted.has(o.path) ||
    (o.kind === "directory" && directoryName(o.path) === name);
  const gone = new Set(
    row.occurrences.filter(goes).map((o) => o.resolved_path)
  );
  const stays = (o: Occurrence) =>
    !goes(o) && !(o.kind === "symlink" && gone.has(o.resolved_path));
  const keeps = new Set(
    row.occurrences
      .filter((o) => o.scope === "user" && o.plugin === undefined && stays(o))
      .map((o) => o.configuration)
  );
  const sees = new Set([
    ...row.agents,
    ...row.occurrences
      .filter((o) => o.scope === "user" && o.plugin === undefined)
      .map((o) => o.configuration),
  ]);
  return {
    keeping: configurations.filter((c) => keeps.has(c.id)),
    losing: configurations.filter((c) => sees.has(c.id) && !keeps.has(c.id)),
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
