// Updates on the Skills screen: Check updates (`skill check-updates`), which opens the Update
// skills dialog (`update-dialog.tsx`) when it found an update, the Update of one skill
// (`skill update <name>`, from its banner) and the run of that dialog, which the sidebar's update
// pill opens too: one `skill update <name>...` for every skill selected, each row following what
// the CLI emits about its skill. Each runs through the runner and says what happened by what the
// CLI emitted; the list redraws from the inventory and the update check that follow, never from a
// result predicted here.
import { useMemo } from "react";
import { create } from "zustand";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type {
  CliEvent,
  ErrorEvent,
  LibraryEntry,
  LogEvent,
  UpdateAvailableEvent,
} from "#/cli/events.gen.ts";
import { dismissToast, notify } from "#/components/toast.tsx";
import type { ToastAction } from "#/components/toast.tsx";
import { andList } from "#/lib/plural.ts";
import { checkingText } from "#/screens/skills/checking-text.tsx";
import { shortSource } from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { cancelCommand, agentx, usePending } from "#/store/runner.ts";
import type { Outcome } from "#/store/runner.ts";
import { runUntilSettled } from "#/store/settle.ts";
import { addActivity, useAppStore } from "#/store/store.ts";
import type { Problem } from "#/store/store.ts";

/** The label of Check updates, which the runner shows as pending while the check runs. */
export const CHECK_UPDATES = "Check updates";

/** The label of the Update of `name`, which its banner's Update shows as pending. */
export const updateLabel = (name: string) => `update ${name}`;

const plural = (n: number, one: string, many: string) =>
  n === 1 ? `${n} ${one}` : `${n} ${many}`;

/** The skills a command updated, by the `library_skill` events it emitted. */
const updatedIn = (events: CliEvent[]) =>
  events.filter((e) => e.type === "library_skill");

/** The errors a command emitted: its own, and in a run of several names, one per skill it gave up on. */
const errorsIn = (events: CliEvent[]) =>
  events.flatMap((e) => (e.type === "error" ? [e] : []));

/** How many merges a command left for the user to finish, by its `conflict` events. */
const conflictsIn = (events: CliEvent[]) =>
  events.filter((e) => e.type === "conflict").length;

/** Whether the check runs. */
export const useCheckingUpdates = () => usePending(CHECK_UPDATES);

/** Add source: `source add <url>`, so that the skill can be updated from it again. */
/** Add source on a toast: `source add <source>`, for a skill whose source was removed. */
export const addSource = (source: string): ToastAction => ({
  label: "Add source",
  onClick: () => {
    void runUntilSettled(["source", "add", source], {
      label: `add source ${source}`,
      success: `Added ${shortSource(source)} source`,
    });
  },
});

/** What an Update that exited 0 did, in the words of the results table. */
const updatedText = (row: SkillRow, events: CliEvent[]) => {
  const updated = updatedIn(events).find((e) => e.name === row.name);
  if (updated === undefined) {
    return `${row.name} skill is already up to date`;
  }
  if (row.kind === "yours") {
    return `Updated ${row.name} skill. Publish when ready.`;
  }
  return updated.state === "modified"
    ? `Updated ${row.name} skill. Merged with your local edits`
    : `Updated ${row.name} skill`;
};

/**
 * Update: `skill update <name>`. Exit 4 left a merge for the user to finish, which the runner
 * opens the skill for; exit 5 is a skill whose source was removed from this machine, which
 * Add source brings back. Any other failure is the runner's error toast.
 */
export const updateSkill = async (row: SkillRow) => {
  const source = row.entry?.source ?? row.update?.source;
  const outcome = await runUntilSettled(["skill", "update", row.name], {
    // Exit 5: the CLI's message, with the way out in place of the command its hint names.
    failure: ({ exitCode, events }) => {
      const error = events.find((e) => e.type === "error");
      return exitCode === EXIT_CODES.not_found &&
        error !== undefined &&
        source !== undefined
        ? { action: addSource(source), message: error.message }
        : undefined;
    },
    label: updateLabel(row.name),
    success: (events) => updatedText(row, events),
  });
  if (
    outcome.exitCode === EXIT_CODES.pending_merge &&
    conflictsIn(outcome.events) > 0
  ) {
    notify(
      `${row.name} skill has conflicts. Resolve them to finish the update`,
      undefined,
      { kind: "warning" }
    );
  }
};

/**
 * The updates the app offers, by name: every update candidate of the last check but those of a
 * skill with a pending merge, which the skill's banner finishes first.
 */
export const offeredUpdates = (
  updates: Iterable<UpdateAvailableEvent>,
  library: readonly LibraryEntry[]
) => {
  const offers = [...updates].filter(
    (u) =>
      library.find((entry) => entry.name === u.name)?.pending_merge !== true
  );
  offers.sort((a, b) => a.name.localeCompare(b.name));
  return offers;
};

