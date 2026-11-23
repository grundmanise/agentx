import { useState } from "react";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type { SourceEvent } from "#/cli/events.gen.ts";
import { ProblemText } from "#/components/problem-text.tsx";
import { Button } from "#/components/ui/button.tsx";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "#/components/ui/dialog.tsx";
import { Input } from "#/components/ui/input.tsx";
import { Spinner } from "#/components/ui/spinner.tsx";
import { rewriteHint } from "#/lib/app-hint.ts";
import type { HintRule } from "#/lib/app-hint.ts";
import { count, HUB_LABEL } from "#/screens/sources/projection.ts";
import { agentx, usePending } from "#/store/runner.ts";
import type { Outcome } from "#/store/runner.ts";
import type { Problem } from "#/store/store.ts";
import { useAppStore } from "#/store/store.ts";

/**
 * The hints of `source add` that name a command, in the app's words: the hub's skills
 * are the ones under Hub.
 */
export const addSourceHintRules: HintRule[] = [
  {
    app: () => `see and install its skills under ${HUB_LABEL}`,
    cli: /^run 'agentx skill list --remote' to see its skills, then 'agentx skill add --name <name>' to install one$/u,
  },
];

/**
 * What went wrong with a command run on a link the user typed, such as `source add`, in
 * the CLI's words: a source git cannot reach as "Couldn't reach {link}", and the CLI's hint in the
 * app's words by `rules`.
 */
export const linkProblem = (
  outcome: Outcome,
  {
    command,
    rules,
    typed,
  }: { command: string; rules: readonly HintRule[]; typed: string }
): Problem => {
  const error = outcome.events.find((e) => e.type === "error");
  const message =
    outcome.exitCode === EXIT_CODES.source
      ? `Couldn't reach ${typed}`
      : (error?.message ??
        outcome.startError ??
        `agentx ${command} exited with code ${outcome.exitCode ?? "none"}`);
  const hint = rewriteHint(error?.hint, rules);
  return hint === undefined ? { message } : { hint, message };
};

/** The pending label of `source add` from this dialog. */
export const ADD_SOURCE_LABEL = "add source";

/** The field, its hint, what went wrong under it, and the buttons. Mounted while the dialog is open. */
const AddSourceForm = ({
  onAdded,
  onClose,
}: {
  onAdded?: (source: SourceEvent) => void;
  onClose: () => void;
}) => {
  const [url, setUrl] = useState("");
  const [problem, setProblem] = useState<Problem | undefined>();
  // What was typed when it was refused: it keeps its case when it starts the CLI's words.
  const [refused, setRefused] = useState("");
  const pending = usePending(ADD_SOURCE_LABEL);

  const submit = async () => {
    const typed = url.trim();
    if (typed === "" || ADD_SOURCE_LABEL in useAppStore.getState().pending) {
      return;
    }
    setProblem(undefined);
    const outcome = await agentx(["source", "add", typed], {
      label: ADD_SOURCE_LABEL,
      ownFailure: true,
      success: (events) => {
        const source = events.find((e) => e.type === "source");
        return `Added ${source?.url ?? typed}: ${count(source?.skills ?? 0, "skill")}`;
      },
    });
    if (outcome.exitCode === EXIT_CODES.ok) {
      const source = outcome.events.find((e) => e.type === "source");
      if (source !== undefined) {
        onAdded?.(source);
      }
      onClose();
      return;
    }
    setProblem(
      linkProblem(outcome, {
        command: "source add",
        rules: addSourceHintRules,
        typed,
      })
    );
    setRefused(typed);
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
          kind="mono"
          aria-label="Git URL or owner/name"
          placeholder="Git URL or owner/name"
          aria-invalid={problem === undefined ? undefined : true}
          value={url}
          onChange={(e) => {
            setUrl(e.target.value);
          }}
        />
        <p className="type-secondary text-text-hint m-0">
          Add #branch or #tag to pin it
        </p>
        {problem !== undefined && (
          <ProblemText problem={problem} keep={[refused]} />
        )}
      </div>
      <DialogFooter>
        <DialogClose asChild>
          <Button variant="ghost">Cancel</Button>
        </DialogClose>
        <Button type="submit" variant="primary" disabled={url.trim() === ""}>
          {pending && <Spinner />}
          Add
        </Button>
      </DialogFooter>
    </form>
  );
};

/**
 * The Add source dialog: a URL or `owner/name`, with `#<ref>` to pin it, which `source add`
 * fetches (docs/help-center/cli/source.mdx). A failure shows under the field, in the CLI's words:
 * a source git cannot reach as "Couldn't reach {url}" with the CLI's hint. The form is mounted
 * while the dialog is open, so it opens clean each time.
 */
export const AddSourceDialog = ({
  open,
  onOpenChange,
  onAdded,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The source the CLI added, from its `source` event. */
  onAdded?: (source: SourceEvent) => void;
}) => (
  <Dialog open={open} onOpenChange={onOpenChange}>
    <DialogContent aria-describedby={undefined}>
      <DialogHeader>
        <DialogTitle>Add source</DialogTitle>
      </DialogHeader>
      <AddSourceForm
        onAdded={onAdded}
        onClose={() => {
          onOpenChange(false);
        }}
      />
    </DialogContent>
  </Dialog>
);
