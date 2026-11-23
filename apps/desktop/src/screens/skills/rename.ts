// What renaming a hub skill takes, as pure functions over rows: whether Rename
// takes a skill, the command line, and a refusal's hint in the Rename dialog's own words
// (docs/spec/cli-contract.md, "Renaming a skill").
import type { ErrorEvent } from "#/cli/events.gen.ts";
import { againRule, doctorRule, rewriteHint } from "#/lib/app-hint.ts";
import type { HintRule } from "#/lib/app-hint.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";

/** Whether `skill rename` takes `row`: a hub skill, which a fork branch holds. */
export const canRename = (row: SkillRow) => row.kind === "hub";

/** `skill rename <old> <new>`. */
export const renameArgs = (old: string, newName: string): string[] => [
  "skill",
  "rename",
  old,
  newName,
];

/** The hints of `skill rename` that name a command, matched whole. */
const renameHintRules: HintRule[] = [
  {
    app: () => "choose another name",
    cli: /^choose another name with '.+'$/u,
  },
  {
    app: ({ old }) => `remove ${old ?? "it"} to finish the rename`,
    cli: /^run 'agentx skill remove (?<old>\S+)' to finish the rename$/u,
  },
  doctorRule,
  againRule("rename again"),
];

/**
 * The hint the Rename dialog shows for `error`: the CLI's, in the dialog's words where it names a
 * command.
 */
export const renameHint = (error: ErrorEvent): string | undefined =>
  rewriteHint(error.hint, renameHintRules);
