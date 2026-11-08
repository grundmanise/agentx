// The banner the detail pane shows for a skill: one at a time, the first row of this table that
// matches. Later changes add rows, in the order they should win.
import type {
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
import type { PillKind, SkillRow } from "#/screens/skills/projection.ts";

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
  /** `skill place <name>`: every enabled agent, and puts back what is missing. */
  | { kind: "add-to-agents" }
  /** `skill place <name> --to <configuration>` for the displaced placement. */
  | { kind: "put-back"; placement: LibraryPlacement; agent: string }
  /** `adopt --skill <name>`, for a skill another tool installed. */
  | { kind: "adopt" }
  /** `source add <url>`, for a skill whose source was removed. */
  | { kind: "add-source"; source: string }
  /** Fork, which opens the Fork dialog, for a skill its source no longer holds. */
  | { kind: "fork" }
  /** Opens the Publish dialog, which runs `skill publish <name>`. */
  | { kind: "publish" }
  /** Goes to Settings, where an account remote is set. */
  | { kind: "open-settings" }
  /**
   * The buttons of an edited skill of a source: Fork, which opens the Fork dialog, and Publish to
   * {source}, which opens the Publish dialog, when `publishTo` names a source this machine can
   * write to.
   */
  | { kind: "edited"; publishTo?: string };

export interface Banner {
  tone: BannerTone;
  /** The tile's icon: an arrow for an update, the alert for everything else. */
  icon?: "update";
  title: string;
  text?: string;
  action?: BannerAction;
  /**
   * The pills that say the same state as this banner. The detail pane's title row leaves them
   * out while the banner shows, so a state is said once; the list rows, which have no banner,
   * keep them.
   */
  covers?: PillKind[];
  /**
   * Whether this banner says where the skill came from. The detail pane's title row then leaves
   * out the origin text beside the name.
   */
  coversOrigin?: boolean;
}

/** What the banners read besides the row: the store's sources and the skill's diff. */
export interface BannerFacts {
  /** The added sources, from `source list`: the account remote, and each source's access. */
  sources: SourceEvent[];
  /** How many files `skill diff <name>` reports changed; undefined until it was read. */
  changedFiles?: number;
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

const notPublished = (row: SkillRow) =>
  row.kind === "yours" &&
  (row.entry?.drift?.includes("not published") ?? false);

const filesWord = (n: number) => (n === 1 ? "1 file" : `${n} files`);

const agentName = (configurations: Configuration[], id: string) =>
  configurations.find((c) => c.id === id)?.name ?? id;

/** The files an update changes, by path, joined as one line, then `kept` when the skill has edits. */
const changedFiles = (row: SkillRow, kept: string) =>
  [
    ...(row.update?.files ?? []).map((f) => f.path),
    ...(row.modified ? [kept] : []),
  ].join(" · ");

const table: BannerRule[] = [
  {
    banner: (row) => ({
      action: { kind: "update" },
      icon: "update",
      text: (row.update?.files ?? []).map((f) => f.path).join(" · "),
      title: "Changes from your other machines",
      tone: "primary",
    }),
    when: (row) => row.update !== undefined && fromAnotherMachine(row.update),
  },
  {
    banner: (row) => ({
      action: { kind: "update" },
      icon: "update",
      text: changedFiles(row, "edits you have not published are kept"),
      title: "The original has a new version",
      tone: "primary",
    }),
    when: (row) => row.update !== undefined && row.kind === "yours",
  },
  {
    banner: (row) => ({
      action: { kind: "update" },
      icon: "update",
      text: changedFiles(row, "your edits are kept"),
      title: `A newer version from ${shortSource(row.update?.source ?? row.entry?.source ?? "")}`,
      tone: "primary",
    }),
    when: (row) => row.update !== undefined,
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
        text: `Changes you make in your library won't reach ${agent} until you reconnect it.`,
        title: `${agent} is using its own copy of this skill`,
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
        covers: ["source-removed"],
        text: `Add ${shortSource(source)} source to check for updates.`,
        title: "Source removed",
        tone: "danger",
      };
    },
    when: (row) => row.entry?.drift?.includes("source removed") ?? false,
  },
  {
    banner: () => ({
      action: { kind: "fork" },
      covers: ["removed-from-source"],
      text: "No updates are possible for this skill unless it is added again in the source.",
      title: "Removed from source",
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
    when: (row) => row.kind === "yours" && row.modified && row.published,
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
          : { kind: "edited" },
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

/** The banner for `row`, or none. */
export const bannerFor = (
  row: SkillRow,
  configurations: Configuration[],
  facts: BannerFacts
): Banner | undefined =>
  table
    .find((rule) => rule.when(row, facts))
    ?.banner(row, configurations, facts);
