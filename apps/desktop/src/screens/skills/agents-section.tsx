// AGENTS in the detail pane: one toggle chip per configuration that places skills in its own
// directory, then Universal for the clients that read the library. A chip runs one command and
// shows pending until the command ends and the next inventory arrives; the inventory then says
// what the chip shows. A locked or displaced chip runs nothing and toasts why.
//
// The chips that are on come first, then the rest by the popularity of the agent client. The
// order is fixed when the pane opens, so a chip stays where it is when it is toggled. Collapsed,
// the section shows the chips that fit in two rows, and every chip that is on.
import { useCallback, useState } from "react";
import type { ReactNode } from "react";

import type { Configuration } from "#/cli/events.gen.ts";
import { AgentChip } from "#/components/agent-chip.tsx";
import { AgentLogo, LogoStack } from "#/components/agent-logo.tsx";
import { notify } from "#/components/toast.tsx";
import { Button } from "#/components/ui/button.tsx";
import { popularityOf } from "#/lib/agent-popularity.ts";
import {
  chipLabel,
  chipStateOf,
  chipTitle,
  DISPLACED_REASON,
  lockReason,
  universalTitle,
} from "#/screens/skills/placements.ts";
import { UNIVERSAL } from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { runUntilSettled, useSettling } from "#/store/settle.ts";

/**
 * Adds or removes the skill's placement in `c`, with an Undo that does the opposite. Off: `skill
 * place <name> --to <id>`; on: `skill remove <name> --from <id>`.
 */
const toggle = (row: SkillRow, c: Configuration, add: boolean) => {
  void runUntilSettled(
    add
      ? ["skill", "place", row.name, "--to", c.id]
      : ["skill", "remove", row.name, "--from", c.id],
    {
      action: {
        label: "Undo",
        onClick: () => {
          toggle(row, c, !add);
        },
      },
      label: chipLabel(row, c),
      success: add
        ? `Added ${row.name} to ${c.name}`
        : `Removed ${row.name} from ${c.name}`,
    }
  );
};

const DirectChip = ({ row, c }: { row: SkillRow; c: Configuration }) => {
  const state = chipStateOf(row, c);
  const locked = lockReason(row);
  const pending = useSettling(chipLabel(row, c));
  return (
    <AgentChip
      logo={<AgentLogo client={c} size={28} />}
      name={c.name}
      state={state}
      title={chipTitle(row, c, state)}
      locked={locked !== undefined}
      pending={pending}
      onClick={() => {
        if (locked !== undefined) {
          notify(locked);
        } else if (state === "displaced") {
          notify(DISPLACED_REASON);
        } else if (!pending) {
          toggle(row, c, state === "off");
        }
      }}
    />
  );
};

/** The rows of chips the section shows while collapsed. */
const COLLAPSED_ROWS = 2;

/** One chip of the section: a configuration's, or Universal's. */
interface Entry {
  /** The configuration id, or UNIVERSAL. */
  id: string;
  name: string;
  on: boolean;
  /** The popularity of its agent client: 0 is the most used. */
  rank: number;
  chip: ReactNode;
}

/** How many rows items this wide take when they wrap in `width`, `gap` apart. */
const rowsOf = (widths: number[], width: number, gap: number) => {
  let rows = 0;
  let used = 0;
  for (const w of widths) {
    if (rows > 0 && used + gap + w <= width) {
      used += gap + w;
    } else {
      rows += 1;
      used = w;
    }
  }
  return rows;
};

/** What the list measures of itself: its width, the gap between chips, and each item's width. */
interface Sizes {
  width: number;
  gap: number;
  chips: number[];
  button: number;
}

/**
 * The sizes of `list`. Its last child is the button; a chip that is not shown is still laid out,
 * out of the flow, so it has its width. Widths round up, so the count never promises a chip a
 * row has no room for.
 */
const sizesOf = (list: HTMLElement): Sizes => {
  const chips = [...list.children].map((child) =>
    Math.ceil(child.getBoundingClientRect().width)
  );
  const button = chips.pop() ?? 0;
  return {
    button,
    chips,
    gap: Number(getComputedStyle(list).columnGap.replace("px", "")) || 0,
    width: list.clientWidth,
  };
};

const sameSizes = (a: Sizes | null, b: Sizes) =>
  a !== null &&
  a.width === b.width &&
  a.gap === b.gap &&
  a.button === b.button &&
  a.chips.length === b.chips.length &&
  a.chips.every((w, i) => w === b.chips[i]);

/**
 * How many of the leading chips the collapsed rows hold, with the chips that are `kept` in view
 * whatever their place and the button after them; null when every chip fits without the button,
 * or nothing is measured yet.
 */
