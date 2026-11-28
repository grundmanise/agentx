// The banner the detail pane shows for a skill: one at a time, the first row of this table that
// matches. Later changes add rows, in the order they should win.
import type {
  ConflictEvent,
  Configuration,
  LibraryPlacement,
  Source,
} from "#/cli/events.gen.ts";
import {
  adoptArgs,
  displacedPlacement,
  importOf,
} from "#/screens/skills/placements.ts";
import {
  fromAnotherMachine,
  shortSource,
  toolName,
} from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";

export type BannerTone =
  | "danger"
  | "attention"
  | "primary"
  | "yours"
  | "neutral";

/** What the banner's button does, one command each (src/screens/skills/actions.tsx). */
export type BannerAction =
  /**
   * `skill update <name>`: takes in the update the last check found. See changes sits beside it
   * and opens the What's new sheet (src/screens/skills/update-sheet.tsx).
   */
  | { kind: "update" }
  /**
   * A pending merge: Show folder reveals it, Discard my edits runs `skill update <name> --theirs`
   * after a confirm dialog, and Apply merge runs `skill update <name> --continue` to apply the
   * merge folder's files as they were left.
   */
  | { kind: "conflict" }
  /** `skill place <name>`: every enabled agent, and puts back what is missing. */
  | { kind: "add-to-agents" }
  /** `skill place <name> --to <configuration>` for the displaced placement. */
  | { kind: "put-back"; placement: LibraryPlacement; agent: string }
  /** The `skill import` command line of `adoptArgs`, for an unmanaged skill it can take over. */
  | { kind: "adopt" }
  /** Opens the library skill `name`, which keeps an unmanaged skill from being imported. */
  | { kind: "open-skill"; name: string }
  /** `source add <url>`, for a skill whose source was removed. */
  | { kind: "add-source"; source: string }
  /** Opens the Publish dialog, which runs `skill publish <name>`. */
  | { kind: "publish" }
  /** Goes to Settings, where a hub is set. */
  | { kind: "open-settings" }
  /**
   * The button of an edited skill of a source whose source this machine can write to: Publish to
   * {source}, which opens the Publish dialog. Fork is in the action row, not on the banner.
   */
  | { kind: "edited" };

export interface Banner {
  tone: BannerTone;
  /** The tile's icon: an arrow for an update, the alert for everything else. */
  icon?: "update";
  title: string;
  text?: string;
  action?: BannerAction;
  /**
   * Whether this banner says where the skill came from. The detail pane's title row then leaves
   * out the origin text beside the name.
   */
  coversOrigin?: boolean;
}

/** The last `conflict` event of each skill, by name, as the store keeps them. */
type Conflicts = Record<string, ConflictEvent>;

/** What the banners read besides the row: the store's sources, conflicts and the skill's diff. */
export interface BannerFacts {
  /** The added sources, from `source list`: the hub, and each source's access. */
  sources: Source[];
  /** How many files `skill diff <name>` reports changed; undefined until it was read. */
  changedFiles?: number;
  /** The store's last `conflict` event of each skill, for the files a pending merge left. */
  conflicts?: Conflicts;
  /** The paths `skill diff <name> --update` reports for the skill's update; undefined until it was read. */
  updateFiles?: string[];
}

interface BannerRule {
  when: (row: SkillRow, facts: BannerFacts) => boolean;
  banner: (
    row: SkillRow,
    configurations: Configuration[],
    facts: BannerFacts
  ) => Banner;
}

/** The hub, the one source added with `--hub`, when one is set. */
export const hubOf = (sources: Source[]) => sources.find((s) => s.hub === true);

/** Why Publish does nothing for a hub skill while the hub is read-only, as the last add or fetch of it found. */
export const readOnlyHubReason = (sources: Source[]) => {
  const hub = hubOf(sources);
  return hub?.access === "read-only"
    ? `${shortSource(hub.url)} is read-only`
    : undefined;
};

/**
 * A hub skill edited since it was published: what a bare `skill publish` covers, and
 * what the title bar's status counts as edited.
 */
export const hasEditsToPublish = (row: SkillRow) =>
  row.kind === "hub" && row.modified && row.published;

/**
 * A hub skill the hub holds no branch of: a publish sends every file, and
 * the title bar counts it as new.
 */
export const notPublished = (row: SkillRow) =>
  row.kind === "hub" && (row.entry?.drift?.includes("not published") ?? false);

const filesWord = (n: number) => (n === 1 ? "1 file" : `${n} files`);

const agentName = (configurations: Configuration[], id: string) =>
  configurations.find((c) => c.id === id)?.name ?? id;

/** The files an update changes, by path, joined as one line, then the promise when the skill has edits. */
const changedFiles = (row: SkillRow, facts: BannerFacts) =>
  [
    ...(facts.updateFiles ?? []),
    ...(row.modified ? ["Your local edits are kept."] : []),
  ].join(" · ");

/**
 * Where an update comes from: its source, or for a hub skill with an upstream, the
 * upstream.
 */
const updateOrigin = (row: SkillRow) => {
  const upstream = row.kind === "hub" ? row.entry?.upstream : undefined;
  return shortSource(upstream ?? row.entry?.source ?? "");
};

/** What a pending merge left to resolve, with the number of files when a `conflict` event said it. */
const conflictText = (conflict: ConflictEvent | undefined) => {
  if (conflict === undefined || conflict.files.length === 0) {
    return "Your edits and the update changed the same lines. Edit the files in the merge folder, then apply the merge.";
  }
  const n = conflict.files.length;
  return n === 1
    ? `Your edits and the update changed the same lines in ${conflict.files[0]?.path ?? "1 file"}. Edit it in the merge folder, then apply the merge.`
    : `Your edits and the update changed the same lines in ${n} files. Edit them in the merge folder, then apply the merge.`;
};

