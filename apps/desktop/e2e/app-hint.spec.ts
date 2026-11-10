// A CLI hint that names a flag or an `agentx` command, in the words of the place that shows it:
// the Fork dialog's hints by `forkHint`, and the shared rules, as tables.
import { expect, test } from "@playwright/test";

import type { ErrorEvent } from "#/cli/events.gen.ts";
import { againRule, doctorRule, rewriteHint } from "#/lib/app-hint.ts";
import { forkHint } from "#/screens/skills/fork.ts";

const refused = (message: string, hint: string): ErrorEvent => ({
  code: "refused",
  hint,
  message,
  schema_version: 1,
  type: "error",
});

const beside =
  "fork it beside the skill with 'agentx skill fork scratch --name <new>'";

// [the CLI's error, Use a new name on, the hint the dialog shows]
const forkTable: [ErrorEvent, boolean, string | undefined][] = [
  [
    refused(
      "scratch is itself a Git repository, at /home/me/.agents/skills/scratch/.git; forking it in its place would move that repository into the fork's worktree, where git would take it for the fork",
      `move its .git out of the skill, then run 'agentx skill fork scratch' again, or ${beside}`
    ),
    false,
    "move its .git out of the skill, or turn on Use a new name",
  ],
  [
    refused(
      "scratch is itself a Git repository, at /home/me/.agents/skills/scratch/.git",
      `move its .git out of the skill, then run 'agentx skill fork scratch' again, or ${beside}`
    ),
    true,
    "move its .git out of the skill",
  ],
  [
    refused(
      "/home/me/.agents/skills/scratch is a symlink to /home/me/work/scratch; forking scratch in its place moves the library directory into the fork's worktree and would move the link rather than the files it leads to",
      `replace the link with the directory it points to, then run 'agentx skill fork scratch' again, or ${beside}`
    ),
    false,
    "replace the link with the directory it points to, then fork again, or turn on Use a new name",
  ],
  [
    refused(
      "the library and agentx home are on different file systems, so scratch cannot be moved into its fork's worktree",
      `keep AGENTX_HOME and AGENTX_LIBRARY on one file system, or ${beside}`
    ),
    false,
    "keep the library and agentx home on one file system, or turn on Use a new name",
  ],
  [
    refused(
      "scratch is already one of your own skills",
      "fork it under a new name with 'agentx skill fork scratch --name <new>'"
    ),
    false,
    "turn on Use a new name",
  ],
  [
    refused(
      "Scratch is not a valid skill name",
      "fork it under a valid name with 'agentx skill fork Scratch --name <new>'"
    ),
    false,
    "turn on Use a new name and give it a valid name",
  ],
  [
    refused(
      "the account repo already holds notes as one of your own skills",
      "choose another name with --name <new>"
    ),
    true,
    "choose another name",
  ],
  [
    refused(
      "the account repo already holds scratch as one of your own skills",
      "choose another name with --name <new>"
    ),
    false,
    "turn on Use a new name and choose another name",
  ],
  [
    refused(
      "/home/me/.agentx/worktrees/scratch already exists",
      "move it aside, then run 'agentx skill fork scratch' again"
    ),
    false,
    "move it aside, then fork again",
  ],
  [
    refused(
      "the import branch refs/heads/managed/scratch records no version agentx can read",
      "run 'agentx doctor' and check the account repo it names"
    ),
    false,
    "use Run checks in Settings to check the account repo",
  ],
  [
    refused(
      "scratch changed while it was being forked, so nothing was changed",
      "run the command again"
    ),
    false,
    "fork again",
  ],
  // A hint with no flag or command is shown as the CLI wrote it.
  [
    refused(
      "a skill name is at most 64 characters",
      "choose a name such as my-skill"
    ),
    false,
    "choose a name such as my-skill",
  ],
];

for (const [error, renaming, want] of forkTable) {
  test(`forkHint(${JSON.stringify(error.hint)}, renaming ${renaming})`, () => {
    expect(forkHint(error, 6, "scratch", renaming)).toBe(want);
  });
}

test("forkHint tells a refusal by exit 6 and the code refused, not by the message alone", () => {
  const error = {
    ...refused(
      "scratch is itself a Git repository, at /home/me/.agents/skills/scratch/.git",
      "move its .git out of the skill"
    ),
    code: "internal",
  } as const;
  expect(forkHint(error, 10, "scratch", false)).toBe(
    "move its .git out of the skill"
  );
});

const sharedTable: [string | undefined, string | undefined][] = [
  [
    "run 'agentx doctor' and check the library it names",
    "use Run checks in Settings to check the library",
  ],
  [
    "run 'agentx doctor' and check agentx home",
    "use Run checks in Settings to check agentx home",
  ],
  [
    "make SKILL.md UTF-8 text, then run the command again",
    "make SKILL.md UTF-8 text, then publish again",
  ],
  [
    "check that you can write to the library",
    "check that you can write to the library",
  ],
  [undefined, undefined],
];

for (const [hint, want] of sharedTable) {
  test(`rewriteHint(${JSON.stringify(hint)})`, () => {
    expect(rewriteHint(hint, [doctorRule, againRule("publish again")])).toBe(
      want
    );
  });
}
