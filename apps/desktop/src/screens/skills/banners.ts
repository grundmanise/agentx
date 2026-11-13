// The banner the detail pane shows for a skill: one at a time, the first row of this table that
// matches. Later changes add rows, in the order they should win.
import type {
  ConflictEvent,
  Configuration,
  LibraryPlacement,
  SourceEvent,
} from "#/cli/events.gen.ts";
import { displacedPlacement } from "#/screens/skills/placements.ts";
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
   * A pending merge: Show folder reveals it, Abort runs `skill update <name> --abort` after a
   * confirm dialog, and Update runs `skill update <name>` again to apply what was resolved.
   */
  | { kind: "conflict" }
  /** `skill place <name>`: every enabled agent, and puts back what is missing. */
  | { kind: "add-to-agents" }
  /** `skill place <name> --to <configuration>` for the displaced placement. */
  | { kind: "put-back"; placement: LibraryPlacement; agent: string }
  /** `adopt --skill <name>`, for a skill another tool installed. */
  | { kind: "adopt" }
  /** `source add <url>`, for a skill whose source was removed. */
  | { kind: "add-source"; source: string }
  /** Opens the Publish dialog, which runs `skill publish <name>`. */
  | { kind: "publish" }
  /** Goes to Settings, where an account remote is set. */
  | { kind: "open-settings" }
  /**
   * The button of an edited skill of a source whose source this machine can write to: Publish to
   * {source}, which opens the Publish dialog. Fork is in the action row, not on the banner.
   */
  | { kind: "edited"; publishTo: string };

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
  /** The added sources, from `source list`: the account remote, and each source's access. */
  sources: SourceEvent[];
  /** How many files `skill diff <name>` reports changed; undefined until it was read. */
  changedFiles?: number;
  /** The store's last `conflict` event of each skill, for the files a pending merge left. */
  conflicts?: Conflicts;
}

interface BannerRule {
  when: (row: SkillRow, facts: BannerFacts) => boolean;
  banner: (
    row: SkillRow,
    configurations: Configuration[],
    facts: BannerFacts
  ) => Banner;
}

/** The account remote, the one source added with `--account`, when one is set. */
export const accountRemoteOf = (sources: SourceEvent[]) =>
  sources.find((s) => s.account === true);

/**
 * One of your own skills edited since it was published: what a bare `skill publish` covers, and
 * what the title bar's publish status counts.
 */
export const hasEditsToPublish = (row: SkillRow) =>
  row.kind === "yours" && row.modified && row.published;

const notPublished = (row: SkillRow) =>
  row.kind === "yours" &&
  (row.entry?.drift?.includes("not published") ?? false);

const filesWord = (n: number) => (n === 1 ? "1 file" : `${n} files`);

const agentName = (configurations: Configuration[], id: string) =>
  configurations.find((c) => c.id === id)?.name ?? id;

/** The files an update changes, by path, joined as one line, then the promise when the skill has edits. */
const changedFiles = (row: SkillRow) =>
  [
    ...(row.update?.files ?? []).map((f) => f.path),
    ...(row.modified ? ["Your local edits are kept."] : []),
  ].join(" · ");

/**
 * Where an update comes from: its source, or for one of your own skills with an upstream, the
 * upstream.
 */
const updateOrigin = (row: SkillRow) => {
  const upstream =
    row.kind === "yours"
      ? (row.update?.upstream ?? row.entry?.upstream)
      : undefined;
  return shortSource(upstream ?? row.update?.source ?? row.entry?.source ?? "");
};

/** What a pending merge left to resolve, with the number of files when a `conflict` event said it. */
const conflictText = (conflict: ConflictEvent | undefined) => {
  if (conflict === undefined || conflict.files.length === 0) {
    return "Your edits and the update changed the same lines. Resolve them in the merge folder, then update again.";
  }
  const n = conflict.files.length;
  return `Your edits and the update changed the same lines in ${n === 1 ? "1 file" : `${n} files`}. Resolve them in the merge folder, then update again.`;
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
    banner: (row) => ({
      action: { kind: "update" },
      icon: "update",
      text: changedFiles(row),
      title: `New version from ${updateOrigin(row)}`,
      tone: "primary",
    }),
    when: (row) => row.update !== undefined && !fromAnotherMachine(row.update),
  },
  {
    banner: (row) => ({
      action: { kind: "update" },
      icon: "update",
      text: (row.update?.files ?? []).map((f) => f.path).join(" · "),
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
      const tool = row.adoption === undefined ? "" : toolName(row.adoption);
      return {
        action: { kind: "adopt" },
        coversOrigin: true,
        text: "Adopt to manage it from agentx",
        title: `Added by ${tool}`,
        tone: "neutral",
      };
    },
    when: (row) => row.adoption?.state === "candidate",
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
      text: "It seems this skill was removed from the source.",
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
      text: "Only on this machine. Publish it to your account remote.",
      title: "Not published",
      tone: "yours",
    }),
    when: (row, facts) =>
      notPublished(row) && accountRemoteOf(facts.sources) !== undefined,
  },
  {
    banner: () => ({
      action: { kind: "open-settings" },
      text: "Set an account remote to publish it.",
      title: "Not published",
      tone: "yours",
    }),
    when: (row, facts) =>
      notPublished(row) && accountRemoteOf(facts.sources) === undefined,
  },
  {
    banner: (row, _configurations, facts) => {
      const source = row.entry?.source ?? "";
      const writable =
        facts.sources.find((s) => s.url === source)?.access === "writable";
      const n = facts.changedFiles;
      return {
        action: writable
          ? { kind: "edited", publishTo: shortSource(source) }
          : undefined,
        text:
          n === undefined
            ? "Updates won't overwrite your edits."
            : `${filesWord(n)} ${n === 1 ? "differs" : "differ"} from ${shortSource(source)}. Updates won't overwrite them.`,
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
