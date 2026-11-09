// The banner the detail pane shows for a skill: one at a time, the first row of this table that
// matches. Later changes add rows, in the order they should win.
import type { Configuration, LibraryPlacement } from "#/cli/events.gen.ts";
import { displacedPlacement } from "#/screens/skills/placements.ts";
import { shortSource, toolName } from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";

export type BannerTone =
  | "danger"
  | "attention"
  | "primary"
  | "yours"
  | "neutral";

/** What the banner's button does, one command each (src/screens/skills/actions.tsx). */
export type BannerAction =
  /** `skill place <name>`: every enabled agent, and puts back what is missing. */
  | { kind: "add-to-agents" }
  /** `skill place <name> --to <configuration>` for the displaced placement. */
  | { kind: "put-back"; placement: LibraryPlacement; agent: string }
  /** `adopt --skill <name>`, for a skill another tool installed. */
  | { kind: "adopt" }
  /** `source add <url>`, for a skill whose source was removed. */
  | { kind: "add-source"; source: string }
  /** `skill fork <name>`, for a skill its source no longer holds. */
  | { kind: "fork" };

export interface Banner {
  tone: BannerTone;
  title: string;
  text?: string;
  action?: BannerAction;
  /**
   * Whether this banner says where the skill came from. The detail pane's title row then leaves
   * out the origin text beside the name.
   */
  coversOrigin?: boolean;
}

interface BannerRule {
  when: (row: SkillRow) => boolean;
  banner: (row: SkillRow, configurations: Configuration[]) => Banner;
}

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
    banner: (row) => ({
      action: { kind: "fork" },
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
];

/** The banner for `row`, or none. */
export const bannerFor = (
  row: SkillRow,
  configurations: Configuration[]
): Banner | undefined =>
  table.find((rule) => rule.when(row))?.banner(row, configurations);
