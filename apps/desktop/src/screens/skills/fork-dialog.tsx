// The Fork dialog. The `ForkButton` of the detail pane's action row opens it; `ForkDialogs`, which
// the Skills screen mounts once, shows it, so it outlives the detail pane of the skill forked,
// which the fork replaces in the selection. Fork runs `skill fork <name>`, or `--name <new>` with
// Use a different name on, and the screen redraws from the inventory that follows: the fork is
// selected by the name the CLI reports, and shows once that inventory holds it. For a plugin's
// skill the switch starts on, with `my-<name>` in the Name field; turned off, a line says that the
// agents with the plugin will see more than one skill of the name. A refusal (exit 6), such as a library
// entry that is a symlink, keeps the dialog open with the CLI's message and a hint in the dialog's
// own words (`forkHint`), as one sentence-cased paragraph (`ProblemText`), the skill's name and
// the new one keeping their case, and turns Use a different name on, since forking beside the
// skill is the way out the CLI names; the hint names what the form then leaves to do. A change to
// the switch or the name clears the refusal, which was about the form as it was.
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
import { Switch } from "#/components/ui/switch.tsx";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "#/components/ui/tooltip.tsx";
import {
  breaksSkillNameRule,
  isSkillName,
  SKILL_NAME_RULE,
} from "#/lib/skill-name.ts";
import {
  firstName,
  forkArgs,
  forkHint,
  forkTarget,
  forkText,
  formAfter,
  renamesAtFirst,
} from "#/screens/skills/fork.ts";
import type { ForkFields } from "#/screens/skills/fork.ts";
import { openSkill } from "#/screens/skills/open.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { usePending } from "#/store/runner.ts";
import type { Outcome } from "#/store/runner.ts";
import { runUntilSettled, useSettling } from "#/store/settle.ts";
import type { Problem } from "#/store/store.ts";

/** What the CLI said when it refused a fork, and the names its words may start with. */
interface Refusal {
  problem: Problem;
  /** The skill's name, and the new name typed, if any: each keeps its case. */
  keep: string[];
}

/**
 * The refusal of a fork of `target` that failed: the CLI's message, and its hint in the dialog's
 * words (`forkHint`), for the form as the refusal leaves it.
 */
const refusalOf = (
  outcome: Outcome,
  target: string,
  form: ForkFields,
  keep: string[]
): Refusal => {
  const error = outcome.events.find((e) => e.type === "error");
  const message =
    error?.message ??
    outcome.startError ??
    `agentx skill fork exited with code ${outcome.exitCode ?? "none"}`;
  const hint =
    error === undefined
      ? undefined
      : forkHint(error, outcome.exitCode, target, form);
  return {
    keep,
    problem: hint === undefined ? { message } : { hint, message },
  };
};

/** The label of the Fork control for `row`: the runner's name for it. */
export const forkLabel = (row: SkillRow) => `fork ${row.key}`;

interface ForkState {
  /** The skill the Fork dialog is open for. */
  forking?: SkillRow;
  /** False while the dialog closes: it keeps its skill until its close animation ends. */
  open: boolean;
}

const useForkStore = create<ForkState>()(() => ({ open: false }));

/** Opens the Fork dialog for `row`. */
const openFork = (row: SkillRow) => {
  useForkStore.setState({ forking: row, open: true });
};

const closeFork = () => {
  useForkStore.setState({ open: false });
};

/** Why Fork does nothing while the skill has a merge pending, which `skill fork` refuses. */
export const FORK_WHILE_MERGING = "Can't fork while update in progress";

/**
 * Fork, a secondary button in the detail pane's action row: opens the dialog. While the skill has
 * a merge pending, which `skill fork` refuses, it looks disabled and does nothing, and its Tooltip
 * says why; it stays focusable, so the keyboard reaches the Tooltip too.
 */
export const ForkButton = ({ row }: { row: SkillRow }) => {
  const pending = useSettling(forkLabel(row));
  const merging = row.entry?.pending_merge === true;
  const button = (
    <Button
      variant="secondary"
      size="sm"
      disabled={pending}
      aria-busy={pending ? true : undefined}
      aria-disabled={merging ? true : undefined}
      onClick={
        merging
          ? undefined
          : () => {
              openFork(row);
            }
      }
    >
      {pending && <Spinner />}
      Fork
    </Button>
  );
  if (!merging) {
    return button;
  }
  return (
    <Tooltip>
      <TooltipTrigger asChild>{button}</TooltipTrigger>
      <TooltipContent>{FORK_WHILE_MERGING}</TooltipContent>
    </Tooltip>
  );
};

