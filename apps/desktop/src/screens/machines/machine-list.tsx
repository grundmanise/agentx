import type { ReactNode } from "react";

import { LogoStack } from "#/components/agent-logo.tsx";
import { ListRow } from "#/components/list-row.tsx";
import { Badge } from "#/components/ui/badge.tsx";
import { Spinner } from "#/components/ui/spinner.tsx";
import { useNow } from "#/lib/now.ts";
import { plural } from "#/lib/plural.ts";
import { changedWhen } from "#/screens/machines/projection.ts";
import type { MachineView } from "#/screens/machines/projection.ts";

/** How many agent logos a row draws; the rest are a "+n" count. */
const MAX_LOGOS = 4;

/** The name a machine goes by: its label, or what the glossary calls the cloud machine. */
export const nameOf = (machine: MachineView) =>
  machine.kind === "cloud" ? "Cloud machine" : machine.label;

/**
 * One machine's row: its label, the This machine pill and its agents' logos, then when its report
 * last changed and how many skills its agents see. This machine's row is as it is now, so it says
 * no time.
 */
const MachineRow = ({
  machine,
  now,
  selected,
  onSelect,
}: {
  machine: MachineView;
  now: number;
  selected: boolean;
  onSelect: () => void;
}) => {
  const clients = machine.agents.map((a) => ({ client: a.id, name: a.name }));
  const changed =
    machine.changedAt === undefined
      ? undefined
      : changedWhen(machine.changedAt, now);
  const line = [
    changed !== undefined && `Changed ${changed}`,
    plural(machine.skillCount, "skill"),
  ]
    .filter((part) => part !== false)
    .join(" · ");
  const name = nameOf(machine);
  return (
    <ListRow
      label={name}
      description={[machine.kind === "this" ? "This machine" : "", line]
        .filter((part) => part !== "")
        .join(". ")}
      selected={selected}
      onSelect={onSelect}
    >
      <span className="gap-row-line flex min-w-0 flex-1 flex-col">
        <span className="gap-row-inline flex h-5 items-center">
          <span className="type-row-name text-row-name truncate">{name}</span>
          {machine.kind === "this" && (
            <Badge tone="neutral">This machine</Badge>
          )}
          <span className="flex-1" />
          {clients.length > 0 && (
            <LogoStack
              clients={clients.slice(0, MAX_LOGOS)}
              size={20}
              hidden={clients.slice(MAX_LOGOS)}
            />
          )}
        </span>
        <span className="type-row-desc text-row-desc truncate">{line}</span>
      </span>
    </ListRow>
  );
};

/**
 * The Machines list column: the title and count, then this machine, the other machines the hub
 * holds and the cloud machine, and under them a line when no other machine can show yet.
 */
export const MachineList = ({
  machines,
  selectedId,
  onSelect,
  reading,
  note,
}: {
  machines: MachineView[];
  selectedId: string | undefined;
  onSelect: (machine: MachineView) => void;
  /** The hub is being read: a spinner turns beside the count. */
  reading: boolean;
  /** What stands under the rows while no other machine shows. */
  note?: ReactNode;
}) => {
  const now = useNow();
  return (
    <section
      aria-label="Machines list"
      aria-busy={reading ? true : undefined}
      className="w-split-list bg-shell-content-bg border-shell-divider flex flex-none flex-col border-r"
    >
      <div className="pt-split-header-top px-split-header flex items-baseline gap-3 pb-0">
        <h1 className="type-page-title text-text-primary m-0">Machines</h1>
        <span
          aria-label={plural(machines.length, "machine")}
          className="type-technical text-text-caption"
        >
          {machines.length}
        </span>
        {reading && (
          <span className="text-text-hint flex self-center">
            <Spinner />
          </span>
        )}
      </div>
      <div className="pt-split-list-top pr-split-list-right pb-split-list-bottom pl-split-list-left flex min-h-0 flex-1 flex-col gap-6 overflow-auto">
        <ul className="gap-rows m-0 flex list-none flex-col p-0">
          {machines.map((machine) => (
            <li key={machine.id} className="flex">
              <MachineRow
                machine={machine}
                now={now}
                selected={machine.id === selectedId}
                onSelect={() => {
                  onSelect(machine);
                }}
              />
            </li>
          ))}
        </ul>
        {note}
      </div>
    </section>
  );
};
