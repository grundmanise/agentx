// The Remove dialog (DESIGN.md §6 "Banner actions and dialogs"): what leaves the machine with the
// skill, and for one of your own, whether its branch leaves the account remote too. Remove runs
// `skill remove <name>`, with --remote when that is checked, and never offers an Undo: a skill of a
// source is added again from its source, and unpublished edits of your own are gone.
import { useId, useState } from "react";

import type { Configuration } from "#/cli/events.gen.ts";
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
import { seenBy } from "#/screens/skills/placements.ts";
import { shortSource } from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { runUntilSettled } from "#/store/settle.ts";
import { useAppStore } from "#/store/store.ts";

/** The label of the Remove control for `row`: the runner's name for it. */
export const removeLabel = (row: SkillRow) => `remove ${row.key}`;

/** What getting the skill back takes, by where it came from. */
const Consequence = ({ row }: { row: SkillRow }) => {
  if (row.kind === "source") {
    return (
      <p className="type-body text-text-secondary m-0">
        {`Add it again from ${shortSource(row.entry?.source ?? "")} to get it back.`}
      </p>
    );
  }
  if (row.kind === "yours") {
    return (
      <p className="type-body text-text-secondary m-0">
        Edits you have not published are deleted.
      </p>
    );
  }
  return null;
};

export const RemoveDialog = ({
  row,
  configurations,
  open,
  onOpenChange,
}: {
  row: SkillRow;
  configurations: Configuration[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) => {
  const [remote, setRemote] = useState(false);
  const checkboxId = useId();
  const hasAccountRemote = useAppStore((s) =>
    s.sources.some((source) => source.account === true)
  );
  const agents = seenBy(row, configurations);
  // Your own published skill, when an account remote is set, can leave that too.
  const offersRemote =
    row.kind === "yours" && row.published && hasAccountRemote;
  // The choice lasts one confirmation: a dialog dismissed and opened again starts unticked, so
  // the account remote's branch goes only when the box is ticked in the confirmation that removes.
  const close = () => {
    setRemote(false);
    onOpenChange(false);
  };
  const remove = () => {
    const args = ["skill", "remove", row.name];
    void runUntilSettled(
      offersRemote && remote ? [...args, "--remote"] : args,
      {
        label: removeLabel(row),
        success: `Removed ${row.name}`,
      }
    );
    close();
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
          <DialogTitle>{`Remove ${row.name}?`}</DialogTitle>
          <DialogDescription>
            It will be deleted from every agent on this machine:
          </DialogDescription>
        </DialogHeader>
        <ul
          aria-label="Agents that see it"
          className="type-body text-text-secondary m-0 flex list-none flex-col gap-2 p-0"
        >
          {agents.map((c) => (
            <li key={c.id} className="flex items-center gap-2.5">
              <AgentLogo client={c} size={20} decorative />
              {c.name}
            </li>
          ))}
        </ul>
        <Consequence row={row} />
        {offersRemote && (
          <label htmlFor={checkboxId} className="flex items-start gap-2.5">
            <Checkbox
              id={checkboxId}
              checked={remote}
              onCheckedChange={(checked) => {
                setRemote(checked === true);
              }}
            />
            <span className="flex min-w-0 flex-col gap-0.5">
              <span className="type-body text-text-primary">
                Also delete it from your account remote
              </span>
              <span className="type-secondary text-text-muted">
                Your other machines keep it until you remove it there.
              </span>
            </span>
          </label>
        )}
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="ghost">Cancel</Button>
          </DialogClose>
          <Button variant="danger" onClick={remove}>
            Remove
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
