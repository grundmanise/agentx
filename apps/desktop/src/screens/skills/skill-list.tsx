import { defaultRangeExtractor, useVirtualizer } from "@tanstack/react-virtual";
import type { Range } from "@tanstack/react-virtual";
import {
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
 * A run of rows the list places as one block: a group's rows, or one open plugin's. Each block
 * draws only its rows near the part of the list that shows.
 */
interface Block {
  key: string;
  group: Group;
  subgroup?: Subgroup;
  rows: SkillRow[];
}

const groupKey = (group: Group) => `group:${group.key}`;
const subgroupKey = (subgroup: Subgroup) => `subgroup:${subgroup.key}`;

/** The blocks in the order the list shows them; a collapsed plugin has none. */
const blocksOf = (groups: Group[], collapsed: ReadonlySet<string>): Block[] =>
  groups.flatMap((group): Block[] =>
    group.subgroups === undefined
      ? [{ group, key: groupKey(group), rows: group.rows }]
      : group.subgroups
          .filter((subgroup) => !collapsed.has(subgroup.key))
          .map((subgroup) => ({
            group,
            key: subgroupKey(subgroup),
            rows: subgroup.rows,
            subgroup,
          }))
  );

/**
 * Where a block is: the offset of its rows from the top of the list, and the height of the
 * headers pinned above them, which a row comes into view below; in px.
 */
interface Place {
  offset: number;
  pinned: number;
}

const samePlaces = (
  a: ReadonlyMap<string, Place>,
  b: ReadonlyMap<string, Place>
) =>
  a.size === b.size &&
  [...b].every(([key, place]) => {
    const was = a.get(key);
    return was?.offset === place.offset && was.pinned === place.pinned;
  });

const sameSet = (a: ReadonlySet<string>, b: ReadonlySet<string>) =>
  a.size === b.size && [...b].every((key) => a.has(key));

/**
 * The height the list assumes for a row it has not drawn yet, in px, close to what the tokens
 * give: a row with `row.spacing` above it, with or without its description line. Each row takes
 * its measured height once it is drawn.
 */
const estimatedHeight = (row: SkillRow | undefined) =>
  row?.description === "" ? 44 : 66;

/**
 * The height a drawn row takes in the list: the height the browser lays it out at, up to the
 * next whole pixel. Every row then starts on a whole pixel, where the list can scroll to, and
 * none overlaps the next.
 */
const measuredHeight = (element: Element, entry?: ResizeObserverEntry) =>
  Math.ceil(
    entry?.borderBoxSize[0]?.blockSize ?? element.getBoundingClientRect().height
  );

/** How many rows a block keeps drawn beyond each edge of what shows. */
const OVERSCAN = 8;

/** How far beyond each edge of what shows a block counts as near, and draws its rows, in px. */
const NEAR_PX = 480;

/** A drawn row: the list sets its offset from the top of its block. */
const PLACED_ROW = "absolute top-0 left-0 flex w-full";
/** A row after the first of its block, `row.spacing` below the row before it. */
const PLACED_ROW_AFTER = "pt-rows absolute top-0 left-0 flex w-full";
/** A row of a plugin's subgroup, indented so its glyph sits under the plugin's name. */
const PLACED_SUBROW = "pl-subgroup-indent absolute top-0 left-0 flex w-full";
const PLACED_SUBROW_AFTER =
  "pt-rows pl-subgroup-indent absolute top-0 left-0 flex w-full";

/** Where a row is placed: after the first of its block, `row.spacing` lower; in a subgroup, indented. */
const placedRow = (at: number, inSubgroup: boolean) => {
  if (!inSubgroup) {
    return at === 0 ? PLACED_ROW : PLACED_ROW_AFTER;
  }
  return at === 0 ? PLACED_SUBROW : PLACED_SUBROW_AFTER;
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

/**
 * The rows of one block. Only the rows near the part of the list that shows are in the
 * document, each at its offset in the block, which the library writes on it; the selected row
 * stays drawn wherever the list is scrolled to, so it keeps the focus a click gave it.
 */
const RowBlock = ({
  block,
  place,
  near,
  scroller,
  selectedKey,
  forks,
  configurations,
  facts,
  onSelect,
  watcher,
  onLayout,
  onList,
  onScroll,
}: {
  block: Block;
  place: Place;
  /** Whether the block is near the part of the list that shows. */
  near: boolean;
  scroller: RefObject<HTMLElement | null>;
  selectedKey: string | undefined;
  forks: Map<string, SkillRow[]>;
  configurations: Configuration[];
  facts: Parameters<typeof SkillRowItem>[0]["facts"];
  onSelect: (row: SkillRow) => void;
  /** Tells the list when the block comes near the part that shows, or goes far. */
  watcher: IntersectionObserver | null;
  /** Tells the list that the block measured its rows, which can move the blocks below it. */
  onLayout: () => void;
  /** Keeps the block's list element, which the list measures. */
  onList: (key: string, element: HTMLUListElement | null) => void;
  /** Keeps how the block scrolls one of its rows into view, by its place in the block. */
  onScroll: (key: string, scroll: ((index: number) => void) | null) => void;
}) => {
  const { key, rows, subgroup } = block;
  const selectedIndex = useMemo(
    () => rows.findIndex((row) => row.key === selectedKey),
    [rows, selectedKey]
  );
  // The virtualizer's functions return new values while they stay the same functions, which
  // React Compiler cannot memoize. The app is built without it, and this component reads them
  // on every render and passes none of them to a memoized component.
  // oxlint-disable-next-line react/incompatible-library
  const virtualizer = useVirtualizer({
    count: rows.length,
    // The library sets each drawn row's offset and the block's height on the elements itself.
    directDomUpdates: true,
    directDomUpdatesMode: "position",
    estimateSize: useCallback(
      (index: number) => estimatedHeight(rows[index]),
      [rows]
    ),
    getItemKey: useCallback(
      (index: number) => rows[index]?.key ?? index,
      [rows]
    ),
    getScrollElement: () => scroller.current,
    measureElement: measuredHeight,
    onChange: onLayout,
    overscan: OVERSCAN,
    // A block far from what shows draws no rows, but the selected one.
    rangeExtractor: useCallback(
      (range: Range) => {
        const indexes = near ? defaultRangeExtractor(range) : [];
        if (selectedIndex === -1 || indexes.includes(selectedIndex)) {
          return indexes;
        }
        // The drawn indexes are one run, so the selected row is before or after all of them.
        return selectedIndex < (indexes[0] ?? 0)
          ? [selectedIndex, ...indexes]
          : [...indexes, selectedIndex];
      },
      [near, selectedIndex]
    ),
    scrollMargin: place.offset,
    scrollPaddingStart: place.pinned,
  });
  const { containerRef } = virtualizer;
  const listRef = useCallback(
    (element: HTMLUListElement) => {
      containerRef(element);
      onList(key, element);
      watcher?.observe(element);
      return () => {
        watcher?.unobserve(element);
        onList(key, null);
        containerRef(null);
      };
    },
    [containerRef, onList, key, watcher]
  );
  useEffect(() => {
    onScroll(key, (index) => {
      virtualizer.scrollToIndex(index, { align: "auto" });
    });
    return () => {
      onScroll(key, null);
    };
  }, [key, onScroll, virtualizer]);
  return (
    <ul ref={listRef} data-block={key} className="relative">
      {virtualizer.getVirtualItems().map(({ index }) => {
        const row = rows[index];
        if (row === undefined) {
          return null;
        }
        return (
          <li
            key={row.key}
            ref={virtualizer.measureElement}
            data-index={index}
            aria-posinset={index + 1}
            aria-setsize={rows.length}
            className={placedRow(index, subgroup !== undefined)}
          >
            <SkillRowItem
              row={row}
              forks={forks.get(row.name) ?? NO_FORKS}
              configurations={configurations}
              facts={facts}
              selected={row.key === selectedKey}
              onSelect={onSelect}
            />
          </li>
        );
      })}
    </ul>
  );
};

const NO_PLACE: Place = { offset: 0, pinned: 0 };

/** The block and place of the row `key`, or none when the filters leave it out. */
const locate = (blocks: Block[], key: string | undefined) => {
  let found: { block: string; index: number; key: string } | undefined;
  for (const block of blocks) {
    const index = block.rows.findIndex((row) => row.key === key);
    if (key !== undefined && index !== -1) {
      found = { block: block.key, index, key };
      break;
    }
  }
  return found;
};

/**
 * The rows in their groups, each group under its header. The headers are in the document and
 * pin themselves with `position: sticky`: a group's header at the top of the list, a plugin's
 * under it, each while its section shows, so the next one pushes it out. Only the rows near the
 * part of the list that shows are in the document, so the list stays quick with hundreds of
 * skills: each group's label carries its full count, and each row its place in its group. The
 * selected row comes into view below the pinned headers when the selection changes.
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
  const blocks = useMemo(
    () => blocksOf(groups, collapsed),
    [groups, collapsed]
  );

  // The list's scroll element and content, the blocks' lists, the pinned headers above them,
  // and how each block scrolls a row into view.
  const scroller = useRef<HTMLElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const lists = useRef(new Map<string, HTMLUListElement>());
  const headers = useRef(new Map<string, HTMLDivElement>());
  const scrolls = useRef(new Map<string, (index: number) => void>());
  const onList = useCallback(
    (key: string, element: HTMLUListElement | null) => {
      if (element === null) {
        lists.current.delete(key);
      } else {
        lists.current.set(key, element);
      }
    },
    []
  );
  const onHeader = useCallback(
    (key: string, element: HTMLDivElement | null) => {
      if (element === null) {
        headers.current.delete(key);
      } else {
        headers.current.set(key, element);
      }
    },
    []
  );
  const onScroll = useCallback(
    (key: string, scroll: ((index: number) => void) | null) => {
      if (scroll === null) {
        scrolls.current.delete(key);
      } else {
        scrolls.current.set(key, scroll);
      }
    },
    []
  );

  // Where each block is, and which blocks are near the part of the list that shows. The library
  // places each block's rows from its offset in the list, which the headers above it decide.
  const [places, setPlaces] = useState<ReadonlyMap<string, Place>>(
    () => new Map()
  );
  const [near, setNear] = useState<ReadonlySet<string>>(() => new Set());
  const measure = () => {
    const list = scroller.current?.getBoundingClientRect();
    const top = content.current?.getBoundingClientRect().top;
    if (list === undefined || top === undefined) {
      return;
    }
    const next = new Map<string, Place>();
    const shown = new Set<string>();
    for (const block of blocks) {
      const element = lists.current.get(block.key);
      if (element === undefined) {
        continue;
      }
      const box = element.getBoundingClientRect();
      const pinned =
        (headers.current.get(groupKey(block.group))?.offsetHeight ?? 0) +
        (block.subgroup === undefined
          ? 0
          : (headers.current.get(subgroupKey(block.subgroup))?.offsetHeight ??
            0));
      next.set(block.key, { offset: box.top - top, pinned });
      if (
        box.bottom >= list.top - NEAR_PX &&
        box.top <= list.bottom + NEAR_PX
      ) {
        shown.add(block.key);
      }
    }
    setPlaces((was) => (samePlaces(was, next) ? was : next));
    setNear((was) => (sameSet(was, shown) ? was : shown));
  };
  const latestMeasure = useRef(measure);
  useLayoutEffect(() => {
    latestMeasure.current = measure;
    measure();
  });
  // A block's offset moves when the rows above it are measured.
  const onLayout = useCallback(() => {
    latestMeasure.current();
  }, []);
  // A block comes near or goes far as the list scrolls.
  const [watcher, setWatcher] = useState<IntersectionObserver | null>(null);
  const scrollerRef = useCallback((element: HTMLElement) => {
    scroller.current = element;
    const observer = new IntersectionObserver(
      (entries) => {
        setNear((was) => {
          const next = new Set(was);
          for (const entry of entries) {
            const key =
              entry.target instanceof HTMLElement
                ? entry.target.dataset.block
                : undefined;
            if (key !== undefined && entry.isIntersecting) {
              next.add(key);
            } else if (key !== undefined) {
              next.delete(key);
            }
          }
          return sameSet(was, next) ? was : next;
        });
      },
      { root: element, rootMargin: `${NEAR_PX}px 0px` }
    );
    setWatcher(observer);
    return () => {
      observer.disconnect();
      setWatcher(null);
      scroller.current = null;
    };
  }, []);

  // The block and place of the selected row, or none when the filters leave it out.
  const located = useMemo(
    () => locate(blocks, selectedKey),
    [blocks, selectedKey]
  );
  // The selected row comes into view when the selection changes (a key press, a click, or a route
  // another screen opened) and when the filters list it again, once its block is placed. A list
  // that changes around the same selected row stays where it is.
  const latestLocated = useRef(located);
  useEffect(() => {
    latestLocated.current = located;
  }, [located]);
  const placed = located !== undefined && places.has(located.block);
  useEffect(() => {
    const at = latestLocated.current;
    if (placed && at !== undefined && at.key === selectedKey) {
      scrolls.current.get(at.block)?.(at.index);
    }
  }, [selectedKey, placed]);

  const blockOf = (key: string) => blocks.find((block) => block.key === key);
  const rowBlock = (block: Block | undefined) =>
    block === undefined ? null : (
      <RowBlock
        block={block}
        place={places.get(block.key) ?? NO_PLACE}
        near={near.has(block.key)}
        scroller={scroller}
        selectedKey={selectedKey}
        forks={forks}
        configurations={configurations}
        facts={facts}
        onSelect={select}
        watcher={watcher}
        onLayout={onLayout}
        onList={onList}
        onScroll={onScroll}
      />
    );
  return (
    <nav
      ref={scrollerRef}
      aria-label="Skills"
      className="pr-split-list-right pb-split-list-bottom pl-split-list-left min-h-0 flex-1 overflow-auto pt-0"
    >
      {groups.length === 0 ? (
        <p className="type-secondary text-text-hint m-0 py-10 text-center">
          No skills match
        </p>
      ) : (
        <div ref={content}>
          {groups.map((group, at) => (
            <section
              key={group.key}
              aria-label={`${group.label} (${group.rows.length})`}
              className={at === 0 ? undefined : "pt-group-spacing"}
            >
              <div
                ref={(element) => {
                  onHeader(groupKey(group), element);
                }}
                className="bg-shell-content-bg sticky top-0 z-10"
              >
                <GroupHeader
                  label={group.label}
                  source={group.source}
                  count={group.rows.length}
                  action={
                    group.key === GROUP_DISK ? (
                      <AdoptAll onOpen={onAdoptAll} />
                    ) : undefined
                  }
                />
              </div>
              {group.subgroups === undefined
                ? rowBlock(blockOf(groupKey(group)))
                : group.subgroups.map((subgroup, place) => (
                    <section
                      key={subgroup.key}
                      aria-label={`${subgroup.label} (${subgroup.rows.length})`}
                      className={place === 0 ? undefined : "pt-subgroup-top"}
                    >
                      <div
                        ref={(element) => {
                          onHeader(subgroupKey(subgroup), element);
                        }}
                        className="bg-shell-content-bg top-group-header sticky z-5 flex"
                      >
                        <SectionToggle
                          label={subgroup.label}
                          count={subgroup.rows.length}
                          open={!collapsed.has(subgroup.key)}
                          onToggle={() => {
                            onToggle(subgroup.key);
                          }}
                        />
                      </div>
                      {rowBlock(blockOf(subgroupKey(subgroup)))}
                    </section>
                  ))}
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
