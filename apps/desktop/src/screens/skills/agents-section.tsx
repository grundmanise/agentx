// AGENTS in the detail pane: one toggle chip per configuration that places skills in its own
// directory, then Universal for the clients that read the library. A chip runs one command and
// shows pending until the command ends and the next inventory arrives; the inventory then says
// what the chip shows. A locked chip runs nothing and toasts why; the toast of an unmanaged
// skill offers Adopt when the CLI can adopt it.
//
// The chips that are on come first, then the rest by the popularity of the agent client. The
// order is fixed when the pane opens, so a chip stays where it is when it is toggled. Collapsed,
// the section shows the chips that fit in two rows, and every chip that is on. A displaced chip
// runs nothing: its switch is disabled, and its Tooltip says why.
import { useState } from "react";
import type { ReactNode } from "react";

import type { Configuration, LogEvent } from "#/cli/events.gen.ts";
import { AgentChip } from "#/components/agent-chip.tsx";
import { AgentLogo, LogoStack } from "#/components/agent-logo.tsx";
import { notify } from "#/components/toast.tsx";
import type { ToastAction } from "#/components/toast.tsx";
import { Button } from "#/components/ui/button.tsx";
import { popularityOf } from "#/lib/agent-popularity.ts";
import { adopt } from "#/screens/skills/actions.tsx";
import {
  chipLabel,
  chipStateOf,
  chipTitle,
  inertReason,
  lockOf,
  placedNowhere,
  universalTitle,
} from "#/screens/skills/placements.ts";
import type { Lock } from "#/screens/skills/placements.ts";
import { UNIVERSAL } from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { runUntilSettled, useSettling } from "#/store/settle.ts";

/**
 * Whether a `skill remove --from <id>` left the skill where `id` still sees it: a directory or a
 * link agentx did not place there, which the CLI leaves as it is with a warning that ends
 * `<id> still sees it` (docs/spec/cli-contract.md, "What a removal may delete").
 */
const stillSees = (logs: LogEvent[], id: string) =>
  logs.some(
    (log) =>
      log.level === "warn" && log.message.endsWith(`; ${id} still sees it`)
  );

/**
 * A warning of a placement or a removal that `id` was left out of, in the toast's shorter words:
 * the path and what it is, then "It was left as is." The CLI's own line goes on to say whether `id`
 * still sees the skill, which the toast's title already says.
 */
const shortWarning = (message: string, id: string): string => {
  const ending = [
    `; ${id} still sees it`,
    `; no placement was made for ${id}`,
  ].find((e) => message.endsWith(e));
  const left = " and was left as it is";
  const what = ending === undefined ? "" : message.slice(0, -ending.length);
  if (!what.endsWith(left)) {
    return message;
  }
  const short = what
    .slice(0, -left.length)
    .replace(" did not place there", " didn't place")
    .replace(/ is not this skill$/u, " has other files");
  return `${short}. It was left as is.`;
};

/**
 * Adds or removes the skill's placement in `c`, with an Undo that does the opposite. Off: `skill
 * place <name> --to <id>`; on: `skill remove <name> --from <id>`. A placement that was skipped,
 * or a removal that left `c` seeing the skill, says so, with the CLI's warning under it, and has no
 * Undo: there is nothing to undo.
 */
const toggle = (row: SkillRow, c: Configuration, add: boolean) => {
  const undo: ToastAction = {
    label: "Undo",
    onClick: () => {
      toggle(row, c, !add);
    },
    track: "skill-agents.undo",
  };
  void runUntilSettled(
    add
      ? ["skill", "place", row.name, "--to", c.id]
      : ["skill", "remove", row.name, "--from", c.id],
    {
      action: (events, logs) =>
        (add ? placedNowhere(events) : stillSees(logs, c.id))
          ? undefined
          : undo,
      label: chipLabel(row, c),
      success: (events, logs) => {
        if (add) {
          return placedNowhere(events)
            ? `${row.name} skill wasn't added to ${c.name}`
            : `Added ${row.name} skill to ${c.name}`;
        }
        return stillSees(logs, c.id)
          ? `${row.name} skill wasn't removed from ${c.name}`
          : `Removed ${row.name} skill from ${c.name}`;
      },
      warnings: (logs) => logs.map((log) => shortWarning(log.message, c.id)),
    }
  );
};

/** The action of a locked chip's toast: Adopt, which unlocks the chips of an unmanaged skill. */
const unlockAction = (row: SkillRow, lock: Lock): ToastAction | undefined =>
  lock.action === "adopt"
    ? {
        label: "Import",
        onClick: () => {
          void adopt(row);
        },
        track: "skill-agents.import",
      }
    : undefined;

/** Toasts why the chips of `row` are locked, with what unlocks them. */
const notifyLocked = (row: SkillRow, lock: Lock) => {
  notify(lock.reason, unlockAction(row, lock));
};

const DirectChip = ({ row, c }: { row: SkillRow; c: Configuration }) => {
  const state = chipStateOf(row, c);
  const locked = lockOf(row);
  const pending = useSettling(chipLabel(row, c));
  return (
    <AgentChip
      track="skill-agents.toggle-agent"
      logo={<AgentLogo client={c} size={28} tooltip={false} />}
      name={c.name}
      state={state}
      title={chipTitle(row, c, state)}
      inert={inertReason(row, c, state)}
      locked={locked !== undefined}
      pending={pending}
      onClick={() => {
        if (locked !== undefined) {
          notifyLocked(row, locked);
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
  const locked = lockOf(row);

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
          track="skill-agents.toggle-universal"
          logo={<LogoStack clients={universal} size={28} />}
          name="Universal"
          state={row.universal ? "on" : "off"}
          title={universalTitle(row)}
          locked={locked !== undefined || !row.universal}
          onClick={() => {
            if (locked !== undefined) {
              notifyLocked(row, locked);
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
  const measured = (list: HTMLDivElement) => {
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
  };
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
          track="skill-agents.show-more"
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
