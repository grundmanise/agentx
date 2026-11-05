import { cva } from "class-variance-authority";
import { useEffect, useRef } from "react";

import type { Configuration } from "#/cli/events.gen.ts";
import { LogoStack } from "#/components/agent-logo.tsx";
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

/** The configurations that have the server on, for their logos. */
const onClients = (row: ServerRow, configurations: Configuration[]) =>
  row.agents
    .map((id) => configurations.find((c) => c.id === id))
    .filter((c) => c !== undefined);

/**
 * The AGENTS cell: the logos of the agents that have the server on. A client that turned it off
 * shows no logo; when every client did, the cell says "Turned off".
 */
const Agents = ({
  row,
  configurations,
}: {
  row: ServerRow;
  configurations: Configuration[];
}) => (
  <td className="flex items-center p-0">
    {row.turnedOff ? (
      <span className="type-secondary text-text-muted">Turned off</span>
    ) : (
      <LogoStack clients={onClients(row, configurations)} size={24} />
    )}
  </td>
);

/**
 * One server in the list (DESIGN.md §4 "Lists and rows", the table tokens): name and plugin tag
 * over the URL or command, the transport pills and the versions count, the tools, and the 24px
 * logos of the agents that have it on. Display only: nothing on a row runs a command. The row
 * another screen opened (`inView`) scrolls into view when it mounts.
 */
export const ServerRowItem = ({
  row,
  configurations,
  inView = false,
}: {
  row: ServerRow;
  configurations: Configuration[];
  /** The route names this server: the row comes into view. */
  inView?: boolean;
}) => {
  const ref = useRef<HTMLTableRowElement>(null);
  useEffect(() => {
    if (inView) {
      ref.current?.scrollIntoView({ block: "center" });
    }
  }, [inView]);
  return (
    <tr
      ref={ref}
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
      <Agents row={row} configurations={configurations} />
    </tr>
  );
};
