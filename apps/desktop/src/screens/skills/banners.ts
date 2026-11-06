// The banner the detail pane shows for a skill: one at a time, the first row of this table that
// matches. Later changes add rows, in the order they should win.
import type { Configuration, LibraryPlacement } from "#/cli/events.gen.ts";
import { displacedPlacement } from "#/screens/skills/placements.ts";
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
  | { kind: "put-back"; placement: LibraryPlacement; agent: string };

export interface Banner {
  tone: BannerTone;
  title: string;
  text?: string;
  action?: BannerAction;
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
];

/** The banner for `row`, or none. */
export const bannerFor = (
  row: SkillRow,
  configurations: Configuration[]
): Banner | undefined =>
  table.find((rule) => rule.when(row))?.banner(row, configurations);
