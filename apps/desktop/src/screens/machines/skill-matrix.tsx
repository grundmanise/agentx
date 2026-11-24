import { useId, useMemo } from "react";

import { CheckIcon } from "#/components/icons/check.tsx";
import { Badge } from "#/components/ui/badge.tsx";
import { Checkbox } from "#/components/ui/checkbox.tsx";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "#/components/ui/table.tsx";
import { useNow } from "#/lib/now.ts";
import { skillsAcross } from "#/screens/machines/across.ts";
import type { Cell } from "#/screens/machines/across.ts";
import { MachinesHeader } from "#/screens/machines/machines-header.tsx";
import {
  changedWhen,
  nameOf,
  originText,
} from "#/screens/machines/projection.ts";
import type { MachineView } from "#/screens/machines/projection.ts";
import { useMachinesView } from "#/screens/machines/view.ts";

/**
 * What a machine has of a skill: a check when it holds it, the Another version pill when what it
 * holds is another version than the first machine of the row that holds the skill, a dash when it
 * does not hold it. The marks say what they mean, so the view has no legend.
 */
const Mark = ({ cell }: { cell: Cell }) => {
  if (cell === "other") {
    return <Badge tone="neutral">Another version</Badge>;
  }
  return cell === "same" ? (
    <span className="text-tone-primary-fg inline-flex align-middle">
      <CheckIcon size="xs" />
      <span className="sr-only">Has it</span>
    </span>
  ) : (
    <span className="text-text-hint">
      <span aria-hidden>–</span>
      <span className="sr-only">Not there</span>
    </span>
  );
};

/** A machine's column caption: its name, and under it the This machine pill or when its report last changed. */
const MachineCaption = ({
  machine,
  now,
}: {
  machine: MachineView;
  now: number;
}) => {
  const name = nameOf(machine);
  const changed =
    machine.changedAt === undefined
      ? undefined
      : changedWhen(machine.changedAt, now);
  return (
    <span className="flex flex-col items-center gap-1">
      <span title={name} className="text-text-primary max-w-full truncate">
        {name}
      </span>
      <span className="flex h-5 max-w-full items-center">
        {machine.kind === "this" ? (
          <Badge tone="neutral">This machine</Badge>
        ) : (
          changed !== undefined && (
            <span className="type-row-desc text-row-desc truncate">
              Changed {changed}
            </span>
          )
        )}
      </span>
    </span>
  );
};

/** Only differences: By skill then lists only the skills some machine lacks or holds another version of. */
const OnlyDifferences = () => {
  const id = useId();
  const checked = useMachinesView((s) => s.onlyDifferences);
  return (
    <label htmlFor={id} className="flex items-center gap-2.5">
      <Checkbox
        id={id}
        checked={checked}
        onCheckedChange={(next) => {
          useMachinesView.setState({ onlyDifferences: next === true });
        }}
      />
      <span className="type-secondary text-text-secondary">
        Only differences
      </span>
    </label>
  );
};

/**
 * The By skill view of the Machines screen (DESIGN.md §6 "Machines screen"): every skill any
 * machine holds, one row each, with a column per machine that says whether the machine holds the
 * skill and whether in another version. It is as wide as the screen, shows no machine's details
 * and opens nothing. Only the table scrolls.
 */
export const SkillMatrix = ({
  machines,
  reading,
}: {
  machines: MachineView[];
  /** The hub is being read. */
  reading: boolean;
}) => {
  const now = useNow();
  const onlyDifferences = useMachinesView((s) => s.onlyDifferences);
  const all = useMemo(() => skillsAcross(machines), [machines]);
  const rows = onlyDifferences ? all.filter((row) => row.differs) : all;
  return (
    <section
      aria-label="Machines by skill"
      aria-busy={reading ? true : undefined}
      className="bg-shell-content-bg flex min-h-0 min-w-0 flex-1 flex-col"
    >
      <MachinesHeader count={machines.length} reading={reading} by="skill">
        <OnlyDifferences />
      </MachinesHeader>
      <div className="px-split-toolbar pb-split-list-bottom flex min-h-0 flex-1 flex-col">
        {rows.length === 0 ? (
          <p className="type-secondary text-text-hint m-0 py-10 text-center">
            {all.length === 0
              ? "No skills on your machines"
              : "No differences between your machines"}
          </p>
        ) : (
          // The cells' own padding puts their text under the title's left edge.
          <div className="-mx-2 flex min-h-0 flex-1 flex-col">
            <Table aria-label="Skills on your machines" scroll layout="fixed">
              <colgroup>
                <col className="w-machines-skill-col" />
                {machines.map((machine) => (
                  <col key={machine.id} className="w-machines-machine-col" />
                ))}
                <col />
              </colgroup>
              <TableHeader>
                <TableRow tone="header">
                  <TableHead sticky pinned>
                    Skill
                  </TableHead>
                  {machines.map((machine) => (
                    <TableHead key={machine.id} sticky align="center">
                      <MachineCaption machine={machine} now={now} />
                    </TableHead>
                  ))}
                  <TableHead sticky />
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((row) => (
                  <TableRow key={row.key}>
                    <TableCell scope="row" pinned>
                      <span className="gap-row-line flex min-w-0 flex-col">
                        <span
                          title={row.name}
                          className="type-row-name text-row-name truncate select-text"
                        >
                          {row.name}
                        </span>
                        <span
                          title={
                            row.origin.source?.full === ""
                              ? undefined
                              : row.origin.source?.full
                          }
                          className="type-row-desc text-row-desc truncate"
                        >
                          {originText(row.origin)}
                        </span>
                      </span>
                    </TableCell>
                    {row.cells.map((cell, at) => (
                      <TableCell key={machines[at]?.id} align="center">
                        <Mark cell={cell} />
                      </TableCell>
                    ))}
                    <TableCell />
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </div>
    </section>
  );
};
