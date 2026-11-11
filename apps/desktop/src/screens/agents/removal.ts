// Which agent configurations can be removed, and removing them. The preview of `config remove`
// (docs/spec/cli-contract.md, "Machine settings") judges every detected configuration and changes
// nothing: the Agents page reads it when it opens, when the detected configurations change and
// when a dialog that removes opens, so a control never offers a removal the CLI would refuse.
// `config remove <id>...` deletes the configuration directories; the cards follow the inventory.
import { create } from "zustand";

import type { ConfigRemovalEvent } from "#/cli/events.gen.ts";
import type { ErrorToast } from "#/components/toast.tsx";
import { againRule, withAppHint } from "#/lib/app-hint.ts";
import type { HintRule } from "#/lib/app-hint.ts";
import { plural } from "#/lib/plural.ts";
import { runCommand } from "#/store/runner.ts";
import type { Outcome } from "#/store/runner.ts";
import { runUntilSettled } from "#/store/settle.ts";

interface Removals {
  /** The last preview: one event per detected configuration. Undefined until one was read. */
  events: ConfigRemovalEvent[] | undefined;
}

const useRemovals = create<Removals>()(() => ({ events: undefined }));

// The latest preview started: a slower earlier one does not overwrite it.
let previews = 0;

/** Reads which configurations can be removed: `config remove`, a preview that changes nothing. */
export const loadRemovals = async () => {
  previews += 1;
  const mine = previews;
  const outcome = await runCommand(["config", "remove"], {
    label: "load config remove",
  });
  if (outcome.exitCode === 0 && previews === mine) {
    useRemovals.setState({
      events: outcome.events.filter((e) => e.type === "config_removal"),
    });
  }
};

/** What the last preview said of the configuration `id`; undefined before it, or when it is gone. */
export const useRemoval = (id: string) =>
  useRemovals((s) => s.events?.find((e) => e.id === id));

/** Every configuration of the last preview; empty before one was read. */
export const useRemovalEvents = () => useRemovals((s) => s.events) ?? [];

/** The label of the control that removes configurations: the runner's name for it. */
export const REMOVE_LABEL = "remove agent configurations";

/**
 * The configurations a removal of `chosen` names: each one and the configurations kept in its
 * directory, which the CLI removes only together, once each.
 */
export const idsToRemove = (chosen: readonly ConfigRemovalEvent[]) => [
  ...new Set(chosen.flatMap((e) => [e.id, ...e.with])),
];

/** "Removed AdaL", or "Removed 3 agent configurations", from what the CLI says it removed. */
const removedText = (events: Outcome["events"]) => {
  const removed = events.filter(
    (e) => e.type === "config_removal" && e.state === "removed"
  );
  const [only] = removed;
  return removed.length === 1 && only?.type === "config_removal"
    ? `Removed ${only.name}`
    : `Removed ${plural(removed.length, "agent configuration")}`;
};

/** The CLI's hint that names the command for the configurations that could go: Clean up does it. */
const othersRule: HintRule = {
  app: () => "open Clean up again to remove the others",
  cli: /^'agentx config remove .+' removes the others$/u,
};

/** A refusal in the app's words: the CLI's message, and a hint that names the app's own control. */
const failureOf = ({ events }: Outcome): ErrorToast | undefined => {
  const error = events.find((e) => e.type === "error");
  return error === undefined
    ? undefined
    : withAppHint(
        error.hint === undefined
          ? { message: error.message }
          : { hint: error.hint, message: error.message },
        [othersRule, againRule("remove it again")]
      );
};

/**
 * Removes `chosen` in one command, with the configurations kept in their directories. The preview
 * is read again afterwards, whatever the outcome: a refusal means it no longer holds.
 */
export const removeConfigurations = async (
  chosen: readonly ConfigRemovalEvent[]
) => {
  await runUntilSettled(["config", "remove", ...idsToRemove(chosen)], {
    failure: failureOf,
    label: REMOVE_LABEL,
    success: removedText,
  });
  void loadRemovals();
};
