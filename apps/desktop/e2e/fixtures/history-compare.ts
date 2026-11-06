// What `skill diff pdf-letters --commit <commit>` reports for a commit of pdf-letters' history
// (e2e/fixtures/skills.ts): how the skill as it is now differs from that commit, one `diff` event
// per file, sorted by path, as the CLI emits them.
import type { DiffEvent } from "#/cli/events.gen.ts";

/** The commit the tests compare with now: "pdf-letters: edit SKILL.md and add examples.md". */
export const comparedCommit = "4c2e9a1bbbb";

const diffEvent = (
  path: string,
  status: DiffEvent["status"],
  patch: string[]
): DiffEvent => ({
  name: "pdf-letters",
  patch: patch.join("\n"),
  path,
  schema_version: 2,
  status,
  type: "diff",
});

/** Since the commit: SKILL.md edited, examples.md deleted, letter.md added. */
export const comparedDiffs: DiffEvent[] = [
  diffEvent("SKILL.md", "modified", [
    "diff --git a/SKILL.md b/SKILL.md",
    "index 3c1d2e4..7a8b9c0 100644",
    "--- a/SKILL.md",
    "+++ b/SKILL.md",
    "@@ -6,3 +6,3 @@ description: Read PDFs and write letters from them",
    " # pdf-letters",
    "-Read the PDF first.",
    "+Read the PDF, then draft the letter.",
    " Keep every letter short.",
    "",
  ]),
  diffEvent("examples.md", "deleted", [
    "diff --git a/examples.md b/examples.md",
    "deleted file mode 100644",
    "index 9ab3f2e..0000000",
    "--- a/examples.md",
    "+++ /dev/null",
    "@@ -1,2 +0,0 @@",
    "-# Examples",
    "-A letter to the bank.",
    "",
  ]),
  diffEvent("letter.md", "added", [
    "diff --git a/letter.md b/letter.md",
    "new file mode 100644",
    "index 0000000..2f1c9e0",
    "--- /dev/null",
    "+++ b/letter.md",
    "@@ -0,0 +1 @@",
    "+Dear {name},",
    "",
  ]),
];
