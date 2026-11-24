// Opening a machine from another screen: the Also on line of a skill's details comes here. The
// Machines screen opens the machine its route names.
import { navigateTo } from "#/lib/navigate.ts";
import { useMachinesView } from "#/screens/machines/view.ts";

/** The route of a machine: its machine id. */
export const machinePath = (id: string) =>
  `/machines/${encodeURIComponent(id)}`;

/** Shows the Machines screen with the machine `id` open, in the By machine view. */
export const openMachine = (id: string) => {
  useMachinesView.setState({ by: "machine" });
  navigateTo(machinePath(id));
};
