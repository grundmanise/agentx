import type { IconName } from "@/components/icon";

export interface Screen {
  /** Route path; nested routes (such as Update review under Skills) keep their parent active. */
  path: string;
  label: string;
  icon: IconName;
}

/** Sidebar order, one evenly spaced list: Inbox, Skills, Add, Agents, MCP servers, Plugins, Settings. */
export const screens: Screen[] = [
  { path: "/inbox", label: "Inbox", icon: "inbox" },
  { path: "/skills", label: "Skills", icon: "skills" },
  { path: "/add", label: "Add", icon: "add" },
  { path: "/agents", label: "Agents", icon: "agents" },
  { path: "/servers", label: "MCP servers", icon: "servers" },
  { path: "/plugins", label: "Plugins", icon: "plugins" },
  { path: "/settings", label: "Settings", icon: "settings" },
];

/** The title each screen shows; Add's page is "Add skills". */
export const titles: Record<string, string> = {
  "/inbox": "Inbox",
  "/skills": "Skills",
  "/add": "Add skills",
  "/agents": "Agents",
  "/servers": "MCP servers",
  "/plugins": "Plugins",
  "/settings": "Settings",
};
