import { useState } from "react";
import type { ReactNode } from "react";
import { useNavigate } from "react-router";

import type { Configuration } from "#/cli/events.gen.ts";
import { AgentLogo, LogoStack } from "#/components/agent-logo.tsx";
import { FilterChip, SortToggle } from "#/components/filter-controls.tsx";
import { RefreshIcon } from "#/components/icons/refresh.tsx";
import { SearchField } from "#/components/search-field.tsx";
import { SplitButton } from "#/components/split-button.tsx";
import { Button } from "#/components/ui/button.tsx";
import { DropdownMenuItem } from "#/components/ui/dropdown-menu.tsx";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "#/components/ui/tooltip.tsx";
import { AdoptDialog } from "#/screens/skills/adopt-dialog.tsx";
import { toolsToAdopt, useAdoptRun } from "#/screens/skills/adopt.ts";
import type { ToolArg } from "#/screens/skills/adopt.ts";
import { CreateSkillDialog } from "#/screens/skills/create-dialog.tsx";
import { FilterPopover } from "#/screens/skills/filter-popover.tsx";
import { forksOf, GROUP_DISK, UNIVERSAL } from "#/screens/skills/projection.ts";
import type {
  Filters,
  Group,
  SkillRow,
  SortOrder,
} from "#/screens/skills/projection.ts";
import { SkillRowItem } from "#/screens/skills/skill-row.tsx";
import { checkUpdates, useCheckingUpdates } from "#/screens/skills/updates.ts";
import { useAppStore } from "#/store/store.ts";

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

/**
 * The ghost Check updates, an icon button as square as Add's three-dots trigger and named by its
 * label, which its tooltip shows; its icon turns once while the check runs (DESIGN.md §8).
 */
const CheckUpdates = () => {
  const running = useCheckingUpdates();
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Check updates"
          aria-busy={running}
          onClick={() => {
            void checkUpdates();
          }}
        >
          <span className={running ? "animate-turn-once flex" : "flex"}>
            <RefreshIcon />
          </span>
        </Button>
      </TooltipTrigger>
      <TooltipContent>Check updates</TooltipContent>
    </Tooltip>
  );
};

const GroupHeader = ({
  label,
  count,
  first,
  action,
}: {
  label: string;
  count: number;
  first: boolean;
  /** A control at the header's right end, such as the Unmanaged group's Adopt all. */
  action?: ReactNode;
}) => (
  <div
    className={
      first
        ? "pt-group-first-top px-group pb-group-bottom gap-group flex items-center"
        : "pt-group-top px-group pb-group-bottom gap-group flex items-center"
    }
  >
    <span className="gap-group flex min-w-0 flex-1 items-baseline">
      <span className="type-caption text-group-label">{label}</span>
      <span className="type-technical-sm text-group-count">{count}</span>
    </span>
    {action}
  </div>
);

/**
 * Adopt all, at the right end of the Unmanaged group's header while another tool installed skills
 * agentx can adopt: it opens the adopt dialog for every such tool.
 */
const AdoptAll = ({ onOpen }: { onOpen: () => void }) => {
  const adoption = useAppStore((s) => s.adoption);
  const busy = useAdoptRun((s) => s.run !== undefined);
  if (toolsToAdopt(adoption).length === 0) {
    return null;
  }
  return (
    <Button variant="primary" size="sm" disabled={busy} onClick={onOpen}>
      Adopt all
    </Button>
  );
};

/**
 * The left column of the Skills screen: title and count, Check updates, Add and Create, the search field, the
 * filter row, and the rows in their groups. A fork made under a new name is one row, in YOUR
 * SKILLS; its original carries a marker for it.
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
  const [creating, setCreating] = useState(false);
  const [adopting, setAdopting] = useState(false);
  // The tools the open adopt dialog covers: every tool with skills to adopt when it opened.
  const adoption = useAppStore((s) => s.adoption);
  const [adoptTools, setAdoptTools] = useState<ToolArg[]>([]);
  // What the rows' banners read besides the row, for their alert triangles.
  const sources = useAppStore((s) => s.sources);
  const conflicts = useAppStore((s) => s.conflicts);
  const facts = { conflicts, sources };
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
        <div className="gap-button flex items-center">
          <CheckUpdates />
          <SplitButton
            size="sm"
            onClick={() => {
              void navigate("/add");
            }}
            menuLabel="More add actions"
            menu={
              <DropdownMenuItem
                onSelect={() => {
                  setCreating(true);
                }}
              >
                <span className="type-body text-text-primary">
                  Create skill
                </span>
              </DropdownMenuItem>
            }
          >
            Add
          </SplitButton>
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
                action={
                  group.key === GROUP_DISK ? (
                    <AdoptAll
                      onOpen={() => {
                        setAdoptTools(toolsToAdopt(adoption).map((t) => t.arg));
                        setAdopting(true);
                      }}
                    />
                  ) : undefined
                }
              />
              <ul className="gap-rows m-0 flex list-none flex-col p-0">
                {group.rows.map((row) => (
                  <li key={row.key} className="flex">
                    <SkillRowItem
                      row={row}
                      forks={forksOf(row, rows)}
                      configurations={configurations}
                      facts={facts}
                      selected={row.key === selectedKey}
                      onSelect={() => {
                        onSelect(row);
                      }}
                    />
                  </li>
                ))}
              </ul>
            </section>
          ))
        )}
      </nav>
      <CreateSkillDialog open={creating} onOpenChange={setCreating} />
      <AdoptDialog
        tools={adoptTools}
        open={adopting}
        onOpenChange={setAdopting}
      />
    </section>
  );
};
