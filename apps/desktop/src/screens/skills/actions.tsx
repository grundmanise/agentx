// The buttons of the Skills detail pane's banners. Each runs exactly one command line and redraws from the inventory that
// follows; a Reconnect the CLI refuses because the folder holds another version opens a confirm
// dialog, whose Replace runs the same command with --force. Abort, on a pending merge, asks first
// too. Show folder runs no command: it shows the merge folder in Finder.
import { useRef, useState } from "react";
import { create } from "zustand";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type { CliEvent, LibraryPlacement } from "#/cli/events.gen.ts";
import { Spinner } from "#/components/spinner.tsx";
import { dismissToast } from "#/components/toast.tsx";
import type { ErrorToast } from "#/components/toast.tsx";
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
import type { Outcome } from "#/store/runner.ts";
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

/** A folder of the skill in an agent's skills folder that stopped an adoption, and why. */
interface LeftFolder {
  path: string;
  /** The CLI's words after the path, such as "folder is not readable by agentx". */
  why: string;
}

/** The CLI's words for each folder that stops an adoption (docs/spec/cli-contract.md, Unmanaged skills). */
const LEFT_FOLDER =
  /^"(?<path>.+?)" (?<why>contains files that differ from the skill version|is also read by .+ set to copy files, while .+ symlinks|folder is not writable by agentx|folder is not readable by agentx)$/u;

const DIFFERS = "contains files that differ from the skill version";

/** The folders `<name> cannot be adopted: "<path>" <why>, and …` names, or none for another refusal. */
const leftFolders = (name: string, message: string): LeftFolder[] => {
  const prefix = `${name} cannot be adopted: `;
  if (!message.startsWith(prefix)) {
    return [];
  }
  const folders = message
    .slice(prefix.length)
    .split(", and ")
    .map((part) => LEFT_FOLDER.exec(part)?.groups);
  return folders.every((g) => g?.path !== undefined && g.why !== undefined)
    ? folders.map((g) => ({ path: g?.path ?? "", why: g?.why ?? "" }))
    : [];
};

/** The Replace confirm an adoption's toast opens for the folders whose files differ. */
export const useAdoptReplace = create<{
  request?: { row: SkillRow; paths: string[] };
}>(() => ({}));

/**
 * The error toast of an adoption a folder of the skill in an agent's skills folder stopped: each
 * folder and why, as the CLI says it, and Replace, which asks, then adopts with --force, for
 * folders whose files differ. undefined for any other failure.
 */
const adoptRefusal = (
  row: SkillRow,
  outcome: Outcome
): ErrorToast | undefined => {
  const error = outcome.events.find((e) => e.type === "error");
  if (error?.type !== "error") {
    return undefined;
  }
  const folders = leftFolders(row.name, error.message);
  if (folders.length === 0) {
    return undefined;
  }
  const replace = folders.filter((f) => f.why === DIFFERS).map((f) => f.path);
  return {
    action:
      replace.length === 0
        ? undefined
        : {
            label: "Replace",
            onClick: () => {
              useAdoptReplace.setState({ request: { paths: replace, row } });
            },
          },
    details: folders.map(({ path, why }) => `${path} ${why}.`),
    keep: [row.name],
    lasting: true,
    message: `${row.name} skill wasn't adopted`,
  };
};

/**
 * Adopt: the `adopt` command line of `adoptArgs` for an unmanaged skill, which agentx manages
 * from then on. A skill taken out of a client's own skills directory becomes a library skill
 * under its directory's name, so it is selected by the name the CLI reports. With `force`, an
 * agent's folder of the skill whose files differ gives way to it.
 */
export const adopt = async (row: SkillRow, force = false) => {
  const args = adoptArgs(row);
  if (args === undefined) {
    return;
  }
  const outcome = await runUntilSettled(force ? [...args, "--force"] : args, {
    failure: (failed) => adoptRefusal(row, failed),
    label: adoptLabel(row),
    success: (events) => `Adopted ${adoptedName(events) ?? row.name} skill`,
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
      <AlertDialog open={confirm} onOpenChange={setConfirm}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{`Replace ${agent}'s copy?`}</AlertDialogTitle>
            <AlertDialogDescription>
              {`${agent}'s copy of ${row.name} is different from the one in your library. Replacing it deletes ${agent}'s changes.`}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <p className="type-technical text-text-caption m-0 break-all">
            {placement.path}
          </p>
          <AlertDialogFooter>
            <AlertDialogCancel asChild>
              <Button variant="ghost">Cancel</Button>
            </AlertDialogCancel>
            <Button
              variant="danger"
              onClick={() => {
                setConfirm(false);
                void putBack(true);
              }}
            >
              Replace
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
};

/**
 * The confirm that Replace on an adoption's toast opens: adopting with --force deletes the files
 * of the folders listed.
 */
export const AdoptReplaceConfirm = () => {
  const request = useAdoptReplace((s) => s.request);
  const close = () => {
    useAdoptReplace.setState({ request: undefined });
  };
  const one = request?.paths.length === 1;
  return (
    <AlertDialog
      open={request !== undefined}
      onOpenChange={(open) => {
        if (!open) {
          close();
        }
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {one ? "Replace this folder?" : "Replace these folders?"}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {`${one ? "It contains" : "They contain"} files that differ from ${request?.row.name ?? ""} in your library. Adopting the skill replaces ${one ? "it" : "them"}, and deletes those files.`}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {request?.paths.map((path) => (
          <p
            key={path}
            className="type-technical text-text-caption m-0 break-all"
          >
            {path}
          </p>
        ))}
        <AlertDialogFooter>
          <AlertDialogCancel asChild>
            <Button variant="ghost">Cancel</Button>
          </AlertDialogCancel>
          <Button
            variant="danger"
            onClick={() => {
              close();
              if (request !== undefined) {
                void adopt(request.row, true);
              }
            }}
          >
            Replace
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
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
