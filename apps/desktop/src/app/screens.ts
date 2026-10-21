import { AddIcon } from "#/components/icons/add.tsx";
import { AgentsIcon } from "#/components/icons/agents.tsx";
import type { IconComponent } from "#/components/icons/icon-props.ts";
import { InboxIcon } from "#/components/icons/inbox.tsx";
import { PluginsIcon } from "#/components/icons/plugins.tsx";
import { ServersIcon } from "#/components/icons/servers.tsx";
import { SettingsIcon } from "#/components/icons/settings.tsx";
import { SkillsIcon } from "#/components/icons/skills.tsx";

export interface Screen {
  /** Route path; nested routes (such as Update review under Skills) keep their parent active. */
  path: string;
  label: string;
  icon: IconComponent;
}

/** Sidebar order, one evenly spaced list: Inbox, Skills, Add, Agents, MCP servers, Plugins, Settings. */
export const screens: Screen[] = [
  { icon: InboxIcon, label: "Inbox", path: "/inbox" },
  { icon: SkillsIcon, label: "Skills", path: "/skills" },
  { icon: AddIcon, label: "Add", path: "/add" },
  { icon: AgentsIcon, label: "Agents", path: "/agents" },
  { icon: ServersIcon, label: "MCP servers", path: "/servers" },
  { icon: PluginsIcon, label: "Plugins", path: "/plugins" },
  { icon: SettingsIcon, label: "Settings", path: "/settings" },
];

/** The title each screen shows; Add's page is "Add skills". */
export const titles: Record<string, string> = {
  "/add": "Add skills",
  "/agents": "Agents",
  "/inbox": "Inbox",
  "/plugins": "Plugins",
  "/servers": "MCP servers",
  "/settings": "Settings",
  "/skills": "Skills",
};
