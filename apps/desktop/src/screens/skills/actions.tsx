// The buttons that settle what a skill needs, shared by the banners of the Skills detail pane and
// the cards of the Inbox so that both run the same command, show the same pending state and ask
// the same question before `--force`. Each runs exactly one command line and redraws from the
// inventory that follows; a command the CLI refuses because a folder holds other content opens a
// confirm dialog, whose Replace or Take it over runs the same command with --force.
import { useState } from "react";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type { LibraryPlacement } from "#/cli/events.gen.ts";
import { Spinner } from "#/components/spinner.tsx";
import { dismissToast } from "#/components/toast.tsx";
import { Button } from "#/components/ui/button.tsx";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "#/components/ui/dialog.tsx";
import {
  agentsWord,
  holdsOtherContent,
  placedCount,
} from "#/screens/skills/placements.ts";
import { shortSource } from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { runUntilSettled, useSettling } from "#/store/settle.ts";
import { dropReconcile } from "#/store/store.ts";

/** How the button looks where it sits: a ghost in a banner, secondary on an Inbox card. */
export type ActionVariant = "ghost" | "secondary";

/** The label of the Adopt control for `name`: the runner's name for it, shared by pill, banner and card. */
export const adoptLabel = (name: string) => `adopt ${name}`;

/** The label of the Reconnect control for a placement of `row`. */
export const reconnectLabel = (row: SkillRow, placement: LibraryPlacement) =>
  `put back ${row.key} ${placement.configuration}`;

export const restoreLabel = (name: string) => `restore ${name}`;
export const takeOverLabel = (name: string) => `take over ${name}`;
export const addSourceLabel = (source: string) => `add source ${source}`;
export const forkLabel = (name: string) => `fork ${name}`;

/** Adopt: `adopt --skill <name>`. The skill is then managed, and agentx can update it. */
export const adopt = async (name: string) =>
  await runUntilSettled(["adopt", "--skill", name], {
    label: adoptLabel(name),
    success: `Adopted ${name}`,
  });

/** A small button that runs a command, pending while the command and its inventory come. */
export const ActionButton = ({
  label,
  pending,
  onClick,
  variant = "ghost",
}: {
  label: string;
  pending: boolean;
  onClick: () => void;
  variant?: ActionVariant;
}) => (
  <Button
    variant={variant}
    size="sm"
    disabled={pending}
    aria-busy={pending ? true : undefined}
    onClick={onClick}
  >
    {pending && <Spinner />}
    {label}
  </Button>
);

/** Add to agents: `skill place <name>`, into every enabled agent. */
export const AddToAgents = ({
  row,
  variant,
}: {
  row: SkillRow;
  variant?: ActionVariant;
}) => {
  const label = `place ${row.key}`;
  const pending = useSettling(label);
  return (
    <ActionButton
      label="Add to agents"
      pending={pending}
      variant={variant}
      onClick={() => {
        void runUntilSettled(["skill", "place", row.name], {
          label,
          success: (events) =>
            `Added ${row.name} to ${agentsWord(placedCount(events))}`,
        });
      }}
    />
  );
};

/** Adopt, for a skill another tool installed. */
export const Adopt = ({
  name,
  variant,
}: {
  name: string;
  variant?: ActionVariant;
}) => {
  const pending = useSettling(adoptLabel(name));
  return (
    <ActionButton
      label="Adopt"
      pending={pending}
      variant={variant}
      onClick={() => {
        void adopt(name);
      }}
    />
  );
};

/**
 * Reconnect: `skill place <name> --to <configuration>`. When the CLI refuses because the folder
 * holds another version, the dialog asks before running it again with --force.
 */
