// What each agent sees, in the detail pane's AGENTS: an info button beside the caption, and, when
// some agent sees more than one skill of the name, a notice under the chips with Details. Both
// open the same popover: a table with a column for each place that holds a skill of the name and
// a row for each agent that sees more than one, highlighted, then a row for the agents that see
// the same one (`sightsTable`). `onRename`, once the app can rename a skill, adds Rename yours to
// the notice and the popover.
import { useId } from "react";
import type { ReactElement } from "react";

import { AlertTriangleIcon } from "#/components/icons/alert-triangle.tsx";
import { CheckIcon } from "#/components/icons/check.tsx";
import { InfoIcon } from "#/components/icons/info.tsx";
import { Button } from "#/components/ui/button.tsx";
import {
  Popover,
  PopoverContent,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from "#/components/ui/popover.tsx";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "#/components/ui/table.tsx";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "#/components/ui/tooltip.tsx";
import { clashText, sightsTable } from "#/screens/skills/sights.ts";
import type { NameSights, SightsTable } from "#/screens/skills/sights.ts";

const TITLE = "What each agent sees";

/** Renames your library's skill of the name; Rename yours shows only with it. */
type OnRename = (() => void) | undefined;

const RenameYours = ({
  table,
  onRename,
}: {
  table: SightsTable;
  onRename: OnRename;
}) =>
  onRename !== undefined && table.renamable ? (
    <Button variant="primary" size="sm" onClick={onRename}>
      Rename yours
    </Button>
  ) : null;

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

/** The popover's body: the table, then what the highlighted rows mean. */
const SightsBody = ({
  table,
  onRename,
}: {
  table: SightsTable;
  onRename: OnRename;
}) => {
  const titleId = useId();
  return (
    <PopoverContent aria-labelledby={titleId}>
      <PopoverHeader>
        <PopoverTitle id={titleId}>{table.title}</PopoverTitle>
      </PopoverHeader>
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
        <div className="flex flex-col items-start gap-2.5">
          <p className="type-secondary text-text-muted m-0">{table.footer}</p>
          <RenameYours table={table} onRename={onRename} />
        </div>
      )}
    </PopoverContent>
  );
};

/** The popover, opened by `trigger`. */
const SightsPopover = ({
  sights,
  trigger,
  onRename,
}: {
  sights: NameSights;
  trigger: ReactElement;
  onRename: OnRename;
}) => (
  <Popover>
    <PopoverTrigger asChild>{trigger}</PopoverTrigger>
    <SightsBody table={sightsTable(sights)} onRename={onRename} />
  </Popover>
);

/** The info button beside the AGENTS caption, while some agent sees the skill. */
export const SightsButton = ({
  sights,
  onRename,
}: {
  sights: NameSights;
  onRename?: () => void;
}) => {
  if (sights.places.length === 0) {
    return null;
  }
  return (
    <Tooltip>
      <SightsPopover
        sights={sights}
        onRename={onRename}
        trigger={
          <TooltipTrigger asChild>
            <Button variant="quiet" size="icon-xs" aria-label={TITLE}>
              <InfoIcon size="xs" />
            </Button>
          </TooltipTrigger>
        }
      />
      <TooltipContent>{TITLE}</TooltipContent>
    </Tooltip>
  );
};

/**
 * The notice under the chips while some agent sees more than one skill of the name: who also sees
 * which other one, with Details.
 */
export const SightsNotice = ({
  sights,
  onRename,
}: {
  sights: NameSights;
  onRename?: () => void;
}) => {
  const text = clashText(sights);
  if (text === undefined) {
    return null;
  }
  return (
    <div
      role="note"
      aria-label="Skills of the same name"
      className="rounded-sights-notice border-tone-attention-border bg-tone-attention-bg flex items-start gap-2.5 border px-3 py-2.5"
    >
      <span className="text-tone-attention-fg flex h-5 flex-none items-center">
        <AlertTriangleIcon size="xs" />
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-2.5">
        <p className="type-secondary text-text-secondary m-0 text-pretty">
          {text}
        </p>
        <div className="flex flex-wrap gap-1.5">
          <RenameYours table={sightsTable(sights)} onRename={onRename} />
          <SightsPopover
            sights={sights}
            onRename={onRename}
            trigger={
              <Button variant="secondary" size="sm">
                Details
              </Button>
            }
          />
        </div>
      </div>
    </div>
  );
};
