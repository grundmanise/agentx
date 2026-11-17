import { defaultRangeExtractor, useVirtualizer } from "@tanstack/react-virtual";
import type { Range } from "@tanstack/react-virtual";
import {
  Fragment,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import type { ReactNode } from "react";

import type { Configuration } from "#/cli/events.gen.ts";
import { AgentLogo, LogoStack } from "#/components/agent-logo.tsx";
import { FilterChip, SortToggle } from "#/components/filter-controls.tsx";
import { RefreshIcon } from "#/components/icons/refresh.tsx";
import { SearchField } from "#/components/search-field.tsx";
import { SectionToggle } from "#/components/section-toggle.tsx";
import { Spinner } from "#/components/spinner.tsx";
import { SplitButton } from "#/components/split-button.tsx";
import { Button } from "#/components/ui/button.tsx";
import { DropdownMenuItem } from "#/components/ui/dropdown-menu.tsx";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "#/components/ui/tooltip.tsx";
import { AddSkillDialog } from "#/screens/skills/add-skill-dialog.tsx";
import { AdoptDialog } from "#/screens/skills/adopt-dialog.tsx";
import { toolsToAdopt, useAdoptRun } from "#/screens/skills/adopt.ts";
import type { ToolArg } from "#/screens/skills/adopt.ts";
import { CreateSkillDialog } from "#/screens/skills/create-dialog.tsx";
import { FilterPopover } from "#/screens/skills/filter-popover.tsx";
import {
  forksOf,
  GROUP_DISK,
  GROUP_PLUGINS,
  nextOrder,
  UNIVERSAL,
} from "#/screens/skills/projection.ts";
import type {
  Filters,
  Group,
  SkillRow,
  SortOrder,
  Subgroup,
} from "#/screens/skills/projection.ts";
import {
  isRail,
  railHeight,
  sizeRails,
  useRailHeader,
} from "#/screens/skills/section-rails.ts";
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
 * label, which its tooltip shows. While the check runs it is pending (DESIGN.md §6): a `Spinner`
 * takes the refresh icon's place until the check ends.
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
          {running ? <Spinner /> : <RefreshIcon />}
        </Button>
      </TooltipTrigger>
      <TooltipContent>Check updates</TooltipContent>
    </Tooltip>
  );
};

/**
 * A group's caption. A source's group shows the source's full URL in a tooltip, as its caption
 * gives only the short form.
 */
const GroupLabel = ({ label, source }: { label: string; source?: string }) => {
  const caption = (
    <span className="type-caption text-group-label">{label}</span>
  );
  if (source === undefined) {
    return caption;
  }
  return (
    <Tooltip>
      <TooltipTrigger asChild>{caption}</TooltipTrigger>
      <TooltipContent side="top" align="start">
        {source}
      </TooltipContent>
    </Tooltip>
  );
};

