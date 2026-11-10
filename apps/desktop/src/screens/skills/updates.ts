// Updates on the Skills screen: Check updates (`skill check-updates`), the Update of one skill
// (`skill update <name>`, from its banner) and Update all (`skill update --all`,
// from the toast of a check that found more than one). The title bar's publish status runs the same
// check, one at a time with Check updates, and words its toast by what your other machines
// published (checkOtherMachines). Each runs one command through the runner
// and says what happened by what the CLI emitted; the list redraws from the inventory and the
// update check that follow, never from a result predicted here.
import { EXIT_CODES } from "#/cli/events.gen.ts";
import type {
  CliEvent,
  ErrorEvent,
  UpdateAvailableEvent,
} from "#/cli/events.gen.ts";
import { notify } from "#/components/toast.tsx";
import type { ToastAction } from "#/components/toast.tsx";
import { openSkill } from "#/screens/skills/open.ts";
import {
  fromAnotherMachine,
  shortSource,
} from "#/screens/skills/projection.ts";
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

/**
 * Check updates: `skill check-updates`, which fetches each source and lists every update it finds.
 * Its toast says how far it got, one source fetched at a time, then what it found: how many skills
 * have an update, with Update all when there is more than one. A source it could not reach
 * (exit 3) is named in the toast, with a line of the app's own, and the updates it found elsewhere are
 * listed all the same (src/store/loaders.ts).
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
  notify(checking(), cancel, { lasting: true });
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
        notify(checking(event.current, event.total), cancel, {
          inPlace: true,
          lasting: true,
        });
      }
    },
    success: (events) => {
      const n = offered(events);
      if (n === 0) {
        return "Everything is up to date";
      }
      return n === 1 ? "1 skill has an update" : `${n} skills have updates`;
    },
  });
};

/** The updates a check found, in name order: what another machine published, or from a source. */
const foundIn = (events: CliEvent[]) => {
  const found = events.filter(
    (e): e is UpdateAvailableEvent => e.type === "update_available"
  );
  return {
    fromMachines: found.filter(fromAnotherMachine),
    fromSources: found.filter((e) => !fromAnotherMachine(e)),
  };
};

/** What a check found on your other machines, then how many updates from sources. */
const otherMachinesText = (events: CliEvent[]) => {
  const { fromMachines, fromSources } = foundIn(events);
  if (fromMachines.length === 0) {
    return "Nothing new from your other machines";
  }
  const changed = `${plural(fromMachines.length, "skill", "skills")} changed on your other machines`;
  return fromSources.length === 0
    ? changed
    : `${changed}, ${plural(fromSources.length, "update", "updates")} from sources`;
};

/**
 * The check the title bar's publish status runs once everything is published: the same
 * `skill check-updates` as Check updates, and never beside it, which the status shows with a
 * spinner in place of its dot. Its toast says what your other machines published, with Review,
 * which opens the first skill they changed, and how many updates came from sources.
 */
export const checkOtherMachines = async () => {
  if (checkRunning()) {
    return;
  }
  await runCommand(["skill", "check-updates"], {
    action: (events) => {
      const [first] = foundIn(events).fromMachines;
      return first === undefined
        ? undefined
        : {
            label: "Review",
            onClick: () => {
              openSkill(first.name);
            },
          };
    },
    failure: unreachableFailure,
    label: CHECK_UPDATES,
    success: otherMachinesText,
  });
};
