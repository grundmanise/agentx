import { useState } from "react";

import { RefreshIcon } from "#/components/icons/refresh.tsx";
import { Button } from "#/components/ui/button.tsx";
import { Spinner } from "#/components/ui/spinner.tsx";
import { useRouteAgent } from "#/lib/agent-route.ts";
import { formatDate } from "#/lib/dates.ts";
import { useRouteKey } from "#/lib/route-key.ts";
import { AgentFilterPopover } from "#/screens/plugins/filter-popover.tsx";
import { checkTools, useChecking } from "#/screens/servers/check-tools.ts";
import {
  filterServers,
  latestCheck,
  projectServers,
} from "#/screens/servers/projection.ts";
import type { ServerRow } from "#/screens/servers/projection.ts";
import { ServerRowItem } from "#/screens/servers/server-row.tsx";
import { AgentFilterChip } from "#/screens/skills/skill-list.tsx";
import { useAppStore } from "#/store/store.ts";

const none: ServerRow[] = [];
const noAgents: string[] = [];

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
      track="servers.check-tools"
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
 * or header value is ever shown. The route may name a server (`/servers/<logical id>`), as
 * the Plugins screen does when a server is clicked: that row scrolls into view. The Filter shows
 * the servers the checked agents declare, on or off; the Agents page opens it filtered to one.
 */
export const ServersScreen = () => {
  const routeKey = useRouteKey();
  const inventory = useAppStore((s) => s.inventory);
  const rows = inventory === undefined ? none : projectServers(inventory);
  const configurations = inventory?.configurations ?? [];
  // Only the agent configurations that declare a server are filter choices.
  const declaring = configurations.filter((c) =>
    rows.some((r) => r.declaredBy.includes(c.id))
  );
  const [agents, setAgents] = useState(noAgents);
  useRouteAgent((agent) => {
    setAgents([agent]);
  });
  const shown = filterServers(rows, agents);
  return (
    <div className="px-page pb-page gap-page-title flex h-full flex-col overflow-auto">
      <header className="pt-page-top flex items-end justify-between gap-3">
        <div className="flex items-baseline gap-3">
          <h1 className="type-page-title text-text-primary m-0">MCP servers</h1>
          <span className="type-caption text-text-caption">Read-only</span>
        </div>
        {rows.length > 0 && <CheckTools rows={rows} />}
      </header>
      {rows.length > 0 && (
        <div className="gap-split-filter flex min-w-0 items-center">
          <AgentFilterPopover
            configurations={declaring}
            agents={agents}
            onChange={setAgents}
            track="servers"
          />
          {agents.length > 0 && (
            <AgentFilterChip
              agents={agents}
              configurations={declaring}
              onClear={() => {
                setAgents(noAgents);
              }}
              onRemove={(agent) => {
                setAgents(agents.filter((a) => a !== agent));
              }}
            />
          )}
        </div>
      )}
      {shown.length === 0 ? (
        <p className="type-secondary text-text-hint m-0">
          {rows.length === 0
            ? "No MCP servers found in your agents."
            : "No servers match"}
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
            {shown.map((row) => (
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
