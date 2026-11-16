// The Update skills dialog, which the sidebar's update pill and a check by hand that found an
// update open: every skill the last update check found an update for, but those with a pending
// merge, all selected, grouped by source. A row toggles its skill, a group's checkbox every skill
// of its source, and See changes opens the What's new sheet of its update above the dialog.
// Update runs in the dialog (`updates.ts`): each selected row shows its state, then its result,
// and the footer counts the run, then sums it up. The dialog cannot be dismissed while the run
// goes, and Done closes it.
import { cva } from "class-variance-authority";
import { useId, useRef, useState } from "react";
import type { KeyboardEvent } from "react";

import type { UpdateAvailableEvent } from "#/cli/events.gen.ts";
import { AlertIcon } from "#/components/icons/alert.tsx";
import { CheckIcon } from "#/components/icons/check.tsx";
import { CloseIcon } from "#/components/icons/close.tsx";
import { ProblemText } from "#/components/problem-text.tsx";
import { Spinner } from "#/components/spinner.tsx";
import { Button } from "#/components/ui/button.tsx";
import { Checkbox } from "#/components/ui/checkbox.tsx";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "#/components/ui/dialog.tsx";
import { formatAgo, formatDate } from "#/lib/dates.ts";
import { plural } from "#/lib/plural.ts";
import { problemSentence } from "#/lib/problem-text.ts";
import { useNow } from "#/screens/settings/clock.ts";
import { shortSource, updateVersions } from "#/screens/skills/projection.ts";
import { UpdateSheet } from "#/screens/skills/update-sheet.tsx";
import {
  dismissUpdateRun,
  runProgressText,
  runSummaryText,
  updateSkills,
  useOfferedUpdates,
  useUpdateDialog,
  useUpdateRun,
  UPDATE_PILL_ID,
} from "#/screens/skills/updates.ts";
import type { UpdateRunRow, UpdateStatus } from "#/screens/skills/updates.ts";
import { SelectAllRow } from "#/screens/sources/select-all-row.tsx";
import { useAppStore } from "#/store/store.ts";

const DAY = 24 * 60 * 60 * 1000;

/**
 * When the newer version was published, as the update check dates it: how long ago within a
 * month, else its date. Undefined when the CLI dates it not.
 */
const publishedText = (update: UpdateAvailableEvent, now: number) => {
  const time = update.candidate_upstream_time;
  const at = time === undefined ? Number.NaN : Date.parse(time);
  let when: string | undefined;
  if (time !== undefined && !Number.isNaN(at)) {
    when = now - at < 30 * DAY ? formatAgo(time, now) : formatDate(time);
  }
  return when;
};

/** The newer version's date and time in full, for the tooltip of its date. */
const publishedTitle = (time: string) =>
  new Date(time).toLocaleString(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  });

/** Where the update comes from: the upstream of a fork, else the skill's source. */
const sourceOf = (update: UpdateAvailableEvent) => {
  const url = update.upstream ?? update.source;
  return url === undefined || url === "" ? undefined : url;
};

/** The skills of one source, under its short form; `url` is undefined for those the CLI gives none. */
interface SourceGroup {
  label: string;
  url: string | undefined;
  rows: UpdateRunRow[];
}

/** The label of the group of updates the CLI gives no source for. */
const NO_SOURCE = "No source";

/**
 * The rows by source, A to Z by the source's short form, the updates with no source last. Each
 * group keeps the rows' order: A to Z by name, as the app offers the updates.
 */
const groupsOf = (rows: readonly UpdateRunRow[]): SourceGroup[] => {
  const groups = new Map<string, SourceGroup>();
  for (const row of rows) {
    const url = sourceOf(row.update);
    const label = url === undefined ? NO_SOURCE : shortSource(url);
    const group = groups.get(label) ?? { label, rows: [], url };
    group.rows.push(row);
    groups.set(label, group);
  }
  const sorted = [...groups.values()];
  sorted.sort((a, b) =>
    (a.url === undefined) === (b.url === undefined)
      ? a.label.localeCompare(b.label)
      : Number(a.url === undefined) - Number(b.url === undefined)
  );
  return sorted;
};

/** What a screen reader hears for each state, before the name. */
const statusNames: Record<UpdateStatus, string> = {
  conflict: "Needs a merge",
  current: "Up to date",
  failed: "Not updated",
  merged: "Updated",
  running: "Updating",
  skipped: "Not selected",
  updated: "Updated",
  waiting: "Waiting",
};

