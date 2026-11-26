import { useInterval } from "@mantine/hooks";
import { useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";

import type {
  InstallableSkillEvent,
  LibraryEntry,
  SearchResult,
  Source,
} from "#/cli/events.gen.ts";
import { RefreshIcon } from "#/components/icons/refresh.tsx";
import { SearchIcon } from "#/components/icons/search.tsx";
import type { RimRun } from "#/components/progress-rim/rim-progress.ts";
import { notify, notifyError } from "#/components/toast.tsx";
import { Button } from "#/components/ui/button.tsx";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "#/components/ui/input-group.tsx";
import { Spinner } from "#/components/ui/spinner.tsx";
import { problemSentence, skillNamesOf } from "#/lib/problem-text.ts";
import { cn } from "#/lib/utils.ts";
import { FloatingActionBar } from "#/screens/sources/action-bar.tsx";
import { addProblem } from "#/screens/sources/add-problem.ts";
import { AddSourceDialog } from "#/screens/sources/add-source-dialog.tsx";
import {
  addSource,
  stopAddingSource,
  useAddingSources,
} from "#/screens/sources/add-source.ts";
import { useAgentTargets } from "#/screens/sources/agent-targets.tsx";
import { OpenSource } from "#/screens/sources/open-source.tsx";
import {
  count,
  installableCards,
  listingCards,
  pageRef,
  nameOrder,
  oneOfEachName,
  planAdds,
  resultCards,
  sameName,
  sourcePills,
  HUB_SKILLS,
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
import { search } from "#/store/feed.ts";
import { loadInstallable, loadLabel, loadListing } from "#/store/loaders.ts";
import { cancelCommand, agentx, usePending } from "#/store/runner.ts";
import { sourcesOf, useAppStore } from "#/store/store.ts";
import type { Listing } from "#/store/store.ts";

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

/** The label of the read that fills Hub, for `usePending`. */
const HUB_LOADING = loadLabel(["skill", "list", "--remote"]);

/** Whether the pill that was open is gone, its source removed. */
const isGone = (open: string | undefined, pills: SourcePill[]) =>
  open !== undefined && !pills.some((p) => p.key === open);

/**
 * The open pill: the one the user opened, or else the first shared source, Hub when there is none,
 * also once the one opened was removed.
 * `onGone` runs when the pill open at the last render is gone, its source removed.
 */
const useOpenPill = (
  pills: SourcePill[],
  active: string | undefined,
  onGone: () => void
) => {
  const [lastOpen, setLastOpen] = useState<string | undefined>();
  const pill =
    pills.find((p) => p.key === active) ??
    pills.find((p) => p.key !== HUB_SKILLS) ??
    pills[0];
  if (isGone(lastOpen, pills)) {
    onGone();
  }
  if (pill?.key !== lastOpen) {
    setLastOpen(pill?.key);
  }
  return pill;
};

/**
 * Loads what the open pill lists. A source's listing is asked for with each inventory, which
 * reads it only when the inventory gives the source a commit the listing held is not of. Hub's
 * skills are read when the user did not open it, as when it is the first pill.
 */
const useOpenListing = (key: string | undefined, opened?: string) => {
  const inventory = useAppStore((s) => s.inventory);
  useEffect(() => {
    if (key === undefined || inventory === undefined) {
      return;
    }
    if (key !== HUB_SKILLS) {
      void loadListing(key);
    } else if (key !== opened) {
      void loadInstallable();
    }
  }, [key, opened, inventory]);
};

/**
 * Refresh all: one `source fetch --all`. The inventory that follows gives each source's commit
 * and skill count. Hub lists your skills from `skill list --remote`, so they are read again,
 * whenever there is a hub.
 */
const refreshAll = async () => {
  const outcome = await agentx(["source", "fetch", "--all"], {
    label: REFRESH_ALL_LABEL,
  });
  const sources = sourcesOf(useAppStore.getState());
  if (outcome.exitCode !== null && sources.some((s) => s.hub === true)) {
    await loadInstallable();
  }
};

/** The pills, with the source Add source just added until the inventory lists it. */
const usePills = (sources: Source[]) => {
  const [justAdded, setJustAdded] = useState<Source | undefined>();
  // Once the inventory lists it, the inventory alone says whether it is there.
  if (justAdded !== undefined && sources.some((s) => s.id === justAdded.id)) {
    setJustAdded(undefined);
  }
  const pills = sourcePills(
    justAdded === undefined ? sources : [...sources, justAdded]
  );
  return { pills, setJustAdded };
};

/**
 * Your skills on the hub a search matched: of those the hub lists to install, the ones whose
 * name a match under the hub's URL carries.
 */
const hubMatches = (
  installable: InstallableSkillEvent[],
  results: SearchResult[],
  hub: string
) => {
  const names = new Set(
    results.filter((r) => r.source === hub).map((r) => r.name)
  );
  return installable.filter((s) => names.has(s.name));
};

/**
 * Each pill's count of matches, by its key, in place of its skill count: Hub's once your skills
 * on the hub are read.
 */
const matchCounts = (
  pills: SourcePill[],
  results: SearchResult[],
  installable: InstallableSkillEvent[] | undefined
) =>
  new Map(
    pills.flatMap((p) => {
      if (p.key !== HUB_SKILLS) {
        return [[p.key, results.filter((r) => r.source === p.url).length]];
      }
      return installable === undefined
        ? []
        : [[p.key, hubMatches(installable, results, p.source.url).length]];
    })
  );

/** The cards of a search's matches in the pill's source, or of your skills on the hub, A to Z. */
const matchedCards = (
  pill: SourcePill,
  results: SearchResult[],
  library: LibraryEntry[],
  installable: InstallableSkillEvent[] | undefined
): SkillCard[] => {
  if (pill.key === HUB_SKILLS) {
    return installable === undefined
      ? noCards
      : nameOrder(
          installableCards(
            hubMatches(installable, results, pill.source.url),
            library
          )
        );
  }
  return nameOrder(
    resultCards(
      results.filter((r) => r.source === pill.url),
      library,
      () => pageRef(pill.source)
    )
  );
};

/**
 * The card of the open source, which a search's results replace: folded, it fades out while its
 * room closes, and the results move up over it, so the card is never seen cut; unfolded, its room
 * opens while it fades in.
 */
const FoldingCard = ({
  folded,
  children,
}: {
  folded: boolean;
  children: ReactNode;
}) => (
  <div
    className={cn(
      "grid transition-all duration-300 ease-in-out motion-reduce:transition-none",
      folded
        ? "grid-rows-source-card-folded -mt-page-title invisible"
        : "grid-rows-source-card"
    )}
  >
    <div
      className={cn(
        "min-h-0 transition-opacity motion-reduce:transition-none",
        folded ? "opacity-0 duration-150" : "duration-300"
      )}
    >
      {children}
    </div>
  </div>
);

/** Reads your skills on the hub once a search shows, so that Hub can count its matches. */
const useInstallableWhileSearching = (
  searching: boolean,
  hub: boolean,
  read: boolean
) => {
  useEffect(() => {
    if (searching && hub && !read) {
      void loadInstallable();
    }
  }, [searching, hub, read]);
};

/** Whether the listing held of the source `id` is of the commit the inventory gives the source. */
const isListed = (
  sources: Source[],
  listings: Record<string, Listing>,
  id: string | undefined
) => {
  const source = sources.find((s) => s.id === id);
  const listing = id === undefined ? undefined : listings[id];
  return (
    source !== undefined &&
    listing !== undefined &&
    listing.commit === source.commit
  );
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
 * Refresh and Remove, its skills as a list to open and select, search across every shared source through
 * serve, whose matches the pills count and the list shows source by source, and the floating bar that installs the selection with `skill add`. Every list comes from the
 * CLI: the app never filters a listing itself.
 */
export const SourcesScreen = () => {
  const sources = useAppStore(sourcesOf);
  const listings = useAppStore((s) => s.listings);
  const installable = useAppStore((s) => s.installable);
  const library = useAppStore((s) => s.inventory?.library) ?? noEntries;
  const adding = usePending(ADD_LABEL);
  const addingLinks = useAddingSources((s) => s.links);
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
  // The source Add source just added: its skills are selected once its listing is read.
  const [preselect, setPreselect] = useState<string | undefined>();
  const [now, setNow] = useState(() => Date.now());
  // The pill opened last, "" before any, for a load that finishes after the user moved on.
  const opened = useRef("");
  // Where focus goes when the Remove dialog closes and Remove cannot take it.
  const searchField = useRef<HTMLInputElement>(null);
  // The screen's scroll container, which the skill list scrolls in.
  const scroller = useRef<HTMLDivElement>(null);

  // The fetch labels keep time.
  useInterval(
    () => {
      setNow(Date.now());
    },
    60_000,
    { autoInvoke: true }
  );

  const { pills, setJustAdded } = usePills(sources);
  // A source removed while open, opened or first, takes its selection with it; the first pill opens.
  const activePill = useOpenPill(pills, active, () => {
    // A late preselection of a source just added must not land on the next one.
    opened.current = "";
    setActive(undefined);
    setSelection(noSelection);
    setAllMode(false);
  });

  useOpenListing(activePill?.key, active);

  /** Sets the query; an empty one shows the listing, and the next search starts from it. */
  const changeQuery = (text: string) => {
    setQuery(text);
    if (text.trim() === "") {
      setFound(undefined);
    }
  };

  /** Makes `key` the open pill, with nothing selected; a search stays. */
  const select = (key: string) => {
    opened.current = key;
    setActive(key);
    setSelection(noSelection);
    setAllMode(false);
    setPreselect(undefined);
    setNow(Date.now());
  };
  const open = (key: string) => {
    select(key);
    // Opening Hub reads your skills again; a source's listing follows the open pill.
    if (key === HUB_SKILLS) {
      void loadInstallable();
    }
  };

  // A search answer replaces the listing. While the latest query waits for its answer, the last
  // answer stays, so the list and its Select all count change once, not back to the listing between.
  const results = query.trim() === "" ? undefined : found;
  const activeKey = activePill?.key;
  useInstallableWhileSearching(
    results !== undefined,
    pills.some((p) => p.key === HUB_SKILLS),
    installable !== undefined
  );
  const matches =
    results === undefined
      ? undefined
      : matchCounts(pills, results, installable);

  const listing =
    activePill === undefined || activePill.key === HUB_SKILLS
      ? undefined
      : listings[activePill.key];
  const shownCards = (): SkillCard[] => {
    if (activePill === undefined) {
      return noCards;
    }
    if (results !== undefined) {
      return matchedCards(activePill, results, library, installable);
    }
    if (activePill.key === HUB_SKILLS) {
      return installable === undefined
        ? noCards
        : nameOrder(installableCards(installable, library));
    }
    return listing === undefined || activePill.url === undefined
      ? noCards
      : nameOrder(
          listingCards(
            listing.skills,
            activePill.url,
            library,
            pageRef(sources.find((s) => s.id === activePill.key))
          )
        );
  };
  const shown = shownCards();
  const groups: CardGroup[] =
    activePill === undefined
      ? []
      : [
          {
            cards: shown,
            label: activePill.label,
            source: activePill.url ?? HUB_SKILLS,
          },
        ];
  const allCards = groups.flatMap((g) => g.cards);
  const selectable = allCards.filter((c) => c.status === undefined);
  const selectedCount = selectable.filter((c) => selection.has(c.key)).length;
  const selectableCount = oneOfEachName(selectable).length;
  const loading = usePending(activeKey === HUB_SKILLS ? HUB_LOADING : "");

  /** Whether the list shows the listing of the source to preselect. */
  const showsPreselect = () =>
    preselect === activeKey &&
    results === undefined &&
    isListed(sources, listings, preselect);
  if (showsPreselect()) {
    setPreselect(undefined);
    setSelection(new Map(oneOfEachName(selectable).map((c) => [c.key, c])));
    setAllMode(true);
  }

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
    changeQuery("");
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
        // The library holds one skill of a name: selecting one deselects the other.
        for (const other of next.values()) {
          if (sameName(other, card)) {
            next.delete(other.key);
          }
        }
        next.set(card.key, card);
      }
      return next;
    });
  };
  const selectAll = () => {
    setSelection((prev) => {
      const next = new Map(prev);
      for (const card of oneOfEachName(selectable, next.values())) {
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
   * `of` plans having ended. True when every plan ran to the end with exit code 0. Retry after
   * the lock runs `retry` with the plan.
   */
  const runPlans = async (
    plans: AddPlan[],
    addedBefore: number,
    before: number,
    of: number,
    retry: (plan: AddPlan) => void
  ): Promise<boolean> => {
    const [plan, ...rest] = plans;
    if (plan === undefined) {
      return true;
    }
    const outcome = await agentx(plan.args, {
      failure: (o) => {
        const error = o.events.find((e) => e.type === "error");
        const words = error === undefined ? undefined : addProblem(error);
        return words;
      },
      label: ADD_LABEL,
      // The rim reads the CLI's steps (three per skill and a rescan) as a share of the whole install.
      onProgress: (event) => {
        const steps =
          event.total === undefined || event.total === 0
            ? 0
            : event.current / event.total;
        setRim({ fraction: (before + steps) / of, state: "running" });
      },
      retry: () => {
        retry(plan);
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
        // Some skills were skipped: what was added, and why the rest was not.
        const words = addProblem(error);
        notifyError({
          details:
            words !== undefined || error.hint === undefined ? [] : [error.hint],
          // In the app's words, the reason and its way out read as one sentence each.
          hint:
            words === undefined
              ? error.message
              : problemSentence(words, { keep: plan.names }),
          // A skill's name that starts the CLI's message keeps its case.
          keep: [
            ...plan.names,
            ...skillNamesOf(useAppStore.getState().inventory),
          ],
          message: `Added ${installed.length} of ${count(plan.names.length, "skill")}`,
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
      of,
      retry
    );
    return outcome.exitCode === 0 && restOk;
  };

  /** Runs the plans as one install, its progress on the bar's rim. */
  const install = async (plans: AddPlan[]) => {
    setRim({ fraction: 0, state: "running" });
    // Retry after the lock runs the plan as an install of its own, so its rim ends too.
    const ok = await runPlans(plans, 0, 0, plans.length, (plan) => {
      void install([plan]);
    });
    // The rim fills and fades, or drains, before the bar may close (DESIGN.md §8 "Progress rim").
    setRim({ fraction: ok ? 1 : 0, state: ok ? "done" : "stopped" });
  };

  const add = async () => {
    const plans = planAdds({
      allOf:
        allMode && activePill !== undefined && results === undefined
          ? { cards: shown, source: activePill.url ?? HUB_SKILLS }
          : undefined,
      enabled,
      selected: [...selection.values()],
      targets: chosen,
    });
    await install(plans);
  };

  /**
   * Adds the source of `typed` while the user goes on. Once added, it opens with every selectable
   * card selected, unless the user opened another pill meanwhile.
   */
  const addAndOpen = async (typed: string) => {
    const before = opened.current;
    const source = await addSource(typed);
    if (source === undefined) {
      return;
    }
    setJustAdded(source);
    if (opened.current !== before) {
      return;
    }
    select(source.id);
    changeQuery("");
    setPreselect(source.id);
  };

  const empty = (): string | undefined => {
    if (pills.length === 0) {
      return "Add a source to see its skills";
    }
    if (results !== undefined) {
      return shown.length === 0 ? "No skills match" : undefined;
    }
    if (loading || activePill === undefined) {
      return undefined;
    }
    if (activePill.key === HUB_SKILLS) {
      return installable !== undefined && shown.length === 0
        ? "Every skill on your hub is on this machine"
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
              track="sources.refresh-all"
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
              track="sources.add"
              onClick={() => {
                setDialogOpen(true);
              }}
            >
              Add source
            </Button>
          </div>
        </div>
        <InputGroup>
          <InputGroupAddon kind="search">
            <SearchIcon size="sm" />
          </InputGroupAddon>
          <InputGroupInput
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
        </InputGroup>
        <SourceRow
          pills={pills}
          adding={addingLinks}
          active={activeKey}
          matches={matches}
          now={now}
          onOpen={open}
          onStop={stopAddingSource}
        />
        {activePill !== undefined && (
          <FoldingCard folded={results !== undefined}>
            <OpenSource
              // A new source gets a new card, so a Remove dialog open on the old one closes.
              key={activePill.key}
              pill={activePill}
              library={library}
              now={now}
              fallbackFocus={searchField}
            />
          </FoldingCard>
        )}
        {/* Over the card, so the results cover it as they move up into its place. */}
        <div className="bg-shell-content-bg gap-page-title relative z-10 flex flex-none flex-col">
          {allCards.length > 0 && (
            <SelectAllRow
              align="end"
              track="sources.select-all"
              selected={selectedCount}
              total={selectableCount}
              onSelectAll={selectAll}
              onClear={clear}
            />
          )}
          {emptyText === undefined ? (
            <SkillList
              groups={groups.filter((g) => g.cards.length > 0)}
              selected={new Set(selection.keys())}
              onToggle={toggleCard}
              scroller={scroller}
            />
          ) : (
            <p className="type-secondary text-text-hint m-0 py-10 text-center">
              {emptyText}
            </p>
          )}
        </div>
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
        onAdd={(typed) => {
          void addAndOpen(typed);
        }}
      />
    </div>
  );
};
