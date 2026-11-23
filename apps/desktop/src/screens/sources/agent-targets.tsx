// The install targets of a `skill add` (DESIGN.md §6 "Agent picker"): the Universal stack, locked
// on, then "Add to" and round logo toggles, at most five, the rest behind a "+n" button that opens
// the agent picker. The Add skills bar of Sources and the Add skill dialog show it; `useAgentTargets` holds the choice, which starts as the
// enabled configurations, where `skill add` installs when no agent is named.
import { useState } from "react";

import type { Configuration } from "#/cli/events.gen.ts";
import { AgentLogo, LogoStack } from "#/components/agent-logo.tsx";
import { LockMark } from "#/components/check-mark.tsx";
import { Badge } from "#/components/ui/badge.tsx";
import { Button } from "#/components/ui/button.tsx";
import { Toggle } from "#/components/ui/toggle.tsx";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "#/components/ui/tooltip.tsx";
import { andList } from "#/lib/plural.ts";
import { AgentPickerDialog } from "#/screens/sources/agent-picker-dialog.tsx";
import {
  directConfigurations,
  universalConfigurations,
} from "#/screens/sources/projection.ts";
import { useAppStore } from "#/store/store.ts";

/** How many target logos show before the rest go behind the "+n" button. */
const SHOWN_TARGETS = 5;

const noConfigurations: Configuration[] = [];

/**
 * The install targets the user chose, from the inventory's configurations: the enabled ones
 * that take a placement until the user changes them. `direct` are the configurations that take a
 * placement, `universal` those whose client reads the library, `chosen` the ids of the targets in
 * inventory order, and `enabled` the ids of the enabled ones, for `toFlags`.
 */
export const useAgentTargets = () => {
  const configurations =
    useAppStore((s) => s.inventory?.configurations) ?? noConfigurations;
  const [targets, setTargets] = useState<string[] | undefined>();
  const direct = directConfigurations(configurations);
  const universal = universalConfigurations(configurations);
  const enabled = direct.filter((c) => c.enabled).map((c) => c.id);
  const chosen = direct
    .map((c) => c.id)
    .filter((id) => (targets ?? enabled).includes(id));
  const toggle = (id: string) => {
    setTargets(
      chosen.includes(id) ? chosen.filter((t) => t !== id) : [...chosen, id]
    );
  };
  const clear = () => {
    setTargets([]);
  };
  return { chosen, clear, direct, enabled, toggle, universal };
};

/** The ids of `direct`, the chosen ones first, each group in inventory order. */
const targetOrder = (direct: Configuration[], targets: string[]) => [
  ...direct.filter((c) => targets.includes(c.id)).map((c) => c.id),
  ...direct.filter((c) => !targets.includes(c.id)).map((c) => c.id),
];

/** What the Universal stack means, in its tooltip. */
const universalHint = "Skills are always added to universal agents";

/**
 * The Universal stack: the clients that read the library, so every skill reaches them. Always
 * on and never a toggle: the chosen ring and a lock, then a divider before "Add to". A screen
 * reader hears the tooltip's text and the clients' names.
 */
const UniversalMark = ({ universal }: { universal: Configuration[] }) => (
  <>
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="rounded-pill ring-accent-primary ring-offset-overlay-bg relative flex flex-none ring-2 ring-offset-2">
          <LogoStack clients={universal} size={28} tooltip={false} decorative />
          <span className="rounded-checkbox ring-overlay-bg absolute -right-1.5 -bottom-1.5 flex ring-2">
            <LockMark />
          </span>
          <span className="sr-only">{`${universalHint}: ${andList(
            universal.map((c) => c.name)
          )}`}</span>
        </span>
      </TooltipTrigger>
      <TooltipContent>{universalHint}</TooltipContent>
    </Tooltip>
    <span aria-hidden className="bg-shell-divider h-6 w-px flex-none" />
  </>
);

/**
 * The configurations shown as logos, and the rest, in `order`. A configuration the order does not
 * know yet (a new inventory) goes last. When one more would fit in place of the "+n" button, all
 * of them show.
 */
const splitTargets = (direct: Configuration[], order: string[]) => {
  const rank = (c: Configuration) => {
    const i = order.indexOf(c.id);
    return i === -1 ? order.length : i;
  };
  const ordered = [...direct];
  ordered.sort((a, b) => rank(a) - rank(b));
  return ordered.length <= SHOWN_TARGETS + 1
    ? { hidden: [], shown: ordered }
    : {
        hidden: ordered.slice(SHOWN_TARGETS),
        shown: ordered.slice(0, SHOWN_TARGETS),
      };
};

/** "16 more agents", and how many of them are chosen. */
const moreLabel = (hidden: number, chosen: number) =>
  chosen === 0
    ? `${hidden} more agents`
    : `${hidden} more agents, ${chosen} chosen`;

/**
 * The Universal stack, then "Add to" and the targets as logo toggles, laid out by the parent's
 * flex row. The order is fixed when it mounts and when the agent picker closes, so a logo stays in
 * place while it is toggled.
 */
export const AgentTargets = ({
  skills,
  direct,
  universal,
  targets,
  onToggleTarget,
  onClearTargets,
  disabled,
}: {
  /** How many skills the install adds, for the agent picker's subtitle. */
  skills: number;
  /** The configurations that take a placement, in inventory order. */
  direct: Configuration[];
  /** The configurations whose client reads the library. */
  universal: Configuration[];
  /** The ids of the chosen targets. */
  targets: string[];
  onToggleTarget: (id: string) => void;
  onClearTargets: () => void;
  /** The install runs: the choice is fixed. */
  disabled: boolean;
}) => {
  const [order, setOrder] = useState(() => targetOrder(direct, targets));
  const [picking, setPicking] = useState(false);
  const { shown, hidden } = splitTargets(direct, order);
  const logoToggle = (c: Configuration) => (
    <Tooltip key={c.id}>
      <TooltipTrigger asChild>
        <Toggle
          kind="logo"
          pressed={targets.includes(c.id)}
          disabled={disabled}
          onPressedChange={() => {
            onToggleTarget(c.id);
          }}
          aria-label={c.name}
        >
          <AgentLogo client={c} size={28} tooltip={false} decorative />
        </Toggle>
      </TooltipTrigger>
      <TooltipContent>{c.name}</TooltipContent>
    </Tooltip>
  );
  const hiddenChosen = hidden.filter((c) => targets.includes(c.id)).length;
  return (
    <>
      {universal.length > 0 && <UniversalMark universal={universal} />}
      <span className="type-secondary text-text-muted whitespace-nowrap">
        Add to
      </span>
      <fieldset className="m-0 flex items-center gap-2 border-0 p-0">
        <legend className="sr-only">Add to</legend>
        {shown.map(logoToggle)}
        {hidden.length > 0 && (
          <Button
            variant="secondary"
            size="sm"
            disabled={disabled}
            aria-haspopup="dialog"
            aria-label={moreLabel(hidden.length, hiddenChosen)}
            onClick={() => {
              setPicking(true);
            }}
          >
            {`+${hidden.length}`}
            {hiddenChosen > 0 && (
              <Badge tone="picker-count" size="picker-count">
                {hiddenChosen}
              </Badge>
            )}
          </Button>
        )}
      </fieldset>
      <AgentPickerDialog
        open={picking}
        onOpenChange={(open) => {
          setPicking(open);
          if (!open) {
            setOrder(targetOrder(direct, targets));
          }
        }}
        skills={skills}
        configurations={direct}
        universal={universal}
        targets={targets}
        onToggleTarget={onToggleTarget}
        onClearTargets={onClearTargets}
      />
    </>
  );
};
