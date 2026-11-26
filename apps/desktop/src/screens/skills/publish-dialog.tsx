// Publishing a skill (docs/help-center/cli/skill.mdx, "Publish a skill"): the Publish dialog, the
// banner buttons that open it, and the toast each outcome of `skill publish` gives. The button
// reads the changed files as it opens the dialog: `skill diff <name>`, or, for a skill never
// published, every file `skill files <name>` lists, since the publish sends them all. Publish runs
// `skill publish <name>`, with `--message <text>` when What changed holds text other than the
// summary it starts with, which says what the CLI's own message says; the run's progress shows
// until it ends. The toast follows the `publish` event's outcome and the exit code; a failure the
// toast does not name keeps the runner's error toast, the CLI's message then its hint as one
// sentence-cased paragraph, the skill's name keeping its case, and the hint in the app's words
// where the CLI's names a command (`publishFailure` in publish.ts).
import { useId, useRef, useState } from "react";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type { DiffEvent, ProgressEvent } from "#/cli/events.gen.ts";
import { notify, notifySuccess } from "#/components/toast.tsx";
import { Button } from "#/components/ui/button.tsx";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogStatus,
  DialogTitle,
} from "#/components/ui/dialog.tsx";
import { Input } from "#/components/ui/input.tsx";
import { Spinner } from "#/components/ui/spinner.tsx";
import { WithReason } from "#/components/with-reason.tsx";
import { navigateTo } from "#/lib/navigate.ts";
import {
  hubOf,
  notPublished,
  readOnlyHubReason,
} from "#/screens/skills/banners.ts";
import { fileStatusLabels } from "#/screens/skills/files.tsx";
import { shortSource } from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import {
  alreadyPublished,
  changedElsewhere,
  notifyNoHub,
  publishFailure,
  refusedForHub,
} from "#/screens/skills/publish.ts";
import { agentx, stopOnClose } from "#/store/runner.ts";
import type { Outcome } from "#/store/runner.ts";
import { runUntilSettled, useSettling } from "#/store/settle.ts";
import { sourcesOf, useAppStore } from "#/store/store.ts";

/** The label of the publish of `name`, for its pending state. */
export const publishLabel = (name: string) => `publish ${name}`;
/** The labels of the reads of the files a publish of `name` would send. */
const filesLabel = (name: string) => `publish files ${name}`;
const diffLabel = (name: string) => `publish diff ${name}`;

const updateLabel = (name: string) => `update ${name}`;

/** Update: `skill update <name>`, which takes in what changed somewhere else. */
const update = (name: string) => {
  void runUntilSettled(["skill", "update", name], {
    label: updateLabel(name),
    success: `Updated ${name} skill`,
  });
};

/**
 * The command line of a publish: `--message` only when What changed holds more than spaces and
 * more than `summary`, the text it starts with, for which the CLI writes its own message.
 */
export const publishArgs = (name: string, message: string, summary = "") => {
  const text = message.trim();
  return text === "" || text === summary.trim()
    ? ["skill", "publish", name]
    : ["skill", "publish", name, "--message", text];
};

/** What the CLI's message says a commit does to a file, by its status in `skill diff`. */
const summaryVerbs: Record<DiffEvent["status"], string> = {
  added: "add",
  deleted: "delete",
  modified: "edit",
};

/**
 * What What changed starts with: what the commit does to its first two files, and how many more
 * it changes, as the CLI's own message says it ("Edit SKILL.md, add examples.md and 3 more files").
 * Empty while the files are unread or none changed.
 */
export const changeSummary = (files: FilesState) => {
  if (files.kind !== "read" || files.diffs.length === 0) {
    return "";
  }
  const said = files.diffs
    .slice(0, 2)
    .map(
      (d) =>
        `${files.fresh === true ? "add" : summaryVerbs[d.status]} ${d.path}`
    );
  const more = files.diffs.length - 2;
  const text =
    more > 0
      ? `${said.join(", ")} and ${more === 1 ? "1 more file" : `${more} more files`}`
      : said.join(" and ");
  return `${text.charAt(0).toUpperCase()}${text.slice(1)}`;
};

/**
 * The toast of a publish that ended, when the outcome has one of its own. A failure with none
 * keeps the toast the runner showed. `hubSet` is whether the store knows of one.
 */
const reportPublish = (row: SkillRow, outcome: Outcome, hubSet: boolean) => {
  const { name } = row;
  const event = outcome.events.find(
    (e) => e.type === "publish" && e.name === name
  );
  const result = event?.type === "publish" ? event.outcome : undefined;
  if (result === "behind" || result === "moved") {
    // A skill of a source is published to its source, which holds the newer version.
    notify(
      changedElsewhere(
        name,
        row.kind === "source" ? row.entry?.source : undefined
      ),
      {
        label: "Update",
        onClick: () => {
          update(name);
        },
        track: "publish-toast.update",
      },
      { kind: "warning" }
    );
    return;
  }
  // Nothing was sent: the skill already matches the published version, which is not a success.
  if (outcome.exitCode === EXIT_CODES.ok && result === "up to date") {
    notify(alreadyPublished(name));
    return;
  }
  if (outcome.exitCode === EXIT_CODES.ok) {
    notifySuccess(`Published ${name} skill`);
    return;
  }
  if (refusedForHub(outcome, hubSet)) {
    notifyNoHub();
  }
};

/** What the run is doing, from its last `progress` event. */
const progressText = (name: string, progress: ProgressEvent | undefined) => {
  if (progress?.phase === "fetch" && progress.subject !== undefined) {
    return `Fetching ${shortSource(progress.subject)}…`;
  }
  return `Publishing ${name}…`;
};