const StatusMark = ({ status }: { status: UpdateStatus }) => {
  switch (status) {
    case "running": {
      return (
        <span className="text-text-muted flex">
          <Spinner />
        </span>
      );
    }
    case "updated":
    case "merged":
    case "current": {
      return (
        <span className="text-tone-primary-fg flex">
          <CheckIcon />
        </span>
      );
    }
    case "conflict": {
      return (
        <span className="text-tone-attention-fg flex">
          <AlertIcon />
        </span>
      );
    }
    case "failed": {
      return (
        <span className="text-tone-danger-fg flex">
          <CloseIcon />
        </span>
      );
    }
    case "waiting":
    case "skipped": {
      return <span className="size-4 flex-none" />;
    }
    default: {
      return null;
    }
  }
};

const noteVariants = cva("type-notice m-0", {
  variants: {
    tone: {
      attention: "text-tone-attention-fg",
      danger: "text-tone-danger-fg",
    },
  },
});

/** The line under a row of a run, for a skill the user must act on: a conflict, or a failure. */
const ResultNote = ({ row }: { row: UpdateRunRow }) => {
  const { name } = row.update;
  switch (row.status) {
    case "conflict": {
      return (
        <p className={noteVariants({ tone: "attention" })}>
          Merge conflict. Open the skill to resolve it.
        </p>
      );
    }
    case "failed": {
      return row.reason === undefined ? null : (
        <p className={noteVariants({ tone: "danger" })}>
          {problemSentence({ message: row.reason }, { keep: [name] })}
        </p>
      );
    }
    case "waiting":
    case "skipped":
    case "running":
    case "updated":
    case "merged":
    case "current": {
      return null;
    }
    default: {
      return null;
    }
  }
};

const rowVariants = cva("relative flex items-start gap-3 px-3 py-2", {
  variants: {
    state: {
      // Before the run: the whole row toggles its skill.
      idle: "rounded-update-skills-row hover:bg-update-skills-row-hover",
      // In the run, a skill selected.
      run: "",
      // In the run, a skill left out.
      skipped: "opacity-update-skills-skipped",
    },
  },
});

/**
 * "2 days ago", with the date in full as its tooltip. Before the run it is a second
 * label of the row's checkbox, above the row's hit area so that its tooltip shows, and a click on
 * it still toggles the skill.
 */
const Published = ({
  text,
  title,
  checkbox,
}: {
  text: string;
  title: string;
  checkbox: string | undefined;
}) =>
  checkbox === undefined ? (
    <span
      title={title}
      className="type-secondary text-text-hint whitespace-nowrap"
    >
      {text}
    </span>
  ) : (
    <label
      htmlFor={checkbox}
      title={title}
      className="type-secondary text-text-hint relative cursor-pointer whitespace-nowrap"
    >
      {text}
    </label>
  );

const Dot = () => (
  <span aria-hidden className="type-secondary text-text-caption">
    ·
  </span>
);

/**
 * One skill. Before the run, its name is a label of the round checkbox at the right whose hit
 * area covers the row, so a click anywhere on it toggles the skill, but on See changes, which
 * sits above it and opens the What's new sheet. In the run, the state mark takes the checkbox's
 * place.
 */
const Row = ({
  row,
  idle,
  now,
  onToggle,
  onSeeChanges,
}: {
  row: UpdateRunRow;
  idle: boolean;
  now: number;
  onToggle: () => void;
  onSeeChanges: () => void;
}) => {
  const id = useId();
  const { update } = row;
  const published = publishedText(update, now);
  const selected = row.status !== "skipped";
  let state: "idle" | "run" | "skipped" = "run";
  if (idle) {
    state = "idle";
  } else if (!selected) {
    state = "skipped";
  }
  return (
    <li className={rowVariants({ state })}>
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <div className="flex min-w-0 items-baseline gap-2">
          {idle ? (
            // The label's hit area covers the row; the controls above it are positioned.
            <label
              htmlFor={id}
              className="type-body text-text-primary min-w-0 cursor-pointer truncate after:absolute after:inset-0"
            >
              {update.name}
            </label>
          ) : (
            <span className="type-body text-text-primary min-w-0 truncate">
              <span className="sr-only">{`${statusNames[row.status]}: `}</span>
              {update.name}
            </span>
          )}
          {published !== undefined && (
            <>
              <Dot />
              <Published
                text={published}
                title={publishedTitle(update.candidate_upstream_time ?? "")}
                checkbox={idle ? id : undefined}
              />
            </>
          )}
          {idle && (
            <>
              <Dot />
              <span className="relative flex">
                <Button variant="link" size="inline" onClick={onSeeChanges}>
                  <span className="type-secondary whitespace-nowrap">
                    See changes
                  </span>
                </Button>
              </span>
            </>
          )}
        </div>
        {row.edited && (
          <p className={noteVariants({ tone: "attention" })}>
            Skill has local changes; update merges with edits.
          </p>
        )}
        {!idle && <ResultNote row={row} />}
        {!idle &&
          row.warnings?.map((warning) => (
            <p key={warning} className={noteVariants({ tone: "attention" })}>
              {warning}
            </p>
          ))}
      </div>
      <span className="relative flex h-5 items-center">
        {idle ? (
          <Checkbox
            id={id}
            aria-label={update.name}
            checked={selected}
            onCheckedChange={onToggle}
          />
        ) : (
          <StatusMark status={row.status} />
        )}
      </span>
    </li>
  );
};

