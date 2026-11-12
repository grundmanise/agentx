// The Agents page (DESIGN.md §6 "Agent client card" and "Universal card"): every agent
// configuration the scan detected, with its counts. A card's switch decides only where later
// installs and creations go when no agent is named: `config enable <id>` and `config disable
// <id>`, with Place all (`config enable <id> --place-all`) offered on the toast. The clients that
// read the library share the Universal card and have no switch: they see the library whatever
// their setting. Remove, in the menu of a card and of a Universal column, deletes the
// configuration's directory after one confirm dialog (`config remove <id> --yes`): the user
// decides what goes, and the cards follow the inventory that comes after the command.
import { useState } from "react";

import type {
  CliEvent,
  Configuration,
  InventoryEvent,
} from "#/cli/events.gen.ts";
import { AgentLogo, LogoStack } from "#/components/agent-logo.tsx";
import { MoreIcon } from "#/components/icons/more.tsx";
import { Spinner } from "#/components/spinner.tsx";
import { Button } from "#/components/ui/button.tsx";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "#/components/ui/dropdown-menu.tsx";
import { Switch } from "#/components/ui/switch.tsx";
import { againRule, withAppHint } from "#/lib/app-hint.ts";
import { plural } from "#/lib/plural.ts";
import {
  countsOf,
  libraryPathOf,
  unplacedCount,
} from "#/screens/agents/counts.ts";
import { Confirm } from "#/screens/settings/confirm.tsx";
import { placedAllText } from "#/screens/skills/placements.ts";
import type { Outcome } from "#/store/runner.ts";
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

/** The label of a configuration's Remove: the runner's name for it. */
const removeLabel = (c: Configuration) => `remove ${c.id}`;

/**
 * "Removed Antigravity", or "Removed Antigravity and Antigravity CLI" when the deleted directory
 * held another configuration: the names the CLI says it removed.
 */
const removedText = (events: CliEvent[]) => {
  const names = events
    .filter((e) => e.type === "config_removal")
    .map((e) => e.name);
  return `Removed ${names.join(", ").replace(/, (?=[^,]*$)/u, " and ")}`;
};

/** A failed removal in the app's words: the CLI's message, and a hint that names Remove. */
const removeFailure = ({ events }: Outcome) => {
  const error = events.find((e) => e.type === "error");
  return error === undefined
    ? undefined
    : withAppHint(
        error.hint === undefined
          ? { message: error.message }
          : { hint: error.hint, message: error.message },
        [againRule("remove it again")]
      );
};

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
            aria-label={`More actions for ${c.name}`}
            disabled={pending}
          >
            <MoreIcon />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent>
          <DropdownMenuItem
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
        onConfirm={() => {
          setConfirming(false);
          remove(c);
        }}
      />
    </>
  );
};

const AgentCard = ({
  c,
  inventory,
}: {
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
      className="rounded-agent-card border-card-border bg-card-bg p-agent-card flex min-w-0 flex-col gap-4 border"
    >
      <div className="flex items-start justify-between gap-3">
        <AgentLogo client={c} size={44} decorative />
        <span className="flex items-center gap-2">
          {pending && (
            <span className="text-text-hint flex">
              <Spinner />
            </span>
          )}
          <Switch
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
      </div>
      <div className="flex min-w-0 flex-col gap-1">
        <h2 className="type-agent-card-name text-card-title m-0">{c.name}</h2>
        <span
          className="type-technical text-text-caption truncate"
          title={c.path}
        >
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
    </article>
  );
};

const UniversalCard = ({
  universal,
  inventory,
}: {
  universal: Configuration[];
  inventory: InventoryEvent;
}) => {
  const library = libraryPathOf(inventory);
  return (
    <article
      aria-label="Universal"
      className="rounded-agent-card border-card-border bg-card-bg p-agent-card flex border"
    >
      <div className="w-universal-lead flex flex-none flex-col gap-3">
        <LogoStack clients={universal} size={44} surface="card" decorative />
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
      {universal.map((c) => {
        const counts = countsOf(inventory, c);
        return (
          <section
            key={c.id}
            aria-label={c.name}
            className="border-shell-divider ml-5 flex min-w-0 flex-1 flex-col gap-1 border-l pl-5"
          >
            <div className="flex items-center justify-between gap-3">
              <h3 className="type-agent-card-name text-card-title m-0">
                {c.name}
              </h3>
              <AgentMenu c={c} />
            </div>
            <span
              className="type-technical text-text-caption truncate"
              title={c.path}
            >
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
    </article>
  );
};

export const AgentsScreen = () => {
  const inventory = useAppStore((s) => s.inventory);
  const configurations = inventory?.configurations ?? [];
  const direct = configurations.filter((c) => !c.reads_library);
  const universal = configurations.filter((c) => c.reads_library);
  return (
    <div className="pt-page-top px-page pb-page gap-page-title flex h-full flex-col overflow-auto">
      <h1 className="type-page-title text-text-primary m-0">Agents</h1>
      {inventory !== undefined && (
        <div className="flex flex-col gap-7">
          {direct.length > 0 && (
            <section
              aria-label="Direct agents"
              className="gap-agent-card grid grid-cols-4"
            >
              {direct.map((c) => (
                <AgentCard key={c.id} c={c} inventory={inventory} />
              ))}
            </section>
          )}
          {universal.length > 0 && (
            <UniversalCard universal={universal} inventory={inventory} />
          )}
          {configurations.length === 0 && (
            <p className="type-secondary text-text-hint m-0">
              No agent configurations detected on this machine
            </p>
          )}
        </div>
      )}
    </div>
  );
};