const GroupHeader = ({
  label,
  source,
  count,
  action,
}: {
  label: string;
  /** The full URL of a source's group, which the tooltip on its label shows. */
  source?: string;
  count: number;
  /** A control at the header's right end, such as the Unmanaged group's Adopt all. */
  action?: ReactNode;
}) => (
  <div className="pt-group-top px-group pb-group-bottom gap-group min-h-group-header flex items-center">
    <span className="gap-group flex min-w-0 flex-1 items-baseline">
      <GroupLabel label={label} source={source} />
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
 * Collapse all, at the right end of the PLUGINS group's header while a plugin's subgroup is open:
 * it collapses every one. While all are collapsed it reads Expand all and opens every one. A
 * plain button as tall as its text, so the header keeps `group.header-height` and the plugin's
 * header pinned under it shows whole.
 */
const ToggleAll = ({
  open,
  onToggleAll,
}: {
  /** Whether a plugin's subgroup is open. */
  open: boolean;
  onToggleAll: (open: boolean) => void;
}) => (
  <Button
    variant="plain"
    size="inline"
    onClick={() => {
      onToggleAll(!open);
    }}
  >
    <span className="type-button-sm">
      {open ? "Collapse all" : "Expand all"}
    </span>
  </Button>
);

/** The control at the right end of a group's header, if the group has one. */
const groupAction = (
  group: Group,
  expanded: ReadonlySet<string>,
  onAdoptAll: () => void,
  onToggleAll: (open: boolean) => void
) => {
  if (group.key === GROUP_DISK) {
    return <AdoptAll onOpen={onAdoptAll} />;
  }
  if (group.key === GROUP_PLUGINS) {
    const open = (group.subgroups ?? []).some((s) => expanded.has(s.key));
    return <ToggleAll open={open} onToggleAll={onToggleAll} />;
  }
  return null;
};

/**
 * One thing the list places at its own offset: the space above a group or a plugin, a group's
 * header, a plugin's header, or a row, with its place among the rows of its group or plugin.
 */
type Item =
  | {
      kind: "gap";
      key: string;
      group: Group;
      /** The space above a group (`group.spacing`) or above a plugin (`subgroup.spacing`). */
      above: "group" | "subgroup";
    }
  | { kind: "header"; key: string; group: Group }
  | {
      kind: "subheader";
      key: string;
      group: Group;
      subgroup: Subgroup;
      open: boolean;
    }
  | {
      kind: "row";
      key: string;
      group: Group;
      subgroup?: Subgroup;
      row: SkillRow;
      at: number;
      size: number;
    };

/** The rows of a group, or of one plugin in it, as items. */
const rowItems = (group: Group, rows: SkillRow[], subgroup?: Subgroup) =>
  rows.map((row, at): Item => ({
    at,
    group,
    key: `row:${row.key}`,
    kind: "row",
    row,
    size: rows.length,
    ...(subgroup === undefined ? {} : { subgroup }),
  }));

/**
 * The headers and rows in the order the list shows them, with the space above each group and
 * plugin but the first; a collapsed plugin shows its header only.
 */
const itemsOf = (groups: Group[], expanded: ReadonlySet<string>): Item[] =>
  groups.flatMap((group, i): Item[] => [
    ...(i === 0
      ? []
      : [
          {
            above: "group",
            group,
            key: `gap:group:${group.key}`,
            kind: "gap",
          } satisfies Item,
        ]),
    { group, key: `group:${group.key}`, kind: "header" },
    ...(group.subgroups === undefined
      ? rowItems(group, group.rows)
      : group.subgroups.flatMap((subgroup, at): Item[] => {
          const open = expanded.has(subgroup.key);
          return [
            ...(at === 0
              ? []
              : [
                  {
                    above: "subgroup",
                    group,
                    key: `gap:subgroup:${subgroup.key}`,
                    kind: "gap",
                  } satisfies Item,
                ]),
            {
              group,
              key: `subgroup:${subgroup.key}`,
              kind: "subheader",
              open,
              subgroup,
            },
            ...(open ? rowItems(group, subgroup.rows, subgroup) : []),
          ];
        })),
  ]);

/**
 * The headers each item is under, by their index: its group's header, then in PLUGINS its
 * plugin's header. A header is under itself; the space above a group or plugin is under none.
 */
const headersOf = (items: Item[]): number[][] => {
  let header = -1;
  let subheader = -1;
  return items.map((item, index) => {
    if (item.kind === "gap") {
      return [];
    }
    if (item.kind === "header") {
      header = index;
      subheader = -1;
      return [index];
    }
    if (item.kind === "subheader") {
      subheader = index;
      return [header, index];
    }
    return item.subgroup === undefined ? [header] : [header, subheader];
  });
};

/** Each header's section, by the header's index: the index of the last item under it. */
const sectionsOf = (headers: number[][]) => {
  const sections = new Map<number, number>();
  for (const [index, under] of headers.entries()) {
    for (const header of under) {
      sections.set(header, index);
    }
  }
  return sections;
};

/**
 * The height the list assumes for an item it has not drawn yet, in px, close to what the tokens
 * give: a group's header, and a row with `row.spacing` above it, with or without its description
 * line. Each item takes its measured height once it is drawn.
 */
const estimatedHeight = (item: Item | undefined) => {
  if (item?.kind === "gap") {
    return item.above === "group" ? 16 : 8;
  }
  if (item?.kind === "header") {
    return 30;
  }
  if (item?.kind === "subheader") {
    return 28;
  }
  return item?.row.description === "" ? 44 : 66;
};

/**
 * The height a drawn item takes in the list: the height the browser lays it out at, up to the
 * next whole pixel. Every item then starts on a whole pixel, where the list can scroll to, and
 * none overlaps the next.
 */
const measuredHeight = (element: Element, entry?: ResizeObserverEntry) =>
  isRail(element)
    ? railHeight(element)
    : Math.ceil(
        entry?.borderBoxSize[0]?.blockSize ??
          element.getBoundingClientRect().height
      );

/** How many items the list keeps drawn beyond each edge of what shows. */
const OVERSCAN = 8;

/** A drawn space above a group or plugin: the list sets its offset from the top. */
const PLACED_GAP_GROUP = "pt-group-spacing absolute top-0 left-0 w-full";
const PLACED_GAP_SUBGROUP = "pt-subgroup-top absolute top-0 left-0 w-full";
/**
 * A group's rail: the list sets its offset from the top. It keeps no height; its span covers the
 * group, lets the pointer through to the rows under it, and holds the group's header, which pins
 * at the top of the list while the span shows. Its layer is above a plugin's rail, so a plugin's
 * header slides under the group's.
 */
const RAIL_GROUP = "absolute top-0 left-0 z-10 h-0 w-full";
/** A plugin's rail, whose span covers the plugin and holds its header, pinned under the group's. */
const RAIL_SUBGROUP = "absolute top-0 left-0 z-5 h-0 w-full";
/** The span of a rail: the list sets its height to cover the section. */
const RAIL_SPAN = "pointer-events-none absolute top-0 left-0 w-full";
/** A header in its rail: the list's background behind it, and the pointer on it. */
const PINNED_GROUP = "bg-shell-content-bg pointer-events-auto sticky top-0";
const PINNED_SUBGROUP =
  "bg-shell-content-bg top-group-header pointer-events-auto sticky flex";
const PLACED_ROW = "absolute top-0 left-0 flex w-full";
/** A row after the first of its group, `row.spacing` below the row before it. */
const PLACED_ROW_AFTER = "pt-rows absolute top-0 left-0 flex w-full";
/** A row of a plugin's subgroup, indented so its glyph sits under the plugin's name. */
const PLACED_SUBROW = "pl-subgroup-indent absolute top-0 left-0 flex w-full";
const PLACED_SUBROW_AFTER =
  "pt-rows pl-subgroup-indent absolute top-0 left-0 flex w-full";

/** Where a row is placed: after the first of its block, `row.spacing` lower; in a subgroup, indented. */
const placedRow = (item: { at: number; subgroup?: Subgroup }) => {
  if (item.subgroup === undefined) {
    return item.at === 0 ? PLACED_ROW : PLACED_ROW_AFTER;
  }
  return item.at === 0 ? PLACED_SUBROW : PLACED_SUBROW_AFTER;
};

const NO_FORKS: SkillRow[] = [];

/**
 * The forks made under a new name, by the name of the skill each was forked from and in name
 * order, as `forksOf` gives them for one skill: read once for the list, not once per row.
 */
const forksByOriginal = (rows: SkillRow[]) => {
  const forks = new Map<string, SkillRow[]>();
  for (const row of rows) {
    if (row.forkedFrom !== undefined && !forks.has(row.forkedFrom)) {
      forks.set(row.forkedFrom, forksOf({ name: row.forkedFrom }, rows));
    }
  }
  return forks;
};

/** The drawn part of a group's rows, or of one plugin's: its header when that is drawn, and its drawn rows. */
interface DrawnPart {
  subgroup?: Subgroup;
  subheader?: { index: number; item: Item & { kind: "subheader" } };
  rows: { index: number; item: Item & { kind: "row" } }[];
}

/** The drawn part of one group: its header when that is drawn, and its drawn parts. */
interface DrawnGroup {
  group: Group;
  header?: { index: number; item: Item & { kind: "header" } };
  /** The drawn spaces above the group and above its plugins. */
  gaps: { index: number; item: Item & { kind: "gap" } }[];
  parts: DrawnPart[];
}

/** The drawn items by group and plugin, in order. */
const drawnGroups = (items: Item[], indexes: number[]): DrawnGroup[] => {
  const drawn: DrawnGroup[] = [];
  for (const index of indexes) {
    const item = items[index];
    if (item === undefined) {
      continue;
    }
    let last = drawn.at(-1);
    if (last?.group !== item.group) {
      last = { gaps: [], group: item.group, parts: [] };
      drawn.push(last);
    }
    if (item.kind === "header") {
      last.header = { index, item };
      continue;
    }
    if (item.kind === "gap") {
      last.gaps.push({ index, item });
      continue;
    }
    let part = last.parts.at(-1);
    if (part === undefined || part.subgroup !== item.subgroup) {
      part =
        item.subgroup === undefined
          ? { rows: [] }
          : { rows: [], subgroup: item.subgroup };
      last.parts.push(part);
    }
    if (item.kind === "subheader") {
      part.subheader = { index, item };
    } else {
      part.rows.push({ index, item });
    }
  }
  return drawn;
};

/**
 * The rows in their groups, each group under its header. Only the headers and rows near the part
 * of the list that shows are in the document, so the list stays quick with hundreds of skills:
 * each group's label carries its full count, and each row its place in its group. The selected
 * row comes into view when the selection changes.
 */
const SkillGroups = ({
  rows,
  groups,
  configurations,
  selectedKey,
  onSelect,
  onAdoptAll,
  expanded,
  onToggle,
  onToggleAll,
}: {
  /** Every row, before the filters. */
  rows: SkillRow[];
  /** The rows that pass the filters, grouped. */
  groups: Group[];
  configurations: Configuration[];
  selectedKey: string | undefined;
  onSelect: (row: SkillRow) => void;
  /** Opens the adopt dialog for every tool with skills to adopt. */
  onAdoptAll: () => void;
  /** The keys of the open plugin subgroups; every other one is collapsed. */
  expanded: ReadonlySet<string>;
  /** Collapses or expands a plugin's subgroup, by its key. */
  onToggle: (key: string) => void;
  /** Expands every plugin's subgroup, or collapses every one. */
  onToggleAll: (open: boolean) => void;
}) => {
  // What the rows' banners read besides the row, for their alert triangles.
  const sources = useAppStore((s) => s.sources);
  const conflicts = useAppStore((s) => s.conflicts);
  const facts = useMemo(() => ({ conflicts, sources }), [conflicts, sources]);
  const forks = useMemo(() => forksByOriginal(rows), [rows]);
  // The same function on every render, which calls the latest `onSelect`.
  const latestSelect = useRef(onSelect);
  useEffect(() => {
    latestSelect.current = onSelect;
  }, [onSelect]);
  const select = useCallback((row: SkillRow) => {
    latestSelect.current(row);
  }, []);
  const scroller = useRef<HTMLElement>(null);
  const items = useMemo(() => itemsOf(groups, expanded), [groups, expanded]);
  // Where the selected row is among the items, or -1 when the filters leave it out.
  const selectedIndex = useMemo(
    () =>
      items.findIndex(
        (item) => item.kind === "row" && item.row.key === selectedKey
      ),
    [items, selectedKey]
  );
  // The headers each item is under, and the end of each header's section.
  const headers = useMemo(() => headersOf(items), [items]);
  const sections = useMemo(() => sectionsOf(headers), [headers]);
  // The height of the headers pinned above the selected row, which it comes into view below.
  const [pinnedHeight, setPinnedHeight] = useState(0);
  // The virtualizer's functions return new values while they stay the same functions, which
  // React Compiler cannot memoize. The app is built without it, and this component reads them
  // on every render and passes none of them to a memoized component.
  // oxlint-disable-next-line react/incompatible-library
  const virtualizer = useVirtualizer({
    count: items.length,
    // The library sets each drawn item's offset and the list's height on the elements itself.
    directDomUpdates: true,
    directDomUpdatesMode: "position",
    estimateSize: useCallback(
      (index: number) => estimatedHeight(items[index]),
      [items]
    ),
    getItemKey: useCallback(
      (index: number) => items[index]?.key ?? index,
      [items]
    ),
    getScrollElement: () => scroller.current,
    measureElement: measuredHeight,
    // An item measured or drawn can change where a section ends.
    onChange: (instance) => {
      sizeRails(instance, sections);
    },
    overscan: OVERSCAN,
    // The headers of what shows stay drawn, so they pin; and the selected row stays drawn
    // wherever the list is scrolled to, so it keeps the focus a click gave it.
    rangeExtractor: useCallback(
      (range: Range) => {
        const shown = defaultRangeExtractor(range);
        const indexes = new Set(shown);
        for (const index of shown) {
          for (const header of headers[index] ?? []) {
            indexes.add(header);
          }
        }
        if (selectedIndex !== -1) {
          indexes.add(selectedIndex);
        }
        const drawn = [...indexes];
        drawn.sort((a, b) => a - b);
        return drawn;
      },
      [headers, selectedIndex]
    ),
    scrollPaddingStart: pinnedHeight,
  });
  const railHeader = useRailHeader(virtualizer);
  useLayoutEffect(() => {
    sizeRails(virtualizer, sections);
  });
  useLayoutEffect(() => {
    const pinned = (headers[selectedIndex] ?? [])
      .filter((header) => header !== selectedIndex)
      .reduce(
        (sum, header) =>
          sum + (virtualizer.measurementsCache[header]?.size ?? 0),
        0
      );
    setPinnedHeight(pinned);
  }, [headers, selectedIndex, virtualizer]);

  // A plugin's header that is pinned stays at the top when a click collapses or expands it: the
  // list scrolls back to where the header pins.
  const toggle = (index: number, key: string) => {
    const header = virtualizer.measurementsCache[index];
    const group = virtualizer.measurementsCache[headers[index]?.[0] ?? -1];
    const offset = virtualizer.scrollOffset ?? 0;
    if (header !== undefined && group !== undefined) {
      const place = header.start - group.size;
      if (offset > place) {
        virtualizer.scrollToOffset(place);
      }
    }
    onToggle(key);
  };

  // The selected row comes into view when the selection changes (a key press, a click, or a route
  // another screen opened) and when the filters list it again. A list that changes around the
  // same selected row stays where it is.
  const latestIndex = useRef(selectedIndex);
  useEffect(() => {
    latestIndex.current = selectedIndex;
  }, [selectedIndex]);
  const listed = selectedIndex !== -1;
  useEffect(() => {
    if (listed) {
      virtualizer.scrollToIndex(latestIndex.current, { align: "auto" });
    }
  }, [selectedKey, listed, virtualizer]);

  const drawn = drawnGroups(
    items,
    virtualizer.getVirtualItems().map((v) => v.index)
  );
  return (
    <nav
      ref={scroller}
      aria-label="Skills"
      className="pr-split-list-right pb-split-list-bottom pl-split-list-left min-h-0 flex-1 overflow-auto pt-0"
    >
      {groups.length === 0 ? (
        <p className="type-secondary text-text-hint m-0 py-10 text-center">
          No skills match
        </p>
      ) : (
        <div ref={virtualizer.containerRef} className="relative">
          {drawn.map(({ group, header, gaps, parts }) => (
            <section
              key={group.key}
              aria-label={`${group.label} (${group.rows.length})`}
              className="contents"
            >
              {gaps.map(({ index, item }) => (
                <div
                  key={item.key}
                  ref={virtualizer.measureElement}
                  data-index={index}
                  aria-hidden="true"
                  className={
                    item.above === "group"
                      ? PLACED_GAP_GROUP
                      : PLACED_GAP_SUBGROUP
                  }
                />
              ))}
              {header !== undefined && (
                <div
                  ref={virtualizer.measureElement}
                  data-index={header.index}
                  data-rail=""
                  className={RAIL_GROUP}
                >
                  <div className={RAIL_SPAN}>
                    <div ref={railHeader} className={PINNED_GROUP}>
                      <GroupHeader
                        label={group.label}
                        source={group.source}
                        count={group.rows.length}
                        action={groupAction(
                          group,
                          expanded,
                          onAdoptAll,
                          onToggleAll
                        )}
                      />
                    </div>
                  </div>
                </div>
              )}
              {parts.map(({ subgroup, subheader, rows: partRows }) => {
                const list = (
                  <ul className="contents">
                    {partRows.map(({ index, item }) => (
                      <li
                        key={item.key}
                        ref={virtualizer.measureElement}
                        data-index={index}
                        aria-posinset={item.at + 1}
                        aria-setsize={item.size}
                        className={placedRow(item)}
                      >
                        <SkillRowItem
                          row={item.row}
                          forks={forks.get(item.row.name) ?? NO_FORKS}
                          configurations={configurations}
                          facts={facts}
                          selected={item.row.key === selectedKey}
                          onSelect={select}
                        />
                      </li>
                    ))}
                  </ul>
                );
                if (subgroup === undefined) {
                  return <Fragment key="rows">{list}</Fragment>;
                }
                return (
                  <section
                    key={subgroup.key}
                    aria-label={`${subgroup.label} (${subgroup.rows.length})`}
                    className="contents"
                  >
                    {subheader !== undefined && (
                      <div
                        ref={virtualizer.measureElement}
                        data-index={subheader.index}
                        data-rail=""
                        className={RAIL_SUBGROUP}
                      >
                        <div className={RAIL_SPAN}>
                          <div ref={railHeader} className={PINNED_SUBGROUP}>
                            <SectionToggle
                              label={subgroup.label}
                              count={subgroup.rows.length}
                              open={subheader.item.open}
                              onToggle={() => {
                                toggle(subheader.index, subgroup.key);
                              }}
                            />
                          </div>
                        </div>
                      </div>
                    )}
                    {list}
                  </section>
                );
              })}
            </section>
          ))}
        </div>
      )}
    </nav>
  );
};

/**
 * Add, a button group that opens the Add skill dialog, and whose menu opens the Create skill
 * dialog. The dialogs' open state lives here, so opening or closing one draws this group again
 * and no row of the list.
 */
const AddSkills = () => {
  const [adding, setAdding] = useState(false);
  const [creating, setCreating] = useState(false);
  return (
    <>
      <SplitButton
        size="sm"
        onClick={() => {
          setAdding(true);
        }}
        menuLabel="More add actions"
        menu={
          <DropdownMenuItem
            onSelect={() => {
              setCreating(true);
            }}
          >
            <span className="type-body text-text-primary">Create skill</span>
          </DropdownMenuItem>
        }
      >
        Add
      </SplitButton>
      <AddSkillDialog open={adding} onOpenChange={setAdding} />
      <CreateSkillDialog open={creating} onOpenChange={setCreating} />
    </>
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
  expanded,
  onToggle,
  onToggleAll,
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
  /** The keys of the open plugin subgroups; every other one is collapsed. */
  expanded: ReadonlySet<string>;
  /** Collapses or expands a plugin's subgroup, by its key. */
  onToggle: (key: string) => void;
  /** Expands every plugin's subgroup, or collapses every one. */
  onToggleAll: (open: boolean) => void;
}) => {
  const [adopting, setAdopting] = useState(false);
  // The tools the open adopt dialog covers: every tool with skills to adopt when it opened.
  const adoption = useAppStore((s) => s.adoption);
  const [adoptTools, setAdoptTools] = useState<ToolArg[]>([]);
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
          <AddSkills />
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
            order={order}
            onToggle={() => {
              onOrder(nextOrder(order));
            }}
          />
        </div>
      </div>
      <SkillGroups
        rows={rows}
        groups={groups}
        configurations={configurations}
        selectedKey={selectedKey}
        onSelect={onSelect}
        expanded={expanded}
        onToggle={onToggle}
        onToggleAll={onToggleAll}
        onAdoptAll={() => {
          setAdoptTools(toolsToAdopt(adoption).map((t) => t.arg));
          setAdopting(true);
        }}
      />
      <AdoptDialog
        tools={adoptTools}
        open={adopting}
        onOpenChange={setAdopting}
      />
    </section>
  );
};
