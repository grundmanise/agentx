// The Agents page (DESIGN.md §6 "Agent client card" and "Universal card"): every agent
// configuration the scan detected, with its counts. A card's switch decides only where later
// installs and creations go when no agent is named: `config enable <id>` and `config disable
// <id>`, with Place all (`config enable <id> --place-all`) offered on the toast. The clients that
// read the library share the Universal card and have no switch: they see the library whatever
// their setting. A card's menu removes a leftover configuration, and Clean up removes many at once
// (`config remove <id>...`), both behind a dialog and only for what the CLI's preview lets go.
import { useEffect, useState } from "react";

import type { Configuration, InventoryEvent } from "#/cli/events.gen.ts";
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
import { plural } from "#/lib/plural.ts";
import {
  countsOf,
  libraryPathOf,
  unplacedCount,
} from "#/screens/agents/counts.ts";
import {
  loadRemovals,
  REMOVE_LABEL,
  useRemoval,
  useRemovalEvents,
} from "#/screens/agents/removal.ts";
import {
  CleanUpDialog,
  RemoveAgentDialog,
} from "#/screens/agents/remove-dialogs.tsx";
import { placedAllText } from "#/screens/skills/placements.ts";
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

/**
 * The menu of a configuration's other actions: Remove, which opens the dialog that confirms the
 * removal or says what the configuration still holds. Absent until the preview has judged it.
 */
const AgentMenu = ({ c }: { c: Configuration }) => {
  const removal = useRemoval(c.id);
  const [removing, setRemoving] = useState(false);
  if (removal === undefined) {
    return null;
  }
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="icon"
            aria-label={`More actions for ${c.name}`}
          >
            <MoreIcon />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent>
          <DropdownMenuItem
            onSelect={() => {
              void loadRemovals();
              setRemoving(true);
            }}
          >
            <span className="type-body text-text-primary">Remove</span>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <RemoveAgentDialog
        removal={removal}
        open={removing}
        onOpenChange={setRemoving}
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
  // The preview is read for the configurations detected: again when one appears or goes.
  const detected =
    inventory === undefined
      ? undefined
      : configurations.map((c) => c.id).join(" ");
  useEffect(() => {
    if (detected !== undefined) {
      void loadRemovals();
    }
  }, [detected]);
  const removable = useRemovalEvents().filter((e) => e.state === "removable");
  const removing = useSettling(REMOVE_LABEL);
  const [cleaning, setCleaning] = useState(false);
  return (
    <div className="pt-page-top px-page pb-page gap-page-title flex h-full flex-col overflow-auto">
      <div className="flex items-center justify-between gap-3">
        <h1 className="type-page-title text-text-primary m-0">Agents</h1>
        {removable.length > 0 && (
          <span className="flex items-center gap-2">
            {removing && (
              <span className="text-text-hint flex">
                <Spinner />
              </span>
            )}
            <Button
              size="sm"
              disabled={removing}
              onClick={() => {
                void loadRemovals();
                setCleaning(true);
              }}
            >
              Clean up
            </Button>
          </span>
        )}
      </div>
      <CleanUpDialog open={cleaning} onOpenChange={setCleaning} />
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
