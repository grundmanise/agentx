// The Update skills dialog, which the sidebar's update pill and a check by hand that found an
// update open: every skill the last update check found an update for, but those with a pending
// merge, all selected, grouped by source in sections that collapse. A row toggles its skill, a
// group's Select all or Deselect all every skill of its source, and See changes opens the What's
// new sheet of its update above the dialog. A row is two lines at most: the name, then one note.
// Update runs in the dialog (`updates.ts`): the selected rows alone stay, the state of each in
// place of its checkbox before its name, and once the run ended only the rows with something to say are left: a conflict, an error or a
// warning. The footer counts the run, then sums it up. The dialog cannot be dismissed while the
// run goes, and Done closes it.
import { cva } from "class-variance-authority";
import { useId, useRef, useState } from "react";
import type { KeyboardEvent } from "react";

import type { UpdateAvailableEvent } from "#/cli/events.gen.ts";
import { AlertIcon } from "#/components/icons/alert.tsx";
import { CheckIcon } from "#/components/icons/check.tsx";
import { CloseIcon } from "#/components/icons/close.tsx";
import { ProblemText } from "#/components/problem-text.tsx";
import { SectionToggle } from "#/components/section-toggle.tsx";
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
  UPDATED,
  useOfferedUpdates,
  useUpdateDialog,
  useUpdateRun,
  UPDATE_PILL_ID,
} from "#/screens/skills/updates.ts";
import type {
  UpdateRun,
  UpdateRunRow,
  UpdateStatus,
} from "#/screens/skills/updates.ts";
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

/**
 * The skills of one source, under its short form; `url` is undefined for those the CLI gives none.
 * `key` is the URL, as two hosts can share a short form.
 */
interface SourceGroup {
  key: string;
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
    const key = url ?? "";
    const label = url === undefined ? NO_SOURCE : shortSource(url);
    const group = groups.get(key) ?? { key, label, rows: [], url };
    group.rows.push(row);
    groups.set(key, group);
  }
  const sorted = [...groups.values()];
  sorted.sort((a, b) =>
    (a.url === undefined) === (b.url === undefined)
      ? a.label.localeCompare(b.label) || a.key.localeCompare(b.key)
      : Number(a.url === undefined) - Number(b.url === undefined)
  );
  return sorted;
};

/**
 * Whether the dialog lists `row`: before the run, every skill; during it, the selected ones; once
 * it ended, those with something to say, a conflict, an error or a warning.
 */
