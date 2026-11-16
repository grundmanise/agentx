// How the user set up the Skills list: the search, the filters, the sort order and the open
// plugin subgroups. It lives outside the screen, so it stays while another screen shows, and it
// lasts until the app quits.
import { create } from "zustand";

import { noFilters } from "#/screens/skills/projection.ts";
import type { Filters, SortOrder } from "#/screens/skills/projection.ts";

export interface SkillsView {
  filters: Filters;
  order: SortOrder;
  /** The keys of the open plugin subgroups; every other one is collapsed. */
  expanded: ReadonlySet<string>;
}

export const useSkillsView = create<SkillsView>()(() => ({
  expanded: new Set<string>(),
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
export const toggleExpanded = (key: string) => {
  useSkillsView.setState(({ expanded }) => {
    const next = new Set(expanded);
    if (!next.delete(key)) {
      next.add(key);
    }
    return { expanded: next };
  });
};

/** Opens exactly the plugin subgroups with these keys; every other one is collapsed. */
export const setExpanded = (keys: Iterable<string>) => {
  useSkillsView.setState({ expanded: new Set(keys) });
};
