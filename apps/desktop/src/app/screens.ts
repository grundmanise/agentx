import type { IconName } from "#/components/icon.tsx";

export interface Screen {
  /** Route path; nested routes (such as Update review under Skills) keep their parent active. */
  path: string;
  label: string;
  icon: IconName;
}

/** Sidebar order, one evenly spaced list: Inbox, Skills, Add, Agents, MCP servers, Plugins, Settings. */
export const screens: Screen[] = [
  { icon: "inbox", label: "Inbox", path: "/inbox" },
  { icon: "skills", label: "Skills", path: "/skills" },
  { icon: "add", label: "Add", path: "/add" },
  { icon: "agents", label: "Agents", path: "/agents" },
  { icon: "servers", label: "MCP servers", path: "/servers" },
  { icon: "plugins", label: "Plugins", path: "/plugins" },
  { icon: "settings", label: "Settings", path: "/settings" },
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
