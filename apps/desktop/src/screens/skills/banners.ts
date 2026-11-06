// The banner the detail pane shows for a skill: one at a time, the first row of this table that
// matches. Later changes add rows, in the order they should win.
import type {
  Configuration,
  LibraryPlacement,
  SourceEvent,
} from "#/cli/events.gen.ts";
import { displacedPlacement } from "#/screens/skills/placements.ts";
import { shortSource } from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";

export type BannerTone =
  | "danger"
  | "attention"
  | "primary"
  | "yours"
  | "neutral";

/** What the banner's button does, one command each (src/screens/skills/banner-view.tsx). */
export type BannerAction =
  /** `skill place <name>`: every enabled agent, and puts back what is missing. */
  | { kind: "add-to-agents" }
  /** `skill place <name> --to <configuration>` for the displaced placement. */
  | { kind: "put-back"; placement: LibraryPlacement; agent: string }
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
  title: string;
  text?: string;
  action?: BannerAction;
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

const table: BannerRule[] = [
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
    banner: () => ({
      text: "It still works but won't get updates.",
      title: "Removed from its source",
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
      text: "They are only on this machine until you publish them.",
      title: "Edits not published",
      tone: "yours",
    }),
    when: (row) => row.kind === "yours" && row.modified && row.published,
  },
  {
    banner: () => ({
      action: { kind: "publish" },
      text: "Only on this machine. Publish it to put it on your account remote.",
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
      const merge = "Updates merge your edits in.";
      return {
        action: writable
          ? { kind: "edited", publishTo: shortSource(source) }
          : { kind: "edited" },
        text:
          facts.changedFiles === undefined
            ? merge
            : `${filesWord(facts.changedFiles)} changed. ${merge}`,
        title: "Edited outside agentx",
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
