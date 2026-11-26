// The Agents page (DESIGN.md §6 "Agent client card" and "Universal card"): every agent
// configuration the scan detected, with its counts. A card's switch decides only where later
// installs and creations go when no agent is named: `config enable <id>` and `config disable
// <id>`, with Place all (`config enable <id> --place-all`) offered on the toast. The clients that
// read the library share the Universal card and have no switch: they see the library whatever
// their setting. A card's Placement control decides how later placements there are made, as a
// symlink to the library or as a copy (`config placement <id> symlink|copy`); skills already placed
// keep their kind, unless Convert on the toast converts them (`--convert`). Remove, in the menu of a card and of a Universal column, deletes the
// configuration's directory after one confirm dialog (`config remove <id> --yes`): the user
// decides what goes, and the cards follow the inventory that comes after the command. A click on a
// card, or a Universal column, selects it; the floating bar then enables, disables or removes
// every selected agent at once.
import { useId, useState } from "react";

import type { Configuration, InventoryEvent } from "#/cli/events.gen.ts";
import { AgentLogo, LogoStack } from "#/components/agent-logo.tsx";
import { Segmented } from "#/components/filter-controls.tsx";
import { MoreIcon } from "#/components/icons/more.tsx";
import { Button } from "#/components/ui/button.tsx";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "#/components/ui/dropdown-menu.tsx";
import { Spinner } from "#/components/ui/spinner.tsx";
import { Switch } from "#/components/ui/switch.tsx";
import { Toggle } from "#/components/ui/toggle.tsx";
import { plural } from "#/lib/plural.ts";
import { BulkBar } from "#/screens/agents/bulk-bar.tsx";
import {
  countsOf,
  libraryPathOf,
  placedAsCount,
  unplacedCount,
} from "#/screens/agents/counts.ts";
import { removedText, removeFailure } from "#/screens/agents/remove.ts";
import { Confirm } from "#/screens/settings/confirm.tsx";
import { convertedText, placedAllText } from "#/screens/skills/placements.ts";
import { runUntilSettled, useSettling } from "#/store/settle.ts";
import { useAppStore } from "#/store/store.ts";

/** "{n} skills", the number in `text.primary`; the whole reads as one text. */
const Count = ({ n, noun }: { n: number; noun: string }) => (
  <span>
    <span className="text-text-primary font-medium">{n}</span>
    {plural(n, noun).slice(String(n).length)}
  </span>
);

/** The label of a configuration's switch: the runner's name for it. */
const switchLabel = (c: Configuration) => `enable ${c.id}`;

const placeAll = (c: Configuration) => {
  void runUntilSettled(["config", "enable", c.id, "--place-all"], {
    label: switchLabel(c),
    success: (events) => placedAllText(events, c.name),
  });
};

/** The label of a configuration's Placement control: the runner's name for it. */
const placementLabel = (c: Configuration) => `placement ${c.id}`;

type Placement = "symlink" | "copy";

const placements: { value: Placement; label: string }[] = [
  { label: "Symlink", value: "symlink" },
  { label: "Copy", value: "copy" },
];

/** What a card's Placement control decides, on hover. */
const placementTitle = (c: Configuration) =>
  c.placement === "library"
    ? `${c.name}'s skills folder links to your library, so it sees every library skill.`
    : `How new skills reach ${c.name}. Symlink: ${c.name} reads them from your shared library, so updates show at once. Copy: ${c.name} gets its own copy, for an agent client that doesn't follow symlinks. Skills already there stay as they are until you convert them.`;

/**
 * Converts the skills already in `c` to `mode`: `config placement <id> <mode> --convert`. The CLI
 * keeps a copy edited in place, and the toast says how many it kept.
 */
const convert = (c: Configuration, mode: Placement) => {
  void runUntilSettled(["config", "placement", c.id, mode, "--convert"], {
    label: placementLabel(c),
    success: (events) => convertedText(events, c.name, mode),
  });
};

/**
 * How later placements in the configuration are made: a symlink to the library (the default) or
 * a copy. The control shows the inventory's `placement` and is pending until the inventory that
 * follows the command arrives.
 */
