import { useMemo } from "react";

import { RefreshIcon } from "#/components/icons/refresh.tsx";
import { Spinner } from "#/components/spinner.tsx";
import { Button } from "#/components/ui/button.tsx";
import { formatDate } from "#/lib/dates.ts";
import { useRouteKey } from "#/lib/route-key.ts";
import { checkTools, useChecking } from "#/screens/servers/check-tools.ts";
import { latestCheck, projectServers } from "#/screens/servers/projection.ts";
import type { ServerRow } from "#/screens/servers/projection.ts";
import { ServerRowItem } from "#/screens/servers/server-row.tsx";
import { useAppStore } from "#/store/store.ts";

const none: ServerRow[] = [];

const columns = ["SERVER", "TRANSPORT", "TOOLS", "AGENTS"];

/**
 * The ghost Check tools. While the check runs it is pending (DESIGN.md §6): a `Spinner` takes the
 * refresh icon's place, before the label, until the check ends. Its title says
 * when the last check was, the latest `signature_at` over every server, or that none ran yet.
 */
const CheckTools = ({ rows }: { rows: ServerRow[] }) => {
  const checking = useChecking();
  const when = latestCheck(rows);
  return (
    <Button
      variant="ghost"
      size="sm"
      aria-busy={checking}
      title={
        when === undefined ? "No check yet" : `Last check: ${formatDate(when)}`
      }
      onClick={() => {
        void checkTools();
      }}
    >
      {checking ? <Spinner /> : <RefreshIcon size="sm" />}
      Check tools
    </Button>
  );
};

/**
 * The MCP servers screen: every server the agent configurations declare, once per server however
 * many declare it, with its transport, how many tools it exposed the last time agentx checked, and
 * the agents that declare it. Read-only: Check tools runs a scan with a handshake, and is there
 * only while a server is declared; nothing here edits, starts or stops a server. No environment
 * or header value is ever shown. The route may name a server (`/servers/<logical id>`), as a
 * plugin card does when its server row is clicked: that row scrolls into view.
 */
export const ServersScreen = () => {
  const routeKey = useRouteKey();
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
        {rows.length > 0 && <CheckTools rows={rows} />}
      </header>
      {rows.length === 0 ? (
        <p className="type-secondary text-text-hint m-0">
          No MCP servers found in your agents.
        </p>
      ) : (
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
                inView={row.key === routeKey}
              />
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
};
