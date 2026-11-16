import { useEffect, useMemo, useRef } from "react";
import { useNavigate } from "react-router";

import { useRouteKey } from "#/lib/route-key.ts";
import { ForkDialogs } from "#/screens/skills/fork-dialog.tsx";
import { skillPath } from "#/screens/skills/open.ts";
import {
  filterRows,
  forksOf,
  groupRows,
  inCollapsed,
  projectRows,
} from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { SkillDetail } from "#/screens/skills/skill-detail.tsx";
import { SkillList } from "#/screens/skills/skill-list.tsx";
import {
  setFilters,
  setOrder,
  toggleCollapsed,
  useSkillsView,
} from "#/screens/skills/view.ts";
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
 * collapsed subgroup is not shown.
 */
const nextShown = (
  flat: SkillRow[],
  at: number,
  step: number,
  collapsed: ReadonlySet<string>
) => {
  const by = at === -1 ? 1 : step;
  const from = at === -1 ? 0 : at + step;
  const count = by > 0 ? flat.length - from : from + 1;
  return Array.from(
    { length: Math.max(0, count) },
    (_, n) => flat[from + n * by]
  ).find((row) => row !== undefined && !inCollapsed(row, collapsed));
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
  const updates = useAppStore((s) => s.updates);
  const adoption = useAppStore((s) => s.adoption);
  // The search, filters, sort order and collapsed subgroups stay while another screen shows.
  const filters = useSkillsView((s) => s.filters);
  const order = useSkillsView((s) => s.order);
  const collapsed = useSkillsView((s) => s.collapsed);

  const rows = useMemo(
    () =>
      inventory === undefined
        ? none
        : projectRows({ adoption, inventory, updates }),
    [inventory, updates, adoption]
  );
  const groups = useMemo(
    () => groupRows(filterRows(rows, filters), order),
    [rows, filters, order]
  );
  const flat = useMemo(() => groups.flatMap((g) => g.rows), [groups]);
  const selected =
    (routeKey === undefined
      ? undefined
      : (rows.find((r) => r.key === routeKey) ??
        rows.find((r) => r.name === routeKey))) ??
    flat.find((r) => !inCollapsed(r, collapsed)) ??
    flat[0];
  const configurations = inventory?.configurations ?? [];

  // The key the next key press moves from: the last selection made, which the route may not show
  // yet when keys come fast.
  const current = useRef(selected?.key);
  const selectedKey = selected?.key;
  useEffect(() => {
    current.current = selectedKey;
  }, [selectedKey]);
  const select = (row: SkillRow) => {
    current.current = row.key;
    void navigate(skillPath(row.key));
  };

  // j/k and the arrows move the selection through the visible rows, in group order, past the
  // rows of a collapsed subgroup.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey || inField(e.target)) {
        return;
      }
      const step = steps[e.key] ?? 0;
      if (step === 0 || flat.length === 0) {
        return;
      }
      e.preventDefault();
      const at = flat.findIndex((r) => r.key === current.current);
      const next = nextShown(flat, at, step, collapsed);
      if (next !== undefined && next.key !== current.current) {
        select(next);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
    };
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
        collapsed={collapsed}
        onToggle={toggleCollapsed}
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
            configurations={configurations}
          />
        )}
      </section>
      <ForkDialogs />
    </div>
  );
};
