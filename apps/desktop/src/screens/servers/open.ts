// Opening a server from another screen: a plugin card comes here. The MCP servers screen scrolls
// the row of the server its route names into view.
import { navigateTo } from "#/lib/navigate.ts";

/** The route of a server: its row's key, the server's logical id. */
export const serverPath = (key: string) =>
  `/servers/${encodeURIComponent(key)}`;

/** Shows the MCP servers screen with the row of `key` in view. */
export const openServer = (key: string) => {
  navigateTo(serverPath(key));
};
