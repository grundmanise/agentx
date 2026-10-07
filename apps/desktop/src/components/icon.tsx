import { cva } from "class-variance-authority";
import type { VariantProps } from "class-variance-authority";

import Add from "#/assets/icons/add.svg?react";
import Agents from "#/assets/icons/agents.svg?react";
import Inbox from "#/assets/icons/inbox.svg?react";
import Plugins from "#/assets/icons/plugins.svg?react";
import Search from "#/assets/icons/search.svg?react";
import Servers from "#/assets/icons/servers.svg?react";
import Settings from "#/assets/icons/settings.svg?react";
import Skills from "#/assets/icons/skills.svg?react";

// Stroke icons on a 24 grid (DESIGN.md §7), one file each in src/assets/icons. The files draw
// in `currentColor`; `?react` turns a file into a component.
const icons = {
  add: Add,
  agents: Agents,
  inbox: Inbox,
  plugins: Plugins,
  search: Search,
  servers: Servers,
  settings: Settings,
  skills: Skills,
};

export type IconName = keyof typeof icons;

/** Rendered sizes and stroke widths DESIGN.md §7 allows. */
export type IconSize = 13 | 14 | 15 | 16 | 18;
export type IconStroke = 1.75 | 2 | 2.25;

/**
 * The icon's colour. `current` takes the text colour around it. `palette` is the command palette's
 * icon grey. `palette-row` is that grey, turning to the accent while its row (a `group`) is selected.
 */
const iconVariants = cva("", {
  variants: {
    tone: {
      current: "",
      palette: "text-palette-icon",
      "palette-row":
        "text-palette-icon group-data-[selected=true]:text-palette-icon-active",
    },
  },
});

export type IconTone = NonNullable<VariantProps<typeof iconVariants>["tone"]>;

export const Icon = ({
  name,
  size = 16,
  strokeWidth = 1.75,
  tone = "current",
}: {
  name: IconName;
  size?: IconSize;
  strokeWidth?: IconStroke;
  tone?: IconTone;
}) => {
  const Svg = icons[name];
  return (
    <Svg
      width={size}
      height={size}
      strokeWidth={strokeWidth}
      aria-hidden
      className={iconVariants({ tone }) || undefined}
    />
  );
};
