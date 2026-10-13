import add from "#/assets/icons/add.svg";
import agents from "#/assets/icons/agents.svg";
import inbox from "#/assets/icons/inbox.svg";
import plugins from "#/assets/icons/plugins.svg";
import search from "#/assets/icons/search.svg";
import servers from "#/assets/icons/servers.svg";
import settings from "#/assets/icons/settings.svg";
import skills from "#/assets/icons/skills.svg";

// Stroke icons on a 24 grid (DESIGN.md §7), one file each in src/assets/icons. Vite turns each
// import into the file's URL. An <img> cannot take the text colour around it, so each file holds
// its colour (`gray-300`) and its stroke width.
const icons = {
  add,
  agents,
  inbox,
  plugins,
  search,
  servers,
  settings,
  skills,
};

export type IconName = keyof typeof icons;

/** Rendered sizes DESIGN.md §7 allows. */
export type IconSize = 13 | 14 | 15 | 16 | 18;

/** A decorative icon: it always sits next to text that names it, so screen readers skip it. */
export const Icon = ({
  name,
  size = 16,
}: {
  name: IconName;
  size?: IconSize;
}) => (
  <img
    src={icons[name]}
    width={size}
    height={size}
    alt=""
    aria-hidden
    draggable={false}
    className="block flex-none"
  />
);
