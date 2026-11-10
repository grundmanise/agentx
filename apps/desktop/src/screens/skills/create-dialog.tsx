// The Create skill dialog: a name and a one-line description, which `skill new` turns into a
// skill of your own, placed in every enabled agent (docs/help-center/cli/fork.mdx). The Skills and
// Add skills headers open it. Its Create and edit selects the new skill once the inventory that
// follows lists it, and opens its SKILL.md in the editor the user chose (Settings, Editor); a name
// the CLI refuses (exit 6: taken, or outside the grammar) shows its message under Name.
import { useId, useRef, useState } from "react";
import type { RefObject } from "react";

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
import { doctorRule, rewriteHint } from "#/lib/app-hint.ts";
import { openInEditor } from "#/lib/editor.ts";
import {
  breaksSkillNameRule,
  isSkillName,
  SKILL_NAME_RULE,
} from "#/lib/skill-name.ts";
import { skillFile } from "#/screens/skills/edit.ts";
import { openSkill } from "#/screens/skills/open.ts";
import type { Outcome } from "#/store/runner.ts";
import { runCommand, usePending } from "#/store/runner.ts";
import { afterSettled } from "#/store/settle.ts";
import type { Problem } from "#/store/store.ts";
import { useAppStore } from "#/store/store.ts";

/** The pending label of `skill new` from this dialog. */
export const CREATE_SKILL_LABEL = "create skill";

/**
 * The longest description `skill new` takes. The Agent Skills specification gives a description
 * 1 to 1024 characters; the CLI takes one line of at most 1024 bytes and has the last word.
 */
export const DESCRIPTION_LIMIT = 1024;

/** What a description says, as the field states it under itself. */
export const DESCRIPTION_HINT = `What it does and when to use it, in one or two sentences. Up to ${DESCRIPTION_LIMIT} characters.`;

const graphemes = new Intl.Segmenter(undefined, { granularity: "grapheme" });

/** Whether a description is longer than the limit, counted in characters as people see them, not bytes. */
const overDescriptionLimit = (description: string) => {
  let count = 0;
  for (const _ of graphemes.segment(description.trim())) {
    count += 1;
    if (count > DESCRIPTION_LIMIT) {
      return true;
    }
  }
  return false;
};

/** Where a refusal shows: a refused name under Name, anything else under the fields. */
interface Refusal {
  field: "name" | "form";
  problem: Problem;
  /** The name typed when it was refused: it keeps its case when it starts the CLI's words. */
  name: string;
}

/** The `skill new` command line: the description only when one is given. */
const createArgs = (name: string, description: string) =>
  description === ""
    ? ["skill", "new", name]
    : ["skill", "new", name, "--description", description];

/**
 * What went wrong with the skill `name`, in the CLI's words, a hint that names a command put in
 * the app's words.
 */
const refusalOf = (outcome: Outcome, name: string): Refusal => {
  const error = outcome.events.find((e) => e.type === "error");
  const message =
    error?.message ??
    outcome.startError ??
    `agentx skill new exited with code ${outcome.exitCode ?? "none"}`;
  const hint = rewriteHint(error?.hint, [doctorRule]);
  return {
    field: outcome.exitCode === EXIT_CODES.refused ? "name" : "form",
    name,
    problem: hint === undefined ? { message } : { hint, message },
  };
};

