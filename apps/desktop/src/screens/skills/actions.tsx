// The buttons of the Skills detail pane's banners. Each runs exactly one command line and redraws from the inventory that
// follows; a Reconnect the CLI refuses because the folder holds another version opens a confirm
// dialog, whose Replace runs the same command with --force. Abort, on a pending merge, asks first
// too. Show folder runs no command: it shows the merge folder in Finder.
import { useRef, useState } from "react";
import { create } from "zustand";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type {
  CliEvent,
  Configuration,
  LibraryPlacement,
  LogEvent,
} from "#/cli/events.gen.ts";
import { Spinner } from "#/components/spinner.tsx";
import { dismissToast } from "#/components/toast.tsx";
import type { ToastAction } from "#/components/toast.tsx";
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
import { revealInFinder } from "#/lib/reveal.ts";
import { openSkill } from "#/screens/skills/open.ts";
import {
  adoptArgs,
  agentsWord,
  holdsOtherContent,
  placedCount,
} from "#/screens/skills/placements.ts";
import { shortSource, updateVersions } from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { UpdateSheet } from "#/screens/skills/update-sheet.tsx";
import { updateLabel, updateSkill } from "#/screens/skills/updates.ts";
import { runUntilSettled, useSettling } from "#/store/settle.ts";
import { useAppStore } from "#/store/store.ts";

/** The label of the Adopt control for `row`: the runner's name for it. */
const adoptLabel = (row: SkillRow) => `adopt ${row.key}`;

/** The label of the Reconnect control for a placement of `row`. */
const reconnectLabel = (row: SkillRow, placement: LibraryPlacement) =>
  `put back ${row.key} ${placement.configuration}`;

const addSourceLabel = (source: string) => `add source ${source}`;
const abortLabel = (name: string) => `abort update ${name}`;

/** The name the CLI reports for the skill a run of `adopt` took over as one of your own. */
const adoptedName = (events: CliEvent[]) =>
  events.find((e) => e.type === "library_skill")?.name;

/** An agent's folder an adoption left as it is, as its warning names it, and why. */
interface LeftFolder {
  path: string;
  why: string;
  name: string;
  id: string;
}

/**
 * The folder an adoption's warning says it left as it is: `<path> <why> and was left as it is; to
 * replace it with the adopted skill, run 'agentx skill place <name> --to <id> --force'`.
 */
const leftFolder = (message: string): LeftFolder | undefined => {
  const groups =
    /^(?<path>.+?) (?<why>has other files than \S+|is read by .+|is in a folder agentx cannot write to|cannot be read) and was left as it is; to replace it with the adopted skill, run 'agentx skill place (?<name>\S+) --to (?<id>\S+) --force'$/u.exec(
      message
    )?.groups;
  if (groups === undefined) {
    return undefined;
  }
  const { path = "", why = "", name = "", id = "" } = groups;
  return {
    id,
    name,
    path,
    why: why.replace(/^has other files than \S+$/u, "has other files"),
  };
};

/** The lines under the adoption's toast: a folder it left, as "<path> <why>. It was left as is." */
const adoptWarnings = (logs: LogEvent[]) =>
  logs.map((log) => {
    const left = leftFolder(log.message);
    return left === undefined
      ? log.message
      : `${left.path} ${left.why}. It was left as is.`;
  });

/** The folder whose Replace the dialog below asks about, while it is open. */
const useReplaceRequest = create<{ request?: LeftFolder }>()(() => ({}));

/** Replace, on the adoption's toast, for the first folder it left that has other files. */
const replaceAction = (logs: LogEvent[]): ToastAction | undefined => {
  const left = logs
    .map((log) => leftFolder(log.message))
    .find((l) => l?.why === "has other files");
  return left === undefined
    ? undefined
    : {
        label: "Replace",
        onClick: () => {
          useReplaceRequest.setState({ request: left });
        },
      };
};