export const Reconnect = ({
  row,
  placement,
  agent,
  variant,
}: {
  row: SkillRow;
  placement: LibraryPlacement;
  agent: string;
  variant?: ActionVariant;
}) => {
  const label = reconnectLabel(row, placement);
  const pending = useSettling(label);
  const [confirm, setConfirm] = useState(false);
  const putBack = async (force: boolean) => {
    const args = ["skill", "place", row.name, "--to", placement.configuration];
    const outcome = await runUntilSettled(force ? [...args, "--force"] : args, {
      label,
      success: `Reconnected ${row.name} to ${agent}`,
    });
    if (
      !force &&
      outcome.exitCode === EXIT_CODES.refused &&
      holdsOtherContent(outcome.events)
    ) {
      // The dialog answers this refusal: the error toast would only repeat it.
      dismissToast();
      setConfirm(true);
    }
  };
  return (
    <>
      <ActionButton
        label="Reconnect"
        pending={pending}
        variant={variant}
        onClick={() => {
          void putBack(false);
        }}
      />
      <Dialog open={confirm} onOpenChange={setConfirm}>
        <DialogContent>
          <DialogHeader showCloseButton={false}>
            <DialogTitle>{`Replace ${agent}'s copy?`}</DialogTitle>
            <DialogDescription>
              {`${agent}'s copy of ${row.name} is different from the one in your library. Replacing it deletes ${agent}'s changes.`}
            </DialogDescription>
          </DialogHeader>
          <p className="type-technical text-text-caption m-0 break-all">
            {placement.path}
          </p>
          <DialogFooter>
            <DialogClose asChild>
              <Button variant="ghost">Cancel</Button>
            </DialogClose>
            <Button
              variant="danger"
              onClick={() => {
                setConfirm(false);
                void putBack(true);
              }}
            >
              Replace
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
};

/**
 * Restore, for one of your own skills whose folder is gone: `skill place <name>` lays it out again
 * from your saved version. Serve reports the missing folder at its start only, so the card leaves
 * once the command exits 0.
 */
export const Restore = ({
  name,
  variant,
}: {
  name: string;
  variant?: ActionVariant;
}) => {
  const pending = useSettling(restoreLabel(name));
  return (
    <ActionButton
      label="Restore"
      pending={pending}
      variant={variant}
      onClick={() => {
        void (async () => {
          const outcome = await runUntilSettled(["skill", "place", name], {
            label: restoreLabel(name),
            success: `Restored ${name}`,
          });
          if (outcome.exitCode === EXIT_CODES.ok) {
            dropReconcile(name, "worktree missing");
          }
        })();
      }}
    />
  );
};

/**
 * Take it over, for a folder of yours where one of your own skills belongs: after a confirm
 * dialog, `skill place <name> --force` adopts it, every file kept as an unpublished edit.
 */
export const TakeOver = ({
  name,
  path,
  variant,
}: {
  name: string;
  path: string;
  variant?: ActionVariant;
}) => {
  const pending = useSettling(takeOverLabel(name));
  const [confirm, setConfirm] = useState(false);
  const takeOver = async () => {
    const outcome = await runUntilSettled(["skill", "place", name, "--force"], {
      label: takeOverLabel(name),
      success: `Took over ${path}`,
    });
    if (outcome.exitCode === EXIT_CODES.ok) {
      dropReconcile(name, "adopt candidate");
    }
  };
  return (
    <>
      <ActionButton
        label="Take it over"
        pending={pending}
        variant={variant}
        onClick={() => {
          setConfirm(true);
        }}
      />
      <Dialog open={confirm} onOpenChange={setConfirm}>
        <DialogContent>
          <DialogHeader showCloseButton={false}>
            <DialogTitle>Take over this folder?</DialogTitle>
            <DialogDescription>
              {`Its files become unpublished edits of ${name}.`}
            </DialogDescription>
          </DialogHeader>
          <p className="type-technical text-text-caption m-0 break-all">
            {path}
          </p>
          <DialogFooter>
            <DialogClose asChild>
              <Button variant="ghost">Cancel</Button>
            </DialogClose>
            <Button
              variant="secondary"
              onClick={() => {
                setConfirm(false);
                void takeOver();
              }}
            >
              Take it over
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
};

/** Add source again: `source add <url>`, which ends the skill's "source removed" state. */
export const AddSourceAgain = ({
  source,
  variant,
}: {
  source: string;
  variant?: ActionVariant;
}) => {
  const pending = useSettling(addSourceLabel(source));
  return (
    <ActionButton
      label="Add source again"
      pending={pending}
      variant={variant}
      onClick={() => {
        void runUntilSettled(["source", "add", source], {
          label: addSourceLabel(source),
          success: `Added ${shortSource(source)} again`,
        });
      }}
    />
  );
};

/** Fork: `skill fork <name>` makes the skill one of your own, which keeps working without its source. */
export const Fork = ({
  name,
  variant,
}: {
  name: string;
  variant?: ActionVariant;
}) => {
  const pending = useSettling(forkLabel(name));
  return (
    <ActionButton
      label="Fork"
      pending={pending}
      variant={variant}
      onClick={() => {
        void runUntilSettled(["skill", "fork", name], {
          label: forkLabel(name),
          success: `Forked ${name}. It's one of your skills now`,
        });
      }}
    />
  );
};
