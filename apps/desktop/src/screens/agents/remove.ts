import type { CliEvent } from "#/cli/events.gen.ts";
import { againRule, withAppHint } from "#/lib/app-hint.ts";
import { andList } from "#/lib/plural.ts";
import type { Outcome } from "#/store/runner.ts";

/**
 * "Removed Antigravity", or "Removed Antigravity and Antigravity CLI" when the deleted directory
 * held another configuration or several were removed: the names the CLI says it removed.
 */
export const removedText = (events: CliEvent[]) =>
  `Removed ${andList(
    events.filter((e) => e.type === "config_removal").map((e) => e.name)
  )}`;

/** A failed removal in the app's words: the CLI's message, and a hint that names Remove. */
export const removeFailure = ({ events }: Outcome) => {
  const error = events.find((e) => e.type === "error");
  return error === undefined
    ? undefined
    : withAppHint(
        error.hint === undefined
          ? { message: error.message }
          : { hint: error.hint, message: error.message },
        [againRule("remove it again")]
      );
};
