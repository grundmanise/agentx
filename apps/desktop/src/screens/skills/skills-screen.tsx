import { useWindowEvent } from "@mantine/hooks";
import { useEffect, useEffectEvent, useRef } from "react";
import { useNavigate } from "react-router";

import { useRouteAgent } from "#/lib/agent-route.ts";
import { useRouteKey } from "#/lib/route-key.ts";
import { AdoptReplaceConfirm } from "#/screens/skills/actions.tsx";
import { ForkDialogs } from "#/screens/skills/fork-dialog.tsx";
import { skillPath } from "#/screens/skills/open.ts";
import {
  filterRows,
  forksOf,
  groupRows,
  GROUP_PLUGINS,
  inCollapsed,
  noFilters,
  projectRows,
  subgroupKey,
} from "#/screens/skills/projection.ts";
import type { Sections, SkillRow } from "#/screens/skills/projection.ts";
import { RenameDialogs } from "#/screens/skills/rename-dialog.tsx";
import { SkillDetail } from "#/screens/skills/skill-detail.tsx";
import { SkillList } from "#/screens/skills/skill-list.tsx";
import { useSkillsView } from "#/screens/skills/view.ts";
import { useStartup } from "#/startup/startup.ts";
import { useAppStore } from "#/store/store.ts";

const none: SkillRow[] = [];

/** How far each key moves the selection. */
const steps: Record<string, number> = {
  ArrowDown: 1,
  ArrowUp: -1,
  j: 1,
  k: -1,
};

/**
 * The row a key press moves the selection to: the nearest shown row after the one at `at` for a
 * step down, before it for a step up, and the first shown row when no row is selected. A row of a
 * collapsed group or subgroup is not shown.
 */
const nextShown = (
  flat: SkillRow[],
  at: number,
  step: number,
  sections: Sections
) => {
  const by = at === -1 ? 1 : step;
  const from = at === -1 ? 0 : at + step;
  const count = by > 0 ? flat.length - from : from + 1;
  return Array.from(
    { length: Math.max(0, count) },
    (_, n) => flat[from + n * by]
  ).find((row) => row !== undefined && !inCollapsed(row, sections));
};

/** Whether a key press belongs to a field or an overlay, which the list then leaves alone. */
const inField = (target: EventTarget | null) =>
  target instanceof HTMLElement &&
  (target.closest("input, textarea, [contenteditable=true], [role=dialog]") !==
    null ||
    document.querySelector("[role=dialog]") !== null);

/**
 * The Skills screen: every skill on the machine in one list grouped by where it comes from, and
 * the detail pane of the selected one. Read-only: nothing here changes the machine. The selection
 * is the route, so another screen can open a skill by its path.
 */
