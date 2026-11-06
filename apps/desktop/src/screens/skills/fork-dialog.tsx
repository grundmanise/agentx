// The Fork dialog and the dialog of what each agent sees after a plugin's skill is forked. Any
// control opens the Fork dialog with `openFork(row)`, or is the `ForkButton`; `ForkDialogs`, which
// the Skills screen mounts once, shows them, so they outlive the detail pane of the skill forked,
// which the fork replaces in the selection. Fork runs `skill fork <name>`, or `--name <new>` with
// Use a new name on, and the screen redraws from the inventory that follows: the fork is selected
// by the name the CLI reports, and shows once that inventory holds it. A refusal (exit 6), such as
// a library entry that is a symlink, keeps the dialog open with the CLI's words and turns Use a new
// name on, since forking beside the skill is the way out the CLI names.
import { useEffect, useId, useRef, useState } from "react";
import { create } from "zustand";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type { ForkNoteEvent } from "#/cli/events.gen.ts";
import { AgentLogo } from "#/components/agent-logo.tsx";
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
import { Switch } from "#/components/ui/switch.tsx";
import {
  breaksSkillNameRule,
  isSkillName,
  SKILL_NAME_RULE,
} from "#/lib/skill-name.ts";
import { forkArgs, forkTarget, forkText } from "#/screens/skills/fork.ts";
import { openSkill } from "#/screens/skills/open.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { usePending } from "#/store/runner.ts";
import { runUntilSettled, useSettling } from "#/store/settle.ts";
import { useAppStore } from "#/store/store.ts";
import type { Problem } from "#/store/store.ts";

/** The label of the Fork control for `row`: the runner's name for it. */
export const forkLabel = (row: SkillRow) => `fork ${row.key}`;

interface ForkState {
  /** The skill the Fork dialog is open for. */
  forking?: SkillRow;
  /** The notes of the plugin's skill last forked, until Done. */
  notes?: ForkNoteEvent[];
}

const useForkStore = create<ForkState>()(() => ({}));

/** Opens the Fork dialog for `row`. */
export const openFork = (row: SkillRow) => {
  useForkStore.setState({ forking: row });
};

const closeFork = () => {
  useForkStore.setState({ forking: undefined });
};

const closeNotes = () => {
  useForkStore.setState({ notes: undefined });
};

/** Fork, in the detail pane's action row (secondary) or on a banner (ghost): opens the dialog. */
export const ForkButton = ({
  row,
  place = "actions",
}: {
  row: SkillRow;
  place?: "actions" | "banner";
}) => {
  const pending = useSettling(forkLabel(row));
  return (
    <Button
      variant={place === "banner" ? "ghost" : "secondary"}
      size="sm"
      disabled={pending}
      aria-busy={pending ? true : undefined}
      onClick={() => {
        openFork(row);
      }}
    >
      {pending && <Spinner />}
      Fork
    </Button>
  );
};

