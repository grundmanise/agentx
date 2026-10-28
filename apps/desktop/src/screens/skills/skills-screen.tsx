import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router";

import { skillPath } from "#/screens/skills/open.ts";
import {
  filterRows,
  groupRows,
  noFilters,
  projectRows,
} from "#/screens/skills/projection.ts";
import type {
  Filters,
  SkillRow,
  SortOrder,
} from "#/screens/skills/projection.ts";
import { SkillList } from "#/screens/skills/skill-list.tsx";
import { useAppStore } from "#/store/store.ts";

const none: SkillRow[] = [];

/** How far each key moves the selection. */
const steps: Record<string, number> = {
  ArrowDown: 1,
  ArrowUp: -1,
  j: 1,
  k: -1,
};

/** The skill the route names: `/skills/<key>`; a deeper route is another screen's. */
const useRouteKey = (): string | undefined => {
  const splat = useParams()["*"] ?? "";
  if (splat === "" || splat.includes("/")) {
    return undefined;
  }
  try {
    return decodeURIComponent(splat);
  } catch {
    return splat;
  }
};

/** Whether a key press belongs to a field or an overlay, which the list then leaves alone. */
const inField = (target: EventTarget | null) =>
  target instanceof HTMLElement &&
  (target.closest("input, textarea, [contenteditable=true], [role=dialog]") !==
    null ||
    document.querySelector("[role=dialog]") !== null);

/**
 * The Skills screen: every skill on the machine in one list grouped by where it comes from, and
 * the pane of the selected one. Read-only: nothing here changes the machine. The selection
 * is the route, so another screen can open a skill by its path.
 */
export const SkillsScreen = () => {
  const navigate = useNavigate();
  const routeKey = useRouteKey();
  const inventory = useAppStore((s) => s.inventory);
  const updates = useAppStore((s) => s.updates);
  const adoption = useAppStore((s) => s.adoption);
  const [filters, setFilters] = useState<Filters>(noFilters);
  const [order, setOrder] = useState<SortOrder>("asc");

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
        rows.find((r) => r.name === routeKey))) ?? flat[0];
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

  // j/k and the arrows move the selection through the visible rows, in group order.
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
      const next =
        at === -1
          ? (flat[0] ?? flat.at(-1))
          : flat[Math.min(flat.length - 1, Math.max(0, at + step))];
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
      />
      <section
        aria-label="Skill details"
        className="bg-shell-content-bg min-w-0 flex-1 overflow-auto"
      >
        {/* The detail pane comes in a later change; until then, the selected skill's name. */}
        <p className="type-secondary text-text-hint pt-detail-top px-detail m-0">
          {selected === undefined ? "No skills on this machine" : selected.name}
        </p>
      </section>
    </div>
  );
};
