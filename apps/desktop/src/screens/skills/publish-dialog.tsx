// Publishing a skill (docs/help-center/cli/skill.mdx, "Publish a skill"): the Publish dialog, the
// banner buttons that open it, and the toast each outcome of `skill publish` gives. The button
// reads the changed files with `skill diff <name>` as it opens the dialog, and Publish runs `skill
// publish <name>`, with `--message <text>` when What changed is filled, showing the run's progress
// until it ends. The toast follows the `publish` event's outcome and the exit code; a failure the
// toast does not name keeps the runner's error toast, the CLI's message then its hint as one
// sentence-cased paragraph, the skill's name keeping its case.
import { useId, useRef, useState } from "react";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type { DiffEvent, ProgressEvent } from "#/cli/events.gen.ts";
import { Spinner } from "#/components/spinner.tsx";
import { notify } from "#/components/toast.tsx";
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
import { navigateTo } from "#/lib/navigate.ts";
import { accountRemoteOf } from "#/screens/skills/banners.ts";
import { fileStatusLabels } from "#/screens/skills/files.tsx";
import { shortSource } from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { runCommand } from "#/store/runner.ts";
import type { Outcome } from "#/store/runner.ts";
import { runUntilSettled, useSettling } from "#/store/settle.ts";
import { addActivity, useAppStore } from "#/store/store.ts";

/** The label of the publish of `name`, for its pending state. */
export const publishLabel = (name: string) => `publish ${name}`;

const updateLabel = (name: string) => `update ${name}`;

/** Update: `skill update <name>`, which takes in what changed somewhere else. */
const update = (name: string) => {
  void runUntilSettled(["skill", "update", name], {
    label: updateLabel(name),
    success: `Updated ${name}`,
  });
};

/** The command line of a publish: `--message` only when What changed holds more than spaces. */
export const publishArgs = (name: string, message: string) => {
  const text = message.trim();
  return text === ""
    ? ["skill", "publish", name]
    : ["skill", "publish", name, "--message", text];
};

/**
 * The toast of a publish that ended, when the outcome has one of its own. A failure with none
 * keeps the toast the runner showed. `accountRemoteSet` is whether the store knows of one.
 */
const reportPublish = (
  name: string,
  outcome: Outcome,
  accountRemoteSet: boolean
) => {
  const event = outcome.events.find(
    (e) => e.type === "publish" && e.name === name
  );
  const result = event?.type === "publish" ? event.outcome : undefined;
  if (result === "behind" || result === "moved") {
    const text = `${name} changed somewhere else first. Update it, then publish.`;
    notify(text, {
      label: "Update",
      onClick: () => {
        update(name);
      },
    });
    // A refused publish is in the activity list already, in the CLI's words.
    if (outcome.exitCode === EXIT_CODES.ok) {
      addActivity("attention", text);
    }
    return;
  }
  if (outcome.exitCode === EXIT_CODES.ok) {
    const text =
      result === "up to date"
        ? `Nothing to publish in ${name}`
        : `Published ${name}`;
    notify(text);
    addActivity("success", text);
    return;
  }
  const error = outcome.events.find((e) => e.type === "error");
  if (
    outcome.exitCode === EXIT_CODES.refused &&
    event === undefined &&
    (!accountRemoteSet ||
      (error?.type === "error" && error.message === "no account remote is set"))
  ) {
    notify("Set an account remote to publish", {
      label: "Open Settings",
      onClick: () => {
        navigateTo("/settings");
      },
    });
  }
};

/** What the run is doing, from its last `progress` event. */
const progressText = (name: string, progress: ProgressEvent | undefined) => {
  if (progress?.phase === "fetch" && progress.subject !== undefined) {
    return `Fetching ${shortSource(progress.subject)}…`;
  }
  return `Publishing ${name}…`;
};

/** The changed files `skill diff <name>` reports, read when the dialog opens. */
type FilesState =
  | { kind: "reading" }
  | { kind: "read"; diffs: DiffEvent[] }
  | { kind: "failed" };

const ChangedFiles = ({ files }: { files: FilesState }) => {
  if (files.kind === "reading") {
    return (
      <p className="type-secondary text-text-hint m-0">Reading the files…</p>
    );
  }
  if (files.kind === "failed") {
    return (
      <p className="type-secondary text-text-hint m-0">
        The changed files could not be read
      </p>
    );
  }
  if (files.diffs.length === 0) {
    return (
      <p className="type-secondary text-text-hint m-0">No changed files</p>
    );
  }
  return (
    <ul
      aria-label="Changed files"
      className="m-0 flex list-none flex-col gap-1.5 p-0"
    >
      {files.diffs.map((diff) => (
        <li key={diff.path} className="flex items-baseline gap-3">
          <span className="type-technical text-text-secondary min-w-0 flex-1 truncate">
            {diff.path}
          </span>
          <span className="type-technical-sm text-file-row-edited-label flex-none">
            {fileStatusLabels[diff.status]}
          </span>
        </li>
      ))}
    </ul>
  );
};