/**
 * The header of a source's skills: the source's short form as a caption, its full URL as the
 * tooltip. Before the run, a source of two or more skills adds a checkbox at the right, under the
 * rows' checkboxes, that selects them all, checked, unchecked or mixed as they are, and clears
 * them once all are selected; the caption is its label.
 */
const GroupHeader = ({
  group,
  idle,
  onSelect,
}: {
  group: SourceGroup;
  idle: boolean;
  /** Selects every skill of the group, or with `false` leaves them all out. */
  onSelect: (selected: boolean) => void;
}) => {
  const id = useId();
  const selected = group.rows.filter((r) => r.status !== "skipped").length;
  const all = selected === group.rows.length;
  let checked: boolean | "indeterminate" = false;
  if (all) {
    checked = true;
  } else if (selected > 0) {
    checked = "indeterminate";
  }
  const toggles = idle && group.rows.length > 1;
  return (
    <div className="flex h-6 items-center gap-3 px-3">
      <h3 className="m-0 flex min-w-0 flex-1">
        {toggles ? (
          <label
            htmlFor={id}
            title={group.url}
            className="type-caption text-group-label min-w-0 cursor-pointer truncate"
          >
            {group.label}
          </label>
        ) : (
          <span
            title={group.url}
            className="type-caption text-group-label min-w-0 truncate"
          >
            {group.label}
          </span>
        )}
      </h3>
      {toggles && (
        <span className="flex">
          <Checkbox
            id={id}
            checked={checked}
            onCheckedChange={() => {
              onSelect(!all);
            }}
          />
        </span>
      )}
    </div>
  );
};

/** The confirm's label for `selected` of `total` skills. */
const confirmLabel = (selected: number, total: number) => {
  if (selected === 0) {
    return "Update";
  }
  return selected === total && total > 1
    ? `Update all ${total}`
    : `Update ${plural(selected, "skill")}`;
};

/** Whether Enter in the dialog confirms: anywhere but on a button, which Enter presses itself. */
const enterConfirms = (event: KeyboardEvent) =>
  event.key === "Enter" &&
  !(
    event.target instanceof HTMLButtonElement &&
    event.target.getAttribute("role") !== "checkbox"
  );

