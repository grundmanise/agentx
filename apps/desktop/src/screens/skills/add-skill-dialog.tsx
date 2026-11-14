// The Add skill dialog, which Add in the Skills header opens: a link to one skill, which
// `skill add <link>` installs in every enabled agent, adding its source first when this machine
// does not have it (docs/help-center/cli/skill.mdx). The skill is selected once the inventory that
// follows lists it; a link the CLI refuses shows the refusal under the field.
import { useId, useRef, useState } from "react";
import type { RefObject } from "react";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type { CliEvent } from "#/cli/events.gen.ts";
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
import { doctorRule } from "#/lib/app-hint.ts";
import type { HintRule } from "#/lib/app-hint.ts";
import { openSkill } from "#/screens/skills/open.ts";
import { shortSource } from "#/screens/skills/projection.ts";
import { linkProblem } from "#/screens/sources/add-source-dialog.tsx";
import { count } from "#/screens/sources/projection.ts";
import { runCommand, usePending } from "#/store/runner.ts";
import { afterSettled } from "#/store/settle.ts";
import type { Problem } from "#/store/store.ts";
import { useAppStore } from "#/store/store.ts";

/** The pending label of `skill add` from this dialog. */
export const ADD_SKILL_LABEL = "add skill";

/**
 * The hints of `skill add` that name a flag or a command, in the app's words: a link to a folder
 * that holds several skills names them, and a link to one of their folders installs it.
 */
const addSkillHintRules: HintRule[] = [
  {
    app: ({ names }) => `link to one skill's folder: ${names ?? ""}`,
    cli: /^name one with --name, or take them all with --all: (?<names>.+)$/u,
  },
  {
    app: () => "see what the source holds under Sources",
    cli: /^run 'agentx source skills[^']*' to see what (?:it|the source) holds$/u,
  },
  doctorRule,
];

/** The names of the skills the run installed, from its `library_skill` events. */
const installedNames = (events: CliEvent[]) =>
  events.flatMap((e) => (e.type === "library_skill" ? [e.name] : []));

/** The success toast: the skill the run installed and its source, as the events name them. */
const addedText = (events: CliEvent[]) => {
  const names = installedNames(events);
  const source = events.find((e) => e.type === "source");
  const what =
    names.length === 1 ? `${names[0]} skill` : count(names.length, "skill");
  return source === undefined
    ? `Added ${what}`
    : `Added ${what} from ${shortSource(source.url)}`;
};

/** The field, its hint, what went wrong under it, and the buttons. Mounted while the dialog is open. */
const AddSkillForm = ({
  linkField,
  onClose,
}: {
  linkField: RefObject<HTMLInputElement | null>;
  onClose: () => void;
}) => {
  const [link, setLink] = useState("");
  const [problem, setProblem] = useState<Problem | undefined>();
  // What was typed when it was refused: it keeps its case when it starts the CLI's words.
  const [refused, setRefused] = useState("");
  // Add is pending while `skill add` runs and, once it installed the skill, until the inventory
  // that follows lists it. A refusal changed nothing, so it waits for no inventory.
  const running = usePending(ADD_SKILL_LABEL);
  const [selecting, setSelecting] = useState(false);
  const pending = running || selecting;
  const hintId = useId();

  const submit = async () => {
    const typed = link.trim();
    if (
      typed === "" ||
      pending ||
      ADD_SKILL_LABEL in useAppStore.getState().pending
    ) {
      return;
    }
    setProblem(undefined);
    const outcome = await runCommand(["skill", "add", typed], {
      label: ADD_SKILL_LABEL,
      ownFailure: true,
      success: addedText,
    });
    if (outcome.exitCode !== EXIT_CODES.ok) {
      // Another command holding the lock is the runner's toast, with its Retry.
      if (outcome.exitCode !== EXIT_CODES.locked && !outcome.cancelled) {
        setProblem(
          linkProblem(outcome, {
            command: "skill add",
            rules: addSkillHintRules,
            typed,
          })
        );
        setRefused(typed);
      }
      return;
    }
    // The skill is selected from the inventory that follows, never from the command's result.
    const [added] = installedNames(outcome.events);
    setSelecting(true);
    afterSettled(() => {
      onClose();
      if (added !== undefined) {
        openSkill(added);
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
        <Input
          ref={linkField}
          kind="mono"
          aria-label="Link to a skill"
          placeholder="Link to a skill"
          aria-describedby={hintId}
          aria-invalid={problem === undefined ? undefined : true}
          value={link}
          onChange={(e) => {
            setLink(e.target.value);
            setProblem(undefined);
          }}
        />
        <p id={hintId} className="type-secondary text-text-hint m-0">
          A GitHub or GitLab link to the skill&apos;s folder or its SKILL.md
        </p>
        {problem !== undefined && (
          <ProblemText problem={problem} keep={[refused]} />
        )}
      </div>
      <DialogFooter>
        <DialogClose asChild>
          <Button variant="ghost">Cancel</Button>
        </DialogClose>
        <Button
          type="submit"
          variant="primary"
          disabled={link.trim() === ""}
          aria-busy={pending ? true : undefined}
        >
          {pending && <Spinner />}
          Add
        </Button>
      </DialogFooter>
    </form>
  );
};

/**
 * The Add skill dialog. The form is mounted while the dialog is open, so it opens clean each time,
 * with the field focused.
 */
export const AddSkillDialog = ({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) => {
  const linkField = useRef<HTMLInputElement>(null);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        onOpenAutoFocus={(e) => {
          e.preventDefault();
          linkField.current?.focus();
        }}
      >
        <DialogHeader>
          <DialogTitle>Add skill</DialogTitle>
          <DialogDescription>
            Adds the skill to your agents, and its source if it is new.
          </DialogDescription>
        </DialogHeader>
        <AddSkillForm
          linkField={linkField}
          onClose={() => {
            onOpenChange(false);
          }}
        />
      </DialogContent>
    </Dialog>
  );
};
