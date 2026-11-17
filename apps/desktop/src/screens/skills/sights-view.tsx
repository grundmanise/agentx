// What each agent sees, in the detail pane's AGENTS: when some agent sees more than one skill of
// the name, a notice under the chips that ends with Details, a word with a dashed underline. Hover
// on it opens a hover card: a table with a column for each place that holds a skill of the name
// and a row for each agent that sees more than one, highlighted, then a row for the agents that
// see the same one (`sightsTable`). The notice is a Callout, as the detail pane's banner is, with
// no button; the hover card only explains.
import { useId } from "react";

import { Callout } from "#/components/callout.tsx";
import { CheckIcon } from "#/components/icons/check.tsx";
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "#/components/ui/hover-card.tsx";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "#/components/ui/table.tsx";
import { clashText, sightsTable } from "#/screens/skills/sights.ts";
import type { NameSights, SightsTable } from "#/screens/skills/sights.ts";

/** ✓ or –, with the words a screen reader says for each. */
const Sees = ({ sees }: { sees: boolean }) =>
  sees ? (
    <span className="text-tone-primary-fg inline-flex align-middle">
      <CheckIcon size="xs" />
      <span className="sr-only">Sees it</span>
    </span>
  ) : (
    <span className="text-text-hint">
      <span aria-hidden>–</span>
      <span className="sr-only">Does not see it</span>
    </span>
  );

/** The hover card's body: its title, the table, then what the highlighted rows mean. */
const SightsBody = ({ table }: { table: SightsTable }) => {
  const titleId = useId();
  return (
    <>
      <h2
        id={titleId}
        className="type-secondary text-text-primary m-0 font-medium"
      >
        {table.title}
      </h2>
      <Table aria-labelledby={titleId}>
        <TableHeader>
          <TableRow tone="header">
            <TableHead>Agent</TableHead>
            {table.columns.map((column) => (
              <TableHead key={column.key} align="center">
                {column.heading}
              </TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {table.rows.map((row) => (
            <TableRow key={row.key} tone={row.clash ? "attention" : "default"}>
              <TableCell scope="row">
                {row.agents.map((a) => a.name).join(", ")}
              </TableCell>
              {row.sees.map((sees, i) => (
                <TableCell key={table.columns[i]?.key} align="center">
                  <Sees sees={sees} />
                </TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {table.footer !== undefined && (
        <p className="type-secondary text-text-muted m-0">{table.footer}</p>
      )}
    </>
  );
};

/**
 * The notice under the chips while some agent sees more than one skill of the name, a callout: how
 * many skills of the name there are, then Details, which opens the table on hover.
 */
export const SightsNotice = ({ sights }: { sights: NameSights }) => {
  const clash = clashText(sights);
  if (clash === undefined) {
    return null;
  }
  const table = sightsTable(sights);
  return (
    <Callout
      tone="attention"
      label="Skills of the same name"
      title={clash.title}
      text={
        <>
          {clash.text}{" "}
          <HoverCard>
            <HoverCardTrigger asChild>
              <span className="underline decoration-dashed underline-offset-2">
                Details
              </span>
            </HoverCardTrigger>
            <HoverCardContent>
              <SightsBody table={table} />
            </HoverCardContent>
          </HoverCard>
        </>
      }
    />
  );
};