/** The Fork dialog's form, mounted while it is open for `row`, so it opens clean each time. */
const ForkForm = ({ row }: { row: SkillRow }) => {
  const [renaming, setRenaming] = useState(() => renamesAtFirst(row));
  const [name, setName] = useState(() => firstName(row));
  const [refusal, setRefusal] = useState<Refusal | undefined>();
  const pending = usePending(forkLabel(row));
  const nameRef = useRef<HTMLInputElement>(null);
  const switchId = useId();
  const nameValid = isSkillName(name);

  // The field takes the focus when it shows: as the dialog opens for a plugin's skill, on the
  // switch, or when a refusal turns it on. Its name is selected, so typing replaces it.
  useEffect(() => {
    if (renaming) {
      nameRef.current?.select();
    }
  }, [renaming]);

  const submit = async () => {
    if (pending || (renaming && !nameValid)) {
      return;
    }
    setRefusal(undefined);
    const target = forkTarget(row);
    const outcome = await runUntilSettled(
      forkArgs(target, renaming ? name : undefined),
      {
        label: forkLabel(row),
        ownFailure: true,
        success: `Forked ${row.name} skill`,
      }
    );
    if (outcome.exitCode === EXIT_CODES.ok) {
      closeFork();
      const fork = outcome.events.find((e) => e.type === "library_skill");
      openSkill(fork?.name ?? (renaming ? name : row.name));
      return;
    }
    // Exit 4 with a conflict opens the skill whose merge is pending (src/store/runner.ts). One with
    // none, a fork refused for the merge pending, stays open with the refusal like any other.
    if (
      outcome.exitCode === EXIT_CODES.pending_merge &&
      outcome.events.some((e) => e.type === "conflict")
    ) {
      closeFork();
      return;
    }
    // Another command holds the lock: the runner's toast says so, with Retry.
    if (outcome.exitCode === EXIT_CODES.locked) {
      return;
    }
    setRefusal(
      refusalOf(
        outcome,
        target,
        formAfter(outcome.exitCode, renaming, name),
        renaming ? [row.name, name] : [row.name]
      )
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
          <span className="type-body text-text-primary min-w-0 flex-1">
            Use a different name
          </span>
          <Switch
            id={switchId}
            aria-label="Use a different name"
            checked={renaming}
            disabled={pending}
            onCheckedChange={(on) => {
              setRenaming(on);
              setRefusal(undefined);
            }}
          />
        </label>
        {!renaming && row.kind === "plugin" && (
          <p className="type-secondary text-text-muted m-0">
            {`Agents with the plugin will see more than one skill named ${row.name}.`}
          </p>
        )}
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
                setRefusal(undefined);
              }}
            />
            {breaksSkillNameRule(name) && (
              <p className="type-secondary text-text-hint m-0">
                {`Use ${SKILL_NAME_RULE}`}
              </p>
            )}
          </>
        )}
        {refusal !== undefined && (
          <ProblemText problem={refusal.problem} keep={refusal.keep} />
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

/** "Fork {skill}?": what the fork does for this kind of skill, Use a different name, Cancel · Fork. */
const ForkDialog = ({ row, open }: { row: SkillRow; open: boolean }) => {
  const pending = usePending(forkLabel(row));
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        // The dialog stays while its command runs: a refusal comes back to it.
        if (!next && !pending) {
          closeFork();
        }
      }}
    >
      <DialogContent>
        <DialogHeader showCloseButton={false}>
          <DialogTitle>{`Fork ${row.name}?`}</DialogTitle>
          <DialogDescription>{forkText(row)}</DialogDescription>
        </DialogHeader>
        <ForkForm row={row} />
      </DialogContent>
    </Dialog>
  );
};

/** The Fork dialog while one is open, and while it closes. */
export const ForkDialogs = () => {
  const forking = useForkStore((s) => s.forking);
  const open = useForkStore((s) => s.open);
  return forking === undefined ? null : (
    <ForkDialog key={forking.key} row={forking} open={open} />
  );
};
