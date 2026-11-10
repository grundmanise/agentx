// problemSentence, as a table: the CLI's lowercase message and hint, as the app shows them.
import { expect, test } from "@playwright/test";

import { problemSentence } from "#/lib/problem-text.ts";
import type { Problem } from "#/store/store.ts";

const table: [Problem, string][] = [
  [
    {
      hint: "choose another name",
      message:
        "the account repo already holds release-notes as one of your own skills",
    },
    "The account repo already holds release-notes as one of your own skills. Choose another name.",
  ],
  [{ message: "no such skill" }, "No such skill."],
  [{ hint: "try again.", message: "it failed." }, "It failed. Try again."],
  [
    { hint: "is it public?", message: "not found!" },
    "Not found! Is it public?",
  ],
  [
    {
      hint: "run 'agentx source add owner/name' first",
      message: "unknown source owner/name",
    },
    "Unknown source owner/name. Run 'agentx source add owner/name' first.",
  ],
  [
    { hint: "  stop it first  ", message: "agentx serve is already running" },
    "agentx serve is already running. Stop it first.",
  ],
  [{ hint: "", message: "Couldn't reach x" }, "Couldn't reach x."],
  [{ message: "élan is taken" }, "Élan is taken."],
  [
    {
      hint: "couldn't fetch it",
      message: "github.com/acme/skills is unreachable",
    },
    "github.com/acme/skills is unreachable. Couldn't fetch it.",
  ],
  [
    {
      hint: "list its skills",
      message: "https://github.com/me/account is the account remote",
    },
    "https://github.com/me/account is the account remote. List its skills.",
  ],
  [{ message: "release-notes is taken" }, "release-notes is taken."],
  [{ message: "can't write ~/.agentx" }, "Can't write ~/.agentx."],
];

for (const [problem, want] of table) {
  test(`problemSentence(${JSON.stringify(problem)})`, () => {
    expect(problemSentence(problem)).toBe(want);
  });
}

// With names to keep: a known name that starts the message or the hint keeps its case, and the
// same word is capitalised where it is not one of them.
const keepTable: [Problem, readonly string[], string][] = [
  [
    {
      hint: "move its .git out of the skill, or fork it beside the skill with --name <new>",
      message:
        "scratch is itself a Git repository, at /home/me/.agents/skills/scratch/.git",
    },
    ["scratch"],
    "scratch is itself a Git repository, at /home/me/.agents/skills/scratch/.git. Move its .git out of the skill, or fork it beside the skill with --name <new>.",
  ],
  [
    {
      hint: "scratch is the name to fork it under",
      message: "the library entry is a symlink",
    },
    ["scratch"],
    "The library entry is a symlink. scratch is the name to fork it under.",
  ],
  [
    { hint: "scratch it and try again", message: "scratch is taken" },
    [],
    "Scratch is taken. Scratch it and try again.",
  ],
  [{ message: "scratch is taken" }, ["notes"], "Scratch is taken."],
  [{ message: "scratches are taken" }, ["scratch"], "Scratches are taken."],
  [{ message: "Scratch is taken" }, ["scratch"], "Scratch is taken."],
  [
    { hint: "stop it first", message: "agentx serve is already running" },
    ["scratch"],
    "agentx serve is already running. Stop it first.",
  ],
  [
    { message: "github.com/acme/skills is unreachable" },
    ["scratch"],
    "github.com/acme/skills is unreachable.",
  ],
  [
    { message: "~/.agents/skills/scratch is not a directory" },
    ["scratch"],
    "~/.agents/skills/scratch is not a directory.",
  ],
  [
    { hint: "choose another name", message: "release-notes is taken" },
    ["scratch"],
    "release-notes is taken. Choose another name.",
  ],
];

for (const [problem, keep, want] of keepTable) {
  test(`problemSentence(${JSON.stringify(problem)}, { keep: ${JSON.stringify(keep)} })`, () => {
    expect(problemSentence(problem, { keep })).toBe(want);
  });
}
