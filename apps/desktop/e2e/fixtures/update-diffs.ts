// What `skill diff <name> --update` emits for the updates of e2e/fixtures/updates.ts: one `diff`
// event per file the update changes, sorted by path, each with the patch git wrote.
import type { InlineEvent } from "../harness/cli.ts";

/** The newer version of pdf from its source: SKILL.md edited, a script added. */
export const pdfUpdateDiffs: InlineEvent[] = [
  {
    name: "pdf",
    patch: [
      "diff --git a/SKILL.md b/SKILL.md",
      "index 3f2a9c1..8e7d6c5 100644",
      "--- a/SKILL.md",
      "+++ b/SKILL.md",
      "@@ -1,6 +1,7 @@",
      " ---",
      " name: pdf",
      "-description: Fill and merge PDF forms.",
      "+description: Fill, merge and convert PDF forms.",
      " ---",
      " ",
      " # PDF",
      "+Run scripts/convert.py to turn a form into text.",
      "",
    ].join("\n"),
    path: "SKILL.md",
    status: "modified",
    type: "diff",
  },
  {
    name: "pdf",
    patch: [
      "diff --git a/scripts/convert.py b/scripts/convert.py",
      "new file mode 100644",
      "index 0000000..5d1c0a2",
      "--- /dev/null",
      "+++ b/scripts/convert.py",
      "@@ -0,0 +1,2 @@",
      "+import sys",
      "+print(open(sys.argv[1]).read())",
      "",
    ].join("\n"),
    path: "scripts/convert.py",
    status: "added",
    type: "diff",
  },
];

/** The new version of the original pdf-letters was forked from: SKILL.md edited, examples gone. */
export const lettersUpdateDiffs: InlineEvent[] = [
  {
    name: "pdf-letters",
    patch: [
      "diff --git a/SKILL.md b/SKILL.md",
      "index 1a2b3c4..9b8c7d6 100644",
      "--- a/SKILL.md",
      "+++ b/SKILL.md",
      "@@ -3,1 +3,1 @@",
      "-See examples.md for letters.",
      "+Write letters from the form fields.",
      "",
    ].join("\n"),
    path: "SKILL.md",
    status: "modified",
    type: "diff",
  },
  {
    name: "pdf-letters",
    patch: [
      "diff --git a/examples.md b/examples.md",
      "deleted file mode 100644",
      "index 4c3b2a1..0000000",
      "--- a/examples.md",
      "+++ /dev/null",
      "@@ -1,2 +0,0 @@",
      "-# Examples",
      "-Dear Sir or Madam,",
      "",
    ].join("\n"),
    path: "examples.md",
    status: "deleted",
    type: "diff",
  },
];
