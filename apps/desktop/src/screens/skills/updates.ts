// Updates on the Skills screen: Check updates (`skill check-updates`), the Update of one skill
// (`skill update <name>`, from its banner) and Update all (`skill update --all`,
// from the toast of a check that found more than one). Each runs one command through the runner
// and says what happened by what the CLI emitted; the list redraws from the inventory and the
// update check that follow, never from a result predicted here.
import { EXIT_CODES } from "#/cli/events.gen.ts";
import type { CliEvent, ErrorEvent } from "#/cli/events.gen.ts";
import { notify } from "#/components/toast.tsx";
import type { ToastAction } from "#/components/toast.tsx";
import { shortSource } from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { cancelCommand, runCommand, usePending } from "#/store/runner.ts";
import type { Outcome } from "#/store/runner.ts";
import { runUntilSettled } from "#/store/settle.ts";
import { useAppStore } from "#/store/store.ts";

/** The label of Check updates, which the runner shows as pending while the check runs. */
export const CHECK_UPDATES = "Check updates";

/** The label of the Update of `name`, which its banner's Update shows as pending. */
export const updateLabel = (name: string) => `update ${name}`;

const UPDATE_ALL = "update all";

const plural = (n: number, one: string, many: string) =>
  n === 1 ? `${n} ${one}` : `${n} ${many}`;

/** The skills a command updated, by the `library_skill` events it emitted. */
const updatedIn = (events: CliEvent[]) =>
  events.filter((e) => e.type === "library_skill");

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
      `${row.name} skill has conflicts. Resolve them to finish the update`
    );
  }
};

/** Update all: `skill update --all`, every update the last check found. */
export const updateAll = async () => {
  const outcome = await runCommand(["skill", "update", "--all"], {
    label: UPDATE_ALL,
    success: (events) =>
      `Updated ${plural(updatedIn(events).length, "skill", "skills")}`,
  });
  const { exitCode, events } = outcome;
  const waiting = conflictsIn(events);
  if (exitCode === EXIT_CODES.pending_merge && waiting > 0) {
    const updated = plural(updatedIn(events).length, "skill", "skills");
    notify(
      `Updated ${updated}. ${waiting === 1 ? "1 has" : `${waiting} have`} conflicts`
    );
  }
};

/** How many skills the check offers an update for: a skill with a pending merge is not offered one. */
const offered = (events: CliEvent[]) => {
  const library = useAppStore.getState().inventory?.library ?? [];
  return events.filter(
    (e) =>
      e.type === "update_available" &&
      library.find((entry) => entry.name === e.name)?.pending_merge !== true
  ).length;
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

const checking = (current?: number, total?: number) =>
  current === undefined || total === undefined
    ? "Checking for updates"
    : `Checking for updates · ${current} of ${plural(total, "source", "sources")}`;

/** The sources the last check fetched, so that the next one opens with its count. */
let lastTotal: number | undefined;

/**
 * How long Check updates waits for the CLI's count before it opens its toast without one, in ms.
 * The CLI emits the count before its first fetch, so this passes only when it is slow to start.
 */
const COUNT_WAIT_MS = 400;

/**
 * Check updates: `skill check-updates`, which fetches each source and lists every update it finds.
 * Its toast says how far it got, from 0 of the total before the first fetch and then one source
 * fetched at a time, then what it found: how many skills
 * have an update, with Update all when there is more than one. A source it could not reach
 * (exit 3) is named in the toast, with a line of the app's own, and the updates it found elsewhere are
 * listed all the same (src/store/loaders.ts).
 *
 * The toast opens with the count, so that its text and width do not change as the count comes:
 * with the total of the last check, or else when the CLI emits its own, or, if the CLI is slow to,
 * without a count after COUNT_WAIT_MS.
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
  let opened = false;
  const show = (current?: number, total?: number) => {
    notify(checking(current, total), cancel, {
      inPlace: opened,
      lasting: true,
    });
    opened = true;
  };
  let wait: ReturnType<typeof setTimeout> | undefined;
  if (lastTotal === undefined) {
    wait = setTimeout(() => {
      show();
    }, COUNT_WAIT_MS);
  } else {
    show(0, lastTotal);
  }
  try {
    await runCommand(["skill", "check-updates"], {
      action: (events) =>
        offered(events) > 1
          ? {
              label: "Update all",
              onClick: () => {
                void updateAll();
              },
            }
          : undefined,
      failure: unreachableFailure,
      label: CHECK_UPDATES,
      onProgress: (event) => {
        if (event.phase === "fetch") {
          clearTimeout(wait);
          lastTotal = event.total;
          show(event.current, event.total);
        }
      },
      success: (events) => {
        const n = offered(events);
        if (n === 0) {
          return "All skills are up to date";
        }
        return n === 1 ? "1 skill has an update" : `${n} skills have updates`;
      },
    });
  } finally {
    clearTimeout(wait);
  }
};
