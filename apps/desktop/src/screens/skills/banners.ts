// The banner the detail pane shows for a skill: one at a time, the first row of this table that
// matches. Later changes add rows, in the order they should win.
import type { SkillRow } from "#/screens/skills/projection.ts";

export type BannerTone = "danger" | "attention" | "primary" | "yours";

export interface Banner {
  tone: BannerTone;
  title: string;
  text: string;
}

interface BannerRule {
  when: (row: SkillRow) => boolean;
  banner: Banner;
}

const table: BannerRule[] = [
  {
    banner: {
      text: "It still works but won't get updates.",
      title: "Removed from its source",
      tone: "danger",
    },
    when: (row) => row.entry?.drift?.includes("upstream removed") ?? false,
  },
];

/** The banner for `row`, or none. */
export const bannerFor = (row: SkillRow): Banner | undefined =>
  table.find((rule) => rule.when(row))?.banner;
