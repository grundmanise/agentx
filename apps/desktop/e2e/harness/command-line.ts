// Whether the CLI would take a command line, by the commands and flags the CLI's own command tree
// gives (src/cli/commands.gen.ts). The mocked CLI refuses what this refuses, so a command or a
// flag renamed in the CLI fails the app's tests rather than the app. How many arguments a command
// takes is not checked: the generated list does not hold it.
import { CLI_COMMANDS, GLOBAL_FLAGS } from "#/cli/commands.gen.ts";
import type { CliFlags } from "#/cli/commands.gen.ts";

const isFlag = (arg: string) => arg.startsWith("-") && arg !== "-";

/** The command the leading words name: the longest run of them that is a command. */
const commandOf = (words: readonly string[]) =>
  words
    .map((_, i) => words.slice(0, words.length - i).join(" "))
    .find((name) => Object.hasOwn(CLI_COMMANDS, name));

/** Names the first word that is no command, after the words that group commands. */
const unknownCommand = (words: readonly string[]) => {
  const names = Object.keys(CLI_COMMANDS);
  const group = words
    .map((_, i) => words.slice(0, words.length - i).join(" "))
    .find((prefix) => names.some((name) => name.startsWith(`${prefix} `)));
  const depth = group === undefined ? 0 : group.split(" ").length;
  return `agentx ${group === undefined ? "" : `${group} `}has no command "${words[depth] ?? ""}"`;
};

/**
 * Checks the flags at the start of `args` against `flags`, up to the first argument that is not a
 * flag or a flag's value, whose index is `rest`. `reason` says why the CLI would refuse them.
 */
const checkFlags = (
  args: readonly string[],
  flags: CliFlags,
  where: string
): { reason?: string; rest: number } => {
  let i = 0;
  while (i < args.length) {
    const arg = args[i] ?? "";
    if (arg === "--") {
      return { rest: args.length };
    }
    if (!isFlag(arg)) {
      return { rest: i };
    }
    if (!arg.startsWith("--")) {
      return {
        reason: `${arg} is a short flag; the app uses the long names, which commands.gen.ts lists`,
        rest: i,
      };
    }
    const body = arg.slice(2);
    const eq = body.indexOf("=");
    const name = eq === -1 ? body : body.slice(0, eq);
    const value = eq === -1 ? undefined : body.slice(eq + 1);
    if (!Object.hasOwn(flags, name)) {
      return { reason: `${where} has no flag --${name}`, rest: i };
    }
    const takesValue = flags[name];
    if (!takesValue && value !== undefined) {
      return {
        reason: `--${name} is a boolean flag and takes no value, but was given "${value}"`,
        rest: i,
      };
    }
    if (takesValue && value === undefined) {
      if (i + 1 >= args.length) {
        return {
          reason: `--${name} takes a value and was given none`,
          rest: i,
        };
      }
      i += 1;
    }
    i += 1;
  }
  return { rest: i };
};

/** Why the CLI would refuse `args`, or undefined when it would take them. */
export const refusal = (args: readonly string[]): string | undefined => {
  // Global flags may come before the command.
  const lead = checkFlags(args, GLOBAL_FLAGS, "agentx");
  if (lead.reason !== undefined) {
    return lead.reason;
  }
  const after = args.slice(lead.rest);
  const firstFlag = after.findIndex(isFlag);
  const words = firstFlag === -1 ? after : after.slice(0, firstFlag);
  const command = commandOf(words);
  if (command === undefined) {
    return unknownCommand(words);
  }
  const flags = { ...GLOBAL_FLAGS, ...CLI_COMMANDS[command] };
  // Arguments and flags may mix after the command words.
  let rest = after.slice(command.split(" ").length);
  while (rest.length > 0) {
    const checked = checkFlags(rest, flags, `agentx ${command}`);
    if (checked.reason !== undefined) {
      return checked.reason;
    }
    // Skip the argument the check stopped at.
    rest = rest.slice(checked.rest + 1);
  }
  return undefined;
};
