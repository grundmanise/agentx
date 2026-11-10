// The app's words for a CLI hint (DESIGN.md §6 "Refusal under a field"): a hint never names a CLI
// flag or an `agentx` command, so where the CLI's hint does, the place that shows it puts the
// control that does the same in the app in its stead. Each rule matches one hint of the CLI's, in
// the words docs/spec/cli-contract.md gives it, whole: a hint no rule matches is shown as the CLI
// wrote it.
import type { Problem } from "#/store/store.ts";

/** One CLI hint, matched whole, and the app's words for it. */
export interface HintRule {
  cli: RegExp;
  /** The app's words, from the named groups of the match. */
  app: (groups: Partial<Record<string, string>>) => string;
}

/** The app's words for `hint` by the first rule that matches it, or the hint as it is. */
export const rewriteHint = (
  hint: string | undefined,
  rules: readonly HintRule[]
): string | undefined => {
  if (hint === undefined) {
    return undefined;
  }
  for (const rule of rules) {
    const match = rule.cli.exec(hint);
    if (match !== null) {
      return rule.app(match.groups ?? {});
    }
  }
  return hint;
};

/** `problem` with its hint in the app's words. */
export const withAppHint = (
  problem: Problem,
  rules: readonly HintRule[]
): Problem => {
  const hint = rewriteHint(problem.hint, rules);
  return hint === undefined
    ? { message: problem.message }
    : { hint, message: problem.message };
};

/** `agentx doctor`: Run checks, in the Command line section of Settings. */
export const doctorRule: HintRule = {
  app: ({ what }) => `use Run checks in Settings to check ${what ?? ""}`,
  cli: /^run 'agentx doctor' and check (?<what>.+?)(?: it names)?$/u,
};

/** "…, then run the command again": the command is the control's, done again with `verb`. */
export const againRule = (verb: string): HintRule => ({
  app: ({ first }) => (first === undefined ? verb : `${first}, then ${verb}`),
  cli: /^(?:(?<first>.+), then )?run the command again$/u,
});