const table: BannerRule[] = [
  {
    banner: (row, _configurations, facts) => ({
      action: { kind: "conflict" },
      text: conflictText(facts.conflicts?.[row.name]),
      title: "Update has conflicts",
      tone: "attention",
    }),
    when: (row) => row.entry?.pending_merge === true,
  },
  {
    banner: (row, _configurations, facts) => ({
      action: { kind: "update" },
      icon: "update",
      text: changedFiles(row, facts),
      title: `New version from ${updateOrigin(row)}`,
      tone: "primary",
    }),
    when: (row) => row.update !== undefined && !fromAnotherMachine(row.update),
  },
  {
    banner: (_row, _configurations, facts) => ({
      action: { kind: "update" },
      icon: "update",
      text: (facts.updateFiles ?? []).join(" · "),
      title: "Published from another machine",
      tone: "primary",
    }),
    when: (row) => row.update !== undefined && fromAnotherMachine(row.update),
  },
  {
    banner: (row, configurations) => {
      const placement = displacedPlacement(row);
      const agent =
        placement === undefined
          ? ""
          : agentName(configurations, placement.configuration);
      return {
        action:
          placement === undefined
            ? undefined
            : { agent, kind: "put-back", placement },
        text: "It owns a copy of the skill, so your library changes don't reach it.",
        title: `${agent} is not managed by agentx`,
        tone: "attention",
      };
    },
    when: (row) => displacedPlacement(row) !== undefined,
  },
  {
    banner: (row) => {
      const tool =
        row.recorded === undefined ? "" : toolName(row.recorded.tool);
      return {
        action: { kind: "adopt" },
        coversOrigin: true,
        text: "Import to manage it from agentx",
        title: `Added by ${tool}`,
        tone: "neutral",
      };
    },
    when: (row) =>
      row.recorded !== undefined &&
      row.recorded.managed !== true &&
      row.recorded.refusal === undefined,
  },
  {
    // An unmanaged skill no tool recorded.
    banner: () => ({
      action: { kind: "adopt" },
      text: "Import to manage it from agentx",
      title: "Not managed by agentx",
      tone: "neutral",
    }),
    when: (row) => adoptArgs(row) !== undefined,
  },
  {
    // An unmanaged skill `skill import` cannot take, and why.
    banner: (row) => {
      const result = importOf(row);
      const why = result !== undefined && "why" in result ? result : undefined;
      const refused = row.recorded?.refusal !== undefined;
      return {
        action:
          why?.inTheWay === undefined
            ? undefined
            : { kind: "open-skill", name: why.inTheWay },
        coversOrigin: refused,
        text: why?.why,
        title:
          row.recorded?.refusal === undefined
            ? "Not managed by agentx"
            : `Added by ${toolName(row.recorded.tool)}`,
        tone: "neutral",
      };
    },
    when: (row) => {
      const result = importOf(row);
      return result !== undefined && "why" in result;
    },
  },
  {
    banner: (row) => {
      const source = row.entry?.source ?? "";
      return {
        action: { kind: "add-source", source },
        text: `Add ${shortSource(source)} source to check for updates.`,
        title: `${shortSource(source)} was removed`,
        tone: "danger",
      };
    },
    when: (row) => row.entry?.drift?.includes("source removed") ?? false,
  },
  {
    // No action: Fork is in the action row.
    banner: (row) => ({
      text: "The source no longer has this skill.",
      title: `No longer in ${shortSource(row.entry?.source ?? "")}`,
      tone: "danger",
    }),
    when: (row) => row.entry?.drift?.includes("upstream removed") ?? false,
  },
  {
    banner: () => ({
      action: { kind: "add-to-agents" },
      title: "Not in any agent",
      tone: "neutral",
    }),
    // A library skill with no placement and no universal client to read the library.
    when: (row) => row.entry !== undefined && !row.enabled,
  },
  {
    banner: () => ({
      action: { kind: "publish" },
      text: "Publish to share your latest version.",
      title: "Edits not published",
      tone: "yours",
    }),
    when: hasEditsToPublish,
  },
  {
    banner: () => ({
      action: { kind: "publish" },
      text: "Publish it to use it on other machines.",
      title: "Not published",
      tone: "yours",
    }),
    when: (row, facts) =>
      notPublished(row) && hubOf(facts.sources) !== undefined,
  },
  {
    banner: () => ({
      action: { kind: "open-settings" },
      text: "Set a hub to publish it.",
      title: "Not published",
      tone: "yours",
    }),
    when: (row, facts) =>
      notPublished(row) && hubOf(facts.sources) === undefined,
  },
  {
    banner: (row, _configurations, facts) => {
      const source = row.entry?.source ?? "";
      const writable =
        facts.sources.find((s) => s.url === source)?.access === "writable";
      const n = facts.changedFiles;
      // A source the user can write to takes the edits back with Publish.
      if (writable) {
        return {
          action: { kind: "edited" },
          text: `Publish to share them on ${shortSource(source)}.`,
          title: "Edits not published",
          tone: "attention",
        };
      }
      return {
        text:
          n === undefined
            ? "Updates keep your edits."
            : `${filesWord(n)} ${n === 1 ? "differs" : "differ"} from ${shortSource(source)}. Updates keep your edits.`,
        title: "Edited locally",
        tone: "attention",
      };
    },
    when: (row) => row.kind === "source" && row.modified,
  },
];

/** The banner for `row`, or none, from what `facts` says besides the row. */
export const bannerFor = (
  row: SkillRow,
  configurations: Configuration[],
  facts: BannerFacts
): Banner | undefined =>
  table
    .find((rule) => rule.when(row, facts))
    ?.banner(row, configurations, facts);
