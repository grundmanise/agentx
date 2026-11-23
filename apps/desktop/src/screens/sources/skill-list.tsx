import { useVirtualizer } from "@tanstack/react-virtual";
import { cva } from "class-variance-authority";
import {
  memo,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import type { ReactNode, RefObject } from "react";

import type { Configuration, Install } from "#/cli/events.gen.ts";
import { CodePane } from "#/components/code-pane.tsx";
import { InlineMarkdown } from "#/components/inline-markdown.tsx";
import { RemoveSwap } from "#/components/remove-swap.tsx";
import { Spinner } from "#/components/spinner.tsx";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "#/components/ui/accordion.tsx";
import { Toggle } from "#/components/ui/toggle.tsx";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "#/components/ui/tooltip.tsx";
import { formatDate } from "#/lib/dates.ts";
import { markdownText } from "#/lib/inline-markdown.ts";
import { openInBrowser } from "#/lib/open-url.ts";
import { formatSize } from "#/screens/skills/files.tsx";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { RemoveDialog } from "#/screens/skills/remove-dialog.tsx";
import { currentRows, useSkillRows } from "#/screens/skills/rows.ts";
import {
  addedName,
  installOf,
  statusLabels,
} from "#/screens/sources/projection.ts";
import type {
  CardGroup,
  SkillCard,
  SkillMdRef,
} from "#/screens/sources/projection.ts";
import { loadSkillMd } from "#/store/loaders.ts";
import { useSettling } from "#/store/settle.ts";
import { useAppStore } from "#/store/store.ts";
import type { SkillMdRead } from "#/store/store.ts";

const SKILL_MD = "SKILL.md";

const noConfigurations: Configuration[] = [];

/** Which read of a skill's SKILL.md a read in the store is: its subpath at its tree. */
const versionOf = ({ subpath, tree }: SkillMdRef) => `${subpath}\u0000${tree}`;

/**
 * Whether a card has what it shows when open: no SKILL.md to read, or the read of its current
 * version ended, read or not.
 */
const isReady = (card: SkillCard, read: SkillMdRead | undefined) =>
  card.skillMd === undefined ||
  (read !== undefined && read.version === versionOf(card.skillMd));

/**
 * The Remove dialog of an Added card. While open it shows the skill as the inventory has it now,
 * so an agent that gets the skill meanwhile is listed; as it closes it keeps `snapshot`, so its
 * text stays while it goes.
 */
const SourcesRemoveDialog = ({
  snapshot,
  open,
  onClose,
  onCloseAutoFocus,
}: {
  snapshot: SkillRow;
  open: boolean;
  onClose: () => void;
  onCloseAutoFocus: (event: Event, removed: boolean) => void;
}) => {
  const rows = useSkillRows();
  const configurations =
    useAppStore((s) => s.inventory?.configurations) ?? noConfigurations;
  const live = open ? rows.find((r) => r.key === snapshot.key) : undefined;
  return (
    <RemoveDialog
      row={live ?? snapshot}
      configurations={configurations}
      open={open}
      fromUniversal={false}
      onClose={onClose}
      onCloseAutoFocus={onCloseAutoFocus}
    />
  );
};

/** The round check at the end of a selectable skill's row: a ring, filled with the check when selected. */
const CheckMark = ({ checked }: { checked: boolean }) => (
  <svg
    viewBox="0 0 20 20"
    aria-hidden
    focusable="false"
    className="block size-5 flex-none"
  >
    {checked ? (
      <>
        <circle cx="10" cy="10" r="10" className="fill-add-card-check-bg" />
        <path
          d="M6 10.25l2.75 2.75 5.25-6"
          fill="none"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          className="stroke-on-accent"
        />
      </>
    ) : (
      <circle
        cx="10"
        cy="10"
        r="9.25"
        fill="none"
        strokeWidth="1.5"
        className="stroke-add-card-check-ring"
      />
    )}
  </svg>
);

export const statusVariants = cva("type-secondary flex-none font-medium", {
  variants: {
    status: {
      added: "text-add-card-status-added",
      forked: "text-add-card-status-forked",
      "name-in-use": "text-add-card-status-in-use",
      "on-disk": "text-add-card-status-disk",
    },
  },
});

/** A line in the code pane's place while SKILL.md is not shown. */
const PaneHint = ({ children }: { children: string }) => (
  <p className="bg-code-bg py-code px-file-row type-secondary text-text-hint m-0">
    {children}
  </p>
);

/**
 * The skill's SKILL.md from `source read`, in the Code pane. The row opens once the read ends, so
 * this shows the file or why it is not shown.
 */
const SkillMd = ({
  cardKey,
  name,
  skillMd,
}: {
  cardKey: string;
  name: string;
  skillMd: SkillMdRef;
}) => {
  const read = useAppStore((s) => s.skillMds[cardKey]);
  const current =
    read !== undefined && read.version === versionOf(skillMd)
      ? read
      : undefined;
  if (current === undefined) {
    return null;
  }
  if (current.failed === true) {
    return <PaneHint>{`${SKILL_MD} could not be read`}</PaneHint>;
  }
  const { file } = current;
  if (file.truncated === true) {
    return (
      <PaneHint>{`Too large to show (${formatSize(file.size)})`}</PaneHint>
    );
  }
  if (file.binary) {
    return <PaneHint>{`Binary file (${formatSize(file.size)})`}</PaneHint>;
  }
  return (
    <CodePane
      path={SKILL_MD}
      label={`${SKILL_MD} of ${name}`}
      content={file.content ?? ""}
    />
  );
};

/**
 * "View skill": the skill's directory on its source's web page, opened in the browser. It sits
 * beside the name in the open row, over the row's trigger, and takes its own clicks.
 */
const ViewSkill = ({ page }: { page: string }) => (
  <a
    href={page}
    title={page}
    className="type-secondary text-text-primary pointer-events-auto flex-none font-medium underline underline-offset-2"
    onClick={(e) => {
      e.preventDefault();
      void openInBrowser(page);
    }}
  >
    View skill
  </a>
);

/**
 * What an open row shows: why a skill that cannot be selected cannot be, when it last entered and
 * left this machine's library, and its SKILL.md, whose frontmatter holds the description. One of
 * your skills on the account remote has no SKILL.md to read here, so its row shows its description.
 */
const Details = ({
  card,
  install,
}: {
  card: SkillCard;
  install: Install | undefined;
}) => {
  const description =
    card.skillMd === undefined && card.description !== ""
      ? card.description
      : undefined;
  const dates = [
    install?.added === undefined
      ? undefined
      : `Added ${formatDate(install.added)}`,
    install?.removed === undefined
      ? undefined
      : `Removed ${formatDate(install.removed)}`,
  ]
    .filter((part) => part !== undefined)
    .join(" · ");
  const header =
    description !== undefined || card.title !== undefined || dates !== "";
  return (
    <>
      {header && (
        <div className="px-file-row flex flex-col gap-2 pb-3.5">
          {description !== undefined && (
            <p className="type-body text-text-muted m-0 text-pretty">
              <InlineMarkdown text={description} />
            </p>
          )}
          {card.title !== undefined && (
            <p className="type-secondary text-text-hint m-0 text-pretty">
              {card.title}
            </p>
          )}
          {dates !== "" && (
            <p className="type-technical-sm text-text-caption m-0">{dates}</p>
          )}
        </div>
      )}
      {card.skillMd !== undefined && (
        <SkillMd cardKey={card.key} name={card.name} skillMd={card.skillMd} />
      )}
    </>
  );
};

const LOADING = "Loading skill…";

/**
 * A spinner in the description's place while the row's SKILL.md is read, before the row opens.
 * The read is often quick, so it has no text; its tooltip says what it waits for.
 */
const Loading = () => (
  <output className="text-text-muted flex min-w-0 flex-1 items-center">
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="pointer-events-auto flex flex-none">
          <Spinner size="sm" />
        </span>
      </TooltipTrigger>
      <TooltipContent>{LOADING}</TooltipContent>
    </Tooltip>
    <span className="sr-only">{LOADING}</span>
  </output>
);

const noLibrary: never[] = [];
const noInstalls: never[] = [];

/**
 * The date a row shows before its status or check: when an Added skill was added, or when a skill
 * that can be selected left this machine's library. Undefined when this machine recorded neither.
 */
const rowDate = (
  card: SkillCard,
  install: Install | undefined
): string | undefined => {
  if (card.status === "added" && install?.added !== undefined) {
    return formatDate(install.added);
  }
  return card.status === undefined && install?.removed !== undefined
    ? `Removed ${formatDate(install.removed)}`
    : undefined;
};

/**
 * The text at a row's end, before its action: the status of a skill that has no action, or the
 * row's date. Added is the action's own label, so its date sits before it and it never moves.
 */
const rowMeta = (card: SkillCard, date: string | undefined): ReactNode => {
  if (card.status !== undefined && card.status !== "added") {
    return (
      <span className={statusVariants({ status: card.status })}>
        {statusLabels[card.status]}
      </span>
    );
  }
  return date === undefined ? undefined : (
    <span className="type-technical-sm text-file-row-meta flex-none">
      {date}
    </span>
  );
};

/**
 * The Added status of a card whose skill this machine installed from it: a red Remove while
 * hovered or focused, which asks to remove the skill from every agent. It stays Remove, disabled,
 * while that removal runs and until the inventory after it arrives.
 */
const AddedRemove = ({
  card,
  onRemove,
}: {
  card: SkillCard;
  onRemove: (name: string) => void;
}) => {
  const name = useAppStore((s) =>
    addedName(card, s.inventory?.library ?? noLibrary)
  );
  // The Remove dialog's command, as it labels it (`removeLabel`).
  const busy = useSettling(`remove ${name ?? ""}`);
  return (
    <RemoveSwap
      status={statusLabels.added}
      label={`Remove ${name ?? card.name}`}
      busy={name !== undefined && busy}
      onClick={() => {
        if (name !== undefined) {
          onRemove(name);
        }
      }}
    />
  );
};

/**
 * One skill as a card of its own: the row that opens it (the chevron, its name, one line of its
 * description while closed, View skill beside the name while open) and, at its end, the round
 * check that selects it, or for a skill this machine holds already its status, which for an Added
 * skill turns into Remove on hover; before either, while closed, the date of the last add or
 * removal when this machine recorded it. The row is named
 * by the skill alone; its status and description are read as its description. A row asked to
 * open reads its SKILL.md first, with a spinner in the description's place, and opens once the
 * read ends.
 */
const SkillItemView = ({
  card,
  open,
  loading,
  selected,
  onToggle,
  onRemove,
}: {
  card: SkillCard;
  open: boolean;
  /** Whether the row was asked to open and its SKILL.md is being read. */
  loading: boolean;
  selected: boolean;
  onToggle: (card: SkillCard) => void;
  /** Asks to remove the library skill of that name. */
  onRemove: (name: string) => void;
}) => {
  const descriptionId = useId();
  const wanted = open || loading;
  const source = card.skillMd?.source;
  const subpath = card.skillMd?.subpath;
  const tree = card.skillMd?.tree;
  useEffect(() => {
    if (
      wanted &&
      source !== undefined &&
      subpath !== undefined &&
      tree !== undefined
    ) {
      void loadSkillMd(card.key, { source, subpath, tree });
    }
  }, [wanted, card.key, source, subpath, tree]);
  const install = useAppStore((s) =>
    installOf(card, s.inventory?.installs ?? noInstalls)
  );
  const status =
    card.status === undefined ? undefined : statusLabels[card.status];
  const date = rowDate(card, install);
  const description = [
    [status, date].filter((part) => part !== undefined).join(" "),
    markdownText(card.description),
  ]
    .filter((part) => part !== undefined && part !== "")
    .join(". ");
  let action: ReactNode;
  if (card.status === undefined) {
    action = (
      <Toggle
        kind="check"
        pressed={selected}
        onPressedChange={() => {
          onToggle(card);
        }}
        aria-label={`Select ${card.name}`}
      >
        <CheckMark checked={selected} />
      </Toggle>
    );
  } else if (card.status === "added") {
    action = <AddedRemove card={card} onRemove={onRemove} />;
  }
  return (
    <AccordionItem variant="list" value={card.key} selected={selected}>
      <AccordionTrigger
        variant="list"
        label={card.name}
        description={description === "" ? undefined : descriptionId}
        open={open}
        busy={loading}
        meta={rowMeta(card, open ? undefined : date)}
        action={action}
      >
        {/* The name and View skill share a baseline, as their sizes differ. */}
        <span className="flex min-w-0 flex-initial items-baseline gap-3">
          <span
            aria-hidden
            className="type-row-name text-text-primary min-w-0 flex-initial truncate"
          >
            {card.name}
          </span>
          {open && card.page !== undefined && <ViewSkill page={card.page} />}
        </span>
        {loading ? (
          <Loading />
        ) : (
          // The details show the description in full while the row is open.
          <span
            aria-hidden
            className="type-row-desc text-text-muted min-w-0 flex-1 truncate"
          >
            {open ? "" : <InlineMarkdown text={card.description} />}
          </span>
        )}
        {description !== "" && (
          <span id={descriptionId} className="sr-only">
            {description}
          </span>
        )}
      </AccordionTrigger>
      <AccordionContent>
        <Details card={card} install={install} />
      </AccordionContent>
    </AccordionItem>
  );
};

/**
 * A row draws again only when its own props change, not each time the list draws as it scrolls.
 * Its card and `onToggle` keep their identity between the list's draws.
 */
const SkillItem = memo(SkillItemView);

/** One thing the list places at its own offset: a source's caption, or one of its skills. */
type Item =
  | { kind: "caption"; key: string; group: CardGroup; first: boolean }
  | {
      kind: "card";
      key: string;
      group: CardGroup;
      card: SkillCard;
      at: number;
    };

/** The captions, when shown, and the cards in the order the list shows them. */
const itemsOf = (groups: CardGroup[], captions: boolean): Item[] =>
  groups.flatMap((group, i) => [
    ...(captions
      ? [
          {
            first: i === 0,
            group,
            key: `caption:${group.source}`,
            kind: "caption" as const,
          },
        ]
      : []),
    ...group.cards.map((card, at): Item => ({
      at,
      card,
      group,
      key: card.key,
      kind: "card",
    })),
  ]);

/**
 * The height the list assumes for an item it has not drawn yet, in px, close to what the tokens
 * give: a caption, and a closed row with `add-card.spacing` above it unless it opens its group.
 * Each item takes its measured height once it is drawn, an open one included.
 */
const estimatedHeight = (item: Item | undefined) => {
  if (item?.kind === "caption") {
    return item.first ? 28 : 56;
  }
  return item?.at === 0 ? 46 : 52;
};

/**
 * The height a drawn item takes in the list: the height the browser lays it out at, up to the
 * next whole pixel, so that none overlaps the next.
 */
const measuredHeight = (element: Element, entry?: ResizeObserverEntry) =>
  Math.ceil(
    entry?.borderBoxSize[0]?.blockSize ?? element.getBoundingClientRect().height
  );

/** How many items the list keeps drawn beyond each edge of what shows. */
const OVERSCAN = 8;

/** A drawn item: the list sets its offset from the top. */
const PLACED_CAPTION_FIRST = "absolute top-0 left-0 w-full pb-3";
const PLACED_CAPTION = "absolute top-0 left-0 w-full pt-7 pb-3";
const PLACED_CARD_FIRST = "absolute top-0 left-0 w-full";
/** A card after the first of its group, `add-card.spacing` below the one before it. */
const PLACED_CARD = "pt-add-card absolute top-0 left-0 w-full";

/**
 * Where the list starts in the scroller's content, in px, for the virtualizer's scroll margin. It
 * changes whenever something above it in the scroller changes size, comes or goes, such as the
 * card of the open source or the select-all row.
 */
const useOffsetIn = (
  list: RefObject<HTMLElement | null>,
  scroller: RefObject<HTMLElement | null>
) => {
  const [offset, setOffset] = useState(0);
  useLayoutEffect(() => {
    const element = list.current;
    const parent = scroller.current;
    const measure = () => {
      if (element === null || parent === null) {
        return;
      }
      const next = Math.round(
        element.getBoundingClientRect().top -
          parent.getBoundingClientRect().top +
          parent.scrollTop
      );
      setOffset((previous) => (previous === next ? previous : next));
    };
    const sizes = new ResizeObserver(measure);
    // Only what comes before the list moves where it starts.
    const observeSiblings = () => {
      sizes.disconnect();
      for (const child of parent?.children ?? []) {
        if (child === element) {
          break;
        }
        sizes.observe(child);
      }
      measure();
    };
    observeSiblings();
    const children = new MutationObserver(observeSiblings);
    if (parent !== null) {
      children.observe(parent, { childList: true });
    }
    return () => {
      sizes.disconnect();
      children.disconnect();
    };
  }, [list, scroller]);
  return offset;
};

/**
 * The skills to add as one list, a card per skill, one section per source. With `captions`, each
 * section opens with the source's mono caption, as a search answer does. A row opens and closes on
 * a click anywhere but its check, several at once; only the check selects. The list scrolls in
 * `scroller`, the screen's own scroll container, and only the rows near the part that shows are in
 * the document, so a source of thousands of skills stays quick.
 */
export const SkillList = ({
  groups,
  captions,
  selected,
  onToggle,
  scroller,
}: {
  groups: CardGroup[];
  captions: boolean;
  /** The keys of the selected cards. */
  selected: ReadonlySet<string>;
  onToggle: (card: SkillCard) => void;
  /** The element the list scrolls in. */
  scroller: RefObject<HTMLElement | null>;
}) => {
  // The rows asked to open. A row whose SKILL.md is still read waits closed, then opens.
  const [wanted, setWanted] = useState<string[]>([]);
  const skillMds = useAppStore((s) => s.skillMds);
  const list = useRef<HTMLDivElement>(null);
  const offset = useOffsetIn(list, scroller);
  const items = itemsOf(groups, captions);
  const cards = new Map(
    items.flatMap((item) =>
      item.kind === "card" ? [[item.key, item.card] as const] : []
    )
  );
  const ready = (key: string) => {
    const card = cards.get(key);
    return card === undefined || isReady(card, skillMds[key]);
  };
  const open = wanted.filter(ready);
  // A click on a row that opens, or one that waits for its read, asks again: it no longer
  // waits. A click on an open row closes it.
  const onOpenChange = (next: string[]) => {
    const clicked = new Set([
      ...next.filter((key) => !open.includes(key)),
      ...open.filter((key) => !next.includes(key)),
    ]);
    setWanted((previous) => [
      ...previous.filter((key) => !clicked.has(key)),
      ...[...clicked].filter((key) => !previous.includes(key)),
    ]);
  };
  // The same function on every render, which calls the latest `onToggle`.
  const latestToggle = useRef(onToggle);
  useEffect(() => {
    latestToggle.current = onToggle;
  }, [onToggle]);
  const toggle = useCallback((card: SkillCard) => {
    latestToggle.current(card);
  }, []);
  // The skill the Remove dialog asks about. It stays while the dialog closes, so the dialog keeps
  // its text as it goes.
  const [removing, setRemoving] = useState<SkillRow>();
  const [removeOpen, setRemoveOpen] = useState(false);
  const remove = useCallback((name: string) => {
    const row = currentRows().find((r) => r.key === name);
    if (row !== undefined) {
      setRemoving(row);
      setRemoveOpen(true);
    }
  }, []);
  // The virtualizer's functions return new values while they stay the same functions, which
  // React Compiler cannot memoize, so it skips this component. The component reads them on
  // every render and passes none of them to a memoized component.
  // oxlint-disable-next-line react/incompatible-library
  const virtualizer = useVirtualizer({
    count: items.length,
    // The library sets each drawn item's offset and the list's height on the elements itself.
    directDomUpdates: true,
    directDomUpdatesMode: "position",
    estimateSize: (index: number) => estimatedHeight(items[index]),
    getItemKey: (index: number) => items[index]?.key ?? index,
    getScrollElement: () => scroller.current,
    measureElement: measuredHeight,
    overscan: OVERSCAN,
    scrollMargin: offset,
  });

  const drawn = virtualizer.getVirtualItems().flatMap((v) => {
    const item = items[v.index];
    return item === undefined ? [] : [{ index: v.index, item }];
  });
  const sections: { group: CardGroup; nodes: ReactNode[] }[] = [];
  for (const { index, item } of drawn) {
    let last = sections.at(-1);
    if (last?.group !== item.group) {
      last = { group: item.group, nodes: [] };
      sections.push(last);
    }
    last.nodes.push(
      item.kind === "caption" ? (
        <div
          key={item.key}
          ref={virtualizer.measureElement}
          data-index={index}
          className={item.first ? PLACED_CAPTION_FIRST : PLACED_CAPTION}
        >
          <h2 className="type-caption text-group-label m-0">
            {item.group.label}
          </h2>
        </div>
      ) : (
        <div
          key={item.key}
          ref={virtualizer.measureElement}
          data-index={index}
          className={item.at === 0 ? PLACED_CARD_FIRST : PLACED_CARD}
        >
          <SkillItem
            card={item.card}
            open={open.includes(item.card.key)}
            loading={
              wanted.includes(item.card.key) && !open.includes(item.card.key)
            }
            selected={selected.has(item.card.key)}
            onToggle={toggle}
            onRemove={remove}
          />
        </div>
      )
    );
  }

  return (
    <div ref={list} className="flex-none">
      <Accordion
        variant="list"
        label="Skills"
        value={open}
        onValueChange={onOpenChange}
      >
        <div ref={virtualizer.containerRef} className="relative">
          {sections.map(({ group, nodes }) => (
            <section
              key={group.source}
              aria-label={group.label}
              className="contents"
            >
              {nodes}
            </section>
          ))}
        </div>
      </Accordion>
      {removing !== undefined && (
        <SourcesRemoveDialog
          snapshot={removing}
          open={removeOpen}
          onClose={() => {
            setRemoveOpen(false);
          }}
          onCloseAutoFocus={(event, removed) => {
            // The dialog has no trigger: after Cancel the focus goes back to the card's Remove, if
            // it is still drawn. After a removal that Remove is disabled, so the default applies.
            const label = `Remove ${removing.name}`;
            const opener = [
              ...(list.current?.querySelectorAll("button") ?? []),
            ].find((b) => b.getAttribute("aria-label") === label);
            if (!removed && opener !== undefined) {
              event.preventDefault();
              opener.focus();
            }
          }}
        />
      )}
    </div>
  );
};
