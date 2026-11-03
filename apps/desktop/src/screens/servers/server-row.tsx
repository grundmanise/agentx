import { cva } from "class-variance-authority";

import type { Configuration } from "#/cli/events.gen.ts";
import { LogoStack } from "#/components/agent-logo.tsx";
import type { LogoClient } from "#/components/agent-logo.tsx";
import type { Failure, ServerRow } from "#/screens/servers/projection.ts";

const failureVariants = cva("type-secondary whitespace-nowrap", {
  variants: {
    reason: {
      failed: "text-server-tools-fail",
      not_found: "text-server-tools-fail",
      timeout: "text-server-tools-fail",
      unauthorized: "text-server-tools-auth",
      unreachable: "text-server-tools-fail",
    },
  },
});

/** A failure of the last check, in words; hovering it shows the CLI's hint. */
const FailureWord = ({ failure }: { failure: Failure }) => (
  <span title={failure.hint} className={failureVariants(failure)}>
    {failure.word}
  </span>
);

/** The TOOLS cell: how many tools the last check found, or "Not checked", then this session's failures. */
const Tools = ({ row }: { row: ServerRow }) => (
  <td className="flex min-w-0 flex-col gap-0.5 p-0">
    {row.tools === undefined ? (
      <span className="type-secondary text-server-tools-none">Not checked</span>
    ) : (
      <span className="type-secondary text-server-tools">
        {row.tools === 1 ? "1 tool" : `${row.tools} tools`}
      </span>
    )}
    {row.failures.map((failure) => (
      <FailureWord key={failure.reason} failure={failure} />
    ))}
  </td>
);

/** The logos of the agents that declare the server; one that turned it off is dimmed and says so. */
const logoClients = (
  row: ServerRow,
  configurations: Configuration[]
): LogoClient[] =>
  row.agents.flatMap((agent) => {
    const c = configurations.find((cfg) => cfg.id === agent.configuration);
    if (c === undefined) {
      return [];
    }
    return [
      agent.off
        ? {
            client: c.client,
            name: c.name,
            off: true,
            title: `${c.name} turned this server off`,
          }
        : { client: c.client, name: c.name },
    ];
  });

/**
 * One server in the list (DESIGN.md §4 "Lists and rows", the table tokens): name and plugin tag
 * over the URL or command, the transport pills and the versions count, the tools, and the 24px
 * logos of the agents that declare it. Display only: nothing on a row runs a command.
 */
export const ServerRowItem = ({
  row,
  configurations,
}: {
  row: ServerRow;
  configurations: Configuration[];
}) => (
  <tr
    aria-label={row.name}
    className="grid-cols-server gap-x-table-col p-table-row rounded-table-row hover:bg-table-row-hover grid items-center"
  >
    <td className="gap-row-line flex min-w-0 flex-col p-0">
      <span className="gap-row-inline flex min-w-0 items-center">
        <span className="type-server-name text-text-primary truncate">
          {row.name}
        </span>
        {row.plugins.map((plugin) => (
          <span
            key={plugin}
            className="type-technical-sm text-card-tag whitespace-nowrap"
          >
            {`${plugin} plugin`}
          </span>
        ))}
      </span>
      <span className="type-technical text-server-command truncate">
        {row.address}
      </span>
    </td>
    <td className="flex min-w-0 flex-wrap items-center gap-2 p-0">
      {row.transports.map((transport) => (
        <span
          key={transport}
          className="rounded-pill bg-server-transport-bg type-transport-pill text-server-transport-text inline-flex h-5 flex-none items-center px-2 whitespace-nowrap"
        >
          {transport}
        </span>
      ))}
      {row.versions > 1 && (
        <span className="type-technical-sm text-server-versions whitespace-nowrap">
          {`${row.versions} versions`}
        </span>
      )}
    </td>
    <Tools row={row} />
    <td className="flex items-center p-0">
      <LogoStack clients={logoClients(row, configurations)} size={24} />
    </td>
  </tr>
);