/** The Fork dialog's form, mounted while it is open for `row`, so it opens clean each time. */
const ForkForm = ({ row }: { row: SkillRow }) => {
  const skills = useAppStore((s) => s.inventory?.skills);
  const [renaming, setRenaming] = useState(false);
  const [name, setName] = useState("");
  const [problem, setProblem] = useState<Problem | undefined>();
  const pending = usePending(forkLabel(row));
  const nameRef = useRef<HTMLInputElement>(null);
  const switchId = useId();
  const nameValid = isSkillName(name);

  // The field takes the focus when it shows: on the switch, or when a refusal turns it on.
  useEffect(() => {
    if (renaming) {
      nameRef.current?.focus();
    }
  }, [renaming]);

  const submit = async () => {
    if (pending || (renaming && !nameValid)) {
      return;
    }
    setProblem(undefined);
    const target = forkTarget(row, skills ?? []);
    const outcome = await runUntilSettled(
      forkArgs(target, renaming ? name : undefined),
      {
        label: forkLabel(row),
        ownFailure: true,
        success: `Forked ${row.name}`,
      }
    );
    if (outcome.exitCode === EXIT_CODES.ok) {
      closeFork();
      const fork = outcome.events.find((e) => e.type === "library_skill");
      openSkill(fork?.name ?? (renaming ? name : row.name));
      const notes = outcome.events.filter((e) => e.type === "fork_note");
      if (notes.length > 0) {
        useForkStore.setState({ notes });
      }
      return;
    }
    // Exit 4 opens the skill whose merge is pending (src/store/runner.ts).
    if (outcome.exitCode === EXIT_CODES.pending_merge) {
      closeFork();
      return;
    }
    // Another command holds the lock: the runner's toast says so, with Retry.
    if (outcome.exitCode === EXIT_CODES.locked) {
      return;
    }
    const error = outcome.events.find((e) => e.type === "error");
    const message =
      error?.message ??
      outcome.startError ??
      `agentx skill fork exited with code ${outcome.exitCode ?? "none"}`;
    setProblem(
      error?.hint === undefined ? { message } : { hint: error.hint, message }
    );
    if (outcome.exitCode === EXIT_CODES.refused) {
      setRenaming(true);
    }
  };

  return (
    <form
      className="gap-dialog flex flex-col"
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
    >
      <div className="gap-input-label flex flex-col">
        <label htmlFor={switchId} className="flex items-center gap-3">
          <span className="flex min-w-0 flex-1 flex-col gap-0.5">
            <span className="type-body text-text-primary">Use a new name</span>
            {row.kind !== "plugin" && (
              <span className="type-secondary text-text-muted">
                {`The fork then sits beside ${row.name} instead of replacing it.`}
              </span>
            )}
          </span>
          <Switch
            id={switchId}
            aria-label="Use a new name"
            checked={renaming}
            disabled={pending}
            onCheckedChange={setRenaming}
          />
        </label>
        {renaming && (
          <>
            <Input
              ref={nameRef}
              kind="mono"
              aria-label="Name"
              placeholder={`my-${row.name}`}
              aria-invalid={breaksSkillNameRule(name) ? true : undefined}
              value={name}
              disabled={pending}
              onChange={(e) => {
                setName(e.target.value);
              }}
            />
            {breaksSkillNameRule(name) && (
              <p className="type-secondary text-text-hint m-0">
                {`Use ${SKILL_NAME_RULE}`}
              </p>
            )}
          </>
        )}
        {problem !== undefined && (
          <div role="alert" className="flex flex-col gap-1">
            <p className="type-notice text-tone-danger-fg m-0">
              {problem.message}
            </p>
            {problem.hint !== undefined && (
              <p className="type-notice text-text-muted m-0">{problem.hint}</p>
            )}
          </div>
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
          disabled={pending || (renaming && !nameValid)}
          aria-busy={pending ? true : undefined}
        >
          {pending && <Spinner />}
          Fork
        </Button>
      </DialogFooter>
    </form>
  );
};

/** "Fork {skill}": what the fork does for this kind of skill, Use a new name, Cancel · Fork. */
const ForkDialog = ({ row }: { row: SkillRow }) => {
  const pending = usePending(forkLabel(row));
  return (
    <Dialog
      open
      onOpenChange={(next) => {
        // The dialog stays while its command runs: a refusal comes back to it.
        if (!next && !pending) {
          closeFork();
        }
      }}
    >
      <DialogContent>
        <DialogHeader showCloseButton={false}>
          <DialogTitle>{`Fork ${row.name}`}</DialogTitle>
          <DialogDescription>{forkText(row)}</DialogDescription>
        </DialogHeader>
        <ForkForm row={row} />
      </DialogContent>
    </Dialog>
  );
};

/** "What each agent sees": one row per `fork_note`, its agent's logo and name and the note. */
const NotesDialog = ({ notes }: { notes: ForkNoteEvent[] }) => {
  const configurations = useAppStore((s) => s.inventory?.configurations);
  return (
    <Dialog
      open
      onOpenChange={(next) => {
        if (!next) {
          closeNotes();
        }
      }}
    >
      <DialogContent aria-describedby={undefined}>
        <DialogHeader showCloseButton={false}>
          <DialogTitle>What each agent sees</DialogTitle>
        </DialogHeader>
        <ul
          aria-label="What each agent sees"
          className="m-0 flex list-none flex-col gap-4 p-0"
        >
          {notes.map((note) => {
            const client = configurations?.find(
              (c) => c.id === note.configuration
            ) ?? { client: note.configuration, name: note.configuration };
            return (
              <li key={note.configuration} className="flex items-start gap-3">
                <AgentLogo client={client} size={20} decorative />
                <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <span className="type-body text-text-primary">
                    {client.name}
                  </span>
                  <span className="type-secondary text-text-muted">
                    {note.note}
                  </span>
                </span>
              </li>
            );
          })}
        </ul>
        <DialogFooter>
          <Button variant="primary" onClick={closeNotes}>
            Done
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};

/** The Fork dialog while one is open, and the notes of a plugin's skill just forked. */
export const ForkDialogs = () => {
  const forking = useForkStore((s) => s.forking);
  const notes = useForkStore((s) => s.notes);
  return (
    <>
      {forking !== undefined && <ForkDialog key={forking.key} row={forking} />}
      {notes !== undefined && <NotesDialog notes={notes} />}
    </>
  );
};