/** `skill diff <name>`, the read of the files a publish would record. */
const readChanged = async (name: string): Promise<FilesState> => {
  const outcome = await runCommand(["skill", "diff", name], {
    label: `publish diff ${name}`,
  });
  return outcome.exitCode === EXIT_CODES.ok
    ? { diffs: outcome.events.filter((e) => e.type === "diff"), kind: "read" }
    : { kind: "failed" };
};

/** The dialog's body, mounted while it is open, so that it starts clean each time. */
const PublishForm = ({
  row,
  files,
  onClose,
}: {
  row: SkillRow;
  files: FilesState;
  onClose: () => void;
}) => {
  const { name } = row;
  const fieldId = useId();
  const [message, setMessage] = useState("");
  const [progress, setProgress] = useState<ProgressEvent | undefined>();
  const pending = useSettling(publishLabel(name));
  const [running, setRunning] = useState(false);

  const publish = async () => {
    if (running || pending) {
      return;
    }
    setRunning(true);
    setProgress(undefined);
    const outcome = await runUntilSettled(publishArgs(name, message), {
      keep: [name],
      label: publishLabel(name),
      onProgress: setProgress,
    });
    reportPublish(
      name,
      outcome,
      accountRemoteOf(useAppStore.getState().sources) !== undefined
    );
    onClose();
  };

  return (
    <form
      className="gap-dialog flex flex-col"
      onSubmit={(e) => {
        e.preventDefault();
        void publish();
      }}
    >
      <div className="gap-input-label flex flex-col">
        <label htmlFor={fieldId} className="type-secondary text-text-muted">
          What changed
        </label>
        <Input
          id={fieldId}
          placeholder="Leave empty and agentx describes the files"
          value={message}
          disabled={running}
          onChange={(e) => {
            setMessage(e.target.value);
          }}
        />
      </div>
      <section aria-label="Files" className="flex flex-col gap-2.5">
        <h3 className="type-caption text-text-caption m-0">Files</h3>
        <ChangedFiles files={files} />
      </section>
      <DialogFooter>
        {running && (
          <output className="type-secondary text-text-hint flex min-w-0 flex-1 items-center">
            {progressText(name, progress)}
          </output>
        )}
        <DialogClose asChild>
          <Button variant="ghost">Cancel</Button>
        </DialogClose>
        <Button
          type="submit"
          variant="primary"
          disabled={running}
          aria-busy={running ? true : undefined}
        >
          {running && <Spinner />}
          Publish
        </Button>
      </DialogFooter>
    </form>
  );
};

/** Where the publish goes: the account remote for one of your own, else the skill's source. */
const Subtitle = ({ row }: { row: SkillRow }) => {
  const accountRemote = useAppStore((s) => accountRemoteOf(s.sources));
  if (row.kind === "yours") {
    return (
      <DialogDescription>
        {`To your account remote, ${accountRemote?.url ?? ""}`}
      </DialogDescription>
    );
  }
  return (
    <DialogDescription>
      {`To ${shortSource(row.entry?.source ?? "")}, as a change under your own Git name`}
    </DialogDescription>
  );
};

/** The Publish dialog: "Publish {skill}", where it goes, What changed, the files, Cancel · Publish. */
const PublishDialog = ({
  row,
  files,
  open,
  onOpenChange,
}: {
  row: SkillRow;
  files: FilesState;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) => (
  <Dialog open={open} onOpenChange={onOpenChange}>
    <DialogContent>
      <DialogHeader showCloseButton={false}>
        <DialogTitle>{`Publish ${row.name}`}</DialogTitle>
        <Subtitle row={row} />
      </DialogHeader>
      <PublishForm
        row={row}
        files={files}
        onClose={() => {
          onOpenChange(false);
        }}
      />
    </DialogContent>
  </Dialog>
);

/** A banner's button that opens the Publish dialog: Publish, or Publish to {source}. */
export const PublishButton = ({
  row,
  label,
}: {
  row: SkillRow;
  label: string;
}) => {
  const [open, setOpen] = useState(false);
  const [files, setFiles] = useState<FilesState>({ kind: "reading" });
  // The read of the latest opening: an earlier one that ends later does not replace it.
  const opening = useRef(0);
  const pending = useSettling(publishLabel(row.name));
  const openDialog = async () => {
    opening.current += 1;
    const mine = opening.current;
    setFiles({ kind: "reading" });
    setOpen(true);
    const read = await readChanged(row.name);
    if (opening.current === mine) {
      setFiles(read);
    }
  };
  return (
    <>
      <Button
        variant="ghost"
        size="sm"
        disabled={pending}
        aria-busy={pending ? true : undefined}
        onClick={() => {
          void openDialog();
        }}
      >
        {pending && <Spinner />}
        {label}
      </Button>
      <PublishDialog
        row={row}
        files={files}
        open={open}
        onOpenChange={setOpen}
      />
    </>
  );
};

/** Open Settings, where an account remote is set. */
export const OpenSettingsButton = () => (
  <Button
    variant="ghost"
    size="sm"
    onClick={() => {
      navigateTo("/settings");
    }}
  >
    Open Settings
  </Button>
);
