// What forking a skill takes, as pure functions over a row and the inventory: which skills offer
// Fork, how `skill fork` names one, the command line a choice in the Fork dialog produces, and
// what the dialog says the fork does for each kind of skill (docs/spec/cli-contract.md, "Forking
// a skill").
import type { SkillNode } from "#/cli/events.gen.ts";
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

const SKILL_NAME = /^[a-z0-9]+(?:-[a-z0-9]+)*$/u;

/** Whether `name` is one a skill can take: 1 to 64 lowercase letters, digits and single hyphens. */
export const isSkillName = (name: string) =>
  name.length <= 64 && SKILL_NAME.test(name);

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
