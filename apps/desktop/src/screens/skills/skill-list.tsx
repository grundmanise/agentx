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
import type { ReactNode, RefObject } from "react";

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
import { pinAt } from "#/screens/skills/pinned.ts";
import type { Pin, PinHeights } from "#/screens/skills/pinned.ts";
import {
  forksOf,
  GROUP_DISK,
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
  first,
  action,
}: {
  label: string;
  /** The full URL of a source's group, which the tooltip on its label shows. */
  source?: string;
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
const AdoptAll = ({
  onOpen,
  tabbable = true,
}: {
  onOpen: () => void;
  /** Whether the Tab key reaches it; false in the pinned copy of the header. */
  tabbable?: boolean;
}) => {
  const adoption = useAppStore((s) => s.adoption);
  const busy = useAdoptRun((s) => s.run !== undefined);
  if (toolsToAdopt(adoption).length === 0) {
    return null;
  }
  return (
    <Button
      variant="primary"
      size="sm"
      disabled={busy}
      onClick={onOpen}
      tabIndex={tabbable ? undefined : -1}
    >
      Adopt all
    </Button>
  );
};

/**
 * One thing the list places at its own offset: a group's header, a plugin's header, or a row,
 * with its place among the rows of its group or plugin.
 */
type Item =
  | { kind: "header"; key: string; group: Group; first: boolean }
  | {
      kind: "subheader";
      key: string;
      group: Group;
      subgroup: Subgroup;
      open: boolean;
      /** The first subgroup of its group, right under the group's header. */
      first: boolean;
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

/** The headers and rows in the order the list shows them; a collapsed plugin shows its header only. */
const itemsOf = (groups: Group[], collapsed: ReadonlySet<string>): Item[] =>
  groups.flatMap((group, i): Item[] => [
    { first: i === 0, group, key: `group:${group.key}`, kind: "header" },
    ...(group.subgroups === undefined
      ? rowItems(group, group.rows)
      : group.subgroups.flatMap((subgroup, at): Item[] => {
          const open = !collapsed.has(subgroup.key);
          return [
            {
              first: at === 0,
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
 * The height the list assumes for an item it has not drawn yet, in px, close to what the tokens
 * give: a group's header, and a row with `row.spacing` above it, with or without its description
 * line. Each item takes its measured height once it is drawn.
 */
const estimatedHeight = (item: Item | undefined) => {
  if (item?.kind === "header") {
    return item.first ? 30 : 46;
  }
  if (item?.kind === "subheader") {
    return item.first ? 28 : 36;
  }
  return item?.row.description === "" ? 44 : 66;
};

/** The heights the pinned headers take before the list measures them: a first header's. */
const ESTIMATED_PIN: PinHeights = { group: 30, subgroup: 28 };

/**
 * The heights of the pinned headers: the first group header's, and the first plugin header's in
 * its group, as the list measured them. The pinned headers are drawn as those are.
 */
const pinHeights = (
  items: Item[],
  spans: readonly ({ size: number } | undefined)[]
): PinHeights => {
  const subheader = items.findIndex(
    (item) => item.kind === "subheader" && item.first
  );
  return {
    group: spans[0]?.size ?? ESTIMATED_PIN.group,
    subgroup: spans[subheader]?.size ?? ESTIMATED_PIN.subgroup,
  };
};

/** The headers pinned at the top of the list, by their index among the items. */
interface Pinned {
  header: number;
  subheader?: number;
}

/** The headers a pin holds, by their index among the items. */
const pinnedOf = (pin: Pin | undefined): Pinned | undefined => {
  if (pin === undefined) {
    return undefined;
  }
  return pin.subheader === undefined
    ? { header: pin.header }
    : { header: pin.header, subheader: pin.subheader };
};

/**
 * The height a drawn item takes in the list: the height the browser lays it out at, up to the
 * next whole pixel. Every item then starts on a whole pixel, where the list can scroll to, and
 * none overlaps the next.
 */
const measuredHeight = (element: Element, entry?: ResizeObserverEntry) =>
  Math.ceil(
    entry?.borderBoxSize[0]?.blockSize ?? element.getBoundingClientRect().height
  );

/** How many items the list keeps drawn beyond each edge of what shows. */
const OVERSCAN = 8;

/** A drawn item: the list sets its offset from the top. */
const PLACED = "absolute top-0 left-0 w-full";
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
      last = { group: item.group, parts: [] };
      drawn.push(last);
    }
    if (item.kind === "header") {
      last.header = { index, item };
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
 * The headers pinned at the top of the list while it scrolls: the group's header of the part
 * that shows at the top, and in PLUGINS its plugin's header. Each is drawn as the first header of
 * its kind, so it covers the header it pins exactly; the next header pushes it up as it comes
 * under it. It is a copy for the eye and the pointer, hidden from assistive technology and out of
 * the tab order: the headers in the list are still there for both.
 */
const PinnedHeaders = ({
  group,
  subgroup,
  groupLayer,
  subgroupLayer,
  onAdoptAll,
  onToggle,
}: {
  group: Group;
  subgroup?: { subgroup: Subgroup; open: boolean };
  /** The layer the next group's header pushes up. */
  groupLayer: RefObject<HTMLDivElement | null>;
  /** The layer the next plugin's header pushes up, under the group's header. */
  subgroupLayer: RefObject<HTMLDivElement | null>;
  onAdoptAll: () => void;
  onToggle: () => void;
}) => (
  <div aria-hidden="true" className="sticky top-0 z-10 h-0">
    <div ref={groupLayer} className="flex flex-col">
      <div className="bg-shell-content-bg relative z-10">
        <GroupHeader
          label={group.label}
          source={group.source}
          count={group.rows.length}
          first
          action={
            group.key === GROUP_DISK ? (
              <AdoptAll onOpen={onAdoptAll} tabbable={false} />
            ) : undefined
          }
        />
      </div>
      {subgroup !== undefined && (
        <div ref={subgroupLayer} className="bg-shell-content-bg flex">
          <SectionToggle
            label={subgroup.subgroup.label}
            count={subgroup.subgroup.rows.length}
            open={subgroup.open}
            tabbable={false}
            onToggle={onToggle}
          />
        </div>
      )}
    </div>
  </div>
);

/**
 * The rows in their groups, each group under its header. Only the headers and rows near the part
 * of the list that shows are in the document, so the list stays quick with hundreds of skills:
 * each group's label carries its full count, and each row its place in its group. The selected
 * row comes into view when the selection changes, below the pinned headers. While the list
 * scrolls, the header of the group at its top, and in PLUGINS of the plugin, stays pinned there.
 */
const SkillGroups = ({
  rows,
  groups,
  configurations,
  selectedKey,
  onSelect,
  onAdoptAll,
  collapsed,
  onToggle,
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
  /** The keys of the collapsed plugin subgroups. */
  collapsed: ReadonlySet<string>;
  /** Collapses or expands a plugin's subgroup, by its key. */
  onToggle: (key: string) => void;
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
  const items = useMemo(() => itemsOf(groups, collapsed), [groups, collapsed]);
  // Where the selected row is among the items, or -1 when the filters leave it out.
  const selectedIndex = useMemo(
    () =>
      items.findIndex(
        (item) => item.kind === "row" && item.row.key === selectedKey
      ),
    [items, selectedKey]
  );
  // The headers pinned at the top, and the heights they take, which the selected row comes into
  // view below.
  const [pinned, setPinned] = useState<Pinned>();
  const [heights, setHeights] = useState<PinHeights>(ESTIMATED_PIN);
  const groupLayer = useRef<HTMLDivElement>(null);
  const subgroupLayer = useRef<HTMLDivElement>(null);
  const latestPin = useRef<(() => void) | null>(null);
  const selectedItem = items[selectedIndex];
  const pinnedHeight =
    heights.group +
    (selectedItem?.kind === "row" && selectedItem.subgroup !== undefined
      ? heights.subgroup
      : 0);
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
    // The library tells when it measures or redraws, which can move the headers under the pin.
    onChange: () => {
      latestPin.current?.();
    },
    overscan: OVERSCAN,
    // The selected row stays drawn wherever the list is scrolled to, so it keeps the focus a
    // click gave it.
    rangeExtractor: useCallback(
      (range: Range) => {
        const indexes = defaultRangeExtractor(range);
        if (selectedIndex === -1 || indexes.includes(selectedIndex)) {
          return indexes;
        }
        // The drawn indexes are one run, so the selected row is before or after all of them.
        return selectedIndex < (indexes[0] ?? 0)
          ? [selectedIndex, ...indexes]
          : [...indexes, selectedIndex];
      },
      [selectedIndex]
    ),
    scrollPaddingStart: pinnedHeight,
  });

  // Pins the headers for the list's scroll offset, and writes how far the next header pushes
  // them on their layers, as the library writes each item's offset on it.
  const syncPin = () => {
    const spans = virtualizer.measurementsCache;
    const measured = pinHeights(items, spans);
    setHeights((was) =>
      was.group === measured.group && was.subgroup === measured.subgroup
        ? was
        : measured
    );
    const pin = pinAt(items, spans, scroller.current?.scrollTop ?? 0, measured);
    const next = pinnedOf(pin);
    setPinned((was) =>
      was?.header === next?.header && was?.subheader === next?.subheader
        ? was
        : next
    );
    if (pin !== undefined) {
      groupLayer.current?.style.setProperty(
        "translate",
        `0 ${pin.groupShift}px`
      );
      subgroupLayer.current?.style.setProperty(
        "translate",
        `0 ${pin.subgroupShift}px`
      );
    }
  };
  useLayoutEffect(() => {
    latestPin.current = syncPin;
    syncPin();
  });

  // A click on the pinned plugin's header first brings its header in the list to the pinned
  // place, so the plugin stays at the top once its rows collapse.
  const togglePinned = (index: number, key: string) => {
    const span = virtualizer.measurementsCache[index];
    const place =
      span === undefined
        ? undefined
        : span.start + span.size - heights.group - heights.subgroup;
    if (place !== undefined && (scroller.current?.scrollTop ?? 0) > place) {
      virtualizer.scrollToOffset(place);
    }
    onToggle(key);
  };
  const pinnedHeader = pinned === undefined ? undefined : items[pinned.header];
  const pinnedSubheader =
    pinned?.subheader === undefined ? undefined : items[pinned.subheader];

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
      onScroll={() => {
        latestPin.current?.();
      }}
    >
      {groups.length === 0 ? (
        <p className="type-secondary text-text-hint m-0 py-10 text-center">
          No skills match
        </p>
      ) : (
        <>
          {pinned !== undefined && pinnedHeader?.kind === "header" && (
            <PinnedHeaders
              group={pinnedHeader.group}
              {...(pinnedSubheader?.kind === "subheader"
                ? {
                    subgroup: {
                      open: pinnedSubheader.open,
                      subgroup: pinnedSubheader.subgroup,
                    },
                  }
                : {})}
              groupLayer={groupLayer}
              subgroupLayer={subgroupLayer}
              onAdoptAll={onAdoptAll}
              onToggle={() => {
                if (
                  pinned.subheader !== undefined &&
                  pinnedSubheader?.kind === "subheader"
                ) {
                  togglePinned(pinned.subheader, pinnedSubheader.subgroup.key);
                }
              }}
            />
          )}
          <div ref={virtualizer.containerRef} className="relative">
            {drawn.map(({ group, header, parts }) => (
              <section
                key={group.key}
                aria-label={`${group.label} (${group.rows.length})`}
                className="contents"
              >
                {header !== undefined && (
                  <div
                    ref={virtualizer.measureElement}
                    data-index={header.index}
                    className={PLACED}
                  >
                    <GroupHeader
                      label={group.label}
                      source={group.source}
                      count={group.rows.length}
                      first={header.item.first}
                      action={
                        group.key === GROUP_DISK ? (
                          <AdoptAll onOpen={onAdoptAll} />
                        ) : undefined
                      }
                    />
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
                          className={PLACED}
                        >
                          <div
                            className={
                              subheader.item.first
                                ? "flex"
                                : "pt-subgroup-top flex"
                            }
                          >
                            <SectionToggle
                              label={subgroup.label}
                              count={subgroup.rows.length}
                              open={subheader.item.open}
                              onToggle={() => {
                                onToggle(subgroup.key);
                              }}
                            />
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
        </>
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
  collapsed,
  onToggle,
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
  /** The keys of the collapsed plugin subgroups. */
  collapsed: ReadonlySet<string>;
  /** Collapses or expands a plugin's subgroup, by its key. */
  onToggle: (key: string) => void;
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
        collapsed={collapsed}
        onToggle={onToggle}
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
