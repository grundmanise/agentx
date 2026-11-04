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

/** What a click on a displaced chip toasts: the placement is put back from its banner. */
export const DISPLACED_REASON =
  "Put it back first, then manage it from your library";

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
  state: AgentChipState
): string => {
  const locked = lockReason(row);
  if (locked !== undefined) {
    return locked;
  }
  switch (state) {
    case "displaced": {
      return DISPLACED_REASON;
    }
    case "off": {
      return `Add to ${c.name}`;
    }
    case "on": {
      return `Remove from ${c.name}`;
    }
    default: {
      return `Remove from ${c.name}`;
    }
  }
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

/** The error that refuses a Put back because the directory holds another version (exit code 6). */
export const holdsOtherContent = (events: CliEvent[]) =>
  events.some(
    (e) =>
      e.type === "error" &&
      e.message.includes("content differs from the library")
  );
