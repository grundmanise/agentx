// How the user set up the Machines screen: which of its two views shows, and whether the By skill
// view lists only the skills that differ. It lasts until the app quits. A store, and not the
// screen's own state as on Skills, because another screen changes it: a machine opened from a
// skill's Also on line shows in the By machine view (open.ts).
import { create } from "zustand";

/** The views of the Machines screen: one machine at a time, or every skill across the machines. */
export type MachinesBy = "machine" | "skill";

interface MachinesView {
  by: MachinesBy;
  /** By skill lists only the skills some machine lacks or holds another version of. */
  onlyDifferences: boolean;
}

export const useMachinesView = create<MachinesView>()(() => ({
  by: "machine",
  onlyDifferences: false,
}));
