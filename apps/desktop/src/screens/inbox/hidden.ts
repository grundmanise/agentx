// The cards the user chose to keep as they are: Keep and Keep as is hide a card until the app
// restarts and run nothing. The app keeps no state the CLI does not have, and a displaced
// placement or a skill its source removed stays reported until it changes, so the choice lives
// here, in memory, and a restart shows the card again.
import { create } from "zustand";

interface Hidden {
  ids: Record<string, true>;
}

export const useHiddenStore = create<Hidden>()(() => ({ ids: {} }));

/** Hides the card `id` until the app restarts. */
export const hideCard = (id: string) => {
  useHiddenStore.setState((s) => ({ ids: { ...s.ids, [id]: true } }));
};

/** The ids of the cards hidden so far. */
export const useHidden = () => useHiddenStore((s) => s.ids);