/**
 * The files a publish sends, read when the dialog opens: the changed files `skill diff <name>`
 * reports, or, with `fresh`, every file of a skill never published, each new to the remote.
 */
export type FilesState =
  | { kind: "reading" }
  | { kind: "read"; diffs: DiffEvent[]; fresh?: boolean }
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
            {files.fresh === true ? "new" : fileStatusLabels[diff.status]}
          </span>
        </li>
      ))}
    </ul>
  );
};

/**
 * The read of the files a publish would record: `skill diff <name>`, which compares a skill never
 * published with the commit that created it, so a fresh fork reads as unchanged. Such a skill
 * reads every file `skill files <name>` lists but those git ignores, each added.
 */
const readChanged = async (row: SkillRow): Promise<FilesState> => {
  const { name } = row;
  if (notPublished(row)) {
    const outcome = await agentx(["skill", "files", name], {
      label: filesLabel(name),
      sideEffect: false,
    });
    if (outcome.exitCode !== EXIT_CODES.ok) {
      return { kind: "failed" };
    }
    const diffs = outcome.events.flatMap((e): DiffEvent[] =>
      e.type === "skill_file" && e.ignored !== true
        ? [
            {
              name,
              patch: "",
              path: e.path,
              schema_version: e.schema_version,
              status: "added",
              type: "diff",
            },
          ]
        : []
    );
    diffs.sort((a, b) => (a.path < b.path ? -1 : 1));
    return { diffs, fresh: true, kind: "read" };
  }
  const outcome = await agentx(["skill", "diff", name], {
    label: diffLabel(name),
    sideEffect: false,
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
  const summary = changeSummary(files);
  // Undefined until the user types: the field then shows the summary, once the files are read.
  const [typed, setTyped] = useState<string | undefined>();
  const message = typed ?? summary;
  const [progress, setProgress] = useState<ProgressEvent | undefined>();
  const pending = useSettling(publishLabel(name));
  const [running, setRunning] = useState(false);

  const publish = async () => {
    if (running || pending) {
      return;
    }
    setRunning(true);
    setProgress(undefined);
    const outcome = await runUntilSettled(publishArgs(name, message, summary), {
      failure: (ended) => publishFailure(ended, row.entry?.source),
      keep: [name],
      label: publishLabel(name),
      onProgress: setProgress,
    });
    reportPublish(
      row,
      outcome,
      hubOf(sourcesOf(useAppStore.getState())) !== undefined
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
        <label htmlFor={fieldId} className="type-secondary text-text-secondary">
          What changed
        </label>
        <Input
          id={fieldId}
          autoFocus
          placeholder="Describe your changes"
          value={message}
          disabled={running}
          onChange={(e) => {
            setTyped(e.target.value);
          }}
        />
      </div>
      <section aria-label="Files" className="flex flex-col gap-2.5">
        <h3 className="type-caption text-text-caption m-0">Files</h3>
        <ChangedFiles files={files} />
      </section>
      <DialogFooter>
        {running && <DialogStatus>{progressText(name, progress)}</DialogStatus>}
        <DialogClose asChild>
          <Button track="publish-dialog.cancel" variant="ghost">
            Cancel
          </Button>
        </DialogClose>
        <Button
          track="publish-dialog.confirm"
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

/** Where the publish goes: the hub for a hub skill, else the skill's source. */
const Subtitle = ({ row }: { row: SkillRow }) => {
  if (row.kind === "hub") {
    return (
      <DialogDescription>
        {`The ${row.name} skill will be published to your hub.`}
      </DialogDescription>
    );
  }
  return (
    <DialogDescription>
      {`The ${row.name} skill will be published to ${shortSource(row.entry?.source ?? "")}.`}
    </DialogDescription>
  );
};

/** The Publish dialog: "Publish changes", where they go, What changed, the files, Cancel · Publish. */
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
}) => {
  const close = stopOnClose(
    [publishLabel(row.name), filesLabel(row.name), diffLabel(row.name)],
    onOpenChange
  );
  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent>
        <DialogHeader showCloseButton={false}>
          <DialogTitle>Publish changes</DialogTitle>
          <Subtitle row={row} />
        </DialogHeader>
        <PublishForm
          row={row}
          files={files}
          onClose={() => {
            close(false);
          }}
        />
      </DialogContent>
    </Dialog>
  );
};

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
  // A hub skill cannot be published while the hub is read-only; a source's skill shows
  // Publish only while its source is writable.
  const readOnly = useAppStore((s) =>
    row.kind === "hub" ? readOnlyHubReason(sourcesOf(s)) : undefined
  );
  const openDialog = async () => {
    opening.current += 1;
    const mine = opening.current;
    setFiles({ kind: "reading" });
    setOpen(true);
    const read = await readChanged(row);
    if (opening.current === mine) {
      setFiles(read);
    }
  };
  return (
    <>
      <WithReason reason={readOnly}>
        <Button
          track="skill-banner.publish"
          variant="ghost"
          size="sm"
          disabled={pending}
          aria-busy={pending ? true : undefined}
          aria-disabled={readOnly === undefined ? undefined : true}
          onClick={
            readOnly === undefined
              ? () => {
                  void openDialog();
                }
              : undefined
          }
        >
          {pending && <Spinner />}
          {label}
        </Button>
      </WithReason>
      <PublishDialog
        row={row}
        files={files}
        open={open}
        onOpenChange={setOpen}
      />
    </>
  );
};

/** Open Settings, where a hub is set. */
export const OpenSettingsButton = () => (
  <Button
    track="skill-banner.open-settings"
    variant="ghost"
    size="sm"
    onClick={() => {
      navigateTo("/settings");
    }}
  >
    Open Settings
  </Button>
);