export const SkillsScreen = () => {
  const navigate = useNavigate();
  // The skill the route names: `/skills/<key>`; a deeper route is another screen's.
  const routeKey = useRouteKey();
  const inventory = useAppStore((s) => s.inventory);
  // The search, filters, sort order and collapsed sections stay while another screen shows.
  const {
    collapsed,
    expanded,
    filters,
    openSections,
    order,
    setFilters,
    setOrder,
    setSections,
    toggleCollapsed,
    toggleExpanded,
  } = useSkillsView();
  useRouteAgent((agent) => {
    setFilters({ ...noFilters, agents: [agent] });
  });
  const sections = { collapsed, expanded };

  const rows = inventory === undefined ? none : projectRows({ inventory });
  const groups = groupRows(filterRows(rows, filters), order);
  const flat = groups.flatMap((g) => g.rows);
  // Collapse all closes every group and every plugin's subgroup; Expand all opens every group and
  // each plugin's subgroup the filters list.
  const toggleAll = (open: boolean) => {
    if (open) {
      setSections(
        [],
        groups.flatMap((g) => g.subgroups?.map((s) => s.key) ?? [])
      );
    } else {
      setSections(
        groups.map((g) => g.key),
        []
      );
    }
  };
  const selected =
    (routeKey === undefined
      ? undefined
      : (rows.find((r) => r.key === routeKey) ??
        rows.find((r) => r.name === routeKey))) ??
    // The first row whatever is collapsed, so collapsing a section never changes the selection.
    flat[0];
  // A newly selected row in a collapsed group or subgroup, such as a plugin's skill another screen
  // opens, opens its sections so the list shows it. Collapsing one later keeps it collapsed, and so
  // does coming back to the screen, when the effect runs again for the same row.
  const reveal = useEffectEvent((key: string | undefined) => {
    if (
      selected !== undefined &&
      selected.key === key &&
      inCollapsed(selected, sections)
    ) {
      openSections(
        selected.group,
        selected.group === GROUP_PLUGINS ? subgroupKey(selected) : undefined
      );
    }
  });
  const shownKey = selected?.key;
  const revealed = useRef<string | null>(null);
  useEffect(() => {
    if (shownKey !== undefined && shownKey !== revealed.current) {
      revealed.current = shownKey;
      reveal(shownKey);
    }
  }, [shownKey]);
  const configurations = inventory?.configurations ?? [];

  // The key the next key press moves from: the last selection made, which the route may not show
  // yet when keys come fast. `requested` queues the selections made here whose routes have not
  // shown yet; the routes show them in order. A route in the queue drops it up to that entry, and
  // `current` stays: a newer selection may still be on its way. Any other route, such as a skill
  // another screen opens, takes over.
  const current = useRef(selected?.key);
  const requested = useRef<string[]>([]);
  const selectedKey = selected?.key;
  useEffect(() => {
    const at =
      selectedKey === undefined ? -1 : requested.current.indexOf(selectedKey);
    if (at === -1) {
      requested.current = [];
      current.current = selectedKey;
    } else {
      requested.current = requested.current.slice(at + 1);
    }
  }, [selectedKey]);
  const select = (row: SkillRow) => {
    current.current = row.key;
    requested.current = [...requested.current, row.key];
    void navigate(skillPath(row.key));
  };

  // j/k and the arrows move the selection through the visible rows, in group order, past the
  // rows of a collapsed group or subgroup. Not under the launch pane: the list waits for the startup checks.
  useWindowEvent("keydown", (e) => {
    if (
      !useStartup.getState().ready ||
      e.metaKey ||
      e.ctrlKey ||
      e.altKey ||
      inField(e.target)
    ) {
      return;
    }
    const step = steps[e.key] ?? 0;
    if (step === 0 || flat.length === 0) {
      return;
    }
    e.preventDefault();
    const at = flat.findIndex((r) => r.key === current.current);
    const next = nextShown(flat, at, step, sections);
    if (next !== undefined && next.key !== current.current) {
      select(next);
    }
  });

  return (
    <div className="flex min-h-0 flex-1">
      <SkillList
        rows={rows}
        groups={groups}
        configurations={configurations}
        filters={filters}
        onFilters={setFilters}
        order={order}
        onOrder={setOrder}
        selectedKey={selected?.key}
        onSelect={select}
        collapse={sections}
        onToggleGroup={toggleCollapsed}
        onToggle={toggleExpanded}
        onToggleAll={toggleAll}
      />
      <section
        aria-label="Skill details"
        className="bg-shell-content-bg min-w-0 flex-1 overflow-auto"
      >
        {selected === undefined ? (
          <p className="type-secondary text-text-hint pt-detail-top px-detail m-0">
            {rows.length === 0 ? "No skills on this machine" : "Select a skill"}
          </p>
        ) : (
          <SkillDetail
            key={selected.key}
            row={selected}
            forks={forksOf(selected, rows)}
            sameName={rows.filter(
              (r) => r.name === selected.name && r.key !== selected.key
            )}
            configurations={configurations}
          />
        )}
      </section>
      <ForkDialogs />
      <RenameDialogs />
      <AdoptReplaceConfirm />
    </div>
  );
};
