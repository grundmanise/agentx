// What forking a skill takes, as pure functions over a row and the inventory: which skills offer
// Fork, how `skill fork` names one, the command line a choice in the Fork dialog produces, and
// what the dialog says the fork does for each kind of skill, and a refusal's hint in the dialog's
// own words (docs/spec/cli-contract.md, "Forking a skill").
import { EXIT_CODES } from "#/cli/events.gen.ts";
import type { ErrorEvent } from "#/cli/events.gen.ts";
import { againRule, doctorRule, rewriteHint } from "#/lib/app-hint.ts";
import type { HintRule } from "#/lib/app-hint.ts";
import { SKILL_NAME_LIMIT } from "#/lib/skill-name.ts";
import { directoryName, occurrenceOf } from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";

/**
 * Whether the detail pane offers Fork: for a skill of a source and a plugin's skill. An account repo
 * skill is already one, and `skill fork` refuses an unmanaged skill, which Adopt takes
 * over.
 */
export const canFork = (row: SkillRow) =>
  row.kind === "source" || row.kind === "plugin";

/**
 * How `skill fork` names `row`: a library skill by its name, and a plugin's skill always as
 * `<plugin>:<directory>`, which names that plugin's copy and nothing else.
 */
export const forkTarget = (row: SkillRow): string => {
  const occurrence = row.kind === "plugin" ? occurrenceOf(row) : undefined;
  return occurrence?.plugin === undefined
    ? row.name
    : `${occurrence.plugin}:${directoryName(occurrence.path)}`;
};

/** `skill fork <target>`, with `--name <new>` for a fork under a new name. */
export const forkArgs = (target: string, newName?: string): string[] =>
  newName === undefined
    ? ["skill", "fork", target]
    : ["skill", "fork", target, "--name", newName];

/** What the Fork dialog says forking `row` does: a plugin's skill is forked beside the plugin's. */
export const forkText = (row: SkillRow): string =>
  row.kind === "plugin"
    ? "A new copy will be created. Pick a different name, or agents with the plugin will see two skills with that name."
    : "A new copy will be created. Keep the same name to replace this skill, or pick a different name to keep both.";

/**
 * Whether Use a different name starts on: for a plugin's skill, whose fork under its own name
 * leaves more than one skill of that name for the agents with the plugin.
 */
export const renamesAtFirst = (row: SkillRow) => row.kind === "plugin";

const PREFIX = "my-";

/**
 * The name the Name field starts with: `my-<name>` for a plugin's skill, so Fork works at once; a
 * name too long to take the prefix is cut to the limit, without a hyphen at its end.
 */
export const firstName = (row: SkillRow) =>
  renamesAtFirst(row)
    ? `${PREFIX}${row.name}`.slice(0, SKILL_NAME_LIMIT).replace(/-+$/u, "")
    : "";

/**
 * The Use a different name switch and the name field as they stand once a refusal shows: off; on with
 * no name typed, as exit 6 leaves them when Fork ran with the switch off, since the dialog then
 * turns it on; or on with a name typed.
 */
export type ForkFields = "off" | "on" | "named";

/**
 * The form a refusal of a fork leaves: exit 6 turns the switch on. `renaming` and `name` are the
 * switch and the name field when Fork ran.
 */
export const formAfter = (
  exitCode: number | null,
  renaming: boolean,
  name: string
): ForkFields => {
  if (renaming) {
    return name === "" ? "on" : "named";
  }
  return exitCode === EXIT_CODES.refused ? "on" : "off";
};

/** What is left to do to fork beside the skill, as the form stands, or nothing once a name is typed. */
const besideWays: Record<ForkFields, string | undefined> = {
  named: undefined,
  off: "use a different name",
  on: "type a new name",
};

/** `hint`, then `, or <the way beside>` when the form leaves one. */
const orBeside = (hint: string, form: ForkFields) => {
  const way = besideWays[form];
  return way === undefined ? hint : `${hint}, or ${way}`;
};

/**
 * The hint of a fork in place that the CLI refused, in the dialog's words. The contract gives
 * these refusals exit 6 and the code `refused` and no field of their own, so each is told by the
 * start of its message, as the contract words it. The way out the CLI names, forking beside the
 * skill with `--name <new>`, is the switch and the name field, named as the form stands.
 */
const inPlaceHint = (
  message: string,
  target: string,
  form: ForkFields
): string | undefined => {
  if (message.startsWith(`${target} has a Git repository at `)) {
    return orBeside("move .git out of the skill", form);
  }
  if (/^.+ is a symlink to .+; forking .+ in its place /u.test(message)) {
    return orBeside(
      "replace the link with the directory it points to, then fork again",
      form
    );
  }
  if (
    message.startsWith(
      "the library and agentx home are on different file systems"
    )
  ) {
    return orBeside(
      "keep the library and agentx home on one file system",
      form
    );
  }
  if (message === `${target} is already an account skill`) {
    return besideWays[form] ?? "choose another name";
  }
  return undefined;
};

/** The other hints of `skill fork` that name a flag or a command, matched whole. */
const forkHintRules = (form: ForkFields): HintRule[] => [
  {
    app: () =>
      form === "off"
        ? "turn on Use a different name and give it a valid name"
        : "type a valid new name",
    cli: /^fork it under a valid name with '.+'$/u,
  },
  {
    app: () => {
      if (form === "off") {
        return "turn on Use a different name and choose another name";
      }
      return form === "on" ? "type a new name" : "choose another name";
    },
    cli: /^choose another name with --name <new>$/u,
  },
  {
    app: () => "finish the update first",
    cli: /^run '.+ --abort' to give the merge up/u,
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
 * a flag or a command. `form` is the switch and the name field as the refusal leaves them.
 */
export const forkHint = (
  error: ErrorEvent,
  exitCode: number | null,
  target: string,
  form: ForkFields
): string | undefined =>
  (exitCode === EXIT_CODES.refused && error.code === "refused"
    ? inPlaceHint(error.message, target, form)
    : undefined) ?? rewriteHint(error.hint, forkHintRules(form));
