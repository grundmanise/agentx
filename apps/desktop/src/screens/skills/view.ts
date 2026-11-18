// How the user set up the Skills list: the search, the filters, the sort order, the collapsed
// groups and the open plugin subgroups. It lives outside the screen, so it stays while another
// screen shows, and it lasts until the app quits.
import { create } from "zustand";

import { noFilters } from "#/screens/skills/projection.ts";
import type { Filters, SortOrder } from "#/screens/skills/projection.ts";

export interface SkillsView {
  filters: Filters;
  order: SortOrder;
  /** The keys of the collapsed groups; every other one is open. */
  collapsed: ReadonlySet<string>;
  /** The keys of the open plugin subgroups; every other one is collapsed. */
  expanded: ReadonlySet<string>;
}

export const useSkillsView = create<SkillsView>()(() => ({
  collapsed: new Set<string>(),
  expanded: new Set<string>(),
  filters: noFilters,
  order: "asc",
}));

export const setFilters = (filters: Filters) => {
  useSkillsView.setState({ filters });
};

export const setOrder = (order: SortOrder) => {
  useSkillsView.setState({ order });
};

/** A copy of `keys` with `key` taken out if it is in, else put in. */
const flipped = (keys: ReadonlySet<string>, key: string) => {
  const next = new Set(keys);
  if (!next.delete(key)) {
    next.add(key);
  }
  return next;
};

/** Collapses or expands a group, by its key. */
export const toggleCollapsed = (key: string) => {
  useSkillsView.setState(({ collapsed }) => ({
    collapsed: flipped(collapsed, key),
  }));
};

/** Collapses or expands a plugin's subgroup, by its key. */
export const toggleExpanded = (key: string) => {
  useSkillsView.setState(({ expanded }) => ({
    expanded: flipped(expanded, key),
  }));
};

/** Opens exactly the plugin subgroups with these keys; every other one is collapsed. */
export const setExpanded = (keys: Iterable<string>) => {
  useSkillsView.setState({ expanded: new Set(keys) });
};

/** Opens a group and, for a plugin's skill, its subgroup, so the list shows the row. */
export const openSections = (group: string, subgroup?: string) => {
  useSkillsView.setState(({ collapsed, expanded }) => {
    const nextCollapsed = new Set(collapsed);
    nextCollapsed.delete(group);
    const nextExpanded = new Set(expanded);
    if (subgroup !== undefined) {
      nextExpanded.add(subgroup);
    }
    return { collapsed: nextCollapsed, expanded: nextExpanded };
  });
};
