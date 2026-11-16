// How the user set up the Skills list: the search, the filters, the sort order and the collapsed
// plugin subgroups. It lives outside the screen, so it stays while another screen shows, and it
// lasts until the app quits.
import { create } from "zustand";

import { noFilters } from "#/screens/skills/projection.ts";
import type { Filters, SortOrder } from "#/screens/skills/projection.ts";

export interface SkillsView {
  filters: Filters;
  order: SortOrder;
  /** The keys of the plugin subgroups the user collapsed. */
  collapsed: ReadonlySet<string>;
}

export const useSkillsView = create<SkillsView>()(() => ({
  collapsed: new Set<string>(),
  filters: noFilters,
  order: "default",
}));

export const setFilters = (filters: Filters) => {
  useSkillsView.setState({ filters });
};

export const setOrder = (order: SortOrder) => {
  useSkillsView.setState({ order });
};

/** Collapses or expands a plugin's subgroup, by its key. */
export const toggleCollapsed = (key: string) => {
  useSkillsView.setState(({ collapsed }) => {
    const next = new Set(collapsed);
    if (!next.delete(key)) {
      next.add(key);
    }
    return { collapsed: next };
  });
};