/** The fields, what went wrong under them, and the buttons. Mounted while the dialog is open. */
const CreateSkillForm = ({
  nameField,
  onClose,
}: {
  nameField: RefObject<HTMLInputElement | null>;
  onClose: () => void;
}) => {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [refusal, setRefusal] = useState<Refusal | undefined>();
  // Create is pending while `skill new` runs and, once it created the skill, until the inventory
  // that follows lists it. A refusal changed nothing, so it waits for no inventory.
  const running = usePending(CREATE_SKILL_LABEL);
  const [selecting, setSelecting] = useState(false);
  const pending = running || selecting;
  const nameId = useId();
  const nameHintId = useId();
  const descriptionId = useId();
  const descriptionHintId = useId();
  const tooLong = overDescriptionLimit(description);
  const broken = breaksSkillNameRule(name);
  const nameRefused = refusal?.field === "name";

  const submit = async () => {
    if (
      pending ||
      !isSkillName(name) ||
      tooLong ||
      CREATE_SKILL_LABEL in useAppStore.getState().pending
    ) {
      return;
    }
    setRefusal(undefined);
    const outcome = await runCommand(createArgs(name, description.trim()), {
      label: CREATE_SKILL_LABEL,
      ownFailure: true,
      success: (events) =>
        `Created ${events.find((e) => e.type === "library_skill")?.name ?? name} skill`,
    });
    if (outcome.exitCode !== EXIT_CODES.ok) {
      // Another command holding the lock is the runner's toast, with its Retry.
      if (outcome.exitCode !== EXIT_CODES.locked && !outcome.cancelled) {
        setRefusal(refusalOf(outcome, name));
      }
      return;
    }
    const created =
      outcome.events.find((e) => e.type === "library_skill")?.name ?? name;
    // The skill is selected from the inventory that follows, never from the command's result, and
    // its SKILL.md opens then, where the library says it is.
    setSelecting(true);
    afterSettled(() => {
      onClose();
      openSkill(created);
      const library = useAppStore.getState().machine?.library;
      if (library !== undefined) {
        void openInEditor(skillFile(library, created));
      }
    });
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
        <label htmlFor={nameId} className="type-secondary text-text-secondary">
          Name
        </label>
        <Input
          ref={nameField}
          id={nameId}
          kind="mono"
          placeholder="my-skill"
          aria-describedby={nameHintId}
          aria-required
          aria-invalid={broken || nameRefused ? true : undefined}
          value={name}
          onChange={(e) => {
            setName(e.target.value);
            if (nameRefused) {
              setRefusal(undefined);
            }
          }}
        />
        <div id={nameHintId}>
          {nameRefused ? (
            <ProblemText problem={refusal.problem} keep={[refusal.name]} />
          ) : (
            <p
              className={
                broken
                  ? "type-secondary text-tone-danger-fg m-0"
                  : "type-secondary text-text-hint m-0"
              }
            >
              {SKILL_NAME_RULE}
            </p>
          )}
        </div>
      </div>
      <div className="gap-input-label flex flex-col">
        <label
          htmlFor={descriptionId}
          className="type-secondary text-text-secondary"
        >
          Description
        </label>
        <Input
          id={descriptionId}
          placeholder="What the skill is for, in one line"
          aria-describedby={descriptionHintId}
          aria-invalid={tooLong ? true : undefined}
          value={description}
          onChange={(e) => {
            setDescription(e.target.value);
          }}
        />
        <p
          id={descriptionHintId}
          className={
            tooLong
              ? "type-secondary text-tone-danger-fg m-0"
              : "type-secondary text-text-hint m-0"
          }
        >
          {DESCRIPTION_HINT}
        </p>
        {refusal?.field === "form" && (
          <ProblemText problem={refusal.problem} keep={[refusal.name]} />
        )}
      </div>
      <DialogFooter>
        <DialogClose asChild>
          <Button variant="ghost">Cancel</Button>
        </DialogClose>
        <Button
          type="submit"
          variant="primary"
          disabled={!isSkillName(name) || tooLong}
          aria-busy={pending ? true : undefined}
        >
          {pending && <Spinner />}
          Create and edit
        </Button>
      </DialogFooter>
    </form>
  );
};

/**
 * The Create skill dialog. The form is mounted while the dialog is open, so it opens clean each
 * time, with the Name field focused.
 */
export const CreateSkillDialog = ({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) => {
  const nameField = useRef<HTMLInputElement>(null);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        onOpenAutoFocus={(e) => {
          e.preventDefault();
          nameField.current?.focus();
        }}
      >
        <DialogHeader>
          <DialogTitle>Create skill</DialogTitle>
          <DialogDescription>
            Creates a skill of your own in your library and adds it to your
            agents.
          </DialogDescription>
        </DialogHeader>
        <CreateSkillForm
          nameField={nameField}
          onClose={() => {
            onOpenChange(false);
          }}
        />
      </DialogContent>
    </Dialog>
  );
};
