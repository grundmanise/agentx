import { useNavigate } from "react-router";

import type { Configuration } from "#/cli/events.gen.ts";
import { Button } from "#/components/ui/button.tsx";
import { forksOf } from "#/screens/skills/projection.ts";
import type { Group, SkillRow } from "#/screens/skills/projection.ts";
import { SkillRowItem } from "#/screens/skills/skill-row.tsx";

const GroupHeader = ({
  label,
  count,
  first,
}: {
  label: string;
  count: number;
  first: boolean;
}) => (
  <div
    className={
      first
        ? "pt-group-first-top px-group pb-group-bottom gap-group flex items-baseline"
        : "pt-group-top px-group pb-group-bottom gap-group flex items-baseline"
    }
  >
    <span className="type-caption text-group-label">{label}</span>
    <span className="type-technical-sm text-group-count">{count}</span>
  </div>
);

/**
 * The left column of the Skills screen: title and count, the Add button, and the rows in their
 * groups. Forks under a new name are nested under their original.
 */
export const SkillList = ({
  rows,
  groups,
  configurations,
  selectedKey,
  onSelect,
}: {
  /** Every row. */
  rows: SkillRow[];
  /** The rows, grouped. */
  groups: Group[];
  configurations: Configuration[];
  selectedKey: string | undefined;
  onSelect: (row: SkillRow) => void;
}) => {
  const navigate = useNavigate();
  return (
    <section
      aria-label="Skills list"
      className="w-split-list bg-shell-content-bg border-shell-divider flex flex-none flex-col border-r"
    >
      <div className="pt-split-header-top pr-split-header-right pb-split-toolbar-bottom pl-split-header-left flex items-end justify-between gap-3">
        <div className="flex items-baseline gap-3">
          <h1 className="type-page-title text-text-primary m-0">Skills</h1>
          <span
            aria-label={`${rows.length} skills`}
            className="type-technical text-text-caption"
          >
            {rows.length}
          </span>
        </div>
        {/* Check updates and Create come with the changes that build them. */}
        <div className="flex items-center gap-1">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              void navigate("/add");
            }}
          >
            Add
          </Button>
        </div>
      </div>
      <nav
        aria-label="Skills"
        className="pr-split-list-right pb-split-list-bottom pl-split-list-left min-h-0 flex-1 overflow-auto pt-0"
      >
        {groups.length === 0 ? (
          <p className="type-secondary text-text-hint m-0 py-10 text-center">
            No skills match
          </p>
        ) : (
          groups.map((group, i) => (
            <section
              key={group.key}
              aria-label={`${group.label} (${group.rows.length})`}
            >
              <GroupHeader
                label={group.label}
                count={group.rows.length}
                first={i === 0}
              />
              <ul className="gap-rows m-0 flex list-none flex-col p-0">
                {group.rows.map((row) => {
                  const forks = forksOf(row, rows);
                  return (
                    <li key={row.key} className="gap-rows flex flex-col">
                      <SkillRowItem
                        row={row}
                        configurations={configurations}
                        selected={row.key === selectedKey}
                        onSelect={() => {
                          onSelect(row);
                        }}
                      />
                      {forks.length > 0 && (
                        <ul className="gap-rows m-0 flex list-none flex-col p-0 pl-4.5">
                          {forks.map((fork) => (
                            <li key={fork.key} className="flex">
                              <SkillRowItem
                                row={fork}
                                configurations={configurations}
                                selected={fork.key === selectedKey}
                                nested
                                onSelect={() => {
                                  onSelect(fork);
                                }}
                              />
                            </li>
                          ))}
                        </ul>
                      )}
                    </li>
                  );
                })}
              </ul>
            </section>
          ))
        )}
      </nav>
    </section>
  );
};