const PlacementControl = ({
  c,
  inventory,
  disabled,
}: {
  c: Configuration;
  inventory: InventoryEvent;
  disabled: boolean;
}) => {
  const id = useId();
  const pending = useSettling(placementLabel(c));
  const value: Placement = c.placement === "copy" ? "copy" : "symlink";
  const onChange = (mode: Placement) => {
    // The skills already there as the other kind, which the toast offers to convert.
    const n = placedAsCount(inventory, c.id, value);
    void runUntilSettled(["config", "placement", c.id, mode], {
      action:
        n > 0
          ? {
              label: "Convert existing",
              onClick: () => {
                convert(c, mode);
              },
              track: "placement-toast.convert",
            }
          : undefined,
      label: placementLabel(c),
      success:
        mode === "copy"
          ? `${c.name} now gets copies of new skills`
          : `${c.name} now gets symlinks to new skills`,
    });
  };
  return (
    <div className="relative mt-auto flex flex-col gap-2">
      <span className="flex items-center justify-between gap-2">
        <span id={id} className="type-caption text-text-caption">
          Placement
        </span>
        {pending && (
          <span className="text-text-hint flex">
            <Spinner />
          </span>
        )}
      </span>
      <Segmented
        track="agents.card-placement"
        options={placements}
        value={value}
        onChange={onChange}
        labelledBy={id}
        title={placementTitle(c)}
        surface="card"
        disabled={disabled || c.placement === "library"}
        busy={pending}
      />
    </div>
  );
};

/** The label of a configuration's Remove: the runner's name for it. */
const removeLabel = (c: Configuration) => `remove ${c.id}`;

const remove = (c: Configuration) => {
  void runUntilSettled(["config", "remove", c.id, "--yes"], {
    failure: removeFailure,
    label: removeLabel(c),
    success: removedText,
  });
};

/**
 * The menu of a configuration's other actions: Remove, behind a dialog that names the directory
 * that is deleted.
 */
const AgentMenu = ({ c }: { c: Configuration }) => {
  const [confirming, setConfirming] = useState(false);
  const pending = useSettling(removeLabel(c));
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="icon"
            track="agents.card-menu"
            aria-label={`More actions for ${c.name}`}
            disabled={pending}
          >
            <MoreIcon />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent>
          <DropdownMenuItem
            track="agents.card-remove"
            onSelect={() => {
              setConfirming(true);
            }}
          >
            <span className="type-body text-text-primary">Remove</span>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <Confirm
        open={confirming}
        onOpenChange={setConfirming}
        title={`Remove ${c.name}?`}
        description="Its configuration directory is deleted from this machine, with everything in it."
        detail={c.path}
        action="Remove"
        track="remove-agent-dialog.confirm"
        cancelTrack="remove-agent-dialog.cancel"
        onConfirm={() => {
          setConfirming(false);
          remove(c);
        }}
      />
    </>
  );
};

/** How an agent is selected: by a click on its card, the hit area of a toggle with no mark. */
interface Selectable {
  selected: boolean;
  onToggleSelect: () => void;
}

const AgentCard = ({
  c,
  inventory,
  ...selection
}: Selectable & {
  c: Configuration;
  inventory: InventoryEvent;
}) => {
  const counts = countsOf(inventory, c);
  const pending = useSettling(switchLabel(c));
  const onToggle = (checked: boolean) => {
    if (checked) {
      const n = unplacedCount(inventory, c.id);
      void runUntilSettled(["config", "enable", c.id], {
        action:
          n > 0
            ? {
                label: `Place all ${n}`,
                onClick: () => {
                  placeAll(c);
                },
                track: "enable-toast.place-all",
              }
            : undefined,
        label: switchLabel(c),
        success: `${c.name} enabled`,
      });
    } else {
      void runUntilSettled(["config", "disable", c.id], {
        label: switchLabel(c),
        success: `${c.name} disabled. New skills won't go there by default.`,
      });
    }
  };
  return (
    <article
      aria-label={c.name}
      data-selected={selection.selected ? "" : undefined}
      className="rounded-agent-card border-card-border bg-card-bg p-agent-card hover:border-agent-card-hover-border data-selected:border-agent-card-selected-border data-selected:bg-agent-card-selected-bg relative flex min-w-0 flex-col gap-4 border"
    >
      <Toggle
        kind="agent-card"
        track="agents.card-select"
        pressed={selection.selected}
        onPressedChange={() => {
          selection.onToggleSelect();
        }}
        aria-label={`Select ${c.name}`}
      />
      <div className="flex items-start justify-between gap-3">
        <AgentLogo client={c} size={44} decorative />
        <span className="flex items-center gap-2">
          <span className="relative flex items-center gap-2">
            {pending && (
              <span className="text-text-hint flex">
                <Spinner />
              </span>
            )}
            <Switch
              track="agents.card-enable"
              aria-label="Enabled"
              title={
                c.enabled
                  ? "Enabled · new skills go here by default"
                  : "Disabled · skipped by default"
              }
              checked={c.enabled}
              disabled={pending}
              aria-busy={pending ? true : undefined}
              onCheckedChange={onToggle}
            />
            <AgentMenu c={c} />
          </span>
        </span>
      </div>
      <div className="flex min-w-0 flex-col gap-1">
        <h2 className="type-agent-card-name text-card-title m-0">{c.name}</h2>
        <span className="type-technical text-text-caption truncate">
          {c.path}
        </span>
      </div>
      <p className="type-secondary text-card-text m-0">
        <Count n={counts.skills} noun="skill" />
        {" · "}
        <Count n={counts.servers} noun="server" />
        {" · "}
        <Count n={counts.plugins} noun="plugin" />
      </p>
      <PlacementControl c={c} inventory={inventory} disabled={pending} />
    </article>
  );
};

