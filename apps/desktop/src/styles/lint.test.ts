import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { join } from "node:path";

// The design-system lint (.oxlintrc.json) is the guard that keeps components on DESIGN.md. This
// runs it on a fixture of off-system code so a weakened rule fails here, not in review.
const root = join(__dirname, "../..");
const fixture = "lint/fixtures/off-system.tsx";

describe("design-system lint", () => {
  it("rejects every `bad` line of the fixture and accepts every `ok` line", () => {
    let output: string;
    try {
      output = execFileSync(join(root, "node_modules/.bin/oxlint"), ["-f", "json", fixture], {
        cwd: root,
        encoding: "utf8",
      });
    } catch (e) {
      output = (e as { stdout: string }).stdout; // oxlint exits 1 when it reports errors
    }
    const { diagnostics } = JSON.parse(output) as { diagnostics: { labels: { span: { line: number } }[] }[] };
    const flagged = new Set(diagnostics.map((d) => d.labels[0].span.line));

    const lines = readFileSync(join(root, fixture), "utf8").split("\n");
    const marked = (kind: string) =>
      lines.flatMap((l, i) => (l.includes(`{/* ${kind}:`) ? [`${i + 1}: ${l.trim()}`] : []));
    const lineOf = (entry: string) => Number(entry.split(":")[0]);

    expect(marked("bad").length).toBeGreaterThan(0);
    expect(marked("bad").filter((e) => !flagged.has(lineOf(e)))).toEqual([]);
    expect(marked("ok").filter((e) => flagged.has(lineOf(e)))).toEqual([]);
  });
});
