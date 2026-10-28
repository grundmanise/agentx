import { useNavigate } from "react-router";

import type { Configuration } from "#/cli/events.gen.ts";
import { AgentLogo, LogoStack } from "#/components/agent-logo.tsx";
import { FilterChip, SortToggle } from "#/components/filter-controls.tsx";
import { SearchField } from "#/components/search-field.tsx";
import { Button } from "#/components/ui/button.tsx";
import { FilterPopover } from "#/screens/skills/filter-popover.tsx";
import { forksOf, UNIVERSAL } from "#/screens/skills/projection.ts";
import type {
  Filters,
  Group,
  SkillRow,
  SortOrder,
} from "#/screens/skills/projection.ts";
import { SkillRowItem } from "#/screens/skills/skill-row.tsx";

/** What the agent chip says: the one agent chosen, or how many. */
const agentChipLabel = (agents: string[], chosen: Configuration[]): string => {
  const [one] = agents;
  if (agents.length !== 1 || one === undefined) {
    return `${agents.length} agents`;
  }
  if (one === UNIVERSAL) {
    return "Universal";
  }
  return chosen[0]?.name ?? one;
};

/** The chip in the filter row that echoes the agent filter: one agent, or how many. */
const AgentFilterChip = ({
  agents,
  configurations,
  onClear,
}: {
  agents: string[];
  configurations: Configuration[];
  onClear: () => void;
}) => {
  const universal = configurations.filter((c) => c.reads_library);
  const chosen = agents
    .flatMap((id) =>
      id === UNIVERSAL ? universal : configurations.filter((c) => c.id === id)
    )
    .filter((c, i, all) => all.indexOf(c) === i);
  const [single] = chosen;
  const oneAgent = agents.length === 1 && agents[0] !== UNIVERSAL;
  return (
    <FilterChip clearLabel="Clear agents" onClear={onClear}>
      {oneAgent && single !== undefined ? (
        <AgentLogo client={single} size={16} />
      ) : (
        <LogoStack clients={chosen} size={16} />
      )}
      <span className="truncate">{agentChipLabel(agents, chosen)}</span>
    </FilterChip>
  );
};

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
 * The left column of the Skills screen: title and count, the Add button, the search field, the
 * filter row, and the rows in their groups. Forks under a new name are nested under their original.
 */
export const SkillList = ({
  rows,
  groups,
  configurations,
  filters,
  onFilters,
  order,
  onOrder,
  selectedKey,
  onSelect,
}: {
  /** Every row, before the filters. */
  rows: SkillRow[];
  /** The rows that pass the filters, grouped. */
  groups: Group[];
  configurations: Configuration[];
  filters: Filters;
  onFilters: (filters: Filters) => void;
  order: SortOrder;
  onOrder: (order: SortOrder) => void;
  selectedKey: string | undefined;
  onSelect: (row: SkillRow) => void;
}) => {
  const navigate = useNavigate();
  return (
    <section
      aria-label="Skills list"
      className="w-split-list bg-shell-content-bg border-shell-divider flex flex-none flex-col border-r"
    >
      <div className="pt-split-header-top pr-split-header-right pl-split-header-left flex items-end justify-between gap-3 pb-0">
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
      <div className="pt-split-toolbar pr-split-toolbar pb-split-toolbar-bottom pl-split-toolbar-left gap-split-toolbar flex flex-col">
        <SearchField
          label="Filter skills by name"
          placeholder="Filter"
          value={filters.name}
          onChange={(name) => {
            onFilters({ ...filters, name });
          }}
        />
        <div className="gap-split-filter flex min-w-0 items-center">
          <FilterPopover
            rows={rows}
            configurations={configurations}
            filters={filters}
            onChange={onFilters}
          />
          {filters.agents.length > 0 && (
            <AgentFilterChip
              agents={filters.agents}
              configurations={configurations}
              onClear={() => {
                onFilters({ ...filters, agents: [] });
              }}
            />
          )}
          <span className="flex-1" />
          <SortToggle
            descending={order === "desc"}
            onToggle={() => {
              onOrder(order === "asc" ? "desc" : "asc");
            }}
          />
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