const listed = (row: UpdateRunRow, run: UpdateRun | undefined) => {
  if (run === undefined) {
    return true;
  }
  if (row.status === "skipped") {
    return false;
  }
  return (
    run.phase === "running" ||
    row.status === "conflict" ||
    row.status === "failed" ||
    (row.warnings ?? []).length > 0
  );
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

/** Whether `row` updated, or was up to date, with a warning from the CLI: it asks for attention. */
const warned = (row: UpdateRunRow) =>
  UPDATED.has(row.status) && (row.warnings ?? []).length > 0;

/** What a screen reader hears for the row's state, before the name. */
const statusName = (row: UpdateRunRow) =>
  warned(row)
    ? `${statusNames[row.status]} with a warning`
    : statusNames[row.status];

/**
 * The row's state, in the slot its checkbox held before the run, so that the names keep their
 * place whether it shows or not: nothing while it waits, a spinner while it runs, a check once it updated cleanly, the alert
 * for a conflict or a warning, a cross when it did not update.
 */
const StatusMark = ({ row }: { row: UpdateRunRow }) => {
  if (warned(row)) {
    return (
      <span className="text-tone-attention-fg flex">
        <AlertIcon />
      </span>
    );
  }
  switch (row.status) {
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

// The note starts under the name, past the checkbox or the state mark and its gap.
const noteVariants = cva("type-notice m-0 truncate pl-6", {
  variants: {
    tone: {
      attention: "text-tone-attention-fg",
      danger: "text-tone-danger-fg",
    },
  },
});

/** The one line under a row's name: its text, its full text as the tooltip, and its tone. */
interface Note {
  text: string;
  title: string;
  tone: "attention" | "danger";
}

/**
 * The note under a row, one at most so that a row keeps to two lines: once its run ended, a
 * conflict or the CLI's reason, then the first warning the CLI logged for it, all of them in the
 * tooltip; else, before, during and after the run, that a skill with local edits merges with them.
 */
const noteOf = (row: UpdateRunRow, idle: boolean): Note | undefined => {
  if (!idle && row.status === "conflict") {
    const text = "Merge conflict. Open the skill to resolve it.";
    return { text, title: text, tone: "attention" };
  }
  if (!idle && row.status === "failed" && row.reason !== undefined) {
    const text = problemSentence(
      { message: row.reason },
      { keep: [row.update.name] }
    );
    return { text, title: text, tone: "danger" };
  }
  const [warning] = row.warnings ?? [];
  if (!idle && warning !== undefined) {
    return {
      text: warning,
      title: (row.warnings ?? []).join("\n"),
      tone: "attention",
    };
  }
  if (row.edited) {
    const text = "Skill has local changes; update merges with edits.";
    return { text, title: text, tone: "attention" };
  }
  return undefined;
};

const rowVariants = cva("relative flex items-start gap-3 px-3 py-2", {
  variants: {
    state: {
      // Before the run: the whole row toggles its skill.
      idle: "rounded-update-skills-row hover:bg-update-skills-row-hover",
      // In the run and after it.
      run: "",
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
 * One skill, led by a slot before its name, on its line. Before the run the slot holds the round
 * checkbox, and the name is a label of it whose hit area covers the row, so a click anywhere on it
 * toggles the skill, but on See changes, which sits above it and opens the What's new sheet. In
 * the run the state mark takes the checkbox's place, so the name and the note stay where they are.
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
  /** Opens the What's new sheet; `opener` is See changes, which gets the focus back. */
  onSeeChanges: (opener: HTMLElement) => void;
}) => {
  const id = useId();
  const { update } = row;
  const published = publishedText(update, now);
  const note = noteOf(row, idle);
  const selected = row.status !== "skipped";
  return (
    <li className={rowVariants({ state: idle ? "idle" : "run" })}>
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <div className="flex min-w-0 items-baseline gap-2">
          {/* Above the name's hit area, which comes after it. */}
          <span className="relative z-10 flex flex-none self-center">
            {idle ? (
              <Checkbox
                id={id}
                aria-label={update.name}
                checked={selected}
                onCheckedChange={onToggle}
              />
            ) : (
              <StatusMark row={row} />
            )}
          </span>
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
              <span className="sr-only">{`${statusName(row)}: `}</span>
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
                <Button
                  variant="link"
                  size="inline"
                  onClick={(e) => {
                    onSeeChanges(e.currentTarget);
                  }}
                >
                  <span className="type-secondary whitespace-nowrap">
                    See changes
                  </span>
                </Button>
              </span>
            </>
          )}
        </div>
        {note !== undefined && (
          <p title={note.title} className={noteVariants({ tone: note.tone })}>
            {note.text}
          </p>
        )}
      </div>
    </li>
  );
};

/**
 * The header of a source's skills, which collapses or expands them: a chevron and the source's
 * short form as a caption, its full URL as the tooltip. Before the run, a text button at its
 * right end: Select all while any of them is left out, which selects them all, else Deselect all,
 * which clears them.
 */
const GroupHeader = ({
  group,
  idle,
  open,
  onToggle,
  onSelect,
}: {
  group: SourceGroup;
  idle: boolean;
  open: boolean;
  onToggle: () => void;
  /** Selects every skill of the group, or with `false` leaves them all out. */
  onSelect: (selected: boolean) => void;
}) => {
  const all = group.rows.every((r) => r.status !== "skipped");
  return (
    <SectionToggle
      label={group.label}
      title={group.url}
      open={open}
      onToggle={onToggle}
      action={
        idle ? (
          <Button
            variant="link"
            size="inline"
            onClick={() => {
              onSelect(!all);
            }}
          >
            <span className="type-secondary whitespace-nowrap">
              {all ? "Deselect all" : "Select all"}
            </span>
          </Button>
        ) : undefined
      }
    />
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
  // The keys of the collapsed sources: every one is open when the dialog opens.
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set());
  // The update whose What's new sheet is open, and the See changes that opened it.
  const [seeing, setSeeing] = useState<
    { update: UpdateAvailableEvent; opener: HTMLElement } | undefined
  >();

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
  // The groups as the dialog lists them: a group with no row left to list goes.
  const shown = groups
    .map((group) => ({
      ...group,
      rows: group.rows.filter((row) => listed(row, run)),
    }))
    .filter((group) => group.rows.length > 0);

  const toggleSection = (key: string) => {
    setCollapsed((before) => {
      const next = new Set(before);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.add(key);
      }
      return next;
    });
  };
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
      // The run shows every selected row: a section collapsed before it opens again.
      setCollapsed(new Set());
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
          <DialogTitle>Updates available</DialogTitle>
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
        {total === 0 && (
          <p className="type-secondary text-text-hint m-0">
            All skills are up to date
          </p>
        )}
        {/* Once the run ended with nothing to say about any skill, the footer says it all. */}
        {shown.length > 0 && (
          <div className="max-h-update-skills-list -mx-3 flex flex-col gap-3 overflow-auto">
            {shown.map((group) => {
              const open = !collapsed.has(group.key);
              return (
                <section
                  key={group.key}
                  aria-label={group.label}
                  className="flex flex-col gap-0.5"
                >
                  <GroupHeader
                    group={group}
                    idle={idle}
                    open={open}
                    onToggle={() => {
                      toggleSection(group.key);
                    }}
                    onSelect={(select) => {
                      selectGroup(group, select);
                    }}
                  />
                  {open && (
                    <ul className="pl-subgroup-indent m-0 flex list-none flex-col gap-0.5 py-0 pr-0">
                      {group.rows.map((row) => (
                        <Row
                          key={row.update.name}
                          row={row}
                          idle={idle}
                          now={now}
                          onToggle={() => {
                            toggle(row.update.name);
                          }}
                          onSeeChanges={(opener) => {
                            setSeeing({ opener, update: row.update });
                          }}
                        />
                      ))}
                    </ul>
                  )}
                </section>
              );
            })}
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
          update={seeing.update}
          versions={updateVersions(seeing.update)}
          open
          over="dialog"
          onOpenChange={(open) => {
            if (!open) {
              setSeeing(undefined);
            }
          }}
          onCloseAutoFocus={(e) => {
            // The sheet has no trigger: the focus goes back to See changes, or to the confirm.
            e.preventDefault();
            (seeing.opener.isConnected
              ? seeing.opener
              : confirm.current
            )?.focus();
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