/** The Universal card's logo stack shows this many logos, then "+n". */
const STACK_MAX = 5;

const UniversalCard = ({
  universal,
  inventory,
  selected,
  onToggleSelect,
}: {
  universal: Configuration[];
  inventory: InventoryEvent;
  /** The ids of the selected agents. */
  selected: ReadonlySet<string>;
  onToggleSelect: (c: Configuration) => void;
}) => {
  const library = libraryPathOf(inventory);
  return (
    <article
      aria-label="Universal"
      className="rounded-agent-card border-card-border bg-card-bg p-agent-card flex border"
    >
      <div className="w-universal-lead flex flex-none flex-col gap-3">
        <LogoStack
          clients={universal.slice(0, STACK_MAX)}
          hidden={universal.slice(STACK_MAX)}
          size={44}
          surface="card"
          decorative
        />
        <div className="flex flex-col gap-1">
          <h2 className="type-agent-card-name text-card-title m-0">
            Universal
          </h2>
          <span className="type-secondary text-card-text">
            Read your shared library
          </span>
          {library !== undefined && (
            <span
              className="type-technical text-text-caption truncate"
              title={library}
            >
              {library}
            </span>
          )}
        </div>
      </div>
      <div className="grid-cols-universal grid min-w-0 flex-1 gap-y-5">
        {universal.map((c) => {
          const counts = countsOf(inventory, c);
          return (
            <section
              key={c.id}
              aria-label={c.name}
              className="border-shell-divider relative ml-5 flex min-w-0 flex-col gap-1 border-l pl-5"
            >
              <Toggle
                kind="agent-column"
                track="agents.card-select"
                pressed={selected.has(c.id)}
                onPressedChange={() => {
                  onToggleSelect(c);
                }}
                aria-label={`Select ${c.name}`}
              />
              <div className="flex items-center justify-between gap-3">
                <h3 className="type-agent-card-name text-card-title m-0">
                  {c.name}
                </h3>
                <span className="relative flex">
                  <AgentMenu c={c} />
                </span>
              </div>
              <span className="type-technical text-text-caption truncate">
                {c.path}
              </span>
              <p className="type-secondary text-card-text m-0 pt-1">
                <Count n={inventory.library.length} noun="skill" />
                {" · "}
                <Count n={counts.servers} noun="server" />
              </p>
            </section>
          );
        })}
      </div>
    </article>
  );
};

const noIds: ReadonlySet<string> = new Set();

export const AgentsScreen = () => {
  const inventory = useAppStore((s) => s.inventory);
  const [ids, setIds] = useState(noIds);
  const configurations = inventory?.configurations ?? [];
  const direct = configurations.filter((c) => !c.reads_library);
  const universal = configurations.filter((c) => c.reads_library);
  // An agent the inventory no longer holds, once removed, is no longer selected.
  const chosen = configurations.filter((c) => ids.has(c.id));
  const selected = new Set(chosen.map((c) => c.id));
  const toggle = (c: Configuration) => {
    const next = new Set(selected);
    if (!next.delete(c.id)) {
      next.add(c.id);
    }
    setIds(next);
  };
  const clear = () => {
    setIds(noIds);
  };
  return (
    <div className="relative flex h-full min-h-0 flex-col">
      <div className="pt-page-top px-page pb-page gap-page-title flex min-h-0 flex-1 flex-col overflow-auto">
        <h1 className="type-page-title text-text-primary m-0">Agents</h1>
        {inventory !== undefined && (
          <div className="flex flex-col gap-7">
            {universal.length > 0 && (
              <UniversalCard
                universal={universal}
                inventory={inventory}
                selected={selected}
                onToggleSelect={toggle}
              />
            )}
            {direct.length > 0 && (
              <section
                aria-label="Direct agents"
                className="gap-agent-card grid grid-cols-4"
              >
                {direct.map((c) => (
                  <AgentCard
                    key={c.id}
                    c={c}
                    inventory={inventory}
                    selected={selected.has(c.id)}
                    onToggleSelect={() => {
                      toggle(c);
                    }}
                  />
                ))}
              </section>
            )}
            {configurations.length === 0 && (
              <p className="type-secondary text-text-hint m-0">
                No agent configurations detected on this machine
              </p>
            )}
          </div>
        )}
        {/* Room for the floating bar over the last row of cards. */}
        {chosen.length > 0 && <div className="h-floating-bar flex-none" />}
      </div>
      <BulkBar chosen={chosen} onClear={clear} />
    </div>
  );
};
