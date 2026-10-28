import { useMemo } from "react";
import { useNavigate, useParams } from "react-router";

import { skillPath } from "#/screens/skills/open.ts";
import { groupRows, projectRows } from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { SkillList } from "#/screens/skills/skill-list.tsx";
import { useAppStore } from "#/store/store.ts";

const none: SkillRow[] = [];

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

/**
 * The Skills screen: every skill on the machine in one list grouped by where it comes from, and
 * the pane of the selected one. Read-only: nothing here changes the machine. The selection is the
 * route, so another screen can open a skill by its path.
 */
export const SkillsScreen = () => {
  const navigate = useNavigate();
  const routeKey = useRouteKey();
  const inventory = useAppStore((s) => s.inventory);
  const updates = useAppStore((s) => s.updates);
  const adoption = useAppStore((s) => s.adoption);

  const rows = useMemo(
    () =>
      inventory === undefined
        ? none
        : projectRows({ adoption, inventory, updates }),
    [inventory, updates, adoption]
  );
  const groups = useMemo(() => groupRows(rows, "asc"), [rows]);
  const selected =
    (routeKey === undefined
      ? undefined
      : (rows.find((r) => r.key === routeKey) ??
        rows.find((r) => r.name === routeKey))) ?? groups[0]?.rows[0];
  const configurations = inventory?.configurations ?? [];
  const select = (row: SkillRow) => {
    void navigate(skillPath(row.key));
  };

  return (
    <div className="flex min-h-0 flex-1">
      <SkillList
        rows={rows}
        groups={groups}
        configurations={configurations}
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
