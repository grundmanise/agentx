import { useEffect } from "react";
import type { ReactNode } from "react";
import { useNavigate } from "react-router";

import { useRouteKey } from "#/lib/route-key.ts";
import { MachineDetail } from "#/screens/machines/machine-detail.tsx";
import { MachineList } from "#/screens/machines/machine-list.tsx";
import { machinePath } from "#/screens/machines/open.ts";
import { useMachines } from "#/screens/machines/use-machines.ts";
import { hubOf } from "#/screens/skills/banners.ts";
import { OpenSettingsButton } from "#/screens/skills/publish-dialog.tsx";
import { loadFleet, loadLabel } from "#/store/loaders.ts";
import { usePending } from "#/store/runner.ts";
import { useAppStore } from "#/store/store.ts";

/** Whether a hub is set; undefined until the sources are read, when the screen would guess. */
const useHubSet = () => {
  const hub = useAppStore((s) => hubOf(s.sources) !== undefined);
  const loadingSources = usePending(loadLabel(["source", "list"]));
  const noSources = useAppStore((s) => s.sources.length === 0);
  return loadingSources && noSources ? undefined : hub;
};

/** One line under the rows while no other machine shows, with at most one action. */
const Note = ({
  children,
  action,
}: {
  children: ReactNode;
  action?: ReactNode;
}) => (
  <div className="flex flex-col items-center gap-3 px-3.5">
    <p className="type-secondary text-text-hint m-0 text-center text-pretty">
      {children}
    </p>
    {action}
  </div>
);

/**
 * The Machines screen: this machine, the other machines that wrote an inventory report to the hub
 * and the cloud machine in the list on the left, and the open machine on the right, with the
 * skills each of its agents sees. This machine is the applied inventory; the others are what the
 * hub holds, read with `machine list` each time the screen opens and after a report is removed,
 * and sent by serve after its check of the hub. The open machine is the route, so another screen
 * can open a machine by its path.
 */
export const MachinesScreen = () => {
  const navigate = useNavigate();
  const machines = useMachines();
  const hub = useHubSet();
  const fleetRead = useAppStore((s) => s.fleet !== undefined);
  const reading = usePending(loadLabel(["machine", "list"]));
  // A screen that shows again runs its effects again: the hub is read each time this one opens,
  // while a hub is set, since the command needs one.
  useEffect(() => {
    if (hub === true) {
      void loadFleet();
    }
  }, [hub]);
  // The machine the route names, `/machines/<id>`, while it is listed, else the first one: this
  // machine.
  const routeKey = useRouteKey();
  const selected = machines.find((m) => m.id === routeKey) ?? machines[0];
  let note: ReactNode;
  if (hub === false) {
    note = (
      <Note action={<OpenSettingsButton />}>
        Set a hub to see your other machines here.
      </Note>
    );
  } else if (
    hub === true &&
    fleetRead &&
    !machines.some((m) => m.kind === "other")
  ) {
    note = <Note>A machine shows here once Report inventory is on there.</Note>;
  }
  return (
    <div className="flex min-h-0 flex-1">
      <MachineList
        machines={machines}
        selectedId={selected?.id}
        onSelect={(machine) => {
          void navigate(machinePath(machine.id));
        }}
        reading={reading}
        note={note}
      />
      <section
        aria-label="Machine details"
        className="bg-shell-content-bg flex min-w-0 flex-1 flex-col overflow-hidden"
      >
        {selected !== undefined && (
          <MachineDetail key={selected.id} machine={selected} />
        )}
      </section>
    </div>
  );
};