/** The updates the app offers, from the store as it is now; a new list only when one changes. */
export const useOfferedUpdates = () => {
  const updates = useAppStore((s) => s.updates);
  const library = useAppStore((s) => s.inventory?.library);
  return useMemo(
    () => offeredUpdates(Object.values(updates), library ?? []),
    [updates, library]
  );
};

/** How many skills the check offers an update for: a skill with a pending merge is not offered one. */
const offered = (events: CliEvent[]) =>
  offeredUpdates(
    events.flatMap((e) => (e.type === "update_available" ? [e] : [])),
    useAppStore.getState().inventory?.library ?? []
  ).length;

/** Whether the app offers an update now, as the sidebar's update pill counts them. */
const offersUpdates = () => {
  const { inventory, updates } = useAppStore.getState();
  return (
    offeredUpdates(Object.values(updates), inventory?.library ?? []).length > 0
  );
};

/** The sources a check could not reach: those of its fetches that its error names. */
const unreachable = (events: CliEvent[], message: string) => {
  const fetched = events.flatMap((e) =>
    e.type === "progress" &&
    e.phase === "fetch" &&
    e.subject !== undefined &&
    message.includes(e.subject)
      ? [e.subject]
      : []
  );
  return [...new Set(fetched)].map(shortSource);
};

/** Exit 3 of a check: the sources it could not reach, with the app's own second line. */
const unreachableToast = (events: CliEvent[], error: ErrorEvent) => {
  const sources = unreachable(events, error.message);
  return {
    hint: "Verify that you can reach the source",
    message:
      sources.length === 0
        ? "Couldn't reach every source"
        : `Couldn't reach ${sources.join(", ")}`,
  };
};

/** Exit 3 of a check, whoever runs it: the sources it could not reach. */
const unreachableFailure = ({ exitCode, events }: Outcome) => {
  const error = events.find((e) => e.type === "error");
  return exitCode === EXIT_CODES.source && error !== undefined
    ? unreachableToast(events, error)
    : undefined;
};

/** Whether a check runs: there is one at a time, whichever control started it. */
const checkRunning = () =>
  Object.hasOwn(useAppStore.getState().pending, CHECK_UPDATES);

/** Whether the Update skills dialog is open: the sidebar's update pill and Check updates open it. */
export const useUpdateDialog = create<{ open: boolean }>()(() => ({
  open: false,
}));

/** The id of the sidebar's update pill, which takes the focus back when the dialog closes. */
export const UPDATE_PILL_ID = "update-pill";

/** Opens the Update skills dialog. */
export const openUpdateDialog = () => {
  useUpdateDialog.setState({ open: true });
};

/**
 * Check updates: `skill check-updates`, which fetches each source and lists every update it finds.
 * Its toast says "Checking for updates…", then how far it got, "0/10" before the first fetch and
 * then one source fetched at a time, in room it keeps from the start. When the app then offers an update, the toast goes and the Update skills
 * dialog opens; else the toast says that all skills are up to date. A source it could not reach
 * (exit 3) is named in the toast, with a line of the app's own, and the updates it found elsewhere
 * are listed all the same (src/store/loaders.ts), in the dialog.
 */
export const checkUpdates = async () => {
  if (checkRunning()) {
    return;
  }
  const cancel: ToastAction = {
    label: "Cancel",
    onClick: () => {
      void cancelCommand(CHECK_UPDATES);
    },
  };
  notify(checkingText(), cancel, { kind: "progress", lasting: true });
  const { exitCode } = await agentx(["skill", "check-updates"], {
    failure: unreachableFailure,
    label: CHECK_UPDATES,
    onProgress: (event) => {
      if (event.phase === "fetch") {
        notify(checkingText(event.current, event.total), cancel, {
          inPlace: true,
          kind: "progress",
          lasting: true,
        });
      }
    },
    // An update found: the dialog says so, in place of a toast.
    success: (events) =>
      offered(events) === 0 ? "All skills are up to date" : undefined,
  });
  if (
    (exitCode === EXIT_CODES.ok || exitCode === EXIT_CODES.source) &&
    offersUpdates()
  ) {
    // Exit 3 keeps its toast, which names the source the check could not reach.
    if (exitCode === EXIT_CODES.ok) {
      dismissToast();
    }
    openUpdateDialog();
  }
};

/**
 * Where a skill of the Update skills dialog is in its run. Before the run every selected skill is
 * `waiting` and every other one `skipped`; the run's command makes every selected skill `running`,
 * then each ends `updated` (`merged` when its edits were merged in), `current` when there was
 * nothing to update after all, `conflict` when the merge waits for the user, or `failed` with the
 * CLI's reason. A skill the run never reached goes back to `waiting` when it stopped.
 */
