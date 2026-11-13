import { useEffect, useMemo, useRef, useState } from "react";

import type {
  Configuration,
  LibraryEntry,
  ProgressEvent,
  SearchResult,
  SourceEvent,
  SourceSkillEvent,
} from "#/cli/events.gen.ts";
import { SearchIcon } from "#/components/icons/search.tsx";
import { notify, notifyError } from "#/components/toast.tsx";
import { Button } from "#/components/ui/button.tsx";
import { Input } from "#/components/ui/input.tsx";
import { skillNamesOf } from "#/lib/problem-text.ts";
import { ActionBar } from "#/screens/sources/action-bar.tsx";
import { AddSourceDialog } from "#/screens/sources/add-source-dialog.tsx";
import { OpenSource } from "#/screens/sources/open-source.tsx";
import {
  count,
  directConfigurations,
  groupCards,
  installableCards,
  listingCards,
  orderCards,
  planAdds,
  resultCards,
  sourcePills,
  universalConfigurations,
  YOUR_SKILLS,
} from "#/screens/sources/projection.ts";
import type {
  AddPlan,
  CardGroup,
  SkillCard,
  SourcePill,
} from "#/screens/sources/projection.ts";
import { SelectAllRow } from "#/screens/sources/select-all-row.tsx";
import { SkillCards } from "#/screens/sources/skill-cards.tsx";
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
/** How long the search field waits after a change before it asks serve. */
const SEARCH_DEBOUNCE_MS = 150;

const noEntries: LibraryEntry[] = [];
const noConfigurations: Configuration[] = [];
const noCards: SkillCard[] = [];

type Selection = ReadonlyMap<string, SkillCard>;
const noSelection: Selection = new Map();

/** A search answer, kept with the query it answers. */
interface Found {
  query: string;
  results: SearchResult[];
}

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
 * The Sources screen, under Skills: the sources as pills, the card of the open one with its facts,
 * Refresh and Remove, its skills as cards to select, search across every source through serve,
 * and the floating bar that installs the selection with `skill add`. Every list comes from the
 * CLI: the app never filters a listing itself.
 */
export const SourcesScreen = () => {
  const sources = useAppStore((s) => s.sources);
  const listings = useAppStore((s) => s.listings);
  const installable = useAppStore((s) => s.installable);
  const library = useAppStore((s) => s.inventory?.library) ?? noEntries;
  const configurations =
    useAppStore((s) => s.inventory?.configurations) ?? noConfigurations;
  const adding = usePending(ADD_LABEL);

  // The pill the user opened; before any, the first pill is open.
  const [active, setActive] = useState<string | undefined>();
  const [query, setQuery] = useState("");
  const [found, setFound] = useState<Found | undefined>();
  const [selection, setSelection] = useState<Selection>(noSelection);
  // The user started from Select all or the preselection: the listing's source takes `--all`.
  const [allMode, setAllMode] = useState(false);
  const [targets, setTargets] = useState<string[] | undefined>();
  const [progress, setProgress] = useState<ProgressEvent | undefined>();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  // The pill opened last, "" before any, for a load that finishes after the user moved on.
  const opened = useRef("");
  // Where focus goes when the Remove dialog closes and Remove cannot take it.
  const searchField = useRef<HTMLInputElement>(null);

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
    setActive(undefined);
    setSelection(noSelection);
    setAllMode(false);
  });
  const activeKey = activePill?.key;

  useLoadUnopened(activePill, active);

  /** Makes `key` the open pill, with nothing selected and no search. */
  const select = (key: string) => {
    opened.current = key;
    setActive(key);
    setSelection(noSelection);
    setAllMode(false);
    setQuery("");
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
        : orderCards(installableCards(installable, library));
    }
    return listing === undefined || activePill.url === undefined
      ? noCards
      : orderCards(listingCards(listing, activePill.url, library));
  }, [activePill, installable, listing, library]);
  // A search answer replaces the grid while it answers the query as it stands.
  const results =
    found !== undefined && found.query === query.trim()
      ? found.results
      : undefined;
  const resultGroups = useMemo(
    () =>
      results === undefined
        ? undefined
        : groupCards(orderCards(resultCards(results, library))),
    [results, library]
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

  // Each settled query asks serve once; the answer to the latest request replaces the grid.
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
                setFound({ query: text, results: answer });
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

  const direct = directConfigurations(configurations);
  const universal = universalConfigurations(configurations);
  const enabled = direct.filter((c) => c.enabled).map((c) => c.id);
  const chosen = direct
    .map((c) => c.id)
    .filter((id) => (targets ?? enabled).includes(id));
  const toggleTarget = (id: string) => {
    setTargets(
      chosen.includes(id) ? chosen.filter((t) => t !== id) : [...chosen, id]
    );
  };

  /** Runs the plans one after another, `addedBefore` skills having landed so far. */
  const runPlans = async (
    plans: AddPlan[],
    addedBefore: number
  ): Promise<void> => {
    const [plan, ...rest] = plans;
    if (plan === undefined) {
      return;
    }
    setProgress(undefined);
    const outcome = await runCommand(plan.args, {
      label: ADD_LABEL,
      onProgress: setProgress,
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
      return;
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
    await runPlans(rest, addedBefore + installed.length);
  };

  const add = async () => {
    await runPlans(
      planAdds({
        allOf:
          allMode && activePill !== undefined && results === undefined
            ? { cards: shown, source: activePill.url ?? YOUR_SKILLS }
            : undefined,
        enabled,
        selected: [...selection.values()],
        targets: chosen,
      }),
      0
    );
    setProgress(undefined);
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
      <div className="pt-page-top px-page pb-page gap-page-title flex min-h-0 flex-1 flex-col overflow-auto">
        <div className="flex items-start justify-between gap-4">
          <h1 className="type-page-title text-text-primary m-0">Sources</h1>
          <Button
            variant="secondary"
            onClick={() => {
              setDialogOpen(true);
            }}
          >
            Add source
          </Button>
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
              setQuery(e.target.value);
            }}
          />
        </div>
        <SourceRow pills={pills} active={activeKey} now={now} onOpen={open} />
        {/* The card of the open source; a search looks across every source, so it hides. */}
        {activePill !== undefined && query.trim() === "" && (
          <OpenSource
            pill={activePill}
            library={library}
            now={now}
            fallbackFocus={searchField}
          />
        )}
        {allCards.length > 0 && (
          <SelectAllRow
            selected={selectedCount}
            total={selectable.length}
            added={allCards.length - selectable.length}
            onSelectAll={selectAll}
            onClear={clear}
          />
        )}
        {emptyText === undefined ? (
          <SkillCards
            groups={groups.filter((g) => g.cards.length > 0)}
            captions={resultGroups !== undefined}
            selected={new Set(selection.keys())}
            onToggle={toggleCard}
          />
        ) : (
          <p className="type-secondary text-text-hint m-0 py-10 text-center">
            {emptyText}
          </p>
        )}
        {/* Room for the floating bar over the last row of cards. */}
        {selection.size > 0 && <div className="h-floating-bar flex-none" />}
      </div>
      {selection.size > 0 && (
        <ActionBar
          count={selection.size}
          direct={direct}
          universal={universal}
          targets={chosen}
          onToggleTarget={toggleTarget}
          adding={adding}
          progress={progress}
          onAdd={() => {
            void add();
          }}
          onCancel={() => {
            void cancelCommand(ADD_LABEL);
          }}
        />
      )}
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
