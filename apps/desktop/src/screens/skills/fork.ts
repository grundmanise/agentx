// What forking a skill takes, as pure functions over a row and the inventory: which skills offer
// Fork, how `skill fork` names one, the command line a choice in the Fork dialog produces, and
// what the dialog says the fork does for each kind of skill, and a refusal's hint in the dialog's
// own words (docs/spec/cli-contract.md, "Forking a skill").
import { EXIT_CODES } from "#/cli/events.gen.ts";
import type { ErrorEvent, SkillNode } from "#/cli/events.gen.ts";
import { againRule, doctorRule, rewriteHint } from "#/lib/app-hint.ts";
import type { HintRule } from "#/lib/app-hint.ts";
import { directoryName, occurrenceOf } from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";

/**
 * Whether the detail pane offers Fork: for a skill of a source, a plugin's skill and an unmanaged
 * skill in the library. One of your own skills is already yours, and a skill an agent client holds
 * in its own skills directory is one `skill fork` cannot find.
 */
export const canFork = (row: SkillRow) =>
  row.kind === "source" ||
  row.kind === "plugin" ||
  (row.kind === "disk" && row.entry !== undefined);

/**
 * How `skill fork` names `row`: a library skill by its name; a plugin's skill by its directory,
 * as `<plugin>:<directory>` when the plugins of the machine provide more than one skill of that
 * directory's name, which the CLI otherwise refuses.
 */
export const forkTarget = (row: SkillRow, skills: SkillNode[]): string => {
  const occurrence = row.kind === "plugin" ? occurrenceOf(row) : undefined;
  if (occurrence?.plugin === undefined) {
    return row.name;
  }
  const directory = directoryName(occurrence.path);
  const plugins = new Set(
    skills
      .flatMap((node) => node.occurrences)
      .filter(
        (o) => o.plugin !== undefined && directoryName(o.path) === directory
      )
      .map((o) => o.plugin)
  );
  return plugins.size > 1 ? `${occurrence.plugin}:${directory}` : directory;
};

/** `skill fork <target>`, with `--name <new>` for a fork under a new name. */
export const forkArgs = (target: string, newName?: string): string[] =>
  newName === undefined
    ? ["skill", "fork", target]
    : ["skill", "fork", target, "--name", newName];

/** What the Fork dialog says forking `row` does, by its kind. */
export const forkText = (row: SkillRow): string => {
  switch (row.kind) {
    case "plugin": {
      return `It becomes one of your skills, in your library. The plugin's ${row.name} stays as it is; agentx tells you after forking what each agent sees.`;
    }
    case "disk": {
      return `It becomes one of your skills and replaces ${row.name} in every agent that has it. agentx then keeps its history.`;
    }
    case "source":
    case "yours": {
      return `It becomes one of your skills and replaces ${row.name} in every agent that has it. You can still take new versions of the original.`;
    }
    default: {
      return "";
    }
  }
};

/** The switch that forks beside the skill, as a hint names it. */
const USE_A_NEW_NAME = "turn on Use a new name";

/**
 * The hint of a fork in place that the CLI refused, in the dialog's words. The contract gives
 * these refusals exit 6 and the code `refused` and no field of their own, so each is told by the
 * start of its message, as the contract words it. The way out the CLI names, forking beside the
 * skill with `--name <new>`, is the Use a new name switch, and is left out when it is already on.
 */
const inPlaceHint = (
  message: string,
  target: string,
  renaming: boolean
): string | undefined => {
  const beside = renaming ? "" : `, or ${USE_A_NEW_NAME}`;
  if (message.startsWith(`${target} is itself a Git repository, at `)) {
    return `move its .git out of the skill${beside}`;
  }
  if (/^.+ is a symlink to .+; forking .+ in its place /u.test(message)) {
    return `replace the link with the directory it points to, then fork again${beside}`;
  }
  if (
    message.startsWith(
      "the library and agentx home are on different file systems"
    )
  ) {
    return `keep the library and agentx home on one file system${beside}`;
  }
  if (message === `${target} is already one of your own skills`) {
    return USE_A_NEW_NAME;
  }
  return undefined;
};

/** The other hints of `skill fork` that name a flag or a command, matched whole. */
const forkHintRules = (renaming: boolean): HintRule[] => [
  {
    app: () => `${USE_A_NEW_NAME} and give it a valid name`,
    cli: /^fork it under a valid name with '.+'$/u,
  },
  {
    app: () =>
      renaming
        ? "choose another name"
        : `${USE_A_NEW_NAME} and choose another name`,
    cli: /^choose another name with --name <new>$/u,
  },
  {
    app: () => "move it aside, then fork again",
    cli: /^move it aside, then run '.+' again$/u,
  },
  doctorRule,
  againRule("fork again"),
];

/**
 * The hint the Fork dialog shows for `error`: the CLI's, but in the dialog's words where it names
 * a flag or a command. `renaming` is whether Use a new name was on when Fork ran.
 */
export const forkHint = (
  error: ErrorEvent,
  exitCode: number | null,
  target: string,
  renaming: boolean
): string | undefined =>
  (exitCode === EXIT_CODES.refused && error.code === "refused"
    ? inPlaceHint(error.message, target, renaming)
    : undefined) ?? rewriteHint(error.hint, forkHintRules(renaming));
