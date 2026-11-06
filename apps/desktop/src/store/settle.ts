// A control that changes the machine (a chip, a switch, a banner's button) shows pending until its
// command ends and the inventory that follows it has arrived: the command's outcome and the
// inventory's freshness are separate states (docs/spec/inventory-ordering.md, "Refresh after a
// command"), and what the control shows comes from that inventory, never from the result. The
// runner (src/store/runner.ts) clears its pending mark when the command ends; this module keeps
// the control pending through the refresh the runner asked for.
import { create } from "zustand";

import { runCommand, usePending } from "#/store/runner.ts";
import type { CommandOptions, Outcome } from "#/store/runner.ts";
import { useAppStore } from "#/store/store.ts";
import type { AppState } from "#/store/store.ts";

interface Settling {
  /** The controls whose command ended and whose inventory has not arrived yet. */
  labels: Record<string, true>;
}

const useSettlingStore = create<Settling>()(() => ({ labels: {} }));

/** Whether every refresh asked for so far is answered, or the last scan failed and none will be. */
const settledIn = (s: AppState) =>
  Object.keys(s.refreshes).length === 0 || s.freshness.kind === "failed";

const clear = (label: string) => {
  useSettlingStore.setState((s) => {
    const { [label]: _, ...rest } = s.labels;
    return { labels: rest };
  });
};

/** Clears the control's mark once the serve child has answered every refresh request sent so far. */
const clearWhenSettled = (label: string) => {
  if (settledIn(useAppStore.getState())) {
    clear(label);
    return;
  }
  const unsubscribe = useAppStore.subscribe((s) => {
    if (settledIn(s)) {
      unsubscribe();
      clear(label);
    }
  });
};

/**
 * Runs a command for the control named `options.label`, as `runCommand` does, and keeps the
 * control pending until the inventory that follows the command has arrived. It resolves when the
 * command ends, as `runCommand` does, so the caller can act on the outcome at once.
 */
export const runUntilSettled = async (
  args: readonly string[],
  options: CommandOptions
): Promise<Outcome> => {
  const { label } = options;
  useSettlingStore.setState((s) => ({
    labels: { ...s.labels, [label]: true },
  }));
  const outcome = await runCommand(args, options);
  // The runner asked for the refresh before it returned: the mark goes with its answer.
  clearWhenSettled(label);
  return outcome;
};

/** Whether the control named `label` waits: its command runs, or its inventory is on its way. */
export const useSettling = (label: string) => {
  const running = usePending(label);
  const waiting = useSettlingStore((s) => Object.hasOwn(s.labels, label));
  return running || waiting;
};

/**
 * Calls `then` once the serve child has answered every refresh request sent so far, or the last
 * scan failed and none will be: when the inventory that follows a command has arrived.
 */
export const afterSettled = (then: () => void) => {
  if (settledIn(useAppStore.getState())) {
    then();
    return;
  }
  const unsubscribe = useAppStore.subscribe((s) => {
    if (settledIn(s)) {
      unsubscribe();
      then();
    }
  });
};