/**
 * Adopt: the `adopt` command line of `adoptArgs` for an unmanaged skill, which agentx manages
 * from then on. A skill taken out of a client's own skills directory becomes a library skill
 * under its directory's name, so it is selected by the name the CLI reports.
 */
export const adopt = async (row: SkillRow) => {
  const args = adoptArgs(row);
  if (args === undefined) {
    return;
  }
  const outcome = await runUntilSettled(args, {
    action: (_events, logs) => replaceAction(logs),
    label: adoptLabel(row),
    success: (events) => `Adopted ${adoptedName(events) ?? row.name} skill`,
    warnings: adoptWarnings,
  });
  const adopted = adoptedName(outcome.events);
  if (
    outcome.exitCode === EXIT_CODES.ok &&
    row.entry === undefined &&
    adopted !== undefined
  ) {
    openSkill(adopted);
  }
};

/** A small ghost button that runs a command, pending while the command and its inventory come. */
const ActionButton = ({
  label,
  pending,
  onClick,
}: {
  label: string;
  pending: boolean;
  onClick: () => void;
}) => (
  <Button
    variant="ghost"
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
export const AddToAgents = ({ row }: { row: SkillRow }) => {
  const label = `place ${row.key}`;
  const pending = useSettling(label);
  return (
    <ActionButton
      label="Add to agents"
      pending={pending}
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

/** Adopt, for an unmanaged skill `adopt` can take over. */
export const Adopt = ({ row }: { row: SkillRow }) => {
  const pending = useSettling(adoptLabel(row));
  return (
    <ActionButton
      label="Adopt"
      pending={pending}
      onClick={() => {
        void adopt(row);
      }}
    />
  );
};

/** Asks before `--force` replaces an agent's copy of a skill that differs from the library's. */
const ReplaceCopyConfirm = ({
  open,
  onOpenChange,
  agent,
  name,
  path,
  onReplace,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  agent: string;
  name: string;
  path: string;
  onReplace: () => void;
}) => (
  <AlertDialog open={open} onOpenChange={onOpenChange}>
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>{`Replace ${agent}'s copy?`}</AlertDialogTitle>
        <AlertDialogDescription>
          {`${agent}'s copy of ${name} is different from the one in your library. Replacing it deletes ${agent}'s changes.`}
        </AlertDialogDescription>
      </AlertDialogHeader>
      <p className="type-technical text-text-caption m-0 break-all">{path}</p>
      <AlertDialogFooter>
        <AlertDialogCancel asChild>
          <Button variant="ghost">Cancel</Button>
        </AlertDialogCancel>
        <Button variant="danger" onClick={onReplace}>
          Replace
        </Button>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
);

/**
 * Reconnect: `skill place <name> --to <configuration>`. When the CLI refuses because the folder
 * holds another version, the dialog asks before running it again with --force.
 */
export const Reconnect = ({
  row,
  placement,
  agent,
}: {
  row: SkillRow;
  placement: LibraryPlacement;
  agent: string;
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
        onClick={() => {
          void putBack(false);
        }}
      />
      <ReplaceCopyConfirm
        open={confirm}
        onOpenChange={setConfirm}
        agent={agent}
        name={row.name}
        path={placement.path}
        onReplace={() => {
          setConfirm(false);
          void putBack(true);
        }}
      />
    </>
  );
};

/**
 * The confirm dialog of the adoption toast's Replace: `skill place <name> --to <id> --force`
 * replaces the folder the adoption left, once the user agrees.
 */
export const ReplaceLeftFolder = ({
  configurations,
}: {
  configurations: Configuration[];
}) => {
  const request = useReplaceRequest((s) => s.request);
  const close = () => {
    useReplaceRequest.setState({ request: undefined });
  };
  if (request === undefined) {
    return null;
  }
  const agent =
    configurations.find((c) => c.id === request.id)?.name ?? request.id;
  return (
    <ReplaceCopyConfirm
      open
      onOpenChange={(open) => {
        if (!open) {
          close();
        }
      }}
      agent={agent}
      name={request.name}
      path={request.path}
      onReplace={() => {
        close();
        void runUntilSettled(
          ["skill", "place", request.name, "--to", request.id, "--force"],
          {
            label: `replace ${request.name} ${request.id}`,
            success: `Replaced ${agent}'s copy of ${request.name}`,
          }
        );
      }}
    />
  );
};

/** Add source: `source add <url>`, which ends the skill's "source removed" state. */
export const AddSource = ({ source }: { source: string }) => {
  const pending = useSettling(addSourceLabel(source));
  return (
    <ActionButton
      label="Add source"
      pending={pending}
      onClick={() => {
        void runUntilSettled(["source", "add", source], {
          label: addSourceLabel(source),
          success: `Added ${shortSource(source)} source`,
        });
      }}
    />
  );
};

/** Update: `skill update <name>`, the primary action of an update's banner. */
export const UpdateButton = ({ row }: { row: SkillRow }) => {
  const pending = useSettling(updateLabel(row.name));
  return (
    <Button
      variant="primary"
      size="sm"
      disabled={pending}
      aria-busy={pending ? true : undefined}
      onClick={() => {
        void updateSkill(row);
      }}
    >
      {pending && <Spinner />}
      Update
    </Button>
  );
};

/**
 * See changes: opens the What's new sheet of the skill's update, beside Update on an update's
 * banner. The sheet reads what the update changes and names its two versions, by date where the
 * CLI dates them.
 */
export const SeeChanges = ({ row }: { row: SkillRow }) => {
  const [open, setOpen] = useState(false);
  const button = useRef<HTMLButtonElement>(null);
  const { update } = row;
  if (update === undefined) {
    return null;
  }
  return (
    <>
      <Button
        ref={button}
        variant="ghost"
        size="sm"
        onClick={() => {
          setOpen(true);
        }}
      >
        See changes
      </Button>
      <UpdateSheet
        update={update}
        versions={updateVersions(update)}
        open={open}
        onOpenChange={setOpen}
        onCloseAutoFocus={(e) => {
          // The sheet has no trigger: the focus goes back to See changes.
          e.preventDefault();
          button.current?.focus();
        }}
      />
    </>
  );
};

/**
 * Show folder: the folder of a pending merge, `<agentx home>/merges/<name>/<name>`, where the
 * conflicts are resolved, shown in Finder. agentx home is the machine's, from `machine`; the
 * button waits for it.
 */
export const ShowMergeFolder = ({ name }: { name: string }) => {
  const home = useAppStore((s) => s.machine?.home);
  return (
    <Button
      variant="ghost"
      size="sm"
      disabled={home === undefined}
      onClick={() => {
        if (home !== undefined) {
          void revealInFinder(`${home}/merges/${name}/${name}`);
        }
      }}
    >
      Show folder
    </Button>
  );
};

/**
 * Abort: `skill update <name> --abort` gives up a pending merge, after a confirm dialog. The
 * skill and the update stay as they were; what was resolved in the merge folder is discarded.
 */
export const AbortMerge = ({ name }: { name: string }) => {
  const label = abortLabel(name);
  const pending = useSettling(label);
  const [confirm, setConfirm] = useState(false);
  return (
    <>
      <Button
        variant="danger"
        size="sm"
        disabled={pending}
        aria-busy={pending ? true : undefined}
        onClick={() => {
          setConfirm(true);
        }}
      >
        {pending && <Spinner />}
        Abort
      </Button>
      <AlertDialog open={confirm} onOpenChange={setConfirm}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{`Abort the update of ${name}?`}</AlertDialogTitle>
            <AlertDialogDescription>
              Your resolved files in the merge folder are discarded. The skill
              and the update stay as they were.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel asChild>
              <Button variant="ghost">Cancel</Button>
            </AlertDialogCancel>
            <Button
              variant="danger"
              onClick={() => {
                setConfirm(false);
                void runUntilSettled(["skill", "update", name, "--abort"], {
                  label,
                  success: `Aborted the update of ${name} skill`,
                });
              }}
            >
              Abort
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
};
