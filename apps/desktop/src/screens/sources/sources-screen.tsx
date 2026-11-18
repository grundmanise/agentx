import { useEffect, useMemo, useRef, useState } from "react";

import type {
  LibraryEntry,
  SearchResult,
  SourceEvent,
  SourceSkillEvent,
} from "#/cli/events.gen.ts";
import { RefreshIcon } from "#/components/icons/refresh.tsx";
import { SearchIcon } from "#/components/icons/search.tsx";
import type { RimRun } from "#/components/progress-rim/rim-progress.ts";
import { Spinner } from "#/components/spinner.tsx";
import { notify, notifyError } from "#/components/toast.tsx";
import { Button } from "#/components/ui/button.tsx";
import { Input } from "#/components/ui/input.tsx";
import { skillNamesOf } from "#/lib/problem-text.ts";
import { FloatingActionBar } from "#/screens/sources/action-bar.tsx";
import { AddSourceDialog } from "#/screens/sources/add-source-dialog.tsx";
import { useAgentTargets } from "#/screens/sources/agent-targets.tsx";
import { OpenSource } from "#/screens/sources/open-source.tsx";
import {
  count,
  groupCards,
  installableCards,
  listingCards,
  pageRef,
  nameOrder,
  planAdds,
  resultCards,
  sourcePills,
  YOUR_SKILLS,
} from "#/screens/sources/projection.ts";
import type {
  AddPlan,
  CardGroup,
  SkillCard,
  SourcePill,
} from "#/screens/sources/projection.ts";
import { SelectAllRow } from "#/screens/sources/select-all-row.tsx";
import { SkillList } from "#/screens/sources/skill-list.tsx";
import { SourceRow } from "#/screens/sources/source-row.tsx";
import { useFetchedListings } from "#/screens/sources/use-fetched-listings.ts";
import { search } from "#/store/feed.ts";
import {
  loadInstallable,
  loadLabel,
  loadListing,
  loadSources,
} from "#/store/loaders.ts";
import { cancelCommand, runCommand, usePending } from "#/store/runner.ts";
import { useAppStore } from "#/store/store.ts";

/** The pending label of the install the action bar runs. */
export const ADD_LABEL = "add skills";
/** The pending label of Refresh all, `source fetch --all`. */
const REFRESH_ALL_LABEL = "refresh all sources";
/** How long the search field waits after a change before it asks serve. */
const SEARCH_DEBOUNCE_MS = 150;

const noEntries: LibraryEntry[] = [];
const noCards: SkillCard[] = [];

type Selection = ReadonlyMap<string, SkillCard>;
const noSelection: Selection = new Map();

/** Loads what a pill shows: a source's listing, or your skills on the account remote. */
const loadPill = async (key: string) => {
  await (key === YOUR_SKILLS ? loadInstallable() : loadListing(key));
};

/** The label of the read that fills a pill, for `usePending`. */
const loadingLabel = (key: string) =>
  loadLabel(
    key === YOUR_SKILLS
      ? ["skill", "list", "--remote"]
      : ["source", "skills", key]
  );

/** Whether the pill that was open is gone, its source removed. */
const isGone = (open: string | undefined, pills: SourcePill[]) =>
  open !== undefined && !pills.some((p) => p.key === open);

/**
 * The open pill: the one the user opened, or the first one, also once the one opened was removed.
 * `onGone` runs when the pill open at the last render is gone, its source removed.
 */
const useOpenPill = (
  pills: SourcePill[],
  active: string | undefined,
  onGone: () => void
) => {
  const [lastOpen, setLastOpen] = useState<string | undefined>();
  const pill = pills.find((p) => p.key === active) ?? pills[0];
  if (isGone(lastOpen, pills)) {
    onGone();
  }
  if (pill?.key !== lastOpen) {
    setLastOpen(pill?.key);
  }
  return pill;
};

/**
 * Loads the open pill when the user did not open it, such as the first one, unless
 * `useFetchedListings` reads its listing: Personal, or a source never fetched.
 */
const useLoadUnopened = (pill: SourcePill | undefined, opened?: string) => {
  const key = pill?.key;
  const fetched = pill?.lastFetched;
  useEffect(() => {
    if (
      key !== undefined &&
      key !== opened &&
      (key === YOUR_SKILLS || fetched === undefined)
    ) {
      void loadPill(key);
    }
  }, [key, opened, fetched]);
};

/**
 * Refresh all: one `source fetch --all`, after which the screen reads again the listing of each
 * source `source list` reports a new fetch for. Personal lists your skills from
 * `skill list --remote`, so they are read again too, whenever there is an account remote.
 */
const refreshAll = async () => {
  const outcome = await runCommand(["source", "fetch", "--all"], {
    label: REFRESH_ALL_LABEL,
  });
  const { sources } = useAppStore.getState();
  if (outcome.exitCode !== null && sources.some((s) => s.account === true)) {
    await loadInstallable();
  }
};

