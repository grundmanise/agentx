import { AgentsIcon } from "#/components/icons/agents.tsx";
import type { IconComponent } from "#/components/icons/icon-props.ts";
import { PluginsIcon } from "#/components/icons/plugins.tsx";
import { ServersIcon } from "#/components/icons/servers.tsx";
import { SettingsIcon } from "#/components/icons/settings.tsx";
import { SkillsIcon } from "#/components/icons/skills.tsx";
import { SourcesIcon } from "#/components/icons/sources.tsx";

export interface Screen {
  /** Route path; nested routes (such as Update review under Skills) keep their parent active. */
  path: string;
  label: string;
  icon: IconComponent;
  /** The screens nested under this one in the sidebar, each at a path under its parent's. */
  children?: Screen[];
}

/** The Sources screen, nested under Skills. */
export const SOURCES_PATH = "/skills/sources";

/** Sidebar order: Skills with Sources under it, Agents, MCP servers, Plugins, Settings. */
export const screens: Screen[] = [
  {
    children: [{ icon: SourcesIcon, label: "Sources", path: SOURCES_PATH }],
    icon: SkillsIcon,
    label: "Skills",
    path: "/skills",
  },
  { icon: AgentsIcon, label: "Agents", path: "/agents" },
  { icon: ServersIcon, label: "MCP servers", path: "/servers" },
  { icon: PluginsIcon, label: "Plugins", path: "/plugins" },
  { icon: SettingsIcon, label: "Settings", path: "/settings" },
];

/** Every screen, a nested one right after its parent: the palette's GO TO and the routes. */
export const allScreens: Screen[] = screens.flatMap((s) => [
  s,
  ...(s.children ?? []),
]);

/** The title each screen shows. */
export const titles: Record<string, string> = {
  "/agents": "Agents",
  "/plugins": "Plugins",
  "/servers": "MCP servers",
  "/settings": "Settings",
  "/skills": "Skills",
  [SOURCES_PATH]: "Sources",
};
