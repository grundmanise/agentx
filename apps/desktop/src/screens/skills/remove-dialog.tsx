// The Remove dialog (DESIGN.md §6 "Banner actions and dialogs"): what leaves the machine with the
// skill, the agents that still see it somewhere Remove leaves, and for a hub skill,
// whether its branch leaves the hub too. Opened from the
// Universal chip, it says why the skill leaves every agent: those agents read the library. Remove runs
// `skill remove <name>`, with --remote when that is checked, and never offers an Undo: unpublished
// edits of a hub skill are gone. A skill only agents' own folders hold is removed by its folder's
// name, every folder of it at once.
import { useId, useRef, useState } from "react";

import type { Configuration } from "#/cli/events.gen.ts";
import { AgentLogo } from "#/components/agent-logo.tsx";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "#/components/ui/alert-dialog.tsx";
import { Button } from "#/components/ui/button.tsx";
import { Checkbox } from "#/components/ui/checkbox.tsx";
import { andList } from "#/lib/plural.ts";
import {
  removalOf,
  removeArgs,
  removeName,
} from "#/screens/skills/placements.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import type { CommandOptions } from "#/store/runner.ts";
import { agentx } from "#/store/runner.ts";
import { sourcesOf, useAppStore } from "#/store/store.ts";

/** The label of the Remove control for `row`: the runner's name for it. */
export const removeLabel = (row: SkillRow) => `remove ${row.key}`;

/** The agents that still see the skill after Remove, through a link or a folder it leaves. */
const Kept = ({ keeping }: { keeping: Configuration[] }) => {
  if (keeping.length === 0) {
    return null;
  }
  const see = keeping.length === 1 ? "sees" : "see";
  return (
    <p className="type-body text-text-secondary m-0">
      {`${andList(keeping.map((c) => c.name))} still ${see} it: Remove leaves links and folders under another name.`}
    </p>
  );
};

/** What is lost with the skill: the unpublished edits of a hub skill. */
const Consequence = ({ row }: { row: SkillRow }) => {
  if (row.kind === "hub") {
    return (
      <p className="type-body text-text-secondary m-0">
        Edits you have not published are deleted.
      </p>
    );
  }
  return null;
};

/** The line under the title: why every agent loses the skill when the Universal chip opened the dialog, and what comes next. */
const descriptionOf = (fromUniversal: boolean, losing: boolean) => {
  if (!losing) {
    return "Removes it from your library.";
  }
  return fromUniversal
    ? "Removing it from universal agents will also remove it from your library. It will be removed from:"
    : "Removes it from:";
};

export const RemoveDialog = ({
  row,
  configurations,
  open,
  fromUniversal,
  onClose,
  onCloseAutoFocus,
}: {
  row: SkillRow;
  configurations: Configuration[];
  open: boolean;
  /** Whether the Universal chip opened it: the dialog then says why every agent loses the skill. */
  fromUniversal: boolean;
  onClose: () => void;
  /** Moves focus when the dialog closes, when it has no trigger to return to; `removed` is true after Remove. */
  onCloseAutoFocus?: (event: Event, removed: boolean) => void;
}) => {
  const [remote, setRemote] = useState(false);
  const checkboxId = useId();
  const hasHub = useAppStore((s) =>
    sourcesOf(s).some((source) => source.hub === true)
  );
  const { losing, keeping } = removalOf(row, configurations);
  // Whether this close is the confirmation, for `onCloseAutoFocus`.
  const removed = useRef(false);
  // A published hub skill, when a hub is set, can leave that too.
  const offersRemote = row.kind === "hub" && row.published && hasHub;
  // The choice lasts one confirmation: a dialog dismissed and opened again starts unticked, so
  // the hub's branch goes only when the box is ticked in the confirmation that removes.
  const close = () => {
    setRemote(false);
    onClose();
  };
  const remove = () => {
    const base = removeArgs(removeName(row) ?? row.name);
    const args = offersRemote && remote ? [...base, "--remote"] : base;
    // Retry, after another command held the lock, also keeps Remove pending until the inventory
    // after it arrives.
    const options: CommandOptions = {
      label: removeLabel(row),
      retry: () => {
        void agentx(args, options);
      },
      success: `Removed ${row.name} skill`,
    };
    void agentx(args, options);
    removed.current = true;
    close();
  };
  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          close();
        }
      }}
    >
      <AlertDialogContent
        onCloseAutoFocus={(event) => {
          onCloseAutoFocus?.(event, removed.current);
          removed.current = false;
        }}
      >
        <AlertDialogHeader>
          <AlertDialogTitle>{`Remove ${row.name} from this machine?`}</AlertDialogTitle>
          <AlertDialogDescription>
            {descriptionOf(fromUniversal, losing.length > 0)}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {/* The one block that scrolls: past the dialog's height, the header, the consequence and
            the buttons stay, and the agents scroll between them. */}
        {losing.length > 0 && (
          <ul
            aria-label="Agents that see it"
            className="type-body text-text-secondary m-0 flex min-h-0 list-none flex-col gap-2 overflow-y-auto p-0"
          >
            {losing.map((c) => (
              <li key={c.id} className="flex items-center gap-2.5">
                <AgentLogo client={c} size={20} decorative />
                {c.name}
              </li>
            ))}
          </ul>
        )}
        <Kept keeping={keeping} />
        <Consequence row={row} />
        {offersRemote && (
          <label htmlFor={checkboxId} className="flex items-start gap-2.5">
            <Checkbox
              track="remove-dialog.toggle-delete-from-hub"
              id={checkboxId}
              checked={remote}
              onCheckedChange={(checked) => {
                setRemote(checked === true);
              }}
            />
            <span className="type-body text-text-primary">
              Also delete it from your hub
            </span>
          </label>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel asChild>
            <Button track="remove-dialog.cancel" variant="ghost">
              Cancel
            </Button>
          </AlertDialogCancel>
          <Button
            track="remove-dialog.confirm"
            variant="danger"
            onClick={remove}
          >
            Remove
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
};