export type UpdateStatus =
  | "waiting"
  | "skipped"
  | "running"
  | "updated"
  | "merged"
  | "current"
  | "conflict"
  | "failed";

/** One skill of the run, as the dialog listed it when Update was pressed. */
export interface UpdateRunRow {
  update: UpdateAvailableEvent;
  /** The library holds edits the update merges with. */
  edited: boolean;
  status: UpdateStatus;
  /** Why the CLI did not update it, in its words. */
  reason?: string;
  /** What the CLI warned about while it updated it, such as a placement it skipped. */
  warnings?: string[];
}

/** The run of the dialog: its rows and how far it is. */
export interface UpdateRun {
  /** `running` until its command ends; `failed` when it ended with no word from the CLI. */
  phase: "running" | "ended" | "failed";
  rows: UpdateRunRow[];
  /** Why the run stopped, as the CLI or the app says it. */
  failure?: Problem;
}

/** The one run of the dialog, from Update until the dialog is closed after it. */
export const useUpdateRun = create<{ run: UpdateRun | undefined }>()(() => ({
  run: undefined,
}));

const setRow = (name: string, change: Partial<UpdateRunRow>) => {
  useUpdateRun.setState(({ run }) => ({
    run:
      run === undefined
        ? run
        : {
            ...run,
            rows: run.rows.map((row) =>
              row.update.name === name ? { ...row, ...change } : row
            ),
          },
  }));
};

/** Forgets the run once its dialog closes. A run still going is never forgotten. */
export const dismissUpdateRun = () => {
  if (useUpdateRun.getState().run?.phase !== "running") {
    useUpdateRun.setState({ run: undefined });
  }
};

const selectedRows = (run: UpdateRun) =>
  run.rows.filter((r) => r.status !== "skipped");

/**
 * "Updating 2 of 4…": the skill the run is on, of those selected. Every selected skill runs at
 * once, so it counts the skills done, plus the one under way.
 */
export const runProgressText = (run: UpdateRun) => {
  const selected = selectedRows(run);
  const done = selected.filter(
    (r) => r.status !== "waiting" && r.status !== "running"
  ).length;
  return `Updating ${Math.min(done + 1, selected.length)} of ${selected.length}…`;
};

/** The statuses of a skill the run updated, or found up to date already. */
export const UPDATED: ReadonlySet<UpdateStatus> = new Set<UpdateStatus>([
  "updated",
  "merged",
  "current",
]);

/** "2 of 4 skills updated": what the run did of the skills selected, once it ended. */
export const runSummaryText = (run: UpdateRun) => {
  const rows = selectedRows(run);
  const updated = rows.filter((r) => UPDATED.has(r.status)).length;
  return `${updated} of ${plural(rows.length, "skill", "skills")} updated`;
};

/** Where the run left a skill, and the CLI's reason when it did not update it. */
interface Settled {
  status: UpdateStatus;
  reason?: string;
}

/** Whether `event` says the run is done with `name`: the `progress` event that follows its outcome. */
const doneWith = (event: CliEvent, name: string) =>
  event.type === "progress" &&
  event.phase === "update" &&
  event.subject === name;

/**
 * Where the events of a `skill update` left `name`, by what they say of it: updated, conflicting,
 * given up on, or done with nothing to update. Undefined while they say nothing of it.
 */
const settledIn = (name: string, events: CliEvent[]): Settled | undefined => {
  const updated = updatedIn(events).find((e) => e.name === name);
  if (updated !== undefined) {
    return { status: updated.state === "modified" ? "merged" : "updated" };
  }
  if (events.some((e) => e.type === "conflict" && e.name === name)) {
    return { status: "conflict" };
  }
  const error = errorsIn(events).find((e) => e.skill === name);
  if (error !== undefined) {
    return { reason: error.message, status: "failed" };
  }
  return events.some((e) => doneWith(e, name))
    ? { status: "current" }
    : undefined;
};

/**
 * Where the ended run left `name`: what its events say of it; else up to date when the run
 * succeeded, as a run of one name says nothing of a skill it had nothing to update; else failed
 * with the run's own error, the refusal of a run of one name or a failure of the whole run before
 * it reached the skill. Undefined when the CLI said nothing at all.
 */
const settledBy = (name: string, outcome: Outcome): Settled | undefined => {
  const settled = settledIn(name, outcome.events);
  if (settled !== undefined) {
    return settled;
  }
  if (outcome.exitCode === EXIT_CODES.ok) {
    return { status: "current" };
  }
  const error = errorsIn(outcome.events).find((e) => e.skill === undefined);
  return error === undefined
    ? undefined
    : { reason: error.message, status: "failed" };
};

/**
 * What the CLI warned about while it updated `name`: in a run of several names, the warnings it
 * logged for that skill; in a run of one, every warning of the run.
 */
