// How the user set up the Skills list: the search, the filters, the sort order, the collapsed
// groups and the open plugin subgroups. It is the screen's own state: the screen stays mounted
// while another one shows (see app/kept-screens.tsx), so it stays until the app quits.
import { useMemo, useState } from "react";

import { noFilters } from "#/screens/skills/projection.ts";
import type { Filters, SortOrder } from "#/screens/skills/projection.ts";

/** A copy of `keys` with `key` taken out if it is in, else put in. */
const flipped = (keys: ReadonlySet<string>, key: string) => {
  const next = new Set(keys);
  if (!next.delete(key)) {
    next.add(key);
  }
  return next;
};

const noKeys: ReadonlySet<string> = new Set<string>();

/** The Skills list's setup, and the changes the list makes to it. */
export const useSkillsView = () => {
  const [filters, setFilters] = useState<Filters>(noFilters);
  const [order, setOrder] = useState<SortOrder>("asc");
  /** The keys of the collapsed groups; every other one is open. */
  const [collapsed, setCollapsed] = useState(noKeys);
  /** The keys of the open plugin subgroups; every other one is collapsed. */
  const [expanded, setExpanded] = useState(noKeys);
  const actions = useMemo(
    () => ({
      /** Opens a group and, for a plugin's skill, its subgroup, so the list shows the row. */
      openSections: (group: string, subgroup?: string) => {
        setCollapsed((keys) => {
          const next = new Set(keys);
          next.delete(group);
          return next;
        });
        if (subgroup !== undefined) {
          setExpanded((keys) => new Set([...keys, subgroup]));
        }
      },
      setFilters,
      setOrder,
      /** Collapses exactly the groups with keys in `groups` and opens exactly the plugin subgroups with keys in `subgroups`. */
      setSections: (groups: Iterable<string>, subgroups: Iterable<string>) => {
        setCollapsed(new Set(groups));
        setExpanded(new Set(subgroups));
      },
      /** Collapses or expands a group, by its key. */
      toggleCollapsed: (key: string) => {
        setCollapsed((keys) => flipped(keys, key));
      },
      /** Collapses or expands a plugin's subgroup, by its key. */
      toggleExpanded: (key: string) => {
        setExpanded((keys) => flipped(keys, key));
      },
    }),
    []
  );
  return { collapsed, expanded, filters, order, ...actions };
};
