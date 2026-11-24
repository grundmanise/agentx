// The machines the app shows, for the Machines screen and the Also on line of a skill's details.
// This is the one place that reads the `fleet` slice.
import { useMemo } from "react";

import { projectMachines } from "#/screens/machines/projection.ts";
import { hubOf } from "#/screens/skills/banners.ts";
import { useAppStore } from "#/store/store.ts";

/**
 * This machine, then the other machines the hub holds and the cloud machine; a new list only when
 * a slice it reads changes. What the store holds of the hub counts only while a hub is set: serve
 * sends the machines it last found to a page that connects later, and it finds no more once its
 * hub is removed, so without a hub the slice may still hold that hub's machines.
 */
export const useMachines = () => {
  const inventory = useAppStore((s) => s.inventory);
  const machine = useAppStore((s) => s.machine);
  const fleet = useAppStore((s) =>
    hubOf(s.sources) === undefined ? undefined : s.fleet
  );
  return useMemo(
    () => projectMachines(inventory, machine, fleet),
    [inventory, machine, fleet]
  );
};
