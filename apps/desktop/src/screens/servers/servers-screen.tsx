import { useMemo } from "react";

import { formatDate } from "#/lib/dates.ts";
import { latestCheck, projectServers } from "#/screens/servers/projection.ts";
import type { ServerRow } from "#/screens/servers/projection.ts";
import { ServerRowItem } from "#/screens/servers/server-row.tsx";
import { useAppStore } from "#/store/store.ts";

const none: ServerRow[] = [];

const columns = ["SERVER", "TRANSPORT", "TOOLS", "AGENTS"];

/** What the footnote says: when the tool counts were taken, and what the screen never does. */
const footnote = (rows: ServerRow[]) => {
  const when = latestCheck(rows);
  const counts =
    when === undefined
      ? "No server has been checked yet."
      : `Tool counts come from the last check, ${formatDate(when)}.`;
  return `${counts} Servers you turned off in an agent are never started, and header or environment values are never shown.`;
};

/**
 * The MCP servers screen: every server the agent configurations declare, once per server however
 * many declare it, with its transport, how many tools it exposed the last time agentx checked, and
 * the agents that declare it. Read-only: nothing here edits, starts or stops a server. No
 * environment or header value is ever shown.
 */
export const ServersScreen = () => {
  const inventory = useAppStore((s) => s.inventory);
  const handshakeFailures = useAppStore((s) => s.handshakeFailures);
  const rows = useMemo(
    () =>
      inventory === undefined
        ? none
        : projectServers({ handshakeFailures, inventory }),
    [inventory, handshakeFailures]
  );
  const configurations = inventory?.configurations ?? [];
  return (
    <div className="pt-page-top px-page pb-page gap-page-title flex h-full flex-col overflow-auto">
      <header className="flex items-end justify-between gap-3">
        <div className="flex items-baseline gap-3">
          <h1 className="type-page-title text-text-primary m-0">MCP servers</h1>
          <span className="type-caption text-text-caption">Read-only</span>
        </div>
        {/* Check tools comes with the change that builds it. */}
      </header>
      {rows.length === 0 ? (
        <p className="type-secondary text-text-hint m-0">
          No MCP servers found in your agents.
        </p>
      ) : (
        <div className="flex flex-col gap-4">
          <table
            aria-label="MCP servers"
            className="flex w-full border-separate border-spacing-0 flex-col"
          >
            <thead className="bg-shell-content-bg sticky top-0 flex flex-col">
              <tr className="grid-cols-server gap-x-table-col h-table-header px-table-row border-shell-divider grid items-center border-b">
                {columns.map((column) => (
                  <th
                    key={column}
                    scope="col"
                    className="type-caption text-table-header-text p-0 text-left font-normal"
                  >
                    {column}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody className="flex flex-col pt-1">
              {rows.map((row) => (
                <ServerRowItem
                  key={row.key}
                  row={row}
                  configurations={configurations}
                />
              ))}
            </tbody>
          </table>
          <p className="type-footnote text-text-hint max-w-detail m-0 text-pretty">
            {footnote(rows)}
          </p>
        </div>
      )}
    </div>
  );
};
