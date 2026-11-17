// The Rename dialog, for one of your own skills. Rename in the detail pane's action row opens it,
// and so does Rename your skill under AGENTS, which on a plugin's skill renames your own skill of
// that name (`ownOfName`). `RenameDialogs`, which the Skills screen mounts once, shows it, so it
// outlives the detail pane of the skill renamed, which leaves the inventory. Rename runs `skill
// rename <old> <new>`, and the renamed skill is selected by its new name, shown once the inventory
// that follows holds it. A refusal, such as a name another skill has (exit 6), keeps the dialog
// open with the CLI's message and its hint in the dialog's own words (`renameHint`); a change to
// the name clears it.
import { useEffect, useId, useRef, useState } from "react";
import { create } from "zustand";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import { ProblemText } from "#/components/problem-text.tsx";
import { Spinner } from "#/components/spinner.tsx";
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
import { Input } from "#/components/ui/input.tsx";
import {
  breaksSkillNameRule,
  isSkillName,
  SKILL_NAME_RULE,
} from "#/lib/skill-name.ts";
import { openSkill } from "#/screens/skills/open.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { renameArgs, renameHint } from "#/screens/skills/rename.ts";
import { usePending } from "#/store/runner.ts";
import type { Outcome } from "#/store/runner.ts";
import { runUntilSettled, useSettling } from "#/store/settle.ts";
import type { Problem } from "#/store/store.ts";

/** The label of the Rename control for `row`: the runner's name for it. */
export const renameLabel = (row: SkillRow) => `rename ${row.key}`;

interface RenameState {
  /** The skill the Rename dialog is open for. */
  renaming?: SkillRow;
}

const useRenameStore = create<RenameState>()(() => ({}));

/** Opens the Rename dialog for `row`, one of your own skills. */
export const openRename = (row: SkillRow) => {
  useRenameStore.setState({ renaming: row });
};

const closeRename = () => {
  useRenameStore.setState({ renaming: undefined });
};

/** What the CLI said when it refused a rename: its message, and its hint in the dialog's words. */
const refusalOf = (outcome: Outcome): Problem => {
  const error = outcome.events.find((e) => e.type === "error");
  const message =
    error?.message ??
    outcome.startError ??
    `agentx skill rename exited with code ${outcome.exitCode ?? "none"}`;
  const hint = error === undefined ? undefined : renameHint(error);
  return hint === undefined ? { message } : { hint, message };
};

/** Rename, a secondary button in the detail pane's action row of one of your own skills. */
export const RenameButton = ({ row }: { row: SkillRow }) => {
  const pending = useSettling(renameLabel(row));
  return (
    <Button
      variant="secondary"
      size="sm"
      disabled={pending}
      aria-busy={pending ? true : undefined}
      onClick={() => {
        openRename(row);
      }}
    >
      {pending && <Spinner />}
      Rename
    </Button>
  );
};

/** The Rename dialog's form, mounted while it is open for `row`, so it opens clean each time. */
const RenameForm = ({ row }: { row: SkillRow }) => {
  const [name, setName] = useState(row.name);
  const [refusal, setRefusal] = useState<Problem | undefined>();
  const pending = usePending(renameLabel(row));
  const nameRef = useRef<HTMLInputElement>(null);
  const ruleId = useId();
  const ready = isSkillName(name) && name !== row.name;

  // The name is selected as the dialog opens, so typing replaces it.
  useEffect(() => {
    nameRef.current?.select();
  }, []);

  // Renames to `to`. Retry, after another command held the lock, runs this again, so a rename that
  // then succeeds closes the dialog and selects the skill as the first try would.
  const rename = async (to: string) => {
    const outcome = await runUntilSettled(renameArgs(row.name, to), {
      keep: [row.name, to],
      label: renameLabel(row),
      ownFailure: true,
      retry: () => {
        void rename(to);
      },
      success: `Renamed ${row.name} skill to ${to}`,
    });
    if (outcome.exitCode === EXIT_CODES.ok) {
      closeRename();
      openSkill(to);
      return;
    }
    // Exit 4 opens the skill whose merge is pending (src/store/runner.ts).
    if (outcome.exitCode === EXIT_CODES.pending_merge) {
      closeRename();
      return;
    }
    // Another command holds the lock: the runner's toast says so, with Retry.
    if (outcome.exitCode === EXIT_CODES.locked) {
      return;
    }
    setRefusal(refusalOf(outcome));
    nameRef.current?.select();
  };

  const submit = () => {
    if (pending || !ready) {
      return;
    }
    setRefusal(undefined);
    void rename(name);
  };

  return (
    <form
      className="gap-dialog flex flex-col"
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
    >
      <div className="gap-input-label flex flex-col">
        <Input
          ref={nameRef}
          kind="mono"
          aria-label="Name"
          aria-invalid={breaksSkillNameRule(name) ? true : undefined}
          aria-describedby={breaksSkillNameRule(name) ? ruleId : undefined}
          value={name}
          disabled={pending}
          onChange={(e) => {
            setName(e.target.value);
            setRefusal(undefined);
          }}
        />
        {breaksSkillNameRule(name) && (
          <p id={ruleId} className="type-secondary text-text-hint m-0">
            {`Use ${SKILL_NAME_RULE}`}
          </p>
        )}
        {refusal !== undefined && (
          <ProblemText problem={refusal} keep={[row.name, name]} />
        )}
      </div>
      <DialogFooter>
        <DialogClose asChild>
          <Button variant="ghost" disabled={pending}>
            Cancel
          </Button>
        </DialogClose>
        <Button
          type="submit"
          variant="primary"
          disabled={pending || !ready}
          aria-busy={pending ? true : undefined}
        >
          {pending && <Spinner />}
          Rename
        </Button>
      </DialogFooter>
    </form>
  );
};

/** "Rename {skill}?": the Name field, Cancel · Rename. */
const RenameDialog = ({ row }: { row: SkillRow }) => {
  const pending = usePending(renameLabel(row));
  return (
    <Dialog
      open
      onOpenChange={(next) => {
        // The dialog stays while its command runs: a refusal comes back to it.
        if (!next && !pending) {
          closeRename();
        }
      }}
    >
      <DialogContent>
        <DialogHeader showCloseButton={false}>
          <DialogTitle>{`Rename ${row.name}?`}</DialogTitle>
          <DialogDescription>
            Your agents see it under the new name. Publish it to rename it on
            your account remote too.
          </DialogDescription>
        </DialogHeader>
        <RenameForm row={row} />
      </DialogContent>
    </Dialog>
  );
};

/** The Rename dialog while one is open. */
export const RenameDialogs = () => {
  const renaming = useRenameStore((s) => s.renaming);
  return renaming === undefined ? null : (
    <RenameDialog key={renaming.key} row={renaming} />
  );
};
