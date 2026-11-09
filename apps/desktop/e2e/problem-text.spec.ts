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