/** The dialog's body, mounted while it is open, so that the selection starts whole each time. */
const UpdateForm = ({ onClose }: { onClose: () => void }) => {
  const offers = useOfferedUpdates();
  const library = useAppStore((s) => s.inventory?.library);
  const lastChecked = useAppStore((s) => s.lastChecked);
  const run = useUpdateRun((s) => s.run);
  const now = useNow();
  const confirm = useRef<HTMLButtonElement>(null);
  // The skills the user left out: a skill an update check adds while the dialog is open is selected.
  const [excluded, setExcluded] = useState<ReadonlySet<string>>(new Set());
  const [seeing, setSeeing] = useState<UpdateAvailableEvent | undefined>();

  // Once Update is pressed, the dialog shows the run: its rows as they were then. The run goes
  // in the dialog's order, group by group.
  const groups = groupsOf(
    run?.rows ??
      offers.map((update) => ({
        edited:
          library?.find((entry) => entry.name === update.name)?.state ===
          "modified",
        status: excluded.has(update.name) ? "skipped" : "waiting",
        update,
      }))
  );
  const rows = groups.flatMap((g) => g.rows);
  const idle = run === undefined;
  const running = run?.phase === "running";
  const total = rows.length;
  const selected = rows.filter((r) => r.status !== "skipped").length;

  const toggle = (name: string) => {
    setExcluded((before) => {
      const next = new Set(before);
      if (next.has(name)) {
        next.delete(name);
      } else {
        next.add(name);
      }
      return next;
    });
  };
  const selectGroup = (group: SourceGroup, select: boolean) => {
    setExcluded((before) => {
      const next = new Set(before);
      for (const row of group.rows) {
        if (select) {
          next.delete(row.update.name);
        } else {
          next.add(row.update.name);
        }
      }
      return next;
    });
  };

  const start = () => {
    if (idle && selected > 0) {
      void updateSkills(rows);
    }
  };
  const close = () => {
    if (running) {
      return;
    }
    dismissUpdateRun();
    onClose();
  };

  let status = "";
  if (run !== undefined) {
    status = running ? runProgressText(run) : runSummaryText(run);
  } else if (lastChecked !== undefined) {
    const ago = formatAgo(lastChecked, now);
    status = ago === undefined ? "" : `Checked ${ago}`;
  }

  return (
    <>
      <DialogContent
        onOpenAutoFocus={(e) => {
          e.preventDefault();
          confirm.current?.focus();
        }}
        onCloseAutoFocus={(e) => {
          // The pill that opened the dialog is gone once no update is left: focus Skills then.
          e.preventDefault();
          (
            document.querySelector<HTMLElement>(`#${UPDATE_PILL_ID}`) ??
            document.querySelector<HTMLElement>(
              'nav[aria-label="Main"] a[href="/skills"]'
            )
          )?.focus();
        }}
        onEscapeKeyDown={(e) => {
          if (running) {
            e.preventDefault();
          }
        }}
        onInteractOutside={(e) => {
          if (running) {
            e.preventDefault();
          }
        }}
        onKeyDown={(e) => {
          if (!enterConfirms(e)) {
            return;
          }
          e.preventDefault();
          if (idle) {
            start();
          } else {
            close();
          }
        }}
      >
        <DialogHeader showCloseButton={false}>
          <DialogTitle>Update skills</DialogTitle>
          <DialogDescription>
            Update the selected skills to their latest version.
          </DialogDescription>
        </DialogHeader>
        {idle && groups.length > 1 && (
          <SelectAllRow
            selected={selected}
            total={total}
            added={0}
            onSelectAll={() => {
              setExcluded(new Set());
            }}
            onClear={() => {
              setExcluded(new Set(rows.map((r) => r.update.name)));
            }}
          />
        )}
        {total === 0 ? (
          <p className="type-secondary text-text-hint m-0">
            All skills are up to date
          </p>
        ) : (
          <div className="max-h-update-skills-list -mx-3 flex flex-col gap-3 overflow-auto">
            {groups.map((group) => (
              <section
                key={group.label}
                aria-label={group.label}
                className="flex flex-col gap-0.5"
              >
                <GroupHeader
                  group={group}
                  idle={idle}
                  onSelect={(select) => {
                    selectGroup(group, select);
                  }}
                />
                <ul className="m-0 flex list-none flex-col gap-0.5 p-0">
                  {group.rows.map((row) => (
                    <Row
                      key={row.update.name}
                      row={row}
                      idle={idle}
                      now={now}
                      onToggle={() => {
                        toggle(row.update.name);
                      }}
                      onSeeChanges={() => {
                        setSeeing(row.update);
                      }}
                    />
                  ))}
                </ul>
              </section>
            ))}
          </div>
        )}
        {run?.failure !== undefined && <ProblemText problem={run.failure} />}
        <DialogFooter>
          <output className="type-secondary text-text-hint flex min-w-0 flex-1 items-center">
            {status}
          </output>
          {run === undefined || running ? (
            <>
              <DialogClose asChild>
                <Button variant="ghost" disabled={running}>
                  Cancel
                </Button>
              </DialogClose>
              <Button
                ref={confirm}
                variant="primary"
                disabled={running || selected === 0}
                aria-busy={running ? true : undefined}
                onClick={start}
              >
                {running && <Spinner />}
                {confirmLabel(selected, total)}
              </Button>
            </>
          ) : (
            <Button variant="primary" onClick={close}>
              Done
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
      {/* Outside the dialog's content, so that no key or click in the sheet reaches the dialog. */}
      {seeing !== undefined && (
        <UpdateSheet
          update={seeing}
          versions={updateVersions(seeing)}
          open
          over="dialog"
          onOpenChange={(open) => {
            if (!open) {
              setSeeing(undefined);
            }
          }}
        />
      )}
    </>
  );
};

/**
 * The Update skills dialog. The sidebar's update pill opens it, and so does a check by hand that
 * found an update; while its run goes it cannot be dismissed, and Done closes it.
 */
const closeDialog = () => {
  dismissUpdateRun();
  useUpdateDialog.setState({ open: false });
};

export const UpdateSkillsDialog = () => {
  const open = useUpdateDialog((s) => s.open);
  const running = useUpdateRun((s) => s.run?.phase === "running");
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next && !running) {
          closeDialog();
        }
      }}
    >
      {open && <UpdateForm onClose={closeDialog} />}
    </Dialog>
  );
};