/** The pills, with the source Add source just added until the Sources slice lists it. */
const usePills = (
  sources: SourceEvent[],
  listings: Record<string, SourceSkillEvent[]>
) => {
  const [justAdded, setJustAdded] = useState<SourceEvent | undefined>();
  // Once the slice lists it, the slice alone says whether it is there.
  if (justAdded !== undefined && sources.some((s) => s.id === justAdded.id)) {
    setJustAdded(undefined);
  }
  const pills = useMemo(
    () =>
      sourcePills(
        justAdded === undefined ? sources : [...sources, justAdded],
        listings
      ),
    [sources, justAdded, listings]
  );
  return { pills, setJustAdded };
};

/** The selection without the cards `drop` names. */
const without = (selection: Selection, drop: (card: SkillCard) => boolean) => {
  const next = new Map(selection);
  for (const [key, card] of selection) {
    if (drop(card)) {
      next.delete(key);
    }
  }
  return next;
};

/**
 * The Sources screen, under Skills: Refresh all and Add source, the sources as pills, the card of the open one with its facts,
 * Refresh and Remove, its skills as a list to open and select, search across every source through serve,
 * and the floating bar that installs the selection with `skill add`. Every list comes from the
 * CLI: the app never filters a listing itself.
 */
export const SourcesScreen = () => {
  const sources = useAppStore((s) => s.sources);
  const listings = useAppStore((s) => s.listings);
  const installable = useAppStore((s) => s.installable);
  const library = useAppStore((s) => s.inventory?.library) ?? noEntries;
  const adding = usePending(ADD_LABEL);
  // Pending from the fetch until your skills are read again.
  const [refreshing, setRefreshing] = useState(false);

  // The pill the user opened; before any, the first pill is open.
  const [active, setActive] = useState<string | undefined>();
  const [query, setQuery] = useState("");
  const [found, setFound] = useState<SearchResult[] | undefined>();
  const [selection, setSelection] = useState<Selection>(noSelection);
  // The user started from Select all or the preselection: the listing's source takes `--all`.
  const [allMode, setAllMode] = useState(false);
  // The install the bar's rim shows; it stays until the rim has faded.
  const [rim, setRim] = useState<RimRun | undefined>();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  // The pill opened last, "" before any, for a load that finishes after the user moved on.
  const opened = useRef("");
  // Where focus goes when the Remove dialog closes and Remove cannot take it.
  const searchField = useRef<HTMLInputElement>(null);
  // The screen's scroll container, which the skill list scrolls in.
  const scroller = useRef<HTMLDivElement>(null);

  // The sources are read again when the screen opens, and the fetch labels keep time.
  useEffect(() => {
    void loadSources();
    const timer = setInterval(() => {
      setNow(Date.now());
    }, 60_000);
    return () => {
      clearInterval(timer);
    };
  }, []);

  useFetchedListings(sources);

  const { pills, setJustAdded } = usePills(sources, listings);
  // A source removed while open, opened or first, takes its selection with it; the first pill opens.
  const activePill = useOpenPill(pills, active, () => {
    // A late preselection of a source just added must not land on the next one.
    opened.current = "";
    setActive(undefined);
    setSelection(noSelection);
    setAllMode(false);
  });
  const activeKey = activePill?.key;

  useLoadUnopened(activePill, active);

  /** Sets the query; an empty one shows the listing, and the next search starts from it. */
  const changeQuery = (text: string) => {
    setQuery(text);
    if (text.trim() === "") {
      setFound(undefined);
    }
  };

  /** Makes `key` the open pill, with nothing selected and no search. */
  const select = (key: string) => {
    opened.current = key;
    setActive(key);
    setSelection(noSelection);
    setAllMode(false);
    changeQuery("");
    setNow(Date.now());
  };
  const open = (key: string) => {
    select(key);
    void loadPill(key);
  };

  const listing =
    activePill === undefined || activePill.key === YOUR_SKILLS
      ? undefined
      : listings[activePill.key];
  const shown = useMemo((): SkillCard[] => {
    if (activePill === undefined) {
      return noCards;
    }
    if (activePill.key === YOUR_SKILLS) {
      return installable === undefined
        ? noCards
        : nameOrder(installableCards(installable, library));
    }
    return listing === undefined || activePill.url === undefined
      ? noCards
      : nameOrder(
          listingCards(
            listing,
            activePill.url,
            library,
            pageRef(sources.find((s) => s.id === activePill.key))
          )
        );
  }, [activePill, installable, listing, library, sources]);
  // A search answer replaces the listing. While the latest query waits for its answer, the last
  // answer stays, so the list and its Select all count change once, not back to the listing between.
  const results = query.trim() === "" ? undefined : found;
  const resultGroups = useMemo(
    () =>
      results === undefined
        ? undefined
        : groupCards(
            nameOrder(
              resultCards(
                // A source removed while the answer is shown takes its results with it.
                results.filter((r) => sources.some((s) => s.url === r.source)),
                library,
                (url) => pageRef(sources.find((s) => s.url === url))
              )
            )
          ),
    [results, library, sources]
  );
  const groups: CardGroup[] =
    resultGroups ??
    (activePill === undefined
      ? []
      : [
          {
            cards: shown,
            label: activePill.label,
            source: activePill.url ?? YOUR_SKILLS,
          },
        ]);
  const allCards = groups.flatMap((g) => g.cards);
  const selectable = allCards.filter((c) => c.status === undefined);
  const selectedCount = selectable.filter((c) => selection.has(c.key)).length;
  const loading = usePending(
    activeKey === undefined ? "" : loadingLabel(activeKey)
  );

  // Each settled query asks serve once; the answer to the latest request replaces the listing.
  useEffect(() => {
    const text = query.trim();
    let stale = false;
    const timer =
      text === ""
        ? undefined
        : setTimeout(() => {
            void (async () => {
              const answer = await search(text);
              if (!stale && answer !== undefined) {
                setFound(answer);
              }
            })();
          }, SEARCH_DEBOUNCE_MS);
    return () => {
      stale = true;
      if (timer !== undefined) {
        clearTimeout(timer);
      }
    };
  }, [query]);

  const onRefreshAll = async () => {
    setRefreshing(true);
    // The command and the read never reject: a failure is an outcome.
    await refreshAll();
    setRefreshing(false);
  };

  const toggleCard = (card: SkillCard) => {
    setSelection((prev) => {
      const next = new Map(prev);
      if (next.has(card.key)) {
        next.delete(card.key);
      } else {
        next.set(card.key, card);
      }
      return next;
    });
  };
  const selectAll = () => {
    setSelection((prev) => {
      const next = new Map(prev);
      for (const card of selectable) {
        next.set(card.key, card);
      }
      return next;
    });
    if (results === undefined) {
      setAllMode(true);
    }
  };
  const clear = () => {
    setSelection(noSelection);
    setAllMode(false);
  };

  const {
    chosen,
    clear: clearTargets,
    direct,
    enabled,
    toggle: toggleTarget,
    universal,
  } = useAgentTargets();

  /**
   * Runs the plans one after another, `addedBefore` skills having landed so far and `before` of
   * `of` plans having ended. True when every plan ran to the end with exit code 0.
   */
  const runPlans = async (
    plans: AddPlan[],
    addedBefore: number,
    before: number,
    of: number
  ): Promise<boolean> => {
    const [plan, ...rest] = plans;
    if (plan === undefined) {
      return true;
    }
    const outcome = await runCommand(plan.args, {
      label: ADD_LABEL,
      // The rim reads the CLI's steps (three per skill and a rescan) as a share of the whole install.
      onProgress: (event) => {
        const steps =
          event.total === undefined || event.total === 0
            ? 0
            : event.current / event.total;
        setRim({ fraction: (before + steps) / of, state: "running" });
      },
      success: () => `Added ${count(plan.names.length, "skill")}`,
    });
    const installed = outcome.events
      .filter((e) => e.type === "library_skill")
      .map((e) => e.name);
    if (outcome.cancelled) {
      const steps = outcome.events.filter(
        (e) => e.type === "progress" && e.phase === "install"
      ).length;
      const added = addedBefore + steps;
      notify(
        `Stopped. ${count(added, "skill")} ${added === 1 ? "was" : "were"} added.`
      );
      return false;
    }
    if (outcome.exitCode === 0) {
      setSelection((prev) => without(prev, (c) => c.source === plan.source));
      setAllMode(false);
    } else {
      const error = outcome.events.find((e) => e.type === "error");
      if (installed.length > 0 && error !== undefined) {
        // Some skills were skipped: what was added, why the rest was not, and each skipped skill.
        notifyError({
          details: error.hint === undefined ? [] : [error.hint],
          hint: error.message,
          // A skill's name that starts the CLI's message keeps its case.
          keep: [
            ...plan.names,
            ...skillNamesOf(useAppStore.getState().inventory),
          ],
          message: `Added ${installed.length} of ${count(plan.names.length, "skill")}`,
          warnings: outcome.logs
            .filter((l) => l.level === "warn")
            .map((l) => l.message),
        });
        setSelection((prev) =>
          without(
            prev,
            (c) => c.source === plan.source && installed.includes(c.name)
          )
        );
      }
    }
    const restOk = await runPlans(
      rest,
      addedBefore + installed.length,
      before + 1,
      of
    );
    return outcome.exitCode === 0 && restOk;
  };

  const add = async () => {
    const plans = planAdds({
      allOf:
        allMode && activePill !== undefined && results === undefined
          ? { cards: shown, source: activePill.url ?? YOUR_SKILLS }
          : undefined,
      enabled,
      selected: [...selection.values()],
      targets: chosen,
    });
    setRim({ fraction: 0, state: "running" });
    const ok = await runPlans(plans, 0, 0, plans.length);
    // The rim fills and fades, or drains, before the bar may close (DESIGN.md §8 "Progress rim").
    setRim({ fraction: ok ? 1 : 0, state: ok ? "done" : "stopped" });
  };

  /** A source just added opens with every selectable card selected, once its listing arrives. */
  const onSourceAdded = async (source: SourceEvent) => {
    setJustAdded(source);
    select(source.id);
    await loadPill(source.id);
    if (opened.current !== source.id) {
      return;
    }
    const state = useAppStore.getState();
    const cards = listingCards(
      state.listings[source.id] ?? [],
      source.url,
      state.inventory?.library ?? noEntries
    ).filter((c) => c.status === undefined);
    setSelection(new Map(cards.map((c) => [c.key, c])));
    setAllMode(true);
  };

  const empty = (): string | undefined => {
    if (pills.length === 0) {
      return "Add a source to see its skills";
    }
    if (resultGroups !== undefined) {
      return resultGroups.length === 0 ? "No skills match" : undefined;
    }
    if (loading || activePill === undefined) {
      return undefined;
    }
    if (activePill.key === YOUR_SKILLS) {
      return installable !== undefined && shown.length === 0
        ? "Every skill on your account remote is on this machine"
        : undefined;
    }
    return listing !== undefined && shown.length === 0
      ? "No skills in this source"
      : undefined;
  };
  const emptyText = empty();

  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      <div
        ref={scroller}
        className="pt-page-top px-page pb-page gap-page-title flex min-h-0 flex-1 flex-col overflow-auto"
      >
        <div className="flex items-start justify-between gap-4">
          <h1 className="type-page-title text-text-primary m-0">Sources</h1>
          <div className="gap-button flex items-center">
            <Button
              variant="secondary"
              aria-busy={refreshing}
              disabled={refreshing || pills.length === 0}
              onClick={() => {
                void onRefreshAll();
              }}
            >
              {refreshing ? <Spinner /> : <RefreshIcon />}
              Refresh all
            </Button>
            <Button
              variant="secondary"
              onClick={() => {
                setDialogOpen(true);
              }}
            >
              Add source
            </Button>
          </div>
        </div>
        <div className="text-input-placeholder relative flex items-center">
          <span className="pointer-events-none absolute left-3.5 flex">
            <SearchIcon size="sm" />
          </span>
          <Input
            kind="search"
            type="search"
            ref={searchField}
            aria-label="Search all sources"
            placeholder="Search all sources"
            value={query}
            onChange={(e) => {
              changeQuery(e.target.value);
            }}
          />
        </div>
        <SourceRow pills={pills} active={activeKey} now={now} onOpen={open} />
        {/* The card of the open source; it stays while a search looks across every source. */}
        {activePill !== undefined && (
          <OpenSource
            // A new source gets a new card, so a Remove dialog open on the old one closes.
            key={activePill.key}
            pill={activePill}
            library={library}
            now={now}
            fallbackFocus={searchField}
          />
        )}
        {allCards.length > 0 && (
          <SelectAllRow
            align="end"
            selected={selectedCount}
            total={selectable.length}
            onSelectAll={selectAll}
            onClear={clear}
          />
        )}
        {emptyText === undefined ? (
          <SkillList
            groups={groups.filter((g) => g.cards.length > 0)}
            captions={resultGroups !== undefined}
            selected={new Set(selection.keys())}
            onToggle={toggleCard}
            scroller={scroller}
          />
        ) : (
          <p className="type-secondary text-text-hint m-0 py-10 text-center">
            {emptyText}
          </p>
        )}
        {/* Room for the floating bar over the last skill of the list. */}
        {selection.size > 0 && <div className="h-floating-bar flex-none" />}
      </div>
      <FloatingActionBar
        shown={selection.size > 0 || rim !== undefined}
        count={selection.size}
        direct={direct}
        universal={universal}
        targets={chosen}
        onToggleTarget={toggleTarget}
        onClearTargets={clearTargets}
        adding={adding || rim !== undefined}
        rim={rim}
        onRimSettled={() => {
          setRim(undefined);
        }}
        onAdd={() => {
          void add();
        }}
        onCancel={() => {
          void cancelCommand(ADD_LABEL);
        }}
      />
      <AddSourceDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        onAdded={(source) => {
          void onSourceAdded(source);
        }}
      />
    </div>
  );
};