const warningsOf = (name: string, names: string[], logs: LogEvent[]) =>
  logs
    .filter(
      (l) => l.level === "warn" && (names.length === 1 || l.skill === name)
    )
    .map((l) => l.message);

/** A row's status and reason, and its warnings when it has any. */
const rowChange = (
  settled: Settled,
  warnings: string[]
): Partial<UpdateRunRow> => ({
  status: settled.status,
  ...(settled.reason === undefined ? {} : { reason: settled.reason }),
  ...(warnings.length > 0 ? { warnings } : {}),
});

/** The activity entry of merges that wait for the user: "review skill has conflicts. …". */
const conflictsText = (names: string[]) =>
  names.length === 1
    ? `${names[0]} skill has conflicts. Resolve them to finish the update`
    : `${andList(names)} skills have conflicts. Resolve them to finish the updates`;

/** Whether a command ended in a way the runner answers itself: the lock, the account repo, a cancel. */
const answeredByRunner = (outcome: Outcome) =>
  outcome.exitCode === EXIT_CODES.locked ||
  outcome.exitCode === EXIT_CODES.account_repo ||
  outcome.cancelled;

/** A command that failed with no word from the CLI: it could not start, or was stopped. */
const stoppedProblem = (outcome: Outcome): Problem => ({
  message:
    outcome.startError === undefined
      ? "agentx skill update did not finish"
      : `agentx could not start: ${outcome.startError}`,
});

/** The activity entry the run adds once it ended: what it did, in the words of its summary. */
const logRun = (run: UpdateRun) => {
  const rows = selectedRows(run);
  const clean = rows.every(
    (r) => UPDATED.has(r.status) && (r.warnings ?? []).length === 0
  );
  addActivity(clean ? "success" : "attention", runSummaryText(run));
};

/**
 * Updates the skills of `rows` that are selected in one `skill update <name>...`, the names in the
 * dialog's order. Every selected row runs at once, and each takes its state as the CLI says it is
 * done with its skill, then once more from the whole outcome when the command ended, since a
 * warning on stderr may come after. A merge that conflicts stays on its row and opens no skill;
 * the dialog shows the result, so no toast does. A command that ends in a way the runner answers
 * (the lock, the account repo, a cancel) puts the dialog back as it was before the run, with the
 * skills already updated gone from it; one that ends with no word from the CLI stops the run with
 * its problem, the skills it never reached waiting again.
 */
export const updateSkills = async (rows: UpdateRunRow[]) => {
  if (useUpdateRun.getState().run !== undefined) {
    return;
  }
  const names = rows
    .filter((r) => r.status === "waiting")
    .map((r) => r.update.name);
  if (names.length === 0) {
    return;
  }
  useUpdateRun.setState({
    run: {
      phase: "running",
      rows: rows.map((r) =>
        r.status === "waiting" ? { ...r, status: "running" } : r
      ),
    },
  });
  const events: CliEvent[] = [];
  const logs: LogEvent[] = [];
  const outcome = await runUntilSettled(["skill", "update", ...names], {
    keep: names,
    label: updateLabel(names.join(" ")),
    onEvent: (event) => {
      events.push(event);
      const name = names.find((n) => doneWith(event, n));
      const settled = name === undefined ? undefined : settledIn(name, events);
      if (name !== undefined && settled !== undefined) {
        setRow(name, rowChange(settled, warningsOf(name, names, logs)));
      }
    },
    onLog: (log) => {
      logs.push(log);
      for (const name of names) {
        const warnings = warningsOf(name, names, logs);
        if (warnings.length > 0) {
          setRow(name, { warnings });
        }
      }
    },
    ownActivity: (ended) => {
      const conflicts = names.filter((name) =>
        ended.events.some((e) => e.type === "conflict" && e.name === name)
      );
      return ended.exitCode === EXIT_CODES.pending_merge && conflicts.length > 0
        ? { message: conflictsText(conflicts), tone: "attention" }
        : undefined;
    },
    ownFailure: true,
    retry: () => {
      openUpdateDialog();
    },
    stayOnConflict: true,
  });
  if (answeredByRunner(outcome)) {
    useUpdateRun.setState({ run: undefined });
    return;
  }
  let stopped = false;
  for (const name of names) {
    const settled = settledBy(name, outcome);
    if (settled === undefined) {
      stopped = true;
      setRow(name, { status: "waiting" });
    } else {
      setRow(name, rowChange(settled, warningsOf(name, names, outcome.logs)));
    }
  }
  const { run } = useUpdateRun.getState();
  if (run === undefined) {
    return;
  }
  if (stopped) {
    useUpdateRun.setState({
      run: { ...run, failure: stoppedProblem(outcome), phase: "failed" },
    });
    return;
  }
  const ended: UpdateRun = { ...run, phase: "ended" };
  useUpdateRun.setState({ run: ended });
  logRun(ended);
};