const fitOf = (sizes: Sizes | null, kept: boolean[]): number | null => {
  if (sizes === null || sizes.width === 0) {
    return null;
  }
  const { button, chips, gap, width } = sizes;
  if (rowsOf(chips, width, gap) <= COLLAPSED_ROWS) {
    return null;
  }
  let fit = chips.length - 1;
  while (
    fit > 0 &&
    rowsOf(
      [...chips.filter((_, i) => i < fit || kept[i]), button],
      width,
      gap
    ) > COLLAPSED_ROWS
  ) {
    fit -= 1;
  }
  return fit;
};

export const AgentsSection = ({
  row,
  configurations,
  onRemove,
}: {
  row: SkillRow;
  configurations: Configuration[];
  /** Opens the Remove dialog: the Universal chip, on, removes the skill from the machine. */
  onRemove: () => void;
}) => {
  const direct = configurations.filter((c) => !c.reads_library);
  const universal = configurations.filter((c) => c.reads_library);
  const locked = lockReason(row);

  const entries: Entry[] = direct.map((c) => ({
    chip: <DirectChip row={row} c={c} />,
    id: c.id,
    name: c.name,
    on: chipStateOf(row, c) !== "off",
    rank: popularityOf(c.id),
  }));
  if (universal.length > 0) {
    entries.push({
      chip: (
        <AgentChip
          logo={<LogoStack clients={universal} size={28} />}
          name="Universal"
          state={row.universal ? "on" : "off"}
          title={universalTitle(row)}
          locked={locked !== undefined || !row.universal}
          onClick={() => {
            if (locked !== undefined) {
              notify(locked);
            } else if (row.universal) {
              onRemove();
            }
          }}
        />
      ),
      id: UNIVERSAL,
      name: "Universal",
      on: row.universal,
      // Universal stands where its most used client would.
      rank: Math.min(...universal.map((c) => popularityOf(c.id))),
    });
  }
  entries.sort((a, b) => a.rank - b.rank || a.name.localeCompare(b.name));

  // The order of the pane as it opened: on first. A configuration found since then goes last.
  const [opened, setOpened] = useState(() =>
    [...entries.filter((e) => e.on), ...entries.filter((e) => !e.on)].map(
      (e) => e.id
    )
  );
  const found = entries.filter((e) => !opened.includes(e.id));
  if (found.length > 0) {
    setOpened([...opened, ...found.map((e) => e.id)]);
  }
  entries.sort((a, b) => opened.indexOf(a.id) - opened.indexOf(b.id));

  // Every chip that has been on since the pane opened stays in view while collapsed, so a chip
  // turned off there does not vanish under the pointer.
  const [kept, setKept] = useState(
    () => new Set(entries.filter((e) => e.on).map((e) => e.id))
  );
  if (entries.some((e) => e.on && !kept.has(e.id))) {
    setKept(
      new Set([...kept, ...entries.filter((e) => e.on).map((e) => e.id)])
    );
  }

  const [expanded, setExpanded] = useState(false);
  const [sizes, setSizes] = useState<Sizes | null>(null);
  // Measures when the list mounts, before it is painted, then whenever the pane, a chip or the
  // button changes size: a resized window, a font that loads, the button's label.
  const measured = useCallback((list: HTMLDivElement) => {
    const measure = () => {
      const next = sizesOf(list);
      setSizes((last) => (sameSizes(last, next) ? last : next));
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(list);
    for (const child of list.children) {
      observer.observe(child);
    }
    return () => {
      observer.disconnect();
    };
  }, []);
  const fit = fitOf(
    sizes,
    entries.map((e) => kept.has(e.id))
  );

  const shown = (e: Entry, i: number) =>
    expanded || fit === null || i < fit || kept.has(e.id);
  const hidden = entries.filter((e, i) => !shown(e, i)).length;
  // Nothing to show or hide: every chip fits, or the chips that are on take the rows themselves.
  const collapsible = fit !== null && (expanded || hidden > 0);

  return (
    <div ref={measured} className="gap-agent-chip-wrap relative flex flex-wrap">
      {entries.map((e, i) => (
        <div
          key={e.id}
          className={
            shown(e, i) ? "flex min-w-0" : "invisible absolute top-0 left-0"
          }
        >
          {e.chip}
        </div>
      ))}
      <div
        className={
          collapsible
            ? "flex items-center"
            : "invisible absolute top-0 left-0 flex"
        }
      >
        <Button
          variant="ghost"
          size="sm"
          aria-expanded={expanded}
          onClick={() => {
            setExpanded(!expanded);
          }}
        >
          {expanded ? "Show fewer" : `Show ${hidden || entries.length} more`}
        </Button>
      </div>
    </div>
  );
};
