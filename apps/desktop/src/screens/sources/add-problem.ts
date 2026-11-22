// A refusal of `skill add` in the app's words. The CLI's hint for a skill the library already
// holds names `agentx` commands, which a hint in the app never does.
import type { Problem } from "#/store/store.ts";

/** The CLI's reason for a skill whose name the library already holds, with that name. */
const held = /the library already holds (?<name>\S+) at /gu;

/** The count of skills a run over several could not install, from its message. */
const refusedCount = /^(?<n>\d+) of \d+ skills? could not be installed: /u;

/**
 * `problem` in the app's words when every skill it names was refused because the library already
 * holds one of that name, else undefined.
 */
export const addProblem = (problem: Problem): Problem | undefined => {
  const names = [...problem.message.matchAll(held)].map(
    (m) => m.groups?.name ?? ""
  );
  const n = refusedCount.exec(problem.message)?.groups?.n;
  const refused = n === undefined ? 1 : Number(n);
  if (names.length === 0 || names.length !== refused) {
    return undefined;
  }
  const one = names.length === 1;
  return {
    hint: `remove ${one ? "it" : "them"}, then add again`,
    message: `${names.join(", ")} ${one ? "skill is" : "are"} already in your library`,
  };
};
