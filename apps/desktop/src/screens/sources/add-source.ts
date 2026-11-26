// Add source runs after its dialog closed: `source add <link>` fetches the source while the user
// goes on, and the Sources row shows a pill with a spinner for each link that is being added,
// whose Stop cancels it. Each link has its own run, so several can be added at once.
import { create } from "zustand";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type { SourceEvent } from "#/cli/events.gen.ts";
import { rewriteHint } from "#/lib/app-hint.ts";
import type { HintRule } from "#/lib/app-hint.ts";
import { count, HUB_LABEL } from "#/screens/sources/projection.ts";
import { agentx, cancelCommand } from "#/store/runner.ts";
import type { Outcome } from "#/store/runner.ts";
import type { Problem } from "#/store/store.ts";

/**
 * The hints of `source add` that name a command, in the app's words: the hub's skills
 * are the ones under Hub.
 */
export const addSourceHintRules: HintRule[] = [
  {
    app: () => `see and install its skills under ${HUB_LABEL}`,
    cli: /^run 'agentx skill list --remote' to see its skills, then 'agentx skill add --name <name>' to install one$/u,
  },
];

/**
 * What went wrong with a command run on a link the user typed, such as `source add`, in
 * the CLI's words: a source git cannot reach as "Couldn't reach {link}", and the CLI's hint in the
 * app's words by `rules`.
 */
export const linkProblem = (
  outcome: Outcome,
  {
    command,
    rules,
    typed,
  }: { command: string; rules: readonly HintRule[]; typed: string }
): Problem => {
  const error = outcome.events.find((e) => e.type === "error");
  const message =
    outcome.exitCode === EXIT_CODES.source
      ? `Couldn't reach ${typed}`
      : (error?.message ??
        outcome.startError ??
        `agentx ${command} exited with code ${outcome.exitCode ?? "none"}`);
  const hint = rewriteHint(error?.hint, rules);
  return hint === undefined ? { message } : { hint, message };
};

/** The pending label of `source add` for the link `typed`. */
export const addSourceLabel = (typed: string) => `add source ${typed}`;

/** The links being added, in the order they were. */
export const useAddingSources = create<{ links: readonly string[] }>()(() => ({
  links: [],
}));

const setAdding = (typed: string, on: boolean) => {
  useAddingSources.setState(({ links }) => ({
    links: [...links.filter((l) => l !== typed), ...(on ? [typed] : [])],
  }));
};

/** Stops the add of `typed`. */
export const stopAddingSource = (typed: string) => {
  void cancelCommand(addSourceLabel(typed));
};

/**
 * Runs `source add <typed>` and resolves with the source it added; undefined when it was stopped
 * or failed, which a toast says. A link already being added is not added again.
 */
export const addSource = async (
  typed: string
): Promise<SourceEvent | undefined> => {
  if (useAddingSources.getState().links.includes(typed)) {
    return undefined;
  }
  setAdding(typed, true);
  const outcome = await agentx(["source", "add", typed], {
    failure: (ended) => ({
      ...linkProblem(ended, {
        command: "source add",
        rules: addSourceHintRules,
        typed,
      }),
      keep: [typed],
    }),
    label: addSourceLabel(typed),
    retry: () => {
      void addSource(typed);
    },
    success: (events) => {
      const source = events.find((e) => e.type === "source");
      return `Added ${source?.url ?? typed}: ${count(source?.skills ?? 0, "skill")}`;
    },
  });
  setAdding(typed, false);
  return outcome.exitCode === EXIT_CODES.ok
    ? outcome.events.find((e) => e.type === "source")
    : undefined;
};
