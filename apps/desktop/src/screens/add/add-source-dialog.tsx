import { useState } from "react";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type { SourceEvent } from "#/cli/events.gen.ts";
import { Spinner } from "#/components/spinner.tsx";
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
import { count } from "#/screens/add/projection.ts";
import { runCommand, usePending } from "#/store/runner.ts";
import type { Problem } from "#/store/store.ts";
import { useAppStore } from "#/store/store.ts";

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
  const pending = usePending(ADD_SOURCE_LABEL);

  const submit = async () => {
    const typed = url.trim();
    if (typed === "" || ADD_SOURCE_LABEL in useAppStore.getState().pending) {
      return;
    }
    setProblem(undefined);
    const outcome = await runCommand(["source", "add", typed], {
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
    const error = outcome.events.find((e) => e.type === "error");
    const message =
      outcome.exitCode === EXIT_CODES.source
        ? `Couldn't reach ${typed}`
        : (error?.message ??
          outcome.startError ??
          `agentx source add exited with code ${outcome.exitCode ?? "none"}`);
    setProblem(
      error?.hint === undefined ? { message } : { hint: error.hint, message }
    );
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
