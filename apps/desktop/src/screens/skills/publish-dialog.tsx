// Publishing a skill (docs/help-center/cli/skill.mdx, "Publish a skill"): the Publish dialog, the
// banner buttons that open it, and the toast each outcome of `skill publish` gives. The button
// reads the changed files with `skill diff <name>` as it opens the dialog, and Publish runs `skill
// publish <name>`, with `--message <text>` when What changed is filled, showing the run's progress
// until it ends. The toast follows the `publish` event's outcome and the exit code; a failure the
// toast does not name keeps the runner's error toast, the CLI's message then its hint as one
// sentence-cased paragraph, the skill's name keeping its case, and the hint in the app's words
// where the CLI's names a command (`publishFailure`).
import { useId, useRef, useState } from "react";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type { DiffEvent, ProgressEvent } from "#/cli/events.gen.ts";
import { Spinner } from "#/components/spinner.tsx";
import { notify } from "#/components/toast.tsx";
import type { ErrorToast } from "#/components/toast.tsx";
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
import { doctorRule, againRule, rewriteHint } from "#/lib/app-hint.ts";
import type { HintRule } from "#/lib/app-hint.ts";
import { navigateTo } from "#/lib/navigate.ts";
import { accountRemoteOf } from "#/screens/skills/banners.ts";
import { fileStatusLabels } from "#/screens/skills/files.tsx";
import { shortSource } from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import {
  alreadyPublished,
  changedElsewhere,
  notifyNoAccountRemote,
  refusedForAccountRemote,
} from "#/screens/skills/publish.ts";
import { addSource } from "#/screens/skills/updates.ts";
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
    success: `Updated ${name} skill`,
  });
};

/** The hints of `skill publish` that name a command, matched whole, and the app's words for each. */
const publishHintRules: HintRule[] = [
  {
    app: () => "add the source again, then publish again",
    cli: /^run 'agentx source add .+' to add it again, then publish again$/u,
  },
  {
    app: () => "publish again to publish it as it is now",
    cli: /^run 'agentx skill publish .+' again to publish it as it is now$/u,
  },
  {
    app: () =>
      "use Check updates to record that, or Remove to remove the skill",
    cli: /^run 'agentx skill check-updates' to record that, or 'agentx skill remove .+' to remove it$/u,
  },
  {
    app: ({ why }) => `update the skill to ${why ?? ""}, then publish again`,
    cli: /^run 'agentx skill update .+' to (?<why>take the change in|get them), then publish again$/u,
  },
  {
    app: () =>
      "add the source again with the branch to publish to after a #, then update the skill from it and publish again",
    cli: /^add the source at the branch to publish to with 'agentx source add .+#<branch>', then update the skill from it and publish again$/u,
  },
  {
    app: ({ first }) => `${first ?? ""}, then publish again`,
    cli: /^(?<first>.+), then run 'agentx skill publish .+' again$/u,
  },
  doctorRule,
  againRule("publish again"),
];

/**
 * The error toast of a failed publish whose hint names a command: the CLI's message and its
 * warnings, the hint in the app's words, and Add source for a skill whose source was removed
 * (exit 5). Undefined leaves the runner's own toast.
 */
const publishFailure = (
  row: SkillRow,
  outcome: Outcome
): ErrorToast | undefined => {
  const error = outcome.events.find((e) => e.type === "error");
  const hint = rewriteHint(error?.hint, publishHintRules);
  const source = row.entry?.source;
  const action =
    outcome.exitCode === EXIT_CODES.not_found && source !== undefined
      ? { action: addSource(source) }
      : {};
  return error === undefined || hint === error.hint
    ? undefined
    : {
        ...action,
        hint,
        message: error.message,
        warnings: outcome.logs
          .filter((l) => l.level === "warn")
          .map((l) => l.message),
      };
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
  row: SkillRow,
  outcome: Outcome,
  accountRemoteSet: boolean
) => {
  const { name } = row;
  const event = outcome.events.find(
    (e) => e.type === "publish" && e.name === name
  );
  const result = event?.type === "publish" ? event.outcome : undefined;
  if (result === "behind" || result === "moved") {
    // A skill of a source is published to its source, which holds the newer version.
    const text = changedElsewhere(
      name,
      row.kind === "source" ? row.entry?.source : undefined
    );
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
  // Nothing was sent: the skill already matches the published version, which is not a success.
  if (outcome.exitCode === EXIT_CODES.ok && result === "up to date") {
    const text = alreadyPublished(name);
    notify(text);
    addActivity("attention", text);
    return;
  }
  if (outcome.exitCode === EXIT_CODES.ok) {
    const text = `Published ${name} skill`;
    notify(text);
    addActivity("success", text);
    return;
  }
  if (refusedForAccountRemote(outcome, accountRemoteSet)) {
    notifyNoAccountRemote();
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
      failure: (ended) => publishFailure(row, ended),
      keep: [name],
      label: publishLabel(name),
      onProgress: setProgress,
    });
    reportPublish(
      row,
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
