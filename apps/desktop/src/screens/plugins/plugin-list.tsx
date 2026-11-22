import { useRef } from "react";

import type { Configuration } from "#/cli/events.gen.ts";
import { AgentLogo } from "#/components/agent-logo.tsx";
import { ExpandToggle, SortToggle } from "#/components/filter-controls.tsx";
import { LIST_GUIDE } from "#/components/list-guide.ts";
import { ListRow } from "#/components/list-row.tsx";
import { Pill } from "#/components/pill.tsx";
import { SearchField } from "#/components/search-field.tsx";
import { SectionToggle } from "#/components/section-toggle.tsx";
import { useOnNewKey } from "#/lib/new-key.ts";
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

/** Each click of the sort toggle flips between A to Z and Z to A. */
const nextOrder = (order: PluginOrder): PluginOrder =>
  order === "asc" ? "desc" : "asc";

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

/** A plugin's row, inset under its group's caption, beside the group's guide line. */
const ROW = `pl-subgroup-indent relative flex ${LIST_GUIDE}`;
/** A row after the first of its group, `row.spacing` below the row before it. */
const ROW_AFTER = `pt-rows ${ROW}`;

/**
 * A plugin's place in its group. Once selected, such as from Also installed in, it comes into
 * view when the list has scrolled it out of sight, below its group's pinned header.
 */
const PluginItem = ({
  row,
  first,
  selected,
  onSelect,
}: {
  row: PluginRow;
  first: boolean;
  selected: boolean;
  onSelect: () => void;
}) => {
  const ref = useRef<HTMLLIElement>(null);
  useOnNewKey(selected ? row.key : undefined, () => {
    const item = ref.current;
    const list = item?.closest("[data-plugin-scroll]");
    const header = item?.closest("section")?.querySelector("h2");
    if (!item || !list || !header) {
      return;
    }
    const at = item.getBoundingClientRect();
    if (
      at.top < header.getBoundingClientRect().bottom ||
      at.bottom > list.getBoundingClientRect().bottom
    ) {
      item.scrollIntoView({ block: "center" });
    }
  });
  return (
    <li ref={ref} className={first ? ROW : ROW_AFTER}>
      <PluginRowView row={row} selected={selected} onSelect={onSelect} />
    </li>
  );
};

/**
 * The Plugins list column: the title and count, Search, the Filter button with its agents chip,
 * the expand toggle and the sort toggle, then the plugins grouped by agent configuration.
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
  collapsed,
  onToggleGroup,
  anyOpen,
  onToggleAll,
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
  /** The agent configurations whose groups are collapsed. */
  collapsed: ReadonlySet<string>;
  onToggleGroup: (configuration: string) => void;
  /** Whether any group the filters list is open. */
  anyOpen: boolean;
  /** Collapses every group while any is open, else opens every group. */
  onToggleAll: () => void;
}) => (
  <section
    aria-label="Plugins list"
    className="w-split-list bg-shell-content-bg border-shell-divider flex flex-none flex-col border-r"
  >
    <div className="pt-split-header-top px-split-header flex items-end gap-3 pb-0">
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
    <div className="pt-split-toolbar-top px-split-toolbar pb-split-toolbar-bottom gap-split-toolbar flex flex-col">
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
            onRemove={(agent) => {
              onFilters({
                ...filters,
                agents: filters.agents.filter((a) => a !== agent),
              });
            }}
          />
        )}
        <span className="flex-1" />
        <ExpandToggle
          open={anyOpen}
          disabled={groups.length === 0}
          onToggle={onToggleAll}
        />
        <SortToggle
          order={order}
          onToggle={() => {
            onOrder(nextOrder(order));
          }}
        />
      </div>
    </div>
    <div
      data-plugin-scroll=""
      className="pr-split-list-right pb-split-list-bottom pl-split-list-left min-h-0 flex-1 overflow-auto pt-0"
    >
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
            <h2 className="bg-shell-content-bg sticky top-0 z-10 m-0">
              <SectionToggle
                level="group"
                label={group.configuration.name}
                icon={
                  <AgentLogo
                    client={group.configuration}
                    size={16}
                    decorative
                  />
                }
                count={group.rows.length}
                open={!collapsed.has(group.configuration.id)}
                onToggle={() => {
                  onToggleGroup(group.configuration.id);
                }}
              />
            </h2>
            {!collapsed.has(group.configuration.id) && (
              <ul className="m-0 flex list-none flex-col p-0">
                {group.rows.map((row, at) => (
                  <PluginItem
                    key={row.key}
                    row={row}
                    first={at === 0}
                    selected={row.key === selectedKey}
                    onSelect={() => {
                      onSelect(row);
                    }}
                  />
                ))}
              </ul>
            )}
          </section>
        ))
      )}
    </div>
  </section>
);
