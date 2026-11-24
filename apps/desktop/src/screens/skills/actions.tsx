// The buttons of the Skills detail pane's banners. Each runs exactly one command line and redraws from the inventory that
// follows; a Reconnect the CLI refuses because the folder holds another version opens a confirm
// dialog, whose Replace runs the same command with --force. Discard my edits, on a pending merge,
// asks first too. Show folder runs no command: it shows the merge folder in Finder.
import { useRef, useState } from "react";
import { create } from "zustand";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type { CliEvent, LibraryPlacement } from "#/cli/events.gen.ts";
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
import { Spinner } from "#/components/ui/spinner.tsx";
import { revealIfThere, revealInFinder } from "#/lib/reveal.ts";
import type { Tracked } from "#/lib/track.ts";
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
import {
  settleMerge,
  updateLabel,
  updateSkill,
} from "#/screens/skills/updates.ts";
import type { Outcome } from "#/store/runner.ts";
import { runUntilSettled, useSettling } from "#/store/settle.ts";
import { useAppStore } from "#/store/store.ts";

/** The label of the Adopt control for `row`: the runner's name for it. */
const adoptLabel = (row: SkillRow) => `adopt ${row.key}`;

/** The label of the Reconnect control for a placement of `row`. */
const reconnectLabel = (row: SkillRow, placement: LibraryPlacement) =>
  `put back ${row.key} ${placement.configuration}`;

const addSourceLabel = (source: string) => `add source ${source}`;

/** The name the CLI reports for the skill a `skill import` run took over as a hub skill. */
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

/** The folders `<name> cannot be imported: "<path>" <why>, and …` names, or none for another refusal. */
const leftFolders = (name: string, message: string): LeftFolder[] => {
  const prefix = `${name} cannot be imported: `;
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
            track: "adopt-toast.replace",
          },
    details: folders.map(({ path, why }) => `${path} ${why}`),
    keep: [row.name],
    lasting: true,
    message: `${row.name} skill wasn't imported`,
  };
};

/**
 * Adopt: the `skill import` command line of `adoptArgs` for an unmanaged skill, which agentx
 * manages from then on. A skill taken out of a client's own skills directory becomes a library
 * skill under its directory's name, so it is selected by the name the CLI reports. With `force`,
 * an agent's folder of the skill whose files differ gives way to it.
 */
export const adopt = async (row: SkillRow, force = false) => {
  const args = adoptArgs(row);
  if (args === undefined) {
    return;
  }
  const outcome = await runUntilSettled(force ? [...args, "--force"] : args, {
    failure: (failed) => adoptRefusal(row, failed),
    label: adoptLabel(row),
    success: (events) => `Imported ${adoptedName(events) ?? row.name} skill`,
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
  track,
  label,
  pending,
  onClick,
}: Tracked & {
  label: string;
  pending: boolean;
  onClick: () => void;
}) => (
  <Button
    track={track}
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
      track="skill-banner.add-to-agents"
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

/** Adopt, for an unmanaged skill `skill import` can take over. */
export const Adopt = ({ row }: { row: SkillRow }) => {
  const pending = useSettling(adoptLabel(row));
  return (
    <ActionButton
      track="skill-banner.import"
      label="Import"
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
        track="skill-banner.reconnect"
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
              <Button track="reconnect-dialog.cancel" variant="ghost">
                Cancel
              </Button>
            </AlertDialogCancel>
            <Button
              track="reconnect-dialog.replace"
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
            {`${one ? "It contains" : "They contain"} files that differ from ${request?.row.name ?? ""} in your library. Importing the skill replaces ${one ? "it" : "them"}, and deletes those files.`}
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
            <Button track="adopt-replace-dialog.cancel" variant="ghost">
              Cancel
            </Button>
          </AlertDialogCancel>
          <Button
            track="adopt-replace-dialog.replace"
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
      track="skill-banner.add-source"
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
      track="skill-banner.update"
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
        track="skill-banner.see-changes"
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

/** Shows `<checkout>/<name>` in Finder, or the checkout when it holds no folder of that name. */
const revealMergeFolder = async (checkout: string, name: string) => {
  if (!(await revealIfThere(`${checkout}/${name}`))) {
    await revealInFinder(checkout);
  }
};

/**
 * Show folder: the skill's folder in the checkout of a pending merge, where the conflicts are
 * resolved, shown in Finder. The checkout is `<agentx home>/merges/<name>`, and the skill's folder
 * in it is named after its folder in the source, which is the skill's name but for a skill at a
 * repository's root or one named otherwise: then the checkout itself is shown, holding that one
 * folder. agentx home is the machine's, from `machine`; the button waits for it.
 */
export const ShowMergeFolder = ({ name }: { name: string }) => {
  const home = useAppStore((s) => s.inventory?.machine.home);
  return (
    <Button
      track="skill-banner.show-merge-folder"
      variant="ghost"
      size="sm"
      disabled={home === undefined}
      onClick={() => {
        if (home !== undefined) {
          void revealMergeFolder(`${home}/merges/${name}`, name);
        }
      }}
    >
      Show folder
    </Button>
  );
};

/**
 * Apply merge: `skill update <name> --continue` applies a pending merge with the files of the
 * merge folder as the user left them, saved with no Git. A file still holding a conflict marker
 * is refused, and the runner's toast names it and its line.
 */
export const ApplyMerge = ({ row }: { row: SkillRow }) => {
  const pending = useSettling(updateLabel(row.name));
  return (
    <Button
      track="skill-banner.apply-merge"
      variant="primary"
      size="sm"
      disabled={pending}
      aria-busy={pending ? true : undefined}
      onClick={() => {
        void settleMerge(row, "--continue");
      }}
    >
      {pending && <Spinner />}
      Apply merge
    </Button>
  );
};

/**
 * Discard my edits: `skill update <name> --theirs` drops the skill's edits and applies the update
 * as published, after a confirm dialog, since the edits are lost.
 */
export const DiscardEdits = ({ row }: { row: SkillRow }) => {
  const pending = useSettling(updateLabel(row.name));
  const [confirm, setConfirm] = useState(false);
  return (
    <>
      <Button
        track="skill-banner.discard-edits"
        variant="danger"
        size="sm"
        disabled={pending}
        onClick={() => {
          setConfirm(true);
        }}
      >
        Discard my edits
      </Button>
      <AlertDialog open={confirm} onOpenChange={setConfirm}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Update discarding local edits?</AlertDialogTitle>
            <AlertDialogDescription>
              Your local changes to the skill will be overwritten by the update.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel asChild>
              <Button track="discard-edits-dialog.cancel" variant="ghost">
                Cancel
              </Button>
            </AlertDialogCancel>
            <Button
              track="discard-edits-dialog.confirm"
              variant="danger"
              onClick={() => {
                setConfirm(false);
                void settleMerge(row, "--theirs");
              }}
            >
              Discard
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
};
