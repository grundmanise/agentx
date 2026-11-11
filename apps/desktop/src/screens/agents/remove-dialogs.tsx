// The dialogs that remove agent configurations (DESIGN.md §6 "Removing an agent configuration").
// A card's Remove confirms one removal and names the directory that goes, or, for a configuration
// the CLI keeps, says what it still holds and offers nothing that would fail. Clean up lists the
// configurations that can be removed, each with its path and a checkbox, and removes the chosen
// ones in one `config remove`. Both read the preview again when they open.
import { useId, useState } from "react";

import type { ConfigRemovalEvent } from "#/cli/events.gen.ts";
import { AgentLogo } from "#/components/agent-logo.tsx";
import { Button } from "#/components/ui/button.tsx";
import { Checkbox } from "#/components/ui/checkbox.tsx";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "#/components/ui/dialog.tsx";
import { plural } from "#/lib/plural.ts";
import {
  removeConfigurations,
  useRemovalEvents,
} from "#/screens/agents/removal.ts";

/** "Zenflow", "Antigravity and Antigravity CLI": the names of `ids`, as the preview has them. */
const namesOf = (ids: readonly string[], all: readonly ConfigRemovalEvent[]) =>
  ids
    .map((id) => all.find((e) => e.id === id)?.name ?? id)
    .join(", ")
    .replace(/, (?=[^,]*$)/u, " and ");

const Path = ({ path }: { path: string }) => (
  <p className="type-technical text-text-caption m-0 break-all">{path}</p>
);

/** What a configuration the CLI keeps still holds, as the CLI names it. */
const KeptContent = ({ removal }: { removal: ConfigRemovalEvent }) => (
  <DialogContent>
    <DialogHeader showCloseButton={false}>
      <DialogTitle>{`${removal.name} can't be removed`}</DialogTitle>
      <DialogDescription>
        Its configuration directory holds something of its own:
      </DialogDescription>
    </DialogHeader>
    <Path path={removal.path} />
    <ul
      aria-label="What it holds"
      className="type-body text-text-secondary m-0 flex list-disc flex-col gap-1.5 py-0 pr-0 pl-5"
    >
      {removal.holds.map((held) => (
        <li key={held} className="wrap-break-word">
          {held}
        </li>
      ))}
    </ul>
    <DialogFooter>
      <DialogClose asChild>
        <Button variant="primary">Close</Button>
      </DialogClose>
    </DialogFooter>
  </DialogContent>
);

const RemovableContent = ({
  removal,
  onRemove,
}: {
  removal: ConfigRemovalEvent;
  onRemove: () => void;
}) => {
  const all = useRemovalEvents();
  return (
    <DialogContent>
      <DialogHeader showCloseButton={false}>
        <DialogTitle>{`Remove ${removal.name}?`}</DialogTitle>
        <DialogDescription>
          Its configuration directory is deleted from this machine:
        </DialogDescription>
      </DialogHeader>
      <Path path={removal.path} />
      <p className="type-body text-text-secondary m-0">
        It holds only links and copies of skills that stay on this machine.
      </p>
      {removal.with.length > 0 && (
        <p className="type-body text-text-secondary m-0">
          {`${namesOf(removal.with, all)} ${removal.with.length === 1 ? "is" : "are"} kept in the same directory and removed with it.`}
        </p>
      )}
      <DialogFooter>
        <DialogClose asChild>
          <Button variant="ghost">Cancel</Button>
        </DialogClose>
        <Button variant="danger" onClick={onRemove}>
          Remove
        </Button>
      </DialogFooter>
    </DialogContent>
  );
};

/** A card's Remove: the confirmation of one removal, or why the configuration stays. */
export const RemoveAgentDialog = ({
  removal,
  open,
  onOpenChange,
}: {
  removal: ConfigRemovalEvent;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) => (
  <Dialog open={open} onOpenChange={onOpenChange}>
    {removal.state === "kept" ? (
      <KeptContent removal={removal} />
    ) : (
      <RemovableContent
        removal={removal}
        onRemove={() => {
          void removeConfigurations([removal]);
          onOpenChange(false);
        }}
      />
    )}
  </Dialog>
);

const Row = ({
  removal,
  checked,
  onCheckedChange,
}: {
  removal: ConfigRemovalEvent;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
}) => {
  const id = useId();
  return (
    <li>
      <label htmlFor={id} className="flex items-center gap-2.5">
        <Checkbox
          id={id}
          checked={checked}
          onCheckedChange={(next) => {
            onCheckedChange(next === true);
          }}
        />
        <AgentLogo
          client={{ client: removal.id, name: removal.name }}
          size={20}
          decorative
        />
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="type-body text-text-primary">{removal.name}</span>
          <span
            className="type-technical-sm text-text-caption truncate"
            title={removal.path}
          >
            {removal.path}
          </span>
        </span>
      </label>
    </li>
  );
};

/**
 * Clean up: the configurations that can be removed, all chosen until one is unticked, and one
 * command for the chosen ones. Configurations kept in one directory are chosen together: the
 * outer or shared one goes only with the others, so unticking one unticks what needs it.
 */
export const CleanUpDialog = ({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) => {
  const all = useRemovalEvents();
  const removable = all.filter((e) => e.state === "removable");
  const kept = all.length - removable.length;
  // The unticked ones: a configuration the preview adds later starts ticked like the rest.
  const [skipped, setSkipped] = useState<readonly string[]>([]);
  const chosen = removable.filter((e) => !skipped.includes(e.id));
  const close = () => {
    setSkipped([]);
    onOpenChange(false);
  };
  const toggle = (removal: ConfigRemovalEvent, checked: boolean) => {
    if (checked) {
      const ticked = new Set([removal.id, ...removal.with]);
      setSkipped(skipped.filter((id) => !ticked.has(id)));
    } else {
      const needIt = removable
        .filter((e) => e.with.includes(removal.id))
        .map((e) => e.id);
      setSkipped([...new Set([...skipped, removal.id, ...needIt])]);
    }
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (next) {
          onOpenChange(true);
        } else {
          close();
        }
      }}
    >
      <DialogContent>
        <DialogHeader showCloseButton={false}>
          <DialogTitle>Remove leftover configurations</DialogTitle>
          <DialogDescription>
            These agent configurations hold only links and copies of skills that
            stay on this machine. Removing one deletes its directory.
          </DialogDescription>
        </DialogHeader>
        <ul
          aria-label="Configurations that can be removed"
          className="max-h-cleanup-list m-0 flex list-none flex-col gap-3 overflow-auto p-0"
        >
          {removable.map((removal) => (
            <Row
              key={removal.id}
              removal={removal}
              checked={!skipped.includes(removal.id)}
              onCheckedChange={(checked) => {
                toggle(removal, checked);
              }}
            />
          ))}
        </ul>
        {kept > 0 && (
          <p className="type-secondary text-text-hint m-0">
            {`${plural(kept, "other agent configuration")} ${kept === 1 ? "holds" : "hold"} something of ${kept === 1 ? "its" : "their"} own and ${kept === 1 ? "stays" : "stay"}.`}
          </p>
        )}
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="ghost">Cancel</Button>
          </DialogClose>
          <Button
            variant="danger"
            disabled={chosen.length === 0}
            onClick={() => {
              void removeConfigurations(chosen);
              close();
            }}
          >
            {`Remove ${chosen.length}`}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
