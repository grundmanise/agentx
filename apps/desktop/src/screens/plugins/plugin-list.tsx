import type { Configuration } from "#/cli/events.gen.ts";
import { AgentLogo } from "#/components/agent-logo.tsx";
import { SortToggle } from "#/components/filter-controls.tsx";
import { ListRow } from "#/components/list-row.tsx";
import { Pill } from "#/components/pill.tsx";
import { SearchField } from "#/components/search-field.tsx";
import { plural } from "#/lib/plural.ts";
import { PluginFilterPopover } from "#/screens/plugins/filter-popover.tsx";
import { PluginTile } from "#/screens/plugins/plugin-tile.tsx";
import type {
  PluginFilters,
  PluginGroup,
  PluginOrder,
  PluginRow,
} from "#/screens/plugins/projection.ts";
import { AgentFilterChip } from "#/screens/skills/skill-list.tsx";

/** What a plugin brings, for its row: "5 skills · 2 servers". */
export const contentsOf = (row: PluginRow) =>
  [
    row.skills.length > 0 && plural(row.skills.length, "skill"),
    row.servers.length > 0 && plural(row.servers.length, "server"),
    row.hooks.length > 0 && plural(row.hooks.length, "hook"),
  ]
    .filter((part) => part !== false)
    .join(" · ");

/** The order the sort toggle steps through: default, then A to Z, then Z to A. */
const nextOrder = (order: PluginOrder): PluginOrder => {
  if (order === "default") {
    return "asc";
  }
  return order === "asc" ? "desc" : "default";
};

/** One plugin's row: its tile, name, Disabled pill and version, then what it brings. */
const PluginRowView = ({
  row,
  selected,
  onSelect,
}: {
  row: PluginRow;
  selected: boolean;
  onSelect: () => void;
}) => {
  const contents = contentsOf(row);
  return (
    <ListRow
      label={row.name}
      description={[row.disabled ? "Disabled" : "", row.version, contents]
        .filter((part) => part !== "")
        .join(". ")}
      selected={selected}
      onSelect={onSelect}
    >
      <PluginTile name={row.name} icon={row.icon} size="row" />
      <span
        className={
          row.disabled
            ? "gap-row-line opacity-row-off flex min-w-0 flex-1 flex-col"
            : "gap-row-line flex min-w-0 flex-1 flex-col"
        }
      >
        <span className="gap-row-inline flex h-5 items-center">
          <span className="type-row-name text-row-name truncate">
            {row.name}
          </span>
          {row.disabled && <Pill tone="disabled">Disabled</Pill>}
          <span className="flex-1" />
          <span className="type-technical-sm text-text-caption whitespace-nowrap">
            {row.version}
          </span>
        </span>
        {contents !== "" && (
          <span className="type-row-desc text-row-desc truncate">
            {contents}
          </span>
        )}
      </span>
    </ListRow>
  );
};

/** An agent configuration's header: its logo, its name and how many plugins it holds. */
const GroupHeader = ({ group }: { group: PluginGroup }) => (
  <h2 className="pt-group-top px-group pb-group-bottom gap-group min-h-group-header bg-shell-content-bg sticky top-0 z-10 m-0 flex items-center">
    <AgentLogo client={group.configuration} size={16} decorative />
    <span className="type-caption text-group-label truncate">
      {group.configuration.name}
    </span>
    <span className="type-technical-sm text-group-count">
      {group.rows.length}
    </span>
  </h2>
);

/**
 * The Plugins list column: the title and count, Search, the Filter button with its agents chip and
 * the sort toggle, then the plugins grouped by agent configuration.
 */
export const PluginList = ({
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
  rows: PluginRow[];
  /** The rows that pass the filters, grouped. */
  groups: PluginGroup[];
  /** The agent configurations that hold a plugin. */
  configurations: Configuration[];
  filters: PluginFilters;
  onFilters: (filters: PluginFilters) => void;
  order: PluginOrder;
  onOrder: (order: PluginOrder) => void;
  selectedKey: string | undefined;
  onSelect: (row: PluginRow) => void;
}) => (
  <section
    aria-label="Plugins list"
    className="w-split-list bg-shell-content-bg border-shell-divider flex flex-none flex-col border-r"
  >
    <div className="pt-split-header-top pr-split-header-right pl-split-header-left flex items-end gap-3 pb-0">
      <div className="flex items-baseline gap-3">
        <h1 className="type-page-title text-text-primary m-0">Plugins</h1>
        <span
          aria-label={plural(rows.length, "plugin")}
          className="type-technical text-text-caption"
        >
          {rows.length}
        </span>
      </div>
    </div>
    <div className="pt-split-toolbar pr-split-toolbar pb-split-toolbar-bottom pl-split-toolbar-left gap-split-toolbar flex flex-col">
      <SearchField
        label="Filter plugins by name"
        placeholder="Search"
        value={filters.name}
        onChange={(name) => {
          onFilters({ ...filters, name });
        }}
      />
      <div className="gap-split-filter flex min-w-0 items-center">
        <PluginFilterPopover
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
          order={order}
          onToggle={() => {
            onOrder(nextOrder(order));
          }}
        />
      </div>
    </div>
    <div className="pr-split-list-right pb-split-list-bottom pl-split-list-left min-h-0 flex-1 overflow-auto pt-0">
      {groups.length === 0 ? (
        <p className="type-secondary text-text-hint m-0 py-10 text-center">
          {rows.length === 0
            ? "No plugins found in your agents."
            : "No plugins match"}
        </p>
      ) : (
        groups.map((group, i) => (
          <section
            key={group.configuration.id}
            aria-label={group.configuration.name}
            className={
              i === 0 ? "flex flex-col" : "pt-group-spacing flex flex-col"
            }
          >
            <GroupHeader group={group} />
            <ul className="gap-rows m-0 flex list-none flex-col p-0">
              {group.rows.map((row) => (
                <li key={row.key} className="flex">
                  <PluginRowView
                    row={row}
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
    </div>
  </section>
);
