// What the AGENTS chips, the banners and the Remove dialog say about a skill's placements, and
// what the toasts after their commands say: pure functions over a row, the configurations and a
// command's events, so that every control reads a placement the same way. Nothing here predicts:
// a chip's state comes from the inventory, and a count from the CLI's result.
import type {
  CliEvent,
  Configuration,
  LibraryPlacement,
} from "#/cli/events.gen.ts";
import type { AgentChipState } from "#/components/agent-chip.tsx";
import { plural } from "#/lib/plural.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";

/**
 * The configurations whose own directory is the most specific one that holds `path`: usually one,
 * two when they share a directory, as Zencoder and Zenflow do, and none when it is in no agent's
 * own directory. The most specific wins, since one agent's directory can sit inside another's, as
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

/** `a`, `a and b`, `a, b and c`. */
const andList = (words: string[]) =>
  words.length < 2
    ? words.join("")
    : `${words.slice(0, -1).join(", ")} and ${words.at(-1) ?? ""}`;

/**
 * The configurations that lose the skill with `c`'s placement: those that see it only through
 * `c`'s directory, as Cursor sees a skill placed in Claude Code alone, or Zenflow one in the
 * directory it shares with Zencoder.
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

/**
 * Why no chip of the skill can be changed, or undefined when they can: a plugin's skill belongs
 * to its plugin, and an unmanaged skill is not agentx's to place until it is forked, or adopted
 * when another tool installed it.
 */
export const lockReason = (row: SkillRow): string | undefined => {
  switch (row.kind) {
    case "plugin": {
      const plugin = row.plugin?.name ?? row.occurrences[0]?.plugin ?? "";
      return `Comes with the ${plugin} plugin. Fork it to manage it here`;
    }
    case "disk": {
      return row.adoption?.state === "candidate"
        ? "Adopt it first, then agentx can place it"
        : "agentx doesn't manage it. Fork it to manage it here";
    }
    case "source":
    case "yours": {
      return undefined;
    }
    default: {
      return undefined;
    }
  }
};

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
