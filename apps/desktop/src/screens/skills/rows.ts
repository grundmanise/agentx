// The Skills screen's rows outside the Skills screen: the title bar's sync status and the
// palette read every skill through the same projection (src/screens/skills/projection.ts), so
// they count and name skills as the list does.

import { projectRows } from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { useAppStore } from "#/store/store.ts";
import type { AppState } from "#/store/store.ts";

const none: SkillRow[] = [];

const rowsOf = ({
  inventory,
  adoption,
}: Pick<AppState, "inventory" | "adoption">) =>
  inventory === undefined ? none : projectRows({ adoption, inventory });

/** Every skill on the machine as a row, from the store as it is now. */
export const currentRows = () => rowsOf(useAppStore.getState());

/** Every skill on the machine as a row; a new list only when the inventory or a slice it reads changes. */
export const useSkillRows = () => {
  const inventory = useAppStore((s) => s.inventory);
  const adoption = useAppStore((s) => s.adoption);
  return rowsOf({ adoption, inventory });
};
