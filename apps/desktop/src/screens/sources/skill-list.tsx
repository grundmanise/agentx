import { useVirtualizer } from "@tanstack/react-virtual";
import { cva } from "class-variance-authority";
import {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import type { ReactNode, RefObject } from "react";

import { CodePane } from "#/components/code-pane.tsx";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "#/components/ui/accordion.tsx";
import { Toggle } from "#/components/ui/toggle.tsx";
import { formatSize } from "#/screens/skills/files.tsx";
import { statusLabels } from "#/screens/sources/projection.ts";
import type {
  CardGroup,
  SkillCard,
  SkillMdRef,
} from "#/screens/sources/projection.ts";
import { loadSkillMd } from "#/store/loaders.ts";
import { useAppStore } from "#/store/store.ts";

const SKILL_MD = "SKILL.md";

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

const statusVariants = cva("type-secondary flex-none font-medium", {
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

/** The skill's SKILL.md from `source read`, read when its row opens, in the Code pane. */
const SkillMd = ({
  cardKey,
  name,
  skillMd,
}: {
  cardKey: string;
  name: string;
  skillMd: SkillMdRef;
}) => {
  const { source, subpath, tree } = skillMd;
  const version = `${subpath}\u0000${tree}`;
  const read = useAppStore((s) => s.skillMds[cardKey]);
  useEffect(() => {
    void loadSkillMd(cardKey, { source, subpath, tree });
  }, [cardKey, source, subpath, tree]);

  const current =
    read !== undefined && read.version === version ? read : undefined;
  if (current === undefined) {
    return <PaneHint>{`Reading ${SKILL_MD}…`}</PaneHint>;
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
 * What an open row shows: the full description, why a skill that cannot be selected cannot be,
 * and its SKILL.md. One of your skills on the account remote has no SKILL.md to read here.
 */
const Details = ({ card }: { card: SkillCard }) => (
  <>
    {(card.description !== "" || card.title !== undefined) && (
      <div className="px-file-row flex flex-col gap-2 pb-3.5">
        {card.description !== "" && (
          <p className="type-body text-text-muted m-0 text-pretty">
            {card.description}
          </p>
        )}
        {card.title !== undefined && (
          <p className="type-secondary text-text-hint m-0 text-pretty">
            {card.title}
          </p>
        )}
      </div>
    )}
    {card.skillMd !== undefined && (
      <SkillMd cardKey={card.key} name={card.name} skillMd={card.skillMd} />
    )}
  </>
);

/**
 * One skill as a card of its own: the row that opens it (the chevron, its name, one line of its
 * description while closed) and, at its end, the round check that selects it, or for a skill this
 * machine holds already its status. The row is named by the skill alone; its status and
 * description are read as its description.
 */
const SkillItem = ({
  card,
  open,
  selected,
  onToggle,
}: {
  card: SkillCard;
  open: boolean;
  selected: boolean;
  onToggle: (card: SkillCard) => void;
}) => {
  const descriptionId = useId();
  const status =
    card.status === undefined ? undefined : statusLabels[card.status];
  const description = [status, card.description]
    .filter((part) => part !== undefined && part !== "")
    .join(". ");
  return (
    <AccordionItem variant="list" value={card.key} selected={selected}>
      <AccordionTrigger
        variant="list"
        label={card.name}
        description={description === "" ? undefined : descriptionId}
        open={open}
        meta={
          status === undefined ? undefined : (
            <span className={statusVariants({ status: card.status })}>
              {status}
            </span>
          )
        }
        action={
          card.status === undefined ? (
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
          ) : undefined
        }
      >
        <span className="type-row-name text-text-primary min-w-0 flex-initial truncate">
          {card.name}
        </span>
        {/* The details show the description in full while the row is open. */}
        <span className="type-row-desc text-text-muted min-w-0 flex-1 truncate">
          {open ? "" : card.description}
        </span>
        {description !== "" && (
          <span id={descriptionId} className="sr-only">
            {description}
          </span>
        )}
      </AccordionTrigger>
      <AccordionContent>
        <Details card={card} />
      </AccordionContent>
    </AccordionItem>
  );
};

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
    const observeSiblings = () => {
      sizes.disconnect();
      for (const child of parent?.children ?? []) {
        if (child !== element) {
          sizes.observe(child);
        }
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
  const [open, setOpen] = useState<string[]>([]);
  const list = useRef<HTMLDivElement>(null);
  const offset = useOffsetIn(list, scroller);
  const items = itemsOf(groups, captions);
  // The same function on every render, which calls the latest `onToggle`.
  const latestToggle = useRef(onToggle);
  useEffect(() => {
    latestToggle.current = onToggle;
  }, [onToggle]);
  const toggle = useCallback((card: SkillCard) => {
    latestToggle.current(card);
  }, []);
  // The virtualizer's functions return new values while they stay the same functions, which
  // React Compiler cannot memoize. The app is built without it, and this component reads them
  // on every render and passes none of them to a memoized component.
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
            selected={selected.has(item.card.key)}
            onToggle={toggle}
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
        onValueChange={setOpen}
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
    </div>
  );
};
