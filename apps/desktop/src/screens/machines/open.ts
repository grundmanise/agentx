// Opening a machine from another screen: the Also on line of a skill's details comes here. The
// Machines screen opens the machine its route names.
import { navigateTo } from "#/lib/navigate.ts";

/** The route of a machine: its machine id. */
export const machinePath = (id: string) =>
  `/machines/${encodeURIComponent(id)}`;

/** Shows the Machines screen with the machine `id` open. */
export const openMachine = (id: string) => {
  navigateTo(machinePath(id));
};
